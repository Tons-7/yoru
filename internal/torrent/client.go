package torrent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/parse"
)

type Client struct {
	http *http.Client

	stream *http.Client
	base   string
	engine Engine

	mu         sync.Mutex
	badMagnets map[string]time.Time
	secret     string
}

type Engine interface {
	Ensure(context.Context) error
}

func (c *Client) WithEngine(e Engine) *Client {
	c.engine = e
	return c
}

type Limits struct {
	DownloadBytes int64
	Upload        string

	Adapter string
}

type Network struct {
	Adapter string `json:"adapter"`
	Address string `json:"address,omitempty"`
	Waiting bool   `json:"waiting"`
}

type NetworkReporter interface{ Network() Network }

func (c *Client) Network() Network {
	if r, ok := c.engine.(NetworkReporter); ok {
		return r.Network()
	}
	return Network{}
}

const (
	UploadWhileDownloading = "while_downloading"
	UploadNever            = "never"
)

type Limiter interface{ SetLimits(Limits) }

func (c *Client) SetLimits(l Limits) bool {
	lim, ok := c.engine.(Limiter)
	if ok {
		lim.SetLimits(l)
	}
	return ok
}

const SecretHeader = "X-Yoru-Engine"

func NewSecret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) WithSecret(secret string) *Client {
	c.secret = secret
	return c
}

func (c *Client) WithTransport(rt http.RoundTripper) *Client {
	c.http.Transport = rt
	c.stream.Transport = rt
	return c
}

func (c *Client) Addr() string {
	if u, err := url.Parse(c.base); err == nil {
		return u.Host
	}
	return ""
}

func (c *Client) url(path string) string { return c.base + path }

var ErrUnavailable = errors.New("torrent engine unavailable")

func Within(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

const inspectFailTTL = 3 * time.Minute

func NewClient(base string) *Client {
	return &Client{
		http:       &http.Client{Timeout: 60 * time.Second},
		stream:     &http.Client{},
		base:       strings.TrimRight(base, "/"),
		badMagnets: map[string]time.Time{},
	}
}

type File struct {
	Name       string   `json:"name"`
	Components []string `json:"components"`
	Length     int64    `json:"length"`
	Included   bool     `json:"included"`
}

type Torrent struct {
	ID      int    `json:"id"`
	Details Detail `json:"details"`
}

type Detail struct {
	InfoHash     string `json:"info_hash"`
	Name         string `json:"name"`
	OutputFolder string `json:"output_folder"`
	Files        []File `json:"files"`
}

type Stats struct {
	State         string `json:"state"`
	Error         string `json:"error"`
	Warning       string `json:"warning,omitempty"`
	ProgressBytes int64  `json:"progress_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Finished      bool   `json:"finished"`

	FileProgress []int64 `json:"file_progress"`
	FileIncluded []bool  `json:"file_included"`
	Live         *Live   `json:"live"`
}

type Live struct {
	DownloadSpeed struct {
		Mbps float64 `json:"mbps"`
	} `json:"download_speed"`
	Snapshot struct {
		FetchedBytes int64 `json:"fetched_bytes"`
		PeerStats    struct {
			Live int `json:"live"`
		} `json:"peer_stats"`
	} `json:"snapshot"`
}

func (l *Live) BytesPerSecond() float64 { return l.DownloadSpeed.Mbps * (1 << 20) }

func (s Stats) Peers() int {
	if s.Live == nil {
		return 0
	}
	return s.Live.Snapshot.PeerStats.Live
}

func (s Stats) Received() int64 {
	if s.Live == nil {
		return s.ProgressBytes
	}
	return max(s.ProgressBytes, s.Live.Snapshot.FetchedBytes)
}

func (s Stats) FileDone(d Detail, i int) bool {
	if i < 0 || i >= len(d.Files) || i >= len(s.FileProgress) {
		return s.Finished
	}
	return d.Files[i].Length > 0 && s.FileProgress[i] >= d.Files[i].Length
}

func (s Stats) Percent() float64 {
	if s.TotalBytes == 0 {
		return 0
	}
	return float64(s.ProgressBytes) / float64(s.TotalBytes) * 100
}

const inspectTimeout = 25 * time.Second

func (c *Client) Inspect(parent context.Context, magnet string) (*Torrent, error) {
	if until, bad := c.magnetFailedRecently(magnet); bad {
		return nil, fmt.Errorf("metadata did not resolve; skipping for %s", time.Until(until).Round(time.Second))
	}

	ctx, cancel := context.WithTimeout(parent, inspectTimeout)
	defer cancel()

	got, err := c.add(ctx, magnet, url.Values{"list_only": {"true"}})
	if err != nil && parent.Err() == nil {
		c.markMagnetFailed(magnet)
	}
	return got, err
}

const pickedInspectTimeout = 3 * time.Minute

func (c *Client) InspectPicked(parent context.Context, magnet string) (*Torrent, error) {
	ctx, cancel := context.WithTimeout(parent, pickedInspectTimeout)
	defer cancel()
	return c.add(ctx, magnet, url.Values{"list_only": {"true"}})
}

func (c *Client) magnetFailedRecently(magnet string) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.badMagnets[magnet]
	if ok && time.Now().Before(until) {
		return until, true
	}
	if ok {
		delete(c.badMagnets, magnet)
	}
	return time.Time{}, false
}

func (c *Client) markMagnetFailed(magnet string) {
	c.mu.Lock()
	c.badMagnets[magnet] = time.Now().Add(inspectFailTTL)
	c.mu.Unlock()
}

func (c *Client) Add(ctx context.Context, magnet string, file File) (*Torrent, error) {
	return c.add(ctx, magnet, url.Values{
		"overwrite":        {"true"},
		"only_files_regex": {"^" + regexp.QuoteMeta(file.Name) + "$"},
	})
}

func (c *Client) add(ctx context.Context, magnet string, params url.Values) (*Torrent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.url("/torrents?"+params.Encode()), strings.NewReader(magnet))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")

	var out Torrent
	if err := c.do(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Details(ctx context.Context, id int) (Detail, error) {
	var d Detail
	err := c.get(ctx, fmt.Sprintf("/torrents/%d", id), &d)
	return d, err
}

func (c *Client) Stats(ctx context.Context, id int) (Stats, error) {
	var s Stats
	err := c.get(ctx, fmt.Sprintf("/torrents/%d/stats/v1", id), &s)
	return s, err
}

func (c *Client) StreamURL(id, fileIndex int) string {
	return c.url(fmt.Sprintf("/torrents/%d/stream/%d", id, fileIndex))
}

func (c *Client) WaitLive(ctx context.Context, id int, timeout time.Duration) error {
	until := time.Now().Add(timeout)
	var resumed bool
	var verified int64

	for {
		stats, err := c.Stats(ctx, id)
		switch {
		case err != nil:
		case stats.Error != "":
			return fmt.Errorf("torrent %d: %s", id, stats.Error)
		case stats.State == "live" || stats.Finished:
			return nil
		case stats.State == "initializing" && stats.ProgressBytes > verified:
			verified = stats.ProgressBytes
			until = time.Now().Add(timeout)
		case stats.State == "paused" && !resumed:
			resumed = true
			if err := c.Start(ctx, id); err != nil {
				return fmt.Errorf("torrent %d is paused and would not start: %w", id, err)
			}
		}

		if time.Now().After(until) {
			return fmt.Errorf("torrent %d not live within %s", id, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (c *Client) Prewarm(ctx context.Context, id, fileIndex int, window int64) error {
	if window <= 0 {
		window = 2 << 20
	}
	if err := c.WaitLive(ctx, id, 60*time.Second); err != nil {
		return err
	}
	stream := c.StreamURL(id, fileIndex)

	for _, spec := range []string{
		fmt.Sprintf("bytes=-%d", window),
		fmt.Sprintf("bytes=0-%d", window-1),
	} {
		if err := c.readRange(ctx, stream, spec, window); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) PrewarmHead(ctx context.Context, id, fileIndex int, window int64) error {
	if window <= 0 {
		window = 2 << 20
	}
	if err := c.WaitLive(ctx, id, 60*time.Second); err != nil {
		return err
	}
	return c.readRange(ctx, c.StreamURL(id, fileIndex),
		fmt.Sprintf("bytes=0-%d", window-1), window)
}

func (c *Client) streamClient() *http.Client {
	if c.stream != nil {
		return c.stream
	}
	return http.DefaultClient
}

func (c *Client) readRange(ctx context.Context, stream, spec string, window int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stream, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", spec)

	res, err := c.streamClient().Do(req)
	if err != nil {
		return fmt.Errorf("prewarm %s: %w", spec, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("prewarm %s: engine answered %s", spec, res.Status)
	}
	want := window
	if res.ContentLength >= 0 && res.ContentLength < want {
		want = res.ContentLength
	}
	n, err := io.Copy(io.Discard, io.LimitReader(res.Body, window))
	if err != nil {
		return fmt.Errorf("prewarm %s: %w", spec, err)
	}
	if n < want {
		return fmt.Errorf("prewarm %s: got %d of %d bytes", spec, n, want)
	}
	return nil
}

func (c *Client) PrioritiseAt(ctx context.Context, id, fileIndex int, fraction float64) error {
	if fraction <= 0 || fraction >= 1 {
		return nil
	}

	d, err := c.Details(ctx, id)
	if err != nil {
		return err
	}
	if fileIndex < 0 || fileIndex >= len(d.Files) || d.Files[fileIndex].Length <= 0 {
		return nil
	}

	const window = 2 << 20
	offset := int64(float64(d.Files[fileIndex].Length) * fraction)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.StreamURL(id, fileIndex), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+window-1))

	res, err := c.streamClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, window))
	return nil
}

var ErrPaused = errors.New("download paused")

var AwaitPoll = 5 * time.Second

type StallError struct {
	Percent float64
	Quiet   time.Duration
	Cause   string
}

func (e *StallError) Error() string { return fmt.Sprintf("stalled at %.0f%%", e.Percent) }

type SlowError struct {
	ID      int
	Rate    float64
	Percent float64
}

func (e *SlowError) Error() string {
	return fmt.Sprintf("downloading at %.0f KB/s at %.0f%%", e.Rate/1024, e.Percent)
}

type sample struct {
	at    time.Time
	bytes int64
}

func (c *Client) Await(ctx context.Context, id int, stall time.Duration) error {
	return c.AwaitPaced(ctx, id, stall, 0, 0, 0)
}

func (c *Client) AwaitPaced(ctx context.Context, id int, stall time.Duration, minRate float64, window time.Duration, fewPeers int) error {
	var best int64
	var quiet time.Duration
	var samples []sample

	for {
		stats, err := c.Stats(ctx, id)
		switch {
		case err != nil:
			return err
		case stats.Finished:
			return nil
		case stats.Error != "":
			return fmt.Errorf("%s", stats.Error)
		case stats.State == "paused":
			return ErrPaused
		}

		if stats.ProgressBytes > best {
			best, quiet = stats.ProgressBytes, 0
		} else {
			quiet += AwaitPoll
			if quiet >= stall {
				return &StallError{Percent: stats.Percent(), Quiet: quiet, Cause: stats.Warning}
			}
		}
		if minRate > 0 && window > 0 {
			now := time.Now()
			samples = append(samples, sample{now, stats.ProgressBytes})
			for len(samples) > 1 && now.Sub(samples[1].at) >= window {
				samples = samples[1:]
			}
			if span := now.Sub(samples[0].at); span >= window {
				rate := float64(stats.ProgressBytes-samples[0].bytes) / span.Seconds()
				if rate < minRate && (fewPeers <= 0 || stats.Peers() < fewPeers) {
					return &SlowError{ID: id, Rate: rate, Percent: stats.Percent()}
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(AwaitPoll):
		}
	}
}

func (s Stats) Checking() bool { return s.State == "initializing" && !s.Finished }

func (c *Client) PauseUnfinished(ctx context.Context, keep func(infoHash string) bool) (paused, kept int, checking []string, err error) {
	list, err := c.List(ctx)
	if err != nil {
		return 0, 0, nil, err
	}

	for _, t := range list.Torrents {
		stats, err := c.Stats(ctx, t.ID)
		if err != nil || stats.Finished || stats.State == "paused" {
			continue
		}
		if stats.Checking() {
			checking = append(checking, strings.ToLower(t.InfoHash))
			continue
		}
		if keep != nil && keep(strings.ToLower(t.InfoHash)) {
			kept++
			continue
		}
		if err := c.Pause(ctx, t.ID); err == nil {
			paused++
		}
	}
	return paused, kept, checking, nil
}

func (c *Client) Delete(ctx context.Context, id int) error {
	return c.post(ctx, fmt.Sprintf("/torrents/%d/delete", id))
}

func (c *Client) Forget(ctx context.Context, id int) error {
	return c.post(ctx, fmt.Sprintf("/torrents/%d/forget", id))
}

func alreadyThere(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already live") || strings.Contains(msg, "already paused")
}

func (c *Client) Pause(ctx context.Context, id int) error {
	err := c.post(ctx, fmt.Sprintf("/torrents/%d/pause", id))
	if alreadyThere(err) {
		return nil
	}
	return err
}

func (c *Client) Start(ctx context.Context, id int) error {
	err := c.post(ctx, fmt.Sprintf("/torrents/%d/start", id))
	if alreadyThere(err) {
		return nil
	}
	return err
}

func (c *Client) IncludeFile(ctx context.Context, id, index int, on bool) error {
	verb := "exclude"
	if on {
		verb = "include"
	}
	return c.post(ctx, fmt.Sprintf("/torrents/%d/files/%d/%s", id, index, verb))
}

func (c *Client) DeleteFile(ctx context.Context, id, index int) error {
	return c.post(ctx, fmt.Sprintf("/torrents/%d/files/%d/delete", id, index))
}

type listing struct {
	Torrents []struct {
		ID       int    `json:"id"`
		InfoHash string `json:"info_hash"`
		Name     string `json:"name"`
	} `json:"torrents"`
}

func (c *Client) List(ctx context.Context) (listing, error) {
	var out listing
	err := c.get(ctx, "/torrents", &out)
	return out, err
}

func (c *Client) Live(ctx context.Context) (map[string]int, error) {
	list, err := c.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]int, len(list.Torrents))
	for _, t := range list.Torrents {
		if t.InfoHash != "" {
			out[strings.ToLower(t.InfoHash)] = t.ID
		}
	}
	return out, nil
}

func (c *Client) Ready(ctx context.Context) bool {
	_, err := c.List(ctx)
	return err == nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *Client) do(req *http.Request, out any) error {
	if c.engine != nil {
		if err := c.engine.Ensure(req.Context()); err != nil {
			return err
		}
	}
	if c.secret != "" {
		req.Header.Set(SecretHeader, c.secret)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("engine %s: HTTP %d: %s",
			req.URL.Path, res.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

func PickVideo(files []File) (File, int, bool) {
	best, bestIdx := File{}, -1
	for i, f := range files {
		if !parse.IsVideo(f.Name) {
			continue
		}
		if bestIdx == -1 || f.Length > best.Length {
			best, bestIdx = f, i
		}
	}
	return best, bestIdx, bestIdx >= 0
}

func PickEpisode(files []File, numbers ...int) (File, int, bool) {
	if len(numbers) == 0 {
		return PickVideo(files)
	}
	return PickNumbered(files, numbers[0], numbers)
}

func PickNumbered(files []File, episode int, numbers []int) (File, int, bool) {
	return PickInPack(files, episode, numbers, Scope{})
}

func PickInPack(files []File, episode int, numbers []int, scope Scope) (File, int, bool) {
	numbers = slices.DeleteFunc(slices.Clone(numbers), func(n int) bool { return n <= 0 })
	if episode <= 0 && len(numbers) > 0 {
		episode = numbers[0]
	}
	if episode <= 0 {
		return PickVideo(files)
	}

	var videos []int
	for i, f := range files {
		if parse.IsVideo(f.Name) && !extra(f.Name) {
			videos = append(videos, i)
		}
	}
	if len(videos) == 0 {
		return File{}, -1, false
	}

	if len(videos) == 1 {
		only := files[videos[0]]
		got := episodeInName(parse.BaseName(only.Name))
		if slices.Contains(numbers, got) || (got == 0 && episode <= 1) {
			return only, videos[0], true
		}
		return File{}, -1, false
	}

	floor := medianLength(files, videos) / 4
	numbered := map[int]int{}
	lowest, highest := 0, 0
	for _, i := range videos {
		if files[i].Length < floor {
			continue
		}
		if n := episodeInName(parse.BaseName(files[i].Name)); n > 0 {
			numbered[i] = n
			if lowest == 0 || n < lowest {
				lowest = n
			}
			highest = max(highest, n)
		}
	}
	want := packNumber(episode, numbers, lowest, highest)

	var matches []int
	for _, i := range videos {
		if numbered[i] == want {
			matches = append(matches, i)
		}
	}
	matches = inScope(files, matches, scope)
	if len(matches) == 0 {
		return File{}, -1, false
	}
	best := matches[0]
	for _, i := range matches[1:] {
		if files[i].Length > files[best].Length {
			best = i
		}
	}
	return files[best], best, true
}

type Scope struct {
	Season  int
	Counted int
	Others  []string
}

func (s Scope) seasons() []int {
	var out []int
	if s.Counted > 0 && s.Counted != s.Season {
		out = append(out, s.Counted)
	}
	if s.Season > 0 {
		out = append(out, s.Season)
	}
	return out
}

func inScope(files []File, matches []int, scope Scope) []int {
	if len(matches) == 0 {
		return nil
	}
	wanted := scope.seasons()
	root := sharedFolder(files)
	kept := slices.DeleteFunc(slices.Clone(matches), func(i int) bool {
		stated := pathSeason(files[i].Name)
		return namesOther(strings.TrimPrefix(strings.ReplaceAll(files[i].Name, `\`, "/"), root), scope.Others) ||
			(len(wanted) > 0 && stated > 0 && !slices.Contains(wanted, stated))
	})
	if len(kept) <= 1 {
		return kept
	}
	for _, season := range wanted {
		stating := slices.DeleteFunc(slices.Clone(kept), func(i int) bool { return pathSeason(files[i].Name) != season })
		if len(stating) > 0 && sameFolder(files, stating) {
			return stating
		}
	}
	if sameFolder(files, kept) {
		return kept
	}
	if scope.Season <= 1 {
		plain := slices.DeleteFunc(slices.Clone(kept), func(i int) bool { return pathSeason(files[i].Name) != 0 })
		if len(plain) > 0 && len(plain) < len(kept) && sameFolder(files, plain) {
			return plain
		}
	}
	if sameName(files, kept) && (scope.Season <= 1 || !slices.ContainsFunc(kept, func(i int) bool { return pathSeason(files[i].Name) == 0 })) {
		return kept
	}
	return nil
}

func sharedFolder(files []File) string {
	if len(files) == 0 {
		return ""
	}
	prefix := strings.Split(strings.ReplaceAll(files[0].Name, `\`, "/"), "/")
	prefix = prefix[:len(prefix)-1]
	for _, f := range files[1:] {
		parts := strings.Split(strings.ReplaceAll(f.Name, `\`, "/"), "/")
		parts = parts[:len(parts)-1]
		n := 0
		for n < len(prefix) && n < len(parts) && prefix[n] == parts[n] {
			n++
		}
		prefix = prefix[:n]
	}
	if len(prefix) == 0 {
		return ""
	}
	return strings.Join(prefix, "/") + "/"
}

func sameName(files []File, idx []int) bool {
	first := strings.ToLower(parse.BaseName(files[idx[0]].Name))
	for _, i := range idx[1:] {
		if strings.ToLower(parse.BaseName(files[i].Name)) != first {
			return false
		}
	}
	return true
}

func pathSeason(path string) int {
	parts := strings.Split(strings.ReplaceAll(path, `\`, "/"), "/")
	for k := len(parts) - 1; k >= 0; k-- {
		part := strings.NewReplacer("_", " ", ".", " ").Replace(parts[k])
		if seasonRange.MatchString(part) {
			continue
		}
		if n := parse.SeasonOf(part); n > 0 {
			return n
		}
		if n := parse.Parse(part).Season; n > 0 {
			return n
		}
		if m := wordSeason.FindStringSubmatch(part); m != nil {
			return ordinals[strings.ToLower(m[1])]
		}
	}
	return 0
}

var (
	wordSeason = regexp.MustCompile(`(?i)\b(first|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth)\s+season\b`)
	ordinals   = map[string]int{"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "sixth": 6, "seventh": 7,
		"eighth": 8, "ninth": 9, "tenth": 10}
	seasonRange = regexp.MustCompile(`(?i)\bS\d{1,2}\s?(?:-|~|&|to)\s?S\d{1,2}\b|\bSeasons\s?\d{1,2}\s?(?:-|~|&|to)\s?\d{1,2}\b|` +
		`\bSeason\s?\d{1,2}(?:-|~|&)\d{1,2}\b|\bSeason\s?\d{1,2}\s(?:~|&|to)\s\d{1,2}\b`)
	episodeMarker = regexp.MustCompile(`(?i)\bS\d{1,2}\s?E\d{1,4}|\b(?:E|EP|Episode)\s?\d{1,4}\b|\s-\s\d{1,4}(?:v\d)?\b|\[\d{1,4}(?:v\d)?\]`)
)

func namedPart(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	dir, base := "", path
	if j := strings.LastIndex(path, "/"); j >= 0 {
		dir, base = path[:j], path[j+1:]
	}
	if loc := episodeMarker.FindStringIndex(base); loc != nil {
		base = base[:loc[0]]
	}
	return dir + "/" + base
}

func namesOther(path string, others []string) bool {
	if len(others) == 0 {
		return false
	}
	words := map[string]bool{}
	for _, w := range pathWords.Split(strings.ToLower(namedPart(path)), -1) {
		words[w] = true
	}
	for _, o := range others {
		if words[strings.ToLower(o)] {
			return true
		}
	}
	return false
}

var (
	pathWords   = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	qualityTags = regexp.MustCompile(`(?i)\b(?:\d{3,4}p|4k|uhd|bd|bdrip|blu-?ray|web(?:-?dl|rip)?|hevc|x26[45]|h\.?26[45]|av1|10-?bit|8-?bit|hi10p?|flac|aac|opus|dual[\s._-]?audio|remux)\b|[\[\]\(\)_.\-]+`)
)

func sameFolder(files []File, idx []int) bool {
	dir := func(i int) string {
		p := strings.ReplaceAll(files[i].Name, `\`, "/")
		if j := strings.LastIndex(p, "/"); j >= 0 {
			return strings.TrimSpace(qualityTags.ReplaceAllString(strings.ToLower(p[:j]), " "))
		}
		return ""
	}
	first := dir(idx[0])
	for _, i := range idx[1:] {
		if dir(i) != first {
			return false
		}
	}
	return true
}

func packNumber(episode int, numbers []int, lowest, highest int) int {
	if len(numbers) == 0 {
		return 0
	}
	held := slices.DeleteFunc(slices.Clone(numbers), func(n int) bool { return n < lowest || n > highest })
	if len(held) == 1 {
		return held[0]
	}
	if len(held) == 0 {
		return numbers[0]
	}
	best, distance := held[0], -1
	for _, n := range held {
		first := n - (episode - 1)
		d := max(first-lowest, lowest-first)
		if distance < 0 || d < distance {
			best, distance = n, d
		}
	}
	return best
}

func extra(path string) bool {
	path = strings.ReplaceAll(path, `\`, "/")
	dirs := strings.Split(path, "/")
	for _, d := range dirs[:len(dirs)-1] {
		if extraDirs[strings.ToLower(d)] || strings.HasPrefix(strings.ToLower(d), "nc") {
			return true
		}
	}
	return extraName.MatchString(parse.BaseName(path))
}

var (
	extraDirs = map[string]bool{"extras": true, "extra": true, "sample": true, "samples": true, "trailers": true,
		"trailer": true, "featurettes": true, "specials": true, "sp": true, "menu": true, "menus": true, "bonus": true,
		"cm": true, "pv": true}
	extraName = regexp.MustCompile(`(?i)(?:^|[\s_.\-\[(])(?:NC(?:OP|ED)|OP|ED|PV|CM|Menu|Preview|Trailer|Sample)\s*\d{0,2}(?:$|[\s_.\-\]\)v])`)
)

func medianLength(files []File, idx []int) int64 {
	lengths := make([]int64, 0, len(idx))
	for _, i := range idx {
		lengths = append(lengths, files[i].Length)
	}
	slices.Sort(lengths)
	return lengths[len(lengths)/2]
}

var (
	episodeTags   = regexp.MustCompile(`(?i)[\[\(][^\]\)]*[\]\)]|\b\d{3,4}[pi]\b|\bx?26[45]\b`)
	bracketNum    = regexp.MustCompile(`\[(\d{1,4})(?:v\d)?\]`)
	versionSuffix = regexp.MustCompile(`(?i)(\d{1,4})v\d\b`)
	episodeNum    = regexp.MustCompile(`(?i)(?:\bS\d{1,2}E(\d{1,4})\b|(?:^|[\s_.-])(\d{1,4})(?:[\s_.-]|$))`)
	seasonEpisode = regexp.MustCompile(`(?i)\bS\d{1,2}[\s._]?E(\d{1,4})\b`)
	markedEpisode = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])E(?:P|PISODE)?\.?\s*(\d{1,4})(?:v\d)?\b`)
	dashed        = regexp.MustCompile(`[\s_]-[\s_]+(\d{1,4})(?:v\d)?(?:[\s_]|$)`)
	audioLayout   = regexp.MustCompile(`(?i)(?:^|[\s_.-]|[a-z])[2567]\.[01](?:[\s_.-]|$)`)
	fractional    = regexp.MustCompile(`(?:^|[\s_-])\d{1,4}\.\d(?:[\s_-]|$)`)
)

func episodeInName(name string) int {
	if m := bracketNum.FindStringSubmatch(name); m != nil {
		if n, _ := strconv.Atoi(m[1]); n > 0 && !(len(m[1]) == 4 && n >= 1900 && n <= 2099) {
			return n
		}
	}
	cleaned := episodeTags.ReplaceAllString(name, " ")
	if m := seasonEpisode.FindStringSubmatch(cleaned); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	if m := markedEpisode.FindStringSubmatch(cleaned); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	cleaned = audioLayout.ReplaceAllString(cleaned, " ")
	if fractional.MatchString(cleaned) {
		return 0
	}
	if m := dashed.FindStringSubmatch(cleaned); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && !(len(m[1]) == 4 && n >= 1900 && n <= 2099) {
			return n
		}
	}
	cleaned = versionSuffix.ReplaceAllString(cleaned, "$1 ")

	matches := episodeNum.FindAllStringSubmatch(cleaned, -1)
	for _, match := range slices.Backward(matches) {
		for _, group := range match[1:] {
			if group == "" {
				continue
			}
			n, err := strconv.Atoi(group)
			if err != nil || n <= 0 || n >= 5000 {
				continue
			}
			if len(group) == 4 && n >= 1900 && n <= 2099 {
				continue
			}
			return n
		}
	}
	return 0
}

type ByteRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type FileRanges struct {
	Length int64       `json:"length"`
	Ranges []ByteRange `json:"ranges"`
}

func (c *Client) Ranges(ctx context.Context, id, file int) (FileRanges, error) {
	var out FileRanges
	err := c.get(ctx, fmt.Sprintf("/torrents/%d/files/%d/ranges", id, file), &out)
	return out, err
}

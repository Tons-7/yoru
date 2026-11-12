package update

import (
	"archive/zip"
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"yoru/internal/config"
	"yoru/internal/store"
)

var Version = "dev"

const (
	Repo      = "Tons-7/yoru"
	userAgent = "Yoru"
	sumsFile  = "SHA256SUMS.txt"
	sigFile   = sumsFile + ".sig"
)

var signingKeyHex = "22e0c47d84c95b91d24c386c08e994718a23cec6332f187c92bb3ed5ad4c279b"

var signingKey = mustPublicKey(signingKeyHex)

func mustPublicKey(hexKey string) ed25519.PublicKey {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		panic("update: bad signing key")
	}
	return ed25519.PublicKey(key)
}

type Stage string

const (
	StageIdle        Stage = "idle"
	StageDownloading Stage = "downloading"
	StageVerifying   Stage = "verifying"
	StageReady       Stage = "ready"
	StageRestarting  Stage = "restarting"
	StageFailed      Stage = "failed"
)

type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Sums    string `json:"-"`
	Sig     string `json:"-"`
	Notes   string `json:"notes,omitempty"`
}

type Notes struct {
	Version   string    `json:"version"`
	Notes     string    `json:"notes"`
	Published time.Time `json:"published,omitzero"`
}

type Status struct {
	Current   string    `json:"current"`
	Latest    *Release  `json:"latest,omitempty"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	CheckErr  string    `json:"checkError,omitempty"`
	Stage     Stage     `json:"stage"`
	Bytes     int64     `json:"bytes"`
	Total     int64     `json:"total"`
	Error     string    `json:"error,omitempty"`
	Staged    string    `json:"staged,omitempty"`

	Pending []Notes `json:"pending,omitempty"`

	WhatsNew []Notes `json:"whatsNew,omitempty"`

	UpdatedFrom string `json:"updatedFrom,omitempty"`
}

type Updater struct {
	store   *store.Store
	exe     string
	staging string
	api     string

	web     string
	http    *http.Client
	log     *slog.Logger
	restart func()
	launch  func(exe string) error

	mu     sync.Mutex
	status Status
}

func New(st *store.Store, exe, staging string, log *slog.Logger) *Updater {
	api, web := "https://api.github.com", "https://github.com"

	if override := config.Env("UPDATE_API"); override != "" {
		api, web = override, override
	}
	return &Updater{
		store: st, exe: exe, staging: staging,
		api: api, web: web,
		http:   &http.Client{Timeout: 10 * time.Minute},
		log:    log,
		launch: relaunch,
		status: Status{Current: Version, Stage: StageIdle},
	}
}

func (u *Updater) WithRestart(f func()) *Updater { u.restart = f; return u }

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

func (u *Updater) set(apply func(*Status)) {
	u.mu.Lock()
	apply(&u.status)
	u.mu.Unlock()
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Body       string    `json:"body"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (u *Updater) Check(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rel, err := u.latest(ctx)
	var all []Notes
	if _, limited := errors.AsType[*rateLimited](err); limited {

		if site, serr := u.latestFromSite(ctx); serr == nil {
			u.log.Info("GitHub's API is limiting this network; read the latest release from its site", "version", site.Version)
			rel, err, all = site, nil, u.knownNotes(site.Version)

			if feed, ferr := u.feedNotes(ctx); ferr == nil {
				all = withNotes(all, feed)
				if i := slices.IndexFunc(feed, func(n Notes) bool { return n.Version == site.Version }); i >= 0 {
					rel.Notes = feed[i].Notes
				}
			} else {
				u.log.Warn("release notes from GitHub's feed", "err", ferr)
			}
		} else {
			u.log.Warn("latest release from GitHub's site", "err", serr)
		}
	} else if err == nil {

		if all, err = u.releases(ctx); err != nil {
			u.log.Warn("release notes", "err", err)
			all, err = []Notes{{Version: rel.Version, Notes: rel.Notes}}, nil
		}
	}
	staged := ""
	if st := u.Status(); st.Stage == StageIdle || st.Stage == StageFailed {
		staged = u.verifiedStaged()
	}
	u.set(func(s *Status) {
		s.CheckedAt = time.Now()
		s.CheckErr = ""
		if err != nil {
			s.CheckErr = err.Error()
			return
		}
		s.Latest = &rel
		s.Available = rel.URL != "" && Newer(rel.Version, Version)
		switch {
		case s.Available && (s.Stage == StageIdle || s.Stage == StageFailed) && staged == rel.Version:
			s.Stage, s.Staged, s.Error = StageReady, staged, ""
		case s.Stage == StageReady && s.Staged != rel.Version:
			s.Stage, s.Staged = StageIdle, ""
		}
		s.Pending = pending(all, Version)
		s.WhatsNew = whatsNew(all, Version, s.UpdatedFrom)
	})
	if err != nil {
		return u.Status(), err
	}

	if st := u.Status(); st.Available {
		u.notify(ctx, rel)
	}
	return u.Status(), nil
}

func (u *Updater) latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		u.api+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := u.http.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return Release{}, errors.New("no releases published yet")
	}
	if err := limitOf(res); err != nil {
		return Release{}, err
	}
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github: HTTP %d", res.StatusCode)
	}

	var gh ghRelease
	if err := json.NewDecoder(res.Body).Decode(&gh); err != nil {
		return Release{}, err
	}

	rel := Release{Version: strings.TrimPrefix(gh.Tag, "v"), Notes: gh.Body}
	want := AssetName(rel.Version)
	for _, a := range gh.Assets {
		switch a.Name {
		case want:
			rel.URL = a.URL
		case sumsFile:
			rel.Sums = a.URL
		case sigFile:
			rel.Sig = a.URL
		}
	}
	return rel, nil
}

type rateLimited struct{ reset time.Time }

func (e *rateLimited) Error() string {
	if e.reset.IsZero() {
		return "GitHub is limiting update checks from this network for now; Yoru tries again later"
	}
	return fmt.Sprintf("GitHub is limiting update checks from this network until %s; Yoru tries again later",
		e.reset.Local().Format("15:04"))
}

func limitOf(res *http.Response) error {
	if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	if after, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil {
		return &rateLimited{reset: time.Now().Add(time.Duration(after) * time.Second)}
	}
	if res.Header.Get("X-RateLimit-Remaining") != "0" {
		return nil
	}
	reset, _ := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64)
	if reset == 0 {
		return &rateLimited{}
	}
	return &rateLimited{reset: time.Unix(reset, 0)}
}

func (u *Updater) latestFromSite(ctx context.Context) (Release, error) {
	noFollow := *u.http
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.web+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := noFollow.Do(req)
	if err != nil {
		return Release{}, err
	}
	res.Body.Close()
	loc := res.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if res.StatusCode/100 != 3 || i < 0 {
		return Release{}, fmt.Errorf("github: no latest release (HTTP %d)", res.StatusCode)
	}
	tag := loc[i+len("/tag/"):]
	rel := Release{Version: strings.TrimPrefix(tag, "v")}
	base := u.web + "/" + Repo + "/releases/download/" + tag + "/"
	rel.Sums, rel.Sig = base+sumsFile, base+sigFile

	asset := base + AssetName(rel.Version)
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, asset, nil)
	if err != nil {
		return Release{}, err
	}
	head.Header.Set("User-Agent", userAgent)
	found, err := u.http.Do(head)
	if err != nil {
		return Release{}, err
	}
	found.Body.Close()
	switch {
	case found.StatusCode == http.StatusOK:
		rel.URL = asset
	case found.StatusCode != http.StatusNotFound:
		return Release{}, fmt.Errorf("github: %s: HTTP %d", AssetName(rel.Version), found.StatusCode)
	}
	return rel, nil
}

func (u *Updater) knownNotes(latest string) []Notes {
	st := u.Status()
	all := append(slices.Clone(st.Pending), st.WhatsNew...)
	if Newer(latest, Version) && !slices.ContainsFunc(all, func(n Notes) bool { return n.Version == latest }) {
		all = append(all, Notes{Version: latest})
	}
	return newestFirst(all)
}

func withNotes(known, feed []Notes) []Notes {
	out := slices.Clone(known)
	for _, n := range feed {
		if i := slices.IndexFunc(out, func(k Notes) bool { return k.Version == n.Version }); i >= 0 {
			out[i].Notes = cmp.Or(out[i].Notes, n.Notes)
		} else {
			out = append(out, n)
		}
	}
	return newestFirst(out)
}

func (u *Updater) feedNotes(ctx context.Context) ([]Notes, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.web+"/"+Repo+"/releases.atom", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: release feed (HTTP %d)", res.StatusCode)
	}
	var feed struct {
		Entries []struct {
			Link struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&feed); err != nil {
		return nil, fmt.Errorf("github: release feed: %w", err)
	}
	var out []Notes
	for _, e := range feed.Entries {
		i := strings.LastIndex(e.Link.Href, "/tag/")
		if i < 0 {
			continue
		}
		out = append(out, Notes{Version: strings.TrimPrefix(e.Link.Href[i+len("/tag/"):], "v"), Notes: htmlNotes(e.Content)})
	}
	return out, nil
}

func htmlNotes(rendered string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(rendered))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return strings.TrimSpace(blankLines.ReplaceAllString(b.String(), "\n\n"))
		case html.TextToken:

			text := string(z.Text())
			if strings.TrimSpace(text) != "" {
				b.WriteString(strings.ReplaceAll(text, "\n", " "))
			} else if !strings.Contains(text, "\n") {
				b.WriteString(" ")
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			start := tt != html.EndTagToken
			switch tag := string(name); {
			case len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6':
				b.WriteString("\n\n")
				if start {
					b.WriteString("## ")
				}
			case tag == "li" && start:
				b.WriteString("\n- ")
			case tag == "strong" || tag == "b":
				b.WriteString("**")
			case tag == "p" || tag == "ul" || tag == "ol":
				b.WriteString("\n\n")
			case tag == "br":
				b.WriteString("\n")
			}
		}
	}
}

var blankLines = regexp.MustCompile(`\n{3,}`)

func newestFirst(out []Notes) []Notes {
	slices.SortStableFunc(out, func(a, b Notes) int {
		switch {
		case Newer(a.Version, b.Version):
			return -1
		case Newer(b.Version, a.Version):
			return 1
		}
		return 0
	})
	return out
}

func (u *Updater) releases(ctx context.Context) ([]Notes, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		u.api+"/repos/"+Repo+"/releases?per_page=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: HTTP %d", res.StatusCode)
	}
	var list []ghRelease
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&list); err != nil {
		return nil, err
	}
	out := make([]Notes, 0, len(list))
	for _, r := range list {
		if r.Draft || r.Prerelease {
			continue
		}
		out = append(out, Notes{Version: strings.TrimPrefix(r.Tag, "v"), Notes: r.Body, Published: r.Published})
	}
	return newestFirst(out), nil
}

func pending(all []Notes, current string) []Notes {
	var out []Notes
	for _, n := range all {
		if Newer(n.Version, current) {
			out = append(out, n)
		}
	}
	return out
}

func whatsNew(all []Notes, current, from string) []Notes {
	if current == "dev" {
		return nil
	}
	var out []Notes
	for _, n := range all {
		if Newer(n.Version, current) {
			continue
		}
		same := !Newer(current, n.Version)
		if same || (from != "" && Newer(n.Version, from)) {
			out = append(out, n)
		}
	}
	return out
}

func (u *Updater) Started(ctx context.Context) {
	if u.store == nil || Version == "dev" {
		return
	}
	last, _ := u.store.Setting(ctx, "update.version")
	if last == Version {
		from, _ := u.store.Setting(ctx, "update.previous")
		u.set(func(s *Status) { s.UpdatedFrom = from })
		return
	}
	updated := last != "" && Newer(Version, last)

	if last == "" {
		notified, _ := u.store.Setting(ctx, "update.notified")
		updated = notified == Version
	}
	from := ""
	if updated {
		from = last
	}
	if err := u.store.SetSetting(ctx, "update.version", Version); err != nil {
		u.log.Warn("remember the running version", "err", err)
	}
	if err := u.store.SetSetting(ctx, "update.previous", from); err != nil {
		u.log.Warn("remember the previous version", "err", err)
	}
	u.set(func(s *Status) { s.UpdatedFrom = from })
	if !updated {
		return
	}
	if _, err := u.store.AddNotification(ctx, store.Notification{
		Kind:    store.NotifyUpdate,
		Title:   fmt.Sprintf("Updated to Yoru %s", Version),
		Body:    "See what's new in Settings → About",
		Payload: map[string]any{"version": Version, "updated": true},
	}); err != nil {
		u.log.Warn("updated notification", "err", err)
	}
}

func (u *Updater) fetchSmall(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 64<<10))
}

func AssetName(version string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("%s-%s-update.zip", config.Name, version)
	}
	return fmt.Sprintf("%s-%s-%s-%s.zip", config.Name, version, runtime.GOOS, runtime.GOARCH)
}

func (u *Updater) notify(ctx context.Context, rel Release) {
	if u.store == nil {
		return
	}
	if seen, _ := u.store.Setting(ctx, "update.notified"); seen == rel.Version {
		return
	}
	if _, err := u.store.AddNotification(ctx, store.Notification{
		Kind:    store.NotifyUpdate,
		Title:   fmt.Sprintf("Yoru %s is available", rel.Version),
		Body:    "Open Settings → About to update",
		Payload: map[string]any{"version": rel.Version},
	}); err != nil {
		u.log.Warn("update notification", "err", err)
		return
	}
	if err := u.store.SetSetting(ctx, "update.notified", rel.Version); err != nil {
		u.log.Warn("remember update notification", "err", err)
	}
}

func (u *Updater) Apply() error { return u.fetch(true) }

func (u *Updater) Prepare() error { return u.fetch(false) }

func (u *Updater) fetch(thenInstall bool) error {
	u.mu.Lock()
	st := u.status
	if st.Stage == StageDownloading || st.Stage == StageVerifying || st.Stage == StageRestarting {
		u.mu.Unlock()
		return nil
	}
	if st.Stage == StageReady {
		u.mu.Unlock()
		if thenInstall {
			return u.Install()
		}
		return nil
	}
	if !st.Available || st.Latest == nil {
		u.mu.Unlock()
		return errors.New("no update available")
	}
	rel := *st.Latest
	u.status.Stage, u.status.Bytes, u.status.Total, u.status.Error = StageDownloading, 0, 0, ""
	u.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if err := u.stage(ctx, rel); err != nil {
			u.log.Error("update", "version", rel.Version, "err", err)
			u.set(func(s *Status) { s.Stage, s.Error = StageFailed, err.Error() })
			return
		}
		u.set(func(s *Status) { s.Stage, s.Staged = StageReady, rel.Version })
		if !thenInstall {
			u.log.Info("update downloaded; it installs when a restart is asked for", "version", rel.Version)
			return
		}
		u.Install()
	}()
	return nil
}

func (u *Updater) Install() error {
	u.mu.Lock()
	if u.status.Stage != StageReady {
		u.mu.Unlock()
		return errors.New("no update is downloaded yet")
	}
	version := u.status.Staged
	u.status.Stage = StageRestarting
	u.mu.Unlock()

	if err := u.install(version); err != nil {
		u.log.Error("update", "version", version, "err", err)
		u.set(func(s *Status) { s.Stage, s.Staged, s.Error = StageFailed, "", err.Error() })
		return err
	}
	u.log.Info("restarting into the new version", "version", version)
	if u.restart != nil {
		go u.restart()
	}
	return nil
}

type stagedMark struct {
	Version string `json:"version"`
	Archive string `json:"archive"`
}

func (u *Updater) markPath() string { return filepath.Join(u.staging, "staged.json") }
func (u *Updater) sumsPath() string { return filepath.Join(u.staging, sumsFile) }
func (u *Updater) sigPath() string  { return filepath.Join(u.staging, sumsFile+".sig") }

func (u *Updater) stagedMark() (stagedMark, bool) {
	raw, err := os.ReadFile(u.markPath())
	if err != nil {
		return stagedMark{}, false
	}
	var mark stagedMark
	if json.Unmarshal(raw, &mark) != nil || mark.Version == "" || mark.Archive != AssetName(mark.Version) {
		return stagedMark{}, false
	}
	return mark, true
}

func (u *Updater) stagedVersion() string {
	mark, ok := u.stagedMark()
	if !ok || !Newer(mark.Version, Version) {
		return ""
	}
	return mark.Version
}

func (u *Updater) verifiedStaged() string {
	mark, ok := u.stagedMark()
	if !ok {
		return ""
	}
	if !Newer(mark.Version, Version) {
		u.forgetIfIdle()
		return ""
	}
	if _, err := u.stagedArchive(mark.Version); err != nil {
		u.log.Warn("downloaded update dropped", "version", mark.Version, "err", err)
		u.forgetIfIdle()
		return ""
	}
	return mark.Version
}

func (u *Updater) forgetIfIdle() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status.Stage == StageIdle || u.status.Stage == StageFailed {
		u.forgetStaged()
	}
}

func (u *Updater) stagedArchive(version string) (string, error) {
	mark, ok := u.stagedMark()
	if !ok || mark.Version != version {
		return "", errors.New("the downloaded update is missing; check again")
	}
	raw, err := os.ReadFile(u.sumsPath())
	if err != nil {
		return "", errors.New("the downloaded update is missing its checksums; check again")
	}
	sig, err := os.ReadFile(u.sigPath())
	if err != nil {
		return "", errors.New("the downloaded update is missing its signature; check again")
	}
	archive := filepath.Join(u.staging, mark.Archive)
	if err := verifyArchive(raw, sig, archive); err != nil {
		return "", fmt.Errorf("the downloaded update was changed: %w", err)
	}
	return archive, nil
}

func (u *Updater) forgetStaged() {
	if mark, ok := u.stagedMark(); ok {
		os.Remove(filepath.Join(u.staging, mark.Archive))
	}
	for _, path := range []string{u.markPath(), u.sumsPath(), u.sigPath()} {
		os.Remove(path)
	}
}

func (u *Updater) stage(ctx context.Context, rel Release) error {
	if rel.Sums == "" || rel.Sig == "" {
		return errors.New("release carries no signed " + sumsFile + "; refusing an unverified binary")
	}
	if err := os.MkdirAll(u.staging, 0o755); err != nil {
		return err
	}
	u.forgetStaged()
	name := filepath.Base(rel.URL)
	archive := filepath.Join(u.staging, name)
	kept := false
	defer func() {
		if !kept {
			os.Remove(archive)
		}
	}()

	if err := u.download(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	u.set(func(s *Status) { s.Stage = StageVerifying })
	raw, err := u.fetchSmall(ctx, rel.Sums)
	if err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	sig, err := u.fetchSmall(ctx, rel.Sig)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	if err := verifyArchive(raw, sig, archive); err != nil {
		return err
	}
	if err := os.WriteFile(u.sumsPath(), raw, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(u.sigPath(), sig, 0o644); err != nil {
		return err
	}
	mark, _ := json.Marshal(stagedMark{Version: rel.Version, Archive: name})
	if err := os.WriteFile(u.markPath(), mark, 0o644); err != nil {
		return err
	}
	kept = true
	return nil
}

func (u *Updater) install(version string) error {
	archive, err := u.stagedArchive(version)
	if err != nil {
		u.forgetStaged()
		return err
	}
	fresh := u.exe + ".new"
	if err := extractExe(archive, fresh); err != nil {
		os.Remove(fresh)
		return fmt.Errorf("unpack: %w", err)
	}
	if err := swap(u.exe, fresh); err != nil {
		os.Remove(fresh)
		return fmt.Errorf("replace executable: %w", err)
	}
	if err := u.launch(u.exe); err != nil {
		if rerr := Rollback(u.exe); rerr != nil {
			u.log.Error("restore the previous version", "err", rerr)
		}
		return fmt.Errorf("start the new version: %w", err)
	}
	u.forgetStaged()
	return nil
}

func Rollback(exe string) error {
	old := exe + ".old"
	if _, err := os.Stat(old); err != nil {
		return err
	}
	failed := exe + ".failed"
	os.Remove(failed)
	if err := os.Rename(exe, failed); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(old, exe); err != nil {
		os.Rename(failed, exe)
		return err
	}
	return nil
}

func Handover(args []string) bool {
	return slices.Contains(args, "--wait-for")
}

func Relaunch(exe string) error {
	cmd := exec.Command(exe, carried(os.Args[1:])...)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	config.Detach(cmd)
	return cmd.Start()
}

func (u *Updater) download(ctx context.Context, url, dest string) error {

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	quiet := time.AfterFunc(stallAfter, func() { cancel(errStalled) })
	defer quiet.Stop()
	stallOr := func(err error) error {
		if errors.Is(context.Cause(ctx), errStalled) {
			return errStalled
		}
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	client := *u.http
	client.Timeout = 0
	res, err := client.Do(req)
	if err != nil {
		return stallOr(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	u.set(func(s *Status) { s.Total = res.ContentLength })

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			quiet.Reset(stallAfter)
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			u.set(func(s *Status) { s.Bytes = done })
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return stallOr(rerr)
		}
	}
}

var stallAfter = time.Minute

var errStalled = errors.New("the download stopped: no data for a minute")

func verifyArchive(raw, sig []byte, archive string) error {
	if !ed25519.Verify(signingKey, raw, sig) {
		return errors.New(sumsFile + " is not signed by Yoru's release key; refusing the update")
	}

	want := ""
	name := filepath.Base(archive)
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return fmt.Errorf("%s has no entry for %s", sumsFile, name)
	}

	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

func extractExe(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	want := config.ExeName(config.Name)
	for _, f := range r.File {
		if filepath.Base(f.Name) != want || f.FileInfo().IsDir() {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		defer src.Close()

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, src)
		return err
	}
	return fmt.Errorf("%s not in the archive", want)
}

func swap(exe, fresh string) error {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, exe); err != nil {
		os.Rename(old, exe)
		return err
	}
	return nil
}

func Cleanup(exe string) {
	os.Remove(exe + ".old")
	os.Remove(exe + ".new")
	os.Remove(exe + ".failed")
}

func RelaunchSelf(exe string) error { return relaunch(exe) }

func relaunch(exe string) error { return relaunchCmd(exe).Start() }

func relaunchCmd(exe string) *exec.Cmd {
	cmd := exec.Command(exe, relaunchArgs(os.Getpid(), os.Args[1:])...)
	cmd.Dir = filepath.Dir(exe)

	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	config.Detach(cmd)
	return cmd
}

func relaunchArgs(pid int, current []string) []string {
	return append([]string{"--wait-for", strconv.Itoa(pid)}, carried(current)...)
}

func carried(current []string) []string {
	var args []string
	skip := false
	for _, a := range current {
		switch {
		case skip:
			skip = false
		case a == "--wait-for":
			skip = true
		default:
			args = append(args, a)
		}
	}
	return args
}

func WaitFor(args []string, timeout time.Duration) {
	for i, a := range args {
		if a != "--wait-for" || i+1 >= len(args) {
			continue
		}
		pid, err := strconv.Atoi(args[i+1])
		if err != nil || pid <= 0 {
			return
		}
		deadline := time.Now().Add(timeout)
		for alive(pid) && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
		return
	}
}

func Newer(a, b string) bool {
	if b == "dev" || a == "" {
		return false
	}
	as, bs := parts(a), parts(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out []int
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' }) {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

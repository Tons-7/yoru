// Package update replaces yoru.exe with the newest GitHub release. The zip is
// the only thing touched: bin/, cache/, config.toml and the database stay.
package update

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/config"
	"yoru/internal/store"
)

// Version is stamped by the build (-X yoru/internal/update.Version=...); a "dev" build never updates itself.
var Version = "dev"

const (
	// Renamed from Tons-7/kuro, whose address GitHub forwards here for older versions' update checks.
	Repo      = "Tons-7/yoru"
	userAgent = "Yoru"
	sumsFile  = "SHA256SUMS.txt"
	sigFile   = sumsFile + ".sig"
)

// signingKeyHex can be replaced at build time (-X) only by an end-to-end test signing its own fake release.
var signingKeyHex = "22e0c47d84c95b91d24c386c08e994718a23cec6332f187c92bb3ed5ad4c279b"

// signingKey verifies SHA256SUMS.txt.sig, which a stolen token or hijacked CI cannot forge.
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

// Notes is one release's text, so someone several versions behind reads every one, not just the newest.
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
	// Pending: every release newer than this one, newest first.
	Pending []Notes `json:"pending,omitempty"`
	// WhatsNew: this version's notes, with those of the versions skipped since the one it replaced here.
	WhatsNew []Notes `json:"whatsNew,omitempty"`
	// UpdatedFrom: the version that ran here before this one, when known.
	UpdatedFrom string `json:"updatedFrom,omitempty"`
}

type Updater struct {
	store   *store.Store
	exe     string
	staging string
	api     string
	// web is github.com itself, asked when the API's hourly allowance is spent (latestFromSite).
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
	// An end-to-end test points a real build at a fake release server.
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

// WithRestart sets what ends this process once the new binary is in place.
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

// Check asks GitHub for the latest release and raises a notification the first
// time a newer one is seen. A dev build only records what is out.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rel, err := u.latest(ctx)
	var all []Notes
	if _, limited := errors.AsType[*rateLimited](err); limited {
		// The API allows 60 asks an hour per address, which a VPN's shares; the site's own pages aren't counted.
		if site, serr := u.latestFromSite(ctx); serr == nil {
			u.log.Info("GitHub's API is limiting this network; read the latest release from its site", "version", site.Version)
			rel, err, all = site, nil, u.knownNotes(site.Version)
		} else {
			u.log.Warn("latest release from GitHub's site", "err", serr)
		}
	} else if err == nil {
		// Best effort: without the list, the latest release's own notes still show.
		if all, err = u.releases(ctx); err != nil {
			u.log.Warn("release notes", "err", err)
			all, err = []Notes{{Version: rel.Version, Notes: rel.Notes}}, nil
		}
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

// rateLimited is GitHub's API refusing for the hour; reset is when it lets this network ask again.
type rateLimited struct{ reset time.Time }

func (e *rateLimited) Error() string {
	if e.reset.IsZero() {
		return "GitHub is limiting update checks from this network for now; Yoru tries again later"
	}
	return fmt.Sprintf("GitHub is limiting update checks from this network until %s; Yoru tries again later",
		e.reset.Local().Format("15:04"))
}

// limitOf reads a 403 or 429 that is the API's allowance spent, not a refusal of the repository.
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

// latestFromSite reads the newest tag from github.com's redirect, its assets at fixed addresses; no notes.
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
	// A release without this platform's zip is no update here, as with the API's asset list.
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

// knownNotes are notes fetched before, plus a newer latest without any; none blank for the version running.
func (u *Updater) knownNotes(latest string) []Notes {
	st := u.Status()
	all := append(slices.Clone(st.Pending), st.WhatsNew...)
	if Newer(latest, Version) && !slices.ContainsFunc(all, func(n Notes) bool { return n.Version == latest }) {
		all = append(all, Notes{Version: latest})
	}
	return newestFirst(all)
}

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

// releases lists the published releases' notes, newest first; drafts and pre-releases are nobody's update.
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

// pending is every release newer than current.
func pending(all []Notes, current string) []Notes {
	var out []Notes
	for _, n := range all {
		if Newer(n.Version, current) {
			out = append(out, n)
		}
	}
	return out
}

// whatsNew is current's notes and every release after from up to it; from unknown, current's alone.
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

// Started records the running version; the first start after an update announces it and keeps the old one.
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
	// Versions before this one recorded nothing; the announcement of this very version says it came by update.
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

// fetchSmall reads a release's small text asset.
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

// AssetName is the zip package.ps1 publishes for this platform. Releases also carry kuro-named ones, holding the
// program as kuro.exe, for versions from before the rename, which ask for those.
func AssetName(version string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("%s-%s-update.zip", config.Name, version)
	}
	return fmt.Sprintf("%s-%s-%s-%s.zip", config.Name, version, runtime.GOOS, runtime.GOARCH)
}

// notify announces a version once; the setting remembers which.
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

// Apply fetches the latest release in the background, swaps the executable and
// starts the new one; the caller's restart hook then ends this process.
func (u *Updater) Apply() error {
	// Judged and claimed under one lock: two presses must not start two downloads into one file.
	u.mu.Lock()
	st := u.status
	if st.Stage == StageDownloading || st.Stage == StageVerifying || st.Stage == StageRestarting {
		u.mu.Unlock()
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

		if err := u.apply(ctx, rel); err != nil {
			u.log.Error("update", "version", rel.Version, "err", err)
			u.set(func(s *Status) { s.Stage, s.Error = StageFailed, err.Error() })
			return
		}
		u.set(func(s *Status) { s.Stage = StageRestarting })
		u.log.Info("restarting into the new version", "version", rel.Version)
		if u.restart != nil {
			u.restart()
		}
	}()
	return nil
}

func (u *Updater) apply(ctx context.Context, rel Release) error {
	if rel.Sums == "" || rel.Sig == "" {
		return errors.New("release carries no signed " + sumsFile + "; refusing an unverified binary")
	}
	if err := os.MkdirAll(u.staging, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(u.staging, filepath.Base(rel.URL))
	defer os.Remove(archive)

	if err := u.download(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	u.set(func(s *Status) { s.Stage = StageVerifying })
	if err := u.verify(ctx, rel, archive); err != nil {
		return err
	}

	fresh := u.exe + ".new"
	if err := extractExe(archive, fresh); err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	if err := swap(u.exe, fresh); err != nil {
		os.Remove(fresh)
		return fmt.Errorf("replace executable: %w", err)
	}
	if err := u.launch(u.exe); err != nil {
		// Put the running version back, or nothing is left to start next time.
		if _, rerr := Rollback(u.exe); rerr != nil {
			u.log.Error("restore the previous version", "err", rerr)
		}
		return fmt.Errorf("start the new version: %w", err)
	}
	return nil
}

// Rollback puts the previous binary back in place of a new one that failed, returning the one to start. The
// first Yoru renamed itself on start, so the version before it may wait as kuro.exe.old.
func Rollback(exe string) (string, error) {
	old, back := exe+".old", exe
	if _, err := os.Stat(old); err != nil {
		legacy := filepath.Join(filepath.Dir(exe), config.ExeName(config.Legacy))
		if _, lerr := os.Stat(legacy + ".old"); lerr != nil {
			return "", err
		}
		old, back = legacy+".old", legacy
	}
	failed := exe + ".failed"
	os.Remove(failed)
	if err := os.Rename(exe, failed); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(old, back); err != nil {
		os.Rename(failed, exe)
		return "", err
	}
	return back, nil
}

// TakeName renames a kuro.exe running the renamed code to yoru.exe, before anything holds its path; it returns
// the path to use from then on. A yoru.exe already there is left alone, and so is kuro.exe.
func TakeName(exe string) string {
	if !strings.EqualFold(filepath.Base(exe), config.ExeName(config.Legacy)) {
		return exe
	}
	named := filepath.Join(filepath.Dir(exe), config.ExeName(config.Name))
	if _, err := os.Lstat(named); err == nil {
		return exe
	}
	// A running program can be renamed, though not deleted, on Windows too: the update swap relies on it.
	if err := os.Rename(exe, named); err != nil {
		return exe
	}
	return named
}

// Handover reports a start by an update, the only time a failed start falls back to the old binary.
func Handover(args []string) bool {
	return slices.Contains(args, "--wait-for")
}

// Relaunch starts exe as a plain launch, for handing back to a restored version.
func Relaunch(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	config.Detach(cmd)
	return cmd.Start()
}

func (u *Updater) download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := u.http.Do(req)
	if err != nil {
		return err
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
			return rerr
		}
	}
}

// verify checks the zip against the published SHA256SUMS line for it, and that file against its signature.
func (u *Updater) verify(ctx context.Context, rel Release, archive string) error {
	raw, err := u.fetchSmall(ctx, rel.Sums)
	if err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	sig, err := u.fetchSmall(ctx, rel.Sig)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
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

// extractExe writes the one executable in the zip to dest.
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

// swap moves the running binary aside and the new one in; a failure puts the old one back.
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

// Cleanup removes the last update's leftovers once this version serves; until then .old is the way back. The
// kuro-named ones go too, once this runs under the new name.
func Cleanup(exe string) {
	stems := []string{exe}
	if legacy := filepath.Join(filepath.Dir(exe), config.ExeName(config.Legacy)); !strings.EqualFold(legacy, exe) {
		stems = append(stems, legacy)
	}
	for _, stem := range stems {
		os.Remove(stem + ".old")
		os.Remove(stem + ".new")
		os.Remove(stem + ".failed")
	}
}

// RelaunchSelf starts this binary again, told to wait for this process, for a change that needs a restart.
func RelaunchSelf(exe string) error { return relaunch(exe) }

// relaunch starts the new binary outside the job object, waiting for this exit; a windowed Yoru reopens its UI.
func relaunch(exe string) error { return relaunchCmd(exe).Start() }

func relaunchCmd(exe string) *exec.Cmd {
	cmd := exec.Command(exe, relaunchArgs(os.Getpid(), os.Args[1:])...)
	cmd.Dir = filepath.Dir(exe)
	// Inherited, not NUL, so a handover failing at startup says why.
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	config.Detach(cmd)
	return cmd
}

// relaunchArgs: a fresh --wait-for, the old pair dropped, the rest carried over.
func relaunchArgs(pid int, current []string) []string {
	args := []string{"--wait-for", strconv.Itoa(pid)}
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

// WaitFor reads --wait-for <pid> from the arguments and blocks until that process is gone, so a relaunch
// doesn't race its predecessor for the port, nor move the data folder while it still has the database open.
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

// Newer reports whether a is a later version than b: dotted numbers compared
// left to right, so 2026.08.27 < 2026.08.27.1 and a "dev" build is never behind.
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

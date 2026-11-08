package deps

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bodgit/sevenzip"

	"yoru/internal/proc"
)

type Release struct {
	Version string
	URL     string
	Archive bool
	Digest  string
}

type component struct {
	name    string
	files   []string
	from    []string
	glob    string
	into    string
	resolve func(context.Context, *Manager) (Release, error)
}

var components = []component{
	{name: "ffmpeg", files: componentFiles("ffmpeg"), resolve: resolveFfmpeg},
	{name: "mpv", files: componentFiles("mpv"), resolve: resolveMpv},
	{name: "anime4k", glob: "*.glsl", into: "shaders", resolve: resolveAnime4K},
	{name: "flyhigh", files: FlyHighFiles, from: []string{"pkg", "models"}, into: FlyHighDir, resolve: resolveFlyHigh},
}

type Stage string

const (
	StageResolving   Stage = "resolving"
	StageDownloading Stage = "downloading"
	StageExtracting  Stage = "extracting"
	StageDone        Stage = "done"
	StageFailed      Stage = "failed"
)

type Progress struct {
	Component string `json:"component"`
	Stage     Stage  `json:"stage"`
	Version   string `json:"version,omitempty"`
	Bytes     int64  `json:"bytes"`
	Total     int64  `json:"total"`
	Error     string `json:"error,omitempty"`
}

type Manager struct {
	binDir string
	http   *http.Client
	log    *slog.Logger

	mu        sync.Mutex
	recordMu  sync.Mutex
	running   map[string]*Progress
	installed func(name string)
	latest    map[string]latestVersion
}

type latestVersion struct {
	version string
	at      time.Time
}

func (m *Manager) OnInstalled(hook func(name string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installed = hook
}

func (m *Manager) Latest(ctx context.Context, name string) (string, error) {
	m.mu.Lock()
	if v, ok := m.latest[name]; ok && time.Since(v.at) < time.Hour {
		m.mu.Unlock()
		return v.version, nil
	}
	m.mu.Unlock()

	var spec *component
	for i := range components {
		if components[i].name == name {
			spec = &components[i]
		}
	}
	if spec == nil {
		return "", fmt.Errorf("unknown component %q", name)
	}
	rel, err := spec.resolve(ctx, m)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	if m.latest == nil {
		m.latest = map[string]latestVersion{}
	}
	m.latest[name] = latestVersion{version: rel.Version, at: time.Now()}
	m.mu.Unlock()
	return rel.Version, nil
}

func (m *Manager) LatestKnown(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latest[name].version
}

func New(binDir string, log *slog.Logger) *Manager {
	return &Manager{
		binDir:  binDir,
		http:    &http.Client{Timeout: 30 * time.Minute},
		log:     log,
		running: map[string]*Progress{},
	}
}

func Known(name string) bool {
	for _, c := range components {
		if c.name == name {
			return true
		}
	}
	return false
}

func (m *Manager) Install(name string) error {
	var spec *component
	for i := range components {
		if components[i].name == name {
			spec = &components[i]
		}
	}
	if spec == nil {
		return fmt.Errorf("unknown component %q", name)
	}

	m.mu.Lock()
	if p, busy := m.running[name]; busy && p.Stage != StageDone && p.Stage != StageFailed {
		m.mu.Unlock()
		return nil
	}
	m.running[name] = &Progress{Component: name, Stage: StageResolving}
	m.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()

		if err := m.install(ctx, spec); err != nil {
			m.log.Error("install dependency", "component", name, "err", err)
			m.set(name, func(p *Progress) {
				p.Stage = StageFailed
				p.Error = err.Error()
			})
			return
		}
		m.set(name, func(p *Progress) { p.Stage = StageDone })
		m.log.Info("dependency installed", "component", name)

		m.mu.Lock()
		hook := m.installed
		m.mu.Unlock()
		if hook != nil {
			hook(name)
		}
	}()
	return nil
}

func (m *Manager) Status() []Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Progress, 0, len(m.running))
	for _, p := range m.running {
		out = append(out, *p)
	}
	return out
}

func (m *Manager) set(name string, apply func(*Progress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.running[name]; ok {
		apply(p)
	}
}

func (m *Manager) install(ctx context.Context, spec *component) error {
	release, err := spec.resolve(ctx, m)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	m.set(spec.name, func(p *Progress) {
		p.Stage = StageDownloading
		p.Version = release.Version
	})

	if err := os.MkdirAll(m.binDir, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(m.binDir, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	name := filepath.Base(release.URL)
	if !release.Archive {
		name = spec.files[0]
	}
	archive := filepath.Join(staging, name)
	if err := m.download(ctx, spec.name, release.URL, archive, release.Digest); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	m.set(spec.name, func(p *Progress) { p.Stage = StageExtracting })

	if !release.Archive {
		if err := install(archive, filepath.Join(m.binDir, spec.files[0])); err != nil {
			return err
		}
		return m.record(spec.name, release.Version)
	}

	unpacked := filepath.Join(staging, "unpacked")
	if err := extract(ctx, archive, unpacked); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	into := filepath.Join(m.binDir, spec.into)
	if err := os.MkdirAll(into, 0o755); err != nil {
		return err
	}
	if spec.glob != "" {
		found, err := findAll(unpacked, spec.glob)
		if err != nil || len(found) == 0 {
			return fmt.Errorf("no %s in the archive", spec.glob)
		}
		for _, src := range found {
			if err := install(src, filepath.Join(into, filepath.Base(src))); err != nil {
				return err
			}
		}
		return m.record(spec.name, release.Version)
	}

	for _, want := range spec.files {
		src, err := findOne(unpacked, want, spec.from...)
		if err != nil {
			return err
		}
		if err := install(src, filepath.Join(into, want)); err != nil {
			return err
		}
	}
	return m.record(spec.name, release.Version)
}

func (m *Manager) download(ctx context.Context, name, url, dest, digest string) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	quiet := time.AfterFunc(stallAfter, func() { cancel(errStalled) })
	defer quiet.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	client := *m.http
	client.Timeout = 0
	res, err := client.Do(req)
	if err != nil {
		return stallOr(ctx, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}

	m.set(name, func(p *Progress) { p.Total = res.ContentLength })

	f, err := os.Create(dest)
	if err != nil {
		return err
	}

	started := time.Now()
	var sum hash.Hash = sha256.New()
	if h, ok := strings.CutPrefix(digest, "md5:"); ok {
		sum, digest = md5.New(), h
	}
	n, err := io.Copy(io.MultiWriter(f, sum), &counter{
		reader: res.Body,
		report: func(n int64) { m.set(name, func(p *Progress) { p.Bytes = n }) },
		alive:  func() { quiet.Reset(stallAfter) },
	})
	f.Close()
	if err != nil {
		return stallOr(ctx, err)
	}

	if digest == "" {
		m.log.Warn("no checksum published for this download; installing unverified", "component", name)
	} else if !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), digest) {
		os.Remove(dest)
		return fmt.Errorf("%s: checksum mismatch, refusing to install", name)
	}

	took := time.Since(started)
	m.log.Info("downloaded", "component", name,
		"mb", n>>20, "seconds", took.Round(time.Second).Seconds(),
		"mbps", fmt.Sprintf("%.1f", float64(n)/took.Seconds()/(1<<20)))
	return nil
}

var stallAfter = time.Minute

var errStalled = errors.New("the download stopped: no data for a minute")

func stallOr(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), errStalled) {
		return errStalled
	}
	return err
}

type counter struct {
	reader   io.Reader
	report   func(int64)
	alive    func()
	total    int64
	reported int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if n > 0 && c.alive != nil {
		c.alive()
	}
	c.total += int64(n)
	if c.total-c.reported > 4<<20 {
		c.reported = c.total
		c.report(c.total)
	}
	if err == io.EOF {
		c.report(c.total)
	}
	return n, err
}

func extract(ctx context.Context, archive, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	switch strings.ToLower(filepath.Ext(archive)) {
	case ".zip":
		return unzip(archive, dest)
	case ".7z":
		return un7z(archive, dest)
	}

	name := filepath.Base(archive)
	local := filepath.Join(dest, name)
	if err := os.Rename(archive, local); err != nil {
		return err
	}
	defer os.Remove(local)

	cmd := proc.Hide(exec.CommandContext(ctx, systemTar(), "-xf", name))
	cmd.Dir = dest
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func systemTar() string {
	if runtime.GOOS != "windows" {
		return "tar"
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		if path := filepath.Join(root, "System32", "tar.exe"); exists(path) {
			return path
		}
	}
	return "tar.exe"
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func unzip(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(dest, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry escapes the target directory: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func un7z(archive, dest string) error {
	r, err := sevenzip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(dest, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry escapes the target directory: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

type entry interface {
	Open() (io.ReadCloser, error)
}

func writeEntry(f entry, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func install(src, dest string) error {
	fresh := dest + ".new"
	if err := copyTo(src, fresh); err != nil {
		os.Remove(fresh)
		return err
	}
	return swapIn(fresh, dest)
}

func copyTo(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(dest), err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func swapIn(fresh, dest string) error {
	old := dest + ".old"
	os.Remove(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			os.Remove(fresh)
			return fmt.Errorf("replace %s: %w", filepath.Base(dest), err)
		}
	}
	if err := os.Rename(fresh, dest); err != nil {
		os.Rename(old, dest)
		return fmt.Errorf("replace %s: %w", filepath.Base(dest), err)
	}
	os.Remove(old)
	return nil
}

func findOne(root, name string, from ...string) (string, error) {
	found, err := findAll(root, name)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", fmt.Errorf("%s not found in the archive", name)
	}
	if len(from) == 0 {
		return found[0], nil
	}
	for _, path := range found {
		if slices.Contains(from, filepath.Base(filepath.Dir(path))) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found under %s in the archive", name, strings.Join(from, " or "))
}

func findAll(root, pattern string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ok, _ := filepath.Match(pattern, d.Name()); ok {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

var obsolete = []string{"rqbit"}

func (m *Manager) RemoveObsolete() []string {
	staging, _ := filepath.Glob(filepath.Join(m.binDir, ".install-*"))
	for _, p := range staging {
		os.RemoveAll(p)
	}
	for _, c := range components {
		var leftovers []string
		for _, f := range c.files {
			leftovers = append(leftovers, filepath.Join(m.binDir, c.into, f+".new"), filepath.Join(m.binDir, c.into, f+".old"))
		}
		if c.into != "" && c.glob != "" {
			for _, ext := range []string{".new", ".old"} {
				found, _ := filepath.Glob(filepath.Join(m.binDir, c.into, c.glob+ext))
				leftovers = append(leftovers, found...)
			}
		}
		for _, p := range leftovers {
			os.Remove(p)
		}
	}
	path := filepath.Join(m.binDir, "versions.json")
	versions := map[string]string{}
	raw, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(withoutBOM(raw), &versions)
	}
	var removed []string
	listed := false
	for _, name := range obsolete {
		if _, ok := versions[name]; !ok {
			continue
		}
		for _, file := range []string{name, name + ".exe"} {
			if err := os.Remove(filepath.Join(m.binDir, file)); err == nil {
				removed = append(removed, file)
			}
		}
		delete(versions, name)
		listed = true
	}
	if listed {
		if raw, err := json.MarshalIndent(versions, "", "    "); err == nil {
			writeWhole(path, raw)
		}
	}
	return removed
}

func withoutBOM(raw []byte) []byte { return bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) }

func (m *Manager) record(name, version string) error {
	m.recordMu.Lock()
	defer m.recordMu.Unlock()
	path := filepath.Join(m.binDir, "versions.json")

	versions := map[string]string{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(withoutBOM(raw), &versions)
	}
	versions[name] = version

	raw, err := json.MarshalIndent(versions, "", "    ")
	if err != nil {
		return err
	}
	return writeWhole(path, raw)
}

func writeWhole(path string, raw []byte) error {
	fresh := path + ".new"
	if err := os.WriteFile(fresh, raw, 0o644); err != nil {
		os.Remove(fresh)
		return err
	}
	if err := os.Rename(fresh, path); err != nil {
		os.Remove(fresh)
		return err
	}
	return nil
}

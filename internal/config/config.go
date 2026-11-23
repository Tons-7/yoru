package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const Name = "yoru"

func Env(key string) string { return os.Getenv("YORU_" + key) }

type Config struct {
	Addr     string    `toml:"addr"`
	CacheDir string    `toml:"cache_dir"`
	BinDir   string    `toml:"bin_dir"`
	AniList  AniList   `toml:"anilist"`
	MAL      MAL       `toml:"mal"`
	Torrent  Torrent   `toml:"torrent"`
	Indexers []Indexer `toml:"indexer"`
	Data     string    `toml:"data_dir"`
	VLC      string    `toml:"vlc_path"`
	TLSCert  string    `toml:"tls_cert"`
	TLSKey   string    `toml:"tls_key"`

	dataDir   string
	root      string
	temporary bool
	unknown   []string
	unwritten error
}

var writeTemplate = os.WriteFile

func (c Config) UnwrittenConfig() error { return c.unwritten }

func (c Config) ConfigPath() string { return filepath.Join(c.root, "config.toml") }

func (c Config) UnknownKeys() []string { return c.unknown }

func (c Config) StrayConfig() string {
	p := c.ConfigPath() + ".txt"
	if exists(p) {
		return p
	}
	return ""
}

func (c Config) Temporary() bool { return c.temporary }

type Indexer struct {
	Type  string `toml:"type"`
	URL   string `toml:"url"`
	Adult bool   `toml:"adult"`
}

const DefaultListenPort = 4240

type Torrent struct {
	UploadLimitBytes int   `toml:"upload_limit_bytes"`
	ListenPort       int   `toml:"listen_port"`
	PeerLimit        int   `toml:"peer_limit"`
	UPnP             *bool `toml:"upnp"`
}

func (t Torrent) UPnPEnabled() bool { return t.UPnP == nil || *t.UPnP }

func (t Torrent) PeerPort() int {
	if t.ListenPort > 0 {
		return t.ListenPort
	}
	return DefaultListenPort
}

type AniList struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
}

type MAL struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
}

func (c Config) LocalURL() string {
	_, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		port = "4321"
	}
	return "http://localhost:" + port
}

func appProfileDir() string { return filepath.Join(dataDir(), "window") }

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (c Config) ResolveDataDir(dir string) string { return c.resolve(dir, dataDir()) }

func (c Config) InDownloads(dir string) bool { return within(c.CacheDir, dir) }

func within(parent, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(dir))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (c Config) resolve(dir, fallback string) string {
	switch {
	case dir == "":
		return fallback
	case filepath.IsAbs(dir):
		return filepath.Clean(dir)
	}
	return filepath.Join(c.root, dir)
}

func (c Config) SetDataDir(dir string) error {
	raw, err := os.ReadFile(c.ConfigPath())
	if err != nil {
		return err
	}
	line := fmt.Sprintf("data_dir = %q", filepath.ToSlash(dir))
	lines := strings.Split(string(raw), "\n")
	placed := false
	for i, l := range lines {
		if key, _, ok := strings.Cut(l, "="); ok && strings.TrimSpace(key) == "data_dir" {
			lines[i], placed = line, true
			break
		}
	}
	if !placed {
		at := len(lines)
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "[") {
				at = i
				break
			}
		}
		lines = append(lines[:at], append([]string{line, ""}, lines[at:]...)...)
	}
	return replaceFile(c.ConfigPath(), []byte(strings.Join(lines, "\n")))
}

func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (c Config) VLCPath() string {
	if c.VLC == "" {
		return ""
	}
	return c.resolve(c.VLC, "")
}

func (c Config) TLSFiles() (cert, key string) {
	if c.TLSCert == "" || c.TLSKey == "" {
		return "", ""
	}
	return c.resolve(c.TLSCert, ""), c.resolve(c.TLSKey, "")
}

func (c Config) DataDir() string      { return c.dataDir }
func (c Config) DatabasePath() string { return filepath.Join(c.dataDir, Name+".db") }
func (c Config) LockPath() string     { return filepath.Join(c.dataDir, Name+".lock") }
func (c Config) ProfileDir() string   { return filepath.Join(c.dataDir, "window") }
func (c Config) Path(name string) string {
	return filepath.Join(c.BinDir, name)
}

func (c Config) WithDataDir(dir string) Config {
	c.dataDir = dir
	return c
}

func ExeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func (c Config) Tool(base string) string {
	if base == "ffprobe" {
		if sibling := filepath.Join(filepath.Dir(c.Tool("ffmpeg")), ExeName("ffprobe")); exists(sibling) {
			return sibling
		}
	}
	p := c.Path(ExeName(base))
	if exists(p) {
		return p
	}
	if onPath, err := exec.LookPath(ExeName(base)); err == nil {
		return onPath
	}
	return p
}

func (c Config) RedirectURI() string { return c.redirect("/callback") }

func (c Config) MALRedirectURI() string { return c.redirect("/mal/callback") }

func (c Config) redirect(path string) string {
	_, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		port = "4321"
	}
	return "http://localhost:" + port + path
}

func Load() (Config, error) {
	root, err := rootDir()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{Addr: "127.0.0.1:4321", dataDir: dataDir()}

	path := filepath.Join(root, "config.toml")
	meta, err := toml.DecodeFile(path, &cfg)
	folders := root
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := writeTemplate(path, []byte(template), 0o600); err != nil {
			cfg.unwritten, folders = err, cfg.dataDir
		}
	case err != nil:
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	for _, key := range meta.Undecoded() {
		cfg.unknown = append(cfg.unknown, key.String())
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = "127.0.0.1:4321"
	}

	cfg.root, cfg.temporary = root, underTemp(root) || translocated()
	if cfg.Data != "" {
		cfg.dataDir = cfg.ResolveDataDir(cfg.Data)
	}
	cfg.CacheDir = cfg.resolve(cfg.CacheDir, filepath.Join(folders, "cache"))
	cfg.BinDir = cfg.resolve(cfg.BinDir, filepath.Join(folders, "bin"))
	real := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return path
	}
	for _, dir := range []string{root, cfg.dataDir, cfg.BinDir} {
		if within(cfg.CacheDir, dir) || within(real(cfg.CacheDir), real(dir)) {
			return Config{}, fmt.Errorf("cache_dir %s holds %s; downloads need a folder of their own", cfg.CacheDir, dir)
		}
	}
	for _, dir := range []string{cfg.dataDir, cfg.CacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func rootDir() (string, error) {
	if root := Env("ROOT"); root != "" {
		return filepath.Abs(root)
	}

	exe, err := os.Executable()
	if err != nil {
		return os.Getwd()
	}
	dir := filepath.Dir(exe)

	home, _ := os.UserHomeDir()
	if support, ok := bundleRoot(dir, home); ok {
		return support, os.MkdirAll(support, 0o755)
	}
	if underTemp(dir) {
		return os.Getwd()
	}
	return dir, nil
}

func bundleRoot(dir, home string) (string, bool) {
	if home == "" || !strings.HasSuffix(filepath.ToSlash(dir), ".app/Contents/MacOS") {
		return "", false
	}
	return filepath.Join(home, "Library", "Application Support", "Yoru"), true
}

func FinderPath() {
	if runtime.GOOS == "darwin" {
		os.Setenv("PATH", withPackageDirs(os.Getenv("PATH")))
	}
}

func withPackageDirs(path string) string {
	dirs := filepath.SplitList(path)
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"} {
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func underTemp(dir string) bool {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	return within(tmp, resolved)
}

func DefaultDataDir() string { return dataDir() }

func dataDir() string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, Name)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "."+Name)
}

const template = `# AniList connects in one step from the setup guide or Settings -> Accounts.
# Your own AniList app is optional: its redirect URL is http://localhost:4321/callback

# Use 0.0.0.0:4321 to reach Yoru from a phone or TV on the same network.
# Off the host machine a token is required; GET /api/access prints the URL and
# /api/access/qr.svg renders it as a QR code.
addr = "127.0.0.1:4321"

# Folders. Relative paths are beside yoru.exe; quote Windows paths with single
# quotes ('D:\Yoru\cache'). Restart after editing.
# data_dir: database and window profile (default %LOCALAPPDATA%\yoru)
# cache_dir: downloaded episodes, transcodes, thumbnails, updates
# bin_dir: ffmpeg, mpv, shaders
# data_dir = ""
# cache_dir = "cache"
# bin_dir = "bin"

# VLC, when it is not on PATH or in Program Files: its folder or its binary.
# vlc_path = 'E:\VideoLAN\VLC'

# HTTPS, which a phone needs to install Yoru as an app: a certificate and its key
# (for example from "tailscale cert"). http://localhost keeps working beside it.
# tls_cert = 'my-pc.tailnet.ts.net.crt'
# tls_key = 'my-pc.tailnet.ts.net.key'

[anilist]
client_id = ""
client_secret = ""

# Optional second tracker. Register at https://myanimelist.net/apiconfig
# with redirect URL exactly: http://localhost:4321/mal/callback
[mal]
client_id = ""
client_secret = ""

# Torrent search sites. Easier in Settings > Downloads > Release sites: no
# restart, and a list saved there replaces these blocks. Here, one block each:
# type "nyaa" or "tokyotosho"; adult = true is searched only for adult titles.
# The first listed wins ties. Restart after editing.
#
# [[indexer]]
# type = "nyaa"
# url = ""
`

func translocated() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(filepath.ToSlash(exe), "/AppTranslocation/")
}

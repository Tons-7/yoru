package deps

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const userAgent = "Yoru"

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

func sha256Of(digest string) string {
	if h, ok := strings.CutPrefix(digest, "sha256:"); ok {
		return h
	}
	return ""
}

type ghRelease struct {
	Tag    string    `json:"tag_name"`
	Assets []ghAsset `json:"assets"`
}

func (m *Manager) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func resolveFfmpeg(ctx context.Context, m *Manager) (Release, error) {
	switch runtime.GOOS {
	case "windows":
		return resolveFfmpegWindows(ctx, m)
	case "linux":
		url, err := ffmpegLinuxURL()
		if err != nil {
			return Release{}, err
		}
		sum := m.publishedSum(ctx, url+".md5", 32)
		if sum == "" {
			time.Sleep(2 * time.Second)
			sum = m.publishedSum(ctx, url+".md5", 32)
		}
		rel := Release{Version: "static", URL: url, Archive: true}
		if sum != "" {
			rel.Digest = "md5:" + sum
		}
		return rel, nil
	default:
		return Release{}, manualInstall("ffmpeg")
	}
}

func resolveFfmpegWindows(ctx context.Context, m *Manager) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.gyan.dev/ffmpeg/builds/release-version", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := m.http.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("ffmpeg version: HTTP %d", res.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, 128))
	if err != nil {
		return Release{}, err
	}
	const url = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-full.7z"
	sum := m.publishedSum(ctx, url+".sha256", 64)
	if sum == "" {
		time.Sleep(2 * time.Second)
		sum = m.publishedSum(ctx, url+".sha256", 64)
	}
	if sum == "" {
		m.log.Warn("ffmpeg checksum unavailable; installing unverified")
	}
	return Release{
		Version: strings.TrimSpace(string(raw)),
		URL:     url,
		Archive: true,
		Digest:  sum,
	}, nil
}

func (m *Manager) publishedSum(ctx context.Context, url string, size int) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := m.http.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return ""
	}
	sum, _, _ := strings.Cut(strings.TrimSpace(string(raw)), " ")
	if _, err := hex.DecodeString(sum); err != nil || len(sum) != size {
		return ""
	}
	return sum
}

var mpvAsset = regexp.MustCompile(`^mpv-x86_64-\d{8}-git-[0-9a-f]+\.7z$`)

func resolveMpv(ctx context.Context, m *Manager) (Release, error) {
	if runtime.GOOS != "windows" {
		return Release{}, manualInstall("mpv")
	}

	var release ghRelease
	if err := m.getJSON(ctx,
		"https://api.github.com/repos/shinchiro/mpv-winbuild-cmake/releases/latest", &release); err != nil {
		return Release{}, err
	}

	for _, a := range release.Assets {
		if mpvAsset.MatchString(a.Name) {
			version := strings.TrimSuffix(strings.TrimPrefix(a.Name, "mpv-x86_64-"), ".7z")
			return Release{
				Version: version, URL: a.URL, Archive: true, Digest: sha256Of(a.Digest),
			}, nil
		}
	}
	return Release{}, fmt.Errorf("no plain x86_64 mpv build in %s", release.Tag)
}

const (
	FlyHighVersion = "0.3.7"
	flyhighDigest  = "6365da772ac06b207846e179c3ee781ee4d2ec76a4f95b084febf3ac43149b2d"
	FlyHighDir     = "flyhigh"
)

var FlyHighFiles = []string{"flyhigh_web.js", "flyhigh_web_bg.wasm", "flyhighfg.bin", "flyhighfg.json", "graph.json"}

func resolveFlyHigh(context.Context, *Manager) (Release, error) {
	v := FlyHighVersion
	return Release{
		Version: v,
		URL:     fmt.Sprintf("https://github.com/Tons-7/flyhigh-engine/releases/download/v%s/flyhigh-v%s-web.tar.gz", v, v),
		Archive: true,
		Digest:  flyhighDigest,
	}, nil
}

const CloudflaredVersion = "2026.10.7"

type pinnedAsset struct{ name, digest string }

var cloudflaredAssets = map[string]pinnedAsset{
	"windows/amd64": {"cloudflared-windows-amd64.exe", "2493dbc168923bb3e59e5e7716f71deced92992eea9acd6e9d74f84ac7751343"},
	"windows/arm64": {"cloudflared-windows-amd64.exe", "2493dbc168923bb3e59e5e7716f71deced92992eea9acd6e9d74f84ac7751343"},
	"windows/386":   {"cloudflared-windows-386.exe", "88c6b972b410db0e26745f593510d772c825a13c5a2374819e3f13a05b333082"},
	"darwin/arm64":  {"cloudflared-darwin-arm64.tgz", "3586ccf397e98e5f437e60e7c307ca6ac463e110580503af8bb8e6f9ebf831bd"},
	"darwin/amd64":  {"cloudflared-darwin-amd64.tgz", "02f3edfd87684dd5b705607688e4eb4b323f6e777b81e7b3a85af556aa356848"},
	"linux/amd64":   {"cloudflared-linux-amd64", "1f99fb518b83dec2ba8d870a7cc991d82a77e6196295a3d2754ba17ab9f91872"},
	"linux/arm64":   {"cloudflared-linux-arm64", "3df43aedc9ff291d1735b5cb576cb97e0b157488796db2e32b28aecabb445775"},
	"linux/arm":     {"cloudflared-linux-arm", "74f690f6707182839b63e0465726c7776dd63f0a174390b911d5d71f35cab94e"},
	"linux/386":     {"cloudflared-linux-386", "4823d2f038395617e04e4070601ab45c0c43f31f15ee7586be299e19ef49d46e"},
}

func resolveCloudflared(context.Context, *Manager) (Release, error) {
	a, ok := cloudflaredAssets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return Release{}, fmt.Errorf("no cloudflared build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return Release{
		Version: CloudflaredVersion,
		URL:     "https://github.com/cloudflare/cloudflared/releases/download/" + CloudflaredVersion + "/" + a.name,
		Archive: strings.HasSuffix(a.name, ".tgz"),
		Digest:  a.digest,
	}, nil
}

var anime4kSums = map[string]string{"v4.0.1": "139cd282086457c5adc79caf7b75b8b825091d71c9b54958c18745fea62d7ed7"}

func resolveAnime4K(ctx context.Context, m *Manager) (Release, error) {
	var release ghRelease
	if err := m.getJSON(ctx,
		"https://api.github.com/repos/bloc97/Anime4K/releases/latest", &release); err != nil {
		return Release{}, err
	}

	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, ".zip") {
			digest := sha256Of(a.Digest)
			if digest == "" {
				digest = anime4kSums[release.Tag]
			}
			return Release{
				Version: strings.TrimPrefix(release.Tag, "v"),
				URL:     a.URL,
				Archive: true,
				Digest:  digest,
			}, nil
		}
	}
	return Release{}, fmt.Errorf("no zip in Anime4K release %s", release.Tag)
}

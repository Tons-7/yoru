package torrent

import (
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

var unsafeChars = strings.NewReplacer(
	":", "：", "?", "？", "*", "＊", `"`, "＂", "<", "＜", ">", "＞", "|", "｜", `\`, "＼", "/", "／",
)

var reservedDeviceNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM0": true, "LPT0": true, "COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

const (
	EngineSessionDir = "engine-session"
	LegacySessionDir = "rqbit-session"
	CacheLockName    = "yoru-cache.lock"
)

var ownFolders = map[string]bool{"hls": true, "thumbs": true, "update": true, EngineSessionDir: true, LegacySessionDir: true, CacheLockName: true}

func OwnFolder(name string) bool {
	name = strings.ToLower(name)
	return ownFolders[name] || chapterFile.MatchString(name)
}

var chapterFile = regexp.MustCompile(`^chapters-\d+-\d+\.ini$`)

func SafeName(name string) string {
	if runtime.GOOS != "windows" || name == "." || name == ".." {
		return name
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = unsafeChars.Replace(name)
	trimmed := strings.TrimRight(name, ". ")
	if trimmed != name {
		name = trimmed + "_"
	}
	if stem, _, _ := strings.Cut(name, "."); reservedDeviceNames[strings.ToUpper(strings.TrimSpace(stem))] {
		name = "_" + name
	}
	if name == "" {
		name = "_"
	}
	return name
}

var videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".webm": true, ".ts": true, ".m2ts": true, ".mov": true, ".wmv": true, ".flv": true, ".ogm": true}

func tag(infoHash string) string {
	return " [" + strings.ToLower(infoHash)[:min(8, len(infoHash))] + "]"
}

func Tagged(name, infoHash string) string {
	if ext := filepath.Ext(name); videoExts[strings.ToLower(ext)] {
		return strings.TrimSuffix(name, ext) + tag(infoHash) + ext
	}
	return name + tag(infoHash)
}

func DiskNames(name, infoHash string) []string {
	safe := SafeName(name)
	var out []string
	for _, n := range []string{Tagged(safe, infoHash), name + tag(infoHash), safe, name} {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func StreamHost(name string) (string, bool) {
	host, _, found := strings.Cut(name, ":")
	return host, found && runtime.GOOS == "windows" && host != ""
}

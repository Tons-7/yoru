package torrent

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Windows turns these into errors, or ':' into a hidden stream inside another file.
var unsafeChars = strings.NewReplacer(
	":", "：", "?", "？", "*", "＊", `"`, "＂", "<", "＜", ">", "＞", "|", "｜", `\`, "＼", "/", "／",
)

var reservedDeviceNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// The engine's session folder in the cache, and an old engine's (rqbit), never mistaken for a download.
const (
	EngineSessionDir = "engine-session"
	LegacySessionDir = "rqbit-session"
)

var ownFolders = map[string]bool{"hls": true, "thumbs": true, "update": true, EngineSessionDir: true, LegacySessionDir: true}

// OwnFolder reports a name Yoru keeps for itself in the cache (transcodes, scrub sheets, a staged update,
// sessions, chapter files): a download named like one must not take it, nor a cleanup sweep it.
func OwnFolder(name string) bool {
	name = strings.ToLower(name)
	return ownFolders[name] || strings.HasPrefix(name, "chapters-")
}

// SafeName is a file or folder name as the engine creates it: as given, except what Windows can't take.
func SafeName(name string) string {
	// "." and ".." stay as they are, for the cache-escape check to refuse.
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
	// Trailing dots and spaces are dropped by Windows, so two names would meet in one file.
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

// A pack folder's name can end in ".WEB" or ".x265"; only these are a file's type.
var videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".webm": true, ".ts": true, ".m2ts": true, ".mov": true, ".wmv": true, ".flv": true, ".ogm": true}

func tag(infoHash string) string {
	return " [" + strings.ToLower(infoHash)[:min(8, len(infoHash))] + "]"
}

// Tagged is a name with the hash tag the engine adds when another torrent holds it, before a video
// file's extension so it still opens as video.
func Tagged(name, infoHash string) string {
	if ext := filepath.Ext(name); videoExts[strings.ToLower(ext)] {
		return strings.TrimSuffix(name, ext) + tag(infoHash) + ext
	}
	return name + tag(infoHash)
}

// DiskNames are a download's possible top-level names in the cache: tagged ones first (only this torrent has
// them), then its own made safe, then older versions' forms (the tag after the extension, the name as given).
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

// StreamHost: on Windows a name with ':' was written, before safe names, into a hidden stream of the file
// named by what precedes the colon. That file holds the download, though it lists as empty.
func StreamHost(name string) (string, bool) {
	host, _, found := strings.Cut(name, ":")
	return host, found && runtime.GOOS == "windows" && host != ""
}

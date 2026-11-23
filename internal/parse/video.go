package parse

import "slices"

import "strings"

var videoExt = []string{".mkv", ".mp4", ".avi", ".ts", ".m2ts", ".webm", ".mov", ".m4v", ".ogm", ".wmv"}

func IsVideo(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range videoExt {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func VideoExt(ext string) bool {
	return slices.Contains(videoExt, ext)
}

func BaseName(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	if i := strings.LastIndexByte(path, '.'); i > 0 {
		path = path[:i]
	}
	return path
}

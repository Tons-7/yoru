package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

func spa(assets fs.FS) http.Handler {
	files := http.FileServer(http.FS(assets))
	packed := &packedFiles{assets: assets}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		info, err := fs.Stat(assets, name)
		if err != nil || info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, assets)
			return
		}

		hashed := strings.HasPrefix(name, "assets/")
		if hashed {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if name == "sw.js" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if strings.HasSuffix(name, ".webmanifest") {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		if hashed && packed.serve(w, r, name) {
			return
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "app not built", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(page)))
}

func apiOnly() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send(w, http.StatusNotFound, map[string]any{
			"error": "no frontend in this build; run npm run build in web/",
			"path":  r.URL.Path,
		})
	})
}

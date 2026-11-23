package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

var gzipFrom = 1024

var gzipTypes = map[string]string{
	".js":          "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".wasm":        "application/wasm",
	".json":        "application/json",
	".svg":         "image/svg+xml",
	".webmanifest": "application/manifest+json",
}

var gzipWriters = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return zw
}}

func takesGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(name) != "gzip" {
			continue
		}
		q := strings.TrimPrefix(strings.ReplaceAll(params, " ", ""), "q=")
		return q != "0" && q != "0.0" && q != "0.00" && q != "0.000"
	}
	return false
}

type packedFiles struct {
	assets fs.FS
	mu     sync.Mutex
	files  map[string]func() []byte
}

func (p *packedFiles) get(name string) []byte {
	p.mu.Lock()
	if p.files == nil {
		p.files = map[string]func() []byte{}
	}
	f := p.files[name]
	if f == nil {
		f = sync.OnceValue(func() []byte {
			raw, err := fs.ReadFile(p.assets, name)
			if err != nil || len(raw) < gzipFrom {
				return nil
			}
			var out bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
			zw.Write(raw)
			zw.Close()
			if out.Len() < len(raw) {
				return out.Bytes()
			}
			return nil
		})
		p.files[name] = f
	}
	p.mu.Unlock()
	return f()
}

func (p *packedFiles) serve(w http.ResponseWriter, r *http.Request, name string) bool {
	kind := gzipTypes[path.Ext(name)]
	if kind == "" || r.Method != http.MethodGet || r.Header.Get("Range") != "" || !takesGzip(r) {
		return false
	}
	body := p.get(name)
	if body == nil {
		return false
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
	return true
}

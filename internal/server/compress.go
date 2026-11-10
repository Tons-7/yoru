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

func writeGzip(w io.Writer, raw []byte) {
	zw := gzipWriters.Get().(*gzip.Writer)
	zw.Reset(w)
	zw.Write(raw)
	zw.Close()
	gzipWriters.Put(zw)
}

type packedFile struct {
	once sync.Once
	body []byte
}

type packedFiles struct {
	assets fs.FS
	mu     sync.Mutex
	files  map[string]*packedFile
}

func (p *packedFiles) get(name string) []byte {
	p.mu.Lock()
	if p.files == nil {
		p.files = map[string]*packedFile{}
	}
	f := p.files[name]
	if f == nil {
		f = &packedFile{}
		p.files[name] = f
	}
	p.mu.Unlock()
	f.once.Do(func() {
		raw, err := fs.ReadFile(p.assets, name)
		if err != nil || len(raw) < gzipFrom {
			return
		}
		var out bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
		zw.Write(raw)
		zw.Close()
		if out.Len() < len(raw) {
			f.body = out.Bytes()
		}
	})
	return f.body
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

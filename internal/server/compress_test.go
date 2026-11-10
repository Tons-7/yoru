package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"yoru/internal/config"
)

func unpack(t *testing.T, body []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("broken gzip: %v", err)
	}
	return out
}

func TestAppFilesAreCompressedForBrowsersThatAsk(t *testing.T) {
	script := strings.Repeat("export const answer = 42;\n", 400)
	picture := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 4000)...)
	app := spa(fstest.MapFS{
		"index.html":       {Data: []byte("<html></html>")},
		"assets/app.js":    {Data: []byte(script)},
		"assets/tiny.js":   {Data: []byte("export {}")},
		"assets/cover.png": {Data: picture},
		"assets/sub.wasm":  {Data: bytes.Repeat([]byte("\x00asm"), 600)},
	})
	get := func(target string, headers ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	for range 2 {
		rec := get("/assets/app.js", "Accept-Encoding", "gzip, deflate, br")
		if rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
			t.Fatalf("a script went out as %q, %q", rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Type"))
		}
		if got := unpack(t, rec.Body.Bytes()); string(got) != script {
			t.Fatalf("the unpacked script differs: %d bytes, want %d", len(got), len(script))
		}
		if rec.Body.Len() >= len(script)/4 {
			t.Errorf("the script barely shrank: %d of %d bytes", rec.Body.Len(), len(script))
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("headers: cache %q, vary %q", rec.Header().Get("Cache-Control"), rec.Header().Get("Vary"))
		}
	}
	if rec := get("/assets/sub.wasm", "Accept-Encoding", "gzip"); rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Type") != "application/wasm" {
		t.Errorf("wasm went out as %q, %q", rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Type"))
	}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"a browser that didn't ask": get("/assets/app.js"),
		"a part of a file":          get("/assets/app.js", "Accept-Encoding", "gzip", "Range", "bytes=0-99"),
		"a picture":                 get("/assets/cover.png", "Accept-Encoding", "gzip"),
		"a tiny file":               get("/assets/tiny.js", "Accept-Encoding", "gzip"),
		"the page itself":           get("/", "Accept-Encoding", "gzip"),
		"a browser refusing gzip":   get("/assets/app.js", "Accept-Encoding", "br, gzip;q=0"),
	} {
		if enc := rec.Header().Get("Content-Encoding"); enc != "" {
			t.Errorf("%s was sent %s", name, enc)
		}
	}
	if rec := get("/assets/app.js"); rec.Body.String() != script {
		t.Error("the plain script differs")
	}
	if rec := get("/assets/app.js", "Accept-Encoding", "gzip", "Range", "bytes=0-99"); rec.Code != http.StatusPartialContent || rec.Body.Len() != 100 {
		t.Errorf("a part of a file answered %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestDataIsCompressedForBrowsersThatAsk(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	ask := func(method, target string, gz bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader("{}"))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Host = "127.0.0.1:4321"
		if gz {
			req.Header.Set("Accept-Encoding", "gzip")
		}
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		return rec
	}

	plain := ask("GET", "/api/prefs", false)
	if plain.Code != http.StatusOK || plain.Header().Get("Content-Encoding") != "" {
		t.Fatalf("plain: %d, %q", plain.Code, plain.Header().Get("Content-Encoding"))
	}
	if small := ask("GET", "/api/health", true); small.Header().Get("Content-Encoding") != "" {
		t.Errorf("a small answer was compressed: %d bytes", small.Body.Len())
	}

	was := gzipFrom
	gzipFrom = 16
	t.Cleanup(func() { gzipFrom = was })
	packed := ask("GET", "/api/prefs", true)
	if packed.Code != http.StatusOK || packed.Header().Get("Content-Encoding") != "gzip" || packed.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("asked for gzip: %d, %q, %q", packed.Code, packed.Header().Get("Content-Encoding"), packed.Header().Get("Content-Type"))
	}
	var a, b any
	if err := json.Unmarshal(unpack(t, packed.Body.Bytes()), &a); err != nil {
		t.Fatalf("the unpacked answer is not JSON: %v", err)
	}
	json.Unmarshal(plain.Body.Bytes(), &b)
	if x, _ := json.Marshal(a); !bytes.Equal(x, func() []byte { y, _ := json.Marshal(b); return y }()) {
		t.Error("the unpacked answer differs from the plain one")
	}

	h.call(t, "PUT", "/api/profile/avatar", `{"image":"`+tinyPNG+`"}`)
	if pic := ask("GET", "/api/profile/avatar", true); pic.Code != http.StatusOK || pic.Header().Get("Content-Encoding") != "" || pic.Header().Get("Content-Type") != "image/png" {
		t.Errorf("a picture: %d, %q, %q", pic.Code, pic.Header().Get("Content-Encoding"), pic.Header().Get("Content-Type"))
	}
	if saved := ask("POST", "/api/prefs", true); saved.Header().Get("Content-Encoding") != "" {
		t.Errorf("an answer to a change was compressed")
	}
}

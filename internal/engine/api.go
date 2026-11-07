package engine

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yoru/internal/torrent"
)

const streamReadahead = 16 << 20

func (e *Engine) guard(addr string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Sec-Fetch-Mode") != "" || r.Host != addr {
			http.Error(w, "not for browsers", http.StatusForbidden)
			return
		}
		if !strings.Contains(r.URL.Path, "/stream/") && r.Header.Get(torrent.SecretHeader) != e.secret {
			http.Error(w, "missing engine secret", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (e *Engine) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"engine": "Yoru"})
	})
	mux.HandleFunc("GET /torrents", e.started(e.handleList))
	mux.HandleFunc("POST /torrents", e.started(e.handleAdd))
	mux.HandleFunc("GET /torrents/{id}", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		reply(w, e.detail(en))
	})))
	mux.HandleFunc("GET /torrents/{id}/stats/v1", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		reply(w, e.stats(en))
	})))
	mux.HandleFunc("GET /torrents/{id}/stream/{file}", e.started(e.withEntry(e.handleStream)))
	mux.HandleFunc("GET /torrents/{id}/files/{file}/ranges", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		i, err := strconv.Atoi(r.PathValue("file"))
		if err != nil || en.t.Info() == nil || i < 0 || i >= len(en.t.Files()) {
			http.Error(w, "no such file", http.StatusNotFound)
			return
		}
		reply(w, fileRanges(en.t.Files()[i]))
	})))
	for verb, run := range map[string]func(*entry, int) error{
		"include": func(en *entry, i int) error { return e.setFileIncluded(en, i, true) },
		"exclude": func(en *entry, i int) error { return e.setFileIncluded(en, i, false) },
		"delete":  e.deleteFile,
	} {
		mux.HandleFunc("POST /torrents/{id}/files/{file}/"+verb, e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
			i, err := strconv.Atoi(r.PathValue("file"))
			if err == nil {
				err = run(en, i)
			}
			switch {
			case errors.Is(err, errNoFile):
				fail(w, http.StatusNotFound, err)
			case err != nil:
				fail(w, http.StatusInternalServerError, err)
			default:
				e.log.Info("torrent file "+verb+"d", "id", en.id, "file", i, "name", en.t.Name())
				reply(w, struct{}{})
			}
		})))
	}
	mux.HandleFunc("POST /torrents/{id}/pause", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		e.log.Info("torrent paused", "id", en.id, "name", en.t.Name())
		e.setPaused(en, true)
		reply(w, struct{}{})
	})))
	mux.HandleFunc("POST /torrents/{id}/start", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		e.setPaused(en, false)
		reply(w, struct{}{})
	})))
	mux.HandleFunc("POST /torrents/{id}/delete", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		if err := e.remove(en, true); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		reply(w, struct{}{})
	})))
	mux.HandleFunc("POST /torrents/{id}/forget", e.started(e.withEntry(func(w http.ResponseWriter, r *http.Request, en *entry) {
		e.remove(en, false)
		reply(w, struct{}{})
	})))
	return mux
}

func (e *Engine) started(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := e.Ensure(r.Context()); err != nil {
			fail(w, http.StatusServiceUnavailable, err)
			return
		}
		h(w, r)
	}
}

func (e *Engine) withEntry(h func(http.ResponseWriter, *http.Request, *entry)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		en, err := e.entryByID(id)
		if err != nil {
			fail(w, http.StatusNotFound, err)
			return
		}
		h(w, r, en)
	}
}

func (e *Engine) handleList(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID           int    `json:"id"`
		InfoHash     string `json:"info_hash"`
		Name         string `json:"name"`
		OutputFolder string `json:"output_folder"`
	}
	e.mu.Lock()
	list := make([]item, 0, len(e.byID))
	for _, en := range e.byID {
		list = append(list, item{en.id, en.t.InfoHash().HexString(), en.t.Name(), e.names.folder(en.t)})
	}
	e.mu.Unlock()
	reply(w, map[string]any{"torrents": list})
}

func (e *Engine) handleAdd(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	magnet := string(body)
	q := r.URL.Query()
	if q.Get("list_only") == "true" {
		d, err := e.inspect(r.Context(), magnet)
		if err != nil {
			fail(w, addStatus(err), err)
			return
		}
		reply(w, torrent.Torrent{Details: d})
		return
	}
	en, err := e.add(r.Context(), magnet, q.Get("only_files_regex"))
	if err != nil {
		fail(w, addStatus(err), err)
		return
	}
	reply(w, torrent.Torrent{ID: en.id, Details: e.detail(en)})
}

func addStatus(err error) int {
	if errors.Is(err, torrent.ErrUnavailable) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

func (e *Engine) handleStream(w http.ResponseWriter, r *http.Request, en *entry) {
	if en.t.Info() == nil {
		fail(w, http.StatusConflict, errors.New("invalid state: initializing"))
		return
	}
	idx, err := strconv.Atoi(r.PathValue("file"))
	files := en.t.Files()
	if err != nil || idx < 0 || idx >= len(files) {
		fail(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	f := files[idx]
	inner := f.NewReader()
	defer inner.Close()
	inner.SetContext(r.Context())
	inner.SetReadahead(streamReadahead)
	inner.SetResponsive()
	reader := &watched{Reader: inner, e: e, en: en, offset: f.Offset(), pieceLen: en.t.Info().PieceLength}
	defer reader.done()

	name := filepath.Base(fileName(en.t, f))
	kind := mime.TypeByExtension(filepath.Ext(name))
	if kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	http.ServeContent(w, r, name, time.Time{}, reader)
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, err error) {
	http.Error(w, err.Error(), status)
}

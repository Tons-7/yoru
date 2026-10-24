package server

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"yoru/internal/config"
	"yoru/internal/shortcut"
	"yoru/internal/tray"
)

// appExit ends Yoru, answering first: stopping closes the server under the request.
func (s *Server) appExit(w http.ResponseWriter, r *http.Request) {
	if s.exit == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "this Yoru can't be exited from here"})
		return
	}
	send(w, http.StatusAccepted, map[string]any{"exiting": true})
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.exit()
	}()
}

// appStartup says whether Yoru starts in the tray when the user signs in; a POST turns that on or off.
func (s *Server) appStartup(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err != nil {
		s.fail(w, "find Yoru's program", err)
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			On bool `json:"on"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := shortcut.SetAtSignIn(exe, body.On); err != nil {
			s.fail(w, "start at sign-in", err)
			return
		}
	}
	on, err := shortcut.AtSignIn(exe)
	if err != nil {
		s.fail(w, "start at sign-in", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"on": on, "tray": tray.Available})
}

// watching is another device playing through this Yoru, from its progress reports.
type watching struct {
	device           string
	animeID, episode int
	at               time.Time
}

// watchingFor is how long after its last report a device still counts as playing: reports come every 10 s.
const watchingFor = 45 * time.Second

// noteWatching records another device's report, or forgets it once it paused or stopped.
func (s *Server) noteWatching(r *http.Request, animeID, episode int, stopped bool) {
	key := watcherKey(r)
	s.watchingMu.Lock()
	defer s.watchingMu.Unlock()
	if stopped {
		delete(s.watching, key)
		return
	}
	if s.watching == nil {
		s.watching = map[string]watching{}
	}
	s.watching[key] = watching{device: deviceName(r.UserAgent()), animeID: animeID, episode: episode, at: time.Now()}
}

// watcherKey tells devices apart: by their device cookie, else by address and browser, as phones behind one
// router (or one HTTPS proxy) share an address.
func watcherKey(r *http.Request) string {
	if id := requestDevice(r); id != "" {
		return "device " + id
	}
	return clientAddr(r) + " " + r.UserAgent()
}

// appActivity is what exiting would stop on other devices: who is playing what. Downloads the page knows.
func (s *Server) appActivity(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Device  string `json:"device"`
		Title   string `json:"title"`
		Episode int    `json:"episode"`
	}
	s.watchingMu.Lock()
	var now []watching
	for addr, v := range s.watching {
		if time.Since(v.at) > watchingFor {
			delete(s.watching, addr)
			continue
		}
		now = append(now, v)
	}
	s.watchingMu.Unlock()
	out := []entry{}
	for _, v := range now {
		out = append(out, entry{Device: v.device, Title: s.store.ShownTitle(r.Context(), v.animeID), Episode: v.episode})
	}
	slices.SortFunc(out, func(a, b entry) int { return cmp.Compare(a.Device, b.Device) })
	send(w, http.StatusOK, map[string]any{"watching": out})
}

// appShow brings up Yoru's window: a second launch asks this one, so the window stays the running Yoru's.
func (s *Server) appShow(w http.ResponseWriter, r *http.Request) {
	if s.showWindow == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "this Yoru has no window"})
		return
	}
	go s.showWindow()
	w.WriteHeader(http.StatusNoContent)
}

// logText is the last run's log and this one's, to copy or save and send.
func (s *Server) logText(w http.ResponseWriter, r *http.Request) {
	var b bytes.Buffer
	for _, part := range []struct{ label, name string }{
		{"Previous run", config.Name + ".log.1"}, {"This run", config.Name + ".log"},
	} {
		data, err := readCapped(filepath.Join(s.cfg.DataDir(), part.name), 8<<20)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "==== %s (%s) ====\n", part.label, part.name)
		b.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Has("download") {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="yoru-log-%s.txt"`, time.Now().Format("2006-01-02-1504")))
	}
	w.Write(b.Bytes())
}

// readCapped is a file's last max bytes, where the newest lines are.
func readCapped(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > max {
		f.Seek(st.Size()-max, io.SeekStart)
	}
	return io.ReadAll(io.LimitReader(f, max))
}

// logFolder shows yoru.log in the file manager.
func (s *Server) logFolder(w http.ResponseWriter, r *http.Request) {
	if err := config.Reveal(filepath.Join(s.cfg.DataDir(), config.Name+".log")); err != nil {
		s.fail(w, "show the log", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

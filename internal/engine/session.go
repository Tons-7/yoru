package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"

	"yoru/internal/torrent"
)

// record is one torrent in session.json, with rqbit's field names so its session reads too.
type record struct {
	InfoHash     string   `json:"info_hash"`
	Trackers     []string `json:"trackers,omitempty"`
	OutputFolder string   `json:"output_folder,omitempty"`
	OnlyFiles    []int    `json:"only_files"`
	Paused       bool     `json:"is_paused"`
	// Verify: data came from elsewhere (rqbit), so completion is unknown until hashed.
	Verify bool `json:"verify,omitempty"`
	// Top is the file or folder name the data lives under, when not the torrent's own.
	Top string `json:"top,omitempty"`
}

type sessionDoc struct {
	Torrents []record `json:"torrents"`
}

// saveSession writes the torrents and missing metainfo, so a restart needs nothing from the swarm.
func (e *Engine) saveSession() { e.writeSession(false) }

// writeSession: last is a client's final save (Stop's, a rebind's), and saves after it are dropped.
func (e *Engine) writeSession(last bool) {
	e.saveMu.Lock()
	defer e.saveMu.Unlock()
	e.mu.Lock()
	if e.cl == nil || ((e.offline || e.closed) && !last) {
		e.mu.Unlock()
		return
	}
	doc, withInfo := e.sessionLocked()
	e.mu.Unlock()

	for _, t := range withInfo {
		path := filepath.Join(e.opts.SessionDir, t.InfoHash().HexString()+".torrent")
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := writeMetainfo(path, t.Metainfo()); err != nil {
			e.log.Warn("save torrent metainfo", "hash", t.InfoHash().HexString(), "err", err)
		}
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err == nil {
		err = writeAtomic(sessionFile(e.opts.SessionDir), body)
	}
	if err != nil {
		e.log.Warn("save torrent session", "err", err)
	}
}

// sessionLocked is the in-memory session and the torrents whose metainfo can be saved. Caller holds e.mu.
func (e *Engine) sessionLocked() (sessionDoc, []*bt.Torrent) {
	var doc sessionDoc
	var withInfo []*bt.Torrent
	for _, en := range e.byID {
		r := record{InfoHash: en.t.InfoHash().HexString(), Trackers: en.trackers, Paused: en.paused, Verify: en.verifying}
		if info := en.t.Info(); info != nil {
			if top := e.names.top(info.BestName(), en.t.InfoHash()); top != info.BestName() {
				r.Top = top
			}
		} else if top, ok := e.names.claimed(en.t.InfoHash()); ok {
			// Restored from a magnet, its info not back yet: the name it holds must survive this save.
			r.Top = top
		}
		for i, on := range en.selected {
			if on {
				r.OnlyFiles = append(r.OnlyFiles, i)
			}
		}
		slices.Sort(r.OnlyFiles)
		doc.Torrents = append(doc.Torrents, r)
		if en.t.Info() != nil {
			withInfo = append(withInfo, en.t)
		}
	}
	slices.SortFunc(doc.Torrents, func(a, b record) int { return strings.Compare(a.InfoHash, b.InfoHash) })
	return doc, withInfo
}

func writeMetainfo(path string, mi metainfo.MetaInfo) error {
	var body bytes.Buffer
	if err := mi.Write(&body); err != nil {
		return err
	}
	return writeAtomic(path, body.Bytes())
}

// writeAtomic lands the whole file or none: synced before the rename, so a crash cannot truncate the session.
func writeAtomic(path string, body []byte) error {
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// restore re-adds the saved torrents; on first run it imports rqbit's.
func (e *Engine) restore() error {
	var imported string
	e.mu.Lock()
	carry := e.carry
	e.carry = nil
	e.mu.Unlock()
	doc, err := readSession(e.opts.SessionDir)
	if carry != nil {
		doc, err = *carry, nil
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		doc, imported, err = e.importLegacy()
		if err != nil {
			return err
		}
		if imported != "" {
			e.log.Info("carried over downloads from rqbit", "torrents", len(doc.Torrents), "from", imported)
		}
	case err != nil:
		// Held or refused by the OS (an antivirus, a backup tool): the file is likely fine, and a rebuild
		// would lose every torrent's trackers, names and selection. Not starting now, the next try reads it.
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return err
		}
		// Damaged or mistyped: starting empty would let the next save forget every download, so every
		// torrent whose metainfo is here comes back paused instead.
		doc = e.rebuildSession()
		e.log.Warn("torrent session unreadable; rebuilt from the saved torrents", "err", err, "torrents", len(doc.Torrents))
	}

	for _, r := range doc.Torrents {
		var hash metainfo.Hash
		if err := hash.FromHexString(strings.ToLower(r.InfoHash)); err != nil {
			e.log.Warn("restore torrent: bad info hash", "hash", r.InfoHash, "err", err)
			continue
		}
		e.names.claim(hash, r.Top)
		var t *bt.Torrent
		path := filepath.Join(e.opts.SessionDir, hash.HexString()+".torrent")
		if mi, err := metainfo.LoadFromFile(path); err == nil {
			spec, err := bt.TorrentSpecFromMetaInfoErr(mi)
			if err == nil {
				t, _, err = e.cl.AddTorrentSpec(spec)
			}
			if err != nil {
				e.log.Warn("restore torrent", "hash", r.InfoHash, "err", err)
				continue
			}
		} else {
			// A damaged file blocks the next save's rewrite, leaving the swarm to supply the info each start.
			// One the OS only refused (held, denied) is kept: deleted, a dead swarm could never give it back.
			var pathErr *fs.PathError
			if !errors.As(err, &pathErr) {
				os.Remove(path)
			}
			magnet := (&metainfo.Magnet{InfoHash: hash, Trackers: r.Trackers}).String()
			if t, err = e.cl.AddMagnet(magnet); err != nil {
				e.log.Warn("restore torrent", "hash", r.InfoHash, "err", err)
				continue
			}
		}
		// Nothing moves until the selection is applied below.
		t.DisallowDataDownload()
		e.added(t)

		e.mu.Lock()
		en := e.track(t, r.Trackers)
		for _, i := range r.OnlyFiles {
			en.selected[i] = true
		}
		en.paused = r.Paused
		en.verifying = r.Verify
		e.mu.Unlock()
		go e.resume(en)
	}
	e.saveSession()

	// Ours now holds everything it had, metainfo included.
	if imported != "" && torrent.Within(e.opts.CacheDir, imported) {
		if _, err := os.Stat(sessionFile(e.opts.SessionDir)); err == nil {
			if err := os.RemoveAll(imported); err != nil {
				e.log.Warn("remove rqbit's session", "err", err)
			}
		}
	}
	return nil
}

// resume applies a restored torrent's selection once its info is known and rqbit data hashed.
func (e *Engine) resume(en *entry) {
	select {
	case <-en.t.GotInfo():
	case <-en.t.Closed():
		return
	}
	// Before forgetMissing, which would read a file under its old name as lost.
	e.migrateLegacyNames(en.t)
	e.forgetMissing(en.t)
	e.mu.Lock()
	unclean, selected := e.firstSinceCrashLocked(en.t.InfoHash()), maps.Clone(en.selected)
	e.mu.Unlock()
	// After a crash an unfinished download's newest pieces may be marked complete with their data lost.
	recheck := unclean && !verified(en.t, selected)
	e.mu.Lock()
	en.verifying = en.verifying || recheck
	verify := en.verifying
	e.mu.Unlock()
	if verify {
		err := en.t.VerifyDataContext(context.Background())
		e.mu.Lock()
		en.verifying = false
		if err != nil && !errors.Is(err, context.Canceled) {
			en.err = "checking data on disk: " + err.Error()
		}
		e.mu.Unlock()
		e.log.Info("checked carried-over download", "hash", en.t.InfoHash().HexString(), "done", en.t.BytesCompleted())
		e.saveSession()
	}
	e.apply(en)
}

// firstSinceCrashLocked: the last run stopped uncleanly and this torrent is not yet rechecked for it; true once
// per torrent, as every rebind restores the session again. Caller holds e.mu.
func (e *Engine) firstSinceCrashLocked(hash metainfo.Hash) bool {
	if !e.unclean || e.crashChecked[hash] {
		return false
	}
	if e.crashChecked == nil {
		e.crashChecked = map[metainfo.Hash]bool{}
	}
	e.crashChecked[hash] = true
	return true
}

// rebuildSession lists the torrents whose metainfo is on disk, all paused, and moves the unreadable list aside.
func (e *Engine) rebuildSession() sessionDoc {
	var doc sessionDoc
	path := sessionFile(e.opts.SessionDir)
	os.Rename(path, path+".bad")
	entries, _ := os.ReadDir(e.opts.SessionDir)
	for _, d := range entries {
		name := d.Name()
		if !strings.HasSuffix(name, ".torrent") {
			continue
		}
		mi, err := metainfo.LoadFromFile(filepath.Join(e.opts.SessionDir, name))
		if err != nil {
			continue
		}
		info, err := mi.UnmarshalInfo()
		if err != nil {
			continue
		}
		hash := mi.HashInfoBytes()
		r := record{InfoHash: hash.HexString(), Paused: true}
		// Only what is on disk: selecting a whole pack would make the next play fetch every episode. Under its
		// own name, tagged with its hash after a clash, or as named before safe names (moved on restore).
		own, tagged := safeName(info.BestName()), suffixed(safeName(info.BestName()), hash)
		for _, at := range []struct {
			dir, top string
			safe     bool
		}{{own, own, true}, {tagged, tagged, true}, {info.BestName(), own, false}} {
			for i, fi := range info.UpvertedFiles() {
				path := filepath.Join(e.opts.CacheDir, at.dir)
				if parts := fi.BestPath(); info.IsDir() && at.safe {
					path = filepath.Join(append([]string{path}, safePath(parts)...)...)
				} else if info.IsDir() {
					path = filepath.Join(append([]string{path}, parts...)...)
				}
				if st, err := os.Stat(path); err == nil && st.Size() > 0 {
					r.OnlyFiles = append(r.OnlyFiles, i)
				}
			}
			if len(r.OnlyFiles) > 0 {
				if at.top != info.BestName() {
					r.Top = at.top
				}
				break
			}
		}
		doc.Torrents = append(doc.Torrents, r)
	}
	return doc
}

func readSession(dir string) (sessionDoc, error) {
	var doc sessionDoc
	body, err := os.ReadFile(sessionFile(dir))
	if err != nil {
		return doc, err
	}
	return doc, json.Unmarshal(body, &doc)
}

// importLegacy reads the first rqbit session found: its torrents in the cache,
// their selection and metainfo. It returns the session's directory.
func (e *Engine) importLegacy() (sessionDoc, string, error) {
	var out sessionDoc
	for _, dir := range e.opts.LegacySessions {
		if dir == "" {
			continue
		}
		body, err := os.ReadFile(sessionFile(dir))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var legacy struct {
			Torrents map[string]record `json:"torrents"`
		}
		if err == nil {
			err = json.Unmarshal(body, &legacy)
		}
		// One unreadable old session must not leave the engine starting empty; the next one may do.
		if err != nil {
			e.log.Warn("skipping an unreadable rqbit session", "dir", dir, "err", err)
			continue
		}
		for _, r := range legacy.Torrents {
			if r.InfoHash == "" || !torrent.Within(e.opts.CacheDir, r.OutputFolder) {
				continue
			}
			name := strings.ToLower(r.InfoHash) + ".torrent"
			if mi, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				if err := writeAtomic(filepath.Join(e.opts.SessionDir, name), mi); err != nil {
					return out, "", err
				}
			}
			r.Verify = true
			out.Torrents = append(out.Torrents, r)
		}
		slices.SortFunc(out.Torrents, func(a, b record) int { return strings.Compare(a.InfoHash, b.InfoHash) })
		return out, dir, nil
	}
	return out, "", nil
}

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

	"golang.org/x/sync/errgroup"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
)

type record struct {
	InfoHash     string   `json:"info_hash"`
	Trackers     []string `json:"trackers,omitempty"`
	OutputFolder string   `json:"output_folder,omitempty"`
	OnlyFiles    []int    `json:"only_files"`
	Paused       bool     `json:"is_paused"`
	Verify       bool     `json:"verify,omitempty"`
	Top          string   `json:"top,omitempty"`
}

type sessionDoc struct {
	Torrents []record `json:"torrents"`
}

func (e *Engine) saveSession() { e.writeSession(false) }

func (e *Engine) writeSession(last bool) bool {
	e.saveMu.Lock()
	defer e.saveMu.Unlock()
	e.mu.Lock()
	if e.cl == nil || ((e.offline || e.closed) && !last) {
		e.mu.Unlock()
		return false
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
		return false
	}
	return true
}

func (e *Engine) sessionLocked() (sessionDoc, []*bt.Torrent) {
	var doc sessionDoc
	var withInfo []*bt.Torrent
	for _, en := range e.byID {
		r := record{InfoHash: en.t.InfoHash().HexString(), Trackers: en.trackers, Paused: en.paused,
			Verify: en.verifying || e.crashPending[en.t.InfoHash()]}
		if info := en.t.Info(); info != nil {
			r.Top = e.names.top(info.BestName(), en.t.InfoHash())
		} else if top, ok := e.names.claimed(en.t.InfoHash()); ok {
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

func writeAtomic(path string, body []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func (e *Engine) restore() error {
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
		if savedTorrents(e.opts.SessionDir) {
			doc = e.rebuildSession()
			e.log.Warn("torrent session missing; rebuilt from the saved torrents", "torrents", len(doc.Torrents))
		}
	case err != nil:
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return err
		}
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
		mi, err := metainfo.LoadFromFile(path)
		if err == nil {
			var spec *bt.TorrentSpec
			if spec, err = bt.TorrentSpecFromMetaInfoErr(mi); err == nil {
				if t, _, err = e.cl.AddTorrentSpec(spec); err != nil {
					e.log.Warn("restore torrent from its metainfo; asking the swarm", "hash", r.InfoHash, "err", err)
					t = nil
				}
			}
		}
		if t == nil {
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
		t.DisallowDataDownload()
		e.added(t)

		e.mu.Lock()
		en := e.track(t, r.Trackers)
		for _, i := range r.OnlyFiles {
			en.selected[i] = true
		}
		en.paused = r.Paused
		en.verifying = r.Verify
		if e.unclean && !e.crashChecked[hash] {
			if e.crashPending == nil {
				e.crashPending = map[metainfo.Hash]bool{}
			}
			e.crashPending[hash] = true
		}
		e.mu.Unlock()
		go e.resume(en)
	}
	e.saveSession()
	return nil
}

func (e *Engine) resume(en *entry) {
	select {
	case <-en.t.GotInfo():
	case <-en.t.Closed():
		return
	}
	e.migrateLegacyNames(en.t)
	e.forgetMissing(en.t)
	e.mu.Lock()
	unclean, selected := e.firstSinceCrashLocked(en.t.InfoHash()), maps.Clone(en.selected)
	e.mu.Unlock()
	recheck := unclean && !verified(en.t, selected)
	e.mu.Lock()
	whole := en.verifying
	en.verifying = en.verifying || recheck
	verify := en.verifying
	e.mu.Unlock()
	if verify {
		var err error
		if whole {
			err = en.t.VerifyDataContext(context.Background())
		} else {
			err = verifyUnfinished(context.Background(), en.t, selected)
		}
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

func verifyUnfinished(ctx context.Context, t *bt.Torrent, selected map[int]bool) error {
	seen := map[int]bool{}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, f := range t.Files() {
		if !selected[i] || f.BytesCompleted() >= f.Length() {
			continue
		}
		for k := f.BeginPieceIndex(); k < f.EndPieceIndex(); k++ {
			if seen[k] {
				continue
			}
			seen[k] = true
			g.Go(func() error { return t.Piece(k).VerifyDataContext(ctx) })
		}
	}
	return g.Wait()
}

func (e *Engine) firstSinceCrashLocked(hash metainfo.Hash) bool {
	if !e.unclean || e.crashChecked[hash] {
		return false
	}
	if e.crashChecked == nil {
		e.crashChecked = map[metainfo.Hash]bool{}
	}
	e.crashChecked[hash] = true
	delete(e.crashPending, hash)
	return true
}

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
		if err != nil || info.Validate() != nil {
			continue
		}
		hash := mi.HashInfoBytes()
		r := record{InfoHash: hash.HexString(), Paused: true}
		own, tagged := safeName(info.BestName()), suffixed(safeName(info.BestName()), hash)
		pack := packParts(&info)
		for _, at := range []struct {
			dir, top string
			safe     bool
		}{{tagged, tagged, true}, {own, own, true}, {info.BestName(), own, false}} {
			for i, fi := range info.UpvertedFiles() {
				path := filepath.Join(e.opts.CacheDir, at.dir)
				if parts := fi.BestPath(); info.IsDir() && at.safe {
					path = filepath.Join(append([]string{path}, pack[pathKey(parts)]...)...)
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

func savedTorrents(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "*.torrent"))
	return len(m) > 0
}

func readSession(dir string) (sessionDoc, error) {
	var doc sessionDoc
	body, err := os.ReadFile(sessionFile(dir))
	if err != nil {
		return doc, err
	}
	return doc, json.Unmarshal(body, &doc)
}

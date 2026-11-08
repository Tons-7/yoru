package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"yoru/internal/disk"
	"yoru/internal/library"
	"yoru/internal/parse"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func detached(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
		Season  int `json:"season"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episode are required"})
		return
	}
	if s.prefetch == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	if _, err := s.store.Enqueue(r.Context(), body.AnimeID, body.Season, []int{body.Episode}); err != nil {
		s.fail(w, "queue download", err)
		return
	}
	s.downloader.Wake()

	send(w, http.StatusAccepted, map[string]any{
		"queued": true, "animeId": body.AnimeID, "episode": body.Episode,
	})
}

func (s *Server) downloadRelease(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID  int    `json:"animeId"`
		Episode  int    `json:"episode"`
		Season   int    `json:"season"`
		InfoHash string `json:"infoHash"`
		Title    string `json:"title"`
	}
	if !decode(w, r, &body) {
		return
	}
	hash := strings.ToLower(strings.TrimSpace(body.InfoHash))
	if body.AnimeID == 0 || body.Episode <= 0 || len(hash) != 40 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId, episode and infoHash are required"})
		return
	}
	if s.prefetch == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}
	if s.streams != nil {
		if _, live := s.streams.Episode(sessionID(body.AnimeID, body.Episode)); live {
			send(w, http.StatusConflict, map[string]any{"error": "this episode is playing; choose its release from the player"})
			return
		}
	}

	ctx := r.Context()
	epKey := strconv.Itoa(body.Episode)
	if err := s.store.EnqueueRelease(ctx, body.AnimeID, body.Season, body.Episode, hash, strings.TrimSpace(body.Title)); err != nil {
		s.fail(w, "queue download", err)
		return
	}
	if _, err := s.downloader.Restart(ctx, body.AnimeID, epKey); err != nil {
		s.fail(w, "restart download", err)
		return
	}
	s.downloader.Wake()
	send(w, http.StatusAccepted, map[string]any{"queued": true, "animeId": body.AnimeID, "episode": body.Episode, "infoHash": hash})
}

func (s *Server) downloadEpisodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID  int   `json:"animeId"`
		Season   int   `json:"season"`
		Episodes []int `json:"episodes"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || len(body.Episodes) == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episodes are required"})
		return
	}
	if s.prefetch == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	seen := map[int]bool{}
	episodes := make([]int, 0, len(body.Episodes))
	for _, e := range body.Episodes {
		if e > 0 && !seen[e] {
			seen[e] = true
			episodes = append(episodes, e)
		}
	}
	if len(episodes) == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "no valid episodes"})
		return
	}

	added, err := s.store.Enqueue(r.Context(), body.AnimeID, body.Season, episodes)
	if err != nil {
		s.fail(w, "queue downloads", err)
		return
	}
	s.downloader.Wake()

	send(w, http.StatusAccepted, map[string]any{"queued": added, "animeId": body.AnimeID})
}

func (s *Server) downloadAll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Season  int `json:"season"`
		From    int `json:"from"`
		To      int `json:"to"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}
	from, to := body.From, body.To
	if from <= 0 {
		from = 1
	}
	if to <= 0 {
		count, err := s.store.EpisodeCount(r.Context(), body.AnimeID)
		if err != nil || count <= 0 {
			send(w, http.StatusPreconditionFailed, map[string]any{
				"error": "episode count unknown; give an explicit range",
			})
			return
		}
		to = count
	}
	if from > to {
		send(w, http.StatusBadRequest, map[string]any{"error": "from is past the last episode"})
		return
	}
	if to-from >= 500 {
		send(w, http.StatusBadRequest, map[string]any{"error": "range too large"})
		return
	}
	if s.prefetch == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	episodes := make([]int, 0, to-from+1)
	for episode := from; episode <= to; episode++ {
		episodes = append(episodes, episode)
	}

	added, err := s.store.Enqueue(r.Context(), body.AnimeID, body.Season, episodes)
	if err != nil {
		s.fail(w, "queue downloads", err)
		return
	}
	s.downloader.Wake()

	send(w, http.StatusAccepted, map[string]any{
		"queued": added, "animeId": body.AnimeID, "from": from, "to": to,
	})
}

func (s *Server) cancelDownloadAll(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.PathValue("anime"))
	if animeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "anime id is required"})
		return
	}

	removed, err := s.downloader.Cancel(r.Context(), animeID, r.URL.Query().Get("episode"))
	if err != nil {
		s.fail(w, "cancel downloads", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"cancelled": removed})
}

func (s *Server) downloadQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.QueuedDownloads(r.Context())
	if err != nil {
		s.fail(w, "download queue", err)
		return
	}

	waiting := map[int]int{}
	for _, q := range items {
		if q.State != "failed" {
			waiting[q.AnimeID]++
		}
	}
	out := map[string]any{"items": items, "waiting": waiting, "paused": s.downloader.Paused(),
		"watching": s.downloader.Watching()}
	if free, drive := s.downloader.Short(); free > 0 && len(waiting) > 0 {
		out["short"] = map[string]any{"freeBytes": free, "drive": drive, "needBytes": library.MinFree}
	}
	send(w, http.StatusOK, out)
}

func (s *Server) proceedQueue(w http.ResponseWriter, r *http.Request) {
	s.downloader.Proceed()
	send(w, http.StatusOK, map[string]any{"watching": s.downloader.Watching()})
}

func (s *Server) pauseQueue(w http.ResponseWriter, r *http.Request) { s.setQueuePaused(w, r, true) }

func (s *Server) resumeQueue(w http.ResponseWriter, r *http.Request) { s.setQueuePaused(w, r, false) }

func (s *Server) setQueuePaused(w http.ResponseWriter, r *http.Request, paused bool) {
	if err := s.downloader.SetPaused(r.Context(), paused); err != nil {
		s.fail(w, "pause download queue", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"paused": paused})
}

func (s *Server) prioritiseQueued(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int    `json:"animeId"`
		EpKey   string `json:"epKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and epKey are required"})
		return
	}

	moved, err := s.downloader.Now(r.Context(), body.AnimeID, body.EpKey)
	if err != nil {
		s.fail(w, "prioritise download", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"moved": moved})
}

func (s *Server) downloads(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		send(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}

	items, err := s.store.DownloadStatus(r.Context())
	if err != nil {
		s.fail(w, "downloads", err)
		return
	}
	for i := range items {
		rel := parse.Parse(items[i].Name)
		items[i].Group, items[i].Resolution, items[i].Codec = rel.Group, rel.Resolution, rel.Codec
	}
	parts, err := s.store.DownloadParts(r.Context())
	if err != nil {
		s.log.Warn("download parts", "err", err)
	}

	live, err := s.cache.Progress(r.Context())
	if err != nil {
		s.log.Warn("download progress", "err", err)
	} else {
		for i := range items {
			p, ok := live[strings.ToLower(items[i].InfoHash)]
			if !ok {
				continue
			}
			if items[i].Warning = p.Warning; p.Warning != "" {
				if free, drive, err := disk.Free(s.cfg.CacheDir); err == nil {
					items[i].FreeBytes, items[i].Drive = &free, drive
				}
			}
			items[i].OnDisk = p.Bytes
			items[i].Paused = p.Paused
			items[i].Checking = p.Checking
			items[i].Mbps = p.Mbps
			items[i].Peers = p.Peers
			if p.Total > 0 {
				items[i].TotalSize = p.Total
				items[i].Percent = float64(p.Bytes) / float64(p.Total) * 100
			}
			if p.Finished {
				items[i].Percent = 100
			}
		}
	}

	out := make([]downloadItem, len(items))
	for i, d := range items {
		hash := strings.ToLower(d.InfoHash)
		out[i] = downloadItem{Download: d, BitDepth: parse.Parse(d.Name).BitDepth, Files: len(live[hash].Files),
			Parts: liveParts(parts[hash], live[hash].Files, live[hash].Included, d.Percent >= 100)}
	}
	send(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) pauseFile(w http.ResponseWriter, r *http.Request)  { s.setFilePaused(w, r, true) }
func (s *Server) resumeFile(w http.ResponseWriter, r *http.Request) { s.setFilePaused(w, r, false) }

func (s *Server) setFilePaused(w http.ResponseWriter, r *http.Request, paused bool) {
	hash := strings.ToLower(r.PathValue("hash"))
	index, err := strconv.Atoi(r.PathValue("index"))
	if hash == "" || err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "info hash and file index are required"})
		return
	}
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}
	ctx := r.Context()
	key := library.FileKey(hash, index)
	if paused {
		if err := s.cache.PauseFile(ctx, hash, index, true); err != nil {
			s.fail(w, "pause episode", err)
			return
		}
		s.downloader.SetUserPaused(key, true)
		if _, err := s.store.SetReleaseQueuedPaused(ctx, hash, index, true); err != nil {
			s.log.Warn("keep a paused episode in line", "err", err)
		}
		if animeID, ep, ok := s.store.FileEpisode(ctx, hash, index); ok {
			s.downloader.StopEpisode(animeID, ep)
		}
		send(w, http.StatusOK, map[string]any{"infoHash": hash, "fileIndex": index, "paused": true})
		return
	}

	if s.downloader.PausedByHand(hash) {
		parts, _ := s.store.DownloadParts(ctx)
		for _, p := range parts[hash] {
			if p.FileIndex == index || p.Complete {
				continue
			}
			if err := s.cache.PauseFile(ctx, hash, p.FileIndex, true); err != nil {
				s.log.Warn("pause the rest of a pack", "err", err)
			}
			s.downloader.SetUserPaused(library.FileKey(hash, p.FileIndex), true)
		}
		s.downloader.SetUserPaused(hash, false)
	}
	s.downloader.SetUserPaused(key, false)
	inLine, err := s.store.SetReleaseQueuedPaused(ctx, hash, index, false)
	if err != nil {
		s.fail(w, "resume episode", err)
		return
	}
	if inLine == 0 {
		if err := s.cache.PauseFile(ctx, hash, index, false); err != nil {
			s.fail(w, "resume episode", err)
			return
		}
		if err := s.cache.Resume(ctx, hash); err != nil {
			s.log.Warn("resume the pack", "err", err)
		}
	}
	s.downloader.Wake()
	send(w, http.StatusOK, map[string]any{"infoHash": hash, "fileIndex": index, "paused": false, "inLine": inLine})
}

func (s *Server) removeFile(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	index, err := strconv.Atoi(r.PathValue("index"))
	if hash == "" || err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "info hash and file index are required"})
		return
	}
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}
	episodes, _ := s.store.ReleaseEpisodes(r.Context(), hash, index)
	freed, err := s.cache.RemoveFile(r.Context(), hash, index)
	if err != nil {
		s.fail(w, "remove episode", err)
		return
	}
	s.downloader.Forget(r.Context(), hash, index, episodes)
	s.downloader.PruneHandPaused(r.Context())
	send(w, http.StatusOK, map[string]any{"removed": hash, "fileIndex": index, "freedBytes": freed})
}

type downloadItem struct {
	store.Download
	BitDepth int                  `json:"bitDepth,omitempty"`
	Files    int                  `json:"files,omitempty"`
	Parts    []store.DownloadPart `json:"parts"`
}

func liveParts(parts []store.DownloadPart, files []int64, included []bool, finished bool) []store.DownloadPart {
	out := make([]store.DownloadPart, len(parts))
	for i, p := range parts {
		p.Paused = p.FileIndex >= 0 && p.FileIndex < len(included) && !included[p.FileIndex]
		switch {
		case p.Complete || (finished && !p.Paused):
			p.Done, p.Complete = p.Size, true
		case p.FileIndex >= 0 && p.FileIndex < len(files):
			p.Done = min(files[p.FileIndex], p.Size)
			p.Complete = p.Size > 0 && p.Done >= p.Size
		}
		p.Paused = p.Paused && !p.Complete
		out[i] = p
	}
	return out
}

func (s *Server) packScope(ctx context.Context, animeID int) torrent.Scope {
	if s.finder == nil {
		return torrent.Scope{}
	}
	return s.finder.PackScope(ctx, animeID, 1, 0)
}

func (s *Server) betterCopies(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.URL.Query().Get("anime"))
	offers, err := library.BetterCopies(r.Context(), s.store, s.packScope, animeID)
	if err != nil {
		s.fail(w, "better copies", err)
		return
	}
	if offers == nil {
		offers = []library.BetterCopy{}
	}
	send(w, http.StatusOK, map[string]any{"items": offers})
}

func (s *Server) replaceWithBetter(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.PathValue("anime"))
	var body struct {
		InfoHash string `json:"infoHash"`
	}
	if !decode(w, r, &body) {
		return
	}
	n, err := library.ReplaceWithBetter(r.Context(), s.store, s.packScope, animeID, body.InfoHash)
	if err != nil {
		send(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	s.downloader.Wake()
	send(w, http.StatusOK, map[string]any{"queued": n})
}

func (s *Server) dismissBetter(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.PathValue("anime"))
	var body struct {
		InfoHash string `json:"infoHash"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := library.DismissBetter(r.Context(), s.store, s.packScope, animeID, body.InfoHash); err != nil {
		s.fail(w, "dismiss better copy", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"dismissed": true})
}

func (s *Server) removeDownload(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	if hash == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "info hash is required"})
		return
	}
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	episodes, _ := s.store.ReleaseEpisodes(r.Context(), hash, -1)
	freed, err := s.cache.Remove(r.Context(), hash)
	if err != nil {
		s.fail(w, "remove download", err)
		return
	}
	s.downloader.Forget(r.Context(), hash, -1, episodes)
	send(w, http.StatusOK, map[string]any{"removed": hash, "freedBytes": freed})
}

func (s *Server) pauseDownload(w http.ResponseWriter, r *http.Request) {
	s.setDownloadRunning(w, r, false)
}

func (s *Server) resumeDownload(w http.ResponseWriter, r *http.Request) {
	s.setDownloadRunning(w, r, true)
}

func (s *Server) setDownloadRunning(w http.ResponseWriter, r *http.Request, run bool) {
	hash := strings.ToLower(r.PathValue("hash"))
	if hash == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "info hash is required"})
		return
	}
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	ctx := r.Context()
	if !run {
		s.log.Info("download paused from the downloads list", "hash", hash)
		if err := s.cache.Pause(ctx, hash); err != nil {
			s.fail(w, "pause download", err)
			return
		}
		s.downloader.SetUserPaused(hash, true)
		if _, err := s.store.SetReleaseQueuedPaused(ctx, hash, -1, true); err != nil {
			s.log.Warn("keep paused episodes in line", "err", err)
		}
		send(w, http.StatusOK, map[string]any{"paused": true})
		return
	}
	s.downloader.SetUserPaused(hash, false)
	inLine, err := s.store.SetReleaseQueuedPaused(ctx, hash, -1, false)
	if err != nil {
		s.fail(w, "resume download", err)
		return
	}
	if inLine == 0 {
		if err := s.cache.Resume(ctx, hash); err != nil {
			s.fail(w, "resume download", err)
			return
		}
	}
	s.downloader.Wake()
	send(w, http.StatusOK, map[string]any{"paused": false, "inLine": inLine})
}

func (s *Server) arrangeQueued(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int    `json:"animeId"`
		EpKey   string `json:"epKey"`
		Action  string `json:"action"`
		Target  struct {
			AnimeID int    `json:"animeId"`
			EpKey   string `json:"epKey"`
		} `json:"target"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.EpKey == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and epKey are required"})
		return
	}
	var done bool
	var err error
	switch body.Action {
	case "up", "down", "top", "bottom":
		done, err = s.downloader.Move(r.Context(), body.AnimeID, body.EpKey, body.Action)
	case "before", "after":
		if body.Target.AnimeID == 0 || body.Target.EpKey == "" {
			send(w, http.StatusBadRequest, map[string]any{"error": "a target episode is required"})
			return
		}
		done, err = s.downloader.MoveNear(r.Context(), body.AnimeID, body.EpKey, body.Target.AnimeID, body.Target.EpKey, body.Action == "after")
	case "pause", "resume":
		done, err = s.downloader.PauseEpisode(r.Context(), body.AnimeID, body.EpKey, body.Action == "pause")
	default:
		send(w, http.StatusBadRequest, map[string]any{"error": "action is up, down, top, bottom, before, after, pause or resume"})
		return
	}
	if err != nil {
		s.fail(w, "arrange queue", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"done": done})
}

func (s *Server) keepDownload(w http.ResponseWriter, r *http.Request) {
	s.setDownloadKept(w, r, true)
}

func (s *Server) unkeepDownload(w http.ResponseWriter, r *http.Request) {
	s.setDownloadKept(w, r, false)
}

func (s *Server) setDownloadKept(w http.ResponseWriter, r *http.Request, kept bool) {
	hash := strings.ToLower(r.PathValue("hash"))
	if hash == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "info hash is required"})
		return
	}
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}
	if err := s.cache.Keep(r.Context(), hash, kept); err != nil {
		s.fail(w, "keep download", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"infoHash": hash, "kept": kept})
}

func (s *Server) downloadFiles(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	files, err := s.store.DownloadFiles(r.Context(), hash)
	if err != nil {
		s.fail(w, "download files", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"items": files})
}

func (s *Server) keepFile(w http.ResponseWriter, r *http.Request) { s.setFileKept(w, r, true) }

func (s *Server) unkeepFile(w http.ResponseWriter, r *http.Request) { s.setFileKept(w, r, false) }

func (s *Server) setFileKept(w http.ResponseWriter, r *http.Request, kept bool) {
	hash := strings.ToLower(r.PathValue("hash"))
	index, err := strconv.Atoi(r.PathValue("index"))
	if hash == "" || err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "info hash and file index are required"})
		return
	}
	found, err := s.store.KeepFile(r.Context(), hash, index, kept)
	if err != nil {
		s.fail(w, "keep file", err)
		return
	}
	if !found {
		send(w, http.StatusNotFound, map[string]any{"error": "no such file in the download"})
		return
	}
	send(w, http.StatusOK, map[string]any{"infoHash": hash, "fileIndex": index, "kept": kept})
}

func (s *Server) clearDownloads(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	removed, freed, err := s.cache.Clear(r.Context(), r.URL.Query().Get("scope") == "completed")
	if err != nil {
		s.fail(w, "clear downloads", err)
		return
	}
	s.downloader.PruneHandPaused(r.Context())
	send(w, http.StatusOK, map[string]any{"removed": removed, "freedBytes": freed})
}

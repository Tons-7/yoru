package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"yoru/internal/store"
)

var malSite = "https://myanimelist.net"

var favouriteAnime = regexp.MustCompile(`href="(?:https://myanimelist\.net)?/anime/(\d+)/`)

func (s *Server) malFavouritesImport(w http.ResponseWriter, r *http.Request) {
	name, _ := s.store.Setting(r.Context(), "mal.user_name")
	if name == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "MyAnimeList is not connected"})
		return
	}

	ids, err := profileFavourites(r.Context(), malSite, name)
	if err != nil {
		s.log.Error("mal favourites", "err", err)
		send(w, http.StatusBadGateway, map[string]any{"error": "Couldn't read your MyAnimeList profile. Try again later."})
		return
	}

	var matched, favourited int
	var unmatched []int
	for _, malID := range ids {
		id, ok, err := s.store.AnimeIDByMAL(r.Context(), malID)
		if err != nil {
			s.fail(w, "mal favourites", err)
			return
		}
		if !ok {
			unmatched = append(unmatched, malID)
			continue
		}
		matched++
		held, err := s.store.Bookmark(r.Context(), id)
		if err != nil {
			s.fail(w, "mal favourites", err)
			return
		}
		if held.Favourite {
			continue
		}
		held.Favourite = true
		if err := s.store.SetBookmark(r.Context(), id, held); err != nil {
			s.fail(w, "mal favourites", err)
			return
		}
		favourited++
	}
	send(w, http.StatusOK, map[string]any{
		"found": len(ids), "matched": matched, "favourited": favourited,
		"unmatched": len(unmatched),
	})
}

func profileFavourites(ctx context.Context, site, user string) ([]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site+"/profile/"+url.PathEscape(user), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Yoru")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("myanimelist profile: HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	page := string(raw)
	if !strings.Contains(page, `class="user-profile"`) {
		return nil, errors.New("myanimelist sent something other than the profile page")
	}
	_, block, found := strings.Cut(page, `id="anime_favorites"`)
	if !found {
		return nil, nil
	}
	block, _, _ = strings.Cut(block, `_favorites"`)
	var ids []int
	seen := map[int]bool{}
	for _, m := range favouriteAnime.FindAllStringSubmatch(block, -1) {
		if id, _ := strconv.Atoi(m[1]); id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

type malExport struct {
	Anime []struct {
		ID        int    `xml:"series_animedb_id"`
		Title     string `xml:"series_title"`
		Watched   int    `xml:"my_watched_episodes"`
		Start     string `xml:"my_start_date"`
		Finish    string `xml:"my_finish_date"`
		Score     int    `xml:"my_score"`
		Status    string `xml:"my_status"`
		Rewatched int    `xml:"my_times_watched"`
	} `xml:"anime"`
}

var malStatuses = map[string]string{
	"watching": "CURRENT", "completed": "COMPLETED", "on-hold": "PAUSED",
	"dropped": "DROPPED", "plan to watch": "PLANNING",
}

func isGzip(b []byte) bool { return len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b }

func looksLikeXML(b []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(b, "\xef\xbb\xbf \t\r\n"), []byte("<"))
}

func (s *Server) malEntries(ctx context.Context, raw []byte) ([]store.ExportEntry, int, error) {
	if isGzip(raw) {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, 0, err
		}
		if raw, err = io.ReadAll(io.LimitReader(zr, 64<<20)); err != nil {
			return nil, 0, err
		}
	}

	var export malExport
	if err := xml.Unmarshal(raw, &export); err != nil {
		return nil, 0, fmt.Errorf("not a MyAnimeList export: %w", err)
	}

	var entries []store.ExportEntry
	var unmatched int
	for _, a := range export.Anime {
		id, ok, err := s.store.AnimeIDByMAL(ctx, a.ID)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			unmatched++
			continue
		}
		mal := a.ID
		entries = append(entries, store.ExportEntry{
			AnimeID:     id,
			MalID:       &mal,
			Title:       a.Title,
			Status:      malStatuses[normaliseStatus(a.Status)],
			Progress:    a.Watched,
			Score:       a.Score * 10,
			Repeat:      a.Rewatched,
			StartedAt:   malDate(a.Start),
			CompletedAt: malDate(a.Finish),
		})
	}
	return entries, unmatched, nil
}

func normaliseStatus(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func malDate(s string) string {
	if strings.HasPrefix(s, "0000") {
		return ""
	}
	return s
}

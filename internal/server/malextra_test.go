package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/store"
)

const malXML = `<?xml version="1.0" encoding="UTF-8" ?>
<myanimelist>
  <myinfo><user_name>tons</user_name></myinfo>
  <anime>
    <series_animedb_id>52991</series_animedb_id>
    <series_title><![CDATA[Sousou no Frieren]]></series_title>
    <my_watched_episodes>20</my_watched_episodes>
    <my_start_date>2024-01-05</my_start_date>
    <my_finish_date>0000-00-00</my_finish_date>
    <my_score>9</my_score>
    <my_status>Watching</my_status>
    <my_times_watched>0</my_times_watched>
  </anime>
  <anime>
    <series_animedb_id>999999</series_animedb_id>
    <series_title><![CDATA[Nobody Has This]]></series_title>
    <my_watched_episodes>3</my_watched_episodes>
    <my_score>0</my_score>
    <my_status>Plan to Watch</my_status>
  </anime>
</myanimelist>`

func malHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, config.Config{}, nil)
	malID := 52991
	episodes := 28
	if _, err := h.store.ImportList(t.Context(), []store.Anime{
		{ID: 100, MalID: &malID, Romaji: "Sousou no Frieren", Synonyms: "[]", Genres: "[]", Episodes: &episodes},
	}, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestImportReadsAMyAnimeListExport(t *testing.T) {
	h := malHarness(t)

	rec := postLibrary(t, h, "/api/library/import", malXML)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var rep store.ImportReport
	json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep.Entries != 1 || rep.Skipped != 1 {
		t.Fatalf("report = %+v, want the known show applied and the unknown counted", rep)
	}

	e, _ := h.store.ListEntry(t.Context(), 100)
	if e.Progress != 20 || e.Score != 90 || e.Status != "CURRENT" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestImportReadsAGzippedExport(t *testing.T) {
	h := malHarness(t)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(malXML))
	zw.Close()

	rec := postLibrary(t, h, "/api/library/import", buf.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if e, _ := h.store.ListEntry(t.Context(), 100); e.Progress != 20 {
		t.Fatalf("entry = %+v", e)
	}
}

func TestImportRefusesAnExportNothingMatches(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)

	rec := postLibrary(t, h, "/api/library/import", malXML)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestMALStatusesMap(t *testing.T) {
	for in, want := range map[string]string{
		"Watching": "CURRENT", "Completed": "COMPLETED", "On-Hold": "PAUSED",
		"Dropped": "DROPPED", "Plan to Watch": "PLANNING", "weird": "",
	} {
		if got := malStatuses[normaliseStatus(in)]; got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestMALFavouritesImport(t *testing.T) {
	h := malHarness(t)
	h.store.SetSetting(t.Context(), "mal.user_name", "tons")

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profile/tons" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`<div class="user-profile"><a href="https://myanimelist.net/anime/5114/Fullmetal">recently watched</a></div>
			<ul class="favorites-list anime" id="anime_favorites">
			<li><a href="https://myanimelist.net/anime/52991/Sousou_no_Frieren"><img></a>
			<a href="https://myanimelist.net/anime/52991/Sousou_no_Frieren">Frieren</a></li>
			<li><a href="https://myanimelist.net/anime/999999/Unknown">Unknown</a></li></ul>
			<ul class="favorites-list manga" id="manga_favorites">
			<li><a href="https://myanimelist.net/anime/21/One_Piece">One Piece</a></li></ul>`))
	}))
	t.Cleanup(site.Close)
	was := malSite
	malSite = site.URL
	t.Cleanup(func() { malSite = was })

	rec := postLibrary(t, h, "/api/mal/favourites/import", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var rep map[string]int
	json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep["found"] != 2 || rep["favourited"] != 1 || rep["unmatched"] != 1 {
		t.Fatalf("report = %v", rep)
	}
	if b, _ := h.store.Bookmark(t.Context(), 100); !b.Favourite {
		t.Error("the matched favourite was not set")
	}

	rec = postLibrary(t, h, "/api/mal/favourites/import", "")
	json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep["favourited"] != 0 {
		t.Errorf("favourited %d again", rep["favourited"])
	}
}

func TestMALFavouritesRefuseAPageThatIsNotAProfile(t *testing.T) {
	h := malHarness(t)
	h.store.SetSetting(t.Context(), "mal.user_name", "tons")

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><title>Just a moment...</title></html>`))
	}))
	t.Cleanup(site.Close)
	was := malSite
	malSite = site.URL
	t.Cleanup(func() { malSite = was })

	rec := postLibrary(t, h, "/api/mal/favourites/import", "")
	if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "Couldn't read your MyAnimeList profile") {
		t.Fatalf("status = %d: %s; a page that is not a profile must say so, not read as no favourites", rec.Code, rec.Body.String())
	}
}

func TestMALFavouritesNeedAConnection(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	rec := postLibrary(t, h, "/api/mal/favourites/import", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

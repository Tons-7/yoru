package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yoru/internal/config"
)

func TestScheduleFetchesEachDayOnceAndKeepsMidnightEpisodes(t *testing.T) {
	start := midnight(time.Now().Add(-48*time.Hour), 0).Unix()
	const day = 24 * 3600
	airings := []int64{start, start + 3600, start + day, start + day + 7200, start + 2*day, start + 3*day - 1}
	var asked atomic.Int32
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "airingSchedules") {
			fakeShows(w, body, map[int]string{})
			return
		}
		asked.Add(1)
		var q struct {
			Variables struct{ Start, End int64 } `json:"variables"`
		}
		json.Unmarshal(body, &q)
		var found []string
		for i, at := range airings {
			if at > q.Variables.Start && at < q.Variables.End {
				n := strconv.Itoa(i + 1)
				found = append(found, `{"id":`+n+`,"episode":1,"airingAt":`+strconv.FormatInt(at, 10)+`,"mediaId":`+n+
					`,"media":{"id":`+n+`,"title":{"romaji":"Show `+n+`"}}}`)
			}
		}
		io.WriteString(w, `{"data":{"Page":{"pageInfo":{"hasNextPage":false},"airingSchedules":[`+strings.Join(found, ",")+`]}}}`)
	})

	code, body := h.call(t, "GET", "/api/schedule?tz=0&days=3&start="+strconv.FormatInt(start, 10), "")
	if code != http.StatusOK {
		t.Fatalf("schedule answered %d: %v", code, body)
	}
	var got []int64
	for _, it := range body["items"].([]any) {
		got = append(got, int64(it.(map[string]any)["airingAt"].(float64)))
	}
	if len(got) != len(airings) {
		t.Fatalf("got %d episodes %v, want all %d once each", len(got), got, len(airings))
	}
	for i := range got {
		if got[i] != airings[i] {
			t.Fatalf("episodes out of order or missing: %v, want %v", got, airings)
		}
	}
	if n := asked.Load(); n != 3 {
		t.Errorf("AniList was asked %d times, want once per day (3)", n)
	}
}

package discord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The real Discord app takes the handshake and, with YORU_LIVE_CARD=1, the card as Yoru sends it (shown 15 s,
// then cleared). Opt-in: YORU_LIVE=1 go test ./internal/discord -run LiveDiscord -v
func TestLiveDiscord(t *testing.T) {
	if os.Getenv("YORU_LIVE") == "" {
		t.Skip("set YORU_LIVE=1 with the Discord app running")
	}
	c, err := dialRaw()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	t.Log("handshake accepted: Discord answered READY for the application id")
	if os.Getenv("YORU_LIVE_CARD") == "" {
		return
	}

	w := Watching{Title: "Sousou no Frieren", Episode: "Episode 5 of 28", Cover: aniListCover(t, 154587),
		Link: "https://anilist.co/anime/154587", Position: 300, Duration: 1440}
	card := card{w: w, start: time.Now().Add(-5 * time.Minute)}
	reply := func(activity any) map[string]any {
		t.Helper()
		if err := c.setActivity(activity); err != nil {
			t.Fatal(err)
		}
		_, body, err := c.read()
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal(body, &m)
		return m
	}
	if got := reply(card.activity()); got["evt"] == "ERROR" {
		t.Fatalf("Discord turned the card down: %v", got["data"])
	} else {
		t.Logf("Discord took the card: %v", got["data"])
	}
	time.Sleep(15 * time.Second)
	if got := reply(nil); got["evt"] == "ERROR" {
		t.Fatalf("Discord turned the clear down: %v", got["data"])
	}
}

// aniListCover is the show's cover as the app has it, from AniList.
func aniListCover(t *testing.T, id int) string {
	t.Helper()
	body := fmt.Sprintf(`{"query":"{Media(id:%d){coverImage{large}}}"}`, id)
	res, err := http.Post("https://graphql.anilist.co", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Data struct {
			Media struct {
				CoverImage struct{ Large string } `json:"coverImage"`
			}
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.Data.Media.CoverImage.Large == "" {
		t.Fatalf("cover: HTTP %d, %v", res.StatusCode, err)
	}
	return out.Data.Media.CoverImage.Large
}

// dialRaw is dial without the reader, so the test reads Discord's replies itself.
func dialRaw() (*conn, error) {
	var last error
	for i := range 10 {
		rw, err := openPipe(i)
		if err != nil {
			last = err
			continue
		}
		c := &conn{rw: rw}
		if err := c.handshake(); err != nil {
			rw.Close()
			last = err
			continue
		}
		return c, nil
	}
	return nil, last
}

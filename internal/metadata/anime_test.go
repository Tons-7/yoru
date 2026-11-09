package metadata

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestAnimeRetriesABusyHost(t *testing.T) {
	asked := 0
	c := testClient(t, "tenrai-anime", func(w http.ResponseWriter, r *http.Request) {
		asked++
		if asked == 1 {
			http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
			return
		}
		io.WriteString(w, `{"data":{"mal_id":208685,"title":"A MAL-only show","episodes":8}}`)
	})

	got, err := c.Anime(context.Background(), 208685)
	if err != nil {
		t.Fatal(err)
	}
	if got.MalID != 208685 || got.Romaji != "A MAL-only show" || asked != 2 {
		t.Fatalf("got %+v after %d requests", got, asked)
	}
}

package metadata

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestCastVoicesKeepTheirIDs(t *testing.T) {
	c := testClient(t, "characters", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"character":{"mal_id":417,"name":"Lelouch"},"role":"Main","favorites":5,
			"voice_actors":[{"person":{"mal_id":86,"name":"Fukuyama, Jun"},"language":"Japanese"}]}]}`)
	})
	cast, err := c.Characters(context.Background(), 1575)
	if err != nil || len(cast) != 1 || len(cast[0].Voices) != 1 || cast[0].Voices[0].ID != 86 {
		t.Fatalf("cast = %+v, err %v", cast, err)
	}
}

func TestNoSuchPerson(t *testing.T) {
	c := testClient(t, "tenrai-person", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":{}}`) })
	if _, err := c.PersonPage(context.Background(), 1); err != ErrNoPerson {
		t.Fatalf("err = %v, want ErrNoPerson", err)
	}
}

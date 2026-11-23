package anilist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestDeleteEntryTreatsNotFoundAsDone(t *testing.T) {
	answer := `{"errors":[{"message":"Not Found.","status":404}]}`
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, answer) })
	if err := c.DeleteEntry(context.Background(), 7); err != nil {
		t.Fatalf("an entry already gone: %v, want done", err)
	}
	answer = `{"errors":[{"message":"Bad request","status":400}]}`
	if err := c.DeleteEntry(context.Background(), 7); err == nil {
		t.Fatal("a refused delete counted as done")
	}
}

func TestUnreachableClassification(t *testing.T) {
	live := context.Background()
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	netErr := &net.OpError{Op: "dial", Err: errors.New("refused")}
	for _, c := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"cancelled caller", gone, netErr, false},
		{"network error", live, netErr, true},
		{"rate limited", live, Errors{{Status: 429}}, true},
		{"forbidden", live, Errors{{Status: 403}}, true},
		{"server error", live, Errors{{Status: 503}}, true},
		{"bad request", live, Errors{{Status: 400}}, false},
		{"not found", live, Errors{{Status: 404}}, false},
		{"no error", live, nil, false},
		{"our own rate limit ran out of time", live, fmt.Errorf("%w: rate: Wait(n=1) would exceed context deadline", ErrBusy), false},
	} {
		if got := unreachable(c.ctx, c.err); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFavouritesFollowsPagesAndFailsWhole(t *testing.T) {
	var mu sync.Mutex
	var pages []int
	failSecond := false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables struct {
				Page int `json:"page"`
			} `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		pages = append(pages, body.Variables.Page)
		mu.Unlock()
		if body.Variables.Page == 2 && failSecond {
			io.WriteString(w, `{"errors":[{"message":"bad","status":400}]}`)
			return
		}
		var nodes []string
		from, to, more := 1, 50, true
		if body.Variables.Page == 2 {
			from, to, more = 51, 51, false
		}
		for id := from; id <= to; id++ {
			nodes = append(nodes, fmt.Sprintf(`{"id":%d}`, id))
		}
		fmt.Fprintf(w, `{"data":{"User":{"favourites":{"anime":{"pageInfo":{"hasNextPage":%v},"nodes":[%s]}}}}}`,
			more, strings.Join(nodes, ","))
	})

	ids, err := c.Favourites(context.Background(), 1)
	if err != nil || len(ids) != 51 {
		t.Fatalf("got %d ids, %v; want 51", len(ids), err)
	}
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatalf("asked pages %v, want 1 then 2", pages)
	}

	failSecond = true
	ids, err = c.Favourites(context.Background(), 1)
	if err == nil || ids != nil {
		t.Fatalf("a failed page 2 gave %d ids, %v; want nothing and an error", len(ids), err)
	}
}

package anilist

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestRecommendationsSkipAShowAniListNoLongerHas(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		mu.Lock()
		asked = append(asked, q)
		mu.Unlock()
		if strings.Contains(q, "id: 9") {
			w.Write([]byte(`{"errors":[{"message":"Not Found.","status":404}]}`))
			return
		}
		w.Write([]byte(`{"data":{"m0":{"id":1,"recommendations":{"nodes":[{"rating":5,"mediaRecommendation":{"id":50,"type":"ANIME","isAdult":false}}]}}}}`))
	})

	recs, err := c.Recommendations(context.Background(), []int{1, 9}, 5)
	if err != nil || len(recs) != 1 || recs[0].ID != 50 {
		t.Fatalf("first ask = %+v, %v; want the one show that exists", recs, err)
	}
	mu.Lock()
	asked = nil
	mu.Unlock()
	recs, err = c.Recommendations(context.Background(), []int{1, 9}, 5)
	if err != nil || len(recs) != 1 {
		t.Fatalf("second ask = %+v, %v", recs, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || strings.Contains(asked[0], "id: 9") {
		t.Errorf("second ask sent %d requests (%q), want one without the missing show", len(asked), asked)
	}
}

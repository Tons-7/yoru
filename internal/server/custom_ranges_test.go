package server

import (
	"fmt"
	"net/http"
	"testing"

	"yoru/internal/config"
	"yoru/internal/disk"
)

func TestCustomValuesStayInTheirRanges(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	for _, c := range []struct {
		key, value string
		want       int
	}{
		{"sync.poll_seconds", "60", http.StatusBadRequest},
		{"sync.poll_seconds", "300", http.StatusOK},
		{"sync.poll_seconds", "2700", http.StatusOK},
		{"sync.poll_seconds", "700000", http.StatusBadRequest},
		{"cache.budget_bytes", "1000", http.StatusBadRequest},
		{"cache.budget_bytes", "1073741824", http.StatusOK},
		{"downloads.limit_bytes", "0", http.StatusOK},
		{"downloads.limit_bytes", "1000", http.StatusBadRequest},
		{"downloads.limit_bytes", "3125000", http.StatusOK},
		{"downloads.limit_bytes", "-5", http.StatusBadRequest},
	} {
		if res, body := h.postJSON(t, "/api/prefs", map[string]any{"key": c.key, "value": c.value}); res.StatusCode != c.want {
			t.Errorf("%s=%s answered %d %v, want %d", c.key, c.value, res.StatusCode, body, c.want)
		}
	}

	_, body := h.call(t, "GET", "/api/prefs", "")
	ranges, _ := body["ranges"].(map[string]any)
	budget, _ := ranges["cache.budget_bytes"].(map[string]any)
	if budget == nil || budget["min"] != float64(1<<30) {
		t.Fatalf("ranges = %v", ranges)
	}
	if _, total, _, err := disk.Space(h.server.cfg.CacheDir); err == nil && total > 0 {
		if budget["max"] != float64(total) {
			t.Errorf("budget max = %v, want the drive's %d", budget["max"], total)
		}
		if res, _ := h.postJSON(t, "/api/prefs", map[string]any{"key": "cache.budget_bytes", "value": fmt.Sprint(total + 1)}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("a budget larger than the drive answered %d", res.StatusCode)
		}
	}
}

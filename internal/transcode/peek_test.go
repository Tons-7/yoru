package transcode

import (
	"testing"
	"time"
)

// The loaded bar asks while paused, so its look must leave an idle session to the reaper; real use doesn't.
func TestPeekLeavesTheSessionIdle(t *testing.T) {
	s := &Session{touched: time.Now().Add(-time.Hour)}
	m := &Manager{sessions: map[string]*Session{"a": s}}
	if _, ok := m.Peek("a"); !ok {
		t.Fatal("peek found nothing")
	}
	if time.Since(s.lastTouch()) < time.Minute {
		t.Error("a peek counted as use")
	}
	m.Get("a")
	if time.Since(s.lastTouch()) > time.Minute {
		t.Error("a real lookup didn't count as use")
	}
}

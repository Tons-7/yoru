package bt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

// The error net/http's HTTP/2 transport returns on a GOAWAY, wrapped as a body read wraps it.
func goAway(code string) error {
	return fmt.Errorf("reading body: %w", fmt.Errorf(
		"http2: server sent GOAWAY and closed the connection; LastStreamID=3, ErrCode=%s, debug=%q", code, ""))
}

func TestWebseedErrorLevel(t *testing.T) {
	var ws webseedPeer
	ws.peer.closedCtx = context.Background()
	for _, c := range []struct {
		name string
		err  error
		want slog.Level
	}{
		{"a server asking to slow down", goAway("ENHANCE_YOUR_CALM"), slog.LevelInfo},
		{"any other GOAWAY", goAway("PROTOCOL_ERROR"), slog.LevelWarn},
		{"a plain failure", errors.New("unexpected EOF"), slog.LevelWarn},
	} {
		if got := ws.readChunksErrorLevel(c.err, &webseedRequest{}); got != c.want {
			t.Errorf("%s: level %v, want %v", c.name, got, c.want)
		}
	}

	var cancelled webseedRequest
	cancelled.cancelled.Store(true)
	if got := ws.readChunksErrorLevel(goAway("PROTOCOL_ERROR"), &cancelled); got != slog.LevelDebug {
		t.Errorf("a cancelled request: level %v, want debug", got)
	}
}

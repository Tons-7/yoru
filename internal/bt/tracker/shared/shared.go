package shared

import (
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
)

const (
	None AnnounceEvent = iota
	Completed
	Started
	Stopped
)

type AnnounceEvent int32

func (me *AnnounceEvent) UnmarshalText(text []byte) error {
	i := slices.Index(announceEventStrings, string(text))
	if i == -1 {
		return fmt.Errorf("unknown announce event string: %q", text)
	}
	*me = AnnounceEvent(i)
	return nil
}

func (me AnnounceEvent) MarshalText() ([]byte, error) {
	return []byte(announceEventStrings[me]), nil
}

var announceEventStrings = []string{"", "completed", "started", "stopped"}

func (e AnnounceEvent) String() string {
	if e < 0 || int(e) >= len(announceEventStrings) {
		return fmt.Sprintf("<unknown announce event %d>", e)
	}
	s := announceEventStrings[e]
	if e == None && s == "" {
		s = "<regular>"
	}
	return s
}

var _ interface {
	slog.LogValuer
	encoding.TextMarshaler
} = None

func (me AnnounceEvent) LogValue() slog.Value {
	return slog.StringValue(me.String())
}

func (me AnnounceEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(me.String())
}

func (me *AnnounceEvent) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	return me.UnmarshalText([]byte(str))
}

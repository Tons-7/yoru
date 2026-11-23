package bt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"yoru/internal/bt/tracker"

	"yoru/internal/bt/tracker/shared"
)

func TestLazyLogValuer(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.Level(math.MinInt),
	})
	l := slog.New(h)
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	l2 := tor.withSlogger(l)
	l2.Debug("hi")
	t.Log(buf.String())
}

func TestAnnounceEventJSONHandlerLogsAsString(t *testing.T) {

	type TestStruct struct {
		Event shared.AnnounceEvent `json:"event"`
	}

	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.Level(math.MinInt),
	})
	l := slog.New(h)

	testData := TestStruct{
		Event: shared.Started,
	}

	l.Info("test log entry", "data", testData)

	logOutput := buf.String()
	t.Logf("Log output: %s", logOutput)

	var logEntry map[string]any
	err := json.Unmarshal([]byte(logOutput), &logEntry)
	qt.Assert(t, qt.IsNil(err))

	data, ok := logEntry["data"].(map[string]any)
	qt.Assert(t, qt.IsTrue(ok), qt.Commentf("Expected 'data' field to be an object, got %T", logEntry["data"]))

	event, ok := data["event"].(string)
	qt.Assert(t, qt.IsTrue(ok), qt.Commentf("Expected 'event' field to be a string, got %T with value %v", data["event"], data["event"]))

	expectedEventString := shared.Started.String()
	qt.Assert(t, qt.Equals(event, expectedEventString))

	expectedJSON := `"event":"started"`
	qt.Assert(t, qt.IsTrue(strings.Contains(logOutput, expectedJSON)), qt.Commentf("Expected log output to contain %s, but it didn't. Full output: %s", expectedJSON, logOutput))

	qt.Assert(t, qt.IsFalse(strings.Contains(logOutput, `"event":2`)), qt.Commentf("Event was serialized as integer (2) instead of string"))
	qt.Assert(t, qt.IsFalse(strings.Contains(logOutput, `"event": 2`)), qt.Commentf("Event was serialized as integer (2) instead of string"))
}

func TestSlogLoggerWithSameNameCreatesDuplicateGroups(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.Level(math.MinInt),
	})
	l := slog.New(h)

	l1 := l.With(slog.Group("test", "key1", "value1", "key2", "value2"))

	l2 := l1.With(slog.Group("test", "key3", "value3"))

	l2.Info("test message")

	logOutput := buf.String()
	t.Logf("Log output: %s", logOutput)

	qt.Assert(t, qt.IsTrue(strings.Contains(logOutput, `"test":{"key1":"value1","key2":"value2"}`)))
	qt.Assert(t, qt.IsTrue(strings.Contains(logOutput, `"test":{"key3":"value3"}`)))

	var logEntry map[string]any
	err := json.Unmarshal([]byte(logOutput), &logEntry)
	qt.Assert(t, qt.IsNil(err))

	testGroup, ok := logEntry["test"].(map[string]any)
	qt.Assert(t, qt.IsTrue(ok), qt.Commentf("Expected 'test' field to be an object"))

	qt.Assert(t, qt.Equals(testGroup["key3"], "value3"))
	_, hasKey1 := testGroup["key1"]
	qt.Assert(t, qt.IsFalse(hasKey1), qt.Commentf("Due to JSON duplicate key handling, only the last group should be present"))
}

func TestSlogAnnounceRequest(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		AddSource: false,
		Level:     nil,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			fmt.Println(groups, a)
			a.Value.Any()
			return a
		},
	})
	l := slog.New(h)
	l.Info("test", "req", tracker.AnnounceRequest{})
	t.Log(buf.String())
}

package testutil

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	_ "github.com/anacrolix/envpprof"
)

type StatusWriter interface {
	WriteStatus(io.Writer)
}

var (
	mu  sync.Mutex
	sws = map[string]StatusWriter{}
)

func ExportStatusWriter(sw StatusWriter, path string, t testing.TB) (release func()) {
	name := strings.NewReplacer("{", "_", "}", "_").Replace(t.Name())
	pattern := fmt.Sprintf("/%s/%s", name, path)
	t.Logf("exporting status path %q", pattern)
	release = func() {
		mu.Lock()
		defer mu.Unlock()
		sws[pattern] = nil
	}
	mu.Lock()
	defer mu.Unlock()
	if curSw, ok := sws[pattern]; ok {
		if curSw != nil {
			panic(fmt.Sprintf("%q still in use", pattern))
		}
		sws[pattern] = sw
		return
	}
	http.HandleFunc(
		pattern,
		func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			sw := sws[pattern]
			mu.Unlock()
			if sw == nil {
				http.NotFound(w, r)
				return
			}
			sw.WriteStatus(w)
		},
	)
	sws[pattern] = sw
	return
}

package library

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeepNamesAnUnknownDownload(t *testing.T) {
	c, _ := newCache(t)
	err := c.Keep(context.Background(), "deadbeef", true)
	if err == nil || !strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("keep of an unknown download: %v", err)
	}
}

func TestSamePathFollowsTheSystem(t *testing.T) {
	a := filepath.Join("cache", "Show", "ep.mkv")
	if !samePath(a, filepath.Join("cache", ".", "Show", "ep.mkv")) {
		t.Error("the same path written two ways did not match")
	}
	if samePath(a, filepath.Join("cache", "Show", "other.mkv")) {
		t.Error("two different files matched")
	}
}

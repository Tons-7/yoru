package store

import (
	"context"
	"sync"
	"testing"
)

func TestSettingsCacheFollowsEveryWrite(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	read := func(key string) string {
		t.Helper()
		v, err := s.Setting(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if read("a") != "" {
		t.Fatal("an unset key has a value")
	}
	if err := s.SetSetting(ctx, "a", "1"); err != nil {
		t.Fatal(err)
	}
	if read("a") != "1" {
		t.Fatal("a write was not seen")
	}
	if v, err := s.EnsureSetting(ctx, "a", "2"); err != nil || v != "1" {
		t.Fatalf("ensure kept %q, %v", v, err)
	}
	if v, err := s.EnsureSetting(ctx, "b", "3"); err != nil || v != "3" {
		t.Fatalf("ensure wrote %q, %v", v, err)
	}
	if err := s.SetSetting(ctx, "mal.pulled_at", "9"); err != nil {
		t.Fatal(err)
	}
	if read("mal.pulled_at") != "9" {
		t.Fatal("pulled mark not stored")
	}
	if err := s.ForgetPushed(ctx, "mal"); err != nil {
		t.Fatal(err)
	}
	if read("mal.pulled_at") != "" {
		t.Fatal("a deleted setting is still served")
	}
}

func TestSettingsHideSecretsAndCopyTheMap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.SetSettings(ctx, map[string]string{"anilist.token": "secret", "plain": "1"}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := first["anilist.token"]; leaked || first["plain"] != "1" {
		t.Fatalf("settings = %v", first)
	}
	first["plain"] = "changed"
	if v, _ := s.Setting(ctx, "plain"); v != "1" {
		t.Fatalf("a caller's map reached the cache: %q", v)
	}
}

func TestSettingsCacheUnderLoad(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				s.Setting(ctx, "k")
			}
		})
	}
	wg.Go(func() {
		for range 20 {
			s.SetSetting(ctx, "k", "v")
		}
	})
	wg.Wait()
	if v, _ := s.Setting(ctx, "k"); v != "v" {
		t.Fatalf("last write lost: %q", v)
	}
}

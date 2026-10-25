package player

import (
	"strings"
	"testing"
)

func TestAudioLanguagesForMpv(t *testing.T) {
	if !strings.Contains(AudioLanguages("dub"), "eng") {
		t.Error("dub should prefer english")
	}
	if !strings.Contains(AudioLanguages("sub"), "jpn") {
		t.Error("sub should prefer japanese")
	}
	for _, pref := range []string{"either", ""} {
		if AudioLanguages(pref) != "" {
			t.Errorf("%q should impose no language", pref)
		}
	}
}

// Files tag tracks "eng" or "english" as often as "en"; the order is kept.
func TestSubtitleLanguagesForMpv(t *testing.T) {
	if got := SubtitleLanguages([]string{"en", "pt"}); got != "en,eng,english,pt,por,portuguese,pt-br" {
		t.Errorf("slang = %q", got)
	}
	if got := SubtitleLanguages([]string{"xx"}); got != "xx" {
		t.Errorf("an unknown code should pass through, got %q", got)
	}
	if got := SubtitleLanguages(nil); got != "" {
		t.Errorf("no languages should impose none, got %q", got)
	}
}

// Outlines by the names this mpv has: "outline" from 0.38, the old "border" before; no box, the outline look.
func TestSubtitleLookFollowsTheMpv(t *testing.T) {
	newer := func(o string) bool { return o == "sub-outline-color" || o == "sub-border-style" }
	older := func(o string) bool { return false }
	if got := strings.Join(SubtitleLookArgs("yellow", newer), " "); !strings.Contains(got, "--sub-outline-color=#000000") || strings.Contains(got, "border") {
		t.Errorf("newer mpv: %s", got)
	}
	if got := strings.Join(SubtitleLookArgs("box", older), " "); !strings.Contains(got, "--sub-border-size=3") || strings.Contains(got, "background-box") {
		t.Errorf("older mpv, box asked: %s", got)
	}
	if got := SubtitleLookArgs("", newer); got != nil {
		t.Errorf("as made: %v", got)
	}
}

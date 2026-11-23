package parse

import (
	"slices"
	"testing"
)

func TestSubtitleLanguageMarkers(t *testing.T) {
	for name, want := range map[string]string{
		"[G] Show - 01 [Spanish Subs]": "es",
		"[G] Show - 01 (Rus)":          "ru",
		"[G] Show - 01 Arabic":         "ar",
		"[G] Show - 01 Indo Sub":       "id",
		"[G] Show - 01 Vietsub":        "vi",
		"[G] Show - 01 Thai Sub":       "th",
		"[G] Show - 01 Korean Sub":     "ko",
		"[G] Show - 01 German Sub":     "de",
		"[G] Show - 01 Ita Sub":        "it",
		"[G] Show - 01 VOSTFR":         "fr",
		"[G] Show - 01 [English Subs]": "en",
		"[G] Show - 01 [CHS]":          "zh",
		"[G] Show - 01 [JPSC]":         "zh",
		"[G] Show - 01 [JPTC]":         "zh",
		"[G] Show - 01 [PT-BR]":        "pt",
	} {
		if got, _ := detectSubtitles(name); !slices.Contains(got, want) {
			t.Errorf("%q = %v, want %s", name, got, want)
		}
	}
	for _, name := range []string{"[G] Rusty Show - 01", "[G] Indomitable - 01", "[G] Thailand Story - 01", "[G] Show - 01 [Russian Dub]"} {
		if got, _ := detectSubtitles(name); len(got) != 0 {
			t.Errorf("%q read as %v", name, got)
		}
	}
}

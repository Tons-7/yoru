package torrent

import "testing"

func TestPickEpisodeAcceptsAnAliasNumber(t *testing.T) {
	files := []File{
		{Name: "Bleach - Sennen Kessen-hen - 44.mkv", Length: 700 << 20},
		{Name: "Bleach - Sennen Kessen-hen - 45.mkv", Length: 700 << 20},
		{Name: "Bleach - Sennen Kessen-hen - 46.mkv", Length: 700 << 20},
	}

	got, idx, ok := pickEpisode(files, 5, 45, 411)
	if !ok || idx != 1 || got.Name != files[1].Name {
		t.Errorf("picked %q (%d, %v), want the file numbered 45", got.Name, idx, ok)
	}
	if _, _, ok := pickEpisode(files, 5); ok {
		t.Error("episode 5 should not be found without its alias")
	}
	if _, _, ok := pickEpisode(files, 5, 0); ok {
		t.Error("a zero alias matched something")
	}
}

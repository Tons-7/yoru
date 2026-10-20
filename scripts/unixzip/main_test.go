package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// The program keeps its executable bit through the zip, and an app zip is a bundle macOS can open.
func TestZipKeepsModesAndBuildsTheApp(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yoru")
	png := filepath.Join(dir, "icon.png")
	for _, p := range []string{bin, png} {
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "app.zip")
	if err := write(out, []entry{
		{"Yoru.app/Contents/MacOS/yoru", []byte("data"), 0o755},
		{"Yoru.app/Contents/Info.plist", []byte(infoPlist), 0o644},
		{"Yoru.app/Contents/Resources/yoru.icns", icns([]byte("png")), 0o644},
	}); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	modes := map[string]os.FileMode{}
	for _, f := range r.File {
		modes[f.Name] = f.Mode()
	}
	if modes["Yoru.app/Contents/MacOS/yoru"].Perm() != 0o755 {
		t.Errorf("program mode %v, want 0755", modes["Yoru.app/Contents/MacOS/yoru"])
	}
	if modes["Yoru.app/Contents/Info.plist"].Perm() != 0o644 {
		t.Errorf("plist mode %v", modes["Yoru.app/Contents/Info.plist"])
	}
	if got := icns([]byte("png")); string(got[:4]) != "icns" || len(got) != 19 || string(got[8:12]) != "ic09" {
		t.Errorf("icns = %q", got)
	}
}

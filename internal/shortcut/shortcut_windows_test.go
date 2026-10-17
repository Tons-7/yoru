package shortcut

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoru/internal/proc"
)

// makeShortcut writes a .lnk to target, as Explorer would for a pin or a desktop shortcut.
func makeShortcut(t *testing.T, path, target, icon string) {
	t.Helper()
	script := fmt.Sprintf(`$s = (New-Object -ComObject WScript.Shell).CreateShortcut(%s); $s.TargetPath = %s; $s.IconLocation = %s; $s.Save()`,
		proc.Quote(path), proc.Quote(target), proc.Quote(icon))
	if out, err := proc.PowerShell(context.Background(), "-EncodedCommand", proc.Encoded(script)).CombinedOutput(); err != nil {
		t.Fatalf("make shortcut: %v %s", err, out)
	}
}

func readShortcut(t *testing.T, path string) (target, icon string) {
	t.Helper()
	script := fmt.Sprintf(`$s = (New-Object -ComObject WScript.Shell).CreateShortcut(%s); $s.TargetPath; $s.IconLocation`, proc.Quote(path))
	out, err := proc.PowerShell(context.Background(), "-EncodedCommand", proc.Encoded(script)).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(string(out)), "\r", ""), "\n")
	if len(lines) != 2 {
		t.Fatalf("read %q", out)
	}
	return lines[0], lines[1]
}

// A shortcut or pin to kuro.exe opens yoru.exe after the rename, under its old name; others are left alone.
func TestRetargetFollowsTheRename(t *testing.T) {
	app := t.TempDir()
	old, renamed := filepath.Join(app, "kuro.exe"), filepath.Join(app, "yoru.exe")
	desk, pins := t.TempDir(), filepath.Join(t.TempDir(), "Pinned [taskbar]")
	if err := os.MkdirAll(pins, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(app, "other.exe")
	makeShortcut(t, filepath.Join(desk, "kuro.lnk"), old, old+",0")
	makeShortcut(t, filepath.Join(pins, "kuro.lnk"), strings.ToUpper(old), "")
	makeShortcut(t, filepath.Join(desk, "Other.lnk"), other, "")

	n, err := retarget(context.Background(), old, renamed, proc.Quote(desk)+", "+proc.Quote(pins))
	if err != nil || n != 2 {
		t.Fatalf("retargeted %d, %v; want the two to kuro.exe", n, err)
	}
	if target, icon := readShortcut(t, filepath.Join(desk, "kuro.lnk")); !strings.EqualFold(target, renamed) || !strings.EqualFold(icon, renamed+",0") {
		t.Errorf("desktop shortcut: %s, icon %s", target, icon)
	}
	if target, _ := readShortcut(t, filepath.Join(pins, "kuro.lnk")); !strings.EqualFold(target, renamed) {
		t.Errorf("pin: %s", target)
	}
	if target, _ := readShortcut(t, filepath.Join(desk, "Other.lnk")); !strings.EqualFold(target, other) {
		t.Errorf("an unrelated shortcut now opens %s", target)
	}
}

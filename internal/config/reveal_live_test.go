//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestAnExplorerTitleIsMatchedWithOrWithoutItsSuffix(t *testing.T) {
	for title, want := range map[string]bool{
		"cache":                         true,
		"Cache - File Explorer":         true,
		`D:\yoru\cache - File Explorer`: true,
		"cache2 - File Explorer":        false,
		"Downloads":                     false,
	} {
		if got := showsFolder(title, "cache", `D:\yoru\cache`); got != want {
			t.Errorf("%q: %v, want %v", title, got, want)
		}
	}
}

func TestAnOpenedFolderComesToTheFront(t *testing.T) {
	if os.Getenv("YORU_LIVE_WINDOWS") == "" {
		t.Skip("opens a real Explorer window; set YORU_LIVE_WINDOWS=1")
	}
	dir := filepath.Join(t.TempDir(), "yoru-front-check")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ours := func() []uintptr {
		var out []uintptr
		for _, w := range shellWindows() {
			if showsFolder(w.title, filepath.Base(dir), dir) {
				out = append(out, w.hwnd)
			}
		}
		return out
	}
	t.Cleanup(func() {
		for _, hwnd := range ours() {
			postMessage.Call(hwnd, wmClose, 0, 0)
		}
	})
	if err := OpenFolder(dir); err != nil {
		t.Fatal(err)
	}
	var front bool
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		fg, _, _ := getForegroundWindow.Call()
		for _, hwnd := range ours() {
			front = front || hwnd == fg
		}
		if front {
			break
		}
	}
	if len(ours()) == 0 {
		t.Fatal("no Explorer window for the folder appeared")
	}
	if !front {
		t.Fatal("the folder opened behind the window in front")
	}
}

func TestThePickerOpensInFrontAndACloseIsACancel(t *testing.T) {
	if os.Getenv("YORU_LIVE_WINDOWS") == "" {
		t.Skip("opens a real folder window; set YORU_LIVE_WINDOWS=1")
	}
	type picked struct {
		path string
		err  error
	}
	done := make(chan picked, 1)
	go func() {
		path, err := PickFolder("Yoru check: choose a folder", t.TempDir())
		done <- picked{path, err}
	}()
	var dialog uintptr
	front := false
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if dialog = ownDialog(); dialog != 0 {
			fg, _, _ := getForegroundWindow.Call()
			if front = fg == dialog; front {
				break
			}
		}
	}
	if dialog == 0 {
		t.Fatal("no folder window appeared")
	}
	if _, err := PickFolder("again", ""); err != ErrPickBusy {
		t.Errorf("a second picker while one is open: %v, want busy", err)
	}
	postMessage.Call(dialog, wmClose, 0, 0)
	select {
	case r := <-done:
		if r.err != ErrPickCancelled {
			t.Errorf("closing the window: %q %v, want cancelled", r.path, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the window did not end the pick")
	}
	if !front {
		t.Error("the folder window opened behind the window in front")
	}
}

func TestThePickerReturnsTheFolderChosenInIt(t *testing.T) {
	if os.Getenv("YORU_LIVE_WINDOWS") == "" {
		t.Skip("opens a real folder window; set YORU_LIVE_WINDOWS=1")
	}
	want := filepath.Join(t.TempDir(), "Picked anime")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	type picked struct {
		path string
		err  error
	}
	done := make(chan picked, 1)
	go func() {
		path, err := PickFolder("Yoru check: choose a folder", filepath.Dir(want))
		done <- picked{path, err}
	}()
	var dialog uintptr
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if dialog = ownDialog(); dialog != 0 {
			break
		}
	}
	if dialog == 0 {
		t.Fatal("no folder window appeared")
	}
	time.Sleep(700 * time.Millisecond)
	edit := childOfClass(dialog, "Edit")
	if edit == 0 {
		postMessage.Call(dialog, wmClose, 0, 0)
		t.Fatal("the folder window has no box to type in")
	}
	text, _ := syscall.UTF16PtrFromString(want)
	sendMessage.Call(edit, wmSetText, 0, uintptr(unsafe.Pointer(text)))
	button, _, _ := getDlgItem.Call(dialog, idOK)
	if button == 0 {
		postMessage.Call(dialog, wmClose, 0, 0)
		t.Fatal("the folder window has no Select Folder button")
	}
	postMessage.Call(button, bmClick, 0, 0)
	select {
	case r := <-done:
		if r.err != nil || !strings.EqualFold(r.path, want) {
			t.Fatalf("picked %q %v, want %q", r.path, r.err, want)
		}
	case <-time.After(5 * time.Second):
		postMessage.Call(dialog, wmClose, 0, 0)
		t.Fatal("pressing Select Folder did not end the pick")
	}
}

var (
	sendMessage      = user32.NewProc("SendMessageW")
	getDlgItem       = user32.NewProc("GetDlgItem")
	enumChildWindows = user32.NewProc("EnumChildWindows")
)

const (
	wmSetText = 0x000C
	bmClick   = 0x00F5
	idOK      = 1
)

func childOfClass(parent uintptr, class string) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var name [32]uint16
		n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
		if visible, _, _ := isWindowVisible.Call(hwnd); visible != 0 && syscall.UTF16ToString(name[:n]) == class {
			found = hwnd
			return 0
		}
		return 1
	})
	enumChildWindows.Call(parent, cb, 0)
	return found
}

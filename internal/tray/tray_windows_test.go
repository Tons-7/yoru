package tray

import (
	"runtime"
	"testing"
	"unsafe"
)

// Shell_NotifyIconW reads the struct by its size; a field off by padding and the icon never shows.
func TestNotifyIconDataIsTheSizeWindowsReads(t *testing.T) {
	want := map[string]uintptr{"amd64": 976, "arm64": 976, "386": 956}[runtime.GOARCH]
	if got := unsafe.Sizeof(notifyIconData{}); want != 0 && got != want {
		t.Errorf("NOTIFYICONDATAW is %d bytes, want %d", got, want)
	}
}

// The icon goes up and away again, its thread ending with it.
func TestStartAndClose(t *testing.T) {
	icon, err := Start(Options{Tip: "Yoru test", Open: func() {}, Exit: func() {}})
	if err != nil {
		t.Fatal(err)
	}
	icon.Close()
	select {
	case <-icon.done:
	default:
		t.Error("the icon's thread kept running after Close")
	}
}

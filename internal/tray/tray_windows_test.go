package tray

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestNotifyIconDataIsTheSizeWindowsReads(t *testing.T) {
	want := map[string]uintptr{"amd64": 976, "arm64": 976, "386": 956}[runtime.GOARCH]
	if got := unsafe.Sizeof(notifyIconData{}); want != 0 && got != want {
		t.Errorf("NOTIFYICONDATAW is %d bytes, want %d", got, want)
	}
}

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

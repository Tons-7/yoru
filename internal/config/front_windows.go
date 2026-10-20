package config

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	enumWindows         = user32.NewProc("EnumWindows")
	getWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible     = user32.NewProc("IsWindowVisible")
	isIconic            = user32.NewProc("IsIconic")
	showWindowProc      = user32.NewProc("ShowWindow")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	allowSetForeground  = user32.NewProc("AllowSetForegroundWindow")
)

const (
	swRestore = 9
	asfwAny   = 0xFFFFFFFF
)

// One callback for every search: Go never frees one, and after a couple of thousand it panics.
var (
	frontMu    sync.Mutex
	frontPID   uint32
	frontFound uintptr
	frontEnum  = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		getWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != frontPID {
			return 1
		}
		if visible, _, _ := isWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		frontFound = hwnd
		return 0
	})
)

// toFront raises pid's visible window, restoring it if minimised; false when it has none.
func toFront(pid int) bool {
	frontMu.Lock()
	frontPID, frontFound = uint32(pid), 0
	enumWindows.Call(frontEnum, 0)
	found := frontFound
	frontMu.Unlock()
	if found == 0 {
		return false
	}
	if minimised, _, _ := isIconic.Call(found); minimised != 0 {
		showWindowProc.Call(found, swRestore)
	}
	setForegroundWindow.Call(found)
	return true
}

// AllowForeground lets another process raise its window: a second launch has the user's click, while the
// running Yoru, in the background, would only flash its taskbar button.
func AllowForeground() {
	allowSetForeground.Call(asfwAny)
}

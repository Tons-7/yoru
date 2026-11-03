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
	postMessage         = user32.NewProc("PostMessageW")
)

const (
	swRestore = 9
	asfwAny   = 0xFFFFFFFF
	wmClose   = 0x0010
)

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

func windowOf(pid int) uintptr {
	frontMu.Lock()
	defer frontMu.Unlock()
	frontPID, frontFound = uint32(pid), 0
	enumWindows.Call(frontEnum, 0)
	return frontFound
}

func toFront(pid int) bool {
	found := windowOf(pid)
	if found == 0 {
		return false
	}
	if minimised, _, _ := isIconic.Call(found); minimised != 0 {
		showWindowProc.Call(found, swRestore)
	}
	setForegroundWindow.Call(found)
	return true
}

func askClose(pid int) bool {
	found := windowOf(pid)
	if found == 0 {
		return false
	}
	sent, _, _ := postMessage.Call(found, wmClose, 0, 0)
	return sent != 0
}

func AllowForeground() {
	allowSetForeground.Call(asfwAny)
}

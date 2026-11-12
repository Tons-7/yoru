package config

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
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

var (
	getClassName        = user32.NewProc("GetClassNameW")
	getWindowText       = user32.NewProc("GetWindowTextW")
	getForegroundWindow = user32.NewProc("GetForegroundWindow")
	attachThreadInput   = user32.NewProc("AttachThreadInput")
	bringWindowToTop    = user32.NewProc("BringWindowToTop")
)

type shellWindow struct {
	hwnd  uintptr
	title string
}

var (
	shellMu    sync.Mutex
	shellFound []shellWindow
	shellEnum  = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := isWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		var class [64]uint16
		n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if windows.UTF16ToString(class[:n]) != "CabinetWClass" {
			return 1
		}
		var title [512]uint16
		m, _, _ := getWindowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
		shellFound = append(shellFound, shellWindow{hwnd, windows.UTF16ToString(title[:m])})
		return 1
	})
)

func shellWindows() []shellWindow {
	shellMu.Lock()
	defer shellMu.Unlock()
	shellFound = nil
	enumWindows.Call(shellEnum, 0)
	return append([]shellWindow(nil), shellFound...)
}

func raiseShellWindow(folder string, before []shellWindow) {
	old := map[uintptr]bool{}
	for _, w := range before {
		old[w.hwnd] = true
	}
	name := filepath.Base(folder)
	for try := range 40 {
		time.Sleep(100 * time.Millisecond)
		now := shellWindows()
		for _, w := range now {
			if !old[w.hwnd] {
				forceFront(w.hwnd)
				return
			}
		}
		if try < 10 {
			continue
		}
		for _, w := range now {
			if showsFolder(w.title, name, folder) {
				forceFront(w.hwnd)
				return
			}
		}
	}
}

func showsFolder(title, name, folder string) bool {
	title = strings.ToLower(title)
	for _, want := range []string{strings.ToLower(name), strings.ToLower(folder)} {
		if title == want || strings.HasPrefix(title, want+" - ") {
			return true
		}
	}
	return false
}

func forceFront(hwnd uintptr) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	me := windows.GetCurrentThreadId()
	if fg, _, _ := getForegroundWindow.Call(); fg != 0 && fg != hwnd {
		if thread, _, _ := getWindowThreadPID.Call(fg, 0); thread != 0 && uint32(thread) != me {
			attachThreadInput.Call(uintptr(me), thread, 1)
			defer attachThreadInput.Call(uintptr(me), thread, 0)
		}
	}
	if minimised, _, _ := isIconic.Call(hwnd); minimised != 0 {
		showWindowProc.Call(hwnd, swRestore)
	}
	bringWindowToTop.Call(hwnd)
	setForegroundWindow.Call(hwnd)
}

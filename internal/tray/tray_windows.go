package tray

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const Available = true

type Options struct {
	Tip    string
	Open   func()
	Exit   func()
	Ending func()
}

type Tray struct {
	hwnd windows.HWND
	done chan struct{}
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	registerClassEx       = user32.NewProc("RegisterClassExW")
	createWindowEx        = user32.NewProc("CreateWindowExW")
	defWindowProc         = user32.NewProc("DefWindowProcW")
	destroyWindow         = user32.NewProc("DestroyWindow")
	getMessage            = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	postMessage           = user32.NewProc("PostMessageW")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu            = user32.NewProc("AppendMenuW")
	setMenuDefaultItem    = user32.NewProc("SetMenuDefaultItem")
	trackPopupMenu        = user32.NewProc("TrackPopupMenu")
	destroyMenu           = user32.NewProc("DestroyMenu")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	getCursorPos          = user32.NewProc("GetCursorPos")
	loadIcon              = user32.NewProc("LoadIconW")
	setTimer              = user32.NewProc("SetTimer")
	killTimer             = user32.NewProc("KillTimer")
	getSystemMetrics      = user32.NewProc("GetSystemMetrics")
	privateExtractIcons   = user32.NewProc("PrivateExtractIconsW")
	shellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	extractIconEx         = shell32.NewProc("ExtractIconExW")
	getModuleHandle       = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmTimer           = 0x0113
	wmNull            = 0x0000
	wmLButtonUp       = 0x0202
	wmRButtonUp       = 0x0205
	wmApp             = 0x8000
	wmIcon            = wmApp + 1

	nimAdd     = 0
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString       = 0x0
	mfSeparator    = 0x800
	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100
	tpmNoNotify    = 0x80
	idiApplication = 32512
	menuOpen       = 1
	menuExit       = 2
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type notifyIconData struct {
	Size             uint32
	Wnd              windows.HWND
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             windows.Handle
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUIDItem         windows.GUID
	BalloonIcon      windows.Handle
}

type point struct{ X, Y int32 }

type msg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

var (
	mu             sync.Mutex
	current        *running
	taskbarCreated uint32
	ending         atomic.Bool
	shown          atomic.Bool
	retries        int
)

type running struct {
	opts Options
	icon notifyIconData
}

func Start(opts Options) (*Tray, error) {
	t := &Tray{done: make(chan struct{})}
	ready := make(chan error, 1)
	go t.loop(opts, ready)
	if err := <-ready; err != nil {
		if t.hwnd == 0 {
			return nil, err
		}
		return t, err
	}
	return t, nil
}

const (
	iconRetryTimer = 1
	smCxSmIcon     = 49
)

func Shown() bool { return shown.Load() }

func (t *Tray) loop(opts Options, ready chan<- error) {
	runtime.LockOSThread()
	defer close(t.done)

	instance, _, err := getModuleHandle.Call(0)
	if instance == 0 {
		ready <- err
		return
	}
	class, _ := windows.UTF16PtrFromString("YoruTray")
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: windows.NewCallback(wndProc),
		Instance: windows.Handle(instance), ClassName: class}
	if r, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		ready <- err
		return
	}
	title, _ := windows.UTF16PtrFromString("Yoru")
	hwnd, _, err := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0,
		0, 0, 0, 0, 0, 0, uintptr(instance), 0)
	if hwnd == 0 {
		ready <- err
		return
	}
	t.hwnd = windows.HWND(hwnd)

	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	created, _, _ := registerWindowMessage.Call(uintptr(unsafe.Pointer(name)))

	r := &running{opts: opts}
	r.icon = notifyIconData{Wnd: t.hwnd, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmIcon,
		Icon: appIcon()}
	r.icon.Size = uint32(unsafe.Sizeof(r.icon))
	tip, _ := windows.UTF16FromString(opts.Tip)
	copy(r.icon.Tip[:len(r.icon.Tip)-1], tip)
	mu.Lock()
	current, taskbarCreated = r, uint32(created)
	mu.Unlock()
	if ok, _, err := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&r.icon))); ok == 0 {
		setTimer.Call(hwnd, iconRetryTimer, 5000, 0)
		ready <- errors.Join(errors.New("add the tray icon"), err)
	} else {
		shown.Store(true)
		ready <- nil
	}

	var m msg
	for {
		got, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(got) <= 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func appIcon() windows.Handle {
	if exe, err := os.Executable(); err == nil {
		path, _ := windows.UTF16PtrFromString(exe)
		size, _, _ := getSystemMetrics.Call(smCxSmIcon)
		if size > 0 {
			var icon windows.Handle
			var id uint32
			if n, _, _ := privateExtractIcons.Call(uintptr(unsafe.Pointer(path)), 0, size, size,
				uintptr(unsafe.Pointer(&icon)), uintptr(unsafe.Pointer(&id)), 1, 0); n == 1 && icon != 0 {
				return icon
			}
		}
		var large, small windows.Handle
		if n, _, _ := extractIconEx.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&large)),
			uintptr(unsafe.Pointer(&small)), 1); n > 0 && small != 0 {
			return small
		}
	}
	icon, _, _ := loadIcon.Call(0, idiApplication)
	return windows.Handle(icon)
}

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	mu.Lock()
	r := current
	mu.Unlock()
	switch {
	case r == nil:
	case message == wmIcon:
		switch lParam {
		case wmLButtonUp:
			go r.opts.Open()
		case wmRButtonUp:
			r.menu(windows.HWND(hwnd))
		}
		return 0
	case message == wmQueryEndSession:
		return 1
	case message == wmEndSession:
		if wParam != 0 && r.opts.Ending != nil {
			ending.Store(true)
			r.opts.Ending()
		}
		return 0
	case message == wmClose:
		shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&r.icon)))
		destroyWindow.Call(hwnd)
		return 0
	case message == wmDestroy:
		postQuitMessage.Call(0)
		return 0
	case uint32(message) == taskbarCreated:
		if ok, _, _ := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&r.icon))); ok != 0 {
			shown.Store(true)
			killTimer.Call(hwnd, iconRetryTimer)
		}
		return 0
	case message == wmTimer && wParam == iconRetryTimer:
		retries++
		if ok, _, _ := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&r.icon))); ok != 0 {
			shown.Store(true)
			killTimer.Call(hwnd, iconRetryTimer)
		} else if retries >= 6 {
			killTimer.Call(hwnd, iconRetryTimer)
		}
		return 0
	}
	ret, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
	return ret
}

func (r *running) menu(hwnd windows.HWND) {
	h, _, _ := createPopupMenu.Call()
	if h == 0 {
		return
	}
	defer destroyMenu.Call(h)
	open, _ := windows.UTF16PtrFromString("Open Yoru")
	exit, _ := windows.UTF16PtrFromString("Exit Yoru")
	appendMenu.Call(h, mfString, menuOpen, uintptr(unsafe.Pointer(open)))
	appendMenu.Call(h, mfSeparator, 0, 0)
	appendMenu.Call(h, mfString, menuExit, uintptr(unsafe.Pointer(exit)))
	setMenuDefaultItem.Call(h, menuOpen, 0)

	var pt point
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	setForegroundWindow.Call(uintptr(hwnd))
	cmd, _, _ := trackPopupMenu.Call(h, tpmRightButton|tpmReturnCmd|tpmNoNotify, uintptr(pt.X), uintptr(pt.Y), 0,
		uintptr(hwnd), 0)
	postMessage.Call(uintptr(hwnd), wmNull, 0, 0)
	switch cmd {
	case menuOpen:
		go r.opts.Open()
	case menuExit:
		go r.opts.Exit()
	}
}

func (t *Tray) Close() {
	if t == nil || t.hwnd == 0 || ending.Load() {
		return
	}
	postMessage.Call(uintptr(t.hwnd), wmClose, 0, 0)
	select {
	case <-t.done:
	case <-time.After(2 * time.Second):
	}
}

package config

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                       = windows.NewLazySystemDLL("ole32.dll")
	shell32                     = windows.NewLazySystemDLL("shell32.dll")
	coCreateInstance            = ole32.NewProc("CoCreateInstance")
	coTaskMemFree               = ole32.NewProc("CoTaskMemFree")
	shCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidShellItem        = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	clsctxInprocServer = 0x1
	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosPathMustExist   = 0x800
	sigdnFileSysPath   = 0x80058000
	hrCancelled        = 0x800704C7

	vtRelease    = 2
	vtShow       = 3
	vtSetOptions = 9
	vtGetOptions = 10
	vtSetFolder  = 12
	vtSetTitle   = 17
	vtGetResult  = 20

	vtGetDisplayName = 5
)

type comObject struct{ vtbl *[32]uintptr }

//go:uintptrescapes
func (o *comObject) call(slot int, args ...uintptr) int32 {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return int32(r)
}

func (o *comObject) release() { o.call(vtRelease) }

var pickMu sync.Mutex

func CanPick() bool { return true }

func PickFolder(title, start string) (string, error) {
	if !pickMu.TryLock() {
		raiseDialog(nil)
		return "", ErrPickBusy
	}
	defer pickMu.Unlock()
	type picked struct {
		path string
		err  error
	}
	done := make(chan picked, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		path, err := pickHere(title, start)
		done <- picked{path, err}
	}()
	r := <-done
	return r.path, r.err
}

func pickHere(title, start string) (string, error) {
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && err != syscall.Errno(1) {
		return "", err
	}
	defer windows.CoUninitialize()

	var dlg *comObject
	hr, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if int32(hr) < 0 || dlg == nil {
		return "", syscall.Errno(hr)
	}
	defer dlg.release()

	var opts uint32
	dlg.call(vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	dlg.call(vtSetOptions, uintptr(opts|fosPickFolders|fosForceFileSystem|fosPathMustExist))
	if title != "" {
		if t, err := windows.UTF16PtrFromString(title); err == nil {
			dlg.call(vtSetTitle, uintptr(unsafe.Pointer(t)))
		}
	}
	if info, err := os.Stat(start); err == nil && info.IsDir() {
		if p, err := windows.UTF16PtrFromString(start); err == nil {
			var item *comObject
			hr, _, _ := shCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0,
				uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item)))
			if int32(hr) >= 0 && item != nil {
				dlg.call(vtSetFolder, uintptr(unsafe.Pointer(item)))
				item.release()
			}
		}
	}

	stop := make(chan struct{})
	go raiseDialog(stop)
	shown := dlg.call(vtShow, 0)
	close(stop)
	if uint32(shown) == hrCancelled {
		return "", ErrPickCancelled
	}
	if shown < 0 {
		return "", syscall.Errno(uint32(shown))
	}

	var item *comObject
	if hr := dlg.call(vtGetResult, uintptr(unsafe.Pointer(&item))); hr < 0 || item == nil {
		return "", syscall.Errno(uint32(hr))
	}
	defer item.release()
	var name *uint16
	if hr := item.call(vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); hr < 0 || name == nil {
		return "", syscall.Errno(uint32(hr))
	}
	defer coTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	return windows.UTF16PtrToString(name), nil
}

var (
	dialogMu    sync.Mutex
	dialogPID   uint32
	dialogFound uintptr
	dialogEnum  = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		getWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != dialogPID {
			return 1
		}
		if visible, _, _ := isWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		var class [16]uint16
		n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if windows.UTF16ToString(class[:n]) != "#32770" {
			return 1
		}
		dialogFound = hwnd
		return 0
	})
)

func ownDialog() uintptr {
	dialogMu.Lock()
	defer dialogMu.Unlock()
	dialogPID, dialogFound = uint32(os.Getpid()), 0
	enumWindows.Call(dialogEnum, 0)
	return dialogFound
}

func raiseDialog(stop <-chan struct{}) {
	for range 50 {
		if hwnd := ownDialog(); hwnd != 0 {
			forceFront(hwnd)
			return
		}
		if stop == nil {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

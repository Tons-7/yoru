package power

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	powerCreateRequest = kernel32.NewProc("PowerCreateRequest")
	powerSetRequest    = kernel32.NewProc("PowerSetRequest")
	powerClearRequest  = kernel32.NewProc("PowerClearRequest")
	setExecutionState  = kernel32.NewProc("SetThreadExecutionState")
)

const (
	powerRequestSystemRequired = 1
	reasonSimpleString         = 0x1
	esSystemRequired           = 0x1
)

// reasonContext is REASON_CONTEXT with its simple string; the padding covers the union's detailed form.
type reasonContext struct {
	version, flags uint32
	reason         *uint16
	_              [2]uint32
	_              uintptr
}

// hold: a power request, not SetThreadExecutionState: tied to no OS thread, and powercfg /requests names Yoru.
func hold(why string) (func(), error) {
	text, err := windows.UTF16PtrFromString("Yoru: " + why)
	if err != nil {
		return nil, err
	}
	ctx := reasonContext{flags: reasonSimpleString, reason: text}
	h, _, err := powerCreateRequest.Call(uintptr(unsafe.Pointer(&ctx)))
	if windows.Handle(h) == windows.InvalidHandle {
		return nil, fmt.Errorf("create power request: %w", err)
	}
	if ok, _, err := powerSetRequest.Call(h, powerRequestSystemRequired); ok == 0 {
		windows.CloseHandle(windows.Handle(h))
		return nil, fmt.Errorf("set power request: %w", err)
	}
	return func() {
		powerClearRequest.Call(h, powerRequestSystemRequired)
		windows.CloseHandle(windows.Handle(h))
		// Restarts the idle timer: the sleep setting's full wait from here, not sleep at an idle hour's end.
		setExecutionState.Call(esSystemRequired)
	}, nil
}

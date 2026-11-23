//go:build windows

package library

import (
	"io/fs"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getCompressedFileSize = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

func allocatedSize(path string, info fs.FileInfo) int64 {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return info.Size()
	}

	var high uint32
	low, _, callErr := getCompressedFileSize.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == 0xFFFFFFFF && callErr != syscall.Errno(0) {
		return info.Size()
	}
	return int64(high)<<32 | int64(uint32(low))
}

package disk

import "golang.org/x/sys/windows"

func space(dir string) (int64, int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, err
	}
	var avail, total, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &all); err != nil {
		return 0, 0, err
	}
	return int64(avail), int64(total), nil
}

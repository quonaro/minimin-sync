//go:build windows
// +build windows

package disk

import "golang.org/x/sys/windows"

// FreeBytes returns available bytes on the drive containing path.
func FreeBytes(path string) (uint64, error) {
	var free, total, totalFree uint64
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &free, &total, &totalFree); err != nil {
		return 0, err
	}
	return free, nil
}

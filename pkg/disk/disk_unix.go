//go:build !windows
// +build !windows

package disk

import "golang.org/x/sys/unix"

// FreeBytes returns available bytes on the filesystem containing path.
func FreeBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

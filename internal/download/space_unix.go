//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package download

import "syscall"

// FreeBytes reports the bytes an unprivileged writer may still use on the
// filesystem holding path, which is what FR-038's disk preflight compares the
// plan's sizes against.
func FreeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

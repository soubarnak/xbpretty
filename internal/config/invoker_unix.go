//go:build unix

package config

import (
	"io/fs"
	"syscall"
)

// logOpenFlags make the transaction log open refuse a symlink at the last
// component (O_NOFOLLOW) and never block on a FIFO (O_NONBLOCK). The
// regular-file check then rejects a FIFO before anything is written to it.
const logOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// fileNlink returns how many hard links name the file. The invoker can link a
// file root may write, so the log must be the only name for its inode.
func fileNlink(info fs.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Nlink), true
}

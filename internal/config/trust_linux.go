//go:build linux

package config

import (
	"io/fs"
	"syscall"
)

// fileOwner reads the owning uid of the descriptor's target inode, which is
// how FR-055 judges symlinks: the opened descriptor's target, not the link.
func fileOwner(info fs.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

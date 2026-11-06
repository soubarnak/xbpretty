//go:build !linux

package config

import "io/fs"

// fileOwner fails closed on platforms where the owner cannot be read, so the
// privileged-path gate never trusts a file it cannot judge.
func fileOwner(fs.FileInfo) (uint32, bool) { return 0, false }

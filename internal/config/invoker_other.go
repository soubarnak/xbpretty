//go:build !unix

package config

import "io/fs"

// logOpenFlags is empty where O_NOFOLLOW and O_NONBLOCK are not available. The
// owner check fails closed on such platforms too (see fileOwner in
// trust_other.go), so an invoking user's log is refused there rather than
// written unchecked.
const logOpenFlags = 0

// fileNlink cannot count hard links here, so an invoking user's log is refused
// rather than trusted.
func fileNlink(fs.FileInfo) (uint64, bool) { return 0, false }

//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package download

import "errors"

// FreeBytes has no statvfs on this platform, so the disk preflight is skipped
// rather than guessed. xbps reports the shortage itself on the fallback path.
func FreeBytes(string) (uint64, error) {
	return 0, errors.New("disk space checks are unavailable on this platform")
}

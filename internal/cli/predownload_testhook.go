//go:build xbpretty_testroot

package cli

import "os"

// Predownload test hook, compiled only with the xbpretty_testroot build tag.
// It lets a built-binary test give the process a writable cache directory
// instead of the machine's /var/cache/xbps, so a fake-root real transaction is
// hermetic on any host (see docs/adr/0015 and issue #72).
//
//	XBPRETTY_TEST_CACHE_DIR=<dir>   use <dir> as the xbps cache
func init() {
	if v := os.Getenv("XBPRETTY_TEST_CACHE_DIR"); v != "" {
		cacheDirFn = func() string { return v }
	}
}

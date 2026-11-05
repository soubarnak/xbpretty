//go:build xbpretty_testroot

package cli

import (
	"os"
	"strconv"
)

// Test-only root hook, compiled only with the xbpretty_testroot build tag.
// The built-binary tests use it to drive the successful-transaction path
// (history recording) without running the suite as root. The config package
// reads the same variable, so both halves of a run agree on the effective uid.
//
//	XBPRETTY_TEST_EUID=0    pretend to be root for the transaction guards
func init() {
	if v := os.Getenv("XBPRETTY_TEST_EUID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			euidFn = func() int { return n }
		}
	}
}

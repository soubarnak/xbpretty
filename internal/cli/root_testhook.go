//go:build xbpretty_testroot

package cli

import (
	"os"
	"strconv"
)

// Test-only root hook. It is compiled only with the xbpretty_testroot build
// tag, so the release binary always asks the kernel for the effective uid.
// The built-binary tests use it to exercise root-only transaction and clean
// paths without running the suite as root:
//
//	XBPRETTY_TEST_EUID=0   pretend to be root for the privileged checks
func init() {
	if v := os.Getenv("XBPRETTY_TEST_EUID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			euidFn = func() int { return n }
		}
	}
}

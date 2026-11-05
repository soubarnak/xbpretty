//go:build xbpretty_testroot

package cli

import (
	"os"
	"strconv"
)

// Root test hook, compiled only with the xbpretty_testroot build tag. It is
// the cli counterpart of internal/config/testroot_testhook.go and reads the
// same variable, so a built-binary test can exercise the root-gated sync step
// (FR-013) and the root-only `update` (FR-012) without running the suite as
// root.
//
//	XBPRETTY_TEST_EUID=<uid>   pretend to be that effective user
func init() {
	if v := os.Getenv("XBPRETTY_TEST_EUID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			euidFn = func() int { return n }
		}
	}
}

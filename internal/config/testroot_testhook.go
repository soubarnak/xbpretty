//go:build xbpretty_testroot

package config

import (
	"os"
	"strconv"
)

// Test-only root hook. It is compiled only with the xbpretty_testroot build
// tag, so the production binary has no euid, trust-owner or path override.
//
// The built-binary tests use it to exercise FR-054/FR-055 without running the
// suite as root and without creating /etc files:
//
//	XBPRETTY_TEST_EUID=0              pretend to be root for config decisions
//	XBPRETTY_TEST_TRUST_UID=<uid>     owner a trusted path layer must have
//	XBPRETTY_TEST_SYSTEM_CONFIG=<p>   read the system layer from p
//	XBPRETTY_TEST_SUDO_HOME=<dir>     skip the passwd lookup for SUDO_USER/DOAS_USER
func init() {
	if v := os.Getenv("XBPRETTY_TEST_EUID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			euid = func() int { return n }
		}
	}
	if v := os.Getenv("XBPRETTY_TEST_TRUST_UID"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			trustedUID = uint32(n)
		}
	}
	if v := os.Getenv("XBPRETTY_TEST_SYSTEM_CONFIG"); v != "" {
		systemConfigPath = v
	}
	if v := os.Getenv("XBPRETTY_TEST_SUDO_HOME"); v != "" {
		invokerHomeOverride = v
	}
}

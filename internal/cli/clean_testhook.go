//go:build xbpretty_testroot

package cli

import "os"

// Clean test hook, compiled only with the xbpretty_testroot build tag. It lets
// the built-binary tests point `clean --lists` at a fixture instead of the
// real package database, without adding an escape hatch to the release binary.
//
//	XBPRETTY_TEST_XBPSDB=<dir>   walk <dir> instead of /var/db/xbps
func init() {
	if v := os.Getenv("XBPRETTY_TEST_XBPSDB"); v != "" {
		xbpsDBDir = v
	}
}

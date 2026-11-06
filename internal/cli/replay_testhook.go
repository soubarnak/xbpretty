//go:build xbpretty_testroot

package cli

import "os"

// Replay test hook, compiled only with the xbpretty_testroot build tag. It
// lets a built-binary test keep the temporary cache-only replay repository in
// its own temp tree instead of /var/tmp, without adding an escape hatch to
// the release binary (see docs/adr/0013 and issue #70).
//
//	XBPRETTY_TEST_REPLAY_TMPDIR=<dir>   create and sweep replay repos in <dir>
func init() {
	if v := os.Getenv("XBPRETTY_TEST_REPLAY_TMPDIR"); v != "" {
		replayTmpBase = v
	}
}

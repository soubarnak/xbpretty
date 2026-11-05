//go:build xbpretty_testroot

package cli

import "os"

// Fetch test hook, compiled only with the xbpretty_testroot build tag. It lets
// the built-binary tests point fetch at a local fixture without adding any
// escape hatch to the release binary (see docs/adr/0015 and issue #73).
//
//	XBPRETTY_TEST_FETCH_XBPSD=<dir>       read repository= lines from <dir>
//	XBPRETTY_TEST_FETCH_MIRROR_LIST=<url> fetch the mirror list from <url>
//	XBPRETTY_TEST_FETCH_ARCH=<arch>       probe <arch>-repodata
func init() {
	if v := os.Getenv("XBPRETTY_TEST_FETCH_XBPSD"); v != "" {
		fetchXBPSDDirs = []string{v}
	}
	if v := os.Getenv("XBPRETTY_TEST_FETCH_MIRROR_LIST"); v != "" {
		fetchMirrorListURL = v
	}
	if v := os.Getenv("XBPRETTY_TEST_FETCH_ARCH"); v != "" {
		fetchArch = v
	}
}

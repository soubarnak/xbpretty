//go:build !xbpretty_testroot

package cli

// The replay test hook is compiled only with the xbpretty_testroot build tag;
// the release binary always stages replay repositories under /var/tmp.

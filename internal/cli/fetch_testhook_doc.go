//go:build !xbpretty_testroot

package cli

// The fetch test hook is compiled only with the xbpretty_testroot build tag;
// the release binary always uses the live mirror list and /etc/xbps.d.

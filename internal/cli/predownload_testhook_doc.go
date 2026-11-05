//go:build !xbpretty_testroot

package cli

// The predownload test hook is compiled only with the xbpretty_testroot build
// tag; the release binary always fills /var/cache/xbps.

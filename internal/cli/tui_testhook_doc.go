//go:build !xbpretty_testroot

package cli

// The TUI test hook is compiled only with the xbpretty_testroot build tag;
// the release binary always uses tui.Open, tui.RenderSummary and no frame
// dump or injected failure.

//go:build windows

package tui

import "os"

// eventSignals is what a full-screen session watches on Windows, where the
// engine has no termios support and the surfaces are unavailable; it still
// compiles so the cross-build stays clean (ADR-0017 §5).
var eventSignals = []os.Signal{os.Interrupt}

// isResizeSignal is false on Windows: there is no SIGWINCH.
func isResizeSignal(sig os.Signal) bool { return false }

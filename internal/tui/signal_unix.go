//go:build !windows

package tui

import (
	"os"
	"syscall"
)

// eventSignals is what a full-screen session watches. SIGWINCH drives the
// resize path; the others are the terminal restoration paths.
var eventSignals = []os.Signal{syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

// isResizeSignal reports whether sig is SIGWINCH.
func isResizeSignal(sig os.Signal) bool { return sig == syscall.SIGWINCH }

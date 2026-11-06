// Package term is xbpretty's zero-dependency terminal engine (FR-072,
// ADR-0017). It owns raw and cbreak termios, the alternate screen, cursor and
// mouse/paste toggles, the ordered restore guard, the input decoder, the
// display-width policy, and the optional device-status-report cursor query.
//
// The platform-specific calls live in term_linux.go; every other GOOS gets
// compiling stubs in term_other.go, so the package builds CGO-free everywhere
// and the surfaces simply report an unavailable terminal.
package term

import "os"

// ErrUnsupported is returned by the non-Linux stubs and by a guard asked to
// enter a mode the platform cannot supply.
var ErrUnsupported = errUnsupported

// FileIsTerminal reports whether f is a terminal.
func FileIsTerminal(f *os.File) bool { return isTTY(f) }

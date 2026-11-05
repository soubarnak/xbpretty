package tui

import (
	"os"
	"time"

	"github.com/soubarnak/xbpretty/internal/term"
)

// DefaultAnchorTimeout is the optional DSR budget (ADR-0017 §5): 100 ms, then
// the no-anchor fallback. It is short enough that a silent terminal costs one
// frame at startup, never a hang.
const DefaultAnchorTimeout = 100 * time.Millisecond

// Anchor is an optional absolute cursor position captured with DSR. A terminal
// that never answers leaves OK false; callers then use BeginSequence's
// no-anchor fallback (ADR-0017 §5, AC-055).
type Anchor struct {
	Row, Col int
	OK       bool
}

// LocateAnchor sends the DSR query and waits up to timeout. A timeout is a
// normal outcome for test harnesses, multiplexers and dumb terminals.
func LocateAnchor(in, out *os.File, timeout time.Duration) Anchor {
	res := term.QueryCursor(in, out, timeout)
	return Anchor{Row: res.Row, Col: res.Col, OK: res.OK}
}

// BeginSequence is what an inline surface writes before its first draw. With
// an anchor it writes nothing; without one it starts on a fresh line so the
// box can be re-anchored relative to the cursor. #79 consumes this seam.
func (a Anchor) BeginSequence() string {
	if a.OK {
		return ""
	}
	return term.NoAnchor
}

package tui

import (
	"github.com/soubarnak/xbpretty/internal/term"
)

// Anchor is an optional absolute cursor position captured with DSR. A terminal
// that never answers leaves OK false; callers then use BeginSequence's
// no-anchor fallback (ADR-0017 §5, AC-055).
type Anchor struct {
	Row, Col int
	OK       bool
	// Stray holds bytes read while looking for the DSR reply (keys typed
	// during the timeout window). The caller must replay them into the input
	// decoder instead of dropping user input.
	Stray []byte
}

// BeginSequence is what an inline surface writes before its first draw. With
// an anchor it writes nothing; without one it starts on a fresh line so the
// box can be re-anchored relative to the cursor. The inline viewport consumes
// this seam.
func (a Anchor) BeginSequence() string {
	if a.OK {
		return ""
	}
	return term.NoAnchor
}

package term

// The exact ANSI byte sequences the full-screen surfaces use (ADR-0017 §5).
// They are platform independent: they are only ever written to a tty that the
// platform layer has already accepted.
const (
	AltEnter   = "\x1b[?1049h"
	AltLeave   = "\x1b[?1049l"
	CursorHide = "\x1b[?25l"
	CursorShow = "\x1b[?25h"
	ClearAll   = "\x1b[2J\x1b[H"
	ClearDown  = "\x1b[0J"
	MouseOn    = "\x1b[?1000h\x1b[?1006h"
	MouseOff   = "\x1b[?1000l\x1b[?1006l"
	PasteOn    = "\x1b[?2004h"
	PasteOff   = "\x1b[?2004l"
	DSRQuery   = "\x1b[6n"
	// NoAnchor is the fallback when a terminal never answers DSR: start the
	// inline surface on a fresh line and anchor relative to the cursor.
	NoAnchor = "\r\n"
)

// MoveTo emits a 1-based absolute cursor move.
func MoveTo(row, col int) string {
	return "\x1b[" + itoa(row) + ";" + itoa(col) + "H"
}

// MoveUp emits a relative cursor-up.
func MoveUp(n int) string {
	if n <= 0 {
		return ""
	}
	return "\x1b[" + itoa(n) + "A"
}

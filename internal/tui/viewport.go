package tui

import (
	"io"
	"strings"

	"github.com/soubarnak/xbpretty/internal/term"
)

// clearDown erases from the cursor to the end of the screen. It is the
// viewport's equivalent of term.ClearDown and intentionally matches the byte
// the #66 progress renderer already emits, so the two teardown paths agree.
const clearDown = "\x1b[J"

// Viewport is the inline bottom-anchored progress box (FR-072): no alternate
// screen, a fixed view height, completed lines inserted above the box into
// ordinary scrollback, and re-anchoring after each insertion. It never reads
// input and never enters an alternate screen.
type Viewport struct {
	out    io.Writer
	width  int
	height int
	// started is set by Start, drawn tracks whether the box is currently on
	// screen, hidden is the `?1049h/l` latch's state.
	started bool
	drawn   bool
	hidden  bool
}

// NewViewport creates the inline surface for a terminal of width columns. The
// height is the first drawn frame's line count and never changes afterwards.
func NewViewport(out io.Writer, width int) *Viewport {
	return &Viewport{out: out, width: clampViewportWidth(width)}
}

// clampViewportWidth applies FR-064's min(columns, 80) rule with a small floor
// so a degenerate size still leaves a drawable frame.
func clampViewportWidth(width int) int {
	if width <= 0 {
		return 80
	}
	if width > 80 {
		return 80
	}
	if width < 8 {
		return 8
	}
	return width
}

// Width is the current box width.
func (v *Viewport) Width() int { return v.width }

// Height is the fixed number of rows in the current frame, zero before the
// first draw.
func (v *Viewport) Height() int { return v.height }

// Hidden reports whether the `?1049h` latch has the box hidden.
func (v *Viewport) Hidden() bool { return v.hidden }

// SetWidth follows a terminal resize. The width reflows; the height does not
// (FR-072, AC-055).
func (v *Viewport) SetWidth(width int) {
	v.width = clampViewportWidth(width)
}

// Start anchors the viewport. An answered DSR needs no output; a silent one
// writes the no-anchor fallback so the box starts on a fresh line (AC-055).
func (v *Viewport) Start(a Anchor) {
	if v.started {
		return
	}
	v.started = true
	if seq := a.BeginSequence(); seq != "" {
		_, _ = io.WriteString(v.out, seq)
	}
}

// Draw repaints the box. The cursor ends at column zero of the last row, which
// is the position Insert and Teardown erase from.
func (v *Viewport) Draw(lines []string) {
	if !v.started || v.hidden {
		return
	}
	v.erase()
	v.writeBox(lines)
}

// Insert makes line permanent scrollback above the box and leaves the box
// undrawn; the caller repaints with Draw so the box re-anchors below the new
// line. If no box is on screen the line is written at the current cursor.
func (v *Viewport) Insert(line string) {
	if !v.started || v.hidden {
		return
	}
	v.erase()
	_, _ = io.WriteString(v.out, line+"\n")
}

// Hide clears the box for a child that owns an alternate screen (`?1049h`).
func (v *Viewport) Hide() {
	if !v.started || v.hidden {
		return
	}
	v.erase()
	v.hidden = true
}

// Show redraws the box after the child leaves its alternate screen
// (`?1049l`). The cursor is back where Hide erased the box.
func (v *Viewport) Show(lines []string) {
	if !v.hidden {
		return
	}
	v.hidden = false
	v.writeBox(lines)
}

// Teardown clears the box and stops all further drawing. It is idempotent so
// every exit path can call it.
func (v *Viewport) Teardown() {
	if !v.started {
		return
	}
	v.erase()
	v.started = false
}

// erase unwinds the box from the cursor on its last row to the first row and
// clears the area the box occupied.
func (v *Viewport) erase() {
	if !v.drawn {
		return
	}
	_, _ = io.WriteString(v.out, term.MoveUp(v.height-1)+"\r"+clearDown)
	v.drawn = false
}

// writeBox paints lines top to bottom and parks the cursor at the start of the
// last one.
func (v *Viewport) writeBox(lines []string) {
	v.height = len(lines)
	if len(lines) == 0 {
		return
	}
	_, _ = io.WriteString(v.out, strings.Join(lines, "\n")+"\r")
	v.drawn = true
}

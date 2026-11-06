package tui

import (
	"io"
	"strings"
	"sync"

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
	mu sync.Mutex

	out    io.Writer
	width  int
	height int
	// started is set by Start, drawn tracks whether the box is currently on
	// screen, hidden is the `?1049h/l` latch's state, cursorHidden records
	// that Start hid the cursor and Teardown must show it again. The mutex
	// keeps a deferred Teardown safe against a concurrent render or latch
	// redraw.
	started      bool
	drawn        bool
	hidden       bool
	cursorHidden bool
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
func (v *Viewport) Width() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.width
}

// Height is the fixed number of rows in the current frame, zero before the
// first draw.
func (v *Viewport) Height() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.height
}

// Hidden reports whether the `?1049h` latch has the box hidden.
func (v *Viewport) Hidden() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.hidden
}

// SetWidth follows a terminal resize. The width reflows; the height does not
// (FR-072, AC-055).
func (v *Viewport) SetWidth(width int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.width = clampViewportWidth(width)
}

// Start anchors the viewport. The cursor is hidden first so it never blinks
// inside the box (BF-45). An answered DSR needs nothing more; a silent one
// writes the no-anchor fallback so the box starts on a fresh line (AC-055).
func (v *Viewport) Start(a Anchor) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.started {
		return
	}
	v.started = true
	v.cursorHidden = true
	_, _ = io.WriteString(v.out, term.CursorHide+a.BeginSequence())
}

// Draw repaints the box. The cursor ends at column zero of the last row, which
// is the position Insert and Teardown erase from.
func (v *Viewport) Draw(lines []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
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
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started || v.hidden {
		return
	}
	v.erase()
	_, _ = io.WriteString(v.out, line+"\n")
}

// Hide clears the box for a child that owns an alternate screen (`?1049h`).
func (v *Viewport) Hide() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started || v.hidden {
		return
	}
	v.erase()
	v.hidden = true
}

// Show redraws the box after the child leaves its alternate screen
// (`?1049l`). The cursor is back where Hide erased the box.
func (v *Viewport) Show(lines []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started || !v.hidden {
		return
	}
	v.hidden = false
	v.writeBox(lines)
}

// Teardown shows the cursor again, clears the box and stops all further
// drawing. The erase stays the last write of a teardown, the contract the
// progress tests pin on the stream. It is idempotent so every exit path can
// call it.
func (v *Viewport) Teardown() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started {
		return
	}
	v.showCursor()
	v.erase()
	v.started = false
}

// showCursor undoes Start's cursor hide once. It is a no-op when Start did not
// hide the cursor or when it was already shown.
func (v *Viewport) showCursor() {
	if !v.cursorHidden {
		return
	}
	_, _ = io.WriteString(v.out, term.CursorShow)
	v.cursorHidden = false
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

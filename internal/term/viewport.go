package term

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"
)

// Viewport is the inline bottom-anchored progress box (#50 §7): no alternate
// screen, fixed height, completed lines inserted into ordinary scrollback, and
// re-anchoring after a child writes. It keeps ISIG (cbreak, not raw) and never
// reads keys itself.
type Viewport struct {
	mu      sync.Mutex
	out     io.Writer
	W, H    int
	color   bool
	started bool
	drawn   bool
	hidden  bool
	dsr     DSRResult
}

// NewViewport creates an H-line box, capped at 80 columns.
func NewViewport(out io.Writer, w, h int, color bool) *Viewport {
	return &Viewport{out: out, W: clampWidth(w), H: h, color: color}
}

func clampWidth(w int) int {
	if w > 80 {
		return 80
	}
	if w < 20 {
		return 20
	}
	return w
}

// Width is the (capped) box width.
func (v *Viewport) Width() int { return v.W }

// SetWidth follows the terminal width on the next draw, height never changes.
func (v *Viewport) SetWidth(w int) { v.W = clampWidth(w) }

// Start optionally runs the DSR handshake, then anchors. On DSR failure (the
// common case in tests and many multiplexers) it writes "\r\n" so the box
// starts on a fresh line — the no-anchor strategy, which is sufficient.
func (v *Viewport) Start(in *os.File, dsr bool, timeout time.Duration) DSRResult {
	v.mu.Lock()
	defer v.mu.Unlock()
	if dsr {
		v.dsr = QueryCursor(in, os.Stdout, timeout)
	} else {
		v.dsr = DSRResult{TimedOut: true}
	}
	if !v.dsr.OK {
		_, _ = io.WriteString(v.out, "\r\n")
	}
	v.started = true
	_, _ = io.WriteString(v.out, CursorHide)
	return v.dsr
}

// Draw repaints the box in place. The cursor ends at column 0 of its last line.
func (v *Viewport) Draw(b *Buffer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hidden || !v.started {
		return
	}
	v.eraseLocked()
	v.writeBoxLocked(b)
	v.drawn = true
}

func (v *Viewport) eraseLocked() {
	if !v.drawn {
		return
	}
	_, _ = io.WriteString(v.out, MoveUp(v.H-1)+"\r"+ClearDown)
}

func (v *Viewport) writeBoxLocked(b *Buffer) {
	for y := 0; y < b.H; y++ {
		v.writeStyledRow(b, y)
		if y < b.H-1 {
			_, _ = io.WriteString(v.out, "\r\n")
		} else {
			_, _ = io.WriteString(v.out, "\r")
		}
	}
}

func (v *Viewport) writeStyledRow(b *Buffer, y int) {
	last := b.W - 1
	for last >= 0 {
		c := b.Get(last, y)
		if c.Cont || (c.R != ' ' && c.R != 0) || c.S != ZeroStyle {
			break
		}
		last--
	}
	if last < 0 {
		return
	}
	var cur Style
	styled := false
	for x := 0; x <= last; x++ {
		c := b.Get(x, y)
		if c.Cont {
			continue
		}
		if v.color && (!styled || c.S != cur) {
			_, _ = io.WriteString(v.out, sgr(c.S))
			cur, styled = c.S, true
		}
		r := c.R
		if r == 0 {
			r = ' '
		}
		_, _ = io.WriteString(v.out, string(r))
	}
	if v.color && styled {
		_, _ = io.WriteString(v.out, "\x1b[0m")
	}
}

// Insert pushes a completed line above the box (nala's insert_before). The
// line becomes permanent scrollback; the caller repaints with DrawAfterInsert.
func (v *Viewport) Insert(line string, st Style) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hidden || !v.started {
		return
	}
	v.eraseLocked()
	_, _ = io.WriteString(v.out, line+"\r\n")
	// The next DrawAfterInsert anchors at the current line; if this was the
	// last terminal row the terminal scrolls, which is the insertion we want.
	v.drawn = false
}

// DrawAfterInsert repaints after an Insert, re-anchoring at the new position.
func (v *Viewport) DrawAfterInsert(b *Buffer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hidden || !v.started {
		return
	}
	v.writeBoxLocked(b)
	v.drawn = true
}

// Hide clears the box and marks it hidden (the child owns a full-screen view).
func (v *Viewport) Hide() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hidden {
		return
	}
	v.eraseLocked()
	v.hidden = true
	v.drawn = false
}

// Show re-anchors and repaints after a full-screen child leaves.
func (v *Viewport) Show(b *Buffer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.hidden {
		return
	}
	v.hidden = false
	_, _ = io.WriteString(v.out, CursorHide)
	v.writeBoxLocked(b)
	v.drawn = true
}

// Teardown clears the box, shows the cursor and leaves a clean line.
func (v *Viewport) Teardown() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started {
		return
	}
	if !v.hidden {
		v.eraseLocked()
	}
	_, _ = io.WriteString(v.out, CursorShow+"\r")
	v.started = false
	v.drawn = false
}

// RenderLine is a small helper for the prototype.
func RenderLine(b *Buffer, y int, text string, s Style) {
	b.DrawText(0, y, text, s)
}

var _ = bytes.MinRead

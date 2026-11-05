package tui

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/soubarnak/xbpretty/internal/term"
)

// Style, Color and Mods are aliases so widget code reads naturally without a
// second styling vocabulary.
type Style = term.Style

// ZeroStyle is the unstyled cell.
var ZeroStyle = term.ZeroStyle

// Cell is one terminal cell. A width-2 rune occupies two cells: the first has
// the rune, the second has Cont set and is skipped on flush.
type Cell struct {
	R    rune
	S    Style
	Cont bool
}

// Buffer is a flat cell grid with a dirty-diff flush (ADR-0017 §5).
type Buffer struct {
	W, H  int
	Cells []Cell
}

// NewBuffer allocates a blank buffer.
func NewBuffer(w, h int) *Buffer {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	b := &Buffer{W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range b.Cells {
		b.Cells[i] = Cell{R: ' '}
	}
	return b
}

func (b *Buffer) in(x, y int) bool { return x >= 0 && y >= 0 && x < b.W && y < b.H }

func (b *Buffer) idx(x, y int) int { return y*b.W + x }

// Set paints one cell, clipping out of bounds.
func (b *Buffer) Set(x, y int, r rune, s Style) {
	if !b.in(x, y) {
		return
	}
	b.Cells[b.idx(x, y)] = Cell{R: r, S: s}
}

// SetCont marks a continuation cell.
func (b *Buffer) SetCont(x, y int, s Style) {
	if !b.in(x, y) {
		return
	}
	b.Cells[b.idx(x, y)] = Cell{R: 0, S: s, Cont: true}
}

// Get returns the cell at x,y or a blank.
func (b *Buffer) Get(x, y int) Cell {
	if !b.in(x, y) {
		return Cell{R: ' '}
	}
	return b.Cells[b.idx(x, y)]
}

// Fill paints a rectangle with spaces in style s.
func (b *Buffer) Fill(x, y, w, h int, s Style) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			b.Set(col, row, ' ', s)
		}
	}
}

// DrawText paints s starting at x,y. Width-aware: wide runes take two cells.
func (b *Buffer) DrawText(x, y int, s string, st Style) int {
	col := x
	for _, r := range s {
		w := term.RuneWidth(r)
		if w == 0 {
			continue
		}
		b.Set(col, y, r, st)
		if w == 2 {
			b.SetCont(col+1, y, st)
		}
		col += w
	}
	return col
}

// DrawTextLimit draws at most max cells, clipping (never wrapping).
func (b *Buffer) DrawTextLimit(x, y int, s string, st Style, max int) int {
	if max <= 0 {
		return x
	}
	col := x
	end := x + max
	for _, r := range s {
		w := term.RuneWidth(r)
		if w == 0 {
			continue
		}
		if col+w > end {
			break
		}
		b.Set(col, y, r, st)
		if w == 2 {
			b.SetCont(col+1, y, st)
		}
		col += w
	}
	return col
}

// DrawTextRight draws s right-aligned so that it ends at xEnd-1.
func (b *Buffer) DrawTextRight(xEnd, y int, s string, st Style) {
	b.DrawText(xEnd-term.StringWidth(s), y, s, st)
}

// DrawTextCenter centres s in [x, x+w).
func (b *Buffer) DrawTextCenter(x, y, w int, s string, st Style) {
	if term.StringWidth(s) > w {
		s = term.Truncate(s, w)
	}
	tw := term.StringWidth(s)
	start := x + (w-tw)/2
	if start < x {
		start = x
	}
	b.DrawText(start, y, s, st)
}

// Grid returns the buffer as trimmed text lines (for goldens).
func (b *Buffer) Grid() []string {
	out := make([]string, b.H)
	for y := 0; y < b.H; y++ {
		var sb bytes.Buffer
		for x := 0; x < b.W; x++ {
			c := b.Get(x, y)
			if c.Cont {
				continue
			}
			r := c.R
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		line := sb.String()
		for len(line) > 0 && line[len(line)-1] == ' ' {
			line = line[:len(line)-1]
		}
		out[y] = line
	}
	return out
}

// Text joins the grid with newlines.
func (b *Buffer) Text() string {
	var out bytes.Buffer
	for i, l := range b.Grid() {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(l)
	}
	return out.String()
}

// Flush diffs b against prev and returns the minimal byte stream. color=false
// emits no SGR at all but keeps every glyph and cursor move (FR-072).
func (b *Buffer) Flush(prev *Buffer, color bool) []byte {
	var out bytes.Buffer
	full := prev == nil || prev.W != b.W || prev.H != b.H
	if full {
		out.WriteString(term.ClearAll)
	}
	var last Style
	styled := false
	emitStyle := func(s Style) {
		if !color {
			return
		}
		if styled && s == last {
			return
		}
		out.WriteString(sgr(s))
		last = s
		styled = true
	}
	for y := 0; y < b.H; y++ {
		rowEnd := b.W
		if full {
			for rowEnd > 0 {
				c := b.Get(rowEnd-1, y)
				if c.R != ' ' && c.R != 0 || c.S != ZeroStyle {
					break
				}
				rowEnd--
			}
		}
		rowChanged := full
		if !rowChanged {
			for x := 0; x < b.W; x++ {
				if b.Get(x, y) != prev.Get(x, y) {
					rowChanged = true
					break
				}
			}
		}
		if !rowChanged {
			continue
		}
		x := 0
		for x < rowEnd {
			if !full && b.Get(x, y) == prev.Get(x, y) {
				x++
				continue
			}
			out.WriteString(term.MoveTo(y+1, x+1))
			for x < rowEnd {
				if !full && b.Get(x, y) == prev.Get(x, y) {
					break
				}
				c := b.Get(x, y)
				if c.Cont {
					x++
					continue
				}
				emitStyle(c.S)
				r := c.R
				if r == 0 {
					r = ' '
				}
				out.WriteRune(r)
				x++
			}
		}
	}
	if color && styled {
		out.WriteString("\x1b[0m")
	}
	return out.Bytes()
}

func sgr(s Style) string {
	// Always emit a full state: reset first, then every attribute. Without
	// the reset a default-fg Highlight (e.g. the title) would inherit the
	// previous cell's bright-green foreground.
	params := []string{"0"}
	for _, m := range []struct {
		bit term.Mods
		n   string
	}{
		{term.Bold, "1"}, {term.Dim, "2"}, {term.Italic, "3"}, {term.Underline, "4"},
		{term.Blink, "5"}, {term.Reverse, "7"}, {term.Hidden, "8"}, {term.Strike, "9"},
	} {
		if s.Mods&m.bit != 0 {
			params = append(params, m.n)
		}
	}
	params = append(params, colorParams(s.FG, true)...)
	params = append(params, colorParams(s.BG, false)...)
	return "\x1b[" + join(params, ";") + "m"
}

func colorParams(c term.Color, fg bool) []string {
	switch c.Kind {
	case term.ColorANSI:
		base := 30
		if !fg {
			base = 40
		}
		if c.N >= 8 {
			return []string{strconv.Itoa(base + 60 + int(c.N) - 8)}
		}
		return []string{strconv.Itoa(base + int(c.N))}
	case term.Color256:
		prefix := "38;5;"
		if !fg {
			prefix = "48;5;"
		}
		return []string{prefix + strconv.Itoa(int(c.N))}
	case term.ColorRGB:
		prefix := "38;2;"
		if !fg {
			prefix = "48;2;"
		}
		return []string{prefix + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))}
	}
	return nil
}

func join(ss []string, sep string) string {
	var b bytes.Buffer
	for i, s := range ss {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s)
	}
	return b.String()
}

// DebugFrame renders the grid with its SGR dump for human inspection.
func DebugFrame(b *Buffer, color bool) string {
	return fmt.Sprintf("%s\n%q", b.Text(), b.Flush(nil, color))
}

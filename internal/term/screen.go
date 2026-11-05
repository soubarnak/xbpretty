package term

import (
	"bytes"
	"fmt"
	"strconv"
)

// Mods is the bitmask of SGR modifiers nala's theme can request.
type Mods uint16

const (
	Bold Mods = 1 << iota
	Dim
	Italic
	Underline
	Blink
	Reverse
	Hidden
	Strike
)

// ColorKind distinguishes the SGR colour spaces #45/#50 support.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorANSI
	Color256
	ColorRGB
)

// Color is one terminal colour.
type Color struct {
	Kind    ColorKind
	N       uint8
	R, G, B uint8
}

// ANSI returns an ANSI-16 colour.
func ANSI(n uint8) Color { return Color{Kind: ColorANSI, N: n} }

// Default is the terminal's default colour.
var Default = Color{}

// Style is a cell's complete appearance.
type Style struct {
	FG, BG Color
	Mods   Mods
}

// ZeroStyle is the unstyled cell.
var ZeroStyle = Style{}

// Cell is one terminal cell. A width-2 rune occupies two cells: the first has
// the rune, the second has Cont set and is skipped on flush (§8.4).
type Cell struct {
	R    rune
	S    Style
	Cont bool
}

// Buffer is a flat cell grid.
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
		w := RuneWidth(r)
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
		w := RuneWidth(r)
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
	b.DrawText(xEnd-StringWidth(s), y, s, st)
}

// DrawTextCenter centres s in [x, x+w).
func (b *Buffer) DrawTextCenter(x, y, w int, s string, st Style) {
	if StringWidth(s) > w {
		s = Truncate(s, w)
	}
	tw := StringWidth(s)
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
	out := ""
	for i, l := range b.Grid() {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// Flush diffs cur against prev and writes the minimal-ish byte stream. It
// returns the bytes written so a prototype can capture the frame's SGR.
// color=false emits no SGR at all, keeping every glyph and cursor move.
func (b *Buffer) Flush(prev *Buffer, color bool) []byte {
	var out bytes.Buffer
	full := prev == nil || prev.W != b.W || prev.H != b.H
	if full {
		out.WriteString(ClearAll)
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
			// Start of a changed run.
			out.WriteString(MoveTo(y+1, x+1))
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
	var params []string
	for _, m := range []struct {
		bit Mods
		n   string
	}{{Bold, "1"}, {Dim, "2"}, {Italic, "3"}, {Underline, "4"}, {Blink, "5"}, {Reverse, "7"}, {Hidden, "8"}, {Strike, "9"}} {
		if s.Mods&m.bit != 0 {
			params = append(params, m.n)
		}
	}
	params = append(params, colorParams(s.FG, true)...)
	params = append(params, colorParams(s.BG, false)...)
	if len(params) == 0 {
		return "\x1b[0m"
	}
	return "\x1b[" + join(params, ";") + "m"
}

func colorParams(c Color, fg bool) []string {
	switch c.Kind {
	case ColorANSI:
		base := 30
		if !fg {
			base = 40
		}
		if c.N >= 8 {
			return []string{itoa(base + 60 + int(c.N) - 8)}
		}
		return []string{itoa(base + int(c.N))}
	case Color256:
		prefix := "38;5;"
		if !fg {
			prefix = "48;5;"
		}
		return []string{prefix + itoa(int(c.N))}
	case ColorRGB:
		prefix := "38;2;"
		if !fg {
			prefix = "48;2;"
		}
		return []string{prefix + itoa(int(c.R)) + ";" + itoa(int(c.G)) + ";" + itoa(int(c.B))}
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

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

// DebugFrame renders the grid with an optional SGR dump for human inspection.
func DebugFrame(b *Buffer, color bool) string {
	var out bytes.Buffer
	prev := (*Buffer)(nil)
	out.Write(b.Flush(prev, color))
	return fmt.Sprintf("%s\n%s", b.Text(), out.String())
}

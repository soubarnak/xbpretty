package main

import "github.com/soubarnak/xbpretty/internal/term"

// Block draws nala's two frame styles: thick for summary/history/info, rounded
// for the fetch picker. Borders are drawn even under --color never.
type Block struct {
	X, Y, W, H int
	Rounded    bool
	Style      term.Style
}

func (b *Block) fill(buf *term.Buffer, t Theme) {
	if b.W < 2 || b.H < 2 {
		return
	}
	tl, tr, bl, br, hz, vt := "┏", "┓", "┗", "┛", "━", "┃"
	if b.Rounded {
		tl, tr, bl, br, hz, vt = "╭", "╮", "╰", "╯", "─", "│"
	}
	_ = t
	for x := b.X + 1; x < b.X+b.W-1; x++ {
		buf.Set(x, b.Y, []rune(hz)[0], b.Style)
		buf.Set(x, b.Y+b.H-1, []rune(hz)[0], b.Style)
	}
	for y := b.Y + 1; y < b.Y+b.H-1; y++ {
		buf.Set(b.X, y, []rune(vt)[0], b.Style)
		buf.Set(b.X+b.W-1, y, []rune(vt)[0], b.Style)
	}
	buf.Set(b.X, b.Y, []rune(tl)[0], b.Style)
	buf.Set(b.X+b.W-1, b.Y, []rune(tr)[0], b.Style)
	buf.Set(b.X, b.Y+b.H-1, []rune(bl)[0], b.Style)
	buf.Set(b.X+b.W-1, b.Y+b.H-1, []rune(br)[0], b.Style)
}

// title paints "  <title>  " centred in the top border, with the border glyph
// hidden underneath the title.
func (b *Block) title(buf *term.Buffer, title string, st term.Style) {
	if title == "" || b.W < 4 {
		return
	}
	s := "  " + title + "  "
	tw := term.StringWidth(s)
	if tw > b.W-2 {
		s = term.Truncate(title, b.W-4)
		tw = term.StringWidth(s)
	}
	start := b.X + 1 + (b.W-2-tw)/2
	// Blank the border cells under the title, then paint it.
	for x := start; x < start+tw && x < b.X+b.W-1; x++ {
		buf.Set(x, b.Y, ' ', st)
	}
	buf.DrawTextLimit(start, b.Y, s, st, b.W-2)
}

// bodyRect is the summary/history body: one column of horizontal padding.
func bodyRect(w, h int) (x, y, bw, bh int) {
	return 2, 1, w - 4, h - 2
}

// centerText paints a single centred line.
func centerText(buf *term.Buffer, y int, s string, st term.Style) {
	buf.DrawTextCenter(0, y, buf.W, s, st)
}

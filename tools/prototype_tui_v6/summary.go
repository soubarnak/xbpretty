package main

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/term"
)

// Span is a styled fragment inside a table cell (version diff components).
type Span struct {
	Text  string
	Style term.Style
}

// PkgRow is one package row in a tab.
type PkgRow struct {
	Name     string
	Old      string
	New      string
	Size     string
	Reason   string
	NewSpans []Span
}

// Tab is one operation group.
type Tab struct {
	Label string
	Rows  []PkgRow
}

// SummaryModel is the transaction summary / history detail state.
type SummaryModel struct {
	Title     string
	Tabs      []Tab
	Active    int
	Sel       int
	Download  string
	DiskLabel string
	Disk      string
	History   bool
	Help      []string
}

func (m *SummaryModel) cur() *Tab {
	if m.Active < 0 || m.Active >= len(m.Tabs) {
		return nil
	}
	return &m.Tabs[m.Active]
}

func (m *SummaryModel) moveRow(d int) {
	t := m.cur()
	if t == nil || len(t.Rows) == 0 {
		return
	}
	m.Sel += d
	if m.Sel < 0 {
		m.Sel = 0
	}
	if m.Sel > len(t.Rows)-1 {
		m.Sel = len(t.Rows) - 1
	}
}

func (m *SummaryModel) moveTab(d int) {
	n := len(m.Tabs)
	if n == 0 {
		return
	}
	m.Active += d
	if m.Active < 0 {
		m.Active = 0
	}
	if m.Active > n-1 {
		m.Active = n - 1
	}
	m.Sel = 0
}

func headersFor(t Tab) []string {
	switch t.Label {
	case "Upgrade":
		return []string{"Package:", "Old Version:", "New Version:", "Size:"}
	case "Held":
		diff := false
		for _, r := range t.Rows {
			if r.Old != r.New {
				diff = true
			}
		}
		if diff {
			return []string{"Package:", "Old Version:", "New Version:", "Reason:", "Size:"}
		}
		return []string{"Package:", "Version:", "Reason:", "Size:"}
	}
	return []string{"Package:", "Version:", "Size:"}
}

func cellsFor(t Tab, r PkgRow) []string {
	switch t.Label {
	case "Upgrade":
		return []string{r.Name, r.Old, r.New, r.Size}
	case "Held":
		if r.Old != r.New {
			return []string{r.Name, r.Old, r.New, r.Reason, r.Size}
		}
		return []string{r.Name, r.Old, r.Reason, r.Size}
	}
	v := r.New
	if v == "" {
		v = r.Old
	}
	return []string{r.Name, v, r.Size}
}

func alignsFor(n int) []int {
	// 0 = left, 1 = centre, 2 = right
	switch n {
	case 4:
		return []int{0, 1, 1, 2}
	case 5:
		return []int{0, 1, 1, 1, 2}
	default:
		return []int{0, 1, 2}
	}
}

func roleFor(label string, name string) term.Style {
	switch label {
	case "Remove", "AutoRemove", "Purge", "AutoPurge":
		return Error
	case "Install", "Upgrade":
		return Secondary
	}
	return Notice
}

func spanWidth(spans []Span) int {
	w := 0
	for _, s := range spans {
		w += term.StringWidth(s.Text)
	}
	return w
}

func flatten(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// renderSummary is the operation-tabbed summary frame (#50 §2.3/§3) and the
// read-only history variant.
func renderSummary(m *SummaryModel, w, h int, t Theme) *term.Buffer {
	buf := term.NewBuffer(w, h)
	block := &Block{X: 0, Y: 0, W: w, H: h, Style: Primary}
	block.fill(buf, t)
	block.title(buf, m.Title, Highlight)
	if w < 60 || h < 18 {
		centerText(buf, h/2, TooSmall(w, h), Notice)
		return buf
	}
	bx, by, bw, bh := bodyRect(w, h)

	// Tab strip: labels, then a ━ underline exactly under the active label.
	labels := make([]string, len(m.Tabs))
	for i, tb := range m.Tabs {
		labels[i] = tb.Label
	}
	strip := strings.Join(labels, " ")
	activeStart, activeLen := 0, 0
	pos := 0
	for i, l := range labels {
		if i == m.Active {
			activeStart, activeLen = pos, term.StringWidth(l)
		}
		pos += term.StringWidth(l) + 1
	}
	shift := 0
	if activeStart+activeLen > bw {
		shift = activeStart + activeLen - bw
	}
	x := bx - shift
	pos = 0
	for i, l := range labels {
		st := Regular
		if i == m.Active {
			st = Primary
		}
		buf.DrawTextLimit(x+pos, by, l, st, bw-pos)
		pos += term.StringWidth(l) + 1
	}
	_ = strip
	underStart := bx + activeStart - shift
	if underStart < bx {
		underStart = bx
	}
	end := underStart + activeLen
	if end > bx+bw {
		end = bx + bw
	}
	for ux := underStart; ux < end; ux++ {
		buf.Set(ux, by+1, '━', Primary)
	}

	// Footer rows: operations, spacer, optional download, disk. History shows
	// operation counts only, and reserves three rows for its short help lines.
	footer := make([][2]string, 0, len(m.Tabs)+3)
	for _, tb := range m.Tabs {
		footer = append(footer, [2]string{tb.Label + ":", itoa(len(tb.Rows)) + " Pkgs"})
	}
	F := 0
	if m.History {
		F = len(m.Tabs) + 2
		if F < 3 {
			F = 3
		}
	} else {
		footer = append(footer, [2]string{"", ""})
		if m.Download != "" {
			footer = append(footer, [2]string{"Total download size:", m.Download})
		}
		footer = append(footer, [2]string{m.DiskLabel, m.Disk})
		F = len(footer)
	}
	if F < 2 {
		F = 2
	}

	A := bh - 2 - F
	if A < 4 {
		A = 4
	}
	tableY := by + 2
	tableW := bw - 3
	tableH := A

	// Table block.
	tb := &Block{X: bx, Y: tableY, W: tableW, H: tableH, Style: Primary}
	tb.fill(buf, t)

	// Gutter block with scrollbar.
	gx := bx + tableW
	gb := &Block{X: gx, Y: tableY, W: 3, H: tableH, Style: Primary}
	gb.fill(buf, t)

	tab := m.cur()
	if tab != nil {
		headers := headersFor(*tab)
		natural := make([]int, len(headers))
		for i, hd := range headers {
			natural[i] = term.StringWidth(hd)
		}
		for _, r := range tab.Rows {
			cells := cellsFor(*tab, r)
			for i, c := range cells {
				if i < len(natural) && term.StringWidth(c) > natural[i] {
					natural[i] = term.StringWidth(c)
				}
			}
		}
		T := tableW - 2
		Tp := T - 2
		cols := spaceAround(natural, Tp)
		tableInteriorX := bx + 1
		headerY := tableY + 1
		visible := tableH - 3
		if visible < 0 {
			visible = 0
		}
		scroll := 0
		if m.Sel >= visible && visible > 0 {
			scroll = m.Sel - visible + 1
		}

		// Header row.
		aligns := alignsFor(len(headers))
		for i, hd := range headers {
			cx := tableInteriorX + 2 + cols[i]
			drawAligned(buf, cx, headerY, natural[i], hd, Regular, aligns[i])
		}
		// Data rows.
		for i := 0; i < visible && scroll+i < len(tab.Rows); i++ {
			r := tab.Rows[scroll+i]
			y := headerY + 1 + i
			selected := scroll+i == m.Sel
			for x := tableInteriorX; x < tableInteriorX+T; x++ {
				buf.Set(x, y, ' ', Regular)
			}
			cells := cellsFor(*tab, r)
			for ci := range cells {
				cx := tableInteriorX + 2 + cols[ci]
				st := Regular
				if selected {
					st = Primary
				} else if ci == 0 {
					st = roleFor(tab.Label, r.Name)
				}
				if !selected && tab.Label == "Upgrade" && ci == 2 && len(r.NewSpans) > 0 {
					drawSpansAligned(buf, cx, y, natural[ci], r.NewSpans, aligns[ci])
					continue
				}
				drawAligned(buf, cx, y, natural[ci], cells[ci], st, aligns[ci])
			}
			if selected {
				buf.Set(tableInteriorX, y, '❯', Primary)
			}
		}
		// Scrollbar thumb in the gutter's single interior column.
		interior := tableH - 2
		if interior > 0 {
			// #50's golden frame shows a one-cell thumb when every row fits;
			// only a genuinely scrollable table grows it.
			thumbH := 1
			thumbY := tableY + 1
			if len(tab.Rows) > visible && visible > 0 {
				thumbH = interior * visible / len(tab.Rows)
				if thumbH < 1 {
					thumbH = 1
				}
				maxPos := interior - thumbH
				if maxPos > 0 && len(tab.Rows)-visible > 0 {
					thumbY = tableY + 1 + m.Sel*maxPos/(len(tab.Rows)-visible)
				}
			}
			for yy := tableY + 1; yy < tableY+tableH-1; yy++ {
				buf.Set(gx+1, yy, ' ', Regular)
			}
			for yy := thumbY; yy < thumbY+thumbH && yy < tableY+tableH-1; yy++ {
				buf.Set(gx+1, yy, '█', Primary)
			}
		}
	}

	// Footer left block and help block.
	fy := tableY + tableH
	L, S := 0, 0
	for _, row := range footer {
		if term.StringWidth(row[0]) > L {
			L = term.StringWidth(row[0])
		}
		if term.StringWidth(row[1]) > S {
			S = term.StringWidth(row[1])
		}
	}
	for i, row := range footer {
		y := fy + i
		if y >= by+bh {
			break
		}
		if row[0] != "" {
			buf.DrawText(bx, y, row[0], Regular)
		}
		if row[1] != "" {
			buf.DrawTextRight(bx+L+S, y, row[1], Regular)
		}
	}
	helpX := bx + L + S + 1
	helpW := bw - (L + S + 1)
	if helpW >= 12 {
		var lines []string
		for _, line := range m.Help {
			lines = append(lines, wrapText(line, helpW)...)
		}
		for i, line := range lines {
			if i >= F {
				break
			}
			buf.DrawTextCenter(helpX, fy+i, helpW, line, Secondary)
		}
	}
	return buf
}

// spaceAround is #50 §3.2's deterministic replacement for Flex::SpaceAround.
func spaceAround(natural []int, T int) []int {
	n := len(natural)
	out := make([]int, n)
	C := 0
	for _, w := range natural {
		C += w
	}
	if C > T {
		return out
	}
	if n == 1 {
		return out
	}
	R := T - C
	g := R / n
	h := R % n
	out[0] = g / 2
	for i := 1; i < n; i++ {
		extra := 0
		if i <= h {
			extra = 1
		}
		out[i] = out[i-1] + natural[i-1] + g + extra
	}
	return out
}

func drawAligned(buf *term.Buffer, x, y, w int, s string, st term.Style, align int) {
	s = term.Truncate(s, w)
	sw := term.StringWidth(s)
	switch align {
	case 1:
		buf.DrawText(x+(w-sw)/2, y, s, st)
	case 2:
		buf.DrawText(x+w-sw, y, s, st)
	default:
		buf.DrawText(x, y, s, st)
	}
}

func drawSpansAligned(buf *term.Buffer, x, y, w int, spans []Span, align int) {
	total := spanWidth(spans)
	start := x
	if align == 1 {
		start = x + (w-total)/2
	} else if align == 2 {
		start = x + w - total
	}
	col := start
	for _, sp := range spans {
		col = buf.DrawTextLimit(col, y, sp.Text, sp.Style, x+w-col)
	}
}

// wrapText word-wraps at spaces and hard-splits a single overlong token
// (#45 §3.2's rule), never truncating with an ellipsis.
func wrapText(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	if term.StringWidth(s) <= w {
		return []string{s}
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		for term.StringWidth(word) > w {
			if line != "" {
				out = append(out, line)
				line = ""
			}
			cut := term.Truncate(word, w)
			out = append(out, cut)
			word = word[len(cut):]
		}
		if line == "" {
			line = word
			continue
		}
		if term.StringWidth(line)+1+term.StringWidth(word) <= w {
			line += " " + word
		} else {
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

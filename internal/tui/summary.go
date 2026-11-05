package tui

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/term"
)

// Span is a styled fragment inside a table cell (the version diff).
type Span struct {
	Text string
	Role Role
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

// SummaryModel is the transaction summary (S1) and, read-only, the history
// detail (#77) state. The interaction policy lives in the cli; this type only
// carries the data the frame renders.
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

// MoveRow moves the selection by d, clamped to the active tab. #76 owns the
// key bindings; the model exposes the clamp so the interaction layer cannot
// run off the end.
func (m *SummaryModel) MoveRow(d int) {
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

// RowCount is the active tab's row count (0 when there is no tab).
func (m *SummaryModel) RowCount() int {
	t := m.cur()
	if t == nil {
		return 0
	}
	return len(t.Rows)
}

// SelectedRow returns the active tab's selected row. It is the seam the
// package-info view (#76) and the history detail (#77) consume.
func (m *SummaryModel) SelectedRow() (PkgRow, bool) {
	t := m.cur()
	if t == nil || m.Sel < 0 || m.Sel >= len(t.Rows) {
		return PkgRow{}, false
	}
	return t.Rows[m.Sel], true
}

// ActiveTab returns the active tab. The package-info view needs its label.
func (m *SummaryModel) ActiveTab() (Tab, bool) {
	t := m.cur()
	if t == nil {
		return Tab{}, false
	}
	return *t, true
}

// MoveTab moves the active tab by d, clamped. The selection resets only when
// the active tab actually changes, so a clamped move is a no-op (and paints
// nothing).
func (m *SummaryModel) MoveTab(d int) {
	n := len(m.Tabs)
	if n == 0 {
		return
	}
	next := m.Active + d
	if next < 0 {
		next = 0
	}
	if next > n-1 {
		next = n - 1
	}
	if next == m.Active {
		return
	}
	m.Active = next
	m.Sel = 0
}

// SetSelection clamps the selection to the active tab.
func (m *SummaryModel) SetSelection(row int) {
	t := m.cur()
	if t == nil || len(t.Rows) == 0 {
		m.Sel = 0
		return
	}
	if row < 0 {
		row = 0
	}
	if row > len(t.Rows)-1 {
		row = len(t.Rows) - 1
	}
	m.Sel = row
}

// tabHasVersionDiff reports whether a tab's rows include a changed version.
// Upgrade and Downgrade always have the two-version shape (a history entry can
// record a downgrade); Held only when at least one row differs, so every cell
// in the section keeps the same column count.
func tabHasVersionDiff(t Tab) bool {
	if t.Label == "Upgrade" || t.Label == "Downgrade" {
		return true
	}
	if t.Label != "Held" {
		return false
	}
	for _, r := range t.Rows {
		if r.Old != r.New {
			return true
		}
	}
	return false
}

func headersFor(t Tab, hasDiff bool) []string {
	switch t.Label {
	case "Upgrade", "Downgrade":
		return []string{"Package:", "Old Version:", "New Version:", "Size:"}
	case "Held":
		if hasDiff {
			return []string{"Package:", "Old Version:", "New Version:", "Reason:", "Size:"}
		}
		return []string{"Package:", "Version:", "Reason:", "Size:"}
	}
	return []string{"Package:", "Version:", "Size:"}
}

// cellsFor always returns the section's header shape: a mixed Held section
// lists the same five (or four) columns on every row.
func cellsFor(t Tab, r PkgRow, hasDiff bool) []string {
	switch t.Label {
	case "Upgrade", "Downgrade":
		return []string{r.Name, r.Old, r.New, r.Size}
	case "Held":
		if hasDiff {
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

// alignsFor returns 0=left, 1=centre, 2=right for each column.
func alignsFor(n int) []int {
	switch n {
	case 4:
		return []int{0, 1, 1, 2}
	case 5:
		return []int{0, 1, 1, 1, 2}
	default:
		return []int{0, 1, 2}
	}
}

func roleFor(label string) Role {
	switch label {
	case "Remove", "AutoRemove", "Purge", "AutoPurge":
		return RoleError
	case "Install", "Upgrade":
		return RoleSecondary
	}
	// ReInstall, Configure and Held names are Notice (ADR-0012).
	return RoleNotice
}

// versionSpans implements FR-063's version diff as cell styles. The whole row
// is painted Primary when selected, so the spans only matter otherwise.
func versionSpans(oldV, newV string) []Span {
	decompose := func(v string) (up, rev string) {
		if i := strings.LastIndex(v, "_"); i >= 0 {
			return v[:i], v[i+1:]
		}
		return v, ""
	}
	oldUp, oldRev := decompose(oldV)
	newUp, newRev := decompose(newV)
	if oldUp == newUp {
		if oldRev == newRev {
			return []Span{{Text: newV, Role: RoleRegular}}
		}
		prefix := newUp
		if newRev != "" {
			prefix += "_"
		}
		if prefix == "" {
			return []Span{{Text: newRev, Role: RoleNotice}}
		}
		return []Span{{Text: prefix, Role: RoleRegular}, {Text: newRev, Role: RoleNotice}}
	}
	op := strings.Split(oldUp, ".")
	np := strings.Split(newUp, ".")
	k := 0
	for k < len(op) && k < len(np) && op[k] == np[k] {
		k++
	}
	var spans []Span
	if k > 0 {
		spans = append(spans, Span{Text: strings.Join(np[:k], ".") + ".", Role: RoleRegular})
	}
	for j := k; j < len(np); j++ {
		text := np[j]
		if j == len(np)-1 && newRev != "" {
			text += "_" + newRev
		}
		spans = append(spans, Span{Text: text, Role: RoleNotice})
		if j < len(np)-1 {
			spans = append(spans, Span{Text: ".", Role: RoleRegular})
		}
	}
	if len(spans) == 0 {
		return []Span{{Text: newV, Role: RoleRegular}}
	}
	return spans
}

// VersionSpans exposes the diff algorithm the model builders use.
func VersionSpans(oldV, newV string) []Span { return versionSpans(oldV, newV) }

func spanWidth(spans []Span) int {
	w := 0
	for _, s := range spans {
		w += term.StringWidth(s.Text)
	}
	return w
}

// block draws nala's thick summary frame; the rounded picker frame (#78) is a
// separate widget.
type block struct{ X, Y, W, H int }

func (b *block) fill(buf *Buffer, st Style) {
	if b.W < 2 || b.H < 2 {
		return
	}
	for x := b.X + 1; x < b.X+b.W-1; x++ {
		buf.Set(x, b.Y, '━', st)
		buf.Set(x, b.Y+b.H-1, '━', st)
	}
	for y := b.Y + 1; y < b.Y+b.H-1; y++ {
		buf.Set(b.X, y, '┃', st)
		buf.Set(b.X+b.W-1, y, '┃', st)
	}
	buf.Set(b.X, b.Y, '┏', st)
	buf.Set(b.X+b.W-1, b.Y, '┓', st)
	buf.Set(b.X, b.Y+b.H-1, '┗', st)
	buf.Set(b.X+b.W-1, b.Y+b.H-1, '┛', st)
}

// title paints "  <title>  " centred in the top border.
func (b *block) title(buf *Buffer, title string, st Style) {
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
	for x := start; x < start+tw && x < b.X+b.W-1; x++ {
		buf.Set(x, b.Y, ' ', st)
	}
	buf.DrawTextLimit(start, b.Y, s, st, b.W-2)
}

// RenderSummary draws the operation-tabbed transaction summary (S1) or the
// read-only history detail (S2). It is the frame only: the caller chooses the
// active tab and selection, and owns the key loop.
func RenderSummary(m *SummaryModel, w, h int, t Theme) *Buffer {
	buf := NewBuffer(w, h)
	// Every body cell starts at Regular so a custom background covers the
	// interior (FR-072).
	buf.Fill(0, 0, w, h, t.Regular)
	border := &block{X: 0, Y: 0, W: w, H: h}
	border.fill(buf, t.Primary)
	border.title(buf, m.Title, t.style(RoleHighlight))
	if w < 60 || h < 18 {
		centerText(buf, h/2, TooSmall(w, h), t.style(RoleNotice))
		return buf
	}

	bx, by, bw, bh := bodyRect(w, h)

	// Tab strip and the colour-independent active underline.
	labels := make([]string, len(m.Tabs))
	for i, tb := range m.Tabs {
		labels[i] = tb.Label
	}
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
		st := t.style(RoleRegular)
		if i == m.Active {
			st = t.style(RolePrimary)
		}
		drawX := x + pos
		pos += term.StringWidth(l) + 1
		// Shift-to-active may move the strip's start outside the body. Clip
		// the label at the body's left edge instead of painting over the
		// border and the inner padding (FR-068/FR-071).
		if drawX >= bx+bw {
			continue
		}
		if drawX < bx {
			skip := bx - drawX
			if skip >= term.StringWidth(l) {
				continue
			}
			l = dropCells(l, skip)
			drawX = bx
		}
		buf.DrawTextLimit(drawX, by, l, st, bx+bw-drawX)
	}
	underStart := bx + activeStart - shift
	if underStart < bx {
		underStart = bx
	}
	end := underStart + activeLen
	if end > bx+bw {
		end = bx + bw
	}
	for ux := underStart; ux < end; ux++ {
		buf.Set(ux, by+1, '━', t.style(RolePrimary))
	}

	// Footer rows: operation counts, then (S1 only) the size block.
	footer := make([][2]string, 0, len(m.Tabs)+3)
	for _, tb := range m.Tabs {
		footer = append(footer, [2]string{tb.Label + ":", itoa(len(tb.Rows)) + " Pkgs"})
	}
	footerRows := 0
	if m.History {
		footerRows = len(m.Tabs) + 2
		if footerRows < 3 {
			footerRows = 3
		}
	} else {
		footer = append(footer, [2]string{"", ""})
		if m.Download != "" {
			footer = append(footer, [2]string{"Total download size:", m.Download})
		}
		footer = append(footer, [2]string{m.DiskLabel, m.Disk})
		footerRows = len(footer)
	}
	if footerRows < 2 {
		footerRows = 2
	}

	tableH := bh - 2 - footerRows
	if tableH < 4 {
		tableH = 4
	}
	tableY := by + 2
	tableW := bw - 3
	table := &block{X: bx, Y: tableY, W: tableW, H: tableH}
	table.fill(buf, t.Primary)

	gutterX := bx + tableW
	gutter := &block{X: gutterX, Y: tableY, W: 3, H: tableH}
	gutter.fill(buf, t.Primary)

	tab := m.cur()
	if tab != nil {
		hasDiff := tabHasVersionDiff(*tab)
		headers := headersFor(*tab, hasDiff)
		natural := make([]int, len(headers))
		for i, hd := range headers {
			natural[i] = term.StringWidth(hd)
		}
		for _, r := range tab.Rows {
			cells := cellsFor(*tab, r, hasDiff)
			for i, c := range cells {
				if i < len(natural) && term.StringWidth(c) > natural[i] {
					natural[i] = term.StringWidth(c)
				}
			}
		}
		total := tableW - 2
		available := total - 2
		offsets, widths := columnLayout(natural, available)
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

		aligns := alignsFor(len(headers))
		for i, hd := range headers {
			cx := tableInteriorX + 2 + offsets[i]
			drawAligned(buf, cx, headerY, widths[i], hd, t.style(RoleRegular), aligns[i])
		}
		for i := 0; i < visible && scroll+i < len(tab.Rows); i++ {
			r := tab.Rows[scroll+i]
			y := headerY + 1 + i
			selected := scroll+i == m.Sel
			for x := tableInteriorX; x < tableInteriorX+total; x++ {
				buf.Set(x, y, ' ', t.Regular)
			}
			cells := cellsFor(*tab, r, hasDiff)
			for ci := range cells {
				cx := tableInteriorX + 2 + offsets[ci]
				st := t.style(RoleRegular)
				if selected {
					st = t.style(RolePrimary)
				} else if ci == 0 {
					st = t.style(roleFor(tab.Label))
				}
				if !selected && (tab.Label == "Upgrade" || tab.Label == "Downgrade" || tab.Label == "Held") && ci == 2 && len(r.NewSpans) > 0 {
					drawSpansAligned(buf, cx, y, widths[ci], r.NewSpans, t, aligns[ci])
					continue
				}
				drawAligned(buf, cx, y, widths[ci], cells[ci], st, aligns[ci])
			}
			if selected {
				buf.Set(tableInteriorX, y, '❯', t.style(RolePrimary))
			}
		}
		// Scrollbar thumb in the gutter's single interior column. A table
		// that fits shows a one-cell thumb at the top (ADR-0017 C2).
		interior := tableH - 2
		if interior > 0 {
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
				buf.Set(gutterX+1, yy, ' ', t.Regular)
			}
			for yy := thumbY; yy < thumbY+thumbH && yy < tableY+tableH-1; yy++ {
				buf.Set(gutterX+1, yy, '█', t.style(RolePrimary))
			}
		}
	}

	// Footer left block and the help block one column to its right.
	fy := tableY + tableH
	labelW, valueW := 0, 0
	for _, row := range footer {
		if term.StringWidth(row[0]) > labelW {
			labelW = term.StringWidth(row[0])
		}
		if term.StringWidth(row[1]) > valueW {
			valueW = term.StringWidth(row[1])
		}
	}
	for i, row := range footer {
		y := fy + i
		if y >= by+bh {
			break
		}
		if row[0] != "" {
			buf.DrawText(bx, y, row[0], t.style(RoleRegular))
		}
		if row[1] != "" {
			buf.DrawTextRight(bx+labelW+valueW, y, row[1], t.style(RoleRegular))
		}
	}
	helpX := bx + labelW + valueW + 1
	helpW := bw - (labelW + valueW + 1)
	if helpW >= 12 {
		var lines []string
		for _, line := range m.Help {
			lines = append(lines, wrapText(line, helpW)...)
		}
		for i, line := range lines {
			if i >= footerRows {
				break
			}
			buf.DrawTextCenter(helpX, fy+i, helpW, line, t.style(RoleSecondary))
		}
	}
	return buf
}

// TooSmall is FR-071's notice, centred in the body.
func TooSmall(w, h int) string {
	return "Terminal too small (60x18 minimum; now " + itoa(w) + "x" + itoa(h) + ")"
}

// columnLayout returns each column's offset and draw width inside the table
// interior. When the natural widths fit, SpaceAround spreads them. When they
// do not (the 60x18 floor with realistic names), it shrinks deterministically
// from the left, reserving one cell for every remaining column, so columns
// never overlap or paint over the gutter.
func columnLayout(natural []int, total int) (offsets, widths []int) {
	n := len(natural)
	offsets = make([]int, n)
	widths = make([]int, n)
	if n == 0 {
		return offsets, widths
	}
	content := 0
	for _, w := range natural {
		content += w
	}
	if content <= total {
		copy(widths, natural)
		return spaceAround(natural, total), widths
	}
	remaining := total
	for i := 0; i < n; i++ {
		reserve := n - 1 - i
		maxWidth := remaining - reserve
		if maxWidth < 1 {
			maxWidth = 1
		}
		w := natural[i]
		if w > maxWidth {
			w = maxWidth
		}
		widths[i] = w
		remaining -= w
	}
	pos := 0
	for i := 0; i < n; i++ {
		offsets[i] = pos
		pos += widths[i]
	}
	return offsets, widths
}

// spaceAround is FR-068's deterministic replacement for SpaceAround.
func spaceAround(natural []int, total int) []int {
	n := len(natural)
	out := make([]int, n)
	content := 0
	for _, w := range natural {
		content += w
	}
	if content > total || n <= 1 {
		return out
	}
	extra := total - content
	gap := extra / n
	remainder := extra % n
	out[0] = gap / 2
	for i := 1; i < n; i++ {
		bonus := 0
		if i <= remainder {
			bonus = 1
		}
		out[i] = out[i-1] + natural[i-1] + gap + bonus
	}
	return out
}

func drawAligned(buf *Buffer, x, y, w int, s string, st Style, align int) {
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

func drawSpansAligned(buf *Buffer, x, y, w int, spans []Span, t Theme, align int) {
	total := spanWidth(spans)
	start := x
	if align == 1 {
		start = x + (w-total)/2
	} else if align == 2 {
		start = x + w - total
	}
	col := start
	for _, sp := range spans {
		col = buf.DrawTextLimit(col, y, sp.Text, t.style(sp.Role), x+w-col)
	}
}

// dropCells removes the first n terminal cells from s, never splitting a wide
// rune: a rune that would straddle the cut is dropped whole.
func dropCells(s string, n int) string {
	col := 0
	for i, r := range s {
		if col >= n {
			return s[i:]
		}
		col += term.RuneWidth(r)
	}
	return ""
}

// wrapText word-wraps at spaces and hard-splits a single overlong token,
// never truncating with an ellipsis.
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

// bodyRect is the summary body: one column of horizontal padding.
func bodyRect(w, h int) (x, y, bw, bh int) {
	return 2, 1, w - 4, h - 2
}

func centerText(buf *Buffer, y int, s string, st Style) {
	buf.DrawTextCenter(0, y, buf.W, s, st)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

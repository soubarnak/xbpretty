package tui

import "github.com/soubarnak/xbpretty/internal/term"

// FetchRow is one scored mirror in the fetch picker (FR-070). The display
// strings arrive preformatted: the cli owns the ms and binary-rate rules, the
// widget only lays them out.
type FetchRow struct {
	URL      string
	Location string
	Region   string
	Tier     int
	Latency  string
	Speed    string
	Score    string
}

// FetchModel is the single-select picker state. The cursor starts on row 0,
// which is the fastest mirror because the caller passes the ranked list.
type FetchModel struct {
	Rows []FetchRow
	Sel  int
}

// Move shifts the selection by d with wrap-around, which is FR-070's j/k rule.
func (m *FetchModel) Move(d int) {
	n := len(m.Rows)
	if n == 0 {
		return
	}
	m.Sel = (m.Sel + d + n) % n
}

// SetSelection clamps the selection to the list; Home/End use it.
func (m *FetchModel) SetSelection(i int) {
	if len(m.Rows) == 0 {
		m.Sel = 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i > len(m.Rows)-1 {
		i = len(m.Rows) - 1
	}
	m.Sel = i
}

// fetchCol is one visible picker column.
type fetchCol struct {
	key   string
	name  string
	w     int // natural width; the URL column uses the flexible width instead
	right bool
}

// fetchColumnLayout decides which columns survive and how wide the URL column
// is (FR-070's C1 rule). Location appears only from 120 terminal columns;
// Region, Tier and Speed drop in that order while the URL cannot keep its
// 24-cell floor. Latency and Score are never dropped. The rev 6 golden reads
// the Location threshold as the terminal width (120), not the 118-cell
// interior; keeping that reading preserves the checked-in 120x30 frame.
// Location is untrusted mirror data, so after the ordered drops it is the
// last column removed if the remaining fixed cells still overflow.
func fetchColumnLayout(w int, rows []FetchRow) (cols []fetchCol, urlW int) {
	interior := w - 2
	url := fetchCol{key: "url", name: "Mirrors:"}
	width := func(key, header string, cell func(FetchRow) string) fetchCol {
		c := fetchCol{key: key, name: header, w: term.StringWidth(header)}
		for _, r := range rows {
			if got := term.StringWidth(cell(r)); got > c.w {
				c.w = got
			}
		}
		return c
	}
	reg := width("reg", "Region:", func(r FetchRow) string { return r.Region })
	tier := fetchCol{key: "tier", name: "Tier:", w: 5, right: true}
	lat := width("lat", "Latency:", func(r FetchRow) string { return r.Latency })
	lat.right = true
	spd := width("spd", "Speed:", func(r FetchRow) string { return r.Speed })
	spd.right = true
	score := width("score", "Score:", func(r FetchRow) string { return r.Score })
	score.right = true
	loc := width("loc", "Location:", func(r FetchRow) string { return r.Location })

	middle := []fetchCol{reg, tier, lat, spd, score}
	if w >= 120 {
		middle = append([]fetchCol{loc}, middle...)
	}
	for len(middle) > 2 {
		if layoutTotal(middle) <= interior {
			break
		}
		drop := -1
		for i, c := range middle {
			if c.key == "reg" || c.key == "tier" || c.key == "spd" {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		middle = append(middle[:drop], middle[drop+1:]...)
	}
	// Location is the only column outside the C1 drop order. It is untrusted
	// network data, so never let a long value push Latency or Score past the
	// border: drop it after Region/Tier/Speed could not make room.
	if layoutTotal(middle) > interior {
		for i, c := range middle {
			if c.key == "loc" {
				middle = append(middle[:i], middle[i+1:]...)
				break
			}
		}
	}
	fixed := 0
	for _, c := range middle {
		fixed += c.w
	}
	urlW = interior - (2 + len(middle) + fixed)
	if urlW < 24 {
		urlW = 24
	}
	cols = append([]fetchCol{url}, middle...)
	return cols, urlW
}

// layoutTotal is the width the URL floor plus the middle columns need,
// including the border and one gap between every pair.
func layoutTotal(middle []fetchCol) int {
	total := 2 + 24 + len(middle)
	for _, c := range middle {
		total += c.w
	}
	return total
}

// fetchValues returns the cells for the visible columns.
func fetchValues(cols []fetchCol, r FetchRow) []string {
	all := map[string]string{
		"url": r.URL, "loc": r.Location, "reg": r.Region,
		"tier": itoa(r.Tier), "lat": r.Latency, "spd": r.Speed, "score": r.Score,
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = all[c.key]
	}
	return out
}

// RenderFetch draws the rounded single-select mirror picker (S3). It is the
// frame only: the caller owns the key loop and returns one index.
func RenderFetch(m *FetchModel, w, h int, t Theme) *Buffer {
	buf := NewBuffer(w, h)
	buf.Fill(0, 0, w, h, t.Regular)
	drawFetchFrame(buf, w, h, t)
	drawFetchTitle(buf, w, t)
	if !Fits(w, h) {
		centerText(buf, h/2, TooSmall(w, h), t.style(RoleNotice))
		return buf
	}

	cols, urlW := fetchColumnLayout(w, m.Rows)
	blockW := 2 + urlW
	for i, c := range cols {
		if i > 0 {
			blockW += 1 + c.w
		}
	}
	start := 1 + (w-2-blockW)/2
	if start < 1 {
		start = 1
	}
	listH := h - 2 - 2 - 3
	if listH < 2 {
		listH = 2
	}

	// Header: blank marker cells then the column names.
	x := start + 2
	for i, c := range cols {
		cw := c.w
		if i == 0 {
			cw = urlW
		}
		if i > 0 {
			x++
		}
		drawAligned(buf, x, 1, clampToInterior(cw, w, x), c.name, t.style(RoleRegular), boolAlign(c.right))
		x += cw
	}

	visible := listH - 1
	offset := 0
	if m.Sel >= visible && visible > 0 {
		offset = m.Sel - visible + 1
	}
	for i := 0; i < visible && offset+i < len(m.Rows); i++ {
		idx := offset + i
		row := m.Rows[idx]
		rowY := 2 + i
		chosen := idx == m.Sel
		marker := "☐ "
		if chosen {
			marker = "✓ "
		}
		buf.DrawText(start, rowY, marker, t.style(RoleRegular))
		values := fetchValues(cols, row)
		x := start + 2
		for ci, c := range cols {
			cw := c.w
			if ci == 0 {
				cw = urlW
			}
			if ci > 0 {
				x++
			}
			st := t.style(RoleRegular)
			if chosen && (ci == 0 || ci == len(cols)-1) {
				st = t.style(RoleSecondary)
				st.Mods |= term.Reverse
			}
			drawAligned(buf, x, rowY, clampToInterior(cw, w, x), values[ci], st, boolAlign(c.right))
			x += cw
		}
	}

	// Two-row info band, then the two-line help band (FR-070).
	infoY := 1 + listH
	info := "Score is how many milliseconds it takes to download the repository index."
	infoStyle := t.style(RoleRegular)
	infoStyle.Mods |= term.Italic
	buf.DrawTextCenter(1, infoY+1, w-2, info, infoStyle)
	helpY := infoY + 2
	for i, line := range []string{
		"Use ↓↑ to move, Home/End to go top/bottom,",
		"q/Enter to select, Esc to cancel.",
	} {
		buf.DrawTextCenter(1, helpY+1+i, w-2, line, t.style(RoleSecondary))
	}
	return buf
}

// drawFetchFrame paints the rounded border.
func drawFetchFrame(buf *Buffer, w, h int, t Theme) {
	if w < 2 || h < 2 {
		return
	}
	for x := 1; x < w-1; x++ {
		buf.Set(x, 0, '─', t.style(RolePrimary))
		buf.Set(x, h-1, '─', t.style(RolePrimary))
	}
	for y := 1; y < h-1; y++ {
		buf.Set(0, y, '│', t.style(RolePrimary))
		buf.Set(w-1, y, '│', t.style(RolePrimary))
	}
	buf.Set(0, 0, '╭', t.style(RolePrimary))
	buf.Set(w-1, 0, '╮', t.style(RolePrimary))
	buf.Set(0, h-1, '╰', t.style(RolePrimary))
	buf.Set(w-1, h-1, '╯', t.style(RolePrimary))
}

// drawFetchTitle centres "  xbpretty Fetch  " in the top border.
func drawFetchTitle(buf *Buffer, w int, t Theme) {
	if w < 4 {
		return
	}
	title := "  xbpretty Fetch  "
	tw := term.StringWidth(title)
	if tw > w-2 {
		title = term.Truncate("xbpretty Fetch", w-4)
		tw = term.StringWidth(title)
	}
	start := 1 + (w-2-tw)/2
	st := t.style(RoleHighlight)
	for x := start; x < start+tw && x < w-1; x++ {
		buf.Set(x, 0, ' ', st)
	}
	buf.DrawTextLimit(start, 0, title, st, w-2)
}

// clampToInterior limits a draw so it can never paint over the right border.
// The clamp is a safety net for pathological untrusted widths; the layout
// above normally makes every column fit.
func clampToInterior(cw, w, x int) int {
	if avail := (w - 1) - x; cw > avail {
		cw = avail
	}
	if cw < 0 {
		cw = 0
	}
	return cw
}

func boolAlign(right bool) int {
	if right {
		return 2
	}
	return 0
}

package main

import "github.com/soubarnak/xbpretty/internal/term"

// FetchModel is the full-screen single-select picker (#50 §5).
type FetchModel struct {
	Items []Mirror
	Sel   int
}

func (m *FetchModel) move(d int) {
	n := len(m.Items)
	if n == 0 {
		return
	}
	m.Sel = (m.Sel + d + n) % n
}

// fetchCol is one visible column.
type fetchCol struct {
	key   string
	name  string
	w     int // natural width; the URL column is overridden by urlW
	right bool
}

// fetchColumns decides which columns survive and how wide the URL column is.
//
// #50 §5.2's "dropped when interior width <" table contradicts its own 80x24
// golden frame: at 78 interior columns that frame still shows Region, Tier,
// Latency, Speed and Score. The prototype therefore keeps columns while they
// fit (URL floored at 24 cells) and drops in the specified order — Location,
// Region, Tier, Speed — and shows Location only from 120 columns.
func fetchColumns(w int, mirrors []Mirror) (cols []fetchCol, urlW, blockW int) {
	interior := w - 2
	url := fetchCol{key: "url", name: "Mirrors:"}
	for _, m := range mirrors {
		if term.StringWidth(m.URL) > url.w {
			url.w = term.StringWidth(m.URL)
		}
	}
	if url.w < 8 {
		url.w = 8
	}
	width := func(key, header string, cell func(Mirror) string) fetchCol {
		c := fetchCol{key: key, name: header, w: term.StringWidth(header)}
		for _, m := range mirrors {
			if s := term.StringWidth(cell(m)); s > c.w {
				c.w = s
			}
		}
		return c
	}
	reg := width("reg", "Region:", func(m Mirror) string { return m.Region })
	tier := fetchCol{key: "tier", name: "Tier:", w: 5, right: true}
	lat := width("lat", "Latency:", func(m Mirror) string { return m.Latency })
	lat.right = true
	spd := width("spd", "Speed:", func(m Mirror) string { return m.Speed })
	spd.right = true
	score := width("score", "Score:", func(m Mirror) string { return itoa(m.Score) })
	score.right = true
	loc := width("loc", "Location:", func(m Mirror) string { return m.Location })

	middle := []fetchCol{reg, tier, lat, spd, score}
	if w >= 120 {
		middle = append([]fetchCol{loc}, middle...)
	}
	// Drop in order until URL can keep its 24-cell floor.
	for len(middle) > 3 {
		total := 2 + 24 + len(middle) /* gaps */
		for _, c := range middle {
			total += c.w
		}
		if total <= interior {
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
	fixed := 0
	for _, c := range middle {
		fixed += c.w
	}
	urlW = interior - (2 + len(middle) + fixed)
	if urlW < 24 {
		urlW = 24
	}
	blockW = 2 + urlW + fixed + len(middle)

	cols = append([]fetchCol{url}, middle...)
	// Recompute the overall width with the actual URL width.
	blockW = 2
	for i, c := range cols {
		if i > 0 {
			blockW++
		}
		if i == 0 {
			blockW += urlW
		} else {
			blockW += c.w
		}
	}
	return cols, urlW, blockW
}

// renderFetch draws the picker. List rows fill the flexible band, then a 2-row
// info band and a 3-row help band.
func renderFetch(m *FetchModel, w, h int, t Theme) *term.Buffer {
	buf := term.NewBuffer(w, h)
	block := &Block{X: 0, Y: 0, W: w, H: h, Rounded: true, Style: Primary}
	block.fill(buf, t)
	block.title(buf, "xbpretty Fetch", Highlight)
	if w < 60 || h < 18 {
		centerText(buf, h/2, TooSmall(w, h), Notice)
		return buf
	}
	cols, urlW, blockW := fetchColumns(w, m.Items)
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
		drawAligned(buf, x, 1, cw, c.name, Regular, boolAlign(c.right))
		x += cw
	}

	visible := listH - 1
	offset := 0
	if m.Sel >= visible && visible > 0 {
		offset = m.Sel - visible + 1
	}
	for i := 0; i < visible && offset+i < len(m.Items); i++ {
		idx := offset + i
		mi := m.Items[idx]
		rowY := 2 + i
		chosen := idx == m.Sel
		marker := "☐ "
		if chosen {
			marker = "✓ "
		}
		buf.DrawText(start, rowY, marker, Regular)
		vals := valuesForKeys(cols, mi)
		x := start + 2
		for ci, c := range cols {
			cw := c.w
			if ci == 0 {
				cw = urlW
			}
			if ci > 0 {
				x++
			}
			st := Regular
			if chosen && (ci == 0 || ci == len(cols)-1) {
				st = Secondary
			}
			drawAligned(buf, x, rowY, cw, vals[ci], st, boolAlign(c.right))
			x += cw
		}
	}

	infoY := 1 + listH
	info := "Score is how many milliseconds it takes to download the repository index."
	buf.DrawTextCenter(1, infoY+1, w-2, info, Italic)
	helpY := infoY + 2
	for i, line := range []string{
		"Use ↓↑ to move, Home/End to go top/bottom,",
		"q/Enter to select, Esc to cancel.",
	} {
		buf.DrawTextCenter(1, helpY+1+i, w-2, line, Secondary)
	}
	return buf
}

func valuesForKeys(cols []fetchCol, m Mirror) []string {
	all := map[string]string{
		"url": m.URL, "loc": m.Location, "reg": m.Region,
		"tier": itoa(m.Tier), "lat": m.Latency, "spd": m.Speed, "score": itoa(m.Score),
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = all[c.key]
	}
	return out
}

func boolAlign(right bool) int {
	if right {
		return 2
	}
	return 0
}

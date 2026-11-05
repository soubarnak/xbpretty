package main

import (
	"strings"
)

// Table is the prototype's stand-in for nala's comfy-table 7.2.2 table:
// preset NOTHING, ContentArrangement::DynamicFullWidth, header cells painted
// Highlight, only the last column right-aligned (#45 §3.2).
type Table struct {
	Header []string
	Rows   [][]string
}

// arrange reproduces comfy-table's dynamic arrangement (utils/arrangement/
// dynamic.rs, verified against the 7.2.2 crate with a local harness):
//
//   - available content width = terminal width − padding (2 per column);
//   - fix every column whose natural width is ≤ the current average, raising
//     the average as fixed columns release space;
//   - hand the leftover to every column equally, remainder left-to-right.
//
// The prototype skips comfy-table's optimize_space_after_split optimisation;
// it only matters when the content has to be squeezed, and the tested 80/100
// column frames never squeeze.
func arrange(natural []int, width int, fullWidth bool) []int {
	n := len(natural)
	out := make([]int, n)
	copy(out, natural)
	if !fullWidth || width <= 0 {
		return out
	}
	available := width - 2*n
	if available < 0 {
		available = 0
	}
	sum := 0
	for _, w := range natural {
		sum += w
	}
	distribute := func(remaining int, indices []int) {
		if len(indices) == 0 {
			return
		}
		avg := remaining / len(indices)
		excess := remaining - avg*len(indices)
		for _, i := range indices {
			add := avg
			if excess > 0 {
				add++
				excess--
			}
			out[i] += add
		}
	}
	if sum <= available {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		distribute(available-sum, all)
		return out
	}
	fixed := make([]bool, n)
	remaining, colsLeft := available, n
	for colsLeft > 0 {
		avg := remaining / colsLeft
		if avg == 0 {
			break
		}
		changed := false
		for i := 0; i < n; i++ {
			if fixed[i] || natural[i] > avg {
				continue
			}
			out[i] = natural[i]
			fixed[i] = true
			remaining -= natural[i]
			colsLeft--
			if colsLeft == 0 {
				break
			}
			avg = remaining / colsLeft
			changed = true
		}
		if !changed {
			break
		}
	}
	if colsLeft == 0 {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		distribute(remaining, all)
		return out
	}
	var open []int
	for i := 0; i < n; i++ {
		if !fixed[i] {
			open = append(open, i)
		}
	}
	// Squeezed: comfy-table's distribute_remaining_space *sets* the width of
	// each unfixed column (unlike use_full_width, which adds to it).
	if remaining < len(open) {
		remaining = len(open)
	}
	avg := remaining / len(open)
	excess := remaining - avg*len(open)
	for _, i := range open {
		if excess > 0 {
			out[i] = avg + 1
			excess--
		} else {
			out[i] = avg
		}
	}
	return out
}

// wrapCell splits one cell into lines of at most w columns. Word-wrap at
// spaces, hard-split a single long token — nala never truncates and never sets
// max_height (#45 §3.2). Cells carrying ANSI are returned unsplit: in the
// tested frames the painted cells are short and never wrap, and ANSI-aware
// splitting is a known prototype limitation.
func wrapCell(s string, w int) []string {
	if w <= 0 {
		return []string{""}
	}
	if visibleWidth(s) <= w {
		return []string{s}
	}
	if stripANSI(s) != s {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
	}
	for _, word := range strings.Split(s, " ") {
		if word == "" {
			continue
		}
		if cur.Len() > 0 {
			if cur.Len()+1+len([]rune(word)) <= w {
				cur.WriteByte(' ')
				cur.WriteString(word)
				continue
			}
			flush()
		}
		for len([]rune(word)) > w {
			r := []rune(word)
			flush()
			lines = append(lines, string(r[:w]))
			word = string(r[w:])
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// render lays the table out at width columns. fullWidth=false is the piped
// path: natural widths, no wrapping, no ANSI (#45 §3.2).
func (t Table) render(width int, fullWidth bool, th Theme) []string {
	n := len(t.Header)
	if n == 0 || len(t.Rows) == 0 {
		return nil
	}
	natural := make([]int, n)
	for i, h := range t.Header {
		natural[i] = visibleWidth(h)
	}
	for _, row := range t.Rows {
		for i := 0; i < n && i < len(row); i++ {
			if w := visibleWidth(row[i]); w > natural[i] {
				natural[i] = w
			}
		}
	}
	widths := arrange(natural, width, fullWidth)

	// Build the wrapped cell lines for the header and every row, then emit
	// one physical line per visual row.
	type wrappedCell []string
	build := func(cells []string, paintHeader bool) []wrappedCell {
		out := make([]wrappedCell, n)
		for i := 0; i < n; i++ {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			if paintHeader {
				text = th.Paint(RoleHighlight, text)
			}
			out[i] = wrapCell(text, widths[i])
		}
		return out
	}
	emit := func(cells []wrappedCell) []string {
		height := 1
		for _, c := range cells {
			if len(c) > height {
				height = len(c)
			}
		}
		var out []string
		for line := 0; line < height; line++ {
			parts := make([]string, n)
			for i := 0; i < n; i++ {
				text := ""
				if line < len(cells[i]) {
					text = cells[i][line]
				}
				if i == n-1 {
					parts[i] = padLeft(text, widths[i])
				} else {
					parts[i] = padRight(text, widths[i])
				}
			}
			out = append(out, " "+strings.Join(parts, "  ")+" ")
		}
		return out
	}

	lines := emit(build(t.Header, true))
	for _, row := range t.Rows {
		lines = append(lines, emit(build(row, false))...)
	}
	return lines
}

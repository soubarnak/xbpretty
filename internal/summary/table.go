package summary

import (
	"strings"
	"unicode/utf8"
)

// table is nala's comfy-table stand-in: preset NOTHING, a 2-space minimum
// gap, one leading and one trailing space, and only the last column
// right-aligned.
type table struct {
	header []string
	rows   [][]string
}

// render lays the table out at width columns. fullWidth=false is the piped
// path: natural widths, no wrapping, no ANSI-aware expansion.
func (t table) render(width int, fullWidth bool, r Renderer) []string {
	n := len(t.header)
	if n == 0 || len(t.rows) == 0 {
		return nil
	}
	natural := make([]int, n)
	for i, h := range t.header {
		natural[i] = visibleWidth(h)
	}
	for _, row := range t.rows {
		for i := 0; i < n && i < len(row); i++ {
			if w := visibleWidth(row[i]); w > natural[i] {
				natural[i] = w
			}
		}
	}
	widths := arrange(natural, width, fullWidth)

	type wrappedCell []string
	build := func(cells []string, paintHeader bool) []wrappedCell {
		out := make([]wrappedCell, n)
		for i := 0; i < n; i++ {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			if paintHeader {
				text = r.paint(RoleHighlight, text)
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

	lines := emit(build(t.header, true))
	for _, row := range t.rows {
		lines = append(lines, emit(build(row, false))...)
	}
	return lines
}

// arrange reproduces comfy-table 7.2.2's dynamic arrangement:
//
//   - available content width = terminal width − two padding cells per column;
//   - fix every column whose natural width is ≤ the current average, raising
//     the average as fixed columns release space;
//   - hand the leftover to every column equally, remainder left-to-right;
//   - when squeezed, set the unfixed columns' widths from the remaining space.
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
	if len(open) == 0 {
		return out
	}
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

// wrapCell splits one cell into lines of at most w columns. Plain cells
// word-wrap at spaces and hard-split a single long token; painted cells are
// hard-split at the column width while keeping their SGR state.
func wrapCell(s string, w int) []string {
	if w <= 0 {
		return []string{""}
	}
	if visibleWidth(s) <= w {
		return []string{s}
	}
	if stripANSI(s) == s {
		return wrapPlainCell(s, w)
	}
	return wrapANSICell(s, w)
}

func wrapPlainCell(s string, w int) []string {
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
			if cur.Len()+1+utf8.RuneCountInString(word) <= w {
				cur.WriteByte(' ')
				cur.WriteString(word)
				continue
			}
			flush()
		}
		runes := []rune(word)
		for len(runes) > w {
			flush()
			lines = append(lines, string(runes[:w]))
			runes = runes[w:]
		}
		cur.WriteString(string(runes))
	}
	if cur.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

func wrapANSICell(s string, w int) []string {
	var lines []string
	var cur strings.Builder
	curW := 0
	active := ""
	writeReset := func() {
		if active != "" {
			cur.WriteString(resetSeq)
		}
		lines = append(lines, cur.String())
		cur.Reset()
		if active != "" {
			cur.WriteString(active)
		}
		curW = 0
	}
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z')) {
				j++
			}
			if j < len(s) {
				j++
			}
			seq := s[i:j]
			if seq == resetSeq {
				active = ""
			} else if strings.HasSuffix(seq, "m") {
				active = seq
			}
			cur.WriteString(seq)
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if curW+1 > w {
			writeReset()
		}
		cur.WriteRune(r)
		curW++
		i += size
	}
	if curW > 0 || len(lines) == 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

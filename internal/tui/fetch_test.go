package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/term"
)

// goldenMirrors is the rev 6 four-mirror picker fixture (FR-070, AC-052).
func goldenMirrors() []FetchRow {
	return []FetchRow{
		{URL: "https://repo.voidlinux.org/current", Location: "Finland", Region: "EU", Tier: 1, Latency: "18 ms", Speed: "4.1 MiB/s", Score: "18"},
		{URL: "https://mirror.example.net/voidlinux", Location: "Germany", Region: "EU", Tier: 2, Latency: "31 ms", Speed: "3.2 MiB/s", Score: "31"},
		{URL: "https://mirror.osuosl.org/voidlinux", Location: "United States", Region: "NA", Tier: 2, Latency: "47 ms", Speed: "2.8 MiB/s", Score: "47"},
		{URL: "https://slow.example.org/voidlinux", Location: "Japan", Region: "AS", Tier: 3, Latency: "112 ms", Speed: "0.9 MiB/s", Score: "112"},
	}
}

// TestFetchGoldens is AC-052's frame half: the static picker frame matches the
// checked-in grid at 80x24, 60x18 and the 120-column Location threshold.
func TestFetchGoldens(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"fetch-80x24", 80, 24},
		{"fetch-60x18", 60, 18},
		{"fetch-120x30", 120, 30},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := RenderFetch(&FetchModel{Rows: goldenMirrors()}, c.w, c.h, DefaultTheme())
			if got, want := buf.Text(), string(readGolden(t, c.name+".txt")); got != want {
				t.Errorf("grid differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if got, want := buf.Flush(nil, true), readGolden(t, c.name+".sgr"); string(got) != string(want) {
				t.Errorf("SGR bytes differ\n--- got ---\n%q\n--- want ---\n%q", got, want)
			}
		})
	}
}

// TestFetchCursorWraps is AC-052's navigation half: the cursor starts at the
// fastest mirror and j/k wrap at both ends.
func TestFetchCursorWraps(t *testing.T) {
	m := &FetchModel{Rows: goldenMirrors()}
	if m.Sel != 0 {
		t.Fatalf("initial selection = %d, want the fastest mirror at row 0", m.Sel)
	}
	for _, tc := range []struct {
		move int
		want int
	}{
		{1, 1}, {1, 2}, {1, 3}, {1, 0},
		{-1, 3}, {-1, 2}, {-1, 1}, {-1, 0},
	} {
		m.Move(tc.move)
		if m.Sel != tc.want {
			t.Errorf("Move(%d) selection = %d, want %d", tc.move, m.Sel, tc.want)
		}
	}
	m.SetSelection(2)
	if m.Sel != 2 {
		t.Errorf("SetSelection(2) = %d", m.Sel)
	}
	m.SetSelection(0)
	m.Move(-1)
	if m.Sel != len(m.Rows)-1 {
		t.Errorf("Move(-1) from row 0 = %d, want wrap to %d", m.Sel, len(m.Rows)-1)
	}
}

// TestFetchHighlightMarkersAndBands is AC-052's style half: the selected row
// is Secondary+REVERSED on URL and Score, every row carries a marker, and the
// info band is italic.
func TestFetchHighlightMarkersAndBands(t *testing.T) {
	buf := RenderFetch(&FetchModel{Rows: goldenMirrors()}, 80, 24, DefaultTheme())
	grid := buf.Text()
	if !strings.Contains(grid, "✓ https://repo.voidlinux.org/current") {
		t.Errorf("selected row does not carry the ✓ marker:\n%s", grid)
	}
	if got := strings.Count(grid, "☐"); got != 3 {
		t.Errorf("unselected markers = %d, want 3", got)
	}
	// Row 2 (the first list row) is the selected URL and the last visible
	// column is Score; both carry Secondary|Reverse.
	theme := DefaultTheme()

	// Find the selected row and check the exact style of the URL and score
	// cells by scanning the buffer (grid indexes are bytes, not cells).
	y := 2
	urlX, scoreX := -1, -1
	for x := 0; x < buf.W; x++ {
		switch buf.Get(x, y).R {
		case 'h':
			if urlX < 0 {
				urlX = x
			}
		case '8':
			scoreX = x
		}
	}
	if urlX < 0 || scoreX < 0 {
		t.Fatalf("selected row cells missing at y=%d:\n%s", y, grid)
	}
	sel := buf.Get(urlX, y).S
	sec := theme.Secondary
	sec.Mods |= term.Reverse
	if sel != sec {
		t.Errorf("selected URL style = %+v, want Secondary+REVERSED %+v", sel, sec)
	}
	if got := buf.Get(scoreX, y).S; got != sec {
		t.Errorf("selected Score style = %+v, want Secondary+REVERSED %+v", got, sec)
	}
	// A neighbouring row's URL is Regular.
	if other := buf.Get(urlX, y+1).S; other != theme.Regular {
		t.Errorf("unselected URL style = %+v, want Regular", other)
	}
	// The info band is centred italic and the help band is Secondary.
	infoY := -1
	for i, line := range strings.Split(grid, "\n") {
		if strings.Contains(line, "Score is how many milliseconds") {
			infoY = i
			break
		}
	}
	if infoY < 0 {
		t.Fatalf("info band missing:\n%s", grid)
	}
	infoX := strings.Index(strings.Split(grid, "\n")[infoY], "Score is")
	if got := buf.Get(infoX, infoY).S; got.Mods&term.Italic == 0 {
		t.Errorf("info band style = %+v, want italic", got)
	}
}

// TestFetchColumnsC1 is FR-070's fit rule: Location only from 120 columns,
// Region/Tier/Speed drop in order while the URL cannot keep 24 cells, and
// Latency/Score are never dropped.
func TestFetchColumnsC1(t *testing.T) {
	rows := goldenMirrors()
	cases := []struct {
		w    int
		keys string
	}{
		{60, "url tier lat spd score"},
		{80, "url reg tier lat spd score"},
		{119, "url reg tier lat spd score"},
		{120, "url loc reg tier lat spd score"},
	}
	for _, tc := range cases {
		cols, urlW := fetchColumnLayout(tc.w, rows)
		var keys []string
		for _, c := range cols {
			keys = append(keys, c.key)
		}
		if got := strings.Join(keys, " "); got != tc.keys {
			t.Errorf("width %d columns = %q, want %q", tc.w, got, tc.keys)
		}
		if urlW < 24 {
			t.Errorf("width %d URL width = %d, want >= 24", tc.w, urlW)
		}
	}

	// With long numeric columns, Speed drops after Region and Tier while
	// Latency and Score stay.
	wide := []FetchRow{{
		URL: "https://repo.voidlinux.org/current", Region: "EU", Tier: 1,
		Latency: "1234567890123456789 ms", Speed: "12345678901234567890", Score: "1234567890",
	}}
	cols, _ := fetchColumnLayout(60, wide)
	var keys []string
	for _, c := range cols {
		keys = append(keys, c.key)
	}
	if got := strings.Join(keys, " "); got != "url lat score" {
		t.Errorf("long columns at 60 = %q, want url lat score (Speed dropped, Latency/Score kept)", got)
	}

	// Latency and Score are present at every width.
	for _, w := range []int{60, 70, 80, 100, 119, 120, 140} {
		cols, _ := fetchColumnLayout(w, goldenMirrors())
		seen := map[string]bool{}
		for _, c := range cols {
			seen[c.key] = true
		}
		if !seen["lat"] || !seen["score"] {
			t.Errorf("width %d dropped Latency or Score: %v", w, seen)
		}
	}
}

// TestFetchLongLocationKeepsBorderAndScore is C1's untrusted-data guard: a
// long Location is dropped rather than pushing Latency/Score over the border.
func TestFetchLongLocationKeepsBorderAndScore(t *testing.T) {
	rows := []FetchRow{{
		URL:      "https://repo.voidlinux.org/current",
		Location: strings.Repeat("Very Long Province Name, ", 20),
		Region:   "EU",
		Tier:     1,
		Latency:  "1234567890123456789 ms",
		Speed:    "12345678901234567890",
		Score:    "1234567890",
	}}
	cols, _ := fetchColumnLayout(120, rows)
	var keys []string
	for _, c := range cols {
		keys = append(keys, c.key)
	}
	if got := strings.Join(keys, " "); got != "url lat score" {
		t.Errorf("long Location columns = %q, want the Location dropped and Latency/Score kept", got)
	}
	buf := RenderFetch(&FetchModel{Rows: rows}, 120, 30, DefaultTheme())
	assertFetchBorders(t, buf.Text(), 120)
	grid := buf.Text()
	if strings.Contains(grid, "Location:") {
		t.Errorf("long Location was not dropped:\n%s", grid)
	}
	if !strings.Contains(grid, "Score:") || !strings.Contains(grid, "1234567890") {
		t.Errorf("long Location pushed Score off the frame:\n%s", grid)
	}
}

// TestFetchLongNumericCellsKeepBorder renders the 60x18 pathological columns:
// the border survives and the Score header stays visible even when the fixed
// cells overflow the interior.
func TestFetchLongNumericCellsKeepBorder(t *testing.T) {
	rows := goldenMirrors()
	rows[0] = FetchRow{
		URL: "https://repo.voidlinux.org/current", Region: "EU", Tier: 1,
		Latency: "1234567890123456789 ms", Speed: "12345678901234567890", Score: "1234567890",
	}
	buf := RenderFetch(&FetchModel{Rows: rows}, 60, 18, DefaultTheme())
	assertFetchBorders(t, buf.Text(), 60)
	grid := buf.Text()
	for _, col := range []string{"Mirrors:", "Latency:", "Score:"} {
		if !strings.Contains(grid, col) {
			t.Errorf("overflowed 60x18 frame lost %q:\n%s", col, grid)
		}
	}
	if strings.Contains(grid, "Location:") || strings.Contains(grid, "Region:") {
		t.Errorf("overflowed 60x18 frame kept a dropped column:\n%s", grid)
	}
}

// TestFetchInfoUsesThemeRegularBase proves the info band keeps the theme's
// Regular base (including a custom background) and only adds italic.
func TestFetchInfoUsesThemeRegularBase(t *testing.T) {
	theme := DefaultTheme()
	theme.Regular = term.Style{FG: term.ANSI(2), BG: term.ANSI(0), Mods: term.Bold}
	buf := RenderFetch(&FetchModel{Rows: goldenMirrors()}, 80, 24, theme)
	y := -1
	for i, line := range strings.Split(buf.Text(), "\n") {
		if strings.Contains(line, "Score is how many milliseconds") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatalf("info band missing:\n%s", buf.Text())
	}
	x := -1
	for col := 0; col < buf.W; col++ {
		if buf.Get(col, y).R == 'S' {
			x = col
			break
		}
	}
	if x < 0 {
		t.Fatalf("info text missing at row %d", y)
	}
	want := theme.Regular
	want.Mods |= term.Italic
	if got := buf.Get(x, y).S; got != want {
		t.Errorf("info style = %+v, want theme Regular + italic %+v", got, want)
	}
}

// assertFetchBorders proves every row keeps its left border and the rounded
// right border, so no untrusted cell width can paint over the frame.
func assertFetchBorders(t *testing.T, grid string, w int) {
	t.Helper()
	lines := strings.Split(grid, "\n")
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) == 0 {
			t.Errorf("row %d is empty", i)
			continue
		}
		if i == 0 {
			if runes[0] != '╭' || runes[len(runes)-1] != '╮' {
				t.Errorf("top border overwritten: %q", line)
			}
			continue
		}
		if i == len(lines)-1 {
			if runes[0] != '╰' || runes[len(runes)-1] != '╯' {
				t.Errorf("bottom border overwritten: %q", line)
			}
			continue
		}
		if runes[0] != '│' || runes[len(runes)-1] != '│' {
			t.Errorf("row %d border overwritten (%d cells, want %d): %q", i, len(runes), w, line)
		}
	}
}

// TestFetchInfoWrapsIntoTwoRows: at 60 columns the info sentence
// fills the two-row band instead of truncating its tail.
func TestFetchInfoWrapsIntoTwoRows(t *testing.T) {
	const info = "Score is how many milliseconds it takes to download the repository index."
	lines := strings.Split(RenderFetch(&FetchModel{Rows: goldenMirrors()}, 60, 18, DefaultTheme()).Text(), "\n")
	// Rows 12 and 13 are the band: infoY is 1+listH and the help band starts
	// two rows below it.
	// Each row sits between the rounded side borders.
	inside := func(line string) string { return strings.Trim(line, "│ ") }
	band := strings.Join([]string{inside(lines[12]), inside(lines[13])}, " ")
	if band != info {
		t.Errorf("info band = %q, want %q", band, info)
	}
	if inside(lines[14]) != "" {
		t.Errorf("row 14 = %q, want the blank gap before the help band", lines[14])
	}
}

// TestFetchScrollKeepsOffsetWhenMovingUp: moving up
// inside the visible window leaves the window alone, and the wrap from the
// first mirror back to the last scrolls it to the end.
func TestFetchScrollKeepsOffsetWhenMovingUp(t *testing.T) {
	rows := make([]FetchRow, 30)
	for i := range rows {
		rows[i] = FetchRow{URL: "https://mirror.example/" + itoa(i), Region: "EU", Tier: 1, Latency: "1 ms", Speed: "1 MiB/s", Score: itoa(i + 1)}
	}
	m := &FetchModel{Rows: rows}
	m.SetSelection(29)
	RenderFetch(m, 80, 24, DefaultTheme())
	// 80x24 shows 16 mirror rows, so the window for row 29 starts at 14.
	if m.Offset != 14 {
		t.Fatalf("offset after End = %d, want 14", m.Offset)
	}
	m.Move(-1)
	RenderFetch(m, 80, 24, DefaultTheme())
	if m.Offset != 14 || m.Sel != 28 {
		t.Errorf("after k: offset %d sel %d, want offset 14 sel 28", m.Offset, m.Sel)
	}
	m.Move(1)
	m.Move(1)
	RenderFetch(m, 80, 24, DefaultTheme())
	if m.Sel != 0 || m.Offset != 0 {
		t.Errorf("after wrap to the top: offset %d sel %d, want 0/0", m.Offset, m.Sel)
	}
	m.Move(-1)
	RenderFetch(m, 80, 24, DefaultTheme())
	if m.Sel != 29 || m.Offset != 14 {
		t.Errorf("after wrap to the bottom: offset %d sel %d, want 14/29", m.Offset, m.Sel)
	}
}

// TestFetchNoColorKeepsGlyphs is AC-055's fetch slice: --color never removes
// SGR but keeps every frame glyph.
func TestFetchNoColorKeepsGlyphs(t *testing.T) {
	buf := RenderFetch(&FetchModel{Rows: goldenMirrors()}, 80, 24, DefaultTheme())
	payload := buf.Flush(nil, false)
	if regexp.MustCompile("\x1b\\[[0-9;]*m").Match(payload) {
		t.Fatalf("SGR bytes under --color never: %q", payload)
	}
	for _, glyph := range []string{"╭", "╮", "╰", "╯", "✓", "☐"} {
		if !strings.Contains(buf.Text(), glyph) {
			t.Errorf("glyph %q missing under --color never", glyph)
		}
	}
	if got, want := payload, readGolden(t, "fetch-80x24.nocolor.sgr"); string(got) != string(want) {
		t.Errorf("no-colour bytes differ\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

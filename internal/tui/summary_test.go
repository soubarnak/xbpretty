package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/term"
)

func goldenUpgrade() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []Tab{{Label: "Upgrade", Rows: []PkgRow{
			{Name: "libfoo", Old: "1.2_1", New: "1.3_1", Size: "120 KiB", NewSpans: VersionSpans("1.2_1", "1.3_1")},
			{Name: "libbar", Old: "0.9_2", New: "0.9_3", Size: "44 KiB", NewSpans: VersionSpans("0.9_2", "0.9_3")},
			{Name: "libbaz", Old: "2.0_1", New: "2.0_2", Size: "8 KiB", NewSpans: VersionSpans("2.0_1", "2.0_2")},
		}}},
		Download:  "12.4 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "45.0 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start upgrade",
		},
	}
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "tui", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return raw
}

// TestSummaryGoldens is AC-050's frame half: the static S1 frame matches the
// checked-in grid and SGR bytes at every geometry and operation shape,
// including multi-tab, Held 5-column with a diff, scrollable, history, 60x18,
// 100x30 and 140x40. See testdata/tui/README.md for provenance.
func TestSummaryGoldens(t *testing.T) {
	cases := []struct {
		name  string
		model *SummaryModel
		w, h  int
	}{
		{"summary-80x24", goldenUpgrade(), 80, 24},
		{"summary-100x30", goldenUpgrade(), 100, 30},
		{"summary-140x40", goldenUpgrade(), 140, 40},
		{"summary-60x18", goldenRealistic60x18(), 60, 18},
		{"summary-multitab-100x30", goldenMultiTab(), 100, 30},
		{"held-diff-80x24", goldenHeldMixed(), 80, 24},
		{"scrollable-80x24", goldenScrollable(), 80, 24},
		{"history-80x24", goldenHistory(), 80, 24},
		{"downgrade-80x24", goldenDowngrade(), 80, 24},
		{"reinstall-80x24", goldenReInstall(), 80, 24},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := RenderSummary(c.model, c.w, c.h, DefaultTheme())
			if got, want := buf.Text(), string(readGolden(t, c.name+".txt")); got != want {
				t.Errorf("grid differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if got, want := buf.Flush(nil, true), readGolden(t, c.name+".sgr"); string(got) != string(want) {
				t.Errorf("SGR bytes differ\n--- got ---\n%q\n--- want ---\n%q", got, want)
			}
		})
	}
}

// TestSqueezedColumnsStayInsideTheFrame is the 60x18 floor with realistic
// names: the outer border and gutter survive every table row.
func TestSqueezedColumnsStayInsideTheFrame(t *testing.T) {
	const w, h = 60, 18
	buf := RenderSummary(goldenRealistic60x18(), w, h, DefaultTheme())
	for y := 1; y < h-1; y++ {
		if got := buf.Get(0, y).R; got != '┃' {
			t.Errorf("row %d left border = %q, want ┃", y, got)
		}
		if got := buf.Get(w-1, y).R; got != '┃' {
			t.Errorf("row %d right border = %q, want ┃", y, got)
		}
		line := []rune(buf.Grid()[y])
		if len(line) > w {
			t.Errorf("row %d is %d cells wide, want <= %d", y, len(line), w)
		}
	}
	// The table's right border and the gutter borders are untouched.
	for y := 5; y <= 7; y++ {
		for _, x := range []int{54, 55, 57} {
			if got := buf.Get(x, y).R; got != '┃' {
				t.Errorf("cell (%d,%d) = %q, want a ┃ border", x, y, got)
			}
		}
	}
}

// TestColumnLayoutSqueezed proves the deterministic shrink: no overlap and
// nothing past the interior.
func TestColumnLayoutSqueezed(t *testing.T) {
	natural := []int{26, 22, 22, 8}
	offsets, widths := columnLayout(natural, 49)
	prevEnd := -1
	for i := range widths {
		if widths[i] < 1 {
			t.Fatalf("column %d width = %d, want >= 1", i, widths[i])
		}
		if offsets[i] < prevEnd {
			t.Errorf("column %d starts at %d, previous ended at %d", i, offsets[i], prevEnd)
		}
		prevEnd = offsets[i] + widths[i]
	}
	if prevEnd > 49 {
		t.Errorf("columns consume %d cells, want <= 49", prevEnd)
	}
	// A fitting table keeps the SpaceAround offsets.
	fitOffsets, fitWidths := columnLayout([]int{8, 12, 8, 6}, 40)
	want := spaceAround([]int{8, 12, 8, 6}, 40)
	for i := range want {
		if fitOffsets[i] != want[i] || fitWidths[i] != []int{8, 12, 8, 6}[i] {
			t.Fatalf("fitting layout = %v/%v, want %v/%v", fitOffsets, fitWidths, want, []int{8, 12, 8, 6})
		}
	}
}

// TestReInstallRoleIsNotice pins ADR-0012's role for ReInstall names.
func TestReInstallRoleIsNotice(t *testing.T) {
	out := string(RenderSummary(goldenReInstall(), 80, 24, DefaultTheme()).Flush(nil, true))
	if !strings.Contains(out, "\x1b[0;1;93mzsh\x1b[0m") {
		t.Errorf("ReInstall name is not Notice (1;93):\n%q", out)
	}
	if strings.Contains(out, "\x1b[0;1;94mzsh") {
		t.Errorf("ReInstall name used Secondary (1;94)")
	}
}

// TestHeldDiffPaintsNoticeAndKeepsColumnsAligned covers the 5-column Held
// shape: a changed row paints the new-version diff, and a same-version row
// still uses the same columns.
func TestHeldDiffPaintsNoticeAndKeepsColumnsAligned(t *testing.T) {
	buf := RenderSummary(goldenHeldMixed(), 80, 24, DefaultTheme())
	out := string(buf.Flush(nil, true))
	if !strings.Contains(out, "5.2_\x1b[0;1;93m2\x1b[0m") {
		t.Errorf("Held row bash has no Notice revision diff")
	}
	if !strings.Contains(out, "3.3.\x1b[0;1;93m2_1\x1b[0m") {
		t.Errorf("Held row openssl has no Notice component diff")
	}
	// Compare rune columns: the selected row's ❯ is three UTF-8 bytes but
	// one cell, so byte offsets would lie.
	runeIndex := func(line, needle string) int {
		runes := []rune(line)
		needles := []rune(needle)
		for i := 0; i+len(needles) <= len(runes); i++ {
			match := true
			for j := range needles {
				if runes[i+j] != needles[j] {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
		return -1
	}
	grid := buf.Grid()
	bash, tzdata := -1, -1
	for _, line := range grid {
		if i := runeIndex(line, "bash"); i >= 0 {
			bash = i
		}
		if i := runeIndex(line, "tzdata"); i >= 0 {
			tzdata = i
		}
	}
	if bash < 0 || tzdata < 0 {
		t.Fatal("Held rows missing from the frame")
	}
	if bash != tzdata {
		t.Errorf("Held package column shifted: bash at %d, tzdata at %d", bash, tzdata)
	}
	reason := -1
	for _, line := range grid {
		if i := runeIndex(line, "Manual hold"); i >= 0 {
			if reason < 0 {
				reason = i
			} else if i != reason {
				t.Errorf("Held Reason column shifted: %d vs %d", i, reason)
			}
		}
	}
	if reason < 0 {
		t.Error("Held Reason column missing")
	}
}

// TestSummaryNoColorGolden is AC-055 at the frame level: no SGR bytes, same
// grid.
func TestSummaryNoColorGolden(t *testing.T) {
	buf := RenderSummary(goldenUpgrade(), 80, 24, DefaultTheme())
	if got, want := buf.Flush(nil, false), readGolden(t, "summary-80x24.nocolor.sgr"); string(got) != string(want) {
		t.Errorf("no-colour SGR bytes differ\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

// TestSummaryTooSmallGolden is AC-054's notice frame: the border and title
// stay, the body is one centred line, and no tab/table/footer is drawn.
func TestSummaryTooSmallGolden(t *testing.T) {
	buf := RenderSummary(goldenUpgrade(), 50, 15, DefaultTheme())
	if got, want := buf.Text(), string(readGolden(t, "toolarge-50x15.txt")); got != want {
		t.Errorf("50x15 grid differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	for _, forbidden := range []string{"Upgrade:", "Total download size:", "Disk space required:", "Package:"} {
		if strings.Contains(buf.Text(), forbidden) {
			t.Errorf("too-small body still draws %q", forbidden)
		}
	}
}

// TestSummaryModelNavigationClamps pins the interaction model #76's key loop
// drives: row clamps, tab resets and the accessors the package-info view uses.
func TestSummaryModelNavigationClamps(t *testing.T) {
	m := goldenMultiTab()
	if got := m.RowCount(); got != 2 {
		t.Fatalf("initial RowCount = %d, want 2", got)
	}
	m.MoveRow(1)
	if m.Sel != 1 {
		t.Fatalf("MoveRow(1) Sel = %d, want 1", m.Sel)
	}
	m.MoveRow(10)
	if m.Sel != 1 {
		t.Errorf("MoveRow(10) did not clamp: Sel = %d, want 1", m.Sel)
	}
	m.MoveRow(-10)
	if m.Sel != 0 {
		t.Errorf("MoveRow(-10) did not clamp: Sel = %d, want 0", m.Sel)
	}
	m.SetSelection(-3)
	if m.Sel != 0 {
		t.Errorf("SetSelection(-3) Sel = %d, want 0", m.Sel)
	}
	m.SetSelection(99)
	if m.Sel != 1 {
		t.Errorf("SetSelection(99) Sel = %d, want 1", m.Sel)
	}

	m.MoveTab(1)
	if m.Active != 1 || m.Sel != 0 {
		t.Fatalf("MoveTab(1) = active %d sel %d, want 1/0", m.Active, m.Sel)
	}
	if got := m.RowCount(); got != 1 {
		t.Errorf("Install RowCount = %d, want 1", got)
	}
	row, ok := m.SelectedRow()
	if !ok || row.Name != "figlet" {
		t.Errorf("Install SelectedRow = %+v/%v, want figlet", row, ok)
	}
	tab, ok := m.ActiveTab()
	if !ok || tab.Label != "Install" {
		t.Errorf("ActiveTab = %+v/%v, want Install", tab, ok)
	}
	m.SetSelection(9)
	if m.Sel != 0 {
		t.Errorf("single-row SetSelection(9) Sel = %d, want 0", m.Sel)
	}

	// The last tab has two rows: a clamped move must keep both the tab and
	// the non-zero selection.
	m.Active = len(m.Tabs) - 1
	m.Sel = 1
	m.MoveTab(1)
	if m.Active != len(m.Tabs)-1 || m.Sel != 1 {
		t.Errorf("clamped MoveTab changed state: active %d sel %d, want %d/1", m.Active, m.Sel, len(m.Tabs)-1)
	}
	m.MoveTab(-100)
	if m.Active != 0 || m.Sel != 0 {
		t.Errorf("MoveTab(-100) = active %d sel %d, want 0/0", m.Active, m.Sel)
	}

	empty := &SummaryModel{}
	empty.MoveRow(1)
	empty.MoveTab(1)
	empty.SetSelection(1)
	if empty.RowCount() != 0 {
		t.Errorf("empty RowCount = %d, want 0", empty.RowCount())
	}
	if _, ok := empty.SelectedRow(); ok {
		t.Error("empty SelectedRow reported a row")
	}
	if _, ok := empty.ActiveTab(); ok {
		t.Error("empty ActiveTab reported a tab")
	}
}

// TestRenderPackageInfo pins the package-info frame: the S1 border/title, the
// label/value body, the return help and the too-small branch.
func TestRenderPackageInfo(t *testing.T) {
	m := &PackageInfoModel{
		Title: "xbpretty Upgrade",
		Fields: []InfoField{
			{Label: "Package", Value: "libfoo"},
			{Label: "Installed", Value: "1.2_1"},
			{Label: "Available", Value: "1.3_1"},
		},
		Help: []string{"(q) back to summary"},
	}
	text := RenderPackageInfo(m, 80, 24, DefaultTheme()).Text()
	for _, want := range []string{"xbpretty Upgrade", "Package:", "libfoo", "Installed:", "1.2_1", "Available:", "1.3_1", "(q) back to summary", "┏", "┗"} {
		if !strings.Contains(text, want) {
			t.Errorf("package-info frame missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"Old Version:", "Total download size:", "❯", "Disk space required:"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("package-info frame drew %q:\n%s", unwanted, text)
		}
	}

	small := RenderPackageInfo(m, 50, 15, DefaultTheme()).Text()
	if !strings.Contains(small, TooSmall(50, 15)) {
		t.Errorf("too-small package info missing the notice:\n%s", small)
	}
	if !strings.Contains(small, "xbpretty Upgrade") {
		t.Errorf("too-small package info dropped the title:\n%s", small)
	}
	if strings.Contains(small, "Package:") {
		t.Errorf("too-small package info still draws the body:\n%s", small)
	}
}

// TestDowngradeTabShowsBothVersions is FR-069's history adaptation of the
// version-diff shape: a recorded Downgrade is a two-version operation, so its
// old version must stay on screen instead of collapsing to one column.
func TestDowngradeTabShowsBothVersions(t *testing.T) {
	model := &SummaryModel{
		Title:   "xbpretty History",
		History: true,
		Tabs: []Tab{{Label: "Downgrade", Rows: []PkgRow{
			{Name: "curl", Old: "8.17.0_1", New: "8.16.0_1", Size: "2 MiB", NewSpans: VersionSpans("8.17.0_1", "8.16.0_1")},
		}}},
	}
	frame := RenderSummary(model, 80, 24, DefaultTheme()).Text()
	for _, want := range []string{"Old Version:", "New Version:", "8.17.0_1", "8.16.0_1"} {
		if !strings.Contains(frame, want) {
			t.Errorf("downgrade frame missing %q:\n%s", want, frame)
		}
	}
}

// TestManyOperationTabsStayInsideTheBody is FR-069 with the full ten-group
// entry: the strip shifts to keep the active tab visible but may never paint
// over the border or the one-column inner padding.
func TestManyOperationTabsStayInsideTheBody(t *testing.T) {
	labels := []string{"Remove", "AutoRemove", "Purge", "AutoPurge", "Install", "Reinstall", "Upgrade", "Downgrade", "Configure", "Held"}
	tabs := make([]Tab, 0, len(labels))
	for _, label := range labels {
		tabs = append(tabs, Tab{Label: label, Rows: []PkgRow{{Name: "pkg", Old: "1.0_1", New: "1.0_1", Size: "1 MiB"}}})
	}
	for _, geometry := range []struct{ w, h int }{{80, 24}, {60, 18}} {
		model := &SummaryModel{Title: "xbpretty History", History: true, Tabs: tabs, Active: len(tabs) - 1}
		buf := RenderSummary(model, geometry.w, geometry.h, DefaultTheme())
		for _, y := range []int{1, 2} {
			if got := buf.Get(0, y).R; got != '┃' {
				t.Errorf("%dx%d row %d left border = %q, want ┃", geometry.w, geometry.h, y, got)
			}
			if got := buf.Get(1, y).R; got != ' ' {
				t.Errorf("%dx%d row %d inner padding = %q, want space", geometry.w, geometry.h, y, got)
			}
			if got := buf.Get(geometry.w-1, y).R; got != '┃' {
				t.Errorf("%dx%d row %d right border = %q, want ┃", geometry.w, geometry.h, y, got)
			}
			if got := buf.Get(geometry.w-2, y).R; got != ' ' {
				t.Errorf("%dx%d row %d right padding = %q, want space", geometry.w, geometry.h, y, got)
			}
		}
	}
}

// thumbRowsOf returns the rows that paint a scrollbar thumb cell anywhere in
// the frame, in order.
func thumbRowsOf(buf *Buffer) []int {
	var rows []int
	for y := 0; y < buf.H; y++ {
		for x := 0; x < buf.W; x++ {
			if buf.Get(x, y).R == '█' {
				rows = append(rows, y)
				break
			}
		}
	}
	return rows
}

// TestScrollbarThumbTracksScrollOffset: the thumb follows the first
// visible row, not the selection, so it is never clipped near the end.
func TestScrollbarThumbTracksScrollOffset(t *testing.T) {
	// 80x24 with the four-row footer: the table interior is rows 4..17.
	const firstInterior, lastInterior = 4, 17
	m := goldenScrollable()
	prevTop := -1
	for sel := 0; sel < 30; sel++ {
		m.Sel = sel
		rows := thumbRowsOf(RenderSummary(m, 80, 24, DefaultTheme()))
		if len(rows) != 6 {
			t.Fatalf("Sel %d: thumb rows = %v, want a 6-cell thumb", sel, rows)
		}
		if rows[0] < prevTop {
			t.Errorf("Sel %d: thumb moved up to row %d from %d", sel, rows[0], prevTop)
		}
		prevTop = rows[0]
		if sel <= 12 && rows[0] != firstInterior {
			t.Errorf("Sel %d: thumb starts at row %d, want the first interior row %d", sel, rows[0], firstInterior)
		}
		if sel == 29 && rows[len(rows)-1] != lastInterior {
			t.Errorf("Sel 29: thumb ends at row %d, want the last interior row %d", rows[len(rows)-1], lastInterior)
		}
	}
}

// TestScrollKeepsOffsetWhenMovingUp: after End, k moves the cursor
// up inside the window instead of scrolling it, and PgUp only scrolls once
// the cursor leaves the window.
func TestScrollKeepsOffsetWhenMovingUp(t *testing.T) {
	m := goldenScrollable()
	m.SetSelection(29)
	RenderSummary(m, 80, 24, DefaultTheme())
	offset := m.Offset
	if offset != 17 {
		t.Fatalf("offset after End = %d, want 17", offset)
	}
	m.MoveRow(-1)
	RenderSummary(m, 80, 24, DefaultTheme())
	if m.Offset != offset || m.Sel != 28 {
		t.Errorf("after k: offset %d sel %d, want offset %d sel 28", m.Offset, m.Sel, offset)
	}
	m.MoveRow(-10)
	RenderSummary(m, 80, 24, DefaultTheme())
	if m.Offset != offset || m.Sel != 18 {
		t.Errorf("after PgUp inside the window: offset %d sel %d, want offset %d sel 18", m.Offset, m.Sel, offset)
	}
	m.MoveRow(-10)
	RenderSummary(m, 80, 24, DefaultTheme())
	if m.Offset != 8 || m.Sel != 8 {
		t.Errorf("after PgUp past the window top: offset %d sel %d, want 8/8", m.Offset, m.Sel)
	}
}

// TestSqueezedColumnsKeepGap: the squeeze path keeps
// one blank cell between every pair of columns and shrinks Package first.
func TestSqueezedColumnsKeepGap(t *testing.T) {
	natural := []int{25, 17, 17, 7}
	const total = 49
	offsets, widths := columnLayout(natural, total)
	for i := range widths {
		if widths[i] < 1 {
			t.Fatalf("column %d width = %d, want >= 1", i, widths[i])
		}
		if i > 0 && offsets[i] < offsets[i-1]+widths[i-1]+1 {
			t.Errorf("column %d at %d touches column %d (ends at %d)", i, offsets[i], i-1, offsets[i-1]+widths[i-1])
		}
	}
	if end := offsets[len(offsets)-1] + widths[len(widths)-1]; end > total {
		t.Errorf("columns end at %d, want <= %d", end, total)
	}
	if widths[0] != 8 {
		t.Errorf("Package width = %d, want the 8-cell floor", widths[0])
	}
	if widths[1] != natural[1] || widths[2] != natural[2] {
		t.Errorf("Old/New widths = %d/%d, want the natural %d/%d", widths[1], widths[2], natural[1], natural[2])
	}
}

// TestSqueezedSpansStayInColumn: a version diff wider
// than its column clips on that column's edge and never paints over the
// neighbour on its left.
func TestSqueezedSpansStayInColumn(t *testing.T) {
	spans := []Span{{Text: "1.2_1", Role: RoleRegular}, {Text: "0", Role: RoleNotice}}
	for _, align := range []int{1, 2} {
		buf := NewBuffer(16, 1)
		buf.DrawText(0, 0, "OLDOLD", ZeroStyle)
		drawSpansAligned(buf, 6, 0, 4, spans, DefaultTheme(), align)
		if got := buf.Grid()[0]; got != "OLDOLD1.2_" {
			t.Errorf("align %d grid = %q, want %q", align, got, "OLDOLD1.2_")
		}
	}
}

// TestSqueezedSpanKeepsOldVersionIntact renders a 60x18 table whose New
// Version is wider than its squeezed column, and checks the Old Version text
// on the unselected row survives the diff.
func TestSqueezedSpanKeepsOldVersionIntact(t *testing.T) {
	const newVersion = "2.0_1234567890123456789012345678"
	model := &SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []Tab{{Label: "Upgrade", Rows: []PkgRow{
			{Name: "pkg-a", Old: "1.0_1", New: newVersion, Size: "1 MiB", NewSpans: VersionSpans("1.0_1", newVersion)},
			{Name: "tzdata", Old: "1.0_1", New: newVersion, Size: "1 MiB", NewSpans: VersionSpans("1.0_1", newVersion)},
		}}},
		Download:  "2 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "2 MiB",
	}
	grid := RenderSummary(model, 60, 18, DefaultTheme()).Grid()
	var row string
	for _, line := range grid {
		if strings.Contains(line, "tzdata") {
			row = line
		}
	}
	if !strings.Contains(row, "1.0_1") {
		t.Errorf("Old Version of the unselected row was overwritten:\n%s", strings.Join(grid, "\n"))
	}
}

// TestHelpKeepsConfirmHintAtFloor: at 60x18 the wrapped help block
// grows the footer instead of dropping the confirm hint.
func TestHelpKeepsConfirmHintAtFloor(t *testing.T) {
	frame := RenderSummary(goldenRealistic60x18(), 60, 18, DefaultTheme()).Text()
	if !strings.Contains(frame, "(y) start upgrade") {
		t.Errorf("60x18 frame dropped the confirm hint:\n%s", frame)
	}
}

// interiorWords joins a frame's interior (inside the one-cell border) into
// single-spaced words, so a notice wrapped across rows can be matched.
func interiorWords(buf *Buffer) string {
	var parts []string
	for y := 1; y < buf.H-1; y++ {
		var sb strings.Builder
		for x := 1; x < buf.W-1; x++ {
			sb.WriteRune(buf.Get(x, y).R)
		}
		parts = append(parts, sb.String())
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// assertFrameBorder proves the frame's corners and side glyphs survived.
func assertFrameBorder(t *testing.T, name string, buf *Buffer, tl, tr, bl, br, side rune) {
	t.Helper()
	if buf.Get(0, 0).R != tl || buf.Get(buf.W-1, 0).R != tr || buf.Get(0, buf.H-1).R != bl || buf.Get(buf.W-1, buf.H-1).R != br {
		t.Errorf("%s corners overwritten:\n%s", name, buf.Text())
	}
	for y := 1; y < buf.H-1; y++ {
		if buf.Get(0, y).R != side || buf.Get(buf.W-1, y).R != side {
			t.Errorf("%s row %d side overwritten:\n%s", name, y, buf.Text())
			return
		}
	}
}

// TestTooSmallKeepsBorder: the too-small notice wraps inside the
// border at every narrow width and never paints over it.
func TestTooSmallKeepsBorder(t *testing.T) {
	const h = 10
	for _, w := range []int{30, 40, 46} {
		want := TooSmall(w, h)
		info := &PackageInfoModel{Title: "xbpretty Upgrade", Fields: []InfoField{{Label: "Package", Value: "libfoo"}}}
		summaryBuf := RenderSummary(goldenUpgrade(), w, h, DefaultTheme())
		assertFrameBorder(t, "summary", summaryBuf, '┏', '┓', '┗', '┛', '┃')
		if got := interiorWords(summaryBuf); !strings.Contains(got, want) {
			t.Errorf("%dx%d summary notice = %q, want %q", w, h, got, want)
		}
		infoBuf := RenderPackageInfo(info, w, h, DefaultTheme())
		assertFrameBorder(t, "package info", infoBuf, '┏', '┓', '┗', '┛', '┃')
		if got := interiorWords(infoBuf); !strings.Contains(got, want) {
			t.Errorf("%dx%d package-info notice = %q, want %q", w, h, got, want)
		}
		fetchBuf := RenderFetch(&FetchModel{Rows: goldenMirrors()}, w, h, DefaultTheme())
		assertFrameBorder(t, "fetch", fetchBuf, '╭', '╮', '╰', '╯', '│')
		if got := interiorWords(fetchBuf); !strings.Contains(got, want) {
			t.Errorf("%dx%d fetch notice = %q, want %q", w, h, got, want)
		}
	}
}

// TestRegularBackgroundCoversFrame: a custom Regular background
// reaches every interior cell, including the frame's own glyphs.
func TestRegularBackgroundCoversFrame(t *testing.T) {
	theme := DefaultTheme()
	theme.Regular.BG = term.ANSI(4)
	const w, h = 80, 24
	frames := map[string]*Buffer{
		"summary":      RenderSummary(goldenUpgrade(), w, h, theme),
		"package info": RenderPackageInfo(&PackageInfoModel{Title: "xbpretty Upgrade", Fields: []InfoField{{Label: "Package", Value: "libfoo"}}}, w, h, theme),
		"fetch":        RenderFetch(&FetchModel{Rows: goldenMirrors()}, w, h, theme),
	}
	for name, buf := range frames {
		for y := 1; y < h-1; y++ {
			for x := 1; x < w-1; x++ {
				if got := buf.Get(x, y).S.BG; got != term.ANSI(4) {
					t.Errorf("%s cell (%d,%d) background = %+v, want ANSI(4)", name, x, y, got)
					break
				}
			}
		}
	}
}

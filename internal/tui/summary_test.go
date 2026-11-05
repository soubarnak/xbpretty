package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goldenUpgrade() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []Tab{{Label: "Upgrade", Rows: []PkgRow{
			{Name: "libfoo", Old: "1.2_1", New: "1.3_1", Size: "120 KiB", NewSpans: versionSpans("1.2_1", "1.3_1")},
			{Name: "libbar", Old: "0.9_2", New: "0.9_3", Size: "44 KiB", NewSpans: versionSpans("0.9_2", "0.9_3")},
			{Name: "libbaz", Old: "2.0_1", New: "2.0_2", Size: "8 KiB", NewSpans: versionSpans("2.0_1", "2.0_2")},
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

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

// TestSummaryGolden80x24 is AC-050's frame half: the static 80x24 S1 frame is
// byte-faithful to the ADR-0017/nala golden, for both the cell grid and the
// exact SGR payload.
func TestSummaryGolden80x24(t *testing.T) {
	buf := RenderSummary(goldenUpgrade(), 80, 24, DefaultTheme())
	if got, want := buf.Text(), string(readGolden(t, "summary-80x24.txt")); got != want {
		t.Errorf("80x24 grid differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if got, want := buf.Flush(nil, true), readGolden(t, "summary-80x24.sgr"); string(got) != string(want) {
		t.Errorf("80x24 SGR bytes differ\n--- got ---\n%q\n--- want ---\n%q", got, want)
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

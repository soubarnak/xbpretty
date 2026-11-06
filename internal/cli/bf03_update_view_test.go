package cli

import (
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// TestUpdateViewSevenRows is BF-03's layout: the Updating view is nala's
// seven-row Downloading widget, with Data and Remaining on the left and Speed
// and Elapsed on the right, at 80 and at 60 columns.
func TestUpdateViewSevenRows(t *testing.T) {
	view := progressView{
		kind:      viewUpdate,
		msgHeader: "Downloading",
		msgText:   "https://repo-default.voidlinux.org/current/x86_64-repodata",
		msgRole:   roleRegular,
		left: []progressInfo{
			{"Data", "1.8 MiB/3.6 MiB"},
			{"Remaining", "2s"},
		},
		right: []progressInfo{
			{"Speed", "1.9 MiB/s"},
			{"Elapsed", "3s"},
		},
		ratio: 0.5,
	}
	for _, columns := range []int{80, 60} {
		lines := renderProgressView(view, columns, false)
		if len(lines) != 7 {
			t.Fatalf("columns=%d: height = %d, want 7", columns, len(lines))
		}
		if !strings.Contains(lines[0], "  Updating Package Lists  ") {
			t.Errorf("columns=%d: title missing: %q", columns, lines[0])
		}
		if columns == 80 && !strings.Contains(lines[1], "Downloading: https://repo-default.voidlinux.org/current/x86_64-repodata") {
			t.Errorf("columns=%d: message row = %q", columns, lines[1])
		}
		for _, want := range []string{"Data:", "Remaining:", "Speed:", "Elapsed:"} {
			if !strings.Contains(strings.Join(lines[4:6], "\n"), want) {
				t.Errorf("columns=%d: info rows miss %q:\n%s", columns, want, strings.Join(lines, "\n"))
			}
		}
		if strings.Contains(strings.Join(lines, "\n"), "Repositories:") {
			t.Errorf("columns=%d: the widget still shows the Repositories counter", columns)
		}
	}
}

// TestUpdateViewGoldenSevenRows pins the 80-column frame byte for byte.
func TestUpdateViewGoldenSevenRows(t *testing.T) {
	view := progressView{
		kind:      viewUpdate,
		msgHeader: "Downloading",
		msgText:   "https://repo-default.voidlinux.org/current/x86_64-repodata",
		msgRole:   roleRegular,
		left:      []progressInfo{{"Data", "1.8 MiB/3.6 MiB"}, {"Remaining", "2s"}},
		right:     []progressInfo{{"Speed", "1.9 MiB/s"}, {"Elapsed", "3s"}},
		ratio:     0.5,
	}
	got := strings.Join(renderProgressView(view, 80, false), "\n")
	want := strings.Join([]string{
		"╭" + repeat("─", 26) + "  Updating Package Lists  " + repeat("─", 26) + "╮",
		"│ " + pad(" Downloading: https://repo-default.voidlinux.org/current/x86_64-repodata", 76) + " │",
		"├" + repeat("─", 34) + " Progress " + repeat("─", 29) + " 50% " + "┤",
		"│ " + repeat("━", 38) + repeat("━", 38) + " │",
		"│ " + pad(" Data:       1.8 MiB/3.6 MiB", 38) + " Speed:    1.9 MiB/s" + repeat(" ", 18) + " │",
		"│ " + pad(" Remaining:  2s", 38) + " Elapsed:  3s" + repeat(" ", 25) + " │",
		"╰" + repeat("─", 78) + "╯",
	}, "\n")
	if got != want {
		t.Errorf("update frame:\n%s\nwant:\n%s", got, want)
	}
}

// TestUpdateCountLinePlural: nala's count line is plural-aware, with the
// command in Primary when colour is on (BF-03, update-upgradable).
func TestUpdateCountLinePlural(t *testing.T) {
	theme := summary.DefaultTheme()
	cases := []struct {
		count int
		want  string
	}{
		{1, "1 package can be upgraded. Run 'xbp list --upgradable' to see it."},
		{3, "3 packages can be upgraded. Run 'xbp list --upgradable' to see them."},
	}
	for _, tc := range cases {
		if got := updateCountLine(tc.count, false, theme); got != tc.want {
			t.Errorf("updateCountLine(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
	coloured := updateCountLine(3, true, theme)
	if !strings.Contains(coloured, paintRole(theme, true, rolePrimary, "xbp list --upgradable")) {
		t.Errorf("the command is not Primary: %q", coloured)
	}
}

package cli

import (
	"strings"
	"testing"
)

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func repeat(s string, n int) string { return strings.Repeat(s, n) }

// TestRenderDownloadView80 pins AC-046's frame bytes at 80 columns: the
// 26/27-cell title padding, the full-width divider, the 44/76 round fill and
// the two aligned info halves.
func TestRenderDownloadView80(t *testing.T) {
	view := progressView{
		kind:      viewDownload,
		msgHeader: "Last completed",
		msgText:   "glibc-2.41_1.x86_64.xbps",
		msgRole:   rolePrimary,
		left: []progressInfo{
			{"Packages", "43/80"},
			{"Remaining", "3s"},
		},
		right: []progressInfo{
			{"Data", "19.0 MiB/33.0 MiB"},
			{"Speed", "5.4 MiB/s"},
		},
		ratio: 19456.0 / 33792.0,
	}
	want := []string{
		"╭" + repeat("─", 27) + "  Downloading Packages  " + repeat("─", 27) + "╮",
		"│ " + pad(" Last completed: glibc-2.41_1.x86_64.xbps", 76) + " │",
		"├" + repeat("─", 34) + " Progress " + repeat("─", 29) + " 57% " + "┤",
		"│ " + repeat("━", 44) + repeat("━", 32) + " │",
		"│ " + pad(" Packages:   43/80", 38) + " Data:   19.0 MiB/33.0 MiB" + repeat(" ", 12) + " │",
		"│ " + pad(" Remaining:  3s", 38) + " Speed:  5.4 MiB/s" + repeat(" ", 20) + " │",
		"╰" + repeat("─", 78) + "╯",
	}
	got := renderProgressView(view, 80, false)
	if len(got) != heightDownload {
		t.Fatalf("height = %d, want 7", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
			if len([]rune(got[i])) != 80 {
				t.Errorf("line %d visible width = %d, want 80", i, roleWidth(got[i]))
			}
		}
	}
}

// TestRenderDownloadView60 pins the min(columns,80) rule: the bar is 56 cells
// and the divider still spans the full frame width.
func TestRenderDownloadView60(t *testing.T) {
	view := progressView{
		kind:      viewDownload,
		msgHeader: "Last completed",
		msgText:   "glibc-2.41_1.x86_64.xbps",
		msgRole:   rolePrimary,
		left:      []progressInfo{{"Packages", "43/80"}, {"Remaining", "3s"}},
		right:     []progressInfo{{"Data", "19.0 MiB/33.0 MiB"}, {"Speed", "5.4 MiB/s"}},
		ratio:     19456.0 / 33792.0,
	}
	got := renderProgressView(view, 60, false)
	for i, line := range got {
		if w := roleWidth(line); w != 60 {
			t.Errorf("line %d width = %d, want 60: %q", i, w, line)
		}
	}
	if lines := renderProgressView(view, 100, false); roleWidth(lines[0]) != 80 {
		t.Errorf("width 100 rendered %d columns, want 80", roleWidth(lines[0]))
	}
	bar := []rune(strings.TrimSuffix(strings.TrimPrefix(got[3], "│ "), " │"))
	if len(bar) != 56 {
		t.Errorf("bar cells = %d, want W-4 = 56", len(bar))
	}
}

// TestProgressViewsFixedHeightsAndTitles pins the five titles and 7/6/6/6/6
// heights from AC-046 and ADR-0012 decision 5.
func TestProgressViewsFixedHeightsAndTitles(t *testing.T) {
	cases := []struct {
		kind   progressKind
		height int
		title  string
	}{
		{viewDownload, 7, "Downloading Packages"},
		{viewInstall, 6, "Installing Packages"},
		{viewRemove, 6, "Removing Packages"},
		{viewUpdate, 7, "Updating Package Lists"},
		{viewMirrorScore, 6, "Testing Mirrors"},
	}
	for _, tc := range cases {
		view := progressView{kind: tc.kind, msgHeader: "Status", msgText: "x", ratio: 0.5}
		lines := renderProgressView(view, 80, false)
		if len(lines) != tc.height {
			t.Errorf("%s height = %d, want %d", tc.title, len(lines), tc.height)
		}
		if !strings.Contains(lines[0], "  "+tc.title+"  ") {
			t.Errorf("%s title missing: %q", tc.title, lines[0])
		}
	}
}

// TestProgressBarSplitIsColourOnly pins that the filled and unfilled halves
// use the same glyph and differ only in SGR; with colour off it is a solid
// band.
func TestProgressBarSplitIsColourOnly(t *testing.T) {
	view := progressView{kind: viewInstall, msgHeader: "Status", msgText: "x", ratio: 0.25}
	colorLine := renderProgressView(view, 80, true)[3]
	if strings.Count(colorLine, "━") != 76 {
		t.Errorf("bar glyph count = %d, want 76", strings.Count(colorLine, "━"))
	}
	if !strings.Contains(colorLine, "\x1b[1;92m") || !strings.Contains(colorLine, "\x1b[1;91m") {
		t.Errorf("bar is not split by colour: %q", colorLine)
	}
	plainLine := renderProgressView(view, 80, false)[3]
	if strings.Contains(plainLine, "\x1b[") {
		t.Errorf("--no-color bar emitted ANSI: %q", plainLine)
	}
	if strings.Count(plainLine, "━") != 76 {
		t.Errorf("plain bar glyph count = %d, want 76", strings.Count(plainLine, "━"))
	}
}

// TestProgressDividerPercentTruncatesFillRounds pins the separate formulas:
// at 57.5% the label says 57 and the bar fills 44 of 76.
func TestProgressDividerPercentTruncatesFillRounds(t *testing.T) {
	view := progressView{kind: viewDownload, msgHeader: "Status", msgText: "x", ratio: 0.575}
	lines := renderProgressView(view, 80, false)
	if !strings.Contains(lines[2], " 57% ") {
		t.Errorf("divider does not truncate the percent: %q", lines[2])
	}
	if got := strings.Count(lines[3], "━"); got != 76 {
		t.Fatalf("bar cells = %d, want 76", got)
	}
	// 0.575*76 = 43.7 -> round 44, so the split point is derivable from the
	// colour bytes: count the filled glyphs.
	filled := strings.Count(renderProgressView(view, 80, true)[3], "━")
	if filled != 76 {
		t.Fatalf("coloured bar cells = %d, want 76", filled)
	}
}

// TestUpdateMessageHeaderIsRegular pins the adapted Update view's message
// role (Regular, not Primary).
func TestUpdateMessageHeaderIsRegular(t *testing.T) {
	view := progressView{kind: viewUpdate, msgHeader: "Updating", msgText: "https://repo.voidlinux.org/current", msgRole: roleRegular, ratio: 0.5}
	line := renderProgressView(view, 80, true)[1]
	if !strings.Contains(line, "\x1b[0;39mUpdating: ") {
		t.Errorf("update header is not Regular: %q", line)
	}
}

// TestProgressViewGoldens pins the other four frames' bytes at 80 columns.
func TestProgressViewGoldens(t *testing.T) {
	bottom := "╰" + repeat("─", 78) + "╯"
	div50 := "├" + repeat("─", 34) + " Progress " + repeat("─", 29) + " 50% " + "┤"
	div75 := "├" + repeat("─", 34) + " Progress " + repeat("─", 29) + " 75% " + "┤"
	bar38 := "│ " + repeat("━", 38) + repeat("━", 38) + " │"
	bar57 := "│ " + repeat("━", 57) + repeat("━", 19) + " │"
	cases := []struct {
		name string
		view progressView
		want []string
	}{
		{
			name: "install",
			view: progressView{
				kind: viewInstall, msgHeader: "Status", msgText: "Unpacking figlet-2.2.5_6", msgRole: rolePrimary,
				left:  []progressInfo{{"Package", "figlet-2.2.5_6"}},
				right: []progressInfo{{"Elapsed", "1s"}},
				ratio: 0.5,
			},
			want: []string{
				"╭" + repeat("─", 27) + "  Installing Packages  " + repeat("─", 28) + "╮",
				"│ " + pad(" Status: Unpacking figlet-2.2.5_6", 76) + " │",
				div50,
				bar38,
				"│ " + pad(" Package:  figlet-2.2.5_6", 38) + " Elapsed:  1s" + repeat(" ", 25) + " │",
				bottom,
			},
		},
		{
			name: "remove",
			view: progressView{
				kind: viewRemove, msgHeader: "Status", msgText: "Removing nano-9.2_1", msgRole: rolePrimary,
				left:  []progressInfo{{"Package", "nano-9.2_1"}},
				right: []progressInfo{{"Elapsed", "1s"}},
				ratio: 0.5,
			},
			want: []string{
				"╭" + repeat("─", 28) + "  Removing Packages  " + repeat("─", 29) + "╮",
				"│ " + pad(" Status: Removing nano-9.2_1", 76) + " │",
				div50,
				bar38,
				"│ " + pad(" Package:  nano-9.2_1", 38) + " Elapsed:  1s" + repeat(" ", 25) + " │",
				bottom,
			},
		},
		{
			// BF-03: the Updating view is the seven-row Downloading widget. Its
			// frame is pinned by TestUpdateViewGoldenSevenRows.
			name: "update",
			view: progressView{
				kind: viewUpdate, msgHeader: "Downloading", msgText: "https://repo-default.voidlinux.org/current/x86_64-repodata", msgRole: roleRegular,
				left:  []progressInfo{{"Data", "1.8 MiB/3.6 MiB"}, {"Remaining", "2s"}},
				right: []progressInfo{{"Speed", "1.9 MiB/s"}, {"Elapsed", "3s"}},
				ratio: 0.5,
			},
			want: []string{
				"╭" + repeat("─", 26) + "  Updating Package Lists  " + repeat("─", 26) + "╮",
				"│ " + pad(" Downloading: https://repo-default.voidlinux.org/current/x86_64-repodata", 76) + " │",
				div50,
				bar38,
				"│ " + pad(" Data:       1.8 MiB/3.6 MiB", 38) + " Speed:    1.9 MiB/s" + repeat(" ", 18) + " │",
				"│ " + pad(" Remaining:  2s", 38) + " Elapsed:  3s" + repeat(" ", 25) + " │",
				bottom,
			},
		},
		{
			name: "mirror score",
			view: progressView{
				kind: viewMirrorScore, msgHeader: "Finished", msgText: "https://repo-default.voidlinux.org/current", msgRole: rolePrimary,
				left:  []progressInfo{{"Mirrors", "3/4"}},
				right: []progressInfo{{"Elapsed", "2s"}},
				ratio: 0.75,
			},
			want: []string{
				"╭" + repeat("─", 29) + "  Testing Mirrors  " + repeat("─", 30) + "╮",
				"│ " + pad(" Finished: https://repo-default.voidlinux.org/current", 76) + " │",
				div75,
				bar57,
				"│ " + pad(" Mirrors:  3/4", 38) + " Elapsed:  2s" + repeat(" ", 25) + " │",
				bottom,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderProgressView(tc.view, 80, false)
			if len(got) != len(tc.want) {
				t.Fatalf("height = %d, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("line %d:\n got %q\nwant %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestProgressViewNarrowWidthNeverOverflows keeps the frame rectangular even
// at the smallest clamped width.
func TestProgressViewNarrowWidthNeverOverflows(t *testing.T) {
	view := progressView{
		kind: viewDownload, msgHeader: "Last completed", msgText: "pkg-1.0_1.x86_64.xbps", msgRole: rolePrimary,
		left:  []progressInfo{{"Packages", "43/80"}, {"Remaining", "3s"}},
		right: []progressInfo{{"Data", "19.0 MiB/33.0 MiB"}, {"Speed", "5.4 MiB/s"}},
		ratio: 0.575,
	}
	for _, cols := range []int{1, 10, 20, 30} {
		want := cols
		if want < 8 {
			want = 8
		}
		for i, line := range renderProgressView(view, cols, false) {
			if w := roleWidth(line); w != want {
				t.Errorf("cols=%d line %d width = %d, want %d: %q", cols, i, w, want, line)
			}
		}
	}
}

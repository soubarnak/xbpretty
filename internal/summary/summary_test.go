package summary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	testKiB = uint64(1024)
	testMiB = 1024 * testKiB
)

// goldenSummary is the fixture the rev 6 inline frames were captured from:
// one remove, two installs, one upgrade with a changed version, and one held
// package. The byte figures are chosen so the footer derives exactly the
// prototype frame's `12 MiB` download and `2 MiB` free (AC-044).
func goldenSummary() *Summary {
	rows := []Row{
		{
			Name: "nano", Version: "9.2_1", Action: actionRemove,
			InstalledSize: 3 * testMiB, HasInstalled: true,
		},
		{
			Name: "figlet", Version: "2.2.5_6", Action: actionInstall,
			DownloadSize: 92 * testKiB, HasDownload: true,
		},
		{
			Name: "cowsay", Version: "3.8.4_1", Action: actionInstall,
			DownloadSize: 19 * testKiB, HasDownload: true,
		},
		{
			Name: "voiced", Version: "1.3.0_1", Action: actionUpdate,
			DownloadSize: 12 * testMiB, HasDownload: true,
			InstalledSize: 12*testMiB + 120*testKiB, HasInstalled: true,
		},
		{
			Name: "acl", Version: "2.4.0_1", Action: actionHold,
			InstalledSize: 20 * testKiB, HasInstalled: true,
		},
	}
	old := map[string]Old{
		"voiced": {
			Version:       "1.2.0_1",
			InstalledSize: 11*testMiB + 120*testKiB,
			HasSize:       true,
		},
	}
	return Build(rows, nil, old)
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "summary", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return string(raw)
}

// TestRendererGoldens pins AC-044 byte-for-byte against the prototype's
// captured rev 6 frames: full width at 80 and 100 columns, natural widths when
// piped, the simple layout, and the no-colour frame.
func TestRendererGoldens(t *testing.T) {
	s := goldenSummary()
	cases := []struct {
		name     string
		golden   string
		renderer Renderer
	}{
		{
			name:   "80 columns",
			golden: "summary-80.sgr",
			renderer: Renderer{
				Color: true, Width: 80, Unit: SizeBinary, Theme: DefaultTheme(),
			},
		},
		{
			name:   "100 columns",
			golden: "summary-100.sgr",
			renderer: Renderer{
				Color: true, Width: 100, Unit: SizeBinary, Theme: DefaultTheme(),
			},
		},
		{
			name:   "piped natural widths",
			golden: "summary-80.piped.sgr",
			renderer: Renderer{
				Color: true, Width: 80, Piped: true, Unit: SizeBinary, Theme: DefaultTheme(),
			},
		},
		{
			name:   "piped natural widths at 100 rule columns",
			golden: "summary-100.piped.sgr",
			renderer: Renderer{
				Color: true, Width: 100, Piped: true, Unit: SizeBinary, Theme: DefaultTheme(),
			},
		},
		{
			name:   "simple",
			golden: "summary-simple.sgr",
			renderer: Renderer{
				Color: true, Width: 80, Simple: true, Unit: SizeBinary, Theme: DefaultTheme(),
			},
		},
		{
			name:   "no colour",
			golden: "summary-80.nocolor.txt",
			renderer: Renderer{
				Color: false, Width: 80, Unit: SizeBinary, Theme: DefaultTheme(),
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			if err := tt.renderer.Render(&b, s); err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got, want := b.String(), readGolden(t, tt.golden); got != want {
				t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", tt.golden, got, want)
			}
		})
	}
}

// TestFramesAreStrippedTheSameWay guards the .txt copies: the no-colour frame
// must be exactly the coloured frame with ANSI removed.
func TestFramesAreStrippedTheSameWay(t *testing.T) {
	if got, want := stripANSI(readGolden(t, "summary-80.sgr")), readGolden(t, "summary-80.txt"); got != want {
		t.Errorf("stripped 80-column frame differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

const (
	noticeSeq = "\x1b[1;93m"
	reset     = "\x1b[0m"
)

// TestVersionDiffNala pins AC-045 and FR-063's exact span boundaries.
func TestVersionDiffNala(t *testing.T) {
	tests := []struct {
		name   string
		oldVer string
		newVer string
		want   string
	}{
		{
			name:   "patch and revision change together",
			oldVer: "3.5.1_1", newVer: "3.5.4_1",
			want: "3.5." + noticeSeq + "4_1" + reset,
		},
		{
			name:   "two components change",
			oldVer: "2.38_1", newVer: "2.39_2",
			want: "2." + noticeSeq + "39_2" + reset,
		},
		{
			name:   "revision only",
			oldVer: "1.5.7_1", newVer: "1.5.7_2",
			want: "1.5.7_" + noticeSeq + "2" + reset,
		},
		{
			name:   "dot between highlighted components is plain",
			oldVer: "1.2.3_1", newVer: "1.3.0_1",
			want: "1." + noticeSeq + "3" + reset + "." + noticeSeq + "0_1" + reset,
		},
		{
			name:   "revision rides on the last highlighted component",
			oldVer: "1.0.0_1", newVer: "1.0.1_2",
			want: "1.0." + noticeSeq + "1_2" + reset,
		},
		{
			name:   "old version is not highlighted",
			oldVer: "9.9.9_9", newVer: "10.0.0_1",
			want: noticeSeq + "10" + reset + "." + noticeSeq + "0" + reset + "." + noticeSeq + "0_1" + reset,
		},
		{
			name:   "identical versions stay raw",
			oldVer: "1.0.0_1", newVer: "1.0.0_1",
			want: "1.0.0_1",
		},
		{
			name:   "no old version stays raw",
			oldVer: "", newVer: "1.0.0_1",
			want: "1.0.0_1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VersionDiff(tt.oldVer, tt.newVer, true); got != tt.want {
				t.Errorf("VersionDiff(%q, %q) = %q, want %q", tt.oldVer, tt.newVer, got, tt.want)
			}
		})
	}
	if got := VersionDiff("3.5.1_1", "3.5.4_1", false); got != "3.5.4_1" {
		t.Errorf("no-color VersionDiff = %q, want the raw new version", got)
	}
	if strings.Contains(VersionDiff("1.2.3_1", "1.3.0_1", true), "\x1b[38;5;202m") {
		t.Error("the retired ANSI-202 implementation is still in use")
	}
}

// TestHeldRowWithChangedVersionDiffs covers FR-063's held-row rule.
func TestHeldRowWithChangedVersionDiffs(t *testing.T) {
	s := Build(
		[]Row{{
			Name: "acl", Version: "2.5.0_1", Action: actionHold,
			InstalledSize: 20 * testKiB, HasInstalled: true,
		}},
		nil,
		map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 20 * testKiB, HasSize: true}},
	)
	var b strings.Builder
	if err := (Renderer{Color: true, Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "Old Version:") || !strings.Contains(out, "New Version:") {
		t.Errorf("held row with differing versions has no version columns:\n%s", out)
	}
	if !strings.Contains(out, "2.4.0_1") {
		t.Errorf("old version is missing or not raw:\n%s", out)
	}
	want := "2." + noticeSeq + "5" + reset + "." + noticeSeq + "0_1" + reset
	if !strings.Contains(out, want) {
		t.Errorf("held row diff = missing %q:\n%s", want, out)
	}
	if !strings.Contains(out, "Manual hold") {
		t.Errorf("held row is missing its reason:\n%s", out)
	}
}

// TestOnlySizeColumnIsRightAligned pins the table's alignment rule on a frame
// where the values have different lengths.
func TestOnlySizeColumnIsRightAligned(t *testing.T) {
	s := goldenSummary()
	var b strings.Builder
	if err := (Renderer{Color: true, Width: 100, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "voiced") || strings.Contains(line, "figlet") {
			if !strings.HasSuffix(line, " ") {
				t.Errorf("table row has no trailing space: %q", line)
			}
			if !strings.HasSuffix(strings.TrimRight(line, " "), "12 MiB") &&
				!strings.HasSuffix(strings.TrimRight(line, " "), "92 KiB") {
				t.Errorf("size is not the last, right-aligned cell: %q", line)
			}
		}
	}
}

// TestSizeUnitsUseStrictGreater is FR-062's size rule: [Ui] unit, precision 0,
// and a unit switches only when the value is strictly greater than the base.
func TestSizeUnitsUseStrictGreater(t *testing.T) {
	tests := []struct {
		in   uint64
		unit SizeUnit
		want string
	}{
		{0, SizeBinary, "0 B"},
		{1023, SizeBinary, "1023 B"},
		{1024, SizeBinary, "1024 B"},
		{1 * testMiB, SizeBinary, "1024 KiB"},
		{1*testMiB + 1, SizeBinary, "1 MiB"},
		{12 * testMiB, SizeBinary, "12 MiB"},
		{1 * testGiB, SizeBinary, "1024 MiB"},
		{1000, SizeDecimal, "1000 B"},
		{1000 * 1000, SizeDecimal, "1000 kB"},
		{1000 * 1000 * 1000, SizeDecimal, "1000 MB"},
	}
	for _, tt := range tests {
		if got := HumanUnit(tt.in, tt.unit); got != tt.want {
			t.Errorf("HumanUnit(%d, %q) = %q, want %q", tt.in, tt.unit, got, tt.want)
		}
	}
	if got := Human(1 * testMiB); got != "1024 KiB" {
		t.Errorf("Human(1 MiB) = %q, want 1024 KiB", got)
	}
}

const testGiB = 1024 * 1024 * 1024

// TestSectionsUseTheFixedOrderAndIncludeConfigure covers FR-062's section
// order, the separate Configure section, and the absent Downgrade section.
func TestSectionsUseTheFixedOrderAndIncludeConfigure(t *testing.T) {
	s := Build([]Row{
		{Name: "cfg", Version: "1.0_1", Action: actionConfigure, InstalledSize: 4096, HasInstalled: true},
		{Name: "new", Version: "1.0_1", Action: actionInstall, DownloadSize: 2048, HasDownload: true},
		{Name: "gone", Version: "1.0_1", Action: actionRemove, InstalledSize: 1024, HasInstalled: true},
	}, nil, nil)
	sections := s.sections()
	var labels []string
	for _, sec := range sections {
		labels = append(labels, sec.label)
	}
	want := []string{"Remove", "Install", "Configure"}
	if strings.Join(labels, ",") != strings.Join(want, ",") {
		t.Fatalf("sections = %v, want %v", labels, want)
	}
	for _, label := range labels {
		if label == "Downgrade" {
			t.Error("a Downgrade section rendered")
		}
	}
	var b strings.Builder
	if err := (Renderer{Color: true, Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "Configure") || !strings.Contains(out, "cfg") {
		t.Errorf("configure rows are not listed under Configure:\n%s", out)
	}
	if !strings.Contains(out, "Configure 1") {
		t.Errorf("footer does not count Configure:\n%s", out)
	}
}

// TestRemoveFamilyLabels covers the xbps remove-family mapping to nala's
// separate operation sections.
func TestRemoveFamilyLabels(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"remove", "Remove"},
		{"autoremove", "AutoRemove"},
		{"purge", "Purge"},
		{"autopurge", "AutoPurge"},
	}
	for _, tt := range tests {
		s := Build([]Row{{Name: "pkg", Version: "1_1", Action: actionRemove, InstalledSize: 1, HasInstalled: true}}, nil, nil)
		s.Command = tt.command
		var b strings.Builder
		if err := (Renderer{Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
			t.Fatalf("Render: %v", err)
		}
		if !strings.Contains(b.String(), "\n "+tt.want+"\n") {
			t.Errorf("command %s: section label %q missing:\n%s", tt.command, tt.want, b.String())
		}
	}
}

// TestRolesUseNalasPrefixes pins the nine-role default map and the rule that
// Regular is never emitted inline.
func TestRolesUseNalasPrefixes(t *testing.T) {
	s := goldenSummary()
	var b strings.Builder
	if err := (Renderer{Color: true, Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	for _, seq := range []string{
		"\x1b[1;39m", // Highlight: labels and headers
		"\x1b[1;94m", // Secondary: install/upgrade names
		"\x1b[1;91m", // Error: remove names
		"\x1b[1;93m", // Notice: held names and version diffs
	} {
		if !strings.Contains(out, seq) {
			t.Errorf("frame is missing %q", seq)
		}
	}
	if strings.Contains(out, "\x1b[0;39m") {
		t.Error("Regular is emitted on the inline path")
	}
	if strings.Contains(out, "\x1b[38;5;202m") || strings.Contains(out, "\x1b[1;38;5;202m") {
		t.Error("the retired ANSI-202 colour is emitted")
	}
}

// TestCustomThemeIsUsed proves the renderer consumes the [Color.theme] role
// map rather than hard-coded bytes.
func TestCustomThemeIsUsed(t *testing.T) {
	theme := DefaultTheme()
	theme.Notice = "\x1b[1;95m"
	s := goldenSummary()
	var b strings.Builder
	if err := (Renderer{Color: true, Width: 80, Theme: theme}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), theme.Notice+"acl"+reset) {
		t.Errorf("custom Notice role is not applied:\n%s", b.String())
	}
}

// TestBuildGroupsRows keeps the transaction accounting that the renderer
// depends on: remove bytes free, upgrade replacement bytes subtract, held rows
// are never fetched.
func TestBuildGroupsRows(t *testing.T) {
	rows := []Row{
		{Name: "figlet", Version: "2.2.5_6", Action: actionInstall, InstalledSize: 1000, HasInstalled: true, DownloadSize: 500, HasDownload: true},
		{Name: "acl", Version: "2.5.0_1", Action: actionUpdate, InstalledSize: 3000, HasInstalled: true, DownloadSize: 900, HasDownload: true},
		{Name: "wget", Version: "1.25.0_1", Action: actionRemove, InstalledSize: 2000, HasInstalled: true, DownloadSize: 1, HasDownload: true},
		{Name: "curl", Version: "8.22.0_1", Action: actionUpdate, InstalledSize: 4000, HasInstalled: true, DownloadSize: 700, HasDownload: true},
	}
	old := map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 2500, HasSize: true}}
	s := Build(rows, map[string]bool{"curl": true}, old)

	if len(s.Install) != 1 || s.Install[0].Name != "figlet" {
		t.Errorf("install group = %+v", s.Install)
	}
	if len(s.Upgrade) != 1 || s.Upgrade[0].Name != "acl" {
		t.Errorf("upgrade group = %+v", s.Upgrade)
	}
	if len(s.Remove) != 1 || s.Remove[0].Name != "wget" {
		t.Errorf("remove group = %+v", s.Remove)
	}
	if len(s.Hold) != 1 || s.Hold[0].Name != "curl" {
		t.Errorf("hold group = %+v", s.Hold)
	}
	if s.Download != 500+900 {
		t.Errorf("download = %d, want %d: it counts only fetched, non-held rows", s.Download, 500+900)
	}
	if got, want := s.NetDiskBytes(), int64(1000+3000-2500-2000); got != want {
		t.Errorf("NetDiskBytes = %d, want %d", got, want)
	}
}

func TestFooterCountsOnlyNonEmptySections(t *testing.T) {
	s := Build([]Row{{Name: "figlet", Version: "2.2.5_6", Action: actionInstall, DownloadSize: 1, HasDownload: true}}, nil, nil)
	var b strings.Builder
	if err := (Renderer{Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "Upgrade ") || strings.Contains(out, "Remove ") {
		t.Errorf("footer mentions empty sections:\n%s", out)
	}
	if !strings.Contains(out, "Install 1") {
		t.Errorf("footer missing Install count:\n%s", out)
	}
}

func TestNothingToDo(t *testing.T) {
	var b strings.Builder
	if err := (Renderer{Width: 80, Theme: DefaultTheme()}).Render(&b, Build(nil, nil, nil)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got, want := b.String(), "Nothing to do.\n"; got != want {
		t.Errorf("empty summary = %q, want %q", got, want)
	}
}

func TestNoColorEmitsNoANSI(t *testing.T) {
	s := goldenSummary()
	for _, simple := range []bool{false, true} {
		var b strings.Builder
		if err := (Renderer{Color: false, Width: 80, Simple: simple, Theme: DefaultTheme()}).Render(&b, s); err != nil {
			t.Fatalf("Render: %v", err)
		}
		if strings.Contains(b.String(), "\x1b[") {
			t.Errorf("simple=%v: no-colour frame contains ANSI:\n%q", simple, b.String())
		}
	}
}

func TestBoxCharacterSet(t *testing.T) {
	rounded := BoxCharacterSet("rounded")
	if rounded.TopLeft != "╭" || rounded.BottomRight != "╯" || rounded.Horizontal != "─" {
		t.Errorf("unexpected rounded chars: %+v", rounded)
	}
	if ascii := BoxCharacterSet("ascii"); ascii.TopLeft != "+" || ascii.Vertical != "|" {
		t.Errorf("unexpected ascii chars: %+v", ascii)
	}
}

func TestTruncateVisible(t *testing.T) {
	tests := []struct {
		input    string
		maxWidth int
		wantW    int
	}{
		{"", 10, 0},
		{"hello world", 5, 5},
		{"\x1b[1;32mTotal Packages:\x1b[0m 100", 8, 8},
		{"Phase: ⠋ Unpacking packages", 10, 10},
	}
	for _, tt := range tests {
		got := TruncateVisible(tt.input, tt.maxWidth)
		if w := utf8.RuneCountInString(stripANSI(got)); w != tt.wantW {
			t.Errorf("TruncateVisible(%q, %d) width = %d, want %d", tt.input, tt.maxWidth, w, tt.wantW)
		}
	}
}

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
		{
			name:   "no colour at 100 columns",
			golden: "summary-100.nocolor.txt",
			renderer: Renderer{
				Color: false, Width: 100, Unit: SizeBinary, Theme: DefaultTheme(),
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

// TestFramesAreStrippedTheSameWay guards the .txt copies: each no-colour text
// frame must be exactly its coloured frame with ANSI removed.
func TestFramesAreStrippedTheSameWay(t *testing.T) {
	for _, stem := range []string{
		"summary-80",
		"summary-100",
		"summary-80.piped",
		"summary-100.piped",
		"summary-simple",
	} {
		if got, want := stripANSI(readGolden(t, stem+".sgr")), readGolden(t, stem+".txt"); got != want {
			t.Errorf("stripped %s frame differs\n--- got ---\n%s\n--- want ---\n%s", stem, got, want)
		}
	}
}

const (
	noticeSeq = "\x1b[1;93m"
	reset     = "\x1b[0m"
)

// TestVersionDiffNala pins AC-045 and FR-063's exact span boundaries through
// the renderer's own version cell, the only producer of the diff spans.
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
	coloured := Renderer{Color: true, Theme: DefaultTheme()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := coloured.diffCell(tt.oldVer, tt.newVer); got != tt.want {
				t.Errorf("diffCell(%q, %q) = %q, want %q", tt.oldVer, tt.newVer, got, tt.want)
			}
		})
	}
	plain := Renderer{Color: false, Theme: DefaultTheme()}
	if got := plain.diffCell("3.5.1_1", "3.5.4_1"); got != "3.5.4_1" {
		t.Errorf("no-colour diff cell = %q, want the raw new version", got)
	}
	if strings.Contains(coloured.diffCell("1.2.3_1", "1.3.0_1"), "\x1b[38;5;202m") {
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
		{500000, SizeDecimal, "500 KB"},
		{1000 * 1000, SizeDecimal, "1000 KB"},
		{1000 * 1000 * 1000, SizeDecimal, "1000 MB"},
	}
	for _, tt := range tests {
		if got := HumanUnit(tt.in, tt.unit); got != tt.want {
			t.Errorf("HumanUnit(%d, %q) = %q, want %q", tt.in, tt.unit, got, tt.want)
		}
	}
	if got := WidgetSize(1 * testMiB); got != "1024.0 KiB" {
		t.Errorf("WidgetSize(1 MiB) = %q, want 1024.0 KiB", got)
	}
	if got := Human(1536); got != "1.5 KiB" {
		t.Errorf("Human(1536) = %q, want 1.5 KiB", got)
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
	sections := s.Sections()
	var labels []string
	for _, sec := range sections {
		labels = append(labels, sec.Label)
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
	if !strings.Contains(out, "\x1b[1;93mcfg\x1b[0m") {
		t.Errorf("configure package names are not Notice:\n%s", out)
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

// TestWidgetSizePrecision pins FR-064's widget size rule: Binary, precision 1.
func TestWidgetSizePrecision(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{1536, "1.5 KiB"},
		{436410, "426.2 KiB"},
		{19 * testMiB, "19.0 MiB"},
		{1024, "1024.0 B"},
	}
	for _, tt := range tests {
		if got := WidgetSize(tt.in); got != tt.want {
			t.Errorf("WidgetSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestSectionSize pins the operation-specific Size: source: the install
// family (including upgrade and held) shows the download size when present;
// remove and configure show the installed size they free.
func TestSectionSize(t *testing.T) {
	row := Row{
		InstalledSize: 4096, HasInstalled: true,
		DownloadSize: 1024, HasDownload: true,
	}
	tests := []struct {
		op   string
		want uint64
		ok   bool
	}{
		{"upgrade", 1024, true},
		{"held", 1024, true},
		{"reinstall", 1024, true},
		{"install", 1024, true},
		{"remove", 4096, true},
		{"configure", 4096, true},
	}
	for _, tt := range tests {
		got, ok := sectionSize(tt.op, row)
		if got != tt.want || ok != tt.ok {
			t.Errorf("sectionSize(%q) = (%d, %v), want (%d, %v)", tt.op, got, ok, tt.want, tt.ok)
		}
	}
	noDownload := Row{InstalledSize: 1234, HasInstalled: true}
	if got, ok := sectionSize("upgrade", noDownload); got != 1234 || !ok {
		t.Errorf("sectionSize falls back to installed size, got (%d, %v)", got, ok)
	}
	if _, ok := sectionSize("install", Row{}); ok {
		t.Error("sectionSize reports a size when the row has none")
	}
}

// TestHeldDiffFiveColumnSectionBytes pins the whole 5-column Held frame
// (versions differ + reason): old raw, new diffed, reason, right-aligned size.
func TestHeldDiffFiveColumnSectionBytes(t *testing.T) {
	s := Build(
		[]Row{{
			Name: "acl", Version: "2.5.0_1", Action: actionHold,
			InstalledSize: 20 * testKiB, HasInstalled: true,
		}},
		nil,
		map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 20 * testKiB, HasSize: true}},
	)
	var b strings.Builder
	if err := (Renderer{Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := strings.Join([]string{
		"================================================================================",
		" Held",
		"================================================================================",
		" Package:       Old Version:      New Version:      Reason:               Size: ",
		" acl            2.4.0_1           2.5.0_1           Manual hold          20 KiB ",
		"================================================================================",
		" Summary",
		"================================================================================",
		" Held 1",
		"",
		" Disk space required: 0 B",
		"",
	}, "\n") + "\n"
	if got := b.String(); got != want {
		t.Errorf("held diff frame differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestReInstallSection covers the separate ReInstall operation: Notice name,
// version-diff columns, download-based size and its own footer count.
func TestReInstallSection(t *testing.T) {
	s := Build(
		[]Row{{
			Name: "curl", Version: "8.22.0_1", Action: actionReinstall,
			DownloadSize: 274590, HasDownload: true,
			InstalledSize: 682921, HasInstalled: true,
		}},
		nil,
		map[string]Old{"curl": {Version: "8.21.0_1", InstalledSize: 680000, HasSize: true}},
	)
	var b strings.Builder
	if err := (Renderer{Color: true, Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if !strings.Contains(stripANSI(out), "\n ReInstall\n") {
		t.Errorf("missing ReInstall section:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[1;93mcurl\x1b[0m") {
		t.Errorf("reinstall package name is not Notice:\n%s", out)
	}
	if !strings.Contains(out, "Old Version:") || !strings.Contains(out, "New Version:") {
		t.Errorf("reinstall row is missing version columns:\n%s", out)
	}
	if !strings.Contains(out, "268 KiB") {
		t.Errorf("reinstall size does not use the download size:\n%s", out)
	}
	if !strings.Contains(out, "ReInstall 1") {
		t.Errorf("footer is missing the ReInstall count:\n%s", out)
	}
}

// TestReInstallDiskDeltaNetsOldSize covers FR-012's net delta for a reinstall:
// it replaces files the package already holds, so the installed size it
// occupies today is netted out rather than counted as new disk use.
func TestReInstallDiskDeltaNetsOldSize(t *testing.T) {
	const figletInstalled = 10 * testMiB
	s := Build(
		[]Row{{
			Name: "figlet", Version: "2.2.5_6", Action: actionReinstall,
			DownloadSize: 92 * testKiB, HasDownload: true,
			InstalledSize: figletInstalled, HasInstalled: true,
		}},
		nil,
		map[string]Old{"figlet": {Version: "2.2.5_6", InstalledSize: figletInstalled, HasSize: true}},
	)
	if got := s.NetDiskBytes(); got != 0 {
		t.Errorf("NetDiskBytes = %d, want 0 for a reinstall of an unchanged package", got)
	}
	var b strings.Builder
	if err := (Renderer{Width: 80, Theme: DefaultTheme()}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), "\n Disk space required: 0 B\n") {
		t.Errorf("reinstall disk line is not 0 B:\n%s", b.String())
	}
}

// TestNarrowWrapIndependentOfColour pins that colour does not change how a
// narrow header wraps. Plain headers word-wrap at spaces, so the coloured
// frame must strip to exactly the plain frame.
func TestNarrowWrapIndependentOfColour(t *testing.T) {
	s := Build(
		[]Row{{
			Name: "libreoffice-extensions", Version: "7.5.4.2_1", Action: actionHold,
			InstalledSize: 20 * testKiB, HasInstalled: true,
		}},
		nil,
		map[string]Old{"libreoffice-extensions": {Version: "7.5.3.1_1", InstalledSize: 20 * testKiB, HasSize: true}},
	)
	for _, width := range []int{50, 60} {
		var plain, coloured strings.Builder
		if err := (Renderer{Color: false, Width: width, Theme: DefaultTheme()}).Render(&plain, s); err != nil {
			t.Fatalf("Render plain at %d: %v", width, err)
		}
		if err := (Renderer{Color: true, Width: width, Theme: DefaultTheme()}).Render(&coloured, s); err != nil {
			t.Fatalf("Render coloured at %d: %v", width, err)
		}
		if strings.Contains(plain.String(), "Old Version:") {
			t.Fatalf("width %d: the fixture does not wrap the Old Version header:\n%s", width, plain.String())
		}
		if got, want := stripANSI(coloured.String()), plain.String(); got != want {
			t.Errorf("width %d: colour changes the wrap\n--- stripped colour ---\n%s\n--- plain ---\n%s", width, got, want)
		}
	}
}

// TestWrapPlainCellSqueezedTokenHasNoBlankLines guards the hard-split path: a
// single long token must not flush an empty line before its first chunk.
func TestWrapPlainCellSqueezedTokenHasNoBlankLines(t *testing.T) {
	token := strings.Repeat("x", 43)
	lines := wrapCell(token, 30)
	want := []string{strings.Repeat("x", 30), strings.Repeat("x", 13)}
	if len(lines) != len(want) {
		t.Fatalf("wrapCell(%q, 30) = %q, want %q", token, lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

// TestVisibleWidthIsRuneBased documents and pins the ASCII-only assumption:
// the inline surfaces count runes, not East-Asian display cells.
func TestVisibleWidthIsRuneBased(t *testing.T) {
	for _, s := range []string{"figlet", "é", "界"} {
		if got := VisibleWidth(s); got != 1 && got != len([]rune(s)) {
			t.Errorf("VisibleWidth(%q) = %d, want one column per rune", s, got)
		}
	}
	if got := VisibleWidth("a\x1b[1;39mb\x1b[0m"); got != 2 {
		t.Errorf("VisibleWidth ignores ANSI: got %d, want 2", got)
	}
}

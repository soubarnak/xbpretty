package summary

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func row(name, version, action string, installed, download uint64, hasInstalled, hasDownload bool) Row {
	return Row{
		Name: name, Version: version, Action: action,
		Repo:          "https://repo-default.voidlinux.org/current",
		InstalledSize: installed, DownloadSize: download,
		HasInstalled: hasInstalled, HasDownload: hasDownload,
	}
}

func TestHuman(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "1.5 KiB"},
		{436410, "426.2 KiB"},
		{1048576, "1 MiB"},
		{672593, "656.8 KiB"},
		{1073741824, "1 GiB"},
		{48200000, "46 MiB"},
		{33363382, "31.8 MiB"},
		{50541363, "48.2 MiB"},
	}
	for _, tt := range tests {
		if got := Human(tt.in); got != tt.want {
			t.Errorf("Human(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHumanDoesNotRoundUpToNextUnit(t *testing.T) {
	if got := Human(1023); strings.HasSuffix(got, "KiB") {
		t.Errorf("Human(1023) = %q, want a byte count", got)
	}
}

func TestBuildGroupsRows(t *testing.T) {
	rows := []Row{
		row("figlet", "2.2.5_6", "install", 672593, 94372, true, true),
		row("acl", "2.4.0_1", "update", 43080, 20653, true, true),
		row("wget", "1.25.0_1", "remove", 3673038, 824658, true, true),
		row("curl", "8.22.0_1", "update", 682921, 274590, true, true),
	}
	old := map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 41000, HasSize: true}}
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
	// wget is removed and curl is held, so neither is fetched: only the
	// install and the upgrade carry download bytes.
	wantDownload := uint64(94372 + 20653)
	if s.Download != wantDownload {
		t.Errorf("download = %d, want %d", s.Download, wantDownload)
	}
}

// TestBuildDiskDelta pins FR-012's net delta: new installed size minus old
// installed size minus removed size. A growing upgrade subtracts the bytes it
// replaces, not only the bytes a shrinking one gives back.
func TestBuildDiskDelta(t *testing.T) {
	rows := []Row{
		row("new", "1.0_1", "install", 1000, 500, true, true),
		row("grown", "2.0_1", "update", 3000, 900, true, true),
		row("gone", "1.0_1", "remove", 500, 100, true, true),
	}
	old := map[string]Old{"grown": {Version: "1.0_1", InstalledSize: 2800, HasSize: true}}
	s := Build(rows, nil, old)

	// 1000 installed, 3000 replacing 2800, 500 removed.
	want := int64(1000 + 3000 - 2800 - 500)
	if got := s.NetDiskBytes(); got != want {
		t.Errorf("NetDiskBytes = %d, want %d", got, want)
	}
}

// TestBuildDiskDeltaOnAShrinkingUpgrade is the same formula with the larger
// package on the old side.
func TestBuildDiskDeltaOnAShrinkingUpgrade(t *testing.T) {
	s := Build(
		[]Row{row("shrunk", "2.0_1", "update", 1000, 900, true, true)},
		nil,
		map[string]Old{"shrunk": {Version: "1.0_1", InstalledSize: 3000, HasSize: true}},
	)
	if got, want := s.NetDiskBytes(), int64(1000-3000); got != want {
		t.Errorf("NetDiskBytes = %d, want %d", got, want)
	}
}

func TestFooterFormat(t *testing.T) {
	rows := []Row{
		row("figlet", "2.2.5_6", "install", 672593, 94372, true, true),
		row("acl", "2.4.0_1", "update", 43080, 20653, true, true),
		row("wget", "1.25.0_1", "remove", 3673038, 824658, true, true),
	}
	old := map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 41000, HasSize: true}}
	s := Build(rows, map[string]bool{"heldpkg": false}, old)
	got := Renderer{}.footer(s)

	if !strings.HasPrefix(got, "1 install, 1 upgrade, 1 remove") {
		t.Errorf("footer %q is missing the counts", got)
	}
	if !strings.Contains(got, "download") {
		t.Errorf("footer %q is missing the download total", got)
	}
	if !strings.Contains(got, "disk") {
		t.Errorf("footer %q is missing the disk delta", got)
	}
	// Net = install 672593 + upgrade growth 2080 - removed 3673038 = negative.
	if !strings.Contains(got, "-") {
		t.Errorf("footer %q should show a negative net for a net-removing transaction", got)
	}
}

func TestFooterCountsOnlyNonEmptyGroups(t *testing.T) {
	s := Build([]Row{row("figlet", "2.2.5_6", "install", 672593, 94372, true, true)}, nil, nil)
	got := Renderer{}.footer(s)
	if strings.Contains(got, "upgrade") || strings.Contains(got, "remove") {
		t.Errorf("footer %q mentions empty groups", got)
	}
	if !strings.HasPrefix(got, "1 install") {
		t.Errorf("footer %q", got)
	}
}

func TestRenderOutputsNothingButSummary(t *testing.T) {
	var b strings.Builder
	s := Build([]Row{
		row("figlet", "2.2.5_6", "install", 672593, 94372, true, true),
	}, nil, nil)
	if err := (Renderer{Color: false}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "\x1b[") {
		t.Errorf("colour output without Color enabled:\n%q", out)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("missing group header:\n%s", out)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("missing package row:\n%s", out)
	}
}

func TestRenderUsesColorWhenEnabled(t *testing.T) {
	var b strings.Builder
	s := Build([]Row{row("figlet", "2.2.5_6", "install", 672593, 94372, true, true)}, nil, nil)
	if err := (Renderer{Color: true}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), "\x1b[") {
		t.Errorf("expected ANSI colour:\n%q", b.String())
	}
}

func TestRenderShowsOldToNewForUpgrades(t *testing.T) {
	var b strings.Builder
	s := Build(
		[]Row{row("acl", "2.5.0_1", "update", 43080, 20653, true, true)},
		nil,
		map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 41000, HasSize: true}},
	)
	if err := (Renderer{}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), "2.4.0_1 → 2.5.0_1") {
		t.Errorf("missing old → new version:\n%s", b.String())
	}
}

// TestHoldGroupListedSeparately covers a package the `xbps-query -H` map
// names as held: it is reported under Hold and nowhere else, because a held
// package is not part of the transaction.
func TestHoldGroupListedSeparately(t *testing.T) {
	s := Build(
		[]Row{row("curl", "8.22.0_1", "update", 682921, 274590, true, true)},
		map[string]bool{"curl": true},
		nil,
	)
	if len(s.Hold) != 1 {
		t.Fatalf("hold group = %+v", s.Hold)
	}
	if len(s.Upgrade) != 0 {
		t.Errorf("a held package is listed as an upgrade too: %+v", s.Upgrade)
	}
	if s.Download != 0 {
		t.Errorf("download = %d, want 0: a held package is not fetched", s.Download)
	}
	if s.AddBytes != 0 {
		t.Errorf("added bytes = %d, want 0: a held package is not written", s.AddBytes)
	}
	var b strings.Builder
	if err := (Renderer{}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), "Hold") {
		t.Errorf("missing Hold group:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "1 hold") {
		t.Errorf("footer missing hold count:\n%s", b.String())
	}
}

// TestBuildFilesHoldRowsUnderHold covers the `hold` action a plan carries for
// a package on hold (testdata/parser/0607/hold.txt): FR-011's Hold group is
// where those rows belong, rather than nowhere at all.
func TestBuildFilesHoldRowsUnderHold(t *testing.T) {
	s := Build([]Row{row("acl", "2.4.0_1", "hold", 43080, 20653, true, true)}, nil, nil)
	if len(s.Hold) != 1 || s.Hold[0].Name != "acl" {
		t.Fatalf("hold group = %+v", s.Hold)
	}
	c := s.Counts()
	if c.Hold != 1 || c.Install != 0 || c.Upgrade != 0 || c.Remove != 0 {
		t.Errorf("Counts = %+v", c)
	}
	var b strings.Builder
	if err := (Renderer{}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), "Hold") || !strings.Contains(b.String(), "acl") {
		t.Errorf("rendered summary is missing the held package:\n%s", b.String())
	}
}

func TestTotalAndCounts(t *testing.T) {
	s := Build([]Row{
		row("a", "1.0_1", "install", 1, 1, true, true),
		row("b", "1.0_1", "update", 1, 1, true, true),
	}, nil, nil)
	if s.Total() != 2 {
		t.Errorf("Total = %d, want 2", s.Total())
	}
	c := s.Counts()
	if c.Install != 1 || c.Upgrade != 1 || c.Remove != 0 || c.Hold != 0 {
		t.Errorf("Counts = %+v", c)
	}
}

// TestDownloadTotalCountsOnlyFetchedRows covers FR-012's download figure
// against the captured remove plan (testdata/parser/0607/remove.txt, whose row
// carries a download size of its own): nothing is fetched for a removal, so
// it contributes no download bytes and frees what it occupied instead.
func TestDownloadTotalCountsOnlyFetchedRows(t *testing.T) {
	s := Build([]Row{row("wget", "1.25.0_1", "remove", 3673038, 824658, true, true)}, nil, nil)

	if s.Download != 0 {
		t.Errorf("download = %d, want 0 for a removal", s.Download)
	}
	if got, want := s.NetDiskBytes(), int64(-3673038); got != want {
		t.Errorf("NetDiskBytes = %d, want %d", got, want)
	}
	if footer := (Renderer{}).footer(s); strings.Contains(footer, "download") {
		t.Errorf("footer %q reports a download for a removal", footer)
	}
}

// TestConfigureRowsCountedNotListed covers FR-011 for the `configure` action a
// plan carries for a package left configurable
// (testdata/parser/0607/configure.txt): counted, listed nowhere, and neither
// fetched nor written, because its files are already unpacked.
func TestConfigureRowsCountedNotListed(t *testing.T) {
	s := Build([]Row{
		row("libgcc", "14.2.1+20250405_4", "configure", 186384, 88847, true, true),
		row("figlet", "2.2.5_6", "install", 672593, 94372, true, true),
	}, nil, nil)

	if c := s.Counts(); c.Install != 2 {
		t.Errorf("Install count = %d, want 2: a configure action is counted", c.Install)
	}
	if s.Download != 94372 {
		t.Errorf("download = %d, want 94372: a configure row is not fetched", s.Download)
	}
	if s.AddBytes != 672593 {
		t.Errorf("added bytes = %d, want 672593: a configure row adds nothing to disk", s.AddBytes)
	}

	var b strings.Builder
	if err := (Renderer{}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(b.String(), "libgcc") {
		t.Errorf("the configure row is listed:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "2 install") {
		t.Errorf("the footer does not count the configure row:\n%s", b.String())
	}
}

// TestConfigureOnlyPlanListsNoGroup is the summary a plan of nothing but
// configure rows comes to: counted in the footer, with no group to head.
func TestConfigureOnlyPlanListsNoGroup(t *testing.T) {
	s := Build([]Row{row("libgcc", "14.2.1+20250405_4", "configure", 186384, 88847, true, true)}, nil, nil)
	var b strings.Builder
	if err := (Renderer{Plain: true}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got, want := b.String(), "1 install\n"; got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// TestFooterKeepsTheSpecsNouns pins the wording of FR-012's footer example:
// the nouns do not pluralise, so three installs read `3 install`.
func TestFooterKeepsTheSpecsNouns(t *testing.T) {
	s := Build([]Row{
		row("a", "1.0_1", "install", 1, 1, true, true),
		row("b", "1.0_1", "install", 1, 1, true, true),
		row("c", "1.0_1", "install", 1, 1, true, true),
		row("d", "1.0_1", "hold", 1, 1, true, true),
		row("e", "1.0_1", "hold", 1, 1, true, true),
	}, nil, nil)
	if got := (Renderer{}).footer(s); !strings.HasPrefix(got, "3 install, 2 hold") {
		t.Errorf("footer = %q, want FR-012's unpluralised nouns", got)
	}
}

// TestRenderAlignsColumnsByDisplayWidth covers FR-012's aligned columns. The
// arrow an upgrade version carries is one column wide and three bytes, so a
// group padded by byte count leaves the rows after it shifted.
func TestRenderAlignsColumnsByDisplayWidth(t *testing.T) {
	s := Build(
		[]Row{
			row("acl", "2.5.0_1", "update", 43080, 20653, true, true),
			row("bzip2", "1.0.8_2", "update", 150551, 61517, true, true),
		},
		nil,
		map[string]Old{"acl": {Version: "2.4.0_1", InstalledSize: 41000, HasSize: true}},
	)
	var b strings.Builder
	if err := (Renderer{Plain: true}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var columns []int
	for _, line := range strings.Split(b.String(), "\n") {
		size := ""
		switch {
		case strings.HasPrefix(line, "acl "):
			size = "20.2 KiB"
		case strings.HasPrefix(line, "bzip2 "):
			size = "60.1 KiB"
		default:
			continue
		}
		at := strings.Index(line, size)
		if at < 0 {
			t.Fatalf("row %q does not carry its size %q", line, size)
		}
		columns = append(columns, utf8.RuneCountInString(line[:at]))
	}
	if len(columns) != 2 {
		t.Fatalf("want a row for each package:\n%s", b.String())
	}
	if columns[0] != columns[1] {
		t.Errorf("the size column starts at %d on one row and %d on the other:\n%s",
			columns[0], columns[1], b.String())
	}
}

// TestRenderShowsARemovalsFreedBytesOnce covers the captured remove plan: the
// row reports the bytes the removal frees, once, rather than showing its
// download size and then repeating it. The footer's disk figure is the same
// number again, which is what FR-012 asks for.
func TestRenderShowsARemovalsFreedBytesOnce(t *testing.T) {
	s := Build([]Row{row("wget", "1.25.0_1", "remove", 3673038, 824658, true, true)}, nil, nil)
	var b strings.Builder
	if err := (Renderer{Plain: true}).Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "805.3 KiB") {
		t.Errorf("the removal is shown by its download size:\n%s", out)
	}
	got := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "wget ") {
			got = line
		}
	}
	if want := "wget  1.25.0_1  3.5 MiB"; got != want {
		t.Errorf("remove row = %q, want %q", got, want)
	}
}

func TestVersionDiffHighlighting(t *testing.T) {
	tests := []struct {
		name     string
		oldVer   string
		newVer   string
		color    bool
		wantDiff string
	}{
		{
			name:     "revision only",
			oldVer:   "2.38_1",
			newVer:   "2.38_2",
			color:    true,
			wantDiff: "2.38_\x1b[38;5;202m2\x1b[0m",
		},
		{
			name:     "patch bump",
			oldVer:   "6.8.0_1",
			newVer:   "6.8.101_1",
			color:    true,
			wantDiff: "6.8.\x1b[38;5;202m101\x1b[0m_1",
		},
		{
			name:     "patch bump with revision bump",
			oldVer:   "6.8.0_1",
			newVer:   "6.8.101_2",
			color:    true,
			wantDiff: "6.8.\x1b[38;5;202m101\x1b[0m_\x1b[38;5;202m2\x1b[0m",
		},
		{
			name:     "minor bump",
			oldVer:   "8.1.0_1",
			newVer:   "8.2.1_1",
			color:    true,
			wantDiff: "8.\x1b[38;5;202m2\x1b[0m.\x1b[38;5;202m1\x1b[0m_1",
		},
		{
			name:     "major bump",
			oldVer:   "124.0.1_1",
			newVer:   "125.0.2_1",
			color:    true,
			wantDiff: "\x1b[38;5;202m125\x1b[0m.\x1b[38;5;202m0\x1b[0m.\x1b[38;5;202m2\x1b[0m_1",
		},
		{
			name:     "fewer segments in new version",
			oldVer:   "1.2.3_1",
			newVer:   "1.2_1",
			color:    true,
			wantDiff: "1.\x1b[38;5;202m2\x1b[0m_1",
		},
		{
			name:     "color disabled",
			oldVer:   "2.38_1",
			newVer:   "2.38_2",
			color:    false,
			wantDiff: "2.38_2",
		},
		{
			name:     "no old version",
			oldVer:   "",
			newVer:   "1.0.0_1",
			color:    true,
			wantDiff: "1.0.0_1",
		},
		{
			name:     "identical versions",
			oldVer:   "1.0.0_1",
			newVer:   "1.0.0_1",
			color:    true,
			wantDiff: "1.0.0_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VersionDiff(tt.oldVer, tt.newVer, tt.color)
			if got != tt.wantDiff {
				t.Errorf("VersionDiff(%q, %q, %v) = %q, want %q", tt.oldVer, tt.newVer, tt.color, got, tt.wantDiff)
			}
		})
	}
}

func TestNalaRoundedBoxTables(t *testing.T) {
	rows := []Row{
		row("ripgrep", "14.1.0_1", "install", 2100000, 1100000, true, true),
		row("linux6.8", "6.8.101_1", "update", 154000000, 80000000, true, true),
		row("old-lib", "1.0.0_1", "remove", 450000, 0, true, false),
		row("xbps", "0.61.0_1", "hold", 1800000, 900000, true, true),
	}
	old := map[string]Old{
		"linux6.8": {Version: "6.8.0_1", InstalledSize: 150000000, HasSize: true},
	}
	s := Build(rows, nil, old)

	var b strings.Builder
	r := Renderer{Color: true, Width: 80}
	if err := r.Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()

	// Acceptance criteria:
	// 1. Unicode rounded box containers with headers and item counts
	plainOut := stripANSI(out)
	for _, wantHeader := range []string{
		"╭── Packages To Install (1)",
		"╭── Packages To Upgrade (1)",
		"╭── Packages To Remove (1)",
		"╭── Packages To Hold (1)",
	} {
		if !strings.Contains(plainOut, wantHeader) {
			t.Errorf("rendered output missing header %q:\n%s", wantHeader, out)
		}
	}

	// 2. Upgraded packages display side-by-side versions with arrow delimiter and ANSI 202 orange diff
	if !strings.Contains(plainOut, "6.8.0_1") || !strings.Contains(plainOut, "→") {
		t.Errorf("upgrade row missing arrow diff:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[38;5;202m") {
		t.Errorf("upgrade row missing ANSI 202 orange highlighting:\n%s", out)
	}

	// 3. Actions are color-coded (green for install, blue for upgrade, red for remove, orange for hold)
	if !strings.Contains(out, "\x1b[1;32m") {
		t.Errorf("missing bold green install color:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[1;34m") {
		t.Errorf("missing bold blue upgrade color:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[1;31m") {
		t.Errorf("missing bold red remove color:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[1;38;5;202m") && !strings.Contains(out, "\x1b[38;5;202m") {
		t.Errorf("missing bold orange hold color:\n%s", out)
	}

	// 4. Summary card in matching rounded container
	if !strings.Contains(plainOut, "╭── Summary") {
		t.Errorf("missing Summary card header:\n%s", out)
	}
	if !strings.Contains(plainOut, "1 install, 1 upgrade, 1 remove, 1 hold") {
		t.Errorf("Summary card missing action counts:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(plainOut), "download") || !strings.Contains(strings.ToLower(plainOut), "disk") {
		t.Errorf("Summary card missing download / disk metrics:\n%s", out)
	}

	// 5. Column alignment & pixel-perfect precision
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		// Each box line should start with ╭, │, ├, or ╰
		firstRune, _ := utf8.DecodeRuneInString(line)
		if firstRune != '╭' && firstRune != '│' && firstRune != '├' && firstRune != '╰' {
			t.Errorf("line %d does not start with box rune: %q", i, line)
		}
		w := visibleWidth(line)
		if w != 80 {
			t.Errorf("line %d visible width = %d, want 80: %q", i, w, line)
		}
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

func TestNalaPlaintextFallback(t *testing.T) {
	rows := []Row{
		row("ripgrep", "14.1.0_1", "install", 2100000, 1100000, true, true),
		row("linux6.8", "6.8.101_1", "update", 154000000, 80000000, true, true),
	}
	old := map[string]Old{
		"linux6.8": {Version: "6.8.0_1", InstalledSize: 150000000, HasSize: true},
	}
	s := Build(rows, nil, old)

	// Test Plain: true with Color: true
	var b strings.Builder
	r := Renderer{Color: true, Plain: true}
	if err := r.Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "╭") || strings.Contains(out, "╰") || strings.Contains(out, "│") {
		t.Errorf("plaintext fallback contains box characters:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plaintext fallback contains ANSI codes:\n%s", out)
	}
	if !strings.Contains(out, "Install") || !strings.Contains(out, "Upgrade") {
		t.Errorf("plaintext fallback missing standard headers:\n%s", out)
	}
}

func TestBoxCharacterSet(t *testing.T) {
	rounded := BoxCharacterSet("rounded")
	if rounded.TopLeft != "╭" || rounded.TopRight != "╮" || rounded.BottomLeft != "╰" || rounded.BottomRight != "╯" ||
		rounded.Horizontal != "─" || rounded.Vertical != "│" || rounded.DividerLeft != "├" || rounded.DividerRight != "┤" {
		t.Errorf("unexpected rounded chars: %+v", rounded)
	}

	classic := BoxCharacterSet("classic")
	if classic.TopLeft != "┌" || classic.TopRight != "┐" || classic.BottomLeft != "└" || classic.BottomRight != "┘" ||
		classic.Horizontal != "─" || classic.Vertical != "│" || classic.DividerLeft != "├" || classic.DividerRight != "┤" {
		t.Errorf("unexpected classic chars: %+v", classic)
	}

	ascii := BoxCharacterSet("ascii")
	if ascii.TopLeft != "+" || ascii.TopRight != "+" || ascii.BottomLeft != "+" || ascii.BottomRight != "+" ||
		ascii.Horizontal != "-" || ascii.Vertical != "|" || ascii.DividerLeft != "+" || ascii.DividerRight != "+" {
		t.Errorf("unexpected ascii chars: %+v", ascii)
	}

	def := BoxCharacterSet("")
	if def != rounded {
		t.Errorf("empty style should default to rounded, got %+v", def)
	}
}

func TestNalaClassicBoxTables(t *testing.T) {
	rows := []Row{
		row("ripgrep", "14.1.0_1", "install", 2100000, 1100000, true, true),
		row("linux6.8", "6.8.101_1", "update", 154000000, 80000000, true, true),
	}
	old := map[string]Old{
		"linux6.8": {Version: "6.8.0_1", InstalledSize: 150000000, HasSize: true},
	}
	s := Build(rows, nil, old)

	var b strings.Builder
	r := Renderer{Color: true, Width: 80, BoxStyle: "classic", ProgressBar: true}
	if err := r.Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	plainOut := stripANSI(out)

	if !strings.Contains(plainOut, "┌── Packages To Install (1)") {
		t.Errorf("missing classic install header:\n%s", out)
	}
	if !strings.Contains(plainOut, "┌── Packages To Upgrade (1)") {
		t.Errorf("missing classic upgrade header:\n%s", out)
	}
	if !strings.Contains(plainOut, "┌── Summary") {
		t.Errorf("missing classic summary card header:\n%s", out)
	}
	if strings.Contains(plainOut, "╭") || strings.Contains(plainOut, "╰") {
		t.Errorf("classic output should not contain rounded runes:\n%s", out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		firstRune, _ := utf8.DecodeRuneInString(line)
		if firstRune != '┌' && firstRune != '│' && firstRune != '├' && firstRune != '└' {
			t.Errorf("line %d does not start with classic box rune: %q", i, line)
		}
		w := visibleWidth(line)
		if w != 80 {
			t.Errorf("line %d visible width = %d, want 80: %q", i, w, line)
		}
	}
}

func TestNalaASCIIBoxTables(t *testing.T) {
	rows := []Row{
		row("ripgrep", "14.1.0_1", "install", 2100000, 1100000, true, true),
		row("linux6.8", "6.8.101_1", "update", 154000000, 80000000, true, true),
	}
	old := map[string]Old{
		"linux6.8": {Version: "6.8.0_1", InstalledSize: 150000000, HasSize: true},
	}
	s := Build(rows, nil, old)

	var b strings.Builder
	r := Renderer{Color: true, Width: 80, BoxStyle: "ascii"}
	if err := r.Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()
	plainOut := stripANSI(out)

	if !strings.Contains(plainOut, "+-- Packages To Install (1)") {
		t.Errorf("missing ascii install header:\n%s", out)
	}
	if !strings.Contains(plainOut, "+-- Packages To Upgrade (1)") {
		t.Errorf("missing ascii upgrade header:\n%s", out)
	}
	if !strings.Contains(plainOut, "+-- Summary") {
		t.Errorf("missing ascii summary card header:\n%s", out)
	}
	if strings.Contains(plainOut, "╭") || strings.Contains(plainOut, "┌") {
		t.Errorf("ascii output should not contain unicode box runes:\n%s", out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		firstRune, _ := utf8.DecodeRuneInString(line)
		if firstRune != '+' && firstRune != '|' {
			t.Errorf("line %d does not start with ascii box char: %q", i, line)
		}
		w := visibleWidth(line)
		if w != 80 {
			t.Errorf("line %d visible width = %d, want 80: %q", i, w, line)
		}
	}
}

func TestNalaBoxesWhenNoColor(t *testing.T) {
	rows := []Row{
		row("ripgrep", "14.1.0_1", "install", 2100000, 1100000, true, true),
		row("linux6.8", "6.8.101_1", "update", 154000000, 80000000, true, true),
	}
	old := map[string]Old{
		"linux6.8": {Version: "6.8.0_1", InstalledSize: 150000000, HasSize: true},
	}
	s := Build(rows, nil, old)

	var b strings.Builder
	r := Renderer{Color: false, Plain: false, Width: 80}
	if err := r.Render(&b, s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := b.String()

	if !strings.Contains(out, "╭") || !strings.Contains(out, "╰") || !strings.Contains(out, "│") {
		t.Errorf("expected box layout when Color: false and Plain: false, got:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR box layout contains ANSI escape codes:\n%s", out)
	}
	if !strings.Contains(out, "6.8.0_1 → 6.8.101_1") {
		t.Errorf("upgrade row missing old -> new delta:\n%s", out)
	}
}

func TestTruncateVisible(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		wantW    int
	}{
		{"empty", "", 10, 0},
		{"zero width", "hello", 0, 0},
		{"short ascii", "hello", 10, 5},
		{"exact ascii", "hello", 5, 5},
		{"long ascii", "hello world", 5, 5},
		{"colored string", "\x1b[1;32mTotal Packages:\x1b[0m 100", 8, 8},
		{"unicode runes", "Phase: ⠋ Unpacking packages", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateVisible(tt.input, tt.maxWidth)
			w := VisibleWidth(got)
			if w != tt.wantW {
				t.Errorf("TruncateVisible(%q, %d) visible width = %d, want %d (got %q)",
					tt.input, tt.maxWidth, w, tt.wantW, got)
			}
		})
	}
}

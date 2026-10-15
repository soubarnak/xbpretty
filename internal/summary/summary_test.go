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
	if err := (Renderer{}).Render(&b, s); err != nil {
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
	if err := (Renderer{}).Render(&b, s); err != nil {
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
	if err := (Renderer{}).Render(&b, s); err != nil {
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

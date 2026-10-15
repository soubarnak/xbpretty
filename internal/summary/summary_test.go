package summary

import (
	"strings"
	"testing"
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
		{0, "0B"},
		{1, "1B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1KiB"},
		{1536, "1.5KiB"},
		{436410, "426.2KiB"},
		{1048576, "1MiB"},
		{672593, "656.8KiB"},
		{1073741824, "1GiB"},
		{48200000, "46MiB"},
		{33363382, "31.8MiB"},
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
	if len(s.Upgrade) != 2 {
		t.Errorf("upgrade group = %+v", s.Upgrade)
	}
	if len(s.Remove) != 1 || s.Remove[0].Name != "wget" {
		t.Errorf("remove group = %+v", s.Remove)
	}
	if len(s.Hold) != 1 || s.Hold[0].Name != "curl" {
		t.Errorf("hold group = %+v", s.Hold)
	}
	wantDownload := uint64(94372 + 20653 + 824658 + 274590)
	if s.Download != wantDownload {
		t.Errorf("download = %d, want %d", s.Download, wantDownload)
	}
}

func TestBuildDiskDelta(t *testing.T) {
	rows := []Row{
		row("new", "1.0_1", "install", 1000, 500, true, true),
		row("grown", "2.0_1", "update", 3000, 900, true, true),
		row("gone", "1.0_1", "remove", 500, 100, true, true),
	}
	old := map[string]Old{"grown": {Version: "1.0_1", InstalledSize: 2800, HasSize: true}}
	s := Build(rows, nil, old)

	// new 1000 + grown 3000, minus removed 500. The upgrade's old 2800 bytes
	// are superseded by the new 3000, which is already counted as added.
	want := int64(1000 + 3000 - 500)
	if got := s.NetDiskBytes(); got != want {
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

func TestHoldGroupListedSeparately(t *testing.T) {
	s := Build(
		[]Row{row("curl", "8.22.0_1", "update", 682921, 274590, true, true)},
		map[string]bool{"curl": true},
		nil,
	)
	if len(s.Hold) != 1 {
		t.Fatalf("hold group = %+v", s.Hold)
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

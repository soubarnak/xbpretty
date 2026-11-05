package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// TestSummaryScreenGateMatrix is AC-049's engine half: exactly one cell of the
// gate matrix opens S1, and every other cell falls back to text.
func TestSummaryScreenGateMatrix(t *testing.T) {
	base := summaryGate{
		Mode:       config.ModeTui,
		StdoutTTY:  true,
		StdinTTY:   true,
		Term:       "xterm-256color",
		UTF8Locale: true,
		Root:       true,
		Rows:       3,
	}
	if !summaryScreenAllowed(base) {
		t.Fatal("the full gate did not open S1")
	}
	cases := []struct {
		name  string
		alter func(*summaryGate)
	}{
		{"Auto mode", func(g *summaryGate) { g.Mode = config.ModeAuto }},
		{"Plain mode", func(g *summaryGate) { g.Mode = config.ModePlain }},
		{"TERM=dumb", func(g *summaryGate) { g.Term = "dumb" }},
		{"TERM=DUMB case-insensitive", func(g *summaryGate) { g.Term = "DuMb" }},
		{"stdout not a tty", func(g *summaryGate) { g.StdoutTTY = false }},
		{"stdin not a tty", func(g *summaryGate) { g.StdinTTY = false }},
		{"non-UTF-8 locale", func(g *summaryGate) { g.UTF8Locale = false }},
		{"not root", func(g *summaryGate) { g.Root = false }},
		{"empty plan", func(g *summaryGate) { g.Rows = 0 }},
		{"hold-only plan", func(g *summaryGate) { g.HoldOnly = true }},
		{"-y", func(g *summaryGate) { g.AssumeYes = true }},
		{"-n", func(g *summaryGate) { g.AssumeNo = true }},
		{"--simple", func(g *summaryGate) { g.Simple = true }},
		{"--dry-run", func(g *summaryGate) { g.DryRun = true }},
	}
	for _, c := range cases {
		g := base
		c.alter(&g)
		if summaryScreenAllowed(g) {
			t.Errorf("%s opened S1", c.name)
		}
	}
}

// TestTuiFooterUsesTheConfiguredUnit proves the S1 footer honours [Ui] unit;
// the ADR golden is the Binary rendering of the same totals.
func TestTuiFooterUsesTheConfiguredUnit(t *testing.T) {
	sum := &summary.Summary{
		Command: "upgrade",
		Upgrade: []summary.Row{
			{Name: "libfoo", Version: "1.3_1", Action: "update", InstalledSize: 2 * 1024 * 1024, HasInstalled: true, DownloadSize: 1024 * 1024, HasDownload: true},
		},
		Download: 2 * 1024 * 1024,
		AddBytes: 4 * 1024 * 1024,
	}
	model := tuiModelFromSummary(sum, "upgrade", summary.SizeDecimal)
	if !strings.Contains(model.Download, "MB") || strings.Contains(model.Download, "MiB") {
		t.Errorf("Decimal footer download = %q, want MB units", model.Download)
	}
	if !strings.Contains(model.Disk, "MB") || strings.Contains(model.Disk, "MiB") {
		t.Errorf("Decimal footer disk = %q, want MB units", model.Disk)
	}
	binary := tuiModelFromSummary(sum, "upgrade", summary.SizeBinary)
	if !strings.Contains(binary.Download, "MiB") {
		t.Errorf("Binary footer download = %q, want MiB units", binary.Download)
	}
}

// TestPlanToTuiBuildsTheGoldenShape pins the model conversion the frame
// renders: operation order, titles, version diff spans and footer derivation.
func TestPlanToTuiBuildsTheGoldenShape(t *testing.T) {
	st := &state{unit: summary.SizeBinary, opts: &options{command: "upgrade"}}
	sum := &summary.Summary{
		Command: "upgrade",
		Upgrade: []summary.Row{
			{Name: "libfoo", Version: "1.3_1", Action: "update", DownloadSize: 122880, HasDownload: true},
		},
		Old: map[string]summary.Old{"libfoo": {Version: "1.2_1", HasSize: true}},
	}
	model := tuiModelFromSummary(sum, "upgrade", st.unit)
	if model.Title != "xbpretty Upgrade" {
		t.Errorf("title = %q", model.Title)
	}
	if len(model.Tabs) != 1 || model.Tabs[0].Label != "Upgrade" {
		t.Fatalf("tabs = %+v", model.Tabs)
	}
	row := model.Tabs[0].Rows[0]
	if row.Old != "1.2_1" || row.New != "1.3_1" || row.Size != "120 KiB" {
		t.Errorf("row = %+v", row)
	}
	if len(row.NewSpans) == 0 {
		t.Errorf("upgrade row has no version diff spans")
	}
	if model.DiskLabel != "Disk space required:" {
		t.Errorf("disk label = %q", model.DiskLabel)
	}
}

// infoField returns one package-info field by label.
func infoField(t *testing.T, info *tui.PackageInfoModel, label string) (string, bool) {
	t.Helper()
	if info == nil {
		t.Fatal("summaryPackageInfo returned nil")
	}
	for _, f := range info.Fields {
		if f.Label == label {
			return f.Value, true
		}
	}
	return "", false
}

// TestSummaryPackageInfoLabels pins the operation-aware version label: only
// the install family reports an "Available" version.
func TestSummaryPackageInfoLabels(t *testing.T) {
	cases := []struct {
		name      string
		tab       tui.Tab
		wantLabel string
		wantValue string
		absent    string
	}{
		{
			name:      "remove reports the installed version",
			tab:       tui.Tab{Label: "Remove", Rows: []tui.PkgRow{{Name: "nano", Old: "9.2_1", Size: "3 MiB"}}},
			wantLabel: "Version", wantValue: "9.2_1", absent: "Available",
		},
		{
			name:      "autoremove reports the installed version",
			tab:       tui.Tab{Label: "AutoRemove", Rows: []tui.PkgRow{{Name: "nano", Old: "9.2_1", Size: "3 MiB"}}},
			wantLabel: "Version", wantValue: "9.2_1", absent: "Available",
		},
		{
			name:      "held reports the held version",
			tab:       tui.Tab{Label: "Held", Rows: []tui.PkgRow{{Name: "bash", Old: "5.2_1", New: "5.2_1", Reason: "Manual hold", Size: "1 MiB"}}},
			wantLabel: "Version", wantValue: "5.2_1", absent: "Available",
		},
		{
			name:      "configure reports the configured version",
			tab:       tui.Tab{Label: "Configure", Rows: []tui.PkgRow{{Name: "zsh", New: "5.9_3", Size: "2 MiB"}}},
			wantLabel: "Version", wantValue: "5.9_3", absent: "Available",
		},
		{
			name:      "upgrade reports the available version",
			tab:       tui.Tab{Label: "Upgrade", Rows: []tui.PkgRow{{Name: "glibc", Old: "2.41_1", New: "2.41_2", Size: "12 MiB"}}},
			wantLabel: "Available", wantValue: "2.41_2", absent: "",
		},
		{
			name:      "install reports the available version",
			tab:       tui.Tab{Label: "Install", Rows: []tui.PkgRow{{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"}}},
			wantLabel: "Available", wantValue: "2.2.5_6", absent: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &tui.SummaryModel{Title: "xbpretty Undo", Tabs: []tui.Tab{tc.tab}}
			info := summaryPackageInfo(model)
			got, ok := infoField(t, info, tc.wantLabel)
			if !ok || got != tc.wantValue {
				t.Errorf("%s = %q/%v, want %q", tc.wantLabel, got, ok, tc.wantValue)
			}
			if tc.absent != "" {
				if _, ok := infoField(t, info, tc.absent); ok {
					t.Errorf("package info unexpectedly has %s", tc.absent)
				}
			}
			if tc.tab.Label == "Upgrade" {
				if installed, ok := infoField(t, info, "Installed"); !ok || installed != "2.41_1" {
					t.Errorf("Upgrade Installed = %q/%v, want 2.41_1", installed, ok)
				}
			}
		})
	}
}

// TestSummaryPackageInfoEmptySelection proves `s` with no row is a no-op.
func TestSummaryPackageInfoEmptySelection(t *testing.T) {
	if info := summaryPackageInfo(&tui.SummaryModel{}); info != nil {
		t.Errorf("summaryPackageInfo on an empty model = %+v, want nil", info)
	}
}

// TestTUITestHooksAreAbsentFromOrdinaryBuilds keeps the frame-dump and forced
// failure variables out of the release binary.
func TestTUITestHooksAreAbsentFromOrdinaryBuilds(t *testing.T) {
	ordinary, err := os.ReadFile(buildBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := os.ReadFile(taggedBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"XBPRETTY_TEST_TUI_DUMP", "XBPRETTY_TEST_TUI_FAIL"} {
		if bytes.Contains(ordinary, []byte(name)) {
			t.Errorf("the ordinary build contains %s; the TUI hook leaked out of xbpretty_testroot", name)
		}
		if !bytes.Contains(tagged, []byte(name)) {
			t.Errorf("the xbpretty_testroot build is missing %s", name)
		}
	}
}

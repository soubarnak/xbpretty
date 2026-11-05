package cli

import (
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/summary"
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

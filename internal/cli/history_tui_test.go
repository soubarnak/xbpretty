package cli

import (
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/summary"
)

// TestHistoryScreenGate is FR-067's S2 predicate: the full-screen gate with no
// root, plan or transaction-flag requirement. Only mode, the two TTYs, TERM and
// the locale decide.
func TestHistoryScreenGate(t *testing.T) {
	base := summaryGate{
		Mode:       config.ModeTui,
		StdoutTTY:  true,
		StdinTTY:   true,
		Term:       "xterm-256color",
		UTF8Locale: true,
	}
	if !historyScreenAllowed(base) {
		t.Fatal("the full-screen gate did not open the history detail")
	}
	// S2 is read-only: root and every transaction flag are irrelevant.
	open := []struct {
		name  string
		alter func(*summaryGate)
	}{
		{"non-root", func(g *summaryGate) { g.Root = false }},
		{"-y present", func(g *summaryGate) { g.AssumeYes = true }},
		{"-n present", func(g *summaryGate) { g.AssumeNo = true }},
		{"--simple present", func(g *summaryGate) { g.Simple = true }},
		{"--dry-run present", func(g *summaryGate) { g.DryRun = true }},
	}
	for _, c := range open {
		g := base
		c.alter(&g)
		if !historyScreenAllowed(g) {
			t.Errorf("%s closed the history detail", c.name)
		}
	}
	closed := []struct {
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
	}
	for _, c := range closed {
		g := base
		c.alter(&g)
		if historyScreenAllowed(g) {
			t.Errorf("%s opened the history detail", c.name)
		}
	}
}

// TestHistoryToTuiGroupsInFixedOrder is FR-069's model contract: the tabs are
// the entry's non-empty operation groups in FR-032's fixed order, with the
// read-only title and the counts-only help block, and no download/disk data.
func TestHistoryToTuiGroupsInFixedOrder(t *testing.T) {
	entry := history.Entry{
		SchemaVersion: history.SchemaVersion,
		ID:            7,
		Status:        history.StatusApplied,
		RequestedBy:   "tester (1000)",
		Command:       []string{"upgrade"},
		Packages: []history.Transition{
			{Name: "figlet", Operation: history.OpInstall, After: versionState("2.2.5_6")},
			{Name: "nano", Operation: history.OpRemove, Before: versionState("9.2_1")},
			{Name: "glibc", Operation: history.OpUpgrade, Before: versionState("2.41_1"), After: versionState("2.41_2")},
			{Name: "bash", Operation: history.OpHeld, Before: versionState("5.2_1"), After: versionState("5.2_1"), HeldReason: "ManualHold"},
			{Name: "zsh", Operation: history.OpReinstall, Before: versionState("5.9_3"), After: versionState("5.9_3")},
			{Name: "curl", Operation: history.OpDowngrade, Before: versionState("8.17.0_1"), After: versionState("8.16.0_1")},
		},
	}
	model := historyToTui(entry, summary.SizeBinary)
	if model.Title != "xbpretty History" {
		t.Errorf("title = %q, want xbpretty History", model.Title)
	}
	if !model.History {
		t.Error("history model must set History so the footer drops the download/disk rows")
	}
	wantLabels := []string{"Remove", "Install", "Reinstall", "Upgrade", "Downgrade", "Held"}
	if len(model.Tabs) != len(wantLabels) {
		t.Fatalf("tabs = %+v, want %v", model.Tabs, wantLabels)
	}
	for i, want := range wantLabels {
		if model.Tabs[i].Label != want {
			t.Errorf("tab %d = %q, want %q", i, model.Tabs[i].Label, want)
		}
	}
	remove := model.Tabs[0].Rows[0]
	if remove.Name != "nano" || remove.Old != "9.2_1" || remove.New != "" {
		t.Errorf("remove row = %+v", remove)
	}
	install := model.Tabs[1].Rows[0]
	if install.Name != "figlet" || install.New != "2.2.5_6" || install.Old != "" {
		t.Errorf("install row = %+v", install)
	}
	upgrade := model.Tabs[3].Rows[0]
	if upgrade.Old != "2.41_1" || upgrade.New != "2.41_2" || len(upgrade.NewSpans) == 0 {
		t.Errorf("upgrade row = %+v", upgrade)
	}
	downgrade := model.Tabs[4].Rows[0]
	if downgrade.Old != "8.17.0_1" || downgrade.New != "8.16.0_1" || len(downgrade.NewSpans) == 0 {
		t.Errorf("downgrade row = %+v", downgrade)
	}
	held := model.Tabs[5].Rows[0]
	if held.Old != "5.2_1" || held.New != "5.2_1" || held.Reason != "ManualHold" {
		t.Errorf("held row = %+v", held)
	}
	if model.Download != "" || model.Disk != "" || model.DiskLabel != "" {
		t.Errorf("history model carries download/disk data: %q/%q/%q", model.Download, model.DiskLabel, model.Disk)
	}
	for _, line := range model.Help {
		if line != "(↑) move up | (↓) move down" && line != "(→) next tab | (←) previous tab" && line != "(q) quit" {
			t.Errorf("unexpected history help line %q", line)
		}
	}
}

func versionState(v string) *history.PackageState {
	return &history.PackageState{Version: &v}
}

package cli

import (
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/tui"
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
	wantHelp := "(s) package info | (q) quit"
	if len(model.Help) == 0 || model.Help[len(model.Help)-1] != wantHelp {
		t.Errorf("history help = %v, want a final %q line", model.Help, wantHelp)
	}
}

func versionState(v string) *history.PackageState {
	return &history.PackageState{Version: &v}
}

// TestHistoryRowSizesFollowOperationSemantics is FR-069's table reuse: a
// removal shows the recorded installed size, the install/upgrade family the
// download size, and an unrecorded size the em dash, in the configured unit.
func TestHistoryRowSizesFollowOperationSemantics(t *testing.T) {
	entry := history.Entry{
		Packages: []history.Transition{
			{Name: "nano", Operation: history.OpRemove, Before: versionState("9.2_1"), InstalledSize: 3 * 1024 * 1024, Size: 1024 * 1024},
			{Name: "figlet", Operation: history.OpInstall, After: versionState("2.2.5_6"), Size: 92 * 1024, InstalledSize: 500 * 1024 * 1024},
			{Name: "glibc", Operation: history.OpUpgrade, Before: versionState("2.41_1"), After: versionState("2.41_2"), Size: 12 * 1024 * 1024, InstalledSize: 100 * 1024 * 1024},
			{Name: "zsh", Operation: history.OpConfigure, Before: versionState("5.9_3"), InstalledSize: 7 * 1024 * 1024, Size: 9 * 1024 * 1024},
			{Name: "bash", Operation: history.OpHeld, Before: versionState("5.2_1"), After: versionState("5.2_1")},
		},
	}
	cases := []struct {
		unit  summary.SizeUnit
		sizes map[string]string
	}{
		{summary.SizeBinary, map[string]string{
			"nano": "3 MiB", "figlet": "92 KiB", "glibc": "12 MiB", "zsh": "7 MiB", "bash": "—",
		}},
		{summary.SizeDecimal, map[string]string{
			"nano": "3 MB", "figlet": "94 KB", "glibc": "13 MB", "zsh": "7 MB", "bash": "—",
		}},
	}
	for _, tc := range cases {
		model := historyToTui(entry, tc.unit)
		got := map[string]string{}
		for _, tab := range model.Tabs {
			for _, row := range tab.Rows {
				got[row.Name] = row.Size
			}
		}
		for name, want := range tc.sizes {
			if got[name] != want {
				t.Errorf("unit %v: %s size = %q, want %q", tc.unit, name, got[name], want)
			}
		}
	}
}

// TestHistoryPackageInfoRemoveRow is FR-069's read-only package info: a
// removal's single version is Version, never a duplicate Installed row, and
// the help returns to history.
func TestHistoryPackageInfoRemoveRow(t *testing.T) {
	model := historyToTui(history.Entry{Packages: []history.Transition{
		{Name: "nano", Operation: history.OpRemove, Before: versionState("9.2_1"), InstalledSize: 3 * 1024 * 1024},
	}}, summary.SizeBinary)
	info := summaryPackageInfo(model)
	if info == nil {
		t.Fatal("summaryPackageInfo returned nil")
	}
	if got, ok := infoField(t, info, "Version"); !ok || got != "9.2_1" {
		t.Errorf("Version = %q/%v, want 9.2_1", got, ok)
	}
	for _, absent := range []string{"Installed", "Recorded", "Available"} {
		if _, ok := infoField(t, info, absent); ok {
			t.Errorf("removal package info unexpectedly has %s", absent)
		}
	}
	versions := 0
	for _, f := range info.Fields {
		if f.Label == "Version" {
			versions++
		}
	}
	if versions != 1 {
		t.Errorf("package info has %d Version fields, want 1: %+v", versions, info.Fields)
	}
	if len(info.Help) != 1 || info.Help[0] != "(q) back to history" {
		t.Errorf("history package-info help = %v, want (q) back to history", info.Help)
	}
}

// TestHistoryPackageInfoUpgradeRow proves a two-version row uses the recorded
// labels: Recorded for the before state and Version for the after state.
func TestHistoryPackageInfoUpgradeRow(t *testing.T) {
	model := historyToTui(history.Entry{Packages: []history.Transition{
		{Name: "glibc", Operation: history.OpUpgrade, Before: versionState("2.41_1"), After: versionState("2.41_2"), Size: 12 * 1024 * 1024},
	}}, summary.SizeBinary)
	info := summaryPackageInfo(model)
	if got, ok := infoField(t, info, "Recorded"); !ok || got != "2.41_1" {
		t.Errorf("Recorded = %q/%v, want 2.41_1", got, ok)
	}
	if got, ok := infoField(t, info, "Version"); !ok || got != "2.41_2" {
		t.Errorf("Version = %q/%v, want 2.41_2", got, ok)
	}
	if _, ok := infoField(t, info, "Available"); ok {
		t.Error("history package info still uses the forward-looking Available label")
	}
}

// TestHistoryHelpMentionsPackageInfo keeps the required `s` binding
// discoverable in the counts-only footer.
func TestHistoryHelpMentionsPackageInfo(t *testing.T) {
	joined := strings.Join(historyScreenHelp(), "\n")
	if !strings.Contains(joined, "(s) package info") {
		t.Errorf("history help does not mention s: %q", joined)
	}
}

// Ensure the S1 info view keeps its forward-looking labels.
func TestS1PackageInfoStillUsesAvailable(t *testing.T) {
	model := &tui.SummaryModel{Title: "xbpretty Install", Tabs: []tui.Tab{{Label: "Install", Rows: []tui.PkgRow{
		{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"},
	}}}}
	info := summaryPackageInfo(model)
	if got, ok := infoField(t, info, "Available"); !ok || got != "2.2.5_6" {
		t.Errorf("Available = %q/%v, want 2.2.5_6", got, ok)
	}
	if len(info.Help) != 1 || info.Help[0] != "(q) back to summary" {
		t.Errorf("S1 package-info help = %v, want (q) back to summary", info.Help)
	}
}

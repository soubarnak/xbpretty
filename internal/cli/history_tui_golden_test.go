package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// historyDowngradeEntry is the end-to-end downgrade fixture: the entry-to-model
// conversion and the renderer must both keep the recorded old version.
func historyDowngradeEntry() history.Entry {
	return history.Entry{
		SchemaVersion:    history.SchemaVersion,
		ID:               3,
		Status:           history.StatusApplied,
		RequestedBy:      "tester (1000)",
		Command:          []string{"install", "curl"},
		RequestedTargets: []string{"curl"},
		Packages: []history.Transition{
			{Name: "curl", Operation: history.OpDowngrade, Before: historyState("8.17.0_1"), After: historyState("8.16.0_1"), Size: 2 * 1024 * 1024},
			{Name: "wget", Operation: history.OpDowngrade, Before: historyState("1.24.5_1"), After: historyState("1.24.4_1"), Size: 1024 * 1024},
		},
	}
}

// historyAllGroupsEntry exercises every recorded operation group in one entry:
// ten tabs, which at 80x24 and 60x18 forces the strip's shift-to-active path.
// The recording order is scrambled so the fixed GroupOrder is also covered.
func historyAllGroupsEntry() history.Entry {
	v := func(value string) *history.PackageState {
		return &history.PackageState{Version: &value}
	}
	return history.Entry{
		SchemaVersion:    history.SchemaVersion,
		ID:               4,
		Status:           history.StatusApplied,
		RequestedBy:      "tester (1000)",
		Command:          []string{"upgrade"},
		RequestedTargets: []string{},
		Packages: []history.Transition{
			{Name: "cfg-pkg", Operation: history.OpConfigure, Before: v("1.0_1"), InstalledSize: 1024 * 1024},
			{Name: "held-pkg", Operation: history.OpHeld, Before: v("2.0_1"), After: v("2.1_1"), HeldReason: "ManualHold", InstalledSize: 2 * 1024 * 1024},
			{Name: "up-pkg", Operation: history.OpUpgrade, Before: v("1.0_1"), After: v("1.1_1"), Size: 12 * 1024 * 1024},
			{Name: "re-pkg", Operation: history.OpReinstall, Before: v("3.0_1"), After: v("3.0_1"), Size: 2 * 1024 * 1024},
			{Name: "in-pkg", Operation: history.OpInstall, After: v("4.0_1"), Size: 92 * 1024},
			{Name: "ap-pkg", Operation: history.OpAutoPurge, Before: v("5.0_1"), InstalledSize: 1024 * 1024},
			{Name: "purge-pkg", Operation: history.OpPurge, Before: v("6.0_1"), InstalledSize: 2 * 1024 * 1024},
			{Name: "ar-pkg", Operation: history.OpAutoRemove, Before: v("7.0_1"), InstalledSize: 3 * 1024 * 1024},
			{Name: "down-pkg", Operation: history.OpDowngrade, Before: v("8.2_1"), After: v("8.1_1"), Size: 5 * 1024 * 1024},
			{Name: "rm-pkg", Operation: history.OpRemove, Before: v("9.0_1"), InstalledSize: 4 * 1024 * 1024},
		},
	}
}

func readTuiGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "tui", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return raw
}

// TestHistoryTuiGoldens cuts the end-to-end history frames from historyToTui:
// a downgrade entry keeps both versions, and the ten-group entry shifts its
// tab strip inside the body at both 80x24 and the 60x18 floor. The all-groups
// models select the last tab, which is the shift that used to paint over the
// left border.
func TestHistoryTuiGoldens(t *testing.T) {
	cases := []struct {
		name    string
		entry   history.Entry
		w, h    int
		active  int
		lastTab bool
	}{
		{name: "history-downgrade-80x24", entry: historyDowngradeEntry(), w: 80, h: 24, active: 0},
		{name: "history-allgroups-80x24", entry: historyAllGroupsEntry(), w: 80, h: 24, lastTab: true},
		{name: "history-allgroups-60x18", entry: historyAllGroupsEntry(), w: 60, h: 18, lastTab: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model := historyToTui(c.entry, summary.SizeBinary)
			if c.lastTab {
				model.Active = len(model.Tabs) - 1
			} else {
				model.Active = c.active
			}
			buf := tui.RenderSummary(model, c.w, c.h, tui.DefaultTheme())
			if got, want := buf.Text(), string(readTuiGolden(t, c.name+".txt")); got != want {
				t.Errorf("grid differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if got, want := buf.Flush(nil, true), readTuiGolden(t, c.name+".sgr"); string(got) != string(want) {
				t.Errorf("SGR bytes differ\n--- got ---\n%q\n--- want ---\n%q", got, want)
			}
			// Belt and braces for the clipping fix: the shifted strip may not
			// touch the border or the one-column inner padding.
			for _, y := range []int{1, 2} {
				if got := buf.Get(0, y).R; got != '┃' {
					t.Errorf("row %d left border = %q, want ┃", y, got)
				}
				if got := buf.Get(1, y).R; got != ' ' {
					t.Errorf("row %d inner padding = %q, want space", y, got)
				}
				if got := buf.Get(c.w-1, y).R; got != '┃' {
					t.Errorf("row %d right border = %q, want ┃", y, got)
				}
				if got := buf.Get(c.w-2, y).R; got != ' ' {
					t.Errorf("row %d right padding = %q, want space", y, got)
				}
			}
			if c.lastTab {
				// The active group is still discernible from its underline:
				// the last tab (Held) must be the visible active label.
				line := buf.Grid()[1]
				if !strings.Contains(line, "Held") {
					t.Errorf("shifted strip no longer shows the active Held tab:\n%s", line)
				}
			}
		})
	}
}

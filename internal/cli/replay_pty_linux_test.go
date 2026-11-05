//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/history"
)

// TestUndoUsesTheTuiSummaryScreen pins FR-067/FR-068 for replays: Tui mode
// opens S1 for undo, titled Undo with the forced operation's Purge section,
// and y confirms without a second text prompt.
func TestUndoUsesTheTuiSummaryScreen(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	env := tuiScreenEnv(t, dir, filepath.Join(stubDir, "xbps-query"))
	env["XB_STUB_LIST"] = "uu figlet-2.2.5_6 figlet\n"
	env["XB_STUB_LIST_AFTER"] = ""
	env["XB_STUB_MANUAL"] = ""
	env["XB_STUB_MANUAL_AFTER"] = ""
	env["XB_STUB_HOLD"] = ""
	env["XB_STUB_HOLD_AFTER"] = ""
	env["XB_STUB_AFTER_MARK"] = filepath.Join(dir, "after-transaction")

	r := startPty(t, bin, env, []string{"undo", "1"}, 80, 24)
	waitCapture(t, r, "the Undo S1 screen", func(s string) bool {
		return strings.Contains(s, "xbpretty Undo") && strings.Contains(s, "Purge")
	})
	r.send(t, "y")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("confirmed undo exit = %d\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(r.stderr.String(), "user refused") {
		t.Errorf("screen confirm printed a refusal:\n%s", r.stderr.String())
	}
	raw, err := os.ReadFile(env["XB_STUB_LOG"])
	if err != nil {
		t.Fatalf("reading the stub log: %v", err)
	}
	if !strings.Contains(string(raw), "xbps-remove -y -f figlet") {
		t.Errorf("undo did not run its forced removal:\n%s", raw)
	}
}

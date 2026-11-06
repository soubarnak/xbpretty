package cli

import (
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/history"
)

// driftWarning is the FR-033/FR-035 line a current state that differs from
// the compared state prints. Its presence is the only thing these tests read.
const driftWarning = "but history entry"

// TestRedoAfterUndoPrintsNoDriftWarning pins BF-35: redo compares the current
// state against the entry's before state, so the state an undo just left
// behind is not reported as drift. Without the fix every package warns.
func TestRedoAfterUndoPrintsNoDriftWarning(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"upgrade", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})

	// The machine is at the recorded after state; undo moves it to before.
	undoEnv := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")
	out, errs, code := runBinary(t, bin, undoEnv, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if strings.Contains(out+errs, driftWarning) {
		t.Fatalf("undo warned about drift from a state it just recorded:\n%s%s", out, errs)
	}

	// The next replay starts where undo left the machine: at before.
	redoEnv := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "uu figlet-2.2.6_1 figlet\n")
	out, errs, code = runBinary(t, bin, redoEnv, "redo", "-y")
	if code != 0 {
		t.Fatalf("redo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if strings.Contains(out+errs, driftWarning) {
		t.Errorf("redo after undo printed a drift warning:\n%s%s", out, errs)
	}
	restored := false
	for _, line := range readCommandLog(t, dir) {
		if strings.HasPrefix(line, "xbps-install -y -f ") && strings.HasSuffix(line, " figlet-2.2.6_1") {
			restored = true
		}
	}
	if !restored {
		t.Errorf("redo did not restore the recorded after version figlet-2.2.6_1")
	}
}

// TestUndoStillWarnsWhenTheStateDrifted is the control for BF-35: undo still
// compares against after, so a machine that already sits at before is
// reported as drift.
func TestUndoStillWarnsWhenTheStateDrifted(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"upgrade", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	want := "figlet is 2.2.5_6 but history entry 1 recorded 2.2.6_1; undo still restores 2.2.5_6"
	if !strings.Contains(out+errs, want) {
		t.Errorf("undo did not warn about drift; want %q in:\n%s%s", want, out, errs)
	}
}

// TestRedoOfAnAppliedEntryPrintsNoDriftWarning pins the BF-35 follow-up: a
// redo of an entry that was never undone finds the package already at its
// after state. Redo leaves it alone, so there is no drift to report.
func TestRedoOfAnAppliedEntryPrintsNoDriftWarning(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	out, errs, code := runBinary(t, bin, env, "redo", "-y")
	if code != 0 {
		t.Fatalf("redo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if strings.Contains(out+errs, driftWarning) {
		t.Errorf("redo of an applied entry printed a drift warning:\n%s%s", out, errs)
	}
	if !strings.Contains(out, "Nothing to do.") {
		t.Errorf("redo of an applied entry did not report nothing to do:\n%s%s", out, errs)
	}
}

// TestRedoStillWarnsFromAThirdState is the control for the follow-up: a redo
// from a state that is neither before nor after is still drift.
func TestRedoStillWarnsFromAThirdState(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"upgrade", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.4_1 figlet\n", "uu figlet-2.2.6_1 figlet\n")

	out, errs, code := runBinary(t, bin, env, "redo", "-y")
	if code != 0 {
		t.Fatalf("redo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	want := "figlet is 2.2.4_1 but history entry 1 recorded 2.2.5_6; redo still restores 2.2.6_1"
	if !strings.Contains(out+errs, want) {
		t.Errorf("redo from a third state did not warn; want %q in:\n%s%s", want, out, errs)
	}
}

package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/history"
)

// ---- fixtures -------------------------------------------------------------

// replayEnv is one built-binary replay run: a private history store, the
// stub xbps binaries, and a before/after installed fixture pair. The after
// fixture applies once the real child touched the marker, exactly like a
// successful transaction.
func replayEnv(t *testing.T, dir, before, after string) testEnv {
	t.Helper()
	env := historyRunEnv(t, dir).
		with("XB_STUB_LIST", before).
		with("XB_STUB_LIST_AFTER", after).
		with("XB_STUB_MANUAL", "").
		with("XB_STUB_MANUAL_AFTER", "").
		with("XB_STUB_HOLD", "").
		with("XB_STUB_HOLD_AFTER", "").
		// Every seeded undo restores an older version, so the snapshot's
		// cmpver answer is the downgrade direction.
		with("XB_STUB_CMPVER_EXIT", "1").
		with("XBPRETTY_TEST_REPLAY_TMPDIR", filepath.Join(dir, "replay-tmp"))
	if err := os.MkdirAll(filepath.Join(dir, "replay-tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The marker flips the stub to its after fixtures; a seeded store must
	// start from the before fixtures.
	_ = os.Remove(filepath.Join(dir, "after-transaction"))
	return env
}

func seedCacheArchive(t *testing.T, dir, archive string) {
	t.Helper()
	cache := filepath.Join(dir, "xbps-cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, archive), []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cacheArchivePath(dir, archive string) string {
	return filepath.Join(dir, "xbps-cache", archive)
}

// writeReplayEntry publishes one schema-1 entry and returns it with its id.
func writeReplayEntry(t *testing.T, dir string, entry history.Entry) history.Entry {
	t.Helper()
	if entry.SchemaVersion == 0 {
		entry.SchemaVersion = history.SchemaVersion
	}
	if entry.Status == "" {
		entry.Status = history.StatusApplied
	}
	if entry.RequestedBy == "" {
		entry.RequestedBy = "tester (0)"
	}
	if entry.StartedAt == "" {
		entry.StartedAt = "2026-11-05T10:00:00Z"
	}
	if entry.FinishedAt == "" {
		entry.FinishedAt = "2026-11-05T10:00:01Z"
	}
	if entry.Command == nil {
		entry.Command = []string{}
	}
	if entry.RequestedTargets == nil {
		entry.RequestedTargets = []string{}
	}
	written, err := history.New(dir).Write(entry)
	if err != nil {
		t.Fatalf("seeding history: %v", err)
	}
	return written
}

func pkgState(version string, auto bool) *history.PackageState {
	v := version
	return &history.PackageState{Version: &v, AutoInstalled: &auto}
}

func absentState() *history.PackageState { return nil }

const replayRepoURL = "file:///var/db/xbps/current"

func replayTransition(operation, name, beforeVersion, afterVersion string) history.Transition {
	t := history.Transition{
		Name:          name,
		Arch:          "x86_64",
		Repository:    replayRepoURL,
		Size:          94372,
		InstalledSize: 672593,
		Operation:     operation,
	}
	if beforeVersion != "" {
		t.Before = pkgState(beforeVersion, false)
	}
	if afterVersion != "" {
		t.After = pkgState(afterVersion, false)
	}
	return t
}

func readCommandLog(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "commands.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("reading command log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func commandIndex(lines []string, prefix string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}
	return -1
}

func countCommands(lines []string, prefix string) int {
	n := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func entryFileCount(t *testing.T, dir string) int {
	t.Helper()
	return len(entryFiles(t, dir))
}

func lastEntry(t *testing.T, dir string) history.Entry {
	t.Helper()
	entry, ok, err := history.New(dir).Last()
	if err != nil || !ok {
		t.Fatalf("reading last entry: %v (present=%v)", err, ok)
	}
	return entry
}

// ---- undo ----------------------------------------------------------------

// TestAC026UndoInstallRemovesForcedAndWritesOneReplayEntry pins AC-026: an
// install entry is undone by a forced removal, one new undo entry is written
// and one audit line is appended.
func TestAC026UndoInstallRemovesForcedAndWritesOneReplayEntry(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	lines := readCommandLog(t, dir)
	if got := countCommands(lines, "xbps-remove -y -f figlet"); got != 1 {
		t.Errorf("forced removal runs = %d, want 1; commands:\n%s", got, strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), "-f -f") {
		t.Errorf("a doubled -f reached xbps:\n%s", strings.Join(lines, "\n"))
	}

	if got := entryFileCount(t, store); got != 2 {
		t.Fatalf("entries = %d, want exactly one new replay entry", got)
	}
	replay := lastEntry(t, store)
	if replay.ReplayOf == nil || *replay.ReplayOf != 1 {
		t.Errorf("replay_of = %v, want 1", replay.ReplayOf)
	}
	if replay.ReplayKind == nil || *replay.ReplayKind != "undo" {
		t.Errorf("replay_kind = %v, want undo", replay.ReplayKind)
	}
	if len(replay.Packages) != 1 || replay.Packages[0].Operation != history.OpPurge {
		t.Errorf("replay packages = %+v, want one forced removal", replay.Packages)
	}

	logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the line log: %v", err)
	}
	linesInLog := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(linesInLog) != 1 || !strings.Contains(linesInLog[0], " undo figlet-2.2.5_6") {
		t.Errorf("line log = %q, want one undo line naming the removed pkgver", raw)
	}
	replayRaw, err := os.ReadFile(filepath.Join(store, "2.json"))
	if err != nil {
		t.Fatalf("reading the replay entry: %v", err)
	}
	if strings.Contains(string(replayRaw), "config_files_only") {
		t.Errorf("a replay recorded config_files_only:\n%s", replayRaw)
	}
}

// TestUndoUpgradeRestoresExactOldVersionFromCache pins FR-033/FR-034: an
// upgrade restores the recorded old version from a cache-only archive through
// the temporary replay repository, with one -f and no sync.
func TestUndoUpgradeRestoresExactOldVersionFromCache(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	lines := readCommandLog(t, dir)
	rindex := commandIndex(lines, "xbps-rindex -a ")
	if rindex < 0 {
		t.Fatalf("no xbps-rindex call; commands:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[rindex], "figlet-2.2.5_6.x86_64.xbps") {
		t.Errorf("rindex did not index the cached archive: %q", lines[rindex])
	}
	install := commandIndex(lines, "xbps-install -y -f -R ")
	if install < 0 {
		t.Fatalf("no exact-version restore with -R; commands:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Count(lines[install], "-f") != 1 {
		t.Errorf("restore -f count = %d, want one: %q", strings.Count(lines[install], "-f"), lines[install])
	}
	if !strings.HasSuffix(lines[install], " figlet-2.2.5_6") {
		t.Errorf("restore does not name the exact recorded version: %q", lines[install])
	}
	if commandIndex(lines, "xbps-install -S") >= 0 || commandIndex(lines, "xbps-install -u") >= 0 {
		t.Errorf("a replay synced or upgraded:\n%s", strings.Join(lines, "\n"))
	}
	if commandIndex(lines, "xbps-remove") >= 0 {
		t.Errorf("an upgrade undo removed instead of restoring:\n%s", strings.Join(lines, "\n"))
	}

	// The temporary repository is removed on exit.
	if matches, _ := filepath.Glob(filepath.Join(dir, "replay-tmp", "xbpretty-replay-*")); len(matches) != 0 {
		t.Errorf("replay repositories survived the run: %v", matches)
	}

	// One replay entry records the restore.
	if got := entryFileCount(t, store); got != 2 {
		t.Fatalf("entries = %d, want one new replay entry", got)
	}
	replay := lastEntry(t, store)
	if replay.ReplayKind == nil || *replay.ReplayKind != "undo" {
		t.Errorf("replay_kind = %v, want undo", replay.ReplayKind)
	}
	if len(replay.Packages) != 1 || replay.Packages[0].Operation != history.OpDowngrade {
		t.Errorf("replay packages = %+v, want one downgrade", replay.Packages)
	}
}

// TestUndoUpgradeUsesRepositoryWhenCacheIsEmpty pins the other preflight
// route: a configured repository serving the exact version restores without a
// temporary replay repository.
func TestUndoUpgradeUsesRepositoryWhenCacheIsEmpty(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	if commandIndex(lines, "xbps-rindex") >= 0 {
		t.Errorf("a repository-served restore built a replay repository:\n%s", strings.Join(lines, "\n"))
	}
	install := commandIndex(lines, "xbps-install -y -f ")
	if install < 0 {
		t.Fatalf("no restore; commands:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[install], " -R ") {
		t.Errorf("a repository-served restore passed -R: %q", lines[install])
	}
	if !strings.HasSuffix(lines[install], " figlet-2.2.5_6") {
		t.Errorf("restore does not name the exact version: %q", lines[install])
	}
}

// TestAC027PreflightFailsClosedBeforeAnyMutation pins AC-027's fail-closed
// half: one line per unrestorable package, exit 1, and no mutating child.
func TestAC027PreflightFailsClosedBeforeAnyMutation(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_REPO_MISSING", "figlet-2.2.5_6")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 1 {
		t.Fatalf("undo exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	const want = "xbpretty: undo cannot restore figlet-2.2.5_6.x86_64: not in /var/cache/xbps and no configured repository serves that version\n"
	if errs != want {
		t.Errorf("stderr = %q, want %q", errs, want)
	}
	lines := readCommandLog(t, dir)
	for _, line := range lines {
		// Before any mutation only the read-only snapshot and repository
		// lookups may run; anything else means the preflight was not closed.
		if !strings.HasPrefix(line, "xbps-query ") {
			t.Errorf("a non-read-only child ran before the failed preflight: %q", line)
		}
	}
	if got := entryFileCount(t, store); got != 1 {
		t.Errorf("fail-closed undo wrote history entries: %d", got)
	}
}

// TestAC027UnsupportedEntriesAreRefused pins AC-027's other two refusals: a
// reinstall has no inverse, and an entry with no replayable rows has nothing
// to replay.
func TestAC027UnsupportedEntriesAreRefused(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "--reinstall"},
		Packages: []history.Transition{replayTransition(history.OpReinstall, "figlet", "2.2.5_6", "2.2.5_6")},
	})
	writeReplayEntry(t, store, history.Entry{
		Command: []string{"install", "figlet"},
		Packages: []history.Transition{
			replayTransition(history.OpConfigure, "figlet", "2.2.5_6", "2.2.5_6"),
			replayTransition(history.OpHeld, "figlet", "", ""),
		},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	_, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 1 || !strings.Contains(errs, "xbpretty: history entry 1 contains a reinstall, which has no inverse; undo cannot continue") {
		t.Errorf("reinstall undo = %d, stderr %q", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "undo", "2", "-y")
	if code != 1 || !strings.Contains(errs, "xbpretty: history entry 2 has no package changes to replay") {
		t.Errorf("no-row undo = %d, stderr %q", code, errs)
	}
}

// TestUndoRestoresBeforeRemoving pins FR-033's phase order: every restore runs
// in the first xbps transaction and every removal in the second.
func TestUndoRestoresBeforeRemoving(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command: []string{"install", "figlet", "nano", "-y"},
		Packages: []history.Transition{
			replayTransition(history.OpInstall, "nano", "", "8.0_1"),
			replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1"),
		},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir,
		"uu nano-8.0_1 nano\nuu figlet-2.2.6_1 figlet\n",
		"uu figlet-2.2.5_6 figlet\n")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	install := commandIndex(lines, "xbps-install -y")
	remove := commandIndex(lines, "xbps-remove -y")
	if install < 0 || remove < 0 {
		t.Fatalf("both phases did not run; commands:\n%s", strings.Join(lines, "\n"))
	}
	if install > remove {
		t.Errorf("removal ran before the restore:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[remove], " nano") {
		t.Errorf("removal phase did not target the absent-before package: %q", lines[remove])
	}
	if strings.Contains(lines[install], " nano") {
		t.Errorf("restore phase contained the removal target: %q", lines[install])
	}
}

// TestUndoEmptyPlanIsANoOp pins FR-033's empty-plan rule: a plan whose targets
// already match prints Nothing to do. and exits 0 without a child or entry.
func TestUndoEmptyPlanIsANoOp(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	// The current version already is the undo target.
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if !strings.Contains(out, "Nothing to do.") {
		t.Errorf("stdout = %q, want Nothing to do.", out)
	}
	lines := readCommandLog(t, dir)
	for _, line := range lines {
		if strings.HasPrefix(line, "xbps-install -y") || strings.HasPrefix(line, "xbps-remove -y") {
			t.Errorf("an empty plan ran a child: %q", line)
		}
	}
	if got := entryFileCount(t, store); got != 1 {
		t.Errorf("empty plan wrote history: %d entries", got)
	}
}

// TestUndoPartialRemovalFailureReportsAndWritesNothing pins FR-033's partial
// message and FR-031's nothing-on-failure rule.
func TestUndoPartialRemovalFailureReportsAndWritesNothing(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command: []string{"install", "figlet", "nano", "-y"},
		Packages: []history.Transition{
			replayTransition(history.OpInstall, "nano", "", "8.0_1"),
			replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1"),
		},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir,
		"uu nano-8.0_1 nano\nuu figlet-2.2.6_1 figlet\n",
		"uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_REMOVE_RUN_EXIT", "7")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 7 {
		t.Fatalf("undo exit = %d, want 7\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "xbpretty: undo is partial: the restore phase completed and the removal phase failed") {
		t.Errorf("missing the partial message:\n%s", errs)
	}
	if got := entryFileCount(t, store); got != 1 {
		t.Errorf("partial replay wrote history entries: %d", got)
	}
	logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
	if raw, err := os.ReadFile(logPath); err == nil && len(raw) != 0 {
		t.Errorf("partial replay wrote the line log: %q", raw)
	}
}

// ---- redo ----------------------------------------------------------------

// TestAC028RedoReinstallsAtRecordedVersion pins AC-028: bare redo targets the
// source entry of the newest undo, restores its after version, and writes a
// new redo entry.
func TestAC028RedoReinstallsAtRecordedVersion(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	first := writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})

	// Undo the install: the package becomes absent and a replay entry is
	// written.
	undoEnv := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "")
	if _, errs, code := runBinary(t, bin, undoEnv, "undo", "1", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}

	// Redo: the newest entry is the undo, so bare redo replays its source.
	redoEnv := replayEnv(t, dir, "", "uu figlet-2.2.5_6 figlet\n")
	out, errs, code := runBinary(t, bin, redoEnv, "redo", "-y")
	if code != 0 {
		t.Fatalf("redo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	if got := countCommands(lines, "xbps-install -y -f figlet-2.2.5_6"); got != 1 {
		t.Errorf("exact-version redo installs = %d, want 1; commands:\n%s", got, strings.Join(lines, "\n"))
	}
	if commandIndex(lines, "xbps-install -S") >= 0 || commandIndex(lines, "xbps-remove -y -o") >= 0 {
		t.Errorf("a replay synced or ran implicit autoremove:\n%s", strings.Join(lines, "\n"))
	}

	replay := lastEntry(t, store)
	if replay.ReplayKind == nil || *replay.ReplayKind != "redo" {
		t.Errorf("replay_kind = %v, want redo", replay.ReplayKind)
	}
	if replay.ReplayOf == nil || *replay.ReplayOf != first.ID {
		t.Errorf("replay_of = %v, want %d (the source entry)", replay.ReplayOf, first.ID)
	}
	if got := entryFileCount(t, store); got != 3 {
		t.Errorf("entries = %d, want install + undo + redo", got)
	}
}

// TestRedoLastTargetsNewestEntryLiterally pins FR-035: explicit `redo last`
// is the newest entry, never the source of an undo.
func TestRedoLastTargetsNewestEntryLiterally(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	if _, errs, code := runBinary(t, bin, replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", ""), "undo", "1", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}
	// The newest entry is the undo (after = absent). Redo last replays that
	// absence: it removes the currently installed package.
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "")
	out, errs, code := runBinary(t, bin, env, "redo", "last", "-y")
	if code != 0 {
		t.Fatalf("redo last exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	if commandIndex(lines, "xbps-remove -y -f figlet") < 0 {
		t.Errorf("redo last did not replay the newest entry's absence:\n%s", strings.Join(lines, "\n"))
	}
	replay := lastEntry(t, store)
	if replay.ReplayOf == nil || *replay.ReplayOf != 2 {
		t.Errorf("replay_of = %v, want the literal newest entry 2", replay.ReplayOf)
	}
}

// TestDoubleUndoThenRedoToggles pins FR-035's toggle: undoing twice walks back
// to the installed state, and redo then replays the source undo's absence.
func TestDoubleUndoThenRedoToggles(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})

	// First undo: remove.
	if _, errs, code := runBinary(t, bin, replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", ""), "undo", "-y"); code != 0 {
		t.Fatalf("first undo exit = %d\n%s", code, errs)
	}
	// Second undo: the newest entry's before state is installed, so restore.
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	if _, errs, code := runBinary(t, bin, replayEnv(t, dir, "", "uu figlet-2.2.5_6 figlet\n"), "undo", "-y"); code != 0 {
		t.Fatalf("second undo exit = %d\n%s", code, errs)
	}
	lines := readCommandLog(t, dir)
	if commandIndex(lines, "xbps-install -y -f -R ") < 0 {
		t.Fatalf("second undo did not restore the package:\n%s", strings.Join(lines, "\n"))
	}

	// Redo: the newest entry is the second undo, so its source is the first
	// undo, whose after state is absent.
	if _, errs, code := runBinary(t, bin, replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", ""), "redo", "-y"); code != 0 {
		t.Fatalf("redo exit = %d\n%s", code, errs)
	}
	lines = readCommandLog(t, dir)
	if commandIndex(lines, "xbps-remove -y -f figlet") < 0 {
		t.Errorf("redo did not replay the source undo's absence:\n%s", strings.Join(lines, "\n"))
	}
	if got := entryFileCount(t, store); got != 4 {
		t.Errorf("entries = %d, want install + two undos + redo", got)
	}
}

// ---- clear ---------------------------------------------------------------

// TestAC029ClearOneAndAll pins AC-029: root-only, exact counts, singular and
// plural, and stale tmp sweeping.
func TestAC029ClearOneAndAll(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	for i := 0; i < 3; i++ {
		writeReplayEntry(t, store, history.Entry{
			Command:  []string{"install", "figlet", "-y"},
			Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
		})
	}

	// Non-root: the root hint, exit 1, nothing deleted.
	nonRoot := historyRunEnv(t, dir).readOnlyEnv()
	_, errs, code := runBinary(t, bin, nonRoot, "history", "clear", "2")
	if code != 1 || !strings.Contains(errs, "hint: rerun as root:") {
		t.Errorf("non-root clear = %d, stderr %q; want 1 and the root hint", code, errs)
	}
	if _, err := os.Stat(filepath.Join(store, "2.json")); err != nil {
		t.Errorf("non-root clear deleted an entry: %v", err)
	}

	// Root: one entry, its id in the message.
	env := historyRunEnv(t, dir)
	out, errs, code := runBinary(t, bin, env, "history", "clear", "2")
	if code != 0 || out != "Cleared history entry 2.\n" {
		t.Errorf("clear 2 = %d, stdout %q, stderr %q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(store, "2.json")); !os.IsNotExist(err) {
		t.Errorf("entry 2 survived clear: %v", err)
	}

	// clear last follows the newest remaining id.
	out, errs, code = runBinary(t, bin, env, "history", "clear", "last")
	if code != 0 || out != "Cleared history entry 3.\n" {
		t.Errorf("clear last = %d, stdout %q, stderr %q", code, out, errs)
	}

	// A stale tmp is swept by --all; one numeric entry remains.
	stale := filepath.Join(store, "9.json.tmp")
	if err := os.WriteFile(stale, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	out, errs, code = runBinary(t, bin, env, "history", "clear", "--all")
	if code != 0 || out != "Cleared 1 history entry.\n" {
		t.Errorf("clear --all = %d, stdout %q, stderr %q", code, out, errs)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale tmp survived clear --all: %v", err)
	}
	if got := entryFileCount(t, store); got != 0 {
		t.Errorf("entries after --all = %d, want 0", got)
	}

	// Plural form.
	for i := 0; i < 2; i++ {
		writeReplayEntry(t, store, history.Entry{
			Command:  []string{"install", "figlet", "-y"},
			Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
		})
	}
	out, errs, code = runBinary(t, bin, env, "history", "clear", "--all")
	if code != 0 || out != "Cleared 2 history entries.\n" {
		t.Errorf("plural clear --all = %d, stdout %q, stderr %q", code, out, errs)
	}

	// FR-035/AC-029: an already empty store still reports the zero count.
	out, errs, code = runBinary(t, bin, env, "history", "clear", "--all")
	if code != 0 || out != "Cleared 0 history entries.\n" {
		t.Errorf("empty clear --all = %d, stdout %q, stderr %q", code, out, errs)
	}
}

// TestClearExitCodes pins the clear selectors' usage and missing-entry codes.
func TestClearExitCodes(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	env := historyRunEnv(t, dir)

	_, errs, code := runBinary(t, bin, env, "history", "clear", "bogus")
	if code != 2 || !strings.Contains(errs, `invalid history selector "bogus"`) {
		t.Errorf("bad selector = %d, stderr %q", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "history", "clear", "99")
	if code != 1 || !strings.Contains(errs, "xbpretty: history entry 99 not found") {
		t.Errorf("missing id = %d, stderr %q", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "history", "clear")
	if code != 2 || errs == "" {
		t.Errorf("no selector = %d, stderr %q; want usage error 2", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "history", "clear", "--all", "1")
	if code != 2 || errs == "" {
		t.Errorf("--all with a selector = %d, stderr %q; want usage error 2", code, errs)
	}
}

// ---- store errors and flags ----------------------------------------------

// TestReplayStoreErrors pins the selector, empty-store and unknown-id rules.
func TestReplayStoreErrors(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	env := replayEnv(t, dir, "", "").readOnlyEnv()

	_, errs, code := runBinary(t, bin, env, "undo", "-y")
	if code != 1 || errs != "xbpretty: history is empty\n" {
		t.Errorf("empty undo = %d, stderr %q", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "undo", "1", "-y")
	if code != 1 || errs != "xbpretty: history entry 1 not found\n" {
		t.Errorf("missing undo id = %d, stderr %q", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "undo", "bogus", "-y")
	if code != 2 || !strings.Contains(errs, `invalid history selector "bogus"`) {
		t.Errorf("bad undo selector = %d, stderr %q", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "redo", "-y")
	if code != 1 || errs != "xbpretty: history is empty\n" {
		t.Errorf("empty redo = %d, stderr %q", code, errs)
	}
}

// TestDownloadOnlyStaysRejectedForReplays pins the parser rule: replays never
// accept --download-only.
func TestDownloadOnlyStaysRejectedForReplays(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	env := replayEnv(t, dir, "", "")
	_, errs, code := runBinary(t, bin, env, "undo", "-d")
	if code != 2 || !strings.Contains(errs, "-d") {
		t.Errorf("undo -d = %d, stderr %q; want usage error 2", code, errs)
	}
}

// TestReplayRestoresTheAutomaticInstallBit pins FR-034's best-effort bulk
// pkgdb restoration and its warning on failure.
func TestReplayRestoresTheAutomaticInstallBit(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	entry := history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	}
	before := pkgState("2.2.5_6", false)
	entry.Packages[0].Before = before
	writeReplayEntry(t, store, entry)
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_MANUAL_AFTER", "figlet-2.2.5_6\n")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	if got := countCommands(lines, "xbps-pkgdb -m manual figlet"); got != 1 {
		t.Errorf("automatic-bit restore runs = %d, want 1; commands:\n%s", got, strings.Join(lines, "\n"))
	}

	// A pkgdb failure is a warning, never a changed exit code.
	dir2 := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir2), entry)
	seedCacheArchive(t, dir2, "figlet-2.2.5_6.x86_64.xbps")
	env2 := replayEnv(t, dir2, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_MANUAL_AFTER", "figlet-2.2.5_6\n").
		with("XB_STUB_PKGDB_EXIT", "1")
	out, errs, code = runBinary(t, bin, env2, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo with failing pkgdb exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "xbpretty: could not restore the automatic-install flag for figlet") {
		t.Errorf("missing the automatic-flag warning:\n%s", errs)
	}
}

// TestReplayRoutesItsDownloadSetThroughPredownload pins the #72 seam: a
// repository-served restore predownloads before the restore child.
func TestReplayRoutesItsDownloadSetThroughPredownload(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	entry := history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	}
	// The archive is absent from the cache, so the exact version comes from
	// the repository and the transaction's download set is non-empty.
	entry.Packages[0].Repository = "https://repo-default.voidlinux.org/current"
	writeReplayEntry(t, store, entry)
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")
	// The repository reports the restored version's archive size (BF-36),
	// which is what puts the restore in the download set.
	env = withRepositoryMetadata(t, dir, env, "https://repo-default.voidlinux.org/current", "94KB")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	metadata := commandIndex(lines, "xbps-query -R -s  -p filename-sha256")
	install := commandIndex(lines, "xbps-install -y -f ")
	if metadata < 0 {
		t.Fatalf("predownload metadata pass never ran; commands:\n%s", strings.Join(lines, "\n"))
	}
	if install < 0 || metadata > install {
		t.Errorf("predownload did not precede the restore child:\n%s", strings.Join(lines, "\n"))
	}
}

// TestReplaySweepsStaleReplayRepositories pins FR-034's day-old sweep.
func TestReplaySweepsStaleReplayRepositories(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	base := filepath.Join(dir, "replay-tmp")
	stale := filepath.Join(base, "xbpretty-replay-stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	if _, errs, code := runBinary(t, bin, env, "undo", "1", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale replay repository survived: %v", err)
	}
}

// TestReplayEntryRecordsTheActualTransitions guards against a replay entry
// whose rows do not describe the run: the command, replay fields and exact
// target version are all recorded.
func TestReplayEntryRecordsTheActualTransitions(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	if _, errs, code := runBinary(t, bin, env, "undo", "last", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}
	entry := lastEntry(t, store)
	command, _ := json.Marshal(entry.Command)
	if string(command) != `["undo","last","-y"]` {
		t.Errorf("command = %s, want the argv without the program name", command)
	}
	if len(entry.RequestedTargets) != 0 {
		t.Errorf("requested_targets = %v, want []", entry.RequestedTargets)
	}
	if len(entry.Packages) != 1 || entry.Packages[0].Operation != history.OpDowngrade {
		t.Errorf("packages = %+v, want one downgrade", entry.Packages)
	}
	if got := entry.Packages[0].After; got == nil || got.Version == nil || *got.Version != "2.2.5_6" {
		t.Errorf("replay after = %+v, want the restored 2.2.5_6", got)
	}
}

// TestReplayProgressCountsAsOneEntryPerReplay is a direct guard on FR-031:
// each replay writes exactly one entry even when both phases ran.
func TestReplayProgressCountsAsOneEntryPerReplay(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command: []string{"install", "figlet", "nano", "-y"},
		Packages: []history.Transition{
			replayTransition(history.OpInstall, "nano", "", "8.0_1"),
			replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1"),
		},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir,
		"uu nano-8.0_1 nano\nuu figlet-2.2.6_1 figlet\n",
		"uu figlet-2.2.5_6 figlet\n")

	if _, errs, code := runBinary(t, bin, env, "undo", "1", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}
	if got := entryFileCount(t, store); got != 2 {
		t.Fatalf("entries = %d, want exactly one new replay entry", got)
	}
	if got := lastEntry(t, store).Packages; len(got) != 2 {
		t.Errorf("replay entry packages = %d, want both actual transitions", len(got))
	}
}

// TestRedoMapsRemoveAndPurge pins FR-035's redo mapping for the remove-family
// operations: Remove uninstalls plainly, Purge uninstalls with -f.
func TestRedoMapsRemoveAndPurge(t *testing.T) {
	bin := testrootBinary(t)
	for _, tc := range []struct {
		name    string
		op      string
		wantArg string
	}{
		{"remove", history.OpRemove, "xbps-remove -y figlet"},
		{"purge", history.OpPurge, "xbps-remove -y -f figlet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeReplayEntry(t, historyDirOf(dir), history.Entry{
				Command:  []string{"remove", "figlet", "-y"},
				Packages: []history.Transition{replayTransition(tc.op, "figlet", "2.2.5_6", "")},
			})
			env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "")
			out, errs, code := runBinary(t, bin, env, "redo", "1", "-y")
			if code != 0 {
				t.Fatalf("redo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
			}
			lines := readCommandLog(t, dir)
			if got := countCommands(lines, tc.wantArg); got != 1 {
				t.Errorf("%s redo argv = %q, want one %q; commands:\n%s",
					tc.name, lines, tc.wantArg, strings.Join(lines, "\n"))
			}
			if len(readCommandLog(t, dir)) == 0 {
				t.Fatal("no child ran")
			}
		})
	}
}

// TestRedoReinstallRestoresTheRecordedAfterVersion pins FR-035's Reinstall
// mapping: the after version is restored exactly.
func TestRedoReinstallRestoresTheRecordedAfterVersion(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "--reinstall", "-y"},
		Packages: []history.Transition{replayTransition(history.OpReinstall, "figlet", "2.2.5_6", "2.2.5_6")},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	// The package is absent, so the redo restore is a real change.
	env := replayEnv(t, dir, "", "uu figlet-2.2.5_6 figlet\n")
	out, errs, code := runBinary(t, bin, env, "redo", "1", "-y")
	if code != 0 {
		t.Fatalf("redo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	if commandIndex(lines, "xbps-install -y -f -R ") < 0 ||
		!strings.HasSuffix(lines[commandIndex(lines, "xbps-install -y -f -R ")], " figlet-2.2.5_6") {
		t.Errorf("reinstall redo did not restore the exact after version:\n%s", strings.Join(lines, "\n"))
	}
}

// TestReplayLeavesHoldsAndRepolocksAlone pins FR-034: holds and repolocks are
// recorded but never changed by a replay.
func TestReplayLeavesHoldsAndRepolocksAlone(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	entry := history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	}
	entry.Packages[0].Before.Hold = true
	entry.Packages[0].After.Hold = true
	entry.Packages[0].Before.Repolock = true
	entry.Packages[0].After.Repolock = true
	writeReplayEntry(t, historyDirOf(dir), entry)
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")

	if _, errs, code := runBinary(t, bin, env, "undo", "1", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}
	for _, line := range readCommandLog(t, dir) {
		if strings.HasPrefix(line, "xbps-pkgdb") && (strings.Contains(line, " hold") || strings.Contains(line, "repolock")) {
			t.Errorf("a replay changed a hold or repolock: %q", line)
		}
	}
}

// TestAlreadyEqualReplayLeavesTheAutomaticBitAlone pins FR-033: an
// already-equal target is untouched, including its automatic-install bit.
func TestAlreadyEqualReplayLeavesTheAutomaticBitAlone(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "uu figlet-2.2.5_6 figlet\n")
	if _, errs, code := runBinary(t, bin, env, "undo", "1", "-y"); code != 0 {
		t.Fatalf("undo exit = %d\n%s", code, errs)
	}
	if lines := readCommandLog(t, dir); commandIndex(lines, "xbps-pkgdb") >= 0 {
		t.Errorf("an already-equal replay changed the automatic bit:\n%s", strings.Join(lines, "\n"))
	}
}

// TestAC027OneLinePerUnrestorablePackage pins the per-package half of the
// fail-closed message.
func TestAC027OneLinePerUnrestorablePackage(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	writeReplayEntry(t, store, history.Entry{
		Command: []string{"install", "figlet", "nano", "-y"},
		Packages: []history.Transition{
			replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1"),
			replayTransition(history.OpUpgrade, "nano", "8.0_1", "8.1_1"),
		},
	})
	env := replayEnv(t, dir,
		"uu figlet-2.2.6_1 figlet\nuu nano-8.1_1 nano\n", "").
		with("XB_STUB_REPO_MISSING", "figlet-2.2.5_6 nano-8.0_1")

	_, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 1 {
		t.Fatalf("undo exit = %d, want 1\n%s", code, errs)
	}
	want := "xbpretty: undo cannot restore figlet-2.2.5_6.x86_64: not in /var/cache/xbps and no configured repository serves that version\n" +
		"xbpretty: undo cannot restore nano-8.0_1.x86_64: not in /var/cache/xbps and no configured repository serves that version\n"
	if errs != want {
		t.Errorf("stderr = %q, want one line per package %q", errs, want)
	}
	for _, line := range readCommandLog(t, dir) {
		if !strings.HasPrefix(line, "xbps-query ") {
			t.Errorf("a non-read-only child ran before the failed preflight: %q", line)
		}
	}
}

// TestReplayRejectsMaliciousEntryMetadata pins the privileged-path guard: a
// crafted name cannot escape the cache or be read as an xbps flag, and the
// refusal happens before any child runs.
func TestReplayRejectsMaliciousEntryMetadata(t *testing.T) {
	bin := testrootBinary(t)
	for _, name := range []string{"../evil", "-f", ".hidden"} {
		t.Run(strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			dir := t.TempDir()
			store := historyDirOf(dir)
			if err := os.MkdirAll(store, 0o755); err != nil {
				t.Fatal(err)
			}
			entry := map[string]any{
				"schema_version":    1,
				"id":                1,
				"started_at":        "2026-11-05T10:00:00Z",
				"finished_at":       "2026-11-05T10:00:01Z",
				"status":            "Applied",
				"requested_by":      "tester (0)",
				"command":           []string{"install", name},
				"requested_targets": []string{name},
				"implicit":          false,
				"replay_of":         nil,
				"replay_kind":       nil,
				"packages": []any{map[string]any{
					"name": name, "arch": "x86_64",
					"repository": "file:///repo", "size": 1, "installed_size": 1,
					"operation": "Install",
					"before":    nil,
					"after":     map[string]any{"version": "1.0_1", "auto_installed": false, "hold": false, "repolock": false},
				}},
			}
			raw, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(store, "1.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			env := replayEnv(t, dir, "uu pkg-1.0_1 pkg\n", "")
			_, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
			if code != 1 {
				t.Fatalf("undo exit = %d, want 1\n%s", code, errs)
			}
			if !strings.Contains(errs, "invalid name") {
				t.Errorf("missing the fail-closed metadata line:\n%s", errs)
			}
			if lines := readCommandLog(t, dir); len(lines) != 0 {
				t.Errorf("a crafted entry ran a child:\n%s", strings.Join(lines, "\n"))
			}
			if out, _, code := runBinary(t, bin, env.readOnlyEnv(), "history"); code != 1 || !strings.Contains(out+errs, "invalid name") {
				t.Errorf("history did not fail closed on the crafted entry: %d %q", code, out)
			}
		})
	}
}

// TestReplayRindexRetriesWithForce pins FR-034's -f retry when the index
// already has the entry.
func TestReplayRindexRetriesWithForce(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_RINDEX_FAIL_ONCE_MARK", filepath.Join(dir, "rindex-failed-once"))

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 0 {
		t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := readCommandLog(t, dir)
	if got := countCommands(lines, "xbps-rindex -a "); got != 1 {
		t.Errorf("unforced rindex attempts = %d, want 1", got)
	}
	if got := countCommands(lines, "xbps-rindex -f -a "); got != 1 {
		t.Errorf("forced rindex attempts = %d, want 1; commands:\n%s", got, strings.Join(lines, "\n"))
	}
}

// TestReplayCleansTheReplayRepoAfterChildFailure pins FR-034's cleanup on
// every exit path, including a failed restore child.
func TestReplayCleansTheReplayRepoAfterChildFailure(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
	})
	seedCacheArchive(t, dir, "figlet-2.2.5_6.x86_64.xbps")
	env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_RUN_EXIT", "9")

	out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
	if code != 9 {
		t.Fatalf("undo exit = %d, want 9\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "replay-tmp", "xbpretty-replay-*")); len(matches) != 0 {
		t.Errorf("replay repositories survived a failed child: %v", matches)
	}
}

// TestRedoConfigureHeldOnlyIsRefused pins the no-replayable-row rule for redo
// as well as undo.
func TestRedoConfigureHeldOnlyIsRefused(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command: []string{"install", "figlet"},
		Packages: []history.Transition{
			replayTransition(history.OpConfigure, "figlet", "2.2.5_6", "2.2.5_6"),
			replayTransition(history.OpHeld, "nano", "", ""),
		},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "")
	_, errs, code := runBinary(t, bin, env, "redo", "1", "-y")
	if code != 1 || !strings.Contains(errs, "xbpretty: history entry 1 has no package changes to replay") {
		t.Errorf("redo no-row = %d, stderr %q", code, errs)
	}
}

// TestClearAllCorruptStoreIsTheEscapeHatch pins FR-029: clear --all does not
// need to parse entries, so a corrupt store can still be cleared.
func TestClearAllCorruptStoreIsTheEscapeHatch(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	store := historyDirOf(dir)
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "1.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := historyRunEnv(t, dir)
	out, errs, code := runBinary(t, bin, env, "history", "clear", "--all")
	if code != 0 || out != "Cleared 1 history entry.\n" {
		t.Errorf("clear --all corrupt = %d, stdout %q, stderr %q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(store, "1.json")); !os.IsNotExist(err) {
		t.Errorf("corrupt entry survived clear --all: %v", err)
	}
}

// TestClearLastOnEmptyStore pins the empty-store last selector.
func TestClearLastOnEmptyStore(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "history", "clear", "last")
	if code != 1 || errs != "xbpretty: history is empty\n" {
		t.Errorf("clear last empty = %d, stderr %q", code, errs)
	}
}

// TestNonRootClearAllHint pins the root hint for the --all form.
func TestNonRootClearAllHint(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	env := historyRunEnv(t, dir).readOnlyEnv()
	_, errs, code := runBinary(t, bin, env, "history", "clear", "--all")
	if code != 1 || !strings.Contains(errs, "hint: rerun as root:") || !strings.Contains(errs, "--all") {
		t.Errorf("non-root clear --all = %d, stderr %q", code, errs)
	}
}

// TestReplayCommandValidation pins the replay invocation rules: -n refuses,
// -- passthrough is a usage error, and an explicitly empty selector is not
// silently treated as 'last'.
func TestReplayCommandValidation(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	writeReplayEntry(t, historyDirOf(dir), history.Entry{
		Command:  []string{"install", "figlet", "-y"},
		Packages: []history.Transition{replayTransition(history.OpInstall, "figlet", "", "2.2.5_6")},
	})
	env := replayEnv(t, dir, "uu figlet-2.2.5_6 figlet\n", "")

	_, errs, code := runBinary(t, bin, env, "undo", "1", "-n")
	if code != 1 || !strings.Contains(errs, "xbpretty: user refused confirmation") {
		t.Errorf("undo -n = %d, stderr %q; want the refusal", code, errs)
	}
	_, errs, code = runBinary(t, bin, env, "undo", "1", "--", "-f")
	if code != 2 || !strings.Contains(errs, "no -- passthrough") {
		t.Errorf("undo -- passthrough = %d, stderr %q; want usage error 2", code, errs)
	}
	_, _, code = runBinary(t, bin, env, "undo", "")
	if code != 2 {
		t.Errorf(`undo "" exit = %d, want 2`, code)
	}
}

// TestAllIsOnlyForHistoryClear pins the flag scope.
func TestAllIsOnlyForHistoryClear(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir).readOnlyEnv()
	_, errs, code := runBinary(t, bin, env, "history", "--all")
	if code != 2 || !strings.Contains(errs, "--all is only for history clear") {
		t.Errorf("history --all = %d, stderr %q", code, errs)
	}
}

// TestReplayClampsHistorySourcedSizes pins the arithmetic guard: JSON sizes
// cannot overflow the progress totals or summary arithmetic.
func TestReplayClampsHistorySourcedSizes(t *testing.T) {
	entry := history.Entry{
		Packages: []history.Transition{{
			Name: "figlet", Arch: "x86_64", Repository: replayRepoURL,
			Size: ^uint64(0), InstalledSize: ^uint64(0),
			Operation: history.OpInstall,
			After:     pkgState("2.2.5_6", false),
		}},
	}
	st := &state{stderr: io.Discard}
	plan, err := st.buildReplayPlan(entry, "redo", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.restores) != 1 {
		t.Fatalf("restores = %d, want 1", len(plan.restores))
	}
	if plan.restores[0].size != maxReplaySize || plan.restores[0].installedSize != maxReplaySize {
		t.Errorf("sizes = %d/%d, want both clamped to %d",
			plan.restores[0].size, plan.restores[0].installedSize, maxReplaySize)
	}
}

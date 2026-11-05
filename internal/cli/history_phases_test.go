package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// phaseState builds a state whose runner points at the stub xbps binaries, so
// executePhases can drive a fake two-phase replay without going through Main.
func phaseState(t *testing.T, dir string) (*state, *bytes.Buffer) {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cfg := config.Defaults()
	cfg.Xbps.InstallPath = filepath.Join(stubDir, "xbps-install")
	cfg.Xbps.RemovePath = filepath.Join(stubDir, "xbps-remove")
	cfg.Xbps.QueryPath = filepath.Join(stubDir, "xbps-query")
	cfg.Xbps.UhelperPath = filepath.Join(stubDir, "xbps-uhelper")
	st := &state{
		stdout:     io.Discard,
		stderr:     &stderr,
		stdin:      strings.NewReader(""),
		r:          &runner{cfg: cfg, out: &stderr, stdout: io.Discard, stderr: &stderr, stdin: strings.NewReader("")},
		historyDir: filepath.Join(dir, "history"),
		log:        true,
	}
	return st, &stderr
}

// setPhaseEnv installs the stub fixtures a fake replay needs: an empty before
// state, an installed after state and a temp line log.
func setPhaseEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XB_STUB_LOG", filepath.Join(dir, "commands.log"))
	t.Setenv("XB_STUB_STDOUT", "")
	t.Setenv("XB_STUB_EXIT", "0")
	t.Setenv("XB_STUB_LIST", "")
	t.Setenv("XB_STUB_MANUAL", "")
	t.Setenv("XB_STUB_HOLD", "")
	t.Setenv("XB_STUB_REPOLOCK", "")
	t.Setenv("XB_STUB_AFTER_MARK", filepath.Join(dir, "after-transaction"))
	t.Setenv("XB_STUB_LIST_AFTER", "uu figlet-2.2.5_6 figlet\n")
	t.Setenv("XB_STUB_MANUAL_AFTER", "figlet-2.2.5_6\n")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
}

func replayRows() []xbps.Row {
	return []xbps.Row{{
		Name: "figlet", Version: "2.2.5_6", Action: xbps.ActionInstall,
		Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
		InstalledSize: 672593, DownloadSize: 94372,
		HasInstalled: true, HasDownload: true,
	}}
}

// TestReplayPhasesPublishOneEntryAfterBoth pins the #70 seam this ticket
// advertises: a two-phase replay writes exactly one entry, and only after the
// second phase finishes. The second phase's script fails if an entry exists
// between the phases.
func TestReplayPhasesPublishOneEntryAfterBoth(t *testing.T) {
	dir := t.TempDir()
	setPhaseEnv(t, dir)
	st, stderr := phaseState(t, dir)
	rows := replayRows()
	replayOf := uint32(1)
	st.beginHistoryEntry("undo", &options{}, rows, []string{"undo", "1"}, []string{}, false, &replayOf, "undo")

	sentinel := filepath.Join(dir, "phase2.ran")
	phase2 := filepath.Join(dir, "phase2.sh")
	script := "#!/bin/sh\n" +
		"test ! -e \"$XB_HISTORY_ENTRY\" || exit 7\n" +
		": > \"$XB_PHASE2_RAN\"\n" +
		"exit 0\n"
	if err := os.WriteFile(phase2, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XB_HISTORY_ENTRY", filepath.Join(dir, "history", "1.json"))
	t.Setenv("XB_PHASE2_RAN", sentinel)

	code := st.executePhases("undo", []string{"undo", "1"}, rows,
		xbpsPhase{bin: st.r.cfg.Xbps.InstallPath, args: []string{"-y", "figlet"}, rows: rows},
		xbpsPhase{bin: phase2, rows: nil})
	if code != 0 {
		t.Fatalf("executePhases = %d\n%s", code, stderr.String())
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("the second phase did not run after the first: %v", err)
	}
	files := entryFiles(t, st.historyDir)
	if len(files) != 1 {
		t.Fatalf("entries = %v, want exactly one after both phases", files)
	}
	entry := readHistoryEntry(t, files[0])
	if entry["replay_of"] != float64(1) || entry["replay_kind"] != "undo" {
		t.Errorf("replay fields = %v/%v, want 1/undo", entry["replay_of"], entry["replay_kind"])
	}
	command, _ := entry["command"].([]any)
	if len(command) != 2 || command[0] != "undo" || command[1] != "1" {
		t.Errorf("command = %v, want [undo 1]", command)
	}
	logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
	raw, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(raw), " undo ") {
		t.Errorf("line log = %q, %v; want one undo line", raw, err)
	}
}

// TestReplayPhasesPartialFailureWritesNothing pins the other half: when the
// second phase fails, no entry and no line log line is written.
func TestReplayPhasesPartialFailureWritesNothing(t *testing.T) {
	dir := t.TempDir()
	setPhaseEnv(t, dir)
	st, stderr := phaseState(t, dir)
	rows := replayRows()
	replayOf := uint32(1)
	st.beginHistoryEntry("undo", &options{}, rows, []string{"undo", "1"}, []string{}, false, &replayOf, "undo")

	phase2 := filepath.Join(dir, "phase2.sh")
	if err := os.WriteFile(phase2, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := st.executePhases("undo", []string{"undo", "1"}, rows,
		xbpsPhase{bin: st.r.cfg.Xbps.InstallPath, args: []string{"-y", "figlet"}, rows: rows},
		xbpsPhase{bin: phase2})
	if code != 1 {
		t.Fatalf("executePhases = %d, want 1\n%s", code, stderr.String())
	}
	if files := entryFiles(t, st.historyDir); len(files) != 0 {
		t.Errorf("partial replay wrote entries: %v", files)
	}
	logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
	if raw, err := os.ReadFile(logPath); err == nil && len(raw) != 0 {
		t.Errorf("partial replay wrote the line log: %q", raw)
	}
}

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// historyRunEnv is one built-binary run with a private history store and the
// stub xbps binaries. It pretends to be root so the real transaction path runs
// without the suite being root; XBPRETTY_TEST_TRUST_UID keeps the temp config
// trusted by the config trust gate.
func historyRunEnv(t *testing.T, dir string) testEnv {
	t.Helper()
	return cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_TRUST_UID", strconv.Itoa(os.Getuid())).
		with("XBPRETTY_HISTORY_DIR", filepath.Join(dir, "history")).
		with("XB_STUB_AFTER_MARK", filepath.Join(dir, "after-transaction")).
		with("XB_STUB_LIST_AFTER", "uu figlet-2.2.5_6 figlet\n").
		with("XB_STUB_MANUAL_AFTER", "figlet-2.2.5_6\n").
		with("XB_STUB_HOLD_AFTER", "").
		with("XB_STUB_REPOLOCK_AFTER", "")
}

// readOnlyEnv drops the fake-root hook: reads must work as the invoking user.
func (e testEnv) readOnlyEnv() testEnv {
	e = e.without("XBPRETTY_TEST_EUID")
	return e.without("XBPRETTY_TEST_TRUST_UID")
}

func historyDirOf(dir string) string { return filepath.Join(dir, "history") }

// readHistoryEntry reads and decodes one entry file.
func readHistoryEntry(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decoding %s: %v\n%s", path, err, raw)
	}
	return entry
}

func entryFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// TestHistorySuccessfulInstallWritesSchemaOneEntry pins AC-024's recording
// half through the built binary: one <id>.json after a successful real
// transaction, schema 1, before/after snapshots from the bulk queries and the
// operation derived from them.
func TestHistorySuccessfulInstallWritesSchemaOneEntry(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	files := entryFiles(t, historyDirOf(dir))
	if len(files) != 1 {
		t.Fatalf("entry files = %v, want exactly one", files)
	}
	if got := filepath.Base(files[0]); got != "1.json" {
		t.Errorf("entry filename = %q, want 1.json", got)
	}
	if _, err := os.Stat(files[0] + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file survived: %v", err)
	}
	entry := readHistoryEntry(t, files[0])
	if entry["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", entry["schema_version"])
	}
	if entry["id"] != float64(1) {
		t.Errorf("id = %v, want 1", entry["id"])
	}
	if entry["status"] != "Applied" {
		t.Errorf("status = %v, want Applied", entry["status"])
	}
	for _, key := range []string{"started_at", "finished_at"} {
		value, _ := entry[key].(string)
		if _, err := time.Parse(time.RFC3339, value); err != nil || !strings.HasSuffix(value, "Z") {
			t.Errorf("%s = %q, want an RFC 3339 UTC timestamp", key, value)
		}
	}
	if got, _ := entry["requested_by"].(string); !regexp.MustCompile(`^.+\s\([0-9]+\)$`).MatchString(got) {
		t.Errorf("requested_by = %q, want '<user> (<uid>)'", got)
	}
	command, _ := entry["command"].([]any)
	if len(command) != 3 || command[0] != "install" || command[1] != "figlet" || command[2] != "-y" {
		t.Errorf("command = %v, want the argv without the program name", command)
	}
	targets, _ := entry["requested_targets"].([]any)
	if len(targets) != 1 || targets[0] != "figlet" {
		t.Errorf("requested_targets = %v, want [figlet]", targets)
	}
	if entry["implicit"] != false {
		t.Errorf("implicit = %v, want false", entry["implicit"])
	}
	if entry["replay_of"] != nil || entry["replay_kind"] != nil {
		t.Errorf("replay fields = %v/%v, want null/null", entry["replay_of"], entry["replay_kind"])
	}
	pkgs, _ := entry["packages"].([]any)
	if len(pkgs) != 1 {
		t.Fatalf("packages = %v, want one transition", pkgs)
	}
	pkg, _ := pkgs[0].(map[string]any)
	if pkg["name"] != "figlet" || pkg["operation"] != "Install" {
		t.Errorf("transition = %v, want figlet Install", pkg)
	}
	if pkg["arch"] != "x86_64" || !strings.Contains(pkg["repository"].(string), "repo-default.voidlinux.org") {
		t.Errorf("arch/repository = %v/%v", pkg["arch"], pkg["repository"])
	}
	if pkg["size"] != float64(94372) || pkg["installed_size"] != float64(672593) {
		t.Errorf("sizes = %v/%v, want 94372/672593", pkg["size"], pkg["installed_size"])
	}
	if pkg["before"] != nil {
		t.Errorf("before = %v, want null for an install", pkg["before"])
	}
	after, _ := pkg["after"].(map[string]any)
	if after == nil || after["version"] != "2.2.5_6" {
		t.Fatalf("after = %v, want version 2.2.5_6", pkg["after"])
	}
	if after["auto_installed"] != false {
		t.Errorf("after.auto_installed = %v, want false (manual package)", after["auto_installed"])
	}
	if after["hold"] != false || after["repolock"] != false {
		t.Errorf("after hold/repolock = %v/%v, want false/false", after["hold"], after["repolock"])
	}
}

// TestHistoryIDsAreMaxPlusOne pins FR-029's allocation through the binary: two
// successful transactions produce 1.json then 2.json.
func TestHistoryIDsAreMaxPlusOne(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir)
	for i := 0; i < 2; i++ {
		if _, errs, code := runBinary(t, bin, env, "install", "figlet", "-y"); code != 0 {
			t.Fatalf("run %d exit = %d\n%s", i+1, code, errs)
		}
	}
	if _, err := os.Stat(filepath.Join(historyDirOf(dir), "1.json")); err != nil {
		t.Errorf("first entry missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(historyDirOf(dir), "2.json")); err != nil {
		t.Errorf("second entry missing: %v", err)
	}
}

// TestHistoryListAndDetail pins AC-025's read surface through the built
// binary: ascending list columns and the detail's metadata, Replay of and
// operation groups, all root-free.
func TestHistoryListAndDetail(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir)
	for i := 0; i < 2; i++ {
		if _, errs, code := runBinary(t, bin, env, "install", "figlet", "-y"); code != 0 {
			t.Fatalf("setup run %d exit = %d\n%s", i+1, code, errs)
		}
	}
	read := env.readOnlyEnv()

	out, errs, code := runBinary(t, bin, read, "history")
	if code != 0 {
		t.Fatalf("history exit = %d\n%s", code, errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("history = %q, want a header and two rows", out)
	}
	last := -1
	for _, header := range []string{"ID", "Command", "Date", "Requested-By", "Altered"} {
		index := strings.Index(lines[0], header)
		if index < 0 {
			t.Errorf("header %q missing from %q", header, lines[0])
			continue
		}
		if index <= last {
			t.Errorf("header %q is out of order in %q", header, lines[0])
		}
		last = index
	}
	if !strings.HasPrefix(lines[1], "1 ") || !strings.HasPrefix(lines[2], "2 ") {
		t.Errorf("rows are not ascending by id:\n%s", out)
	}
	for _, line := range lines[1:] {
		if !strings.Contains(line, "install figlet -y") {
			t.Errorf("row %q is missing the command", line)
		}
		if !regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [A-Z]`).MatchString(line) {
			t.Errorf("row %q is missing the local date and time", line)
		}
		if !regexp.MustCompile(`\w+ \(\d+\)`).MatchString(line) {
			t.Errorf("row %q is missing Requested-By", line)
		}
		if !strings.HasSuffix(line, " 1") {
			t.Errorf("row %q is missing Altered = 1", line)
		}
	}

	for _, selector := range [][]string{{"history", "last"}, {"history", "1"}, {"history", "info", "2"}, {"history", "show", "last"}} {
		out, errs, code = runBinary(t, bin, read, selector...)
		if code != 0 {
			t.Errorf("%v exit = %d\n%s", selector, code, errs)
			continue
		}
		last := selector[len(selector)-1]
		wantID := "1"
		if last == "last" || last == "2" {
			wantID = "2"
		}
		if !strings.Contains(out, "ID: "+wantID) {
			t.Errorf("%v output missing ID %s:\n%s", selector, wantID, out)
		}
		group := "Reinstall:"
		if wantID == "1" {
			group = "Install:"
		}
		for _, want := range []string{"Status: Applied", "Command: install figlet -y", "Requested-By:", "Started:", "Finished:", "Requested Targets: figlet", "Altered: 1", group} {
			if !strings.Contains(out, want) {
				t.Errorf("%v output missing %q:\n%s", selector, want, out)
			}
		}
		lastLabel := -1
		for _, label := range []string{"ID:", "Status:", "Command:", "Requested-By:", "Started:", "Finished:", "Requested Targets:", "Altered:"} {
			index := strings.Index(out, label)
			if index <= lastLabel {
				t.Errorf("%v label %q is out of order:\n%s", selector, label, out)
			}
			lastLabel = index
		}
	}
}

// TestHistoryDetailReplayAndGroupOrder pins the detail's `Replay of` line and
// FR-032's fixed operation-group order, including an unparseable timestamp
// shown verbatim.
func TestHistoryDetailReplayAndGroupOrder(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir)
	historyDir := historyDirOf(dir)
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "schema_version": 1,
  "id": 1,
  "started_at": "not-a-timestamp",
  "finished_at": "2026-11-05T10:00:00Z",
  "status": "Applied",
  "requested_by": "root (0)",
  "command": ["undo", "7"],
  "requested_targets": [],
  "implicit": false,
  "replay_of": 7,
  "replay_kind": "undo",
  "packages": [
    {"name": "held", "arch": "x86_64", "repository": "r", "size": 0, "installed_size": 0, "operation": "Held", "before": {"version": "1.0_1", "auto_installed": true, "hold": true, "repolock": false}, "after": {"version": "1.0_1", "auto_installed": true, "hold": true, "repolock": false}, "held_reason": "ManualHold"},
    {"name": "new", "arch": "x86_64", "repository": "r", "size": 1, "installed_size": 2, "operation": "Install", "before": null, "after": {"version": "2.0_1", "auto_installed": false, "hold": false, "repolock": false}},
    {"name": "old", "arch": "x86_64", "repository": "r", "size": 1, "installed_size": 2, "operation": "Remove", "before": {"version": "1.0_1", "auto_installed": true, "hold": false, "repolock": false}, "after": null}
  ]
}
`
	if err := os.WriteFile(filepath.Join(historyDir, "1.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runBinary(t, bin, env.readOnlyEnv(), "history", "1")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	for _, want := range []string{
		"Replay of: 7 (undo)",
		"Started: not-a-timestamp",
		"Requested Targets: None",
		"Remove:", "Install:", "Held:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
	remove := strings.Index(out, "\nRemove:")
	install := strings.Index(out, "\nInstall:")
	held := strings.Index(out, "\nHeld:")
	if !(remove >= 0 && remove < install && install < held) {
		t.Errorf("operation groups are not in fixed order:\n%s", out)
	}
	if !strings.Contains(out, "ManualHold") {
		t.Errorf("Held reason missing:\n%s", out)
	}
}

// TestHistoryEmptyStore pins AC-043's empty-store contract: stdout line,
// exit 0, no store created, and `last` failing with the empty message.
func TestHistoryEmptyStore(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir).readOnlyEnv()
	out, errs, code := runBinary(t, bin, env, "history")
	if code != 0 {
		t.Errorf("history exit = %d, want 0", code)
	}
	if out != "No history entries found.\n" {
		t.Errorf("stdout = %q, want the empty-store line", out)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want nothing with no line log", errs)
	}
	if _, err := os.Stat(historyDirOf(dir)); !os.IsNotExist(err) {
		t.Errorf("history created the store: %v", err)
	}
	out, errs, code = runBinary(t, bin, env, "history", "last")
	if code != 1 {
		t.Errorf("history last exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("history last stdout = %q, want nothing", out)
	}
	if errs != "xbpretty: history is empty\n" {
		t.Errorf("stderr = %q, want the empty message", errs)
	}
}

// TestHistoryLineLogNote pins AC-043's note: only a readable, non-empty line
// log produces it, and no history file is created either way.
func TestHistoryLineLogNote(t *testing.T) {
	bin := taggedBinary(t)
	const note = "xbpretty: earlier transactions are only in the line log and cannot be replayed; run 'xbp log' to read them\n"
	for _, tt := range []struct {
		name    string
		content string
		want    string
	}{
		{"legacy lines", "2026-01-01T00:00:00Z install figlet-2.2.5_6\n", note},
		{"empty log", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			env := historyRunEnv(t, dir).readOnlyEnv()
			logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
			if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(logPath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			out, errs, code := runBinary(t, bin, env, "history")
			if code != 0 {
				t.Fatalf("exit = %d\n%s", code, errs)
			}
			if out != "No history entries found.\n" {
				t.Errorf("stdout = %q", out)
			}
			if errs != tt.want {
				t.Errorf("stderr = %q, want %q", errs, tt.want)
			}
			if _, err := os.Stat(historyDirOf(dir)); !os.IsNotExist(err) {
				t.Errorf("history created the store: %v", err)
			}
		})
	}
}

// TestHistoryErrors pins FR-032's selector and corruption errors and exit
// codes.
func TestHistoryErrors(t *testing.T) {
	bin := taggedBinary(t)
	cases := []struct {
		name  string
		args  []string
		code  int
		want  string
		setup func(t *testing.T, historyDir string)
	}{
		{name: "bad selector", args: []string{"history", "abc"}, code: 2,
			want: "xbpretty: invalid history selector \"abc\"; use an integer id or 'last'"},
		{name: "unknown id", args: []string{"history", "999"}, code: 1,
			want: "xbpretty: history entry 999 not found"},
		{name: "info without target", args: []string{"history", "info"}, code: 2,
			want: "xbpretty: history info needs an entry id or 'last'"},
		{name: "package arguments", args: []string{"history", "1", "2"}, code: 2,
			want: "xbpretty: history takes no package arguments"},
		{name: "corrupt entry", args: []string{"history"}, code: 1,
			want: "1.json",
			setup: func(t *testing.T, historyDir string) {
				if err := os.MkdirAll(historyDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(historyDir, "1.json"), []byte("{not json"), 0o644); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "future schema", args: []string{"history", "1"}, code: 1,
			want: "schema_version",
			setup: func(t *testing.T, historyDir string) {
				if err := os.MkdirAll(historyDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(historyDir, "1.json"), []byte(`{"schema_version":2,"id":1,"packages":[]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Each case gets its own store so corruption setups never leak.
			caseDir := t.TempDir()
			caseEnv := historyRunEnv(t, caseDir).readOnlyEnv()
			if tt.setup != nil {
				tt.setup(t, historyDirOf(caseDir))
			}
			out, errs, code := runBinary(t, bin, caseEnv, tt.args...)
			if code != tt.code {
				t.Errorf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tt.code, out, errs)
			}
			if !strings.Contains(errs, tt.want) {
				t.Errorf("stderr = %q, want %q", errs, tt.want)
			}
		})
	}
}

func assertNoHistoryEntry(t *testing.T, dir string) {
	t.Helper()
	if files := entryFiles(t, historyDirOf(dir)); len(files) != 0 {
		t.Errorf("history entries written: %v", files)
	}
}

// TestHistoryNoWriteMatrix pins FR-031's negative list through the built
// binary: none of these runs may create an entry.
func TestHistoryNoWriteMatrix(t *testing.T) {
	bin := taggedBinary(t)
	holdPlan := "figlet-2.2.5_6 hold x86_64 https://repo-default.voidlinux.org/current\n"
	cases := []struct {
		name  string
		env   func(t *testing.T, dir string) testEnv
		args  []string
		code  int
		noLog bool
	}{
		{name: "dry run", args: []string{"install", "figlet", "--dry-run", "-y"}, code: 0},
		{name: "download only", args: []string{"install", "figlet", "-y", "--download-only"}, code: 1},
		{name: "assume no", args: []string{"install", "figlet", "-n"}, code: 1},
		{name: "non-tty", args: []string{"install", "figlet"}, code: 1},
		{name: "empty plan", env: func(t *testing.T, dir string) testEnv {
			return historyRunEnv(t, dir).with("XB_STUB_STDOUT", "")
		}, args: []string{"install", "figlet", "-y"}, code: 0},
		{name: "hold-only plan", env: func(t *testing.T, dir string) testEnv {
			return historyRunEnv(t, dir).with("XB_STUB_STDOUT", holdPlan)
		}, args: []string{"install", "figlet", "-y"}, code: 0},
		{name: "failed child", env: func(t *testing.T, dir string) testEnv {
			return historyRunEnv(t, dir).with("XB_STUB_EXIT", "1")
		}, args: []string{"install", "figlet", "-y"}, code: 1},
		{name: "failed real execution", env: func(t *testing.T, dir string) testEnv {
			return historyRunEnv(t, dir).with("XB_STUB_RUN_EXIT", "1")
		}, args: []string{"install", "figlet", "-y"}, code: 1, noLog: true},
		{name: "update", args: []string{"update"}, code: 0},
		// #64 implemented clean: xbps-remove -O succeeds against the stub and
		// still writes no history entry.
		{name: "clean", args: []string{"clean"}, code: 0},
		{name: "download", args: []string{"download", "figlet"}, code: 1},
		{name: "non-root", env: func(t *testing.T, dir string) testEnv {
			return historyRunEnv(t, dir).readOnlyEnv()
		}, args: []string{"install", "figlet", "-y"}, code: 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			env := historyRunEnv(t, dir)
			if tt.env != nil {
				env = tt.env(t, dir)
			}
			_, errs, code := runBinary(t, bin, env, tt.args...)
			if code != tt.code {
				t.Errorf("exit = %d, want %d\n%s", code, tt.code, errs)
			}
			assertNoHistoryEntry(t, dir)
			if tt.noLog {
				logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
				if raw, err := os.ReadFile(logPath); err == nil && len(raw) != 0 {
					t.Errorf("failed real execution wrote the line log: %q", raw)
				}
			}
		})
	}
}

// TestHistoryWriteFailureIsAWarning pins FR-031's best-effort write: a store
// that cannot be written warns and leaves the run's exit code alone.
func TestHistoryWriteFailureIsAWarning(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	// A regular file where the store directory belongs makes the write fail
	// for root and non-root alike.
	if err := os.WriteFile(historyDirOf(dir), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := historyRunEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 0 {
		t.Errorf("exit = %d, want 0 despite the history failure\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: history entry not saved:") {
		t.Errorf("stderr = %q, want the history warning", errs)
	}
	// The line log is independent of the failed history write.
	logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
	raw, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(raw), " install figlet-2.2.5_6") {
		t.Errorf("line log = %q, %v; want the transaction line despite the history failure", raw, err)
	}
}

// TestHistoryDualWrite pins FR-035's line-log split: a successful transaction
// writes both, and --no-log stops only the line log.
func TestHistoryDualWrite(t *testing.T) {
	bin := taggedBinary(t)
	t.Run("default writes both", func(t *testing.T) {
		dir := t.TempDir()
		env := historyRunEnv(t, dir)
		if _, errs, code := runBinary(t, bin, env, "install", "figlet", "-y"); code != 0 {
			t.Fatalf("exit = %d\n%s", code, errs)
		}
		if files := entryFiles(t, historyDirOf(dir)); len(files) != 1 {
			t.Errorf("history entries = %v, want one", files)
		}
		logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("reading the line log: %v", err)
		}
		if !strings.Contains(string(raw), " install figlet-2.2.5_6") {
			t.Errorf("line log = %q, want the install line", raw)
		}
	})
	t.Run("--no-log keeps history", func(t *testing.T) {
		dir := t.TempDir()
		env := historyRunEnv(t, dir)
		if _, errs, code := runBinary(t, bin, env, "install", "figlet", "-y", "--no-log"); code != 0 {
			t.Fatalf("exit = %d\n%s", code, errs)
		}
		if files := entryFiles(t, historyDirOf(dir)); len(files) != 1 {
			t.Errorf("--no-log disabled history: entries = %v", files)
		}
		logPath := filepath.Join(dir, "state", "xbpretty", "transactions.log")
		if raw, err := os.ReadFile(logPath); err == nil && len(raw) != 0 {
			t.Errorf("--no-log wrote the line log: %q", raw)
		}
	})
}

// TestHistoryOperationsFromSnapshots pins FR-030's derivation through the
// built binary: each operation comes from the before/after bulk snapshots and
// the plan action, not from the command name.
func TestHistoryOperationsFromSnapshots(t *testing.T) {
	bin := taggedBinary(t)
	const repo = "https://repo-default.voidlinux.org/current"
	cases := []struct {
		name         string
		args         []string
		plan         string
		beforeList   string
		afterList    string
		beforeManual string
		afterManual  string
		cmpverExit   string
		want         string
		wantBefore   bool
		wantAfter    bool
	}{
		{name: "install", args: []string{"install", "figlet", "-y"},
			plan:        "figlet-2.2.5_6 install x86_64 " + repo + " 672593 94372\n",
			afterList:   "uu figlet-2.2.5_6 figlet\n",
			afterManual: "figlet-2.2.5_6\n",
			want:        "Install", wantAfter: true},
		{name: "upgrade", args: []string{"upgrade", "-y"},
			plan:         "figlet-2.0_1 update x86_64 " + repo + " 100 50\n",
			beforeList:   "uu figlet-1.0_1 figlet\n",
			afterList:    "uu figlet-2.0_1 figlet\n",
			beforeManual: "figlet-1.0_1\n", afterManual: "figlet-2.0_1\n",
			cmpverExit: "255", want: "Upgrade", wantBefore: true, wantAfter: true},
		{name: "downgrade", args: []string{"upgrade", "-y"},
			plan:         "figlet-1.0_1 update x86_64 " + repo + " 100 50\n",
			beforeList:   "uu figlet-2.0_1 figlet\n",
			afterList:    "uu figlet-1.0_1 figlet\n",
			beforeManual: "figlet-2.0_1\n", afterManual: "figlet-1.0_1\n",
			cmpverExit: "1", want: "Downgrade", wantBefore: true, wantAfter: true},
		{name: "remove", args: []string{"remove", "wget", "-y"},
			plan:         "wget-1.25.0_1 remove x86_64 " + repo + " 3673038 824658\n",
			beforeList:   "uu wget-1.25.0_1 wget\n",
			beforeManual: "wget-1.25.0_1\n",
			want:         "Remove", wantBefore: true},
		{name: "purge", args: []string{"purge", "wget", "-y"},
			plan:         "wget-1.25.0_1 remove x86_64 " + repo + " 3673038 824658\n",
			beforeList:   "uu wget-1.25.0_1 wget\n",
			beforeManual: "wget-1.25.0_1\n",
			want:         "Purge", wantBefore: true},
		{name: "remove passthrough -f", args: []string{"remove", "wget", "-y", "--", "-f"},
			plan:         "wget-1.25.0_1 remove x86_64 " + repo + " 3673038 824658\n",
			beforeList:   "uu wget-1.25.0_1 wget\n",
			beforeManual: "wget-1.25.0_1\n",
			want:         "Purge", wantBefore: true},
		{name: "remove passthrough -fy cluster", args: []string{"remove", "wget", "-y", "--", "-fy"},
			plan:         "wget-1.25.0_1 remove x86_64 " + repo + " 3673038 824658\n",
			beforeList:   "uu wget-1.25.0_1 wget\n",
			beforeManual: "wget-1.25.0_1\n",
			want:         "Purge", wantBefore: true},
		{name: "autoremove", args: []string{"autoremove", "-y"},
			plan:         "wget-1.25.0_1 remove x86_64 " + repo + " 3673038 824658\n",
			beforeList:   "uu wget-1.25.0_1 wget\n",
			beforeManual: "wget-1.25.0_1\n",
			want:         "AutoRemove", wantBefore: true},
		{name: "autopurge", args: []string{"autopurge", "-y"},
			plan:         "wget-1.25.0_1 remove x86_64 " + repo + " 3673038 824658\n",
			beforeList:   "uu wget-1.25.0_1 wget\n",
			beforeManual: "wget-1.25.0_1\n",
			want:         "AutoPurge", wantBefore: true},
		{name: "reinstall", args: []string{"install", "figlet", "--reinstall", "-y"},
			plan:         "figlet-2.2.5_6 reinstall x86_64 " + repo + " 100 50\n",
			beforeList:   "uu figlet-2.2.5_6 figlet\n",
			afterList:    "uu figlet-2.2.5_6 figlet\n",
			beforeManual: "figlet-2.2.5_6\n", afterManual: "figlet-2.2.5_6\n",
			want: "Reinstall", wantBefore: true, wantAfter: true},
		{name: "configure", args: []string{"install", "figlet", "-y"},
			plan:         "figlet-2.2.5_6 configure x86_64 " + repo + "\n",
			beforeList:   "uu figlet-2.2.5_6 figlet\n",
			afterList:    "uu figlet-2.2.5_6 figlet\n",
			beforeManual: "figlet-2.2.5_6\n", afterManual: "figlet-2.2.5_6\n",
			want: "Configure", wantBefore: true, wantAfter: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			env := historyRunEnv(t, dir).
				with("XB_STUB_STDOUT", tt.plan).
				with("XB_STUB_LIST", tt.beforeList).
				with("XB_STUB_MANUAL", tt.beforeManual).
				with("XB_STUB_LIST_AFTER", tt.afterList).
				with("XB_STUB_MANUAL_AFTER", tt.afterManual)
			if tt.cmpverExit != "" {
				env = env.with("XB_STUB_CMPVER_EXIT", tt.cmpverExit)
			}
			if _, errs, code := runBinary(t, bin, env, tt.args...); code != 0 {
				t.Fatalf("exit = %d\n%s", code, errs)
			}
			files := entryFiles(t, historyDirOf(dir))
			if len(files) != 1 {
				t.Fatalf("entries = %v, want one", files)
			}
			entry := readHistoryEntry(t, files[0])
			pkgs, _ := entry["packages"].([]any)
			if len(pkgs) != 1 {
				t.Fatalf("packages = %v, want one", pkgs)
			}
			pkg, _ := pkgs[0].(map[string]any)
			if pkg["operation"] != tt.want {
				t.Errorf("operation = %v, want %s", pkg["operation"], tt.want)
			}
			if got := pkg["before"] != nil; got != tt.wantBefore {
				t.Errorf("before present = %v, want %v", got, tt.wantBefore)
			}
			if got := pkg["after"] != nil; got != tt.wantAfter {
				t.Errorf("after present = %v, want %v", got, tt.wantAfter)
			}
		})
	}
}

// TestHistoryRecordsHeldRowsWithReason pins FR-031: hold rows are recorded
// with the entry but are never replayed, and a mixed plan keeps both.
func TestHistoryRecordsHeldRowsWithReason(t *testing.T) {
	bin := taggedBinary(t)
	const repo = "https://repo-default.voidlinux.org/current"
	dir := t.TempDir()
	env := historyRunEnv(t, dir).
		with("XB_STUB_STDOUT",
			"other-1.0_1 hold x86_64 "+repo+"\n"+
				"newpkg-1.0_1 install x86_64 "+repo+" 100 50\n").
		with("XB_STUB_LIST", "uu other-1.0_1 other\n").
		with("XB_STUB_MANUAL", "other-1.0_1\n").
		with("XB_STUB_LIST_AFTER", "uu other-1.0_1 other\nuu newpkg-1.0_1 newpkg\n").
		with("XB_STUB_MANUAL_AFTER", "other-1.0_1\n")
	if _, errs, code := runBinary(t, bin, env, "install", "newpkg", "-y"); code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	files := entryFiles(t, historyDirOf(dir))
	if len(files) != 1 {
		t.Fatalf("entries = %v, want one", files)
	}
	entry := readHistoryEntry(t, files[0])
	pkgs, _ := entry["packages"].([]any)
	if len(pkgs) != 2 {
		t.Fatalf("packages = %v, want held and install", pkgs)
	}
	operations := map[string]map[string]any{}
	for _, raw := range pkgs {
		pkg, _ := raw.(map[string]any)
		operations[pkg["operation"].(string)] = pkg
	}
	held, ok := operations["Held"]
	if !ok {
		t.Fatalf("no Held row: %v", operations)
	}
	if held["name"] != "other" || held["held_reason"] != "ManualHold" {
		t.Errorf("held row = %v, want other/ManualHold", held)
	}
	if _, ok := operations["Install"]; !ok {
		t.Errorf("no Install row: %v", operations)
	}
}

// TestHistorySnapshotBits pins the four bulk queries behind the recorded
// state: auto_installed is the inverse of the manual pass, and hold/repolock
// come from -H and --list-repolock-pkgs.
func TestHistorySnapshotBits(t *testing.T) {
	bin := taggedBinary(t)
	const repo = "https://repo-default.voidlinux.org/current"
	dir := t.TempDir()
	env := historyRunEnv(t, dir).
		with("XB_STUB_STDOUT", "dep-1.0_1 install x86_64 "+repo+" 100 50\n").
		with("XB_STUB_MANUAL_AFTER", "").
		with("XB_STUB_LIST_AFTER", "uu dep-1.0_1 dep\n").
		with("XB_STUB_HOLD_AFTER", "dep-1.0_1\n").
		with("XB_STUB_REPOLOCK_AFTER", "dep-1.0_1\n")
	if _, errs, code := runBinary(t, bin, env, "install", "dep", "-y"); code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	files := entryFiles(t, historyDirOf(dir))
	if len(files) != 1 {
		t.Fatalf("entries = %v, want one", files)
	}
	pkg, _ := readHistoryEntry(t, files[0])["packages"].([]any)[0].(map[string]any)
	after, _ := pkg["after"].(map[string]any)
	if after["auto_installed"] != true {
		t.Errorf("auto_installed = %v, want true (not in the manual pass)", after["auto_installed"])
	}
	if after["hold"] != true || after["repolock"] != true {
		t.Errorf("hold/repolock = %v/%v, want true/true", after["hold"], after["repolock"])
	}
}

// TestHistoryDirPrecedenceThroughBinary pins FR-029's resolution through the binary:
// --history-dir beats XBPRETTY_HISTORY_DIR, which beats the default.
func TestHistoryDirPrecedenceThroughBinary(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env-history")
	cliDir := filepath.Join(dir, "cli-history")
	env := historyRunEnv(t, dir).with("XBPRETTY_HISTORY_DIR", envDir)
	if _, errs, code := runBinary(t, bin, env, "install", "figlet", "-y", "--history-dir", cliDir); code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if files := entryFiles(t, cliDir); len(files) != 1 {
		t.Errorf("--history-dir entries = %v, want one", files)
	}
	if files := entryFiles(t, envDir); len(files) != 0 {
		t.Errorf("XBPRETTY_HISTORY_DIR entries = %v, want none", files)
	}
	// Reads honour the same flag.
	out, errs, code := runBinary(t, bin, env.readOnlyEnv(), "history", "--history-dir", cliDir)
	if code != 0 || !strings.Contains(out, "install figlet -y") {
		t.Errorf("history --history-dir = %d, %q, %q", code, out, errs)
	}
}

// TestHistoryNoWriteOnFetch pins FR-031's fetch exclusion through the binary
// with a local mirror fixture, so no network is touched.
func TestHistoryNoWriteOnFetch(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mirrors.lst":
			fmt.Fprintf(w, "EU\t%s/m/\t\t1\n", server.URL)
		case strings.HasSuffix(r.URL.Path, "/current/x86_64-repodata"):
			_, _ = w.Write([]byte{0x28, 0xb5, 0x2f, 0xfd, 0, 0, 0, 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	xbpsd := filepath.Join(dir, "xbps.d")
	if err := os.MkdirAll(xbpsd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xbpsd, "00-repository-main.conf"),
		[]byte("repository=https://repo-default.voidlinux.org/current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := historyRunEnv(t, dir).
		with("XBPRETTY_TEST_FETCH_XBPSD", xbpsd).
		with("XBPRETTY_TEST_FETCH_MIRROR_LIST", server.URL+"/mirrors.lst").
		with("XBPRETTY_TEST_FETCH_ARCH", "x86_64")
	out, errs, code := runBinary(t, bin, env, "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("fetch --dry-run exit = %d\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "Dry run:") {
		t.Errorf("fetch dry run did not complete:\n%s", out)
	}
	assertNoHistoryEntry(t, dir)
}

// TestUntaggedBinaryIgnoresTestEUID pins the test hook's build tag: the
// release binary never treats XBPRETTY_TEST_EUID as root, so it takes the
// non-root path and writes no history.
func TestUntaggedBinaryIgnoresTestEUID(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := historyRunEnv(t, dir) // EUID/TRUST are ignored by the untagged build
	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 1 {
		t.Fatalf("exit = %d, want the non-root refusal\n%s", code, errs)
	}
	if !strings.Contains(errs, "sudo") && !strings.Contains(errs, "doas") {
		t.Errorf("stderr = %q, want the elevation hint", errs)
	}
	assertNoHistoryEntry(t, dir)
}

package cli

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
)

// removePlanFixture is a captured `xbps-remove -n` plan.
const removePlanFixture = "wget-1.25.0_1 remove x86_64 https://repo-default.voidlinux.org/current 3673038 824658\n"

// planFixture is a one-line xbps install plan, exactly as captured from
// xbps 0.60.7 (see testdata/parser/README.md).
const planFixture = "figlet-2.2.5_6 install x86_64 https://repo-default.voidlinux.org/current 672593 94372\n"

// env is one test's world: a config file pointing at stub xbps binaries,
// fixture variables for those stubs, and the command log.
type env struct {
	t       *testing.T
	dir     string
	logPath string
	tty     bool
	root    bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatalf("resolving stub dir: %v", err)
	}
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := strings.Join([]string{
		"xbps_install_path = \"" + filepath.Join(stubDir, "xbps-install") + "\"",
		"xbps_remove_path = \"" + filepath.Join(stubDir, "xbps-remove") + "\"",
		"xbps_query_path = \"" + filepath.Join(stubDir, "xbps-query") + "\"",
		"color = \"never\"",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "xbpretty", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("NO_COLOR", "")
	t.Setenv("XB_STUB_LOG", filepath.Join(dir, "commands.log"))
	t.Setenv("XB_STUB_STDOUT", planFixture)
	t.Setenv("XB_STUB_STDERR", "")
	t.Setenv("XB_STUB_EXIT", "0")
	t.Setenv("XB_STUB_HOLD", "")
	t.Setenv("XB_STUB_OLD", "")
	t.Setenv("XB_STUB_REVDEPS", "")
	t.Setenv("XB_STUB_QUERY", "figlet-2.2.5_6\n657KB\n")

	e := &env{t: t, dir: dir, logPath: filepath.Join(dir, "commands.log")}

	origTerminal, origEuid := isTerminalFn, euidFn
	isTerminalFn = func(f *os.File) bool { return e.tty }
	euidFn = func() int {
		if e.root {
			return 0
		}
		return 1000
	}
	t.Cleanup(func() {
		isTerminalFn, euidFn = origTerminal, origEuid
	})
	return e
}

// isDryRun reports whether a stub invocation was a plan: xbps accepts both
// `-n` and the combined `-Sn`.
func isDryRun(cmd string) bool {
	return strings.Contains(cmd, "-Sn") || strings.Contains(cmd, " -n")
}

// setAnswers scripts the prompt's replies. It must be called before run:
// Main reads the prompt input once, at start-up.
func (e *env) setAnswers(answers string) {
	e.t.Helper()
	path := filepath.Join(e.dir, "answers.txt")
	if err := os.WriteFile(path, []byte(answers), 0o644); err != nil {
		e.t.Fatal(err)
	}
	orig := stdinFn
	stdinFn = func() io.Reader {
		f, err := os.Open(path)
		if err != nil {
			e.t.Fatalf("opening answers: %v", err)
		}
		return f
	}
	e.t.Cleanup(func() { stdinFn = orig })
}

// set writes a stub environment variable for the rest of the test.
func (e *env) set(key, value string) { e.t.Setenv(key, value) }

// run invokes Main and returns its exit code, stdout and stderr.
func (e *env) run(argv ...string) (int, string, string) {
	e.t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		e.t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		e.t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	code := Main(argv)
	os.Stdout, os.Stderr = origOut, origErr
	outW.Close()
	errW.Close()
	out, _ := io.ReadAll(outR)
	errs, _ := io.ReadAll(errR)
	outR.Close()
	errR.Close()
	return code, string(out), string(errs)
}

// commands returns the argv of every stub xbps invocation.
func (e *env) commands() []string {
	e.t.Helper()
	raw, err := os.ReadFile(e.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		e.t.Fatalf("reading command log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestAC001NonRootSummaryHintAndExit(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	code, out, errs := e.run("xbpretty", "install", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "Install") || !strings.Contains(out, "figlet") {
		t.Errorf("stdout is missing the summary:\n%s", out)
	}
	if !strings.Contains(errs, "doas") && !strings.Contains(errs, "sudo") {
		t.Errorf("stderr is missing the elevation hint:\n%s", errs)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted a non-root user:\n%s", errs)
	}
}

func TestAC004PipedStdinHintsYes(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	code, out, errs := e.run("xbpretty", "install", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("summary missing:\n%s", out)
	}
	if !strings.Contains(errs, "-y") {
		t.Errorf("missing the -y hint:\n%s", errs)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted on a pipe:\n%s", errs)
	}
}

func TestAC005DryRunExecutesNothing(t *testing.T) {
	e := newEnv(t)
	e.root = true
	code, out, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("summary missing:\n%s", out)
	}
	cmds := e.commands()
	for _, c := range cmds {
		if strings.HasPrefix(c, "-y") || strings.Contains(c, " -y") {
			t.Errorf("dry run executed a real command: %q", c)
		}
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("dry run prompted:\n%s", errs)
	}
}

func TestAC011NothingToDo(t *testing.T) {
	e := newEnv(t)
	e.set("XB_STUB_STDOUT", "")
	code, out, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Nothing to do.") {
		t.Errorf("stdout = %q, want Nothing to do.", out)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted with nothing to do:\n%s", errs)
	}
}

// TestAC007UnprivilegedUpgradePlanOmitsSyncAndWarns is AC-007 for the command
// it names: upgrade drops -S in an unprivileged plan and says why the cached
// index may be stale.
func TestAC007UnprivilegedUpgradePlanOmitsSyncAndWarns(t *testing.T) {
	e := newEnv(t)
	e.root = false
	code, _, errs := e.run("xbpretty", "upgrade", "-n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	cmds := e.transactionCalls()
	if len(cmds) != 1 || cmds[0] != "-un" {
		t.Errorf("commands = %q, want one -un dry run", cmds)
	}
	if !strings.Contains(errs, "stale") {
		t.Errorf("missing the stale-index warning:\n%s", errs)
	}
}

// planCapture reads a captured dry-run plan from the parser corpus.
func planCapture(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/parser", path))
	if err != nil {
		t.Fatalf("reading capture %s: %v", path, err)
	}
	return string(raw)
}

// rowLine returns the summary line naming a package, or "" when the table
// does not carry it.
func rowLine(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name+" ") {
			return line
		}
	}
	return ""
}

// TestAC009AutoremoveOrphansLandInRemoveGroup drives the captured
// `xbps-remove -on` plan (testdata/parser/0607/autoremove.txt): the orphans
// are the Remove group, each row carrying the bytes its removal frees.
func TestAC009AutoremoveOrphansLandInRemoveGroup(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/autoremove.txt"))
	code, out, errs := e.run("xbpretty", "autoremove", "-n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-on" {
		t.Errorf("commands = %q, want one -on dry run", cmds)
	}
	if !strings.Contains(out, "Remove") {
		t.Errorf("no Remove group:\n%s", out)
	}
	for _, row := range []struct{ name, version, size string }{
		{"cups", "2.4.19_1", "13.1 MiB"},
		{"font-ibm-plex-ttf", "6.4.0_2", "58.3 MiB"},
		{"lsof", "4.99.7_1", "333.4 KiB"},
		{"steam-udev-rules", "1.0.0.61+20260625_1", "17.2 KiB"},
		{"zenity", "4.0.5_1", "10.5 MiB"},
	} {
		line := rowLine(out, row.name)
		if line == "" {
			t.Errorf("%s is missing from the summary:\n%s", row.name, out)
			continue
		}
		if !strings.Contains(line, row.version) || !strings.Contains(line, row.size) {
			t.Errorf("%s row = %q, want version %s and size %s", row.name, line, row.version, row.size)
		}
	}
	if !strings.Contains(out, "5 remove") {
		t.Errorf("footer is missing the remove count:\n%s", out)
	}
	if !strings.Contains(out, "-82.3 MiB disk") {
		t.Errorf("footer is missing the freed disk total:\n%s", out)
	}
}

func TestAC015FailureGetsHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_STDERR", "ERROR: Package 'figlet' not found in repository pool.\n")
	e.set("XB_STUB_PLAN_EXIT", "2")
	code, _, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (xbps's code)", code)
	}
	if !strings.Contains(errs, "not found in repository pool") {
		t.Errorf("raw xbps stderr missing:\n%s", errs)
	}
	if !strings.Contains(errs, "hint:") {
		t.Errorf("missing hint:\n%s", errs)
	}
}

func TestAC016UnmatchedFailureGetsNoHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_STDERR", "ERROR: something unprecedented happened.\n")
	e.set("XB_STUB_PLAN_EXIT", "2")
	code, _, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errs, "something unprecedented") {
		t.Errorf("raw stderr missing:\n%s", errs)
	}
	if strings.Contains(errs, "hint:") {
		t.Errorf("guessed a hint:\n%s", errs)
	}
}

func TestAC017FailedRemoveListsDependents(t *testing.T) {
	e := newEnv(t)
	e.root = true
	// The plan names the packages the remove is aimed at.
	e.set("XB_STUB_STDOUT", removePlanFixture)
	e.set("XB_STUB_REVDEPS", "bar-2.0_1\nbaz-3.0_1\n")
	// The dry run succeeds and tells us which packages are targeted.
	code, _, errs := e.run("xbpretty", "remove", "wget", "-n")
	if code != 0 {
		t.Fatalf("dry-run exit = %d, want 0", code)
	}
	// The real run then fails, and the dependents are reported. The plan
	// stays available so the remove still knows what it was aimed at.
	e.set("XB_STUB_RUN_STDERR", "ERROR: failed to remove wget\n")
	e.set("XB_STUB_EXIT", "1")
	code, _, errs = e.run("xbpretty", "remove", "wget", "-y")
	if code == 0 {
		t.Fatalf("exit = 0, want the failing xbps code")
	}
	if !strings.Contains(errs, "ERROR: failed to remove wget") {
		t.Errorf("raw xbps stderr missing:\n%s", errs)
	}
	if !strings.Contains(errs, "bar-2.0_1") || !strings.Contains(errs, "baz-3.0_1") {
		t.Errorf("dependents not listed:\n%s", errs)
	}
	if !strings.Contains(errs, "-- -F wget") {
		t.Errorf("force flag not mentioned:\n%s", errs)
	}
}

// ranRealCommand reports whether a non-dry xbps command was executed.
func ranRealCommand(e *env) bool {
	for _, c := range e.commands() {
		if strings.Contains(c, "-y") {
			return true
		}
	}
	return false
}

// TestAC019ConfigAssumeYesSkipsPrompt drives the config key, not a flag, at a
// run that actually reaches the prompt: an empty plan returns at FR-015's
// "Nothing to do." before the question is asked, so a test built on one can
// never fail.
func TestAC019ConfigAssumeYesSkipsPrompt(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	cfg := filepath.Join(e.dir, "config", "xbpretty", "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(data, []byte("assume_yes = true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := e.run("xbpretty", "install", "figlet")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted despite assume_yes:\n%s", errs)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("summary missing from stdout:\n%s", out)
	}
	if !ranRealCommand(e) {
		t.Errorf("the transaction never ran past the prompt: %q", e.commands())
	}
}

// TestAC018MissingXBPSBinaryExitsOne pins FR-018a: a configured binary that
// is not there is a clear error with exit 1, and nothing is run.
func TestAC018MissingXBPSBinaryExitsOne(t *testing.T) {
	e := newEnv(t)
	e.root = true
	missing := filepath.Join(e.dir, "no-such-xbps-install")
	e.pointInstallAt(missing)
	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errs, missing) || !strings.Contains(errs, "not found") {
		t.Errorf("stderr does not name the missing binary clearly:\n%s", errs)
	}
	if cmds := e.commands(); len(cmds) != 0 {
		t.Errorf("something ran despite the missing binary: %q", cmds)
	}
}

func TestAC020MalformedConfigExits2(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.dir, "config", "xbpretty", "config.toml")
	if err := os.WriteFile(cfg, []byte("color = \"blue\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := e.run("xbpretty", "install", "figlet")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errs, "line 1") {
		t.Errorf("missing the parse error line number:\n%s", errs)
	}
}

func TestAC021StdoutHoldsOnlyTheSummary(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.set("XB_STUB_STDOUT", planFixture)
	_, out, errs := e.run("xbpretty", "install", "figlet", "-n")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("stdout carries ANSI escapes:\n%q", out)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("summary missing from stdout:\n%s", out)
	}
	if strings.Contains(errs, "Install") {
		t.Errorf("summary leaked to stderr:\n%s", errs)
	}
}

// TestAC010RemovalFreesWithNoDownload covers FR-011 and FR-012 over the
// captured remove plan (testdata/parser/0607/remove.txt): the row reports the
// bytes the removal frees and the footer reports no download, because a
// removal fetches nothing.
func TestAC010RemovalFreesWithNoDownload(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", removePlanFixture)
	code, out, errs := e.run("xbpretty", "remove", "wget", "-n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(out, "805.3 KiB") {
		t.Errorf("the removal is shown by its download size:\n%s", out)
	}
	if !strings.Contains(out, "wget  1.25.0_1  3.5 MiB") {
		t.Errorf("the removal's freed bytes are missing:\n%s", out)
	}
	if strings.Contains(out, "download") {
		t.Errorf("the footer reports a download for a removal:\n%s", out)
	}
	if !strings.Contains(out, "-3.5 MiB disk") {
		t.Errorf("the footer is missing the freed disk delta:\n%s", out)
	}
}

// TestAC023PassthroughReachesBothRuns pins FR-022 for the real run too: the
// pass-through has to survive into the command that executes, not just the
// preview.
func TestAC023PassthroughReachesBothRuns(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", planFixture)
	code, _, errs := e.run("xbpretty", "install", "figlet", "-y", "-v", "--", "-f", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(errs, "-Sn figlet -f figlet") {
		t.Errorf("passthrough missing from the dry run:\n%s", errs)
	}
	if !strings.Contains(errs, "-y figlet -f figlet") {
		t.Errorf("passthrough missing from the real run:\n%s", errs)
	}
}

func TestAC024VerbosePrintsEachCommand(t *testing.T) {
	e := newEnv(t)
	e.root = true
	_, _, errs := e.run("xbpretty", "install", "figlet", "-n", "-v")
	if !strings.Contains(errs, "verbose:") || !strings.Contains(errs, " -Sn figlet") {
		t.Errorf("dry-run command not printed:\n%s", errs)
	}
	if !strings.Contains(errs, "xbps-query") {
		t.Errorf("query command not printed:\n%s", errs)
	}
}

func TestAC022Argv0Dispatch(t *testing.T) {
	e := newEnv(t)
	e.root = true
	// A symlink named xbp-remove must behave exactly like `remove`.
	if got, ok := commandFromArgv("/usr/bin/xbp-remove"); !ok || got != "remove" {
		t.Errorf("commandFromArgv(xbp-remove) = %q, %v; want remove, true", got, ok)
	}
	if got, ok := commandFromArgv("xbp-upgrade"); !ok || got != "upgrade" {
		t.Errorf("commandFromArgv(xbp-upgrade) = %q, %v; want upgrade, true", got, ok)
	}
	if got, ok := commandFromArgv("xbpretty"); ok {
		t.Errorf("commandFromArgv(xbpretty) = %q, true; want no command from the binary name", got)
	}
	if got, ok := commandFromArgv("xbp"); ok {
		t.Errorf("commandFromArgv(xbp) = %q, true; want no command from the binary name", got)
	}
}

func TestAC002EnterRunsDryRunThenRealWithoutSecondSync(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.setAnswers("\n")
	code, _, errs := e.run("xbpretty", "install", "figlet")
	if code != 0 {
		t.Errorf("exit = %d, want 0 (the child's code)", code)
	}
	if strings.Contains(errs, "warning: not running as root") {
		t.Errorf("unexpected unprivileged warning:\n%s", errs)
	}
	cmds := e.commands()
	var plan, real string
	for _, c := range cmds {
		switch {
		case isDryRun(c):
			plan = c
		case strings.Contains(c, "-y"):
			real = c
		}
	}
	if !strings.Contains(plan, "-Sn") {
		t.Errorf("dry run = %q, want -Sn (index synced as root)", plan)
	}
	if !strings.Contains(real, "-y") {
		t.Errorf("real run = %q, want -y", real)
	}
	if strings.Contains(real, "-S") {
		t.Errorf("real run = %q, must not sync a second time (FR-008)", real)
	}
}

func TestAC003PromptAnswers(t *testing.T) {
	tests := []struct {
		name    string
		answers string
		want    int
		ranReal bool
	}{
		{"decline exits 0 without running", "n\n", 0, false},
		{"anything else re-asks then accepts", "x\ny\n", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			e.tty = true
			e.setAnswers(tt.answers)
			code, _, errs := e.run("xbpretty", "install", "figlet")
			if code != tt.want {
				t.Errorf("exit = %d, want %d", code, tt.want)
			}
			ranReal := false
			for _, c := range e.commands() {
				if strings.HasPrefix(c, "-y") {
					ranReal = true
				}
			}
			if ranReal != tt.ranReal {
				t.Errorf("real run executed = %v, want %v (commands: %q)", ranReal, tt.ranReal, e.commands())
			}
			if tt.name == "anything else re-asks then accepts" {
				if n := strings.Count(errs, "[Y/n]"); n != 2 {
					t.Errorf("prompt shown %d times, want 2:\n%s", n, errs)
				}
			}
		})
	}
}

func TestAC006YesSkipsThePrompt(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	code, out, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted despite -y:\n%s", errs)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("summary missing:\n%s", out)
	}
	ranReal := false
	for _, c := range e.commands() {
		if strings.HasPrefix(c, "-y") {
			ranReal = true
		}
	}
	if !ranReal {
		t.Errorf("real run never executed: %q", e.commands())
	}
}

func TestAC007UnprivilegedDryRunOmitsSyncAndWarns(t *testing.T) {
	e := newEnv(t)
	e.root = false
	code, out, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("summary missing:\n%s", out)
	}
	if !strings.Contains(errs, "stale") {
		t.Errorf("missing the stale-index warning:\n%s", errs)
	}
	cmds := e.commands()
	if len(cmds) == 0 {
		t.Fatal("xbps was never called")
	}
	for _, c := range cmds {
		if strings.Contains(c, "-S") {
			t.Errorf("unprivileged plan refreshed the index: %q", c)
		}
	}
}

func TestAC008UpdateStreamsWithoutSummaryOrPrompt(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "")
	e.setAnswers("n\n")
	code, out, errs := e.run("xbpretty", "update")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("update printed a summary to stdout:\n%q", out)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("update prompted:\n%s", errs)
	}
	var sawSync bool
	for _, c := range e.commands() {
		if strings.HasPrefix(c, "-S") || strings.Contains(c, " -S") {
			sawSync = true
		}
		if strings.Contains(c, "-n") {
			t.Errorf("update ran a dry run: %q", c)
		}
	}
	if !sawSync {
		t.Errorf("update did not sync: %q", e.commands())
	}
}

func TestAC012TransactionLogIsWritten(t *testing.T) {
	e := newEnv(t)
	e.root = true
	code, _, _ := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	path, err := config.TransactionLog()
	if err != nil {
		t.Fatalf("log path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transaction log missing: %v", err)
	}
	line := strings.TrimSpace(string(raw))
	if line == "" {
		t.Fatal("transaction log is empty")
	}
	if !strings.Contains(line, "figlet-2.2.5_6") {
		t.Errorf("log line does not name the package with its version: %q", line)
	}
	if !strings.Contains(line, "install") {
		t.Errorf("log line does not name the command: %q", line)
	}
	// ISO-8601 timestamp at the front: 2026-10-15T09:41:01Z
	if len(line) < 20 || line[4] != '-' || line[10] != 'T' {
		t.Errorf("log line does not start with an ISO-8601 timestamp: %q", line)
	}
}

// TestAC012LogFollowsTheInvokersStateHome is AC-012's `alice` path: with no
// XDG_STATE_HOME the log belongs in the invoking user's own state home, which
// the sudo/doas lookup in internal/config resolves.
func TestAC012LogFollowsTheInvokersStateHome(t *testing.T) {
	e := newEnv(t)
	e.root = true
	home := t.TempDir()
	e.set("XDG_STATE_HOME", "")
	e.set("HOME", home)
	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	path := filepath.Join(home, ".local", "state", "xbpretty", "transactions.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transaction log missing from the invoker's state home: %v", err)
	}
	line := strings.TrimSpace(string(raw))
	if !strings.Contains(line, "figlet-2.2.5_6") || !strings.Contains(line, "install") {
		t.Errorf("log line = %q, want the command and the package with its version", line)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "state")); err == nil {
		t.Error("the log followed the state home it was given instead of the user's own")
	}
}

// TestAC013RebootHint pins FR-015a: a successful install or upgrade that
// changed a kernel package or glibc says so on stderr, and a transaction that
// touched neither stays quiet.
func TestAC013RebootHint(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		plan   string
		old    string
		reboot bool
	}{
		{
			name:   "a kernel update asks for a reboot",
			cmd:    "upgrade",
			plan:   "linux6.12-6.12.50_1 update x86_64 https://repo-default.voidlinux.org/current 210000000 32000000\n",
			old:    "linux6.12-6.12.49_1\n205000000\n",
			reboot: true,
		},
		{
			name:   "a glibc update asks for a reboot",
			cmd:    "upgrade",
			plan:   "glibc-2.42_2 update x86_64 https://repo-default.voidlinux.org/current 21000000 3200000\n",
			old:    "glibc-2.42_1\n20800000\n",
			reboot: true,
		},
		{
			name:   "an unrelated transaction stays quiet",
			cmd:    "install",
			plan:   planFixture,
			old:    "figlet-2.2.5_6\n657KB\n",
			reboot: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			e.set("XB_STUB_STDOUT", tt.plan)
			e.set("XB_STUB_OLD", tt.old)
			code, out, errs := e.run("xbpretty", tt.cmd, "-y")
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if got := strings.Contains(errs, "reboot"); got != tt.reboot {
				t.Errorf("reboot hint = %v, want %v\nstderr:\n%s", got, tt.reboot, errs)
			}
			if strings.Contains(out, "reboot") {
				t.Errorf("the reboot hint leaked to stdout:\n%s", out)
			}
		})
	}
}

func TestNoLogFlagSkipsTheLog(t *testing.T) {
	e := newEnv(t)
	e.root = true
	code, _, _ := e.run("xbpretty", "install", "figlet", "-y", "--no-log")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	path, err := config.TransactionLog()
	if err != nil {
		t.Fatalf("log path: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("transaction log was written despite --no-log")
	}
}

func TestParseArgsFlags(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want func(*testing.T, *options, []string)
	}{
		{
			name: "short cluster",
			argv: []string{"install", "-nv", "foo"},
			want: func(t *testing.T, o *options, rest []string) {
				if !o.dryRun || !o.verbose {
					t.Errorf("cluster flags not parsed: %+v", o)
				}
				if len(o.args) != 1 || o.args[0] != "foo" {
					t.Errorf("args = %v", o.args)
				}
			},
		},
		{
			name: "color equals form",
			argv: []string{"install", "--color=always"},
			want: func(t *testing.T, o *options, rest []string) {
				if o.color != "always" || !o.explicit["color"] {
					t.Errorf("color = %q explicit = %v", o.color, o.explicit)
				}
			},
		},
		{
			name: "color space form",
			argv: []string{"install", "--color", "never"},
			want: func(t *testing.T, o *options, rest []string) {
				if o.color != "never" {
					t.Errorf("color = %q", o.color)
				}
			},
		},
		{
			name: "passthrough",
			argv: []string{"install", "--", "-f", "foo"},
			want: func(t *testing.T, o *options, rest []string) {
				if len(o.passthru) != 2 || o.passthru[0] != "-f" {
					t.Errorf("passthru = %v", o.passthru)
				}
			},
		},
		{
			name: "flags before command",
			argv: []string{"-n", "install", "foo"},
			want: func(t *testing.T, o *options, rest []string) {
				if !o.dryRun || o.command != "install" {
					t.Errorf("dryRun = %v command = %q", o.dryRun, o.command)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseArgs(tt.argv, "")
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			tt.want(t, o, nil)
		})
	}
}

func TestParseArgsErrors(t *testing.T) {
	for _, argv := range [][]string{
		{"frobnicate"},
		{"install", "--bogus"},
		{"install", "-z"},
		{"install", "--color=blue"},
	} {
		if _, err := parseArgs(argv, ""); err == nil {
			t.Errorf("parseArgs(%v) succeeded, want error", argv)
		}
	}
}

// upgradePlan runs an upgrade dry run and returns the flags xbps-install was
// called with.
func upgradePlan(t *testing.T, e *env, argv ...string) string {
	t.Helper()
	code, _, errs := e.run(argv...)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	cmds := e.commands()
	if len(cmds) == 0 {
		t.Fatal("xbps-install was never called")
	}
	return cmds[0]
}

// transactionCalls returns the command log without the `xbps-query -H` hold
// lookup every summary makes.
func (e *env) transactionCalls() []string {
	e.t.Helper()
	var out []string
	for _, c := range e.commands() {
		if c == "-H" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// TestUpgradeHonoursTheSyncRules pins FR-004, FR-007 and FR-008 for upgrade:
// the same -S rule as install, written as the single token the acceptance
// criteria pin.
func TestUpgradeHonoursTheSyncRules(t *testing.T) {
	t.Run("root plan refreshes the index", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		if got := upgradePlan(t, e, "xbpretty", "upgrade", "-n"); got != "-Sun" {
			t.Errorf("plan = %q, want -Sun", got)
		}
	})
	t.Run("unprivileged plan drops it", func(t *testing.T) {
		e := newEnv(t)
		e.root = false
		if got := upgradePlan(t, e, "xbpretty", "upgrade", "-n"); got != "-un" {
			t.Errorf("plan = %q, want -un (AC-007)", got)
		}
	})
	t.Run("--no-sync drops it as root too", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		if got := upgradePlan(t, e, "xbpretty", "upgrade", "-n", "--no-sync"); got != "-un" {
			t.Errorf("plan = %q, want -un", got)
		}
	})
	t.Run("the real run does not sync a second time", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		e.setAnswers("\n")
		code, _, errs := e.run("xbpretty", "upgrade")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		cmds := e.transactionCalls()
		if len(cmds) != 2 {
			t.Fatalf("commands = %q, want a dry run and a real run", cmds)
		}
		if cmds[0] != "-Sun" {
			t.Errorf("plan = %q, want -Sun", cmds[0])
		}
		if cmds[1] != "-y -u" {
			t.Errorf("real run = %q, want -y -u (FR-008: no second -S)", cmds[1])
		}
	})
}

// TestAutoremoveNeverSyncs pins FR-005: xbps-remove has no -S, so autoremove
// must never reach for one.
func TestAutoremoveNeverSyncs(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", removePlanFixture)
	code, _, errs := e.run("xbpretty", "autoremove", "-n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-on" {
		t.Errorf("commands = %q, want one -on dry run", cmds)
	}
}

// TestPassthruKeepsTheArgv0Command pins FR-022 against the form FR-018's own
// hint recommends: with no positional argument left, the command named by
// argv[0] must survive.
func TestPassthruKeepsTheArgv0Command(t *testing.T) {
	o, err := parseArgs([]string{"--", "-F", "figlet"}, "remove")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.command != "remove" {
		t.Errorf("command = %q, want remove", o.command)
	}
	if len(o.passthru) != 2 || o.passthru[0] != "-F" || o.passthru[1] != "figlet" {
		t.Errorf("passthru = %v, want [-F figlet]", o.passthru)
	}

	// Without an argv[0] command the same form still means install.
	o, err = parseArgs([]string{"--", "figlet"}, "")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.command != "install" || len(o.args) != 1 || o.args[0] != "figlet" {
		t.Errorf("command = %q args = %v, want install [figlet]", o.command, o.args)
	}

	// Through the link name the pass-through reaches xbps-remove unchanged.
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", removePlanFixture)
	code, _, errs := e.run("xbp-remove", "-n", "--", "-F", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-n -F figlet" {
		t.Errorf("commands = %q, want one -n -F figlet dry run", cmds)
	}
}

// TestRevdepsLookupUsesThePinnedForm pins AC-017: the reverse-dependency
// lookup is `xbps-query -X <pkg>`, not repository mode.
func TestRevdepsLookupUsesThePinnedForm(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", removePlanFixture)
	e.set("XB_STUB_REVDEPS", "bar-2.0_1\n")
	e.set("XB_STUB_RUN_STDERR", "ERROR: failed to remove wget\n")
	e.set("XB_STUB_EXIT", "1")
	code, _, errs := e.run("xbpretty", "remove", "wget", "-y")
	if code == 0 {
		t.Fatalf("exit = 0, want the failing xbps code")
	}
	if !strings.Contains(errs, "bar-2.0_1") {
		t.Errorf("dependents not listed:\n%s", errs)
	}
	for _, c := range e.commands() {
		if !strings.Contains(c, "-X") {
			continue
		}
		if c != "-X wget" {
			t.Errorf("reverse-dependency lookup = %q, want -X wget", c)
		}
	}
}

// TestStaleIndexOnASuccessfulPlanIsExplained covers FR-017 as ADR-002
// measured it: a truncated index leaves `xbps-install -Sun` exiting 0 with an
// empty plan and the error still on stderr, so the plan step has to read
// stderr on a successful exit too. Without this the user sees a bare
// `Nothing to do.` over a broken index.
func TestStaleIndexOnASuccessfulPlanIsExplained(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_STDERR", "ERROR: failed to open repodata: /var/db/xbps/x86_64-repodata: "+
		"Invalid or incomplete multibyte or wide character\n")
	code, out, errs := e.run("xbpretty", "upgrade", "-n")
	if code != 0 {
		t.Errorf("exit = %d, want 0 (xbps's own code)", code)
	}
	if !strings.Contains(out, "Nothing to do.") {
		t.Errorf("stdout = %q, want Nothing to do.", out)
	}
	if !strings.Contains(errs, "failed to open repodata") {
		t.Errorf("the raw xbps stderr was dropped:\n%s", errs)
	}
	if !strings.Contains(errs, "hint:") || !strings.Contains(errs, "xbp update") {
		t.Errorf("the stale-index hint is missing:\n%s", errs)
	}
}

// TestSuccessfulPlanWithUnmatchedStderrStaysQuiet is the other half of the
// same rule: a successful dry run whose stderr matches nothing gets no
// guessed hint, and nothing is echoed back at the user (FR-017, AC-016).
func TestSuccessfulPlanWithUnmatchedStderrStaysQuiet(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDERR", "WARNING: something harmless happened\n")
	code, out, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("summary missing:\n%s", out)
	}
	if errs != "" {
		t.Errorf("stderr is not empty:\n%s", errs)
	}
}

// TestColorAutoChecksTheTerminalAndNoColor covers FR-020: `--color=auto` is
// not `always`. An explicit auto falls through to the TTY and NO_COLOR
// checks exactly like the config key's own default, while always and never
// keep overriding both.
func TestColorAutoChecksTheTerminalAndNoColor(t *testing.T) {
	orig := isTerminalFn
	t.Cleanup(func() { isTerminalFn = orig })

	tests := []struct {
		name    string
		config  string
		argv    []string
		tty     bool
		noColor string
		want    bool
	}{
		{"auto on a terminal", "auto", []string{"--color=auto"}, true, "", true},
		{"auto off a terminal", "auto", []string{"--color=auto"}, false, "", false},
		{"auto honours NO_COLOR", "auto", []string{"--color=auto"}, true, "1", false},
		{"always overrides the terminal", "auto", []string{"--color=always"}, false, "1", true},
		{"never overrides the terminal", "auto", []string{"--color=never"}, true, "", false},
		{"the config key still decides", "always", nil, false, "1", true},
		{"config never wins over the default", "never", nil, true, "", false},
		{"config auto off a terminal", "auto", nil, false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isTerminalFn = func(*os.File) bool { return tt.tty }
			t.Setenv("NO_COLOR", tt.noColor)
			argv := append([]string{"install", "figlet"}, tt.argv...)
			opts, err := parseArgs(argv, "")
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if got := wantColor(tt.config, opts); got != tt.want {
				t.Errorf("wantColor(%q, %v) = %v, want %v", tt.config, tt.argv, got, tt.want)
			}
		})
	}
}

// TestUpdateIsLogged covers FR-013 for the one real execution that produces
// no package rows: an index sync is still a transaction, so its line names
// the command with no list after it.
func TestUpdateIsLogged(t *testing.T) {
	e := newEnv(t)
	e.root = true
	code, _, errs := e.run("xbpretty", "update")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	path, err := config.TransactionLog()
	if err != nil {
		t.Fatalf("log path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transaction log missing: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("log has %d lines, want 1:\n%s", len(lines), raw)
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 {
		t.Errorf("log line = %q, want a timestamp and the command alone", lines[0])
	}
	if _, err := time.Parse(time.RFC3339, fields[0]); err != nil {
		t.Errorf("log line does not start with an ISO-8601 timestamp: %q", lines[0])
	}
	if len(fields) > 1 && fields[1] != "update" {
		t.Errorf("log line does not name the command: %q", lines[0])
	}
	if strings.HasSuffix(lines[0], " ") {
		t.Errorf("log line has a trailing space: %q", lines[0])
	}
}

// TestSignalExitCodeMatchesTheShell pins the mapping FR-014 and AC-014 need:
// a child killed by a signal never had an exit code of its own, so the code a
// shell reports is passed through instead of Go's -1.
func TestSignalExitCodeMatchesTheShell(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   int
	}{
		{"SIGINT", "kill -INT $$", 130},
		{"SIGTERM", "kill -TERM $$", 143},
		{"a real exit code passes through", "exit 17", 17},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := exitCode(exec.Command("/bin/sh", "-c", tt.script).Run())
			if !ok {
				t.Fatalf("%q reported no exit code", tt.script)
			}
			if code != tt.want {
				t.Errorf("exitCode = %d, want %d", code, tt.want)
			}
		})
	}
}

// writeSlowChild writes an xbps-install stand-in that records the call and
// its pid, then sleeps until it is signalled. The shared stub exits at once,
// which leaves no window for a signal to land in.
func writeSlowChild(t *testing.T, dir, logPath, pidPath string) string {
	t.Helper()
	path := filepath.Join(dir, "slow-xbps-install")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + logPath + "\n" +
		"printf '%s' \"$$\" > " + pidPath + "\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// pointInstallAt rewrites the config so xbps_install_path names another
// binary, keeping the other two on the shared stubs.
func (e *env) pointInstallAt(path string) {
	e.t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		e.t.Fatal(err)
	}
	cfg := strings.Join([]string{
		"xbps_install_path = \"" + path + "\"",
		"xbps_remove_path = \"" + filepath.Join(stubDir, "xbps-remove") + "\"",
		"xbps_query_path = \"" + filepath.Join(stubDir, "xbps-query") + "\"",
		"color = \"never\"",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(e.dir, "config", "xbpretty", "config.toml"), []byte(cfg), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// waitForPID blocks until the streamed child has written its pid.
func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatalf("child pid %q: %v", raw, err)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("the streamed child never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAC014SignalledChildDecidesTheExitCode drives a real streamed child,
// interrupts xbpretty, and checks the whole promise AC-014 makes: the child
// receives the signal and xbpretty exits with the code a shell would report.
func TestAC014SignalledChildDecidesTheExitCode(t *testing.T) {
	e := newEnv(t)
	e.root = true
	pidPath := filepath.Join(e.dir, "child.pid")
	e.pointInstallAt(writeSlowChild(t, e.dir, e.logPath, pidPath))

	// Keep the test binary alive whatever the timing: stream arms its own
	// handler only once the child has started.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	defer signal.Stop(sigs)

	done := make(chan int, 1)
	go func() {
		code, _, _ := e.run("xbpretty", "update")
		done <- code
	}()

	childPID := waitForPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })

	// Repeat the signal: a signal delivered before stream's handler is
	// registered is not queued for it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case code := <-done:
			if code != 130 {
				t.Fatalf("exit = %d, want 130 (128 + SIGINT)", code)
			}
			if path, err := config.TransactionLog(); err == nil {
				if _, err := os.Stat(path); err == nil {
					t.Error("an interrupted run wrote to the transaction log")
				}
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("xbpretty did not exit after SIGINT")
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatalf("signalling the test process: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// linkStamp is what a symlink looks like from the outside: its inode and the
// time it was last written. A recreated link shows up as a change even when
// it points at the same place and the filesystem reuses the freed inode.
type linkStamp struct {
	inode   uint64
	modTime time.Time
}

func stamp(t *testing.T, path string) linkStamp {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no inode for %s", path)
	}
	return linkStamp{inode: st.Ino, modTime: info.ModTime()}
}

// TestAC026LinkCreatesSixLinksAndLeavesThemAlone pins FR-025: the six link
// names, a rerun that changes nothing and reports nothing, and a conflicting
// file that is reported rather than overwritten.
func TestAC026LinkCreatesSixLinksAndLeavesThemAlone(t *testing.T) {
	target, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var logged strings.Builder
	st := &state{stdout: io.Discard, stderr: &logged}
	opts := &options{args: []string{dir}}

	names := []string{"xbp", "xbp-install", "xbp-remove", "xbp-upgrade", "xbp-autoremove", "xbp-update"}
	if code := st.runLink(opts); code != 0 {
		t.Fatalf("link exit = %d, want 0\n%s", code, logged.String())
	}
	created := make(map[string]linkStamp, len(names))
	for _, name := range names {
		link := filepath.Join(dir, name)
		got, err := os.Readlink(link)
		if err != nil || got != target {
			t.Fatalf("%s -> %q, %v; want %q", name, got, err, target)
		}
		created[name] = stamp(t, link)
	}

	// A rerun changes nothing and errors nowhere.
	logged.Reset()
	if code := st.runLink(opts); code != 0 {
		t.Errorf("rerun exit = %d, want 0\n%s", code, logged.String())
	}
	if logged.Len() != 0 {
		t.Errorf("rerun wrote to stderr:\n%s", logged.String())
	}
	for _, name := range names {
		link := filepath.Join(dir, name)
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("rerun changed %s: %q, %v", name, got, err)
		}
		if stamp(t, link) != created[name] {
			t.Errorf("rerun replaced the %s symlink", name)
		}
	}

	// A conflicting file is reported, not overwritten.
	conflict := filepath.Join(dir, "xbp-remove")
	if err := os.Remove(conflict); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logged.Reset()
	if code := st.runLink(opts); code == 0 {
		t.Errorf("conflict exit = 0, want non-zero\n%s", logged.String())
	}
	if !strings.Contains(logged.String(), conflict) {
		t.Errorf("the conflict was not reported:\n%s", logged.String())
	}
	if raw, err := os.ReadFile(conflict); err != nil || string(raw) != "mine\n" {
		t.Errorf("the conflicting file was overwritten: %q, %v", raw, err)
	}
}

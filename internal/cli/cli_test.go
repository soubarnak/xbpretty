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
	stubDir string
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
	e := &env{t: t, dir: dir, stubDir: stubDir, logPath: filepath.Join(dir, "commands.log")}
	e.writeConfig("")
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

	// The predownload pass must never reach the machine's real cache or the
	// network from a unit test. A cache path that cannot be a directory makes
	// the pass skip itself; predownload tests point cacheDirFn at a temp dir.
	noCache := filepath.Join(dir, "no-cache")
	if err := os.WriteFile(noCache, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	origCacheDir := cacheDirFn
	cacheDirFn = func() string { return noCache }
	t.Cleanup(func() { cacheDirFn = origCacheDir })

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

// isDryRun reports whether a stub invocation was a plan. It is the same rule
// the stubs use: a call is a plan when its first argument is a flag cluster
// (starts with `-`, not `--`) containing `n`. Nothing after the first
// argument counts.
func isDryRun(cmd string) bool {
	first, _, _ := strings.Cut(cmd, " ")
	return strings.HasPrefix(first, "-") && !strings.HasPrefix(first, "--") && strings.Contains(first, "n")
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

// commands returns the argv of every stub xbps invocation, without the
// binary name the stubs now record.
func (e *env) commands() []string {
	e.t.Helper()
	var out []string
	for _, line := range e.commandsWithBinary() {
		if _, args := splitStubLine(line); args != "" {
			out = append(out, args)
		}
	}
	return out
}

// commandsWithBinary returns each stub invocation as "<binary> <argv>" so a
// test can prove which xbps program actually ran.
func (e *env) commandsWithBinary() []string {
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

// splitStubLine splits one stub log line into its binary name and its argv.
func splitStubLine(line string) (binary, args string) {
	binary, args, _ = strings.Cut(line, " ")
	return binary, args
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
	code, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
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
	code, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
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
	code, _, errs := e.run("xbpretty", "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	cmds := e.transactionCalls()
	if len(cmds) != 1 || cmds[0] != "-n -u" {
		t.Errorf("commands = %q, want one -n -u dry run", cmds)
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

// TestAC009AutoremoveOrphansLandInAutoRemoveGroup drives the captured
// `xbps-remove -on` plan (testdata/parser/0607/autoremove.txt): the orphans are
// nala's AutoRemove group, each row carrying the bytes its removal frees.
func TestAC009AutoremoveOrphansLandInAutoRemoveGroup(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/autoremove.txt"))
	code, out, errs := e.run("xbpretty", "autoremove", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-n -o" {
		t.Errorf("commands = %q, want one -n -o dry run", cmds)
	}
	if !strings.Contains(out, "\n AutoRemove\n") {
		t.Errorf("no AutoRemove group:\n%s", out)
	}
	for _, row := range []struct{ name, version, size string }{
		{"cups", "2.4.19_1", "13 MiB"},
		{"font-ibm-plex-ttf", "6.4.0_2", "58 MiB"},
		{"lsof", "4.99.7_1", "333 KiB"},
		{"steam-udev-rules", "1.0.0.61+20260625_1", "17 KiB"},
		{"zenity", "4.0.5_1", "11 MiB"},
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
	if !strings.Contains(out, "AutoRemove 5") {
		t.Errorf("footer is missing the AutoRemove count:\n%s", out)
	}
	if !strings.Contains(out, "Disk space to free: 82 MiB") {
		t.Errorf("footer is missing the freed disk total:\n%s", out)
	}
}

func TestAC015FailureGetsHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_STDERR", "ERROR: Package 'figlet' not found in repository pool.\n")
	e.set("XB_STUB_PLAN_EXIT", "2")
	code, _, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
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

// hintCapture reads the stdout half of a captured hint case.
func hintCapture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/hints", name+".out"))
	if err != nil {
		t.Fatalf("reading capture %s: %v", name, err)
	}
	return string(raw)
}

// TestFailureWrittenToStdoutGetsHint is ADR-0006: xbps-remove says a package
// is not installed on stdout with nothing on stderr, and the plan step has to
// find the hint there while the raw text is still printed once, unchanged and
// the exit code stays xbps's own. The second case exercises the mechanism on
// the outdated-xbps text, which xbps writes to stdout for an upgrade; the
// measured 0.59.1 flow differs, since its dry run succeeds and only the real
// run is refused (ADR-0006, "the real run reads stderr").
func TestFailureWrittenToStdoutGetsHint(t *testing.T) {
	tests := []struct {
		name    string
		capture string
		argv    []string
		exit    string
		want    string
	}{
		{"not installed", "0607/not-installed", []string{"remove", "this-package-does-not-exist-xyz", "--dry-run"}, "2", "nothing to remove"},
		{"outdated xbps text on stdout", "0591/xbps-unsigned-update-upgrade", []string{"upgrade", "--dry-run"}, "16", "xbps-install -u xbps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			raw := hintCapture(t, tt.capture)
			e.set("XB_STUB_STDOUT", raw)
			e.set("XB_STUB_STDERR", "")
			e.set("XB_STUB_PLAN_EXIT", tt.exit)
			code, out, errs := e.run(append([]string{"xbpretty"}, tt.argv...)...)
			if strconv.Itoa(code) != tt.exit {
				t.Errorf("exit = %d, want %s (xbps's code)", code, tt.exit)
			}
			if out != raw {
				t.Errorf("stdout = %q, want xbps's own %q", out, raw)
			}
			if !strings.Contains(errs, "hint:") || !strings.Contains(errs, tt.want) {
				t.Errorf("stderr is missing the hint naming %q:\n%s", tt.want, errs)
			}
		})
	}
}

// TestInstallRefusedOnStderrGetsHint is ADR-0006's other half: an install is
// refused with the outdated-xbps text on stderr and nothing on stdout, and the
// plan step still hands back xbps's own line plus the hint.
func TestInstallRefusedOnStderrGetsHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	raw, err := os.ReadFile("../../testdata/hints/0591/xbps-unsigned-update.txt")
	if err != nil {
		t.Fatalf("reading capture: %v", err)
	}
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_STDERR", string(raw))
	e.set("XB_STUB_PLAN_EXIT", "16")
	code, _, errs := e.run("xbpretty", "install", "--dry-run", "htop", "curl", "wget")
	if code != 16 {
		t.Errorf("exit = %d, want 16 (xbps's code)", code)
	}
	if !strings.Contains(errs, string(raw)) {
		t.Errorf("stderr is missing xbps's own refusal %q:\n%s", raw, errs)
	}
	if !strings.Contains(errs, "hint:") || !strings.Contains(errs, "xbps-install -u xbps") {
		t.Errorf("stderr is missing the hint naming %q:\n%s", "xbps-install -u xbps", errs)
	}
}

// TestStdoutMessageOnTheWrongCodeGetsNoHint keeps AC-016 over the new stream:
// the not-installed text on stdout with another exit code is not that failure.
func TestStdoutMessageOnTheWrongCodeGetsNoHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", hintCapture(t, "0607/not-installed"))
	e.set("XB_STUB_PLAN_EXIT", "1")
	code, _, errs := e.run("xbpretty", "remove", "this-package-does-not-exist-xyz", "--dry-run")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if strings.Contains(errs, "hint:") {
		t.Errorf("guessed a hint:\n%s", errs)
	}
}

func TestAC016UnmatchedFailureGetsNoHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_STDERR", "ERROR: something unprecedented happened.\n")
	e.set("XB_STUB_PLAN_EXIT", "2")
	code, _, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
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
	code, _, errs := e.run("xbpretty", "remove", "wget", "--dry-run")
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

// writeConfig rewrites the default layer in the rev 6 schema. An empty
// installPath keeps the shared stub; extra lines are appended after [Color].
func (e *env) writeConfig(installPath string, extra ...string) {
	e.t.Helper()
	if installPath == "" {
		installPath = filepath.Join(e.stubDir, "xbps-install")
	}
	lines := []string{
		"[Xbps]",
		`install_path = "` + installPath + `"`,
		`remove_path = "` + filepath.Join(e.stubDir, "xbps-remove") + `"`,
		`query_path = "` + filepath.Join(e.stubDir, "xbps-query") + `"`,
		"",
		"[Color]",
		`mode = "Never"`,
	}
	lines = append(lines, extra...)
	cfg := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(e.dir, "config", "xbpretty", "config.toml"), []byte(cfg), 0o644); err != nil {
		e.t.Fatal(err)
	}
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
	if err := os.WriteFile(cfg, append(data, []byte("\n[Nala]\nassume_yes = true\n")...), 0o644); err != nil {
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
	if err := os.WriteFile(cfg, []byte("[Color]\nmode = \"blue\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := e.run("xbpretty", "install", "figlet")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errs, "xbpretty: config ") || !strings.Contains(errs, ":2:") {
		t.Errorf("missing the config path and line number:\n%s", errs)
	}
	if !strings.Contains(errs, "mode") {
		t.Errorf("the error does not name the key:\n%s", errs)
	}
}

func TestAC021StdoutHoldsOnlyTheSummary(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.set("XB_STUB_STDOUT", planFixture)
	_, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
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
	code, out, errs := e.run("xbpretty", "remove", "wget", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(out, "805.3 KiB") {
		t.Errorf("the removal is shown by its download size:\n%s", out)
	}
	if line := rowLine(out, "wget"); !strings.Contains(line, "1.25.0_1") || !strings.Contains(line, "4 MiB") {
		t.Errorf("the removal's freed bytes are missing:\n%s", out)
	}
	if strings.Contains(out, "download") {
		t.Errorf("the footer reports a download for a removal:\n%s", out)
	}
	if !strings.Contains(out, "Disk space to free: 4 MiB") {
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
	if !strings.Contains(errs, "-n figlet -f figlet") {
		t.Errorf("passthrough missing from the dry run:\n%s", errs)
	}
	if !strings.Contains(errs, "-y figlet -f figlet") {
		t.Errorf("passthrough missing from the real run:\n%s", errs)
	}
}

func TestAC024VerbosePrintsEachCommand(t *testing.T) {
	e := newEnv(t)
	e.root = true
	_, _, errs := e.run("xbpretty", "install", "figlet", "--dry-run", "-v")
	if !strings.Contains(errs, "verbose:") || !strings.Contains(errs, " -n figlet") {
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

func TestAC002InstallNeverSyncsInsideTheTransaction(t *testing.T) {
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
	// FR-007: install does not sync by default.
	if plan != "-n figlet" {
		t.Errorf("dry run = %q, want -n figlet", plan)
	}
	if real != "-y figlet" {
		t.Errorf("real run = %q, want -y figlet", real)
	}
	for _, c := range cmds {
		if c == "-S" {
			t.Errorf("install ran a sync step it never asked for: %q", c)
		}
	}
}

func TestAC003PromptAnswers(t *testing.T) {
	tests := []struct {
		name    string
		answers string
		want    int
		ranReal bool
		wantErr string
	}{
		{"enter proceeds", "\n", 0, true, ""},
		{"y proceeds", "y\n", 0, true, ""},
		{"Y proceeds", "Y\n", 0, true, ""},
		{"yes proceeds", "yes\n", 0, true, ""},
		{"n refuses", "n\n", 1, false, "xbpretty: user refused confirmation"},
		{"N refuses", "N\n", 1, false, "xbpretty: user refused confirmation"},
		{"no refuses", "no\n", 1, false, "xbpretty: user refused confirmation"},
		{"EOF refuses", "", 1, false, "xbpretty: user refused confirmation"},
		{"anything else is invalid", "x\n", 1, false, "xbpretty: 'x' is not a valid response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			e.tty = true
			e.setAnswers(tt.answers)
			code, _, errs := e.run("xbpretty", "install", "figlet")
			if code != tt.want {
				t.Errorf("exit = %d, want %d\n%s", code, tt.want, errs)
			}
			if !strings.Contains(errs, "Do you want to continue? [Y/n] ") {
				t.Errorf("the exact prompt bytes are missing:\n%q", errs)
			}
			if tt.wantErr != "" && !strings.Contains(errs, tt.wantErr) {
				t.Errorf("stderr is missing %q:\n%s", tt.wantErr, errs)
			}
			if tt.wantErr != "" && strings.Count(errs, "Do you want to continue? [Y/n] ") != 1 {
				t.Errorf("the prompt was shown more than once:\n%s", errs)
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

func TestAC007UnprivilegedUpgradeDryRunOmitsSyncAndWarns(t *testing.T) {
	e := newEnv(t)
	e.root = false
	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run")
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
		if c == "-S" || strings.Contains(c, "-S") {
			t.Errorf("unprivileged plan refreshed the index: %q", c)
		}
	}
}

func TestAC009UpdateSyncsThenCountsWithoutSummaryOrPrompt(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/upgrade.txt"))
	e.setAnswers("n\n")
	code, out, errs := e.run("xbpretty", "update")
	if code != 0 {
		t.Errorf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "package(s) can be upgraded. Run 'xbp list --upgradable' to see them.") {
		t.Errorf("update printed no count line:\n%q", out)
	}
	if strings.Contains(out, "Upgrade\n") || strings.Contains(out, "Summary") {
		t.Errorf("update printed a plan summary:\n%q", out)
	}
	if strings.Contains(errs, "Do you want to continue?") {
		t.Errorf("update prompted:\n%s", errs)
	}
	cmds := e.transactionCalls()
	if len(cmds) != 2 || cmds[0] != "-S" || cmds[1] != "-n -u" {
		t.Errorf("commands = %q, want -S then -n -u", cmds)
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
	if strings.HasSuffix(string(raw), " \n") {
		t.Errorf("log line has a trailing space: %q", raw)
	}
}

// TestTransactionLogRecordsChangedPackagesOnly pins the log line contract
// (ADR-0008): an RFC 3339 UTC timestamp, the command, then one name-version
// entry per changed package, single spaces and no trailing space. A held
// package is not touched by the transaction, so it is not written.
func TestTransactionLogRecordsChangedPackagesOnly(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "figlet-2.2.5_6 install x86_64 https://repo-default.voidlinux.org/current 657000 241000\n"+
		"linux6.12-6.12.50_1 hold x86_64 https://repo-default.voidlinux.org/current 210000000 32000000\n")
	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
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
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("log has %d lines, want 1:\n%s", len(lines), raw)
	}
	fields := strings.Split(lines[0], " ")
	want := []string{"install", "figlet-2.2.5_6"}
	if len(fields) != 1+len(want) {
		t.Fatalf("log line = %q, want a timestamp, the command and the changed package only", lines[0])
	}
	ts, err := time.Parse(time.RFC3339, fields[0])
	if err != nil || !strings.HasSuffix(fields[0], "Z") || ts.Location() != time.UTC {
		t.Errorf("log line does not start with an RFC 3339 UTC timestamp: %q", lines[0])
	}
	for i, w := range want {
		if fields[1+i] != w {
			t.Errorf("field %d = %q, want %q (line %q)", 1+i, fields[1+i], w, lines[0])
		}
	}
	if strings.Contains(lines[0], "linux6.12") {
		t.Errorf("log line names a held package: %q", lines[0])
	}
	if strings.HasSuffix(lines[0], " ") {
		t.Errorf("log line has a trailing space: %q", lines[0])
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
			var code int
			var out, errs string
			if tt.cmd == "install" {
				code, out, errs = e.run("xbpretty", tt.cmd, "figlet", "-y")
			} else {
				code, out, errs = e.run("xbpretty", tt.cmd, "-y")
			}
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
				if !o.assumeNo || !o.verbose || o.dryRun {
					t.Errorf("cluster flags not parsed as -n/--assume-no + -v: %+v", o)
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
			argv: []string{"--dry-run", "install", "foo"},
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

// transactionCalls returns the command log without the xbps-query lookups
// (hold state, old versions and installed guards) every transaction makes.
func (e *env) transactionCalls() []string {
	e.t.Helper()
	var out []string
	for _, c := range e.commands() {
		switch {
		case c == "-H", strings.HasPrefix(c, "-p "), strings.HasPrefix(c, "-X "):
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
	t.Run("root plan has a separate sync step and a -n -u transaction", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		code, _, errs := e.run("xbpretty", "upgrade", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, errs)
		}
		if cmds := e.transactionCalls(); len(cmds) != 2 || cmds[0] != "-S" || cmds[1] != "-n -u" {
			t.Errorf("commands = %q, want -S then -n -u", cmds)
		}
	})
	t.Run("unprivileged plan drops the sync", func(t *testing.T) {
		e := newEnv(t)
		e.root = false
		code, _, errs := e.run("xbpretty", "upgrade", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, errs)
		}
		if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-n -u" {
			t.Errorf("commands = %q, want one -n -u dry run", cmds)
		}
	})
	t.Run("--no-sync drops it as root too", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		code, _, errs := e.run("xbpretty", "upgrade", "--dry-run", "--no-sync")
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, errs)
		}
		if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-n -u" {
			t.Errorf("commands = %q, want one -n -u dry run", cmds)
		}
	})
	t.Run("the real run has no -S in its argv", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		e.setAnswers("\n")
		code, _, errs := e.run("xbpretty", "upgrade")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		cmds := e.transactionCalls()
		if len(cmds) != 3 {
			t.Fatalf("commands = %q, want a sync, a dry run and a real run", cmds)
		}
		if cmds[0] != "-S" || cmds[1] != "-n -u" || cmds[2] != "-y -u" {
			t.Errorf("commands = %q, want -S, -n -u, -y -u", cmds)
		}
	})
}

// TestAutoremoveNeverSyncs pins FR-005: xbps-remove has no -S, so autoremove
// must never reach for one.
func TestAutoremoveNeverSyncs(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", removePlanFixture)
	code, _, errs := e.run("xbpretty", "autoremove", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if cmds := e.transactionCalls(); len(cmds) != 1 || cmds[0] != "-n -o" {
		t.Errorf("commands = %q, want one -n -o dry run", cmds)
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
	code, _, errs := e.run("xbp-remove", "--dry-run", "--", "-F", "figlet")
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
	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run")
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

// TestSuccessfulPlanForwardsUnmatchedStderr is §3.2.3: a successful dry run
// still forwards xbps's stderr to stderr, while an unmatched failure gets no
// guessed hint (FR-017, AC-016).
func TestSuccessfulPlanForwardsUnmatchedStderr(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDERR", "WARNING: something harmless happened\n")
	code, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("summary missing:\n%s", out)
	}
	if !strings.Contains(errs, "WARNING: something harmless happened") {
		t.Errorf("the successful plan's stderr was not forwarded:\n%s", errs)
	}
	if strings.Contains(errs, "hint:") {
		t.Errorf("guessed a hint for unmatched stderr:\n%s", errs)
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
		config  config.ColorMode
		argv    []string
		tty     bool
		noColor string
		want    bool
	}{
		{"auto on a terminal", config.ColorAuto, []string{"--color=auto"}, true, "", true},
		{"auto off a terminal", config.ColorAuto, []string{"--color=auto"}, false, "", false},
		{"auto honours NO_COLOR", config.ColorAuto, []string{"--color=auto"}, true, "1", false},
		{"always overrides the terminal", config.ColorAuto, []string{"--color=always"}, false, "1", true},
		{"never overrides the terminal", config.ColorAuto, []string{"--color=never"}, true, "", false},
		{"the config key still decides", config.ColorAlways, nil, false, "1", true},
		{"config never wins over the default", config.ColorNever, nil, true, "", false},
		{"config auto off a terminal", config.ColorAuto, nil, false, "", false},
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
	fields := strings.Split(lines[0], " ")
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

// pointInstallAt rewrites the config so [Xbps] install_path names another
// binary, keeping the other two on the shared stubs.
func (e *env) pointInstallAt(path string) {
	e.t.Helper()
	e.writeConfig(path)
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
// FR-074 promises both SIGINT and SIGTERM forwarding.
func TestAC014SignalledChildDecidesTheExitCode(t *testing.T) {
	for _, tt := range []struct {
		name string
		sig  syscall.Signal
		want int
	}{
		{"SIGINT", syscall.SIGINT, 130},
		{"SIGTERM", syscall.SIGTERM, 143},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			pidPath := filepath.Join(e.dir, "child.pid")
			e.pointInstallAt(writeSlowChild(t, e.dir, e.logPath, pidPath))

			// Keep the test binary alive whatever the timing: stream arms its
			// own handler only once the child has started.
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, tt.sig)
			defer signal.Stop(sigs)

			done := make(chan int, 1)
			go func() {
				code, _, _ := e.run("xbpretty", "update")
				done <- code
			}()

			childPID := waitForPID(t, pidPath)
			t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })

			// Repeat the signal: a signal delivered before stream's handler
			// is registered is not queued for it.
			deadline := time.Now().Add(10 * time.Second)
			for {
				select {
				case code := <-done:
					if code != tt.want {
						t.Fatalf("exit = %d, want %d", code, tt.want)
					}
					if path, err := config.TransactionLogPath(); err == nil {
						if _, err := os.Stat(path); err == nil {
							t.Error("an interrupted run wrote to the transaction log")
						}
					}
					return
				default:
				}
				if time.Now().After(deadline) {
					t.Fatalf("xbpretty did not exit after %s", tt.name)
				}
				if err := syscall.Kill(os.Getpid(), tt.sig); err != nil {
					t.Fatalf("signalling the test process: %v", err)
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
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

// /dev/null is a character device but not a terminal; the piped-stdin rule
// (FR-010) depends on telling the two apart.
func TestIsTerminalRejectsNonTerminals(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	file, err := os.Open("cli_test.go")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for name, f := range map[string]*os.File{"/dev/null": null, "pipe": r, "regular file": file} {
		if isTerminal(f) {
			t.Errorf("isTerminal(%s) = true, want false", name)
		}
	}
}

const nanoPlanFixture = "nano-8.4_1 install x86_64 https://repo-default.voidlinux.org/current 300000 900000\n"

func TestStubRealInstallOfNanoIsNotAPlan(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", nanoPlanFixture)
	e.set("XB_STUB_PLAN_EXIT", "0")
	e.set("XB_STUB_RUN_STDERR", "ERROR: real install of nano failed\n")
	e.set("XB_STUB_EXIT", "3")
	code, _, errs := e.run("xbpretty", "install", "nano", "-y")
	if code != 3 {
		t.Errorf("exit = %d, want 3 (the real run's code)", code)
	}
	if !strings.Contains(errs, "ERROR: real install of nano failed") {
		t.Errorf("real-run stderr missing, plan streams used instead:\n%s", errs)
	}
	var sawReal bool
	for _, c := range e.commands() {
		if !isDryRun(c) && strings.HasPrefix(c, "-y") && strings.Contains(c, "nano") {
			sawReal = true
		}
	}
	if !sawReal {
		t.Errorf("no real run of nano recorded: %q", e.commands())
	}
}

func TestStubRealRemoveOfPackageWithNIsNotAPlan(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "nano-8.4_1 remove x86_64 https://repo-default.voidlinux.org/current 300000 900000\n")
	e.set("XB_STUB_RUN_STDERR", "ERROR: real remove of nano failed\n")
	e.set("XB_STUB_EXIT", "4")
	code, _, errs := e.run("xbpretty", "remove", "nano", "-y")
	if code != 4 {
		t.Errorf("exit = %d, want 4 (the real run's code)", code)
	}
	if !strings.Contains(errs, "ERROR: real remove of nano failed") {
		t.Errorf("real-run stderr missing, plan streams used instead:\n%s", errs)
	}
}

// TestStubClassifiesEveryArgvShape runs both stubs directly with every argv
// shape xbpretty sends and checks the stub and the helper agree.
func TestStubClassifiesEveryArgvShape(t *testing.T) {
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	shapes := []struct {
		args []string
		plan bool
	}{
		{[]string{"-Sn", "nano"}, true},
		{[]string{"-n", "nano"}, true},
		{[]string{"-Sun"}, true},
		{[]string{"-un"}, true},
		{[]string{"-on"}, true},
		{[]string{"-Sn", "nano", "--", "-f"}, true},
		{[]string{"-y", "nano"}, false},
		{[]string{"-y", "-u"}, false},
		{[]string{"-y", "-o"}, false},
		{[]string{"-S"}, false},
		{[]string{}, false},
		{[]string{"-y", "nano", "-n"}, false},
		{[]string{"-y", "nano", "--", "-n"}, false},
		{[]string{"-S", "nano", "-n"}, false},
	}
	for _, name := range []string{"xbps-install", "xbps-remove"} {
		for _, sh := range shapes {
			cmd := exec.Command(filepath.Join(stubDir, name), sh.args...)
			cmd.Env = append(os.Environ(),
				"XB_STUB_LOG=/dev/null",
				"XB_STUB_STDOUT=PLANOUT", "XB_STUB_STDERR=",
				"XB_STUB_RUN_STDOUT=REALOUT",
			)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s %v: %v", name, sh.args, err)
			}
			if gotPlan := string(out) == "PLANOUT"; gotPlan != sh.plan {
				t.Errorf("%s %v: stub plan = %v, want %v", name, sh.args, gotPlan, sh.plan)
			}
			if got := isDryRun(strings.Join(sh.args, " ")); got != sh.plan {
				t.Errorf("isDryRun(%v) = %v, want %v", sh.args, got, sh.plan)
			}
		}
	}
}

func TestNalaInlineSummaryInteractive(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.set("XB_STUB_STDOUT", "xbps-0.60.8_1 update x86_64 https://repo-default.voidlinux.org/current 533481 121137\n")
	e.set("XB_STUB_OLD", lookupRead(t, "0607/xbps.txt"))
	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run", "--color=auto")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}

	plain := ansiEscape.ReplaceAllString(out, "")
	for _, want := range []string{
		"================================================================================",
		" Upgrade",
		" Package:",
		"Old Version:",
		"New Version:",
		" Summary",
		" Upgrade 1",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("interactive output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "\x1b[1;39mUpgrade\x1b[0m") {
		t.Errorf("section label is not Highlight:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[1;94mxbps\x1b[0m") {
		t.Errorf("upgrade name is not Secondary:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[1;93m8_1\x1b[0m") {
		t.Errorf("changed version tail is not Notice:\n%s", out)
	}
	if strings.Contains(out, "\x1b[38;5;202m") || strings.Contains(out, "→") {
		t.Errorf("retired arrow/ANSI-202 diff is still rendered:\n%s", out)
	}
}

func TestNalaPlaintextWhenPiped(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/upgrade.txt"))
	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}

	if strings.Contains(out, "╭") || strings.Contains(out, "╰") || strings.Contains(out, "│") {
		t.Errorf("piped output contains box border runes:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("piped output contains ANSI escape codes:\n%s", out)
	}
	if !strings.Contains(out, "Upgrade") {
		t.Errorf("piped output missing plaintext Upgrade header:\n%s", out)
	}
}

func TestNalaPlaintextWhenNoColor(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.set("NO_COLOR", "1")
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/upgrade.txt"))
	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}

	// Interactive with NO_COLOR keeps the nala frame but emits no ANSI.
	if !strings.Contains(out, "\n Upgrade\n") || !strings.Contains(out, "\n Summary\n") {
		t.Errorf("NO_COLOR interactive output missing the nala summary frame:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR output contains ANSI escape codes:\n%s", out)
	}
}

func TestCLIRunDirectSeam(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/upgrade.txt"))

	var outBuf, errBuf strings.Builder
	code := Run([]string{"xbpretty", "upgrade", "--dry-run"}, strings.NewReader(""), &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("Run exit = %d, want 0\n%s", code, errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "Upgrade") {
		t.Errorf("Run output missing summary:\n%s", outBuf.String())
	}
}

func TestHybridPTYExecutionInteractiveVsNonInteractive(t *testing.T) {
	e := newEnv(t)
	e.root = true

	// Custom stub script that checks whether stdout is a TTY and whether stderr is a TTY.
	stubScript := filepath.Join(e.dir, "custom-xbps-install")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

# Check TTY status of stdout (fd 1) and stderr (fd 2)
if [ -t 1 ] && ! [ -t 2 ]; then
  echo "HYBRID_PTY_PIPE_OK"
elif ! [ -t 1 ] && ! [ -t 2 ]; then
  echo "DIRECT_PIPES_OK"
else
  echo "OTHER_STREAMS: stdout_tty=$( [ -t 1 ] && echo 1 || echo 0 ) stderr_tty=$( [ -t 2 ] && echo 1 || echo 0 )"
fi
exit 0
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	// Case 1: Interactive mode (tty = true, no verbose) -> should spawn PTY on stdout and Pipe on stderr
	e.tty = true
	code, out, errs := e.run("xbpretty", "install", "-y", "--no-progress", "figlet")
	if code != 0 {
		t.Fatalf("interactive run failed code %d: %s", code, errs)
	}
	if !strings.Contains(out, "HYBRID_PTY_PIPE_OK") {
		t.Errorf("expected interactive run to use PTY on stdout and pipe on stderr, got out:\n%s\nerr:\n%s", out, errs)
	}

	// Case 2: Non-interactive mode (tty = false) -> should fall back to direct pipes
	e.tty = false
	code, out, errs = e.run("xbpretty", "install", "-y", "figlet")
	if code != 0 {
		t.Fatalf("non-interactive run failed code %d: %s", code, errs)
	}
	if !strings.Contains(out, "DIRECT_PIPES_OK") {
		t.Errorf("expected non-interactive run to use direct pipes, got out:\n%s\nerr:\n%s", out, errs)
	}

	// Case 3: Verbose mode with tty = true -> should fall back to direct pipes
	e.tty = true
	code, out, errs = e.run("xbpretty", "install", "-v", "-y", "figlet")
	if code != 0 {
		t.Fatalf("verbose run failed code %d: %s", code, errs)
	}
	if !strings.Contains(out, "DIRECT_PIPES_OK") {
		t.Errorf("expected verbose run to use direct pipes, got out:\n%s\nerr:\n%s", out, errs)
	}

	// Case 4: Interactive mode with progress widget active (default, without --no-progress)
	// -> child stdout should be consumed by the widget/parser and NOT leaked to stdout!
	e.tty = true
	code, out, errs = e.run("xbpretty", "install", "-y", "figlet")
	if code != 0 {
		t.Fatalf("interactive run with widget failed code %d: %s", code, errs)
	}
	if strings.Contains(out, "HYBRID_PTY_PIPE_OK") {
		t.Errorf("expected child stdout to be suppressed from stdout when widget is active, got:\n%s", out)
	}
	if !strings.Contains(errs, "Transaction complete:") {
		t.Errorf("expected completion message on stderr, got:\n%s", errs)
	}
}

func TestHybridPTYLineBufferingDuringUnpack(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true

	// Custom stub script that writes a line ending in \n, then sleeps.
	// In a PTY, line-buffered output is flushed immediately on \n without waiting 4KB.
	stubScript := filepath.Join(e.dir, "buffering-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

printf '[*] Unpacking packages\nfiglet-2.2.5_6: unpacking ...\n'
sleep 0.4 2>/dev/null || sleep 1
printf 'figlet-2.2.5_6: installed successfully.\n'
exit 0
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	start := time.Now()
	code, out, errs := e.run("xbpretty", "install", "-y", "--no-progress", "figlet")
	duration := time.Since(start)

	if code != 0 {
		t.Fatalf("run failed code %d: %s", code, errs)
	}
	if duration < 350*time.Millisecond {
		t.Logf("child ran in %v", duration)
	}
	if !strings.Contains(out, "figlet-2.2.5_6: unpacking ...") {
		t.Errorf("expected unpacking line in stdout, got:\n%s", out)
	}
	if !strings.Contains(out, "figlet-2.2.5_6: installed successfully.") {
		t.Errorf("expected installed line in stdout, got:\n%s", out)
	}
}

func TestHybridTelemetryStreamParsingWithStub(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true

	// Stub simulates real xbps streaming stderr download ticks and stdout unpacking lines
	stubScript := filepath.Join(e.dir, "telemetry-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

# Simulate stderr download ticks (TTY format with \r)
printf '[ 0%%] figlet-2.2.5_6.x86_64.xbps: [87KB 50%%] 12MB/s ETA: 00m01s\033[K\r' >&2
printf 'figlet-2.2.5_6.x86_64.xbps: 87KB [avg rate: 12MB/s]\033[K\n' >&2

# Simulate stdout phase and unpack/configure lines
printf '[*] Unpacking packages\n'
printf 'figlet-2.2.5_6: unpacking ...\n'
printf '[*] Configuring unpacked packages\n'
printf 'figlet-2.2.5_6: configuring ...\n'
printf 'figlet-2.2.5_6: installed successfully.\n'
printf '\n1 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.\n'
exit 0
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	code, out, errs := e.run("xbpretty", "install", "-y", "--no-progress", "figlet")
	if code != 0 {
		t.Fatalf("run failed code %d: %s", code, errs)
	}

	if !strings.Contains(out, "figlet-2.2.5_6: unpacking ...") {
		t.Errorf("expected unpack output in stdout, got:\n%s", out)
	}
	if !strings.Contains(out, "figlet-2.2.5_6: configuring ...") {
		t.Errorf("expected configure output in stdout, got:\n%s", out)
	}
	if !strings.Contains(errs, "figlet-2.2.5_6.x86_64.xbps: 87KB [avg rate: 12MB/s]") {
		t.Errorf("expected download completion line in stderr, got:\n%s", errs)
	}
}

func TestHybridFailurePreservesStderrAndHint(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true

	// Simulate failure in xbps matching a known hint
	stubScript := filepath.Join(e.dir, "fail-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

printf 'ERROR: failed to open repodata: No such file or directory\n' >&2
exit 2
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	code, out, errs := e.run("xbpretty", "install", "-y", "figlet")
	if code != 2 {
		t.Fatalf("expected code 2, got %d. out:\n%s\nerrs:\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "ERROR: failed to open repodata") {
		t.Errorf("expected error message in stderr, got:\n%s", errs)
	}
	// Verify stale index hint matched
	if !strings.Contains(errs, "hint:") {
		t.Errorf("expected hint for stale index error, got:\n%s", errs)
	}
}

func TestSummaryIgnoresRetiredBoxStyle(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.set("XB_STUB_STDOUT", "xbps-0.60.8_1 update x86_64 https://repo-default.voidlinux.org/current 533481 121137\n")
	e.set("XB_STUB_OLD", lookupRead(t, "0607/xbps.txt"))

	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run", "--color=auto", "--box-style=classic")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	plain := ansiEscape.ReplaceAllString(out, "")
	if !strings.Contains(plain, "\n Upgrade\n") {
		t.Errorf("summary is missing the nala Upgrade section:\n%s", out)
	}
	if strings.Contains(plain, "┌") || strings.Contains(plain, "╭") {
		t.Errorf("the retired box_style still changes the summary:\n%s", out)
	}
}

func TestPrecedenceColorFlagAndNoColor(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	cfgPath := filepath.Join(e.dir, "config", "xbpretty", "config.toml")
	setMode := func(mode string) {
		t.Helper()
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(data), `mode = "Never"`, `mode = "`+mode+`"`, 1)
		if err := os.WriteFile(cfgPath, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/upgrade.txt"))
	setMode("Auto")

	// 1. With NO_COLOR set, color is disabled under Auto.
	e.set("NO_COLOR", "1")
	code, out, errs := e.run("xbpretty", "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR should disable color under auto config:\n%s", out)
	}

	// 2. Explicit --color=always overrides NO_COLOR and Auto.
	code, out, errs = e.run("xbpretty", "upgrade", "--dry-run", "--color=always")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("--color=always should override NO_COLOR:\n%s", out)
	}

	// 3. Explicit --color=never disables color even if NO_COLOR is unset.
	e.set("NO_COLOR", "")
	code, out, errs = e.run("xbpretty", "upgrade", "--dry-run", "--color=never")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--color=never should disable color:\n%s", out)
	}

	// 4. [Color] mode Never is overridden by explicit --color=always.
	setMode("Never")
	code, out, errs = e.run("xbpretty", "upgrade", "--dry-run", "--color=always")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("--color=always should override [Color] mode Never:\n%s", out)
	}
}

func TestProgressBarOptionsAndFlags(t *testing.T) {
	// 1. parseArgs --no-progress
	o, err := parseArgs([]string{"upgrade", "--no-progress"}, "")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.progressBar == nil || *o.progressBar != false {
		t.Errorf("--no-progress did not set progressBar to false: %+v", o)
	}

	// 2. parseArgs --no-progress-bar
	o, err = parseArgs([]string{"upgrade", "--no-progress-bar"}, "")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.progressBar == nil || *o.progressBar != false {
		t.Errorf("--no-progress-bar did not set progressBar to false: %+v", o)
	}

	// 3. parseArgs --progress-bar=false
	o, err = parseArgs([]string{"upgrade", "--progress-bar=false"}, "")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.progressBar == nil || *o.progressBar != false {
		t.Errorf("--progress-bar=false did not set progressBar to false: %+v", o)
	}

	// 4. parseArgs --progress-bar=true
	o, err = parseArgs([]string{"upgrade", "--progress-bar=true"}, "")
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if o.progressBar == nil || *o.progressBar != true {
		t.Errorf("--progress-bar=true did not set progressBar to true: %+v", o)
	}
}

func TestProgressWidgetEndToEndInteractiveSuccess(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true

	stubScript := filepath.Join(e.dir, "widget-success-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

# Simulate stderr download ticks
printf '[ 50%%] figlet-2.2.5_6.x86_64.xbps: [45KB 50%%] 10MB/s ETA: 00m01s\033[K\r' >&2
printf 'figlet-2.2.5_6.x86_64.xbps: 92KB [avg rate: 10MB/s]\033[K\n' >&2

# Simulate stdout unpack / configure lines
printf '[*] Unpacking packages\n'
printf 'figlet-2.2.5_6: unpacking ...\n'
printf '[*] Configuring unpacked packages\n'
printf 'figlet-2.2.5_6: configuring ...\n'
printf 'figlet-2.2.5_6: installed successfully.\n'
printf '\n1 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.\n'
exit 0
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	code, out, errs := e.run("xbpretty", "install", "-y", "figlet")
	if code != 0 {
		t.Fatalf("run failed code %d: %s", code, errs)
	}

	// 1. Verify Download Phase container rendered in stderr
	if !strings.Contains(errs, "Downloading...") {
		t.Errorf("expected Download Phase container in stderr, got:\n%s", errs)
	}
	if !strings.Contains(errs, "Total Packages:") {
		t.Errorf("expected Total Packages in stderr, got:\n%s", errs)
	}

	// 2. Verify Unpack/Configure Phase container rendered in stderr
	if !strings.Contains(errs, "Running xbps...") {
		t.Errorf("expected Running xbps container in stderr, got:\n%s", errs)
	}
	if !strings.Contains(errs, "Current Target:") {
		t.Errorf("expected Current Target in stderr, got:\n%s", errs)
	}

	// 3. Verify in-place cursor movement in stderr
	if !strings.Contains(errs, "\x1b[5A\r") {
		t.Errorf("expected cursor jumps in stderr, got:\n%s", errs)
	}

	// 4. Verify permanent completion line in stderr
	if !strings.Contains(errs, "✔ Transaction complete:") {
		t.Errorf("expected completion line in stderr, got:\n%s", errs)
	}
	if !strings.Contains(errs, "1 installed") {
		t.Errorf("expected '1 installed' in summary, got:\n%s", errs)
	}

	// 5. Verify stdout holds strictly the plan summary (ADR-0005)
	if !strings.Contains(out, "Install") {
		t.Errorf("expected plan summary in stdout, got:\n%s", out)
	}
	if strings.Contains(out, "Downloading...") || strings.Contains(out, "Running xbps...") {
		t.Errorf("interactive widget lines leaked to stdout:\n%s", out)
	}
}

func TestProgressWidgetEndToEndFailureClearsBoxAndPrintsStderr(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true

	stubScript := filepath.Join(e.dir, "widget-fail-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

printf '[ 10%%] figlet-2.2.5_6.x86_64.xbps: [10KB 10%%] 1MB/s ETA: 00m05s\033[K\r' >&2
printf 'ERROR: [trans] failed to download figlet: Connection refused\n' >&2
exit 1
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	code, out, errs := e.run("xbpretty", "install", "-y", "figlet")
	if code != 1 {
		t.Fatalf("expected code 1, got %d. out:\n%s\nerrs:\n%s", code, out, errs)
	}

	// Widget should clear 5 lines in stderr using atomic clear
	if !strings.Contains(errs, "\x1b[5A\r\x1b[0J") {
		t.Errorf("expected cursor jump clearing box on failure in stderr, got:\n%s", errs)
	}
	// Completion banner should NOT be present
	if strings.Contains(errs, "✔ Transaction complete:") || strings.Contains(out, "✔ Transaction complete:") {
		t.Errorf("completion banner should not appear on failure:\n%s", errs)
	}
	// Raw error must be in stderr
	if !strings.Contains(errs, "ERROR: [trans] failed to download figlet") {
		t.Errorf("stderr missing error message:\n%s", errs)
	}
}

func TestProgressWidgetSuppressedWhenNoProgressOrNonTTY(t *testing.T) {
	e := newEnv(t)
	e.root = true

	stubScript := filepath.Join(e.dir, "widget-suppress-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

printf 'figlet-2.2.5_6.x86_64.xbps: 92KB [avg rate: 10MB/s]\n' >&2
printf '[*] Unpacking packages\n'
printf 'figlet-2.2.5_6: unpacking ...\n'
exit 0
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	// Case 1: --no-progress flag
	e.tty = true
	code, out, errs := e.run("xbpretty", "install", "-y", "--no-progress", "figlet")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if strings.Contains(out, "\x1b[5A\r") {
		t.Errorf("--no-progress should not emit cursor jumps:\n%s", out)
	}
	if strings.Contains(out, "╭── Downloading...") {
		t.Errorf("--no-progress should not render container:\n%s", out)
	}
	if !strings.Contains(out, "figlet-2.2.5_6: unpacking ...") {
		t.Errorf("--no-progress should stream raw lines directly:\n%s", out)
	}

	// Case 2: non-TTY stream (e.tty = false)
	e.tty = false
	code, out, errs = e.run("xbpretty", "install", "-y", "figlet")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if strings.Contains(out, "\x1b[5A\r") {
		t.Errorf("non-TTY should not emit cursor jumps:\n%s", out)
	}
	if strings.Contains(out, "╭── Downloading...") {
		t.Errorf("non-TTY should not render container:\n%s", out)
	}
}

func TestProgressWidgetBoxStylesEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true

	stubScript := filepath.Join(e.dir, "widget-style-stub")
	scriptContent := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac

printf '[ 50%%] figlet-2.2.5_6.x86_64.xbps: [45KB 50%%] 10MB/s ETA: 00m01s\033[K\r' >&2
printf '[*] Unpacking packages\n'
printf 'figlet-2.2.5_6: unpacking ...\n'
exit 0
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	e.pointInstallAt(stubScript)

	// Test classic box style
	code, out, errs := e.run("xbpretty", "install", "-y", "--box-style=classic", "figlet")
	if code != 0 {
		t.Fatalf("classic code %d: %s", code, errs)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("stdout missing classic plan summary:\n%s", out)
	}
	plain := ansiEscape.ReplaceAllString(errs, "")
	if !strings.Contains(plain, "┌── Downloading...") {
		t.Errorf("classic style missing classic download header in stderr:\n%s", errs)
	}
	if !strings.Contains(plain, "┌── Running xbps...") {
		t.Errorf("classic style missing classic running header in stderr:\n%s", errs)
	}

	// Test ascii box style
	code, out, errs = e.run("xbpretty", "install", "-y", "--box-style=ascii", "figlet")
	if code != 0 {
		t.Fatalf("ascii code %d: %s", code, errs)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("stdout missing ascii plan summary:\n%s", out)
	}
	plain = ansiEscape.ReplaceAllString(errs, "")
	if !strings.Contains(plain, "+-- Downloading...") {
		t.Errorf("ascii style missing ascii download header in stderr:\n%s", errs)
	}
	if !strings.Contains(plain, "+-- Running xbps...") {
		t.Errorf("ascii style missing ascii running header in stderr:\n%s", errs)
	}
}

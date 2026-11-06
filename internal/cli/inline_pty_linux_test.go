//go:build linux

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/term"
)

// inlinePty is one in-process Run with stdin, stdout and stderr on the same
// real terminal, which is the shape the inline viewport needs.
type inlinePty struct {
	master, slave *os.File
	cap           *capture
	wait          chan int
	before        term.State
}

func startInlinePty(t *testing.T, argv []string, cols, rows int) *inlinePty {
	t.Helper()
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	setProgressPtySize(t, slave, cols, rows)
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState before: %v", err)
	}
	r := &inlinePty{master: master, slave: slave, cap: &capture{}, wait: make(chan int, 1), before: before}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				r.cap.add(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { r.wait <- Run(argv, slave, slave, slave) }()
	return r
}

func (r *inlinePty) send(t *testing.T, s string) {
	t.Helper()
	if _, err := r.master.WriteString(s); err != nil {
		t.Fatalf("writing %q: %v", s, err)
	}
}

func (r *inlinePty) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-r.wait:
		return code
	case <-time.After(15 * time.Second):
		t.Fatalf("run did not exit; capture:\n%q", r.cap.String())
		return -1
	}
}

func (r *inlinePty) waitFor(t *testing.T, desc string, pred func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.cap.String(); pred(s) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; capture:\n%q", desc, r.cap.String())
	return ""
}

func (r *inlinePty) assertTermiosRestored(t *testing.T) {
	t.Helper()
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after the run")
	}
}

// terminalize makes the package's tty predicate the real ioctl check for the
// duration of one test; the unit harness otherwise fakes it from env.tty.
func terminalize(t *testing.T) {
	t.Helper()
	orig := isTerminalFn
	isTerminalFn = isTerminal
	t.Cleanup(func() { isTerminalFn = orig })
}

// writeInlineStub writes a child that emits a real download tick, a
// completion, an unpack and a configure, with enough delay for a test to
// inspect the terminal mid-run. The plan branch answers `-n`.
func writeInlineStub(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inline-child")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    cat <<'PLAN'
alpha-1_1 install x86_64 https://repo-default.voidlinux.org/current 409600 409600
PLAN
    exit 0
    ;;
esac
` + extra + `
printf '[ 50%%] alpha-1_1.x86_64.xbps: [200KB 50%%] 10MB/s ETA: 00m01s\033[K\r' >&2
sleep 0.20
printf 'alpha-1_1.x86_64.xbps: 400KB [avg rate: 10MB/s]\033[K\n' >&2
printf '[*] Unpacking packages\n'
printf 'alpha-1_1: unpacking ...\n'
sleep 0.05
printf 'alpha-1_1: installed successfully.\n'
printf '[*] Configuring unpacked packages\n'
printf 'alpha-1_1: configuring ...\n'
sleep 0.05
printf 'alpha-1_1: configured successfully.\n'
printf '\n1 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.\n'
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// newInlineEnv builds an env whose install child is the telemetry stub.
func newInlineEnv(t *testing.T, stub string) *env {
	t.Helper()
	e := newEnv(t)
	e.root = true
	e.writeConfig(stub)
	e.set("TERM", "xterm")
	terminalize(t)
	return e
}

// TestInlineViewportOnTheNormalScreen drives FR-072 end to end: no alternate
// screen, the no-DSR fallback, cbreak restored before the child, completed
// lines inserted above the re-anchored box, no key reads and a clean teardown.
func TestInlineViewportOnTheNormalScreen(t *testing.T) {
	newInlineEnv(t, writeInlineStub(t, ""))
	r := startInlinePty(t, []string{"xbpretty", "install", "-y", "alpha"}, 80, 24)

	r.waitFor(t, "the Download view", func(s string) bool {
		return strings.Contains(s, "Downloading Packages")
	})
	// The commit child is running here: termios must already be back to the
	// user's mode (FR-072 restores it before the child starts).
	if now, err := term.GetState(r.slave); err == nil && !now.Equal(r.before) {
		t.Errorf("termios not restored before the commit child started")
	}
	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}

	capture := r.cap.String()
	if strings.Contains(capture, term.AltEnter) {
		t.Error("xbpretty entered an alternate screen")
	}
	// FR-072's no-anchor fallback is the only inline path: a DSR query would
	// have to read the queued stdin bytes, so the box starts on a fresh line.
	firstBox := strings.Index(capture, "╭")
	if strings.Contains(capture, term.DSRQuery) {
		t.Errorf("the inline viewport sent a DSR query")
	}
	if firstBox < 0 || !strings.HasSuffix(capture[:firstBox], term.NoAnchor) {
		t.Errorf("the box did not start with the no-anchor fallback:\n%q", capture[:maxInt(0, firstBox)])
	}
	for _, line := range []string{
		"Last completed: alpha-1_1.x86_64.xbps",
		"alpha-1_1: installed successfully.",
		"alpha-1_1: configured successfully.",
	} {
		at := strings.Index(capture, line)
		if at < 0 {
			t.Errorf("completed line %q was not inserted into scrollback:\n%q", line, capture)
			continue
		}
		// The immediate sequence is <erase><line><box>: the box is erased,
		// the completed line becomes scrollback, and the box is redrawn on
		// the next rows with no other output between.
		if !strings.HasSuffix(capture[:at], "\x1b[J") {
			t.Errorf("inserted line %q is not preceded by the box erase:\n%q", line, capture[maxInt(0, at-16):at])
		}
		rest := capture[at+len(line):]
		if !strings.HasPrefix(rest, "\r\n╭") && !strings.HasPrefix(rest, "╭") {
			t.Errorf("the box did not follow inserted line %q immediately: %q", line, rest[:minInt(len(rest), 8)])
		}
	}
	if !strings.Contains(capture, "q") == false && false {
		t.Fatal("unreachable")
	}
	if strings.Contains(capture, "user refused") {
		t.Errorf("the viewport read the q key:\n%s", capture)
	}
	r.assertTermiosRestored(t)
}

// TestInlineViewportHidesForChildAlternateScreen pins the `?1049h/l` latch:
// the child's enter is seen through the stderr scanner's CR split, the box is
// erased while hidden, and the leave redraws it. The cbreak half of the latch
// is proven directly in TestLiveProgressLatchLeavesAndReentersCbreak.
func TestInlineViewportHidesForChildAlternateScreen(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "entered")
	extra := `printf '\033[?1049h\r' >&2
touch ` + marker + `
sleep 0.25
printf '\033[?1049l\r' >&2
`
	newInlineEnv(t, writeInlineStub(t, extra))
	r := startInlinePty(t, []string{"xbpretty", "install", "-y", "alpha"}, 80, 24)

	r.waitFor(t, "the Download view", func(s string) bool {
		return strings.Contains(s, "Downloading Packages")
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child never entered its alternate screen; capture:\n%q", r.cap.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The enter is observed by the latch, which erases the box and draws
	// nothing while hidden. The child is still asleep, so the erase is the
	// final write.
	hidden := r.waitFor(t, "the hidden box", func(s string) bool {
		return strings.HasSuffix(s, "\x1b[J")
	})
	if strings.Index(hidden[strings.LastIndex(hidden, "\x1b[J"):], "╭") >= 0 {
		t.Errorf("a box was drawn while hidden:\n%q", hidden)
	}
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}
	capture := r.cap.String()
	if strings.Contains(capture, term.AltEnter) || strings.Contains(capture, term.AltLeave) {
		t.Errorf("xbpretty echoed the child's alternate-screen markers")
	}
	if !strings.Contains(capture, "\x1b[J╭") && !strings.Contains(capture, "\x1b[J\r╭") {
		t.Errorf("the box was not redrawn immediately after the hide erase:\n%q", capture)
	}
	r.assertTermiosRestored(t)
}

// TestInlineViewportResizeReflowsWidthOnly pins AC-055's resize half over a
// live child: SIGWINCH repaints at the new width and keeps the fixed height.
func TestInlineViewportResizeReflowsWidthOnly(t *testing.T) {
	newInlineEnv(t, writeInlineStub(t, "sleep 0.30\n"))
	r := startInlinePty(t, []string{"xbpretty", "install", "-y", "alpha"}, 80, 24)

	r.waitFor(t, "the 80-column Download view", func(s string) bool {
		return strings.Contains(s, "Downloading Packages")
	})
	setProgressPtySize(t, r.slave, 60, 24)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("SIGWINCH: %v", err)
	}
	r.waitFor(t, "the 60-column frame", func(s string) bool {
		for _, frame := range progressFrames(s) {
			if lines := frameLines(frame); len(lines) == heightDownload && visibleWidth(lines[0]) == 60 {
				return true
			}
		}
		return false
	})
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}
	// Every Download frame repainted at 60 columns keeps the fixed height.
	for _, frame := range progressFrames(r.cap.String()) {
		lines := frameLines(frame)
		if len(lines) == 0 || !strings.Contains(frame, "Downloading Packages") {
			continue
		}
		if visibleWidth(strings.TrimRight(lines[0], "\r")) != 60 {
			continue
		}
		if len(lines) != heightDownload {
			t.Errorf("resized Download frame height = %d, want %d", len(lines), heightDownload)
		}
	}
	r.assertTermiosRestored(t)
}

// TestInlineViewportCtrlCExits130AndTearsDown is FR-072's owned-transfer
// signal path, delivered as SIGINT to the process (the signal a ^C produces).
// TestInlineViewportCtrlCThroughTheTTY covers the real ^C byte on a pty.
func TestInlineViewportCtrlCExits130AndTearsDown(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_EXIT", "0")
	cacheDir := t.TempDir()
	e.useCacheDir(cacheDir)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))
	e.set("TERM", "xterm")
	e.writeConfig("")
	terminalize(t)

	f.mu.Lock()
	f.block = make(chan struct{})
	f.started = make(chan string, 4)
	started := f.started
	f.mu.Unlock()

	r := startInlinePty(t, []string{"xbpretty", "install", "figlet", "-y"}, 80, 24)

	r.waitFor(t, "the Download view", func(s string) bool {
		return strings.Contains(s, "Downloading Packages")
	})
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		close(f.block)
		t.Fatalf("the transfer never started; capture:\n%q", r.cap.String())
	}
	// During xbpretty's own transfer the terminal is in cbreak: canonical,
	// echo and IEXTEN are off, ISIG and OPOST stay on.
	now, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState during transfer: %v", err)
	}
	if now.Equal(r.before) {
		t.Errorf("the owned transfer did not enter cbreak")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		close(f.block)
		t.Fatalf("signalling the test process: %v", err)
	}
	code := r.exitCode(t)
	close(f.block)
	if code != exitInterrupted {
		t.Fatalf("exit = %d, want 130\n%s", code, r.cap.String())
	}
	capture := r.waitFor(t, "the interruption line", func(s string) bool {
		return strings.Contains(s, "xbpretty: interrupted")
	})
	// Teardown clears from the box before the message.
	clearAt := strings.LastIndex(capture, "\x1b[J")
	msgAt := strings.Index(capture, "xbpretty: interrupted")
	if clearAt < 0 || msgAt < 0 || clearAt > msgAt {
		t.Errorf("the box was not cleared before the interruption line:\n%q", capture)
	}
	if strings.Contains(capture, term.AltEnter) {
		t.Error("the inline viewport entered an alternate screen")
	}
	r.assertTermiosRestored(t)
}

// visibleWidth is summary.VisibleWidth with the trailing CR a pty can leave
// on a captured line removed.
func visibleWidth(s string) int {
	return summary.VisibleWidth(strings.TrimSuffix(s, "\r"))
}

// ptyAll is one built binary with stdin, stdout and stderr on the same pty, so
// the S1 screen and the inline viewport share one ordered stream.
type ptyAll struct {
	master, slave *os.File
	cmd           *exec.Cmd
	cap           *capture
	wait          chan error
	before        term.State
}

func startPtyAll(t *testing.T, bin string, env map[string]string, args []string, cols, rows int) *ptyAll {
	t.Helper()
	return startPtyAllMode(t, bin, env, args, cols, rows, false)
}

// startPtyAllControlling makes the pty the child's controlling terminal, so a
// ^C byte written to the master reaches the foreground process group as
// SIGINT, exactly as a real terminal does.
func startPtyAllControlling(t *testing.T, bin string, env map[string]string, args []string, cols, rows int) *ptyAll {
	t.Helper()
	return startPtyAllMode(t, bin, env, args, cols, rows, true)
}

func startPtyAllMode(t *testing.T, bin string, env map[string]string, args []string, cols, rows int, controlling bool) *ptyAll {
	t.Helper()
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	setProgressPtySize(t, slave, cols, rows)
	cmd := exec.Command(bin, args...)
	cmd.Env = envWith(env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	if controlling {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	}
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState before: %v", err)
	}
	r := &ptyAll{master: master, slave: slave, cmd: cmd, cap: &capture{}, wait: make(chan error, 1), before: before}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		master.Close()
		slave.Close()
	})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				r.cap.add(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the binary: %v", err)
	}
	go func() { r.wait <- cmd.Wait() }()
	return r
}

func (r *ptyAll) send(t *testing.T, s string) {
	t.Helper()
	if _, err := r.master.WriteString(s); err != nil {
		t.Fatalf("writing %q: %v", s, err)
	}
}

func (r *ptyAll) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case err := <-r.wait:
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("wait: %v", err)
		return -1
	case <-time.After(15 * time.Second):
		t.Fatalf("binary did not exit; capture:\n%q", r.cap.String())
		return -1
	}
}

func (r *ptyAll) waitFor(t *testing.T, desc string, pred func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.cap.String(); pred(s) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; capture:\n%q", desc, r.cap.String())
	return ""
}

// writeInlineScreenStub is the built-binary child: the plan branch answers the
// dry run, the real branch emits a download completion and an unpack.
func writeInlineScreenStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "screen-child")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    cat <<'PLAN'
libfoo-1.3_1 update x86_64 https://repo-default.voidlinux.org/current 15728640 4334114
libbar-0.9_3 update x86_64 https://repo-default.voidlinux.org/current 15728640 4334114
PLAN
    exit 0
    ;;
esac
printf '[ 50%%] libfoo-1.3_1.x86_64.xbps: [7MB 50%%] 10MB/s ETA: 00m01s\033[K\r' >&2
sleep 0.50
printf 'libfoo-1.3_1.x86_64.xbps: 15MB [avg rate: 10MB/s]\033[K\n' >&2
printf '[*] Unpacking packages\n'
printf 'libfoo-1.3_1: unpacking ...\n'
sleep 0.05
printf 'libfoo-1.3_1: installed successfully.\n'
printf '\n2 downloaded, 2 installed, 0 updated, 0 configured, 0 removed, 0 on hold.\n'
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// inlineScreenEnv writes a Tui+Always config that points the upgrade child at
// stub and answers the old-version lookups through query.
func inlineScreenEnv(t *testing.T, dir, query, stub string) map[string]string {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"[Xbps]",
		`install_path = "` + stub + `"`,
		`remove_path = "` + filepath.Join(stubDir, "xbps-remove") + `"`,
		`query_path = "` + query + `"`,
		"",
		"[Color]",
		`mode = "Always"`,
		"",
		"[Ui]",
		`mode = "Tui"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "xbpretty", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"XDG_CONFIG_HOME":         cfgDir,
		"XDG_STATE_HOME":          filepath.Join(dir, "state"),
		"XB_STUB_LOG":             filepath.Join(dir, "commands.log"),
		"XBPRETTY_TEST_EUID":      "0",
		"XBPRETTY_TEST_TRUST_UID": strconv.Itoa(os.Getuid()),
		"TERM":                    "xterm-256color",
		"LANG":                    "C.UTF-8",
		"LC_ALL":                  "C.UTF-8",
		"LC_CTYPE":                "C.UTF-8",
		"NO_COLOR":                "",
		"COLUMNS":                 "",
		"XBPRETTY_HISTORY_DIR":    filepath.Join(dir, "history"),
	}
}

// TestInlineViewportAfterS1Confirm is FR-072's handoff: confirming S1 leaves
// the alternate screen, disables mouse, shows the cursor and restores termios
// before the viewport is built and before the commit child emits.
func TestInlineViewportAfterS1Confirm(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := inlineScreenEnv(t, dir, query, writeInlineScreenStub(t))
	r := startPtyAll(t, bin, env, []string{"upgrade"}, 80, 24)

	r.waitFor(t, "the S1 frame", func(s string) bool {
		return strings.Contains(s, "xbpretty Upgrade")
	})
	r.send(t, "y")
	r.waitFor(t, "the inline box", func(s string) bool {
		return strings.Contains(s, "Downloading Packages") || strings.Contains(s, "Installing Packages")
	})
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}
	capture := r.cap.String()

	// The S1 leave is ordered before the first viewport box; the child starts
	// after both. No DSR query may appear, because it would consume stdin.
	leave := strings.Index(capture, term.AltLeave)
	cursorShow := strings.Index(capture, term.CursorShow)
	mouseOff := strings.LastIndex(capture, term.MouseOff)
	boxAt := strings.Index(capture, "╭")
	if leave < 0 || cursorShow < 0 || mouseOff < 0 {
		t.Fatalf("S1 did not restore before the viewport; capture:\n%q", capture)
	}
	if !(leave < cursorShow && cursorShow < boxAt) {
		t.Errorf("handoff order leave=%d cursor=%d box=%d, want leave < cursor < box", leave, cursorShow, boxAt)
	}
	if strings.Contains(capture, term.DSRQuery) {
		t.Errorf("the inline viewport sent a DSR query")
	}
	if got := strings.Count(capture, term.AltEnter); got != 1 {
		t.Errorf("alternate-screen entries = %d, want 1 (S1 only)", got)
	}
	if got := strings.Count(capture, term.AltLeave); got != 1 {
		t.Errorf("alternate-screen leaves = %d, want 1 (S1 only)", got)
	}
	// Completed child lines are scrollback above a box that keeps re-anchoring.
	for _, line := range []string{
		"Last completed: libfoo-1.3_1.x86_64.xbps",
		"libfoo-1.3_1: installed successfully.",
	} {
		at := strings.Index(capture, line)
		if at < 0 {
			t.Errorf("completed line %q was not inserted:\n%q", line, capture)
			continue
		}
		if strings.Index(capture[at:], "╭") < 0 {
			t.Errorf("the box did not re-anchor below %q", line)
		}
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after the inline run")
	}
}

// writeStdinStub is a child that records the first line it reads from stdin,
// proving the inline viewport left queued input for it.
func writeStdinStub(t *testing.T) (string, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "stdin-line")
	path := filepath.Join(t.TempDir(), "stdin-child")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    cat <<'PLAN'
alpha-1_1 install x86_64 https://repo-default.voidlinux.org/current 409600 409600
PLAN
    exit 0
    ;;
esac
IFS= read -r line
printf '%s' "$line" > ` + marker + `
printf '[ 50%%] alpha-1_1.x86_64.xbps: [200KB 50%%] 10MB/s ETA: 00m01s\033[K\r' >&2
sleep 0.05
printf 'alpha-1_1.x86_64.xbps: 400KB [avg rate: 10MB/s]\033[K\n' >&2
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, marker
}

// TestInlineViewportDoesNotConsumeChildStdin pins FR-072's "no keys": bytes
// written while the viewport is starting stay queued and reach the child.
func TestInlineViewportDoesNotConsumeChildStdin(t *testing.T) {
	stub, marker := writeStdinStub(t)
	newInlineEnv(t, stub)
	r := startInlinePty(t, []string{"xbpretty", "install", "-y", "alpha"}, 80, 24)
	// Sent immediately, before the child can have read anything; a DSR
	// handshake on the same terminal would have swallowed it.
	r.send(t, "HELLO\n")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := os.ReadFile(marker)
		if err == nil && string(got) == "HELLO" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child never read the queued stdin line (marker=%q, err=%v)", got, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestInlineSignalInCbreakWindowRestoresTermios delivers SIGINT after cbreak
// is entered but before the child exists. The pre-armed handler must keep the
// signal alive, the child still reports the interruption and the deferred
// restore must put the termios flags back.
func TestInlineSignalInCbreakWindowRestoresTermios(t *testing.T) {
	newInlineEnv(t, writeInlineStub(t, "sleep 0.20\n"))
	orig := inlineEnteredFn
	fired := false
	inlineEnteredFn = func() {
		if fired {
			return
		}
		fired = true
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}
	t.Cleanup(func() { inlineEnteredFn = orig })

	r := startInlinePty(t, []string{"xbpretty", "install", "-y", "alpha"}, 80, 24)
	r.waitFor(t, "the Download view", func(s string) bool {
		return strings.Contains(s, "Downloading Packages")
	})
	code := r.exitCode(t)
	if code != exitInterrupted {
		t.Fatalf("exit = %d, want 130\n%s", code, r.cap.String())
	}
	r.assertTermiosRestored(t)
	if strings.Contains(r.cap.String(), term.AltEnter) {
		t.Error("the signal path entered an alternate screen")
	}
}

// TestInlineViewportResizeDuringPredownload pins AC-055's resize rule while
// xbpretty owns the transfer: the inline surface's own SIGWINCH watcher
// reflows the box to 60 columns without changing its fixed height.
func TestInlineViewportResizeDuringPredownload(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_EXIT", "0")
	e.useCacheDir(t.TempDir())
	f := newRepoFixture(t)
	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))
	e.set("TERM", "xterm")
	e.writeConfig("")
	terminalize(t)

	f.mu.Lock()
	f.block = make(chan struct{})
	f.started = make(chan string, 4)
	started := f.started
	block := f.block
	f.mu.Unlock()

	r := startInlinePty(t, []string{"xbpretty", "install", "figlet", "-y"}, 80, 24)
	r.waitFor(t, "the Download view", func(s string) bool {
		return strings.Contains(s, "Downloading Packages")
	})
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		close(block)
		t.Fatalf("the transfer never started; capture:\n%q", r.cap.String())
	}
	setProgressPtySize(t, r.slave, 60, 24)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		close(block)
		t.Fatalf("SIGWINCH: %v", err)
	}
	r.waitFor(t, "the 60-column predownload frame", func(s string) bool {
		for _, frame := range progressFrames(s) {
			lines := frameLines(frame)
			if strings.Contains(frame, "Downloading Packages") && len(lines) == heightDownload &&
				visibleWidth(strings.TrimRight(lines[0], "\r")) == 60 {
				return true
			}
		}
		return false
	})
	close(block)
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}
	r.assertTermiosRestored(t)
}

// TestInlineViewportCtrlCThroughTheTTY is the real terminal path: a ^C byte on
// the pty reaches the foreground process group, the child reports 130 and the
// deferred teardown restores the terminal.
func TestInlineViewportCtrlCThroughTheTTY(t *testing.T) {
	bin := testrootBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := inlineScreenEnv(t, dir, query, writeInlineScreenStub(t))
	r := startPtyAllControlling(t, bin, env, []string{"upgrade"}, 80, 24)

	r.waitFor(t, "the S1 frame", func(s string) bool {
		return strings.Contains(s, "xbpretty Upgrade")
	})
	r.send(t, "y")
	r.waitFor(t, "the inline box", func(s string) bool {
		return strings.Contains(s, "Downloading Packages") || strings.Contains(s, "Installing Packages")
	})
	r.send(t, "\x03")
	if code := r.exitCode(t); code != exitInterrupted {
		t.Fatalf("exit = %d, want 130\n%s", code, r.cap.String())
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after a ^C")
	}
	capture := r.cap.String()
	if strings.Count(capture, term.AltEnter) != 1 {
		t.Errorf("alternate-screen entries = %d, want 1 (S1 only)", strings.Count(capture, term.AltEnter))
	}
	// The box is torn down after the interruption rather than left painted.
	if strings.HasSuffix(capture, "╯\r") {
		t.Errorf("the inline box was left on screen after ^C")
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

//go:build linux

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/term"
)

// tuiScreenPlan is the S1 fixture. Its three download sizes total 13002342
// bytes (WidgetSize "12.4 MiB") and its installed sizes total 45 MiB, which is
// exactly the ADR-0017 footer; the Size column renders each row as "4 MiB".
const tuiScreenPlan = `libfoo-1.3_1 update x86_64 https://repo-default.voidlinux.org/current 15728640 4334114
libbar-0.9_3 update x86_64 https://repo-default.voidlinux.org/current 15728640 4334114
libbaz-2.0_2 update x86_64 https://repo-default.voidlinux.org/current 15728640 4334114
`

// buildTuiBinary builds the binary with the xbpretty_testroot hook so a
// non-root test process can exercise the root-gated S1 screen.
func buildTuiBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "xbpretty")
	cmd := exec.Command("go", "build", "-tags", "xbpretty_testroot", "-o", bin, "../../cmd/xbpretty")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building test binary: %v\n%s", err, out)
	}
	return bin
}

// tuiOldQuery answers the old-version lookups the S1 model needs and reports
// no held packages.
func tuiOldQuery(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "query")
	script := `#!/bin/sh
case "$*" in
  *"-H"*) exit 0 ;;
  *pkgver,installed_size*)
    case "$*" in
      *libfoo*) printf 'libfoo-1.2_1\n'; exit 0 ;;
      *libbar*) printf 'libbar-0.9_2\n'; exit 0 ;;
      *libbaz*) printf 'libbaz-2.0_1\n'; exit 0 ;;
      *) exit 2 ;;
    esac
    ;;
esac
exit 2
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// tuiScreenEnv writes a Tui+Always config that points at the stubs and returns
// the environment for one S1 run.
func tuiScreenEnv(t *testing.T, dir, query string) map[string]string {
	return tuiScreenEnvMode(t, dir, query, "Tui", "Always")
}

func tuiScreenEnvMode(t *testing.T, dir, query, mode, color string) map[string]string {
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
		`install_path = "` + filepath.Join(stubDir, "xbps-install") + `"`,
		`remove_path = "` + filepath.Join(stubDir, "xbps-remove") + `"`,
		`query_path = "` + query + `"`,
		"",
		"[Color]",
		`mode = "` + color + `"`,
		"",
		"[Ui]",
		`mode = "` + mode + `"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "xbpretty", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"XDG_CONFIG_HOME":         cfgDir,
		"XDG_STATE_HOME":          filepath.Join(dir, "state"),
		"XB_STUB_LOG":             filepath.Join(dir, "commands.log"),
		"XB_STUB_STDOUT":          tuiScreenPlan,
		"XB_STUB_STDERR":          "",
		"XB_STUB_HOLD":            "",
		"XB_STUB_OLD":             "",
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

// capture collects the pty master's stream under a mutex.
type capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capture) add(b []byte) {
	c.mu.Lock()
	c.buf.Write(b)
	c.mu.Unlock()
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// ptyRun is one live binary on a real pty.
type ptyRun struct {
	master *os.File
	slave  *os.File
	cmd    *exec.Cmd
	cap    *capture
	stderr bytes.Buffer
	wait   chan error
	before term.State
}

func startPty(t *testing.T, bin string, env map[string]string, args []string, cols, rows int) *ptyRun {
	t.Helper()
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err := setPtySize(slave, cols, rows); err != nil {
		t.Skipf("cannot size the pty: %v", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = envWith(env)
	cmd.Stdin = slave
	cmd.Stdout = slave
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState before start: %v", err)
	}
	r := &ptyRun{master: master, slave: slave, cmd: cmd, cap: &capture{}, wait: make(chan error, 1), before: before}
	cmd.Stderr = &r.stderr
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

func (r *ptyRun) send(t *testing.T, s string) {
	t.Helper()
	if _, err := r.master.WriteString(s); err != nil {
		t.Fatalf("writing %q: %v", s, err)
	}
}

func (r *ptyRun) exitCode(t *testing.T) int {
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
	case <-time.After(10 * time.Second):
		t.Fatalf("binary did not exit; stderr:\n%s", r.stderr.String())
		return -1
	}
}

func waitCapture(t *testing.T, r *ptyRun, desc string, pred func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		s := r.cap.String()
		if pred(s) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; captured:\n%q\nstderr:\n%s", desc, r.cap.String(), r.stderr.String())
	return ""
}

// frameTail returns the stream from the last full repaint onwards.
func frameTail(s string) string {
	if i := strings.LastIndex(s, term.ClearAll); i >= 0 {
		return s[i:]
	}
	return s
}

func goldenBytes(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "tui", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return raw
}

// TestS1FrameBytesAndCancel is AC-050's PTY frame half plus AC-054's normal
// exit: the real binary on an 80x24 pty paints the golden SGR payload and a q
// refusal restores the alternate screen, cursor, mouse, paste and termios.
func TestS1FrameBytesAndCancel(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	golden := goldenBytes(t, "s1-upgrade-80x24.sgr")

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitCapture(t, r, "the 80x24 frame", func(s string) bool {
		i := strings.Index(s, term.ClearAll)
		return i >= 0 && len(s) >= i+len(golden)
	})
	snap := r.cap.String()
	start := strings.Index(snap, term.ClearAll)
	frame := snap[start : start+len(golden)]
	if string(frame) != string(golden) {
		t.Errorf("S1 frame bytes differ from the golden\n--- got ---\n%q\n--- want ---\n%q", frame, golden)
	}
	for _, seq := range []string{term.AltEnter, term.CursorHide, term.MouseOn, term.PasteOn} {
		if !strings.Contains(snap, seq) {
			t.Errorf("the screen entry is missing %q", seq)
		}
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("cancelled S1 exit = %d, want 1", code)
	}
	if !strings.Contains(r.stderr.String(), "xbpretty: user refused confirmation") {
		t.Errorf("refusal line missing:\n%s", r.stderr.String())
	}
	out := waitCapture(t, r, "the screen leave", func(s string) bool {
		return strings.Contains(s, term.AltLeave) && strings.Contains(s, term.CursorShow)
	})
	for _, seq := range []string{term.MouseOff, term.PasteOff, term.AltLeave, term.CursorShow} {
		if !strings.Contains(out, seq) {
			t.Errorf("the restore is missing %q", seq)
		}
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after cancel")
	}
}

// TestS1ConfirmStartsTransaction is AC-050's confirm half: y leaves the screen
// and the real transaction starts with -y.
func TestS1ConfirmStartsTransaction(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	golden := goldenBytes(t, "s1-upgrade-80x24.sgr")

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitCapture(t, r, "the 80x24 frame", func(s string) bool {
		i := strings.Index(s, term.ClearAll)
		return i >= 0 && len(s) >= i+len(golden)
	})
	r.send(t, "y")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("confirmed S1 exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(r.stderr.String(), "user refused") {
		t.Errorf("confirm printed a refusal:\n%s", r.stderr.String())
	}
	log, err := os.ReadFile(env["XB_STUB_LOG"])
	if err != nil {
		t.Fatalf("reading the stub log: %v", err)
	}
	if !strings.Contains(string(log), "-y") {
		t.Errorf("the real transaction did not start with -y:\n%s", log)
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after confirm")
	}
}

// TestS1SigtermRestores is AC-054's signal path: SIGTERM exits 143 after
// leaving the alternate screen and restoring the termios.
func TestS1SigtermRestores(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	golden := goldenBytes(t, "s1-upgrade-80x24.sgr")

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitCapture(t, r, "the 80x24 frame", func(s string) bool {
		i := strings.Index(s, term.ClearAll)
		return i >= 0 && len(s) >= i+len(golden)
	})
	if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if code := r.exitCode(t); code != 143 {
		t.Errorf("SIGTERM exit = %d, want 143", code)
	}
	out := waitCapture(t, r, "the screen leave after SIGTERM", func(s string) bool {
		return strings.Contains(s, term.AltLeave) && strings.Contains(s, term.CursorShow)
	})
	if !strings.Contains(out, term.MouseOff) || !strings.Contains(out, term.PasteOff) {
		t.Errorf("SIGTERM restore is missing the mouse/paste toggles:\n%q", out)
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after SIGTERM")
	}
}

// TestS1ResizeNoticeAndReturn is AC-054's resize path: 80x24 -> 50x15 shows
// the notice with no table/footer, and 80x24 restores the full frame.
func TestS1ResizeNoticeAndReturn(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	golden := goldenBytes(t, "s1-upgrade-80x24.sgr")

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitCapture(t, r, "the 80x24 frame", func(s string) bool {
		i := strings.Index(s, term.ClearAll)
		return i >= 0 && len(s) >= i+len(golden)
	})

	if err := setPtySize(r.slave, 50, 15); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := r.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("SIGWINCH: %v", err)
	}
	waitCapture(t, r, "the too-small notice", func(s string) bool {
		return strings.Contains(frameTail(s), "Terminal too small (60x18 minimum; now 50x15)")
	})
	tail := frameTail(r.cap.String())
	if strings.Contains(tail, "Total download size:") || strings.Contains(tail, "Package:") {
		t.Errorf("the too-small body still draws the table/footer:\n%q", tail)
	}
	if !strings.Contains(tail, "xbpretty Upgrade") {
		t.Errorf("the too-small body dropped the title:\n%q", tail)
	}

	if err := setPtySize(r.slave, 80, 24); err != nil {
		t.Fatalf("resize back: %v", err)
	}
	if err := r.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("SIGWINCH back: %v", err)
	}
	waitCapture(t, r, "the restored 80x24 frame", func(s string) bool {
		return strings.Contains(frameTail(s), "Total download size:12.4 MiB")
	})

	r.send(t, "q")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("cancel after resize exit = %d, want 1", code)
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after resize")
	}
}

// TestS1ColorNeverHasNoSGR is AC-055 at the binary seam: --color never keeps
// every glyph and cursor control but emits no SGR byte.
func TestS1ColorNeverHasNoSGR(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnvMode(t, dir, query, "Tui", "Never")

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitCapture(t, r, "the no-colour frame", func(s string) bool {
		return strings.Contains(frameTail(s), "Disk space required:")
	})
	tail := frameTail(r.cap.String())
	for i := 0; i+1 < len(tail); i++ {
		if tail[i] != 0x1b || tail[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(tail) && tail[j] != 'm' && !(tail[j] >= 'A' && tail[j] <= 'Z') {
			j++
		}
		if j < len(tail) && tail[j] == 'm' {
			t.Fatalf("--color never emitted SGR %q", tail[i:j+1])
		}
	}
	for _, glyph := range []string{"┏", "❯", "━", "┃"} {
		if !strings.Contains(tail, glyph) {
			t.Errorf("--color never removed %q", glyph)
		}
	}
	r.send(t, "q")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("no-colour cancel exit = %d, want 1", code)
	}
}

// TestS1GateMatrixPTY is AC-049 through the real binary: exactly the full gate
// opens S1; every excluded cell takes the text path with no alternate screen.
func TestS1GateMatrixPTY(t *testing.T) {
	bin := buildTuiBinary(t)
	cases := []struct {
		name       string
		alter      func(map[string]string)
		args       []string
		cols, rows int
		mode       string
		wantScreen bool
		wantExit   int
		send       string
	}{
		{
			name: "60x18 opens",
			cols: 60, rows: 18, wantScreen: true, wantExit: 1, send: "q",
		},
		{
			name: "59x17 falls back", cols: 59, rows: 17,
			wantExit: 1, send: "n\n",
		},
		{
			name: "17 rows falls back", cols: 80, rows: 17,
			wantExit: 1, send: "n\n",
		},
		{
			name: "Auto mode falls back",
			mode: "Auto",
			cols: 80, rows: 24, wantExit: 1, send: "n\n",
		},
		{
			name:  "TERM=dumb falls back",
			alter: func(env map[string]string) { env["TERM"] = "dumb" },
			cols:  80, rows: 24, wantExit: 1, send: "n\n",
		},
		{
			name:  "non-UTF-8 locale falls back",
			alter: func(env map[string]string) { env["LC_ALL"] = "C"; env["LC_CTYPE"] = "C"; env["LANG"] = "C" },
			cols:  80, rows: 24, wantExit: 1, send: "n\n",
		},
		{
			name:  "non-root falls back",
			alter: func(env map[string]string) { env["XBPRETTY_TEST_EUID"] = "1000" },
			cols:  80, rows: 24, wantExit: 1,
		},
		{
			name: "-y excludes S1",
			args: []string{"install", "fixture", "-y"},
			cols: 80, rows: 24, wantExit: 0,
		},
		{
			name: "-n excludes S1",
			args: []string{"install", "fixture", "-n"},
			cols: 80, rows: 24, wantExit: 1,
		},
		{
			name: "--simple excludes S1",
			args: []string{"install", "fixture", "--simple"},
			cols: 80, rows: 24, wantExit: 1, send: "n\n",
		},
		{
			name: "--dry-run excludes S1",
			args: []string{"install", "fixture", "--dry-run"},
			cols: 80, rows: 24, wantExit: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			query := tuiOldQuery(t, dir)
			mode := tc.mode
			if mode == "" {
				mode = "Tui"
			}
			env := tuiScreenEnvMode(t, dir, query, mode, "Always")
			if tc.alter != nil {
				tc.alter(env)
			}
			args := tc.args
			if args == nil {
				args = []string{"install", "fixture"}
			}
			r := startPty(t, bin, env, args, tc.cols, tc.rows)
			if tc.send != "" {
				r.send(t, tc.send)
			}
			if code := r.exitCode(t); code != tc.wantExit {
				t.Errorf("exit = %d, want %d\nstderr:\n%s", code, tc.wantExit, r.stderr.String())
			}
			out := r.cap.String()
			if tc.wantScreen {
				if !strings.Contains(out, term.AltEnter) {
					t.Errorf("S1 did not open: %q", out)
				}
				return
			}
			if strings.Contains(out, term.AltEnter) {
				t.Errorf("an excluded gate opened the alternate screen: %q", out)
			}
		})
	}
}

// TestUpdateNeverOpensAReleaseScreen is AC-053: Tui mode and an interactive
// update never enter the alternate screen and never read a key.
func TestUpdateNeverOpensAReleaseScreen(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	// update's count plan is empty, so it finishes without a prompt.
	env["XB_STUB_STDOUT"] = ""

	r := startPty(t, bin, env, []string{"update"}, 80, 24)
	if code := r.exitCode(t); code != 0 {
		t.Errorf("update exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if out := r.cap.String(); strings.Contains(out, term.AltEnter) {
		t.Errorf("update entered the alternate screen: %q", out)
	}
	if strings.Contains(r.stderr.String(), "Accept these changes") {
		t.Errorf("update opened a release-info prompt:\n%s", r.stderr.String())
	}
}

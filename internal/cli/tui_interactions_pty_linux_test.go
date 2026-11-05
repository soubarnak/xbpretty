//go:build linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/term"
)

// tuiScrollPlan builds n update rows so the table is genuinely scrollable.
func tuiScrollPlan(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "pkg%d-1.1_%d update x86_64 https://repo-default.voidlinux.org/current 1048576 1048576\n", i, i)
	}
	return b.String()
}

// tuiFrameEnv is tuiScreenEnv plus the test-only frame dump directory (#76).
func tuiFrameEnv(t *testing.T, dir, query, dump string) map[string]string {
	t.Helper()
	env := tuiScreenEnv(t, dir, query)
	env["XBPRETTY_TEST_TUI_DUMP"] = dump
	return env
}

// dumpedFrames returns the frames the test-only hook wrote, in draw order.
func dumpedFrames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("reading frame dump: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading frame %s: %v", name, err)
		}
		out = append(out, string(raw))
	}
	return out
}

// waitFrameCount waits for at least n dumped frames and returns them.
func waitFrameCount(t *testing.T, r *ptyRun, dir string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if frames := dumpedFrames(t, dir); len(frames) >= n {
			return frames
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d frames; got %d; stderr:\n%s", n, len(dumpedFrames(t, dir)), r.stderr.String())
	return nil
}

var selectedMarker = regexp.MustCompile(`❯\s+(\S+)`)

// selectedPackage returns the package name on the ❯ row.
func selectedPackage(t *testing.T, frame string) string {
	t.Helper()
	m := selectedMarker.FindStringSubmatch(frame)
	if len(m) != 2 {
		t.Fatalf("frame has no selected package:\n%s", frame)
	}
	return m[1]
}

// thumbRows returns the grid rows painted with the scrollbar thumb.
func thumbRows(frame string) []int {
	var rows []int
	for i, line := range strings.Split(frame, "\n") {
		if strings.ContainsRune(line, '█') {
			rows = append(rows, i)
		}
	}
	return rows
}

// TestS1NavigationClampsAndScrolls is AC-050's navigation half: j/k,
// Home/End, PgUp/PgDn and the mouse wheel move the selection, clamp at the
// ends, and move the scrollbar thumb.
func TestS1NavigationClampsAndScrolls(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	dump := t.TempDir()
	env := tuiFrameEnv(t, dir, query, dump)
	env["XB_STUB_STDOUT"] = tuiScrollPlan(30)

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	frames := waitFrameCount(t, r, dump, 1)
	if got := selectedPackage(t, frames[0]); got != "pkg0" {
		t.Fatalf("initial selection = %q, want pkg0", got)
	}
	topThumb := thumbRows(frames[0])
	if len(topThumb) == 0 {
		t.Fatalf("initial frame has no scrollbar thumb:\n%s", frames[0])
	}

	// j then k: selection follows and returns.
	r.send(t, "j")
	frames = waitFrameCount(t, r, dump, 2)
	if got := selectedPackage(t, frames[1]); got != "pkg1" {
		t.Fatalf("after j selection = %q, want pkg1", got)
	}
	r.send(t, "k")
	frames = waitFrameCount(t, r, dump, 3)
	if got := selectedPackage(t, frames[2]); got != "pkg0" {
		t.Fatalf("after k selection = %q, want pkg0", got)
	}

	// k at the top clamps: no new frame is drawn.
	r.send(t, "k")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 3 {
		t.Fatalf("clamped k drew a frame: %d frames, want 3", got)
	}

	// End clamps to the last row and the thumb leaves the top.
	r.send(t, "\x1b[F")
	frames = waitFrameCount(t, r, dump, 4)
	if got := selectedPackage(t, frames[3]); got != "pkg29" {
		t.Fatalf("after End selection = %q, want pkg29", got)
	}
	bottomThumb := thumbRows(frames[3])
	if len(bottomThumb) == 0 || bottomThumb[0] <= topThumb[0] {
		t.Fatalf("scrollbar thumb did not move down: top %v, end %v", topThumb, bottomThumb)
	}

	// j at the bottom clamps.
	r.send(t, "j")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 4 {
		t.Fatalf("clamped j drew a frame: %d frames, want 4", got)
	}

	// Home returns to the first row and the top thumb position.
	r.send(t, "\x1b[H")
	frames = waitFrameCount(t, r, dump, 5)
	if got := selectedPackage(t, frames[4]); got != "pkg0" {
		t.Fatalf("after Home selection = %q, want pkg0", got)
	}
	if got := thumbRows(frames[4]); len(got) == 0 || got[0] != topThumb[0] {
		t.Fatalf("Home thumb %v, want top %v", got, topThumb)
	}

	// PgDn moves ten rows and clamps at the end.
	r.send(t, "\x1b[6~")
	frames = waitFrameCount(t, r, dump, 6)
	if got := selectedPackage(t, frames[5]); got != "pkg10" {
		t.Fatalf("after first PgDn selection = %q, want pkg10", got)
	}
	r.send(t, "\x1b[6~")
	frames = waitFrameCount(t, r, dump, 7)
	if got := selectedPackage(t, frames[6]); got != "pkg20" {
		t.Fatalf("after second PgDn selection = %q, want pkg20", got)
	}
	r.send(t, "\x1b[6~")
	frames = waitFrameCount(t, r, dump, 8)
	if got := selectedPackage(t, frames[7]); got != "pkg29" {
		t.Fatalf("after clamped PgDn selection = %q, want pkg29", got)
	}

	// PgUp moves ten rows back up.
	r.send(t, "\x1b[5~")
	frames = waitFrameCount(t, r, dump, 9)
	if got := selectedPackage(t, frames[8]); got != "pkg19" {
		t.Fatalf("after PgUp selection = %q, want pkg19", got)
	}

	// The SGR wheel moves one row per notch, in both directions.
	r.send(t, "\x1b[<65;10;20M")
	frames = waitFrameCount(t, r, dump, 10)
	if got := selectedPackage(t, frames[9]); got != "pkg20" {
		t.Fatalf("after wheel-down selection = %q, want pkg20", got)
	}
	r.send(t, "\x1b[<64;10;20M")
	frames = waitFrameCount(t, r, dump, 11)
	if got := selectedPackage(t, frames[10]); got != "pkg19" {
		t.Fatalf("after wheel-up selection = %q, want pkg19", got)
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("cancel exit = %d, want 1", code)
	}
}

// activeTab reads the tab label covered by the `━` underline.
func activeTab(t *testing.T, frame string) string {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) < 3 {
		t.Fatalf("frame is too short for a tab strip:\n%s", frame)
	}
	labelLine := []rune(lines[1])
	underline := []rune(lines[2])
	start, length := -1, 0
	for i, r := range underline {
		if r == '━' {
			if start < 0 {
				start = i
			}
			length++
		} else if start >= 0 {
			break
		}
	}
	if start < 0 || length == 0 || start+length > len(labelLine) {
		t.Fatalf("frame has no active-tab underline:\n%s", frame)
	}
	return strings.TrimSpace(string(labelLine[start : start+length]))
}

// TestS1TabsSwitchInFixedOrder is AC-050's tab half: the strip lists present
// operations in the fixed order, h/l and the arrow keys move the active tab
// and its underline, the selection resets to row 0, and clamping draws nothing.
func TestS1TabsSwitchInFixedOrder(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	dump := t.TempDir()
	env := tuiFrameEnv(t, dir, query, dump)
	env["XB_STUB_STDOUT"] = "newpkg-1.0_1 install x86_64 https://repo-default.voidlinux.org/current 1048576 1048576\n" +
		"libfoo-1.3_1 update x86_64 https://repo-default.voidlinux.org/current 15728640 4334114\n"

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	frames := waitFrameCount(t, r, dump, 1)
	if got := activeTab(t, frames[0]); got != "Install" {
		t.Fatalf("initial active tab = %q, want Install", got)
	}
	if !strings.Contains(frames[0], "Install Upgrade") {
		t.Fatalf("tab strip is not in the fixed order:\n%s", frames[0])
	}
	if got := selectedPackage(t, frames[0]); got != "newpkg" {
		t.Fatalf("initial Install selection = %q, want newpkg", got)
	}

	r.send(t, "l")
	frames = waitFrameCount(t, r, dump, 2)
	if got := activeTab(t, frames[1]); got != "Upgrade" {
		t.Fatalf("after l active tab = %q, want Upgrade", got)
	}
	if got := selectedPackage(t, frames[1]); got != "libfoo" {
		t.Fatalf("after l selection = %q, want libfoo (row reset)", got)
	}

	// l at the last tab clamps: no new frame is drawn.
	r.send(t, "l")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 2 {
		t.Fatalf("clamped l drew a frame: %d frames, want 2", got)
	}

	r.send(t, "h")
	frames = waitFrameCount(t, r, dump, 3)
	if got := activeTab(t, frames[2]); got != "Install" {
		t.Fatalf("after h active tab = %q, want Install", got)
	}

	// h at the first tab clamps.
	r.send(t, "h")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 3 {
		t.Fatalf("clamped h drew a frame: %d frames, want 3", got)
	}

	r.send(t, "\x1b[C")
	frames = waitFrameCount(t, r, dump, 4)
	if got := activeTab(t, frames[3]); got != "Upgrade" {
		t.Fatalf("after right arrow active tab = %q, want Upgrade", got)
	}
	r.send(t, "\x1b[D")
	frames = waitFrameCount(t, r, dump, 5)
	if got := activeTab(t, frames[4]); got != "Install" {
		t.Fatalf("after left arrow active tab = %q, want Install", got)
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("cancel exit = %d, want 1", code)
	}
}

// TestS1PackageInfoRoundTrip is AC-050's `s` half: s opens the selected row's
// metadata in the same frame, keys other than q/Esc are inert, returning
// restores the summary with the selection intact, and the transaction can
// still be confirmed afterwards.
func TestS1PackageInfoRoundTrip(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	dump := t.TempDir()
	env := tuiFrameEnv(t, dir, query, dump)

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	frames := waitFrameCount(t, r, dump, 1)
	if !strings.Contains(frames[0], "Old Version:") {
		t.Fatalf("summary frame missing its table:\n%s", frames[0])
	}

	// Move off the first row, then inspect it.
	r.send(t, "j")
	frames = waitFrameCount(t, r, dump, 2)
	if got := selectedPackage(t, frames[1]); got != "libbar" {
		t.Fatalf("selection before s = %q, want libbar", got)
	}
	r.send(t, "s")
	frames = waitFrameCount(t, r, dump, 3)
	info := frames[2]
	for _, want := range []string{
		"xbpretty Upgrade",
		"Package:", "libbar",
		"Operation:", "Upgrade",
		"Installed:", "0.9_2",
		"Available:", "0.9_3",
		"(q) back to summary",
	} {
		if !strings.Contains(info, want) {
			t.Errorf("package-info frame missing %q:\n%s", want, info)
		}
	}
	for _, unwanted := range []string{"❯", "Old Version:", "Total download size:"} {
		if strings.Contains(info, unwanted) {
			t.Errorf("package-info frame still draws %q:\n%s", unwanted, info)
		}
	}

	// n and Enter are inert in the info view: no frame is drawn.
	r.send(t, "n")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 3 {
		t.Fatalf("inert n in package info drew a frame: %d frames, want 3", got)
	}
	r.send(t, "\r")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 3 {
		t.Fatalf("Enter in package info drew a frame: %d frames, want 3", got)
	}

	// q returns to the summary with the selection preserved.
	r.send(t, "q")
	frames = waitFrameCount(t, r, dump, 4)
	if got := selectedPackage(t, frames[3]); got != "libbar" {
		t.Fatalf("selection after returning = %q, want libbar", got)
	}
	if !strings.Contains(frames[3], "Old Version:") {
		t.Fatalf("returning did not restore the summary frame:\n%s", frames[3])
	}

	// Esc also returns, and y still confirms the transaction afterwards.
	r.send(t, "s")
	frames = waitFrameCount(t, r, dump, 5)
	if !strings.Contains(frames[4], "Operation:") {
		t.Fatalf("second s did not reopen package info:\n%s", frames[4])
	}
	r.send(t, "\x1b")
	frames = waitFrameCount(t, r, dump, 6)
	if !strings.Contains(frames[5], "❯") {
		t.Fatalf("Esc did not return to the summary:\n%s", frames[5])
	}
	r.send(t, "y")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("confirm after package-info round trip exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	log, err := os.ReadFile(env["XB_STUB_LOG"])
	if err != nil {
		t.Fatalf("reading the stub log: %v", err)
	}
	if !strings.Contains(string(log), "-y") {
		t.Errorf("the transaction did not start with -y after the round trip:\n%s", log)
	}
}

// TestS1ResizeBelowFloorCancelKeepsWorking is AC-054's resize half: the
// too-small body keeps the border and title, drops tab/table/footer, and the
// cancel keys still refuse the transaction.
func TestS1ResizeBelowFloorCancelKeepsWorking(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	dump := t.TempDir()
	env := tuiFrameEnv(t, dir, query, dump)

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitFrameCount(t, r, dump, 1)
	if err := setPtySize(r.slave, 50, 15); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := r.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("SIGWINCH: %v", err)
	}
	frames := waitFrameCount(t, r, dump, 2)
	small := frames[1]
	if !strings.Contains(small, "Terminal too small (60x18 minimum; now 50x15)") {
		t.Fatalf("too-small frame missing the notice:\n%s", small)
	}
	if !strings.Contains(small, "xbpretty Upgrade") || !strings.Contains(small, "┏") || !strings.Contains(small, "┗") {
		t.Fatalf("too-small frame dropped the title or border:\n%s", small)
	}
	for _, unwanted := range []string{"Package:", "Total download size:", "Upgrade:", "Old Version:"} {
		if strings.Contains(small, unwanted) {
			t.Errorf("too-small frame still draws %q:\n%s", unwanted, small)
		}
	}
	// Row 1 is the tab strip in the full frame; the too-small body must not
	// paint it (the title lives in row 0's border).
	if strings.Contains(strings.Split(small, "\n")[1], "Upgrade") {
		t.Errorf("too-small frame still draws the tab strip:\n%s", small)
	}

	r.send(t, "n")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("cancel while too small exit = %d, want 1", code)
	}
	if !strings.Contains(r.stderr.String(), "xbpretty: user refused confirmation") {
		t.Errorf("refusal line missing:\n%s", r.stderr.String())
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios not restored after a too-small cancel")
	}
}

// startPtyDetached starts the binary on a pty with no master reader. Closing
// the master then delivers a real hangup to the child's stdin, which is how
// the EOF binding is exercised; a reader would keep the master description
// alive and suppress the hangup.
func startPtyDetached(t *testing.T, bin string, env map[string]string, args []string, cols, rows int) *ptyRun {
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
	r := &ptyRun{master: master, slave: slave, cmd: cmd, cap: &capture{}, stderr: &syncBuffer{}, wait: make(chan error, 1), before: before}
	cmd.Stderr = r.stderr
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		master.Close()
		slave.Close()
	})
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the binary: %v", err)
	}
	go func() { r.wait <- cmd.Wait() }()
	return r
}

// waitStderr waits for the binary's stderr to contain want.
func waitStderr(t *testing.T, r *ptyRun, want string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.stderr.String(); strings.Contains(s, want) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for stderr %q; got:\n%s", want, r.stderr.String())
	return ""
}

// TestS1FailedInitWarnsOnceAndFallsBack is FR-067's fallback half: a failed
// screen open warns exactly once, prints no screen sequence, and the text
// summary finishes the transaction with the text path's exit code.
func TestS1FailedInitWarnsOnceAndFallsBack(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	env["XBPRETTY_TEST_TUI_FAIL"] = "init"

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitStderr(t, r, "full-screen interface unavailable")
	r.send(t, "n\n")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit = %d, want 1\nstderr:\n%s", code, r.stderr.String())
	}
	errs := r.stderr.String()
	if got := strings.Count(errs, "full-screen interface unavailable"); got != 1 {
		t.Errorf("init-failure warnings = %d, want 1:\n%s", got, errs)
	}
	out := r.cap.String()
	if strings.Contains(out, term.AltEnter) {
		t.Errorf("failed init still entered the alternate screen: %q", out)
	}
	if !strings.Contains(out, "Total download size:") {
		t.Errorf("failed init did not fall back to the text summary:\n%q", out)
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios not restored after a failed init")
	}
}

// TestS1FailedRenderWarnsOnceRestoresAndFallsBack is FR-067's failed-render
// path: the terminal is restored before the one warning, and the text path
// takes over.
func TestS1FailedRenderWarnsOnceRestoresAndFallsBack(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnv(t, dir, query)
	env["XBPRETTY_TEST_TUI_FAIL"] = "render"

	r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
	waitStderr(t, r, "full-screen interface failed")
	// The restore must precede the warning; the alternate screen is left
	// before the text summary is asked to continue.
	waitCapture(t, r, "the restore after the failed render", func(s string) bool {
		return strings.Contains(s, term.AltLeave) && strings.Contains(s, term.CursorShow)
	})
	r.send(t, "n\n")
	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit = %d, want 1\nstderr:\n%s", code, r.stderr.String())
	}
	counting := r.stderr.String()
	if got := strings.Count(counting, "full-screen interface failed"); got != 1 {
		t.Errorf("render-failure warnings = %d, want 1:\n%s", got, counting)
	}
	if strings.Contains(counting, "full-screen interface unavailable") {
		t.Errorf("the render failure also warned as an init failure:\n%s", counting)
	}
	if !strings.Contains(r.cap.String(), "Total download size:") {
		t.Errorf("failed render did not fall back to the text summary:\n%q", r.cap.String())
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios not restored after a failed render")
	}
}

// TestS1KeyBindings is AC-050's confirm/refuse matrix: y/Y confirm and start
// the transaction, q/Q/n/N/Esc/Ctrl-C/EOF refuse with exit 1, and Enter is
// inert (it neither exits nor draws).
func TestS1KeyBindings(t *testing.T) {
	bin := buildTuiBinary(t)
	cases := []struct {
		name string
		send string
		eof  bool
		want int
	}{
		{"y confirms", "y", false, 0},
		{"Y confirms", "Y", false, 0},
		{"q refuses", "q", false, 1},
		{"Q refuses", "Q", false, 1},
		{"n refuses", "n", false, 1},
		{"N refuses", "N", false, 1},
		{"esc refuses", "\x1b", false, 1},
		{"ctrl-c refuses", "\x03", false, 1},
		{"eof refuses", "", true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			query := tuiOldQuery(t, dir)
			env := tuiScreenEnv(t, dir, query)
			if tc.eof {
				detachedDump := t.TempDir()
				env["XBPRETTY_TEST_TUI_DUMP"] = detachedDump
				r := startPtyDetached(t, bin, env, []string{"upgrade"}, 80, 24)
				waitFrameCount(t, r, detachedDump, 1)
				_ = r.master.Close()
				if code := r.exitCode(t); code != tc.want {
					t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, tc.want, r.stderr.String())
				}
				if !strings.Contains(r.stderr.String(), "xbpretty: user refused confirmation") {
					t.Errorf("refusal line missing:\n%s", r.stderr.String())
				}
				return
			}
			r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
			waitCapture(t, r, "the 80x24 frame", func(s string) bool {
				return strings.Contains(s, "Disk space required:")
			})
			r.send(t, tc.send)
			if code := r.exitCode(t); code != tc.want {
				t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, tc.want, r.stderr.String())
			}
			if tc.want == 0 {
				log, err := os.ReadFile(env["XB_STUB_LOG"])
				if err != nil {
					t.Fatalf("reading the stub log: %v", err)
				}
				if !strings.Contains(string(log), "-y") {
					t.Errorf("confirm did not start the transaction with -y:\n%s", log)
				}
				return
			}
			if !strings.Contains(r.stderr.String(), "xbpretty: user refused confirmation") {
				t.Errorf("refusal line missing:\n%s", r.stderr.String())
			}
			waitCapture(t, r, "the restore", func(s string) bool {
				return strings.Contains(s, term.AltLeave)
			})
		})
	}

	t.Run("Enter is inert", func(t *testing.T) {
		dir := t.TempDir()
		query := tuiOldQuery(t, dir)
		dump := t.TempDir()
		env := tuiFrameEnv(t, dir, query, dump)
		r := startPty(t, bin, env, []string{"upgrade"}, 80, 24)
		waitFrameCount(t, r, dump, 1)
		r.send(t, "\r")
		time.Sleep(150 * time.Millisecond)
		if got := len(dumpedFrames(t, dump)); got != 1 {
			t.Fatalf("Enter drew a frame: %d frames, want 1", got)
		}
		r.send(t, "q")
		if code := r.exitCode(t); code != 1 {
			t.Errorf("exit after inert Enter = %d, want 1", code)
		}
	})
}

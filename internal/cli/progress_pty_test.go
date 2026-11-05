//go:build linux

package cli

import (
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// setPtySize applies a TIOCSWINSZ window size to a pty file.
func setProgressPtySize(t *testing.T, f *os.File, cols, rows int) {
	t.Helper()
	ws := winsize{ws_col: uint16(cols), ws_row: uint16(rows)}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		t.Skipf("setting pty size: %v", errno)
	}
}

// readPty drains a pty master until the slave is closed.
func readPty(master *os.File) func() string {
	var mu sync.Mutex
	var b strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				b.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return b.String()
	}
}

func progressFrames(capture string) []string {
	var frames []string
	offset := 0
	for {
		start := strings.Index(capture[offset:], "╭")
		if start < 0 {
			return frames
		}
		start += offset
		end := strings.Index(capture[start:], "╰")
		if end < 0 {
			return frames
		}
		end += start
		// The bottom border starts at "╰" and ends at the next line break.
		stop := len(capture)
		for i := end; i < len(capture); i++ {
			if capture[i] == '\r' || capture[i] == '\n' {
				stop = i
				break
			}
		}
		frames = append(frames, capture[start:stop])
		offset = stop
	}
}

func frameLines(frame string) []string {
	lines := strings.Split(frame, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

// ptyWidgetRun runs Run with both streams on real PTYs of the requested size
// and returns the captured stderr, stdout and exit code.
func ptyWidgetRun(t *testing.T, argv []string, cols int) (string, string, int) {
	t.Helper()
	stub := writeTelemetryStub(t)
	e := newEnv(t)
	e.root = true
	e.writeConfig(stub)

	orig := isTerminalFn
	isTerminalFn = isTerminal
	t.Cleanup(func() { isTerminalFn = orig })

	outMaster, outSlave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	errMaster, errSlave, err2 := openPty()
	if err2 != nil {
		t.Skipf("no pseudo-terminal: %v", err2)
	}
	defer outMaster.Close()
	defer errMaster.Close()
	setProgressPtySize(t, outSlave, cols, 24)
	setProgressPtySize(t, errSlave, cols, 24)

	outCapture := readPty(outMaster)
	errCapture := readPty(errMaster)

	done := make(chan int, 1)
	go func() { done <- Run(argv, strings.NewReader(""), outSlave, errSlave) }()
	var code int
	select {
	case code = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the PTY run never finished")
	}
	outSlave.Close()
	errSlave.Close()
	<-time.After(50 * time.Millisecond)
	return errCapture(), outCapture(), code
}

// writeTelemetryStub writes a stub that emits real xbps telemetry for one
// archive and one package.
func writeTelemetryStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry-stub")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac
printf '[ 50%%] figlet-2.2.5_6.x86_64.xbps: [45KB 50%%] 10MB/s ETA: 00m01s\033[K\r' >&2
sleep 0.05
printf 'figlet-2.2.5_6.x86_64.xbps: 92KB [avg rate: 10MB/s]\033[K\n' >&2
sleep 0.05
printf '[*] Unpacking packages\n'
printf 'figlet-2.2.5_6: unpacking ...\n'
printf '[*] Configuring unpacked packages\n'
printf 'figlet-2.2.5_6: configuring ...\n'
sleep 0.05
printf 'figlet-2.2.5_6: installed successfully.\n'
printf '\n1 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.\n'
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAC046WidgetOverPTY80 drives AC-046 end to end: width min(cols,80),
// Download height 7, Install height 6, and a clean teardown with no completion
// line.
func TestAC046WidgetOverPTY80(t *testing.T) {
	errOut, _, code := ptyWidgetRun(t, []string{"xbpretty", "install", "-y", "figlet"}, 80)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errOut)
	}
	frames := progressFrames(errOut)
	if len(frames) < 2 {
		t.Fatalf("captured %d frames, want at least a Download and an Install frame:\n%q", len(frames), errOut)
	}
	if got := len(frameLines(frames[0])); got != heightDownload {
		t.Errorf("Download frame height = %d, want 7", got)
	}
	last := frames[len(frames)-1]
	if !strings.Contains(last, "Installing Packages") {
		t.Errorf("last frame is not the Install view:\n%s", last)
	}
	if got := len(frameLines(last)); got != heightOther {
		t.Errorf("Install frame height = %d, want 6", got)
	}
	for _, frame := range frames {
		for i, line := range frameLines(frame) {
			if w := summary.VisibleWidth(line); w != 80 {
				t.Errorf("frame line %d width = %d, want 80: %q", i, w, line)
			}
		}
		if !strings.Contains(frame, " Progress ") {
			t.Errorf("frame missing the centred Progress label:\n%s", frame)
		}
	}
	if strings.Contains(errOut, "Transaction complete") || strings.Contains(errOut, "✔") {
		t.Errorf("teardown printed a completion line:\n%s", errOut)
	}
	if !strings.HasSuffix(strings.TrimRight(errOut, "\x00"), "\x1b[J") {
		t.Errorf("teardown did not clear the box: %q", errOut)
	}
}

// TestAC046WidgetWidthIsCappedAt80 checks the min(columns,80) rule at a wider
// terminal.
func TestAC046WidgetWidthIsCappedAt80(t *testing.T) {
	errOut, _, code := ptyWidgetRun(t, []string{"xbpretty", "install", "-y", "figlet"}, 100)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errOut)
	}
	frames := progressFrames(errOut)
	if len(frames) == 0 {
		t.Fatalf("no frames captured:\n%q", errOut)
	}
	for i, line := range frameLines(frames[0]) {
		if w := summary.VisibleWidth(line); w != 80 {
			t.Errorf("100-column terminal line %d width = %d, want 80: %q", i, w, line)
		}
	}
}

// TestAC046WidgetAt60 checks the squeezed width keeps the full-frame divider
// and the W-4 bar.
func TestAC046WidgetAt60(t *testing.T) {
	errOut, _, code := ptyWidgetRun(t, []string{"xbpretty", "install", "-y", "figlet"}, 60)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errOut)
	}
	frames := progressFrames(errOut)
	if len(frames) == 0 {
		t.Fatalf("no frames captured:\n%q", errOut)
	}
	for i, line := range frameLines(frames[0]) {
		if w := summary.VisibleWidth(line); w != 60 {
			t.Errorf("line %d width = %d, want 60: %q", i, w, line)
		}
	}
	bar := []rune(strings.TrimSuffix(strings.TrimPrefix(frameLines(frames[0])[3], "│ "), " │"))
	if len(bar) != 56 {
		t.Errorf("bar cells = %d, want W-4 = 56", len(bar))
	}
}

// TestAC047PlainLineOverPTY runs with stderr on a TTY and stdout piped, so
// the widget is unavailable and the 40-cell line is used instead.
func TestAC047PlainLineOverPTY(t *testing.T) {
	stub := writeTelemetryStub(t)
	e := newEnv(t)
	e.root = true
	e.writeConfig(stub)
	orig := isTerminalFn
	isTerminalFn = isTerminal
	t.Cleanup(func() { isTerminalFn = orig })

	errMaster, errSlave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	setProgressPtySize(t, errSlave, 80, 24)
	defer errMaster.Close()
	capture := readPty(errMaster)

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		done <- Run([]string{"xbpretty", "install", "-y", "figlet"}, strings.NewReader(""), outW, errSlave)
	}()
	var code int
	select {
	case code = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the PTY run never finished")
	}
	outW.Close()
	errSlave.Close()
	_, _ = io.ReadAll(outR)
	outR.Close()
	<-time.After(50 * time.Millisecond)
	errOut := capture()
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "\r\x1b[2K[") {
		t.Errorf("plain line did not redraw in place:\n%q", errOut)
	}
	if strings.Contains(errOut, "╭") {
		t.Errorf("the widget drew even though stdout is redirected:\n%s", errOut)
	}
	plain := errOut[strings.LastIndex(errOut, "\x1b[2K[")+len("\x1b[2K"):]
	closeAt := strings.IndexByte(plain, ']')
	if closeAt < 0 {
		t.Fatalf("plain line has no closing bar bracket:\n%q", errOut)
	}
	if cells := closeAt - 1; cells != plainLineWidth {
		t.Errorf("plain bar cells = %d, want 40", cells)
	}
	if !strings.Contains(errOut, "| Packages: ") {
		t.Errorf("plain line missing its Packages filter:\n%q", errOut)
	}
	if strings.Contains(errOut, "\x1b[1;") || strings.Contains(errOut, "\x1b[0;") {
		t.Errorf("plain line carried colour:\n%q", errOut)
	}
}

// TestAC048ChildOutputForwardedVerbatim covers the no-live-view rule: with
// stderr redirected no live surface draws, and the child's output reaches
// stderr unchanged.
func TestAC048ChildOutputForwardedVerbatim(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	stub := filepath.Join(t.TempDir(), "plain-stub")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
esac
printf 'child stdout line\n'
printf 'child stderr line\n' >&2
exit 0
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(stub)
	code, out, errs := e.run("xbpretty", "install", "-y", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "child stdout line") || !strings.Contains(errs, "child stderr line") {
		t.Errorf("child output was not forwarded on stderr:\n%s", errs)
	}
	if strings.Contains(out, "child stdout line") {
		t.Errorf("child stdout leaked to stdout:\n%s", out)
	}
	if strings.Contains(errs, "\r\x1b[2K") || strings.Contains(errs, "╭") {
		t.Errorf("a live surface drew on a redirected stderr:\n%q", errs)
	}
}

// TestAC046UpdateView drives the adapted Updating Package Lists view from
// xbps's repository sync output.
func TestAC046UpdateView(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	stub := filepath.Join(t.TempDir(), "update-stub")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
  -S)
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current' ...\n"
    sleep 0.05
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/nonfree' ...\n"
    exit 0
    ;;
esac
exit 0
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(stub)
	e.set("XB_STUB_STDOUT", "")

	code, _, errs := e.run("xbpretty", "update")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "Updating Package Lists") {
		t.Errorf("missing the Update view:\n%s", errs)
	}
	if !strings.Contains(errs, "Updating: https://repo-default.voidlinux.org/current") {
		t.Errorf("missing the repository URL message:\n%s", errs)
	}
	if !strings.Contains(errs, "Repositories:") {
		t.Errorf("missing the Repositories counter:\n%s", errs)
	}
	if strings.Contains(errs, "Transaction complete") {
		t.Errorf("the Update view printed a completion line:\n%s", errs)
	}
}

// TestAC046MirrorScoreView feeds the Testing Mirrors view from #73's
// fetch.ProgressEvent stream.
func TestAC046MirrorScoreView(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "one", region: "EU", location: "A", tier: 1, delay: 40 * time.Millisecond},
		{name: "two", region: "EU", location: "B", tier: 2, delay: 80 * time.Millisecond},
	})
	e, _ := newFetchEnv(t, fixture)
	e.root = true
	e.tty = true
	e.set("TERM", "xterm")

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 1 { // #74's write seam still refuses
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	frames := progressFrames(errs)
	if len(frames) == 0 {
		t.Fatalf("no MirrorScore frames captured:\n%q", errs)
	}
	if !strings.Contains(frames[0], "Testing Mirrors") {
		t.Errorf("first frame is not the MirrorScore view:\n%s", frames[0])
	}
	if strings.Contains(frames[0], "Installing Packages") || strings.Contains(frames[0], "Downloading Packages") {
		t.Errorf("a transaction view flashed before the probes:\n%s", frames[0])
	}
	if !strings.Contains(frames[0], "Mirrors:  0/2") {
		t.Errorf("first frame did not seed the mirror total:\n%s", frames[0])
	}
	if !strings.Contains(errs, "Finished: ") {
		t.Errorf("missing the Finished message:\n%s", errs)
	}
	if !strings.Contains(errs, "Mirrors: ") {
		t.Errorf("missing the Mirrors counter:\n%s", errs)
	}
	if strings.Contains(errs, "Transaction complete") {
		t.Errorf("the MirrorScore view printed a completion line:\n%s", errs)
	}
}

// TestCtrlCCancelsOwnedTransfer pins FR-072: Ctrl-C during xbpretty's probe
// round tears the view down, prints the interruption and exits 130.
func TestCtrlCCancelsOwnedTransfer(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		want int
	}{
		{"SIGINT", syscall.SIGINT, exitInterrupted},
		{"SIGTERM", syscall.SIGTERM, 128 + int(syscall.SIGTERM)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newFetchFixture(t, []fetchMirror{
				{name: "slow", region: "EU", location: "A", tier: 1, delay: 2 * time.Second},
			})
			e, _ := newFetchEnv(t, fixture)
			e.root = true
			e.tty = true
			e.set("TERM", "xterm")

			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, tc.sig)
			defer signal.Stop(sigs)

			type fetchResult struct {
				code int
				errs string
			}
			done := make(chan fetchResult, 1)
			go func() {
				code, _, errs := e.run("xbpretty", "fetch")
				done <- fetchResult{code: code, errs: errs}
			}()

			deadline := time.Now().Add(10 * time.Second)
			for {
				select {
				case result := <-done:
					if result.code != tc.want {
						t.Fatalf("exit = %d, want %d\n%s", result.code, tc.want, result.errs)
					}
					if !strings.Contains(result.errs, "xbpretty: interrupted") {
						t.Errorf("missing the interruption message:\n%s", result.errs)
					}
					return
				default:
				}
				if time.Now().After(deadline) {
					t.Fatalf("fetch did not exit after %s", tc.name)
				}
				if err := syscall.Kill(os.Getpid(), tc.sig); err != nil {
					t.Fatalf("signalling the test process: %v", err)
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}

// TestAC048StdoutCarriesOnlyDurableOutput keeps the stream split: a piped dry
// run's stdout has the summary, no ANSI and no carriage returns.
func TestAC048StdoutCarriesOnlyDurableOutput(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	code, out, errs := e.run("xbpretty", "install", "--dry-run", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("stdout carries ANSI:\n%q", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stdout carries a carriage return:\n%q", out)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("stdout is missing the summary:\n%s", out)
	}
}

// TestUpdateViewFailurePrintsCapturedOutput keeps FR-064's failure path for
// the update view: the box clears and the sync's captured output is printed.
func TestUpdateViewFailurePrintsCapturedOutput(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	stub := filepath.Join(t.TempDir(), "update-fail-stub")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
  -S)
    printf 'ERROR: failed to open repodata: /var/db/xbps/x86_64-repodata\n' >&2
    exit 2
    ;;
esac
exit 0
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(stub)
	e.set("XB_STUB_STDOUT", "")

	code, _, errs := e.run("xbpretty", "update")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, errs)
	}
	if !strings.Contains(errs, "Updating Package Lists") {
		t.Errorf("missing the Update view:\n%s", errs)
	}
	const raw = "ERROR: failed to open repodata"
	clearAt := strings.Index(errs, "\x1b[J")
	rawAt := strings.Index(errs, raw)
	hintAt := strings.Index(errs, "hint:")
	if clearAt < 0 {
		t.Fatalf("the view did not clear on failure:\n%s", errs)
	}
	if rawAt < 0 {
		t.Fatalf("captured sync output was dropped:\n%s", errs)
	}
	if hintAt < 0 {
		t.Fatalf("the stale-index hint was dropped:\n%s", errs)
	}
	if !(clearAt < rawAt && rawAt < hintAt) {
		t.Errorf("ordering is clear=%d raw=%d hint=%d; want clear before raw before hint", clearAt, rawAt, hintAt)
	}
}

// TestAC047NoLiveSurfaceWhenOnlyStdoutIsATTY covers the real per-file TTY
// matrix: stdout is a terminal but stderr is a pipe, so the widget is
// unavailable and the plain line is unavailable too. The child output is
// forwarded verbatim on stderr.
func TestAC047NoLiveSurfaceWhenOnlyStdoutIsATTY(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.writeConfig(writeTelemetryStub(t))

	orig := isTerminalFn
	isTerminalFn = isTerminal
	t.Cleanup(func() { isTerminalFn = orig })

	outMaster, outSlave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer outMaster.Close()
	setProgressPtySize(t, outSlave, 80, 24)
	go func() {
		_, _ = io.Copy(io.Discard, outMaster)
	}()

	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var captured strings.Builder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&captured, errR)
	}()

	done := make(chan int, 1)
	go func() {
		done <- Run([]string{"xbpretty", "install", "-y", "figlet"}, strings.NewReader(""), outSlave, errW)
	}()
	var code int
	select {
	case code = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the PTY run never finished")
	}
	outSlave.Close()
	errW.Close()
	wg.Wait()
	errR.Close()
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, captured.String())
	}
	errs := captured.String()
	if !strings.Contains(errs, "figlet-2.2.5_6: unpacking ...") {
		t.Errorf("child output was not forwarded on stderr:\n%s", errs)
	}
	if strings.Contains(errs, "╭") || strings.Contains(errs, "\r\x1b[2K") {
		t.Errorf("a live surface drew although stderr is a pipe:\n%q", errs)
	}
}

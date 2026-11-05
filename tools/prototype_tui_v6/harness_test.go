package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/soubarnak/xbpretty/internal/term"
)

// ---------- PTY plumbing (pure stdlib, mirrored from internal/cli/pty_linux.go) ----------

type ptyWS struct{ Row, Col, X, Y uint16 }

func openPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open /dev/ptmx: %v", err)
	}
	var ptn uint32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptn))); e != 0 {
		t.Fatalf("TIOCGPTN: %v", e)
	}
	var unlock int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		t.Fatalf("TIOCSPTLCK: %v", e)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", ptn), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	return master, slave
}

func setPtySize(t *testing.T, f *os.File, w, h int) {
	t.Helper()
	ws := ptyWS{Row: uint16(h), Col: uint16(w)}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); e != 0 {
		t.Fatalf("TIOCSWINSZ: %v", e)
	}
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

func protoBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "proto52-bin")
		if err != nil {
			buildErr = err
			return
		}
		builtBin = filepath.Join(dir, "proto52")
		cmd := exec.Command("go", "build", "-o", builtBin, ".")
		cmd.Stderr = os.Stderr
		buildErr = cmd.Run()
	})
	if buildErr != nil {
		t.Fatalf("build prototype: %v", buildErr)
	}
	return builtBin
}

type proc struct {
	t       *testing.T
	cmd     *exec.Cmd
	master  *os.File
	slave   *os.File
	orig    syscall.Termios
	trace   string
	frames  string
	mu      sync.Mutex
	out     bytes.Buffer
	done    chan error
	cleanup []func()
}

func startProto(t *testing.T, surface string, w, h int, extraEnv ...string) *proc {
	t.Helper()
	bin := protoBinary(t)
	master, slave := openPty(t)
	setPtySize(t, slave, w, h)
	orig, err := term.GetTermios(slave)
	if err != nil {
		t.Fatalf("tcgetattr: %v", err)
	}
	dir := t.TempDir()
	p := &proc{t: t, master: master, slave: slave, orig: orig,
		trace: filepath.Join(dir, "trace.log"), frames: filepath.Join(dir, "frames")}
	cmd := exec.Command(bin, "-surface", surface)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"PROTO_TRACE="+p.trace,
		"PROTO_FRAME_DIR="+p.frames,
		"NO_COLOR=",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	p.cmd = cmd
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.out.Write(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	p.done = make(chan error, 1)
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = master.Close()
		_ = slave.Close()
	})
	return p
}

func (p *proc) output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

func (p *proc) send(s string) {
	if _, err := p.master.WriteString(s); err != nil {
		p.t.Fatalf("write keys: %v", err)
	}
}

// answerDSR replies to the first ESC[6n query, proving the reply parser and
// the DSR-anchored path (the default harness leaves the query unanswered).
func (p *proc) answerDSR(reply string) {
	go func() {
		for i := 0; i < 100; i++ {
			if strings.Contains(p.output(), "\x1b[6n") {
				_, _ = p.master.WriteString(reply)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

func (p *proc) resize(w, h int) {
	setPtySize(p.t, p.slave, w, h)
	_ = syscall.Kill(p.cmd.Process.Pid, syscall.SIGWINCH)
}

func (p *proc) waitTrace(substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(p.trace)
		if strings.Contains(string(b), substr) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func (p *proc) traceText() string {
	b, _ := os.ReadFile(p.trace)
	return string(b)
}

func (p *proc) waitExit(timeout time.Duration) (int, error) {
	select {
	case err := <-p.done:
		if err == nil {
			return 0, nil
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return -1, err
	case <-time.After(timeout):
		return -1, fmt.Errorf("timeout waiting for exit")
	}
}

func (p *proc) termios() syscall.Termios {
	t, _ := term.GetTermios(p.slave)
	return t
}

func flagsOf(t syscall.Termios) uint32 {
	return t.Lflag & (syscall.ICANON | syscall.ECHO | syscall.ISIG)
}

func (p *proc) assertRestored() {
	p.t.Helper()
	got := p.termios()
	if flagsOf(got) != flagsOf(p.orig) {
		p.t.Fatalf("termios not restored: orig=%#x got=%#x", flagsOf(p.orig), flagsOf(got))
	}
}

func latestFrame(t *testing.T, dir, surface string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if strings.Contains(n, "-"+surface+"-") && strings.HasSuffix(n, ".txt") && !strings.Contains(n, "nocolor") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Logf("no %s frames in %s: %v", surface, dir, ents)
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, names[len(names)-1]))
	if err != nil || len(b) == 0 {
		t.Logf("frame %q read=%d err=%v", names[len(names)-1], len(b), err)
	}
	return string(b)
}

func waitFrame(t *testing.T, p *proc, surface string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := latestFrame(t, p.frames, surface); s != "" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no frame for %s; trace:\n%s", surface, p.traceText())
	return ""
}

// ---------- golden frame from #50 §2.7 ----------

const expectedSummary80 = `┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  xbpretty Upgrade  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ Upgrade                                                                      ┃
┃ ━━━━━━━                                                                      ┃
┃ ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓┏━┓ ┃
┃ ┃     Package:        Old Version:        New Version:         Size:    ┃┃█┃ ┃
┃ ┃❯    libfoo             1.2_1               1.3_1           120 KiB    ┃┃ ┃ ┃
┃ ┃     libbar             0.9_2               0.9_3            44 KiB    ┃┃ ┃ ┃
┃ ┃     libbaz             2.0_1               2.0_2             8 KiB    ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛┗━┛ ┃
┃ Upgrade:              3 Pkgs           (↑) move up | (↓) move down           ┃
┃                                      (→) next tab | (←) previous tab         ┃
┃ Total download size:12.4 MiB (s) package info | (q) quit | (y) start upgrade ┃
┃ Disk space required:45.0 MiB                                                 ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛`

// ---------- tests ----------

func TestSummaryGolden80x24(t *testing.T) {
	got := renderSummary(goldenUpgrade(), 80, 24, Theme{Color: true}).Text()
	if got != expectedSummary80 {
		gl, wl := strings.Split(got, "\n"), strings.Split(expectedSummary80, "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var a, b string
			if i < len(wl) {
				a = wl[i]
			}
			if i < len(gl) {
				b = gl[i]
			}
			if a != b {
				t.Errorf("row %d:\n want %q\n  got %q", i, a, b)
			}
		}
		t.Fatalf("summary frame differs from #50 §2.7")
	}
}

func TestFetchGolden80x24(t *testing.T) {
	got := strings.Split(renderFetch(&FetchModel{Items: goldenMirrors()}, 80, 24, Theme{Color: true}).Text(), "\n")
	want := []string{
		"╭──────────────────────────────  xbpretty Fetch  ──────────────────────────────╮",
		"│  Mirrors:                             Region: Tier: Latency:    Speed: Score:│",
		"│✓ https://repo.voidlinux.org/current   EU          1    18 ms 4.1 MiB/s     18│",
		"│☐ https://mirror.example.net/voidlinux EU          2    31 ms 3.2 MiB/s     31│",
		"│☐ https://mirror.osuosl.org/voidlinux  NA          2    47 ms 2.8 MiB/s     47│",
		"│☐ https://slow.example.org/voidlinux   AS          3   112 ms 0.9 MiB/s    112│",
	}
	for i, w := range want {
		if i >= len(got) {
			t.Fatalf("missing row %d", i)
		}
		if got[i] != w {
			t.Errorf("row %d:\n want %q\n  got %q", i, w, got[i])
		}
	}
	if !strings.Contains(got[19], "Score is how many milliseconds") {
		t.Errorf("info line row19 = %q", got[19])
	}
	if !strings.Contains(got[21], "Use ↓↑ to move") || !strings.Contains(got[22], "Esc to cancel.") {
		t.Errorf("help lines = %q / %q", got[21], got[22])
	}
}

func TestNoColorHasNoSGR(t *testing.T) {
	b := renderSummary(goldenUpgrade(), 80, 24, Theme{Color: false})
	payload := b.Flush(nil, false)
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if loc := re.FindIndex(payload); loc != nil {
		t.Fatalf("SGR bytes under --color never: %q", payload[loc[0]:loc[1]])
	}
	txt := b.Text()
	for _, glyph := range []string{"┏", "❯", "━", "┃"} {
		if !strings.Contains(txt, glyph) {
			t.Fatalf("glyph %q missing under --color never", glyph)
		}
	}
	// Cursor/clear controls are layout, not colour, and must survive.
	if !bytes.Contains(payload, []byte("\x1b[2J")) {
		t.Fatalf("layout controls missing under --color never")
	}
}

func TestSummaryPTYRestore(t *testing.T) {
	p := startProto(t, "summary", 80, 24)
	if !p.waitTrace("SCREEN draw", 5*time.Second) {
		t.Fatalf("no draw; trace:\n%s", p.traceText())
	}
	if !strings.Contains(p.traceText(), "SCREEN enter surface=summary 80x24") {
		t.Fatalf("entry trace missing")
	}
	// Raw mode disables ISIG while the screen is up.
	if p.termios().Lflag&syscall.ISIG != 0 {
		t.Errorf("ISIG still set in raw mode")
	}
	p.send("q")
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 1 {
		t.Fatalf("summary refuse exit=%d want 1", code)
	}
	out := p.output()
	for _, want := range []string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[?25l", "\x1b[?25h", "\x1b[?1000h", "\x1b[?1000l", "\x1b[?2004h", "\x1b[?2004l"} {
		if !strings.Contains(out, want) {
			t.Errorf("byte stream missing %q", want)
		}
	}
	if !strings.Contains(out, "xbpretty: user refused confirmation") {
		t.Errorf("refusal line missing")
	}
	p.assertRestored()
	if f := latestFrame(t, p.frames, "summary"); f != expectedSummary80 {
		gl, wl := strings.Split(f, "\n"), strings.Split(expectedSummary80, "\n")
		for i := 0; i < len(gl) && i < len(wl); i++ {
			if gl[i] != wl[i] {
				t.Errorf("PTY frame row %d:\n want %q\n  got %q", i, wl[i], gl[i])
				break
			}
		}
		t.Errorf("PTY frame differs: got %d lines, want %d", len(gl), len(wl))
	}
}

func TestKeysPTY(t *testing.T) {
	p := startProto(t, "summary", 80, 24)
	waitFrame(t, p, "summary", 5*time.Second)
	keys := "\x1b[B\x1b[A\x1b[C\x1b[D\x1b[H\x1b[F\x1b[6~\x1b[5~\x1b[<64;10;10M\x1b[200~y\x1b[201~"
	p.send(keys)
	time.Sleep(150 * time.Millisecond)
	p.send("y")
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("confirm exit=%d want 0", code)
	}
	tr := p.traceText()
	for _, want := range []string{"KEY down", "KEY up", "KEY right", "KEY left", "KEY home", "KEY end", "KEY pgdn", "KEY pgup", "KEY wheel-up", "KEY paste-discarded", "KEY rune:y", "SCREEN confirm exit=0"} {
		if !strings.Contains(tr, want) {
			t.Errorf("trace missing %q:\n%s", want, tr)
		}
	}
	// The pasted y must never surface as a rune key.
	if strings.Count(tr, "KEY rune:y") != 1 {
		t.Errorf("pasted y leaked into the key stream:\n%s", tr)
	}
	p.assertRestored()
}

func TestResizePTY(t *testing.T) {
	p := startProto(t, "summary", 80, 24)
	waitFrame(t, p, "summary", 5*time.Second)
	p.resize(100, 30)
	if !p.waitTrace("SCREEN resize 100x30", 5*time.Second) {
		t.Fatalf("no 100x30 resize; trace:\n%s", p.traceText())
	}
	time.Sleep(80 * time.Millisecond)
	if f := latestFrame(t, p.frames, "summary"); !strings.Contains(f, "xbpretty Upgrade") {
		t.Errorf("100x30 frame lost the title:\n%s", f)
	}
	p.resize(50, 15)
	if !p.waitTrace("SCREEN resize 50x15", 5*time.Second) {
		t.Fatalf("no 50x15 resize; trace:\n%s", p.traceText())
	}
	time.Sleep(80 * time.Millisecond)
	small := latestFrame(t, p.frames, "summary")
	if !strings.Contains(small, "Terminal too small (60x18 minimum; now 50x15)") {
		t.Fatalf("too-small notice missing:\n%s", small)
	}
	if strings.Contains(small, "Package:") || strings.Contains(small, "Upgrade:") {
		t.Errorf("below-floor frame still draws the table/footer")
	}
	p.resize(80, 24)
	if !p.waitTrace("SCREEN resize 80x24", 5*time.Second) {
		t.Fatalf("no restore resize; trace:\n%s", p.traceText())
	}
	time.Sleep(80 * time.Millisecond)
	if f := latestFrame(t, p.frames, "summary"); !strings.Contains(f, "xbpretty Upgrade") {
		t.Errorf("frame did not return after growing back:\n%s", f)
	}
	p.send("q")
	if code, _ := p.waitExit(5 * time.Second); code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
	p.assertRestored()
}

func TestFetchPTYConfirm(t *testing.T) {
	p := startProto(t, "fetch", 80, 24)
	frame := waitFrame(t, p, "fetch", 5*time.Second)
	if !strings.Contains(frame, "xbpretty Fetch") || !strings.Contains(frame, "✓") {
		t.Fatalf("fetch frame:\n%s", frame)
	}
	p.send("j")
	time.Sleep(100 * time.Millisecond)
	p.send("q")
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("fetch confirm exit=%d want 0", code)
	}
	if !strings.Contains(p.output(), "Sources have been written") {
		t.Errorf("confirmation line missing")
	}
	p.assertRestored()
}

func TestFetchPTYCancel(t *testing.T) {
	p := startProto(t, "fetch", 80, 24)
	waitFrame(t, p, "fetch", 5*time.Second)
	p.send("\x1b")
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 1 {
		t.Fatalf("fetch cancel exit=%d want 1", code)
	}
	if !strings.Contains(p.output(), "xbpretty: no mirror was selected") {
		t.Errorf("cancel line missing")
	}
	p.assertRestored()
}

func TestHistoryPTY(t *testing.T) {
	p := startProto(t, "history", 80, 24)
	frame := waitFrame(t, p, "history", 5*time.Second)
	if !strings.Contains(frame, "xbpretty History") {
		t.Fatalf("history frame:\n%s", frame)
	}
	if strings.Contains(frame, "Total download size") {
		t.Errorf("history footer must not show download size")
	}
	p.send("q")
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("history quit exit=%d want 0", code)
	}
	p.assertRestored()
}

func TestInlineViewportPTY(t *testing.T) {
	p := startProto(t, "inline", 80, 24)
	if !p.waitTrace("INLINE dsr=timeout fallback=no-anchor", 3*time.Second) {
		t.Fatalf("DSR fallback not logged; trace:\n%s", p.traceText())
	}
	if !p.waitTrace("INLINE insert", 5*time.Second) {
		t.Fatalf("no message insertion; trace:\n%s", p.traceText())
	}
	if !p.waitTrace("INLINE hide", 5*time.Second) {
		t.Fatalf("no hide latch; trace:\n%s", p.traceText())
	}
	code, err := p.waitExit(8 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 0 {
		t.Fatalf("inline exit=%d want 0", code)
	}
	out := p.output()
	if strings.Contains(out, "\x1b[?1049h") {
		t.Errorf("inline viewport entered the alternate screen")
	}
	if strings.Count(out, "Last completed:") < 3 {
		t.Errorf("completed lines were not inserted into scrollback")
	}
	if !strings.Contains(out, "\x1b[?25h") {
		t.Errorf("cursor was not shown on teardown")
	}
	p.assertRestored()
	seq, _ := os.ReadFile(filepath.Join(p.frames, "inline-sequence.txt"))
	if strings.Count(string(seq), "INSERT") < 3 {
		t.Errorf("inline sequence log:\n%s", seq)
	}
}

func TestInlineDSRReply(t *testing.T) {
	p := startProto(t, "inline", 80, 24)
	p.answerDSR("\x1b[12;5R")
	if !p.waitTrace("INLINE dsr=reply 12;5", 3*time.Second) {
		t.Fatalf("DSR reply not parsed; trace:\n%s", p.traceText())
	}
	if !p.waitTrace("INLINE complete", 8*time.Second) {
		t.Fatalf("inline did not complete; trace:\n%s", p.traceText())
	}
	if code, err := p.waitExit(8 * time.Second); err != nil || code != 0 {
		t.Fatalf("exit=%d err=%v", code, err)
	}
	p.assertRestored()
}

func TestInlineCtrlCIsSignal(t *testing.T) {
	p := startProto(t, "inline", 80, 24)
	if !p.waitTrace("INLINE draw", 5*time.Second) {
		t.Fatalf("no first draw; trace:\n%s", p.traceText())
	}
	time.Sleep(120 * time.Millisecond)
	p.send("\x03")
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 130 {
		t.Fatalf("Ctrl-C in inline viewport exit=%d want 130; trace:\n%s", code, p.traceText())
	}
	if !strings.Contains(p.traceText(), "INLINE signal=interrupt exit=130") {
		t.Errorf("signal handler not reached; trace:\n%s", p.traceText())
	}
	if !strings.Contains(p.output(), "\x1b[?25h") {
		t.Errorf("cursor not restored on Ctrl-C")
	}
	p.assertRestored()
}

func TestPanicRestore(t *testing.T) {
	p := startProto(t, "summary", 80, 24, "PROTO_PANIC_AFTER=350ms")
	waitFrame(t, p, "summary", 5*time.Second)
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 2 {
		t.Fatalf("panic exit=%d want 2", code)
	}
	for _, want := range []string{"\x1b[?1049l", "\x1b[?25h"} {
		if !strings.Contains(p.output(), want) {
			t.Errorf("panic stream missing %q", want)
		}
	}
	p.assertRestored()
	if !strings.Contains(p.traceText(), "SCREEN panic=") {
		t.Errorf("panic restore not traced")
	}
}

func TestSigtermRestore(t *testing.T) {
	p := startProto(t, "summary", 80, 24)
	waitFrame(t, p, "summary", 5*time.Second)
	_ = syscall.Kill(p.cmd.Process.Pid, syscall.SIGTERM)
	code, err := p.waitExit(5 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 143 {
		t.Fatalf("SIGTERM exit=%d want 143", code)
	}
	if !strings.Contains(p.output(), "\x1b[?1049l") || !strings.Contains(p.output(), "\x1b[?25h") {
		t.Errorf("SIGTERM did not restore the screen")
	}
	p.assertRestored()
}

func TestCaptureFixtures(t *testing.T) {
	if os.Getenv("PROTO_CAPTURE") == "" {
		t.Skip("set PROTO_CAPTURE=1 to refresh committed captures")
	}
	dir := "../../testdata/prototype/tui"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// PTY-captured summary.
	p := startProto(t, "summary", 80, 24)
	waitFrame(t, p, "summary", 5*time.Second)
	time.Sleep(60 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, "pty-summary-80x24.txt"), []byte(latestFrame(t, p.frames, "summary")), 0o644)
	p.send("q")
	_, _ = p.waitExit(5 * time.Second)

	// Too-small notice via a live resize.
	p = startProto(t, "summary", 80, 24)
	waitFrame(t, p, "summary", 5*time.Second)
	p.resize(50, 15)
	if !p.waitTrace("SCREEN resize 50x15", 5*time.Second) {
		t.Fatalf("resize missing")
	}
	time.Sleep(100 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, "pty-toolarge-50x15.txt"), []byte(latestFrame(t, p.frames, "summary")), 0o644)
	p.send("q")
	_, _ = p.waitExit(5 * time.Second)

	// Inline sequence.
	p = startProto(t, "inline", 80, 24)
	if !p.waitTrace("INLINE complete", 8*time.Second) {
		t.Fatalf("inline did not complete; trace:\n%s", p.traceText())
	}
	_, _ = p.waitExit(8 * time.Second)
	for _, name := range []string{"inline-sequence.txt"} {
		b, err := os.ReadFile(filepath.Join(p.frames, name))
		if err == nil {
			_ = os.WriteFile(filepath.Join(dir, "pty-"+name), b, 0o644)
		}
	}
	ents, _ := os.ReadDir(p.frames)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "001-inline-") {
			b, _ := os.ReadFile(filepath.Join(p.frames, e.Name()))
			_ = os.WriteFile(filepath.Join(dir, "pty-inline-box.txt"), b, 0o644)
		}
	}
}

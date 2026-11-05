//go:build linux

package term

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPty allocates a master/slave pseudo-terminal with the standard library.
func openPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	var ptn uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptn))); errno != 0 {
		master.Close()
		t.Skipf("TIOCGPTN: %v", errno)
	}
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		t.Skipf("TIOCSPTLCK: %v", errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", ptn), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Skipf("open pty slave: %v", err)
	}
	t.Cleanup(func() {
		slave.Close()
		master.Close()
	})
	return master, slave
}

func setPtySize(t *testing.T, f *os.File, w, h int) {
	t.Helper()
	if err := SetSize(f, w, h); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
}

// readUntil reads from f until want appears or the timeout expires.
func readUntil(master *os.File, want string, timeout time.Duration) (string, error) {
	type chunk struct {
		data string
		err  error
	}
	ch := make(chan chunk, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 256)
		for {
			n, err := master.Read(buf)
			b.Write(buf[:n])
			if strings.Contains(b.String(), want) {
				ch <- chunk{data: b.String()}
				return
			}
			if err != nil {
				ch <- chunk{data: b.String(), err: err}
				return
			}
		}
	}()
	select {
	case c := <-ch:
		return c.data, c.err
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for %q", want)
	}
}

// TestGuardRestoresTermiosAndScreen is AC-054's engine half: entry changes the
// termios, output is ordered, and the restore returns the exact flags.
func TestGuardRestoresTermiosAndScreen(t *testing.T) {
	master, slave := openPty(t)
	setPtySize(t, slave, 80, 24)

	before, err := GetState(slave)
	if err != nil {
		t.Fatalf("GetState before: %v", err)
	}
	if !FileIsTerminal(slave) {
		t.Fatal("pty slave is not a terminal")
	}

	g := NewGuard(slave, slave)
	if err := g.EnterFullscreen(true); err != nil {
		t.Fatalf("EnterFullscreen: %v", err)
	}
	during, err := GetState(slave)
	if err != nil {
		t.Fatalf("GetState during: %v", err)
	}
	if during.Equal(before) {
		t.Error("raw mode did not change the termios")
	}
	if during.t.Lflag&syscall.ISIG != 0 {
		t.Error("raw mode left ISIG on")
	}
	if during.t.Lflag&syscall.ICANON != 0 || during.t.Lflag&syscall.ECHO != 0 {
		t.Errorf("raw Lflag = %#x, want ICANON and ECHO off", during.t.Lflag)
	}
	if !g.Active() {
		t.Error("guard is not active after EnterFullscreen")
	}

	g.Restore()
	after, err := GetState(slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(before) {
		t.Errorf("termios flags changed: before %+v after %+v", before.t, after.t)
	}
	if g.Active() {
		t.Error("guard is still active after Restore")
	}

	out, err := readUntil(master, CursorShow, 5*time.Second)
	if err != nil {
		t.Fatalf("reading the guard bytes: %v\n%s", err, out)
	}
	order := []string{CursorHide, AltEnter, PasteOn, MouseOn, MouseOff, PasteOff, AltLeave, CursorShow}
	pos := 0
	for _, seq := range order {
		i := strings.Index(out[pos:], seq)
		if i < 0 {
			t.Fatalf("guard bytes missing %q in order\n%q", seq, out)
		}
		pos += i + len(seq)
	}
}

// TestGuardRestoresAfterPanic is AC-054's panic path: a recover-then-restore
// defer returns the termios to its original flags.
func TestGuardRestoresAfterPanic(t *testing.T) {
	_, slave := openPty(t)
	before, err := GetState(slave)
	if err != nil {
		t.Fatalf("GetState before: %v", err)
	}
	func() {
		g := NewGuard(slave, slave)
		defer g.Restore()
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		if err := g.EnterFullscreen(true); err != nil {
			t.Fatalf("EnterFullscreen: %v", err)
		}
		panic("boom")
	}()
	after, err := GetState(slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(before) {
		t.Errorf("termios not restored after panic: before %+v after %+v", before.t, after.t)
	}
}

// TestGuardRestoresOnSecondCall proves Restore is idempotent; a signal path
// and a deferred path may both call it.
func TestGuardRestoresOnSecondCall(t *testing.T) {
	_, slave := openPty(t)
	before, _ := GetState(slave)
	g := NewGuard(slave, slave)
	if err := g.EnterFullscreen(false); err != nil {
		t.Fatalf("EnterFullscreen: %v", err)
	}
	g.Restore()
	g.Restore()
	after, _ := GetState(slave)
	if !after.Equal(before) {
		t.Errorf("second Restore changed the termios: before %+v after %+v", before.t, after.t)
	}
}

// TestSize proves the winsize round trip the PTY harness relies on.
func TestSize(t *testing.T) {
	_, slave := openPty(t)
	setPtySize(t, slave, 100, 30)
	w, h, err := Size(slave)
	if err != nil || w != 100 || h != 30 {
		t.Fatalf("Size = %d,%d,%v want 100,30", w, h, err)
	}
}

// TestQueryCursorTimeoutAndReply is AC-055's DSR-optional behaviour: silence
// times out (the no-anchor fallback), and a reply parses.
func TestQueryCursorTimeoutAndReply(t *testing.T) {
	master, slave := openPty(t)

	res := QueryCursor(slave, slave, 50*time.Millisecond)
	if res.OK || !res.TimedOut {
		t.Fatalf("DSR-silent result = %+v, want timeout", res)
	}
	if out, _ := readUntil(master, DSRQuery, 2*time.Second); !strings.Contains(out, DSRQuery) {
		t.Fatalf("query bytes = %q", out)
	}

	// A DSR reply must not be echoed or line-buffered: put the slave in raw
	// mode before writing the response into the master.
	saved, err := GetState(slave)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if err := SetState(slave, rawState(saved)); err != nil {
		t.Fatalf("SetState raw: %v", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = master.WriteString("\x1b[12;5R")
	}()
	res = QueryCursor(slave, slave, time.Second)
	if !res.OK || res.Row != 12 || res.Col != 5 {
		t.Fatalf("DSR reply = %+v, want 12;5", res)
	}
}

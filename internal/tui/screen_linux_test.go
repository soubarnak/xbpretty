package tui

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/soubarnak/xbpretty/internal/term"
)

func openTestPty(t *testing.T) (*os.File, *os.File) {
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

func waitEvent(t *testing.T, s *Screen, kind EventKind) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %d", kind)
		}
	}
}

// TestScreenRestoresAndDecodes is AC-054's session half: key decoding through
// a real tty, SIGWINCH delivering a resize, and Close restoring the termios.
func TestScreenRestoresAndDecodes(t *testing.T) {
	master, slave := openTestPty(t)
	if err := term.SetSize(slave, 80, 24); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	s, err := Open(slave, slave, true, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if w, h := s.Size(); w != 80 || h != 24 {
		t.Errorf("Size = %dx%d, want 80x24", w, h)
	}
	if err := s.Draw(RenderSummary(goldenUpgrade(), 80, 24, DefaultTheme())); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	if err := s.Draw(RenderSummary(goldenUpgrade(), 80, 24, DefaultTheme())); err != nil {
		t.Fatalf("second Draw: %v", err)
	}

	if _, err := master.WriteString("j"); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, s, EventKey)
	if ev.Key.Type != term.KeyRune || ev.Key.Rune != 'j' {
		t.Fatalf("decoded %+v, want rune j", ev.Key)
	}

	if err := term.SetSize(slave, 50, 15); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	ev = waitEvent(t, s, EventResize)
	if ev.Width != 50 || ev.Height != 15 {
		t.Fatalf("resize event = %dx%d, want 50x15", ev.Width, ev.Height)
	}

	s.Close()
	after, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(before) {
		t.Errorf("termios not restored after Screen.Close")
	}
}

// TestScreenRestoresOnPanic proves Run restores before re-raising.
func TestScreenRestoresOnPanic(t *testing.T) {
	_, slave := openTestPty(t)
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		s, err := Open(slave, slave, false, false)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		_ = s.Run(func(*Screen) error {
			panic("screen panic")
		})
	}()
	after, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(before) {
		t.Errorf("termios not restored after a screen panic")
	}
}

// TestScreenCloseStopsDecoder proves Close joins the input goroutine: after
// it returns, a key written to the terminal is still there for the next
// reader, and a second screen can be opened on the same tty.
func TestScreenCloseStopsDecoder(t *testing.T) {
	master, slave := openTestPty(t)
	s, err := Open(slave, slave, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	if _, err := master.WriteString("j\n"); err != nil {
		t.Fatalf("writing the probe key: %v", err)
	}
	type result struct {
		b   []byte
		err error
	}
	read := make(chan result, 1)
	go func() {
		buf := make([]byte, 8)
		n, err := slave.Read(buf)
		read <- result{buf[:n], err}
	}()
	select {
	case r := <-read:
		if !strings.Contains(string(r.b), "j") {
			t.Fatalf("read %q, want the probe key", r.b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a closed screen's decoder consumed stdin")
	}

	second, err := Open(slave, slave, false, false)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if _, err := master.WriteString("k"); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, second, EventKey)
	if ev.Key.Type != term.KeyRune || ev.Key.Rune != 'k' {
		t.Fatalf("second screen decoded %+v, want rune k", ev.Key)
	}
	second.Close()
}

// TestScreenConcurrentClose stresses the done channel and the decoder join
// under -race.
func TestScreenConcurrentClose(t *testing.T) {
	master, slave := openTestPty(t)
	s, err := Open(slave, slave, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	go func() {
		for i := 0; i < 20; i++ {
			_, _ = master.WriteString("j")
		}
	}()
	s.Close()
}

// TestLocateAnchorKeepsStray proves the DSR handshake replays bytes typed
// during the query window instead of dropping them.
func TestLocateAnchorKeepsStray(t *testing.T) {
	master, slave := openTestPty(t)
	// Raw mode keeps the reply and the stray key out of the canonical line
	// buffer.
	g := term.NewGuard(slave, slave)
	if err := g.EnterFullscreen(false); err != nil {
		t.Fatalf("raw mode: %v", err)
	}
	defer g.Restore()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = master.WriteString("j\x1b[12;5R")
	}()
	a := LocateAnchor(slave, slave, time.Second)
	if !a.OK || a.Row != 12 || a.Col != 5 {
		t.Fatalf("anchor = %+v, want 12;5", a)
	}
	if string(a.Stray) != "j" {
		t.Errorf("Stray = %q, want the typed byte", a.Stray)
	}
}

// TestNoAnchorFallback is AC-055's DSR half: a silent terminal times out and
// the inline surface starts on a fresh line instead of an absolute anchor.
func TestNoAnchorFallback(t *testing.T) {
	a := LocateAnchor(nil, nil, 10*time.Millisecond)
	if a.OK {
		t.Fatalf("DSR on nil files reported an anchor: %+v", a)
	}
	if got := a.BeginSequence(); got != term.NoAnchor {
		t.Errorf("no-anchor BeginSequence = %q, want %q", got, term.NoAnchor)
	}
	if got := (Anchor{Row: 3, Col: 1, OK: true}).BeginSequence(); got != "" {
		t.Errorf("anchored BeginSequence = %q, want empty", got)
	}
}

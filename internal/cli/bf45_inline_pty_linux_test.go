//go:build linux

package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/term"
)

// bf45Drain copies everything the master reads into c until the pty closes.
func bf45Drain(master *os.File, c *capture) {
	buf := make([]byte, 4096)
	for {
		n, err := master.Read(buf)
		if n > 0 {
			c.add(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// bf45WaitFor polls the capture until want appears or the deadline passes.
func bf45WaitFor(t *testing.T, c *capture, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.String(); strings.Contains(s, want) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; capture:\n%q", want, c.String())
	return ""
}

// bf45Pty is a real terminal with a drained master, so the inline surface's
// bytes can be inspected in the order the terminal would receive them.
type bf45Pty struct {
	master, slave *os.File
	cap           *capture
	before        term.State
}

func newBF45Pty(t *testing.T) *bf45Pty {
	t.Helper()
	terminalize(t)
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	setProgressPtySize(t, slave, 80, 24)
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState before: %v", err)
	}
	p := &bf45Pty{master: master, slave: slave, cap: &capture{}, before: before}
	go bf45Drain(master, p.cap)
	return p
}

// settle writes an end marker after the surface's bytes and waits for it, so
// every byte the surface wrote is in the capture before any assertion.
func (p *bf45Pty) settle(t *testing.T) string {
	t.Helper()
	const marker = "\x01BF45END"
	if _, err := p.slave.WriteString(marker); err != nil {
		t.Fatalf("writing the end marker: %v", err)
	}
	return bf45WaitFor(t, p.cap, marker)
}

// assertTermiosRestored compares the slave's flags with the ones captured
// before the surface started.
func (p *bf45Pty) assertTermiosRestored(t *testing.T) {
	t.Helper()
	after, err := term.GetState(p.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(p.before) {
		t.Errorf("termios flags not restored after the inline surface closed")
	}
}

// assertCursorBracketsTheBox checks the BF-45 order on a real terminal: the
// cursor is hidden before the first box, and the last cursor toggle leaves it
// shown. Teardown shows the cursor before its erase, and a guard restore shows
// it again after, so the final show is the one that counts.
func assertCursorBracketsTheBox(t *testing.T, s string) {
	t.Helper()
	hideAt := strings.Index(s, term.CursorHide)
	boxAt := strings.Index(s, "╭")
	if hideAt < 0 || boxAt < 0 || hideAt > boxAt {
		t.Errorf("cursor not hidden before the first box (hide=%d box=%d):\n%q", hideAt, boxAt, s)
	}
	lastHide := strings.LastIndex(s, term.CursorHide)
	lastShow := strings.LastIndex(s, term.CursorShow)
	if lastShow < 0 || lastShow < lastHide {
		t.Errorf("cursor not left shown (hide=%d show=%d):\n%q", lastHide, lastShow, s)
	}
}

// TestBF45InlineSurfaceHidesAndShowsTheCursor is BF-45 on a real terminal:
// starting the inline surface hides the cursor before the box, and closing it
// shows the cursor again after the box is erased, with termios restored.
func TestBF45InlineSurfaceHidesAndShowsTheCursor(t *testing.T) {
	p := newBF45Pty(t)
	st := &state{stdin: p.slave, stderr: p.slave}
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(p.slave, false, 80, liveWidgetMode, model, summary.DefaultTheme())

	inline := st.startInline(live)
	if inline == nil {
		t.Fatal("startInline built no viewport on a terminal")
	}
	live.Request(true)
	bf45WaitFor(t, p.cap, "╭")
	inline.Close()

	assertCursorBracketsTheBox(t, p.settle(t))
	p.assertTermiosRestored(t)
}

// TestBF45EarlyReturnShowsTheCursor covers a surface that is closed before any
// frame is drawn, as an early return does: the cursor must still come back.
func TestBF45EarlyReturnShowsTheCursor(t *testing.T) {
	p := newBF45Pty(t)
	st := &state{stdin: p.slave, stderr: p.slave}
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(p.slave, false, 80, liveWidgetMode, model, summary.DefaultTheme())

	inline := st.startInline(live)
	if inline == nil {
		t.Fatal("startInline built no viewport on a terminal")
	}
	inline.Close()

	s := p.settle(t)
	hideAt := strings.Index(s, term.CursorHide)
	showAt := strings.LastIndex(s, term.CursorShow)
	if hideAt < 0 || showAt < hideAt {
		t.Fatalf("early return did not hide then show the cursor (hide=%d show=%d):\n%q", hideAt, showAt, s)
	}
	p.assertTermiosRestored(t)
}

// TestBF45PredownloadToCommitTranscript runs a full install on a real terminal:
// the predownload surface hands over to the commit surface. The cursor brackets
// the whole run, and the handoff writes no blank run (BF-45 part 2's transcript
// property): between the predownload's last box and the commit box there is at
// most one newline, the no-anchor break.
func TestBF45PredownloadToCommitTranscript(t *testing.T) {
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

	r := startInlinePty(t, []string{"xbpretty", "install", "figlet", "-y"}, 80, 24)
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, r.cap.String())
	}
	s := r.cap.String()
	assertCursorBracketsTheBox(t, s)

	commitAt := strings.Index(s, "Installing Packages")
	if commitAt < 0 {
		t.Fatalf("the commit surface never drew:\n%q", s)
	}
	commitBox := strings.LastIndex(s[:commitAt], "╭")
	predownloadEnd := strings.LastIndex(s[:commitBox], "╯")
	if predownloadEnd < 0 {
		t.Fatalf("the predownload box was not drawn before the commit box:\n%q", s)
	}
	if handoff := s[predownloadEnd:commitBox]; strings.Count(handoff, "\n") > 1 {
		t.Errorf("predownload to commit wrote %d newlines, want at most the no-anchor break:\n%q",
			strings.Count(handoff, "\n"), handoff)
	}
}

// TestBF45RenderPanicShowsTheCursor covers the panic path on a real terminal:
// a render panic unwinds through the surface's deferred Close, which must show
// the cursor and restore termios before the panic reaches the caller.
func TestBF45RenderPanicShowsTheCursor(t *testing.T) {
	p := newBF45Pty(t)
	st := &state{stdin: p.slave, stderr: p.slave}
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(p.slave, false, 80, liveWidgetMode, model, summary.DefaultTheme())

	inline := st.startInline(live)
	if inline == nil {
		t.Fatal("startInline built no viewport on a terminal")
	}
	live.Request(true)
	bf45WaitFor(t, p.cap, "╭")

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the render panic was swallowed")
			}
		}()
		defer inline.Close()
		live.Apply(true, func(*progressModel) { panic("render boom") })
	}()

	assertCursorBracketsTheBox(t, p.settle(t))
	p.assertTermiosRestored(t)
}

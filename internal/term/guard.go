//go:build linux

package term

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// Guard owns one terminal mode change and restores it exactly once on every
// exit path (#50 §8.7). It is the piece that must never leave a broken tty.
type Guard struct {
	In  *os.File
	Out *os.File

	mu       sync.Mutex
	saved    syscall.Termios
	haveSave bool
	mode     string // "", "raw", "cbreak"
	alt      bool
	mouse    bool
	paste    bool
	cursorHi bool
	dead     bool
}

// NewGuard creates a guard for an input/output pair.
func NewGuard(in, out *os.File) *Guard { return &Guard{In: in, Out: out} }

func (g *Guard) write(s string) {
	if g.dead || g.Out == nil {
		return
	}
	if _, err := g.Out.WriteString(s); err != nil {
		g.dead = true
	}
}

// EnterFullscreen is raw mode + alternate screen + hidden cursor + mouse/paste
// toggles, in nala's order. Rollback mirrors nala's explicit unwinding.
func (g *Guard) EnterFullscreen(mouse bool) (err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	saved, err := GetTermios(g.In)
	if err != nil {
		return fmt.Errorf("tcgetattr: %w", err)
	}
	g.saved, g.haveSave = saved, true
	if err := SetTermios(g.In, rawTermios(saved)); err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	g.mode = "raw"
	// From here a failure must roll back what already happened.
	rollback := func() {
		if g.alt {
			g.write(AltLeave)
			g.alt = false
		}
		g.cursorHi = false
		g.write(CursorShow)
		g.mouse = false
		g.paste = false
		_ = SetTermios(g.In, g.saved)
		g.mode = ""
	}
	g.cursorHi = true
	g.write(CursorHide)
	g.write(AltEnter)
	g.alt = true
	g.write(PasteOn)
	g.paste = true
	if mouse {
		g.write(MouseOn)
		g.mouse = true
	}
	if g.dead {
		rollback()
		return fmt.Errorf("terminal write failed")
	}
	return nil
}

// EnterInline is cbreak only: no alternate screen, ISIG and OPOST kept (#7.3).
func (g *Guard) EnterInline() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	saved, err := GetTermios(g.In)
	if err != nil {
		return fmt.Errorf("tcgetattr: %w", err)
	}
	g.saved, g.haveSave = saved, true
	if err := SetTermios(g.In, cbreakTermios(saved)); err != nil {
		return fmt.Errorf("cbreak: %w", err)
	}
	g.mode = "cbreak"
	return nil
}

// RestoreTermios restores the saved mode without touching the screen.
func (g *Guard) RestoreTermios() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.restoreTermiosLocked()
}

func (g *Guard) restoreTermiosLocked() {
	if g.haveSave && g.mode != "" {
		_ = SetTermios(g.In, g.saved)
		g.mode = ""
	}
}

// Restore unwinds in the reverse order of entry and is idempotent.
func (g *Guard) Restore() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dead {
		g.restoreTermiosLocked()
		return
	}
	if g.mouse {
		g.write(MouseOff)
		g.mouse = false
	}
	if g.paste {
		g.write(PasteOff)
		g.paste = false
	}
	if g.alt {
		g.write(AltLeave)
		g.alt = false
	}
	if g.cursorHi {
		g.write(CursorShow)
		g.cursorHi = false
	}
	g.restoreTermiosLocked()
	g.write("\r")
}

// RestoreAndReport is Restore plus an optional one-line diagnostic on stderr.
func (g *Guard) RestoreAndReport(msg string) {
	g.Restore()
	if msg != "" {
		_, _ = os.Stderr.WriteString(msg)
	}
}

// Active reports whether any terminal state is currently held.
func (g *Guard) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.mode != "" || g.alt || g.mouse || g.paste
}

// DSRResult is the outcome of a cursor-position query.
type DSRResult struct {
	Row, Col int
	Stray    []byte
	OK       bool
	TimedOut bool
}

// QueryCursor sends ESC[6n and waits up to timeout for ESC[r;cR. This is the
// optional anchor optimisation: a timeout is expected in dumb/test terminals
// and must fall back to the no-anchor strategy (#50 §8.3).
func QueryCursor(in, out *os.File, timeout time.Duration) DSRResult {
	res := DSRResult{}
	if _, err := out.WriteString(DSRQuery); err != nil {
		res.TimedOut = true
		return res
	}
	deadline := time.Now().Add(timeout)
	var buf []byte
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			res.TimedOut = true
			res.Stray = buf
			return res
		}
		ready, err := Select(in.Fd(), int(remain/time.Millisecond)+1)
		if err != nil || !ready {
			res.TimedOut = true
			res.Stray = buf
			return res
		}
		chunk, _ := ReadAvailable(in.Fd())
		if len(chunk) == 0 {
			res.TimedOut = true
			res.Stray = buf
			return res
		}
		buf = append(buf, chunk...)
		if row, col, n, ok := parseDSR(buf); ok {
			res.Row, res.Col, res.OK = row, col, true
			res.Stray = append([]byte(nil), buf[n:]...)
			return res
		}
		if len(buf) > 64 {
			res.TimedOut = true
			res.Stray = buf
			return res
		}
	}
}

func parseDSR(b []byte) (row, col, consumed int, ok bool) {
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b || i+1 >= len(b) || b[i+1] != '[' {
			continue
		}
		j := i + 2
		r, seen := 0, false
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			r = r*10 + int(b[j]-'0')
			j++
			seen = true
		}
		if !seen || j >= len(b) || b[j] != ';' {
			continue
		}
		j++
		c, seen := 0, false
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			c = c*10 + int(b[j]-'0')
			j++
			seen = true
		}
		if !seen || j >= len(b) || b[j] != 'R' {
			continue
		}
		return r, c, j + 1, true
	}
	return 0, 0, 0, false
}

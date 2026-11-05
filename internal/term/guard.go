package term

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Guard owns one terminal mode change and restores it exactly once on every
// exit path (ADR-0017 §5). It is the piece that must never leave a broken tty:
// the surface runner defers Restore, the signal path calls Restore before it
// returns, and the panic path recovers, restores and re-panics.
type Guard struct {
	In  *os.File
	Out *os.File

	mu       sync.Mutex
	saved    State
	haveSave bool
	mode     string // "" | "raw" | "cbreak"
	alt      bool
	mouse    bool
	paste    bool
	cursorHi bool
	dead     bool
}

// NewGuard creates a guard for an input/output pair.
func NewGuard(in, out *os.File) *Guard { return &Guard{In: in, Out: out} }

func (g *Guard) write(s string) {
	if g.Out == nil {
		return
	}
	// Never short-circuit on a previous failure: a restore is best-effort and
	// must attempt every sequence even after an earlier write errored.
	if _, err := g.Out.WriteString(s); err != nil {
		g.dead = true
	}
}

// EnterFullscreen is raw mode + hidden cursor + alternate screen + paste and,
// optionally, mouse toggles. A failure after raw mode is rolled back before it
// returns, so a failed entry leaves the terminal as it was.
func (g *Guard) EnterFullscreen(mouse bool) (err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	saved, err := GetState(g.In)
	if err != nil {
		return fmt.Errorf("tcgetattr: %w", err)
	}
	g.saved, g.haveSave = saved, true
	if err := SetState(g.In, rawState(saved)); err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	g.mode = "raw"
	rollback := func() {
		if g.alt {
			g.write(AltLeave)
			g.alt = false
		}
		if g.cursorHi {
			g.write(CursorShow)
			g.cursorHi = false
		}
		if g.paste {
			g.write(PasteOff)
			g.paste = false
		}
		if g.mouse {
			g.write(MouseOff)
			g.mouse = false
		}
		_ = SetState(g.In, g.saved)
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

// EnterInline is cbreak only: no alternate screen, ISIG and OPOST kept. It is
// the engine seam for the inline progress viewport (#79, FR-072).
func (g *Guard) EnterInline() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	saved, err := GetState(g.In)
	if err != nil {
		return fmt.Errorf("tcgetattr: %w", err)
	}
	g.saved, g.haveSave = saved, true
	if err := SetState(g.In, cbreakState(saved)); err != nil {
		return fmt.Errorf("cbreak: %w", err)
	}
	g.mode = "cbreak"
	return nil
}

// SetMouse turns SGR mouse reporting on or off and tracks the change so
// Restore unwinds it in order. The full-screen summary uses it to drop mouse
// reporting while the terminal is below the 60x18 floor (FR-071).
func (g *Guard) SetMouse(on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if on == g.mouse {
		return
	}
	if on {
		g.write(MouseOn)
	} else {
		g.write(MouseOff)
	}
	g.mouse = on
}

// RestoreTermios restores the saved mode without touching the screen.
func (g *Guard) RestoreTermios() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.restoreTermiosLocked()
}

func (g *Guard) restoreTermiosLocked() {
	if g.haveSave && g.mode != "" {
		_ = SetState(g.In, g.saved)
		g.mode = ""
	}
}

// Restore unwinds in the exact reverse order of entry and is idempotent. The
// screen sequences are still written when the output failed mid-entry.
func (g *Guard) Restore() {
	g.mu.Lock()
	defer g.mu.Unlock()
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

// QueryCursor sends ESC[6n and waits up to timeout for ESC[r;cR. A timeout is
// expected in dumb terminals and test harnesses; callers then use the
// no-anchor fallback (ADR-0017 §5, AC-055).
func QueryCursor(in, out *os.File, timeout time.Duration) DSRResult {
	res := DSRResult{}
	if in == nil || out == nil {
		res.TimedOut = true
		return res
	}
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
		ready, err := waitReadable(in.Fd(), int(remain/time.Millisecond)+1)
		if err != nil || !ready {
			res.TimedOut = true
			res.Stray = buf
			return res
		}
		var chunk [256]byte
		n, readErr := readFD(in, chunk[:])
		if n <= 0 {
			res.TimedOut = true
			res.Stray = buf
			_ = readErr
			return res
		}
		buf = append(buf, chunk[:n]...)
		if row, col, start, end, ok := parseDSRLocated(buf); ok {
			res.Row, res.Col, res.OK = row, col, true
			// Preserve every non-reply byte: keys typed before the reply and
			// anything trailing it must reach the decoder, not be dropped.
			res.Stray = append([]byte(nil), buf[:start]...)
			res.Stray = append(res.Stray, buf[end:]...)
			return res
		}
		if len(buf) > 64 {
			res.TimedOut = true
			res.Stray = buf
			return res
		}
	}
}

// ParseDSR finds ESC[row;colR in b and returns the consumed length. Callers
// that read the stream themselves must preserve the prefix too.
func ParseDSR(b []byte) (row, col, consumed int, ok bool) {
	row, col, _, consumed, ok = parseDSRLocated(b)
	return row, col, consumed, ok
}

func parseDSR(b []byte) (row, col, consumed int, ok bool) { return ParseDSR(b) }

// parseDSRLocated is ParseDSR plus the reply's start index, so the caller can
// keep both the prefix and the suffix as stray bytes.
func parseDSRLocated(b []byte) (row, col, start, end int, ok bool) {
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
		return r, c, i, j + 1, true
	}
	return 0, 0, 0, 0, false
}

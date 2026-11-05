package cli

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// inlineSurface is the terminal state around the inline progress viewport
// (FR-072): cbreak while xbpretty owns a transfer, the bottom box on the
// normal screen, the width watcher, and the ordered restore on every exit
// path. It never enters an alternate screen and never reads a key.
//
// The viewport deliberately does not use the optional DSR anchor handshake:
// the query has to read stdin while the terminal is in cbreak, which would
// swallow keys queued for a child that has not started yet. FR-072 makes DSR
// optional, so the inline path always takes its `\r\n` no-anchor fallback.
type inlineSurface struct {
	guard *term.Guard
	vp    *tui.Viewport
	live  *liveProgress

	winch     chan os.Signal
	stopWinch chan struct{}
	stopOnce  sync.Once
}

// inlineEnteredFn is a test-only seam run after cbreak is entered and before
// the caller's child exists. Tests use it to deliver SIGINT inside the window
// where the terminal has changed but no child has been spawned yet.
var inlineEnteredFn = func() {}

// startInline builds the viewport before any child that may read stdin. When
// stdin is a terminal it enters cbreak (ISIG on, so Ctrl-C stays a real
// signal) and starts the width watcher. It returns nil when the plain line is
// in use or the streams are not files.
func (st *state) startInline(live *liveProgress) *inlineSurface {
	if live == nil || live.mode != liveWidgetMode {
		return nil
	}
	out, ok := asFile(st.stderr)
	if !ok {
		return nil
	}
	vp := tui.NewViewport(out, terminalWidth(st.stderr))
	s := &inlineSurface{vp: vp, live: live}
	if in, ok := asFile(st.stdin); ok && isTTY(st.stdin) {
		s.guard = term.NewGuard(in, out)
		if err := s.guard.EnterInline(); err != nil {
			// A tty that cannot be switched to cbreak still gets the box;
			// only input echo and the alt-screen latch's termios half are lost.
			s.guard = nil
		} else {
			inlineEnteredFn()
		}
	}
	vp.Start(tui.Anchor{})
	live.SetInline(vp, s.guard, s.guard != nil)
	s.watchWidth(func() int { return terminalWidth(st.stderr) })
	return s
}

// watchWidth owns SIGWINCH for as long as the viewport lives, so a resize
// reflows the box during xbpretty's own transfers too, not only while a child
// is watched by the stream engine (FR-072, AC-055). The height stays fixed.
func (s *inlineSurface) watchWidth(width func() int) {
	s.winch = make(chan os.Signal, 1)
	s.stopWinch = make(chan struct{})
	signal.Notify(s.winch, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-s.winch:
				if w := width(); w > 0 && s.live != nil {
					s.live.SetWidth(w)
				}
			case <-s.stopWinch:
				return
			}
		}
	}()
}

// RestoreTermios hands the keyboard back before a child that may read stdin.
// The latch must not re-enter cbreak for that child's `?1049l`.
func (s *inlineSurface) RestoreTermios() {
	if s == nil || s.guard == nil {
		return
	}
	s.guard.RestoreTermios()
	if s.live != nil {
		s.live.SetCbreak(false)
	}
}

// Close clears the box and restores the terminal. It is idempotent and runs on
// every exit path (normal, failure, signal and a panic unwinding through the
// caller). It finishes the renderer first so the teardown and the last draw
// cannot interleave.
func (s *inlineSurface) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.winch != nil {
			signal.Stop(s.winch)
			close(s.stopWinch)
		}
	})
	if s.live != nil {
		s.live.Finish()
	}
	s.vp.Teardown()
	if s.guard != nil {
		s.guard.Restore()
	}
}

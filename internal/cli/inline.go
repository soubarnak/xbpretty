package cli

import (
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// inlineSurface is the terminal state around the inline progress viewport
// (FR-072): cbreak while xbpretty owns a transfer, the DSR-anchored bottom box
// on the normal screen, and the ordered restore on every exit path. It never
// enters an alternate screen and never reads a key.
type inlineSurface struct {
	guard *term.Guard
	vp    *tui.Viewport
	live  *liveProgress
}

// startInline builds the viewport before any child that may read stdin. When
// stdin is a terminal it enters cbreak (ISIG on, so Ctrl-C stays a real
// signal) and asks DSR for a cursor anchor; a silent terminal takes the
// no-anchor fallback. It returns nil when the plain line is in use or the
// streams are not files.
func (st *state) startInline(live *liveProgress) *inlineSurface {
	if live == nil || live.mode != liveWidgetMode {
		return nil
	}
	out, ok := asFile(st.stderr)
	if !ok {
		return nil
	}
	vp := tui.NewViewport(out, terminalWidth(st.stderr))
	s := &inlineSurface{vp: vp}
	var anchor tui.Anchor
	if in, ok := asFile(st.stdin); ok && isTTY(st.stdin) {
		s.guard = term.NewGuard(in, out)
		if err := s.guard.EnterInline(); err != nil {
			// A tty that cannot be switched to cbreak still gets the box;
			// only input echo and the alt-screen latch's termios half are lost.
			s.guard = nil
		} else {
			anchor = tui.LocateAnchor(in, out, tui.DefaultAnchorTimeout)
		}
	}
	vp.Start(anchor)
	s.live = live
	live.SetInline(vp, s.guard, s.guard != nil)
	return s
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

// Close clears the box and restores the terminal. It is idempotent and is
// deferred by every caller, so a normal exit, a failure and a signal all take
// the same path.
func (s *inlineSurface) Close() {
	if s == nil {
		return
	}
	s.vp.Teardown()
	if s.guard != nil {
		s.guard.Restore()
	}
}

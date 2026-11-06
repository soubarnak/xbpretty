package tui

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/term"
)

// AltScreenLatch is FR-072's two-state `ESC[?1049h`/`ESC[?1049l` latch. It is
// deliberately not an ANSI parser: it recognises only the alternate-screen
// enter and leave markers, reports the transitions, and keeps enough of a
// split marker to survive chunk boundaries.
type AltScreenLatch struct {
	hidden bool
	tail   string
}

// Observe feeds one chunk of child output and reports the transitions it
// caused. hide is true only on an enter marker while visible; show is true
// only on a leave marker while hidden. Repeats are inert.
func (l *AltScreenLatch) Observe(chunk string) (hide, show bool) {
	data := l.tail + chunk
	if !l.hidden && strings.Contains(data, term.AltEnter) {
		l.hidden = true
		hide = true
	}
	if l.hidden && strings.Contains(data, term.AltLeave) {
		l.hidden = false
		show = true
	}
	l.tail = altMarkerSuffix(data)
	return hide, show
}

// Hidden reports the latch's current state.
func (l *AltScreenLatch) Hidden() bool { return l.hidden }

// altMarkerSuffix keeps the longest suffix that could still be a prefix of one
// of the two markers, so a sequence split across writes is not missed.
func altMarkerSuffix(s string) string {
	for _, marker := range []string{term.AltEnter, term.AltLeave} {
		for n := len(marker) - 1; n > 0; n-- {
			if strings.HasSuffix(s, marker[:n]) {
				return s[len(s)-n:]
			}
		}
	}
	return ""
}

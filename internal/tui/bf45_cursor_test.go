package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/term"
)

// TestViewportAnchorHidesTheCursor is BF-45's first half: an anchored inline
// box hides the cursor before anything else is written, so the terminal never
// shows a blinking cursor inside the progress box.
func TestViewportAnchorHidesTheCursor(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{Row: 4, Col: 1, OK: true})
	if got, want := out.String(), term.CursorHide; got != want {
		t.Fatalf("anchored Start = %q, want %q", got, want)
	}
	vp.Draw(inlineBox)
	if !strings.HasPrefix(out.String(), term.CursorHide) {
		t.Fatalf("the cursor is not hidden before the first frame: %q", out.String())
	}
}

// TestViewportNoAnchorHidesTheCursorBeforeTheNewline keeps the fallback's
// line break after the hide, so the first frame still starts on a fresh line.
func TestViewportNoAnchorHidesTheCursorBeforeTheNewline(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{})
	if got, want := out.String(), term.CursorHide+term.NoAnchor; got != want {
		t.Fatalf("no-anchor Start = %q, want %q", got, want)
	}
}

// TestViewportTeardownShowsTheCursor is BF-45's second half: teardown shows the
// cursor again, then erases the box, and a second teardown writes nothing.
func TestViewportTeardownShowsTheCursor(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{OK: true})
	vp.Draw(inlineBox)
	before := out.Len()
	vp.Teardown()
	if got, want := out.String()[before:], term.CursorShow+term.MoveUp(2)+"\r"+clearDown; got != want {
		t.Fatalf("teardown = %q, want %q", got, want)
	}
	after := out.Len()
	vp.Teardown()
	if out.Len() != after {
		t.Fatalf("a second Teardown wrote %q", out.String()[after:])
	}
}

// TestViewportTeardownShowsTheCursorWithoutADraw covers an anchored viewport
// torn down before its first frame: the cursor was hidden, so it must be shown.
func TestViewportTeardownShowsTheCursorWithoutADraw(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{OK: true})
	vp.Teardown()
	if got, want := out.String(), term.CursorHide+term.CursorShow; got != want {
		t.Fatalf("early teardown = %q, want %q", got, want)
	}
}

// TestViewportNeverStartedLeavesTheCursorAlone keeps a viewport that was never
// anchored from touching the cursor on teardown.
func TestViewportNeverStartedLeavesTheCursorAlone(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Teardown()
	if out.Len() != 0 {
		t.Fatalf("Teardown without Start wrote %q", out.String())
	}
}

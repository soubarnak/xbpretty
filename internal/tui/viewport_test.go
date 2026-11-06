package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/term"
)

// box is a three-line stand-in for the rendered progress frame.
var inlineBox = []string{"┏━┓", "┃x┃", "┗━┛"}

// TestViewportNoAnchorStart pins FR-072's no-anchor fallback: a silent DSR
// leaves the box starting on a fresh line ("\r\n") before the first draw.
func TestViewportNoAnchorStart(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{})
	if got, want := out.String(), term.CursorHide+term.NoAnchor; got != want {
		t.Fatalf("Start without anchor = %q, want %q", got, want)
	}
	vp.Draw(inlineBox)
	want := term.CursorHide + term.NoAnchor + "┏━┓\n┃x┃\n┗━┛\r"
	if got := out.String(); got != want {
		t.Fatalf("no-anchor draw = %q, want %q", got, want)
	}
	if got := vp.Height(); got != 3 {
		t.Errorf("Height = %d, want 3", got)
	}
}

// TestViewportAnchorWritesNothingBeforeTheBox keeps the DSR reply path quiet:
// an answered query needs no extra line. The only byte is the cursor hide
// (BF-45), which is not a line and does not move the box.
func TestViewportAnchorWritesNothingBeforeTheBox(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{Row: 12, Col: 1, OK: true})
	if got, want := out.String(), term.CursorHide; got != want {
		t.Fatalf("anchored Start wrote %q, want only the cursor hide %q", got, want)
	}
}

// TestViewportInsertPutsTheLineAboveTheBox is the core of FR-072's
// insert-above-the-box behaviour: the completed line becomes scrollback and
// the box is redrawn one row lower.
func TestViewportInsertPutsTheLineAboveTheBox(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{Row: 1, Col: 1, OK: true})
	vp.Draw(inlineBox)
	first := out.String()

	vp.Insert("Last completed: figlet-2.2.5_6.x86_64.xbps")
	insertSeq := out.String()[len(first):]
	wantInsert := term.MoveUp(2) + "\r" + clearDown +
		"Last completed: figlet-2.2.5_6.x86_64.xbps\n"
	if insertSeq != wantInsert {
		t.Fatalf("insert = %q, want %q", insertSeq, wantInsert)
	}

	vp.Draw(inlineBox)
	reanchor := out.String()[len(first)+len(insertSeq):]
	wantDraw := "┏━┓\n┃x┃\n┗━┛\r"
	if reanchor != wantDraw {
		t.Fatalf("re-anchored draw = %q, want %q", reanchor, wantDraw)
	}
	if got := vp.Height(); got != 3 {
		t.Errorf("insert changed box height to %d", got)
	}
}

// TestViewportRepeatedInserts re-anchors after every line, so a burst of
// completions leaves one scrollback line per archive and a single box.
func TestViewportRepeatedInserts(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{OK: true})
	vp.Draw(inlineBox)
	for _, line := range []string{"one", "two", "three"} {
		vp.Insert(line)
		vp.Draw(inlineBox)
	}
	got := out.String()
	for _, line := range []string{"one", "two", "three"} {
		if strings.Count(got, line+"\n") != 1 {
			t.Errorf("line %q does not appear exactly once above the box:\n%q", line, got)
		}
	}
	if strings.Count(got, "┏━┓") != 4 {
		t.Errorf("box redraw count = %d, want 4 (initial + one per insert)", strings.Count(got, "┏━┓"))
	}
	// Every insertion erases the previous box before writing its line; the
	// following Draw repaints on the cleared rows and needs no second erase.
	if strings.Count(got, clearDown) != 3 {
		t.Errorf("clear-down count = %d, want one per insertion", strings.Count(got, clearDown))
	}
}

// TestViewportSetWidthReflowsWidthOnly pins AC-055's resize half at the seam:
// the width follows the terminal; the fixed view height does not.
func TestViewportSetWidthReflowsWidthOnly(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{OK: true})
	vp.Draw(inlineBox)
	before := out.Len()
	vp.SetWidth(50)
	if got := vp.Width(); got != 50 {
		t.Fatalf("Width = %d, want 50", got)
	}
	vp.Draw([]string{"┏━━┓", "┃xx┃", "┗━━┛"})
	if got := vp.Height(); got != 3 {
		t.Errorf("resize changed height to %d, want 3", got)
	}
	redraw := out.String()[before:]
	want := term.MoveUp(2) + "\r" + clearDown + "┏━━┓\n┃xx┃\n┗━━┛\r"
	if redraw != want {
		t.Fatalf("resize redraw = %q, want %q", redraw, want)
	}
}

// TestViewportHideShowsTheFullScreenLatch covers FR-072's two-state latch:
// hiding clears the box and suppresses draws; showing redraws at the anchor.
func TestViewportHideShowsTheFullScreenLatch(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{OK: true})
	vp.Draw(inlineBox)
	before := out.Len()
	vp.Hide()
	if got := vp.Hidden(); !got {
		t.Fatal("Hide did not mark the viewport hidden")
	}
	hideSeq := out.String()[before:]
	if want := term.MoveUp(2) + "\r" + clearDown; hideSeq != want {
		t.Fatalf("hide = %q, want %q", hideSeq, want)
	}
	afterHide := out.Len()
	vp.Draw(inlineBox)
	if out.Len() != afterHide {
		t.Fatalf("Draw while hidden wrote %q", out.String()[afterHide:])
	}
	vp.Show(inlineBox)
	if vp.Hidden() {
		t.Fatal("Show did not clear the hidden state")
	}
	if got, want := out.String()[afterHide:], "┏━┓\n┃x┃\n┗━┛\r"; got != want {
		t.Fatalf("show = %q, want %q", got, want)
	}
}

// TestViewportTeardownClears is FR-072's teardown: show the cursor again
// (BF-45), erase the box, stop drawing, and stay idempotent around a deferred
// second call.
func TestViewportTeardownClears(t *testing.T) {
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
	vp.Draw(inlineBox)
	vp.Insert("late")
	if out.Len() != after {
		t.Fatalf("writes after teardown: %q", out.String()[after:])
	}
}

// TestViewportInsertWithoutDraw writes the line at the cursor when no box is
// on screen, which is the first event of a stream that completes before its
// first frame.
func TestViewportInsertWithoutDraw(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Start(Anchor{OK: true})
	vp.Insert("first")
	if got, want := out.String(), term.CursorHide+"first\n"; got != want {
		t.Fatalf("insert before draw = %q, want %q", got, want)
	}
}

// TestAltScreenLatchIsTwoState is FR-072's `?1049h/l` latch: each marker is a
// transition, repeats are inert, and a split sequence still works.
func TestAltScreenLatchIsTwoState(t *testing.T) {
	var latch AltScreenLatch
	if hide, show := latch.Observe("plain output"); hide || show {
		t.Fatalf("plain output toggled the latch: hide=%v show=%v", hide, show)
	}
	if hide, show := latch.Observe("x" + term.AltEnter + "y"); !hide || show {
		t.Fatalf("AltEnter: hide=%v show=%v, want hide only", hide, show)
	}
	if hide, show := latch.Observe(term.AltEnter); hide || show {
		t.Fatalf("repeated AltEnter toggled: hide=%v show=%v", hide, show)
	}
	if hide, show := latch.Observe(term.AltLeave); hide || !show {
		t.Fatalf("AltLeave: hide=%v show=%v, want show only", hide, show)
	}
	if hide, show := latch.Observe(term.AltLeave); hide || show {
		t.Fatalf("repeated AltLeave toggled: hide=%v show=%v", hide, show)
	}
	// A marker split across two writes is still recognised.
	if hide, _ := latch.Observe(term.AltEnter[:4]); hide {
		t.Fatal("a partial marker toggled the latch")
	}
	if hide, _ := latch.Observe(term.AltEnter[4:]); !hide {
		t.Fatal("the split AltEnter marker was not recognised")
	}
}

// TestViewportShowBeforeStartIsInert keeps Show honest about lifecycle:
// nothing can be redrawn before Start or after Teardown.
func TestViewportShowBeforeStartIsInert(t *testing.T) {
	var out bytes.Buffer
	vp := NewViewport(&out, 80)
	vp.Show(inlineBox)
	if out.Len() != 0 {
		t.Fatalf("Show before Start wrote %q", out.String())
	}
	vp.Start(Anchor{OK: true})
	vp.Hide()
	vp.Teardown()
	before := out.Len()
	vp.Show(inlineBox)
	if out.Len() != before {
		t.Fatalf("Show after Teardown wrote %q", out.String()[before:])
	}
}

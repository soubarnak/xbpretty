package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/term"
)

// TestBufferDiff is AC-055's cell-buffer half: a full first frame clears, an
// identical frame emits nothing, and one changed cell emits one move plus the
// new glyph instead of the whole row.
func TestBufferDiff(t *testing.T) {
	a := NewBuffer(10, 2)
	a.DrawText(0, 0, "hello", ZeroStyle)
	first := a.Flush(nil, true)
	if !bytes.Contains(first, []byte(term.ClearAll)) {
		t.Errorf("first frame must clear: %q", first)
	}
	if payload := a.Flush(a, true); len(payload) != 0 {
		t.Errorf("identical frame emitted %q", payload)
	}
	b := NewBuffer(10, 2)
	b.DrawText(0, 0, "hello", ZeroStyle)
	b.Set(1, 0, 'a', ZeroStyle)
	payload := b.Flush(a, true)
	if !bytes.Contains(payload, []byte("a")) || !bytes.Contains(payload, []byte(term.MoveTo(1, 2))) {
		t.Errorf("single-cell diff = %q", payload)
	}
	if bytes.Contains(payload, []byte("hello")) {
		t.Errorf("unchanged run re-emitted: %q", payload)
	}

	// Trailing blanks are never part of the draw.
	c := NewBuffer(10, 1)
	c.DrawText(0, 0, "hi", ZeroStyle)
	if p := c.Flush(nil, false); strings.Contains(string(p), "hi   ") {
		t.Errorf("trailing blanks emitted: %q", p)
	}
}

// TestNoColorRemovesOnlySGR is AC-055's colour rule: --color never removes the
// SGR bytes but keeps every glyph and cursor control.
func TestNoColorRemovesOnlySGR(t *testing.T) {
	buf := RenderSummary(goldenUpgrade(), 80, 24, DefaultTheme())
	plain := string(buf.Flush(nil, false))
	if strings.Contains(plain, "\x1b[") {
		// Cursor controls are ESC sequences; only SGR (ESC[...m) is forbidden.
		for i := 0; i < len(plain); i++ {
			if plain[i] == 0x1b && i+1 < len(plain) && plain[i+1] == '[' {
				j := i + 2
				for j < len(plain) && plain[j] != 'm' && plain[j] != 'H' && plain[j] != 'J' && !(plain[j] >= 'A' && plain[j] <= 'Z') {
					j++
				}
				if j < len(plain) && plain[j] == 'm' {
					t.Fatalf("--color never emitted SGR %q", plain[i:j+1])
				}
			}
		}
	}
	for _, glyph := range []string{"┏", "❯", "━", "┃"} {
		if !strings.Contains(plain, glyph) {
			t.Errorf("--color never removed glyph %q", glyph)
		}
	}
	colored := string(buf.Flush(nil, true))
	if !strings.Contains(colored, "\x1b[1;92m") {
		t.Errorf("coloured frame is missing the Primary SGR: %q", colored[:min(len(colored), 120)])
	}
}

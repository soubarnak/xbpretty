package term

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1},
		{'┏', 1},
		{'━', 1},
		{'✓', 1},
		{'☐', 1},
		{'❯', 1},
		{'↑', 1},
		{'中', 2},
		{'あ', 2},
		{'한', 2},
		{'Ａ', 2},
		{0x0301, 0}, // combining acute
		{0x200d, 0}, // ZWJ
		{0xfe0f, 0}, // VS16
		{0x1f600, 2},
		{0x1f3fb, 0}, // skin tone
		{0x00, 0},
		{0x07, 0},
		{0x7f, 0},
	}
	for _, c := range cases {
		if got := RuneWidth(c.r); got != c.want {
			t.Errorf("RuneWidth(%U)=%d want %d", c.r, got, c.want)
		}
	}
	if got := StringWidth("a中b"); got != 4 {
		t.Errorf("StringWidth mixed=%d want 4", got)
	}
	if got := Truncate("a中b", 2); got != "a" {
		t.Errorf("Truncate cut a wide rune: %q", got)
	}
}

func TestParseDSR(t *testing.T) {
	row, col, n, ok := parseDSR([]byte("\x1b[12;34R"))
	if !ok || row != 12 || col != 34 || n != len("\x1b[12;34R") {
		t.Fatalf("parseDSR = %d,%d,%d,%v", row, col, n, ok)
	}
	// Stray key bytes before the reply must be preserved, not parsed as DSR.
	stray := []byte("j\x1b[7;3R")
	row, col, n, ok = parseDSR(stray)
	if !ok || row != 7 || col != 3 || n != len(stray) {
		t.Fatalf("parseDSR with stray = %d,%d,%d,%v", row, col, n, ok)
	}
	if string(stray[:n-len("\x1b[7;3R")]) != "j" {
		t.Fatalf("stray prefix lost")
	}
	if _, _, _, ok := parseDSR([]byte("\x1b[12;34")); ok {
		t.Fatalf("partial DSR parsed as complete")
	}
}

func TestBufferDiff(t *testing.T) {
	a := NewBuffer(10, 2)
	a.DrawText(0, 0, "hello", ZeroStyle)
	var payload []byte
	first := a.Flush(nil, true)
	if !bytes.Contains(first, []byte("\x1b[2J")) {
		t.Errorf("first frame must clear")
	}
	b := a
	payload = b.Flush(a, true)
	if len(payload) != 0 {
		t.Errorf("identical frame emitted %q", payload)
	}
	b = NewBuffer(10, 2)
	b.DrawText(0, 0, "hello", ZeroStyle)
	b.Set(1, 0, 'a', ZeroStyle)
	payload = b.Flush(a, true)
	if !bytes.Contains(payload, []byte("a")) || !bytes.Contains(payload, []byte("\x1b[1;2H")) {
		t.Errorf("single cell diff = %q", payload)
	}
	if bytes.Contains(payload, []byte("hello")) {
		t.Errorf("unchanged run re-emitted: %q", payload)
	}
	// Trailing blanks must never be part of the draw.
	c := NewBuffer(10, 1)
	c.DrawText(0, 0, "hi", ZeroStyle)
	p := c.Flush(nil, false)
	if strings.Contains(string(p), "hi   ") {
		t.Errorf("trailing blanks emitted: %q", p)
	}
}

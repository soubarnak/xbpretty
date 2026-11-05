package term

import "testing"

// TestRuneWidth is AC-055's width policy: box glyphs and the UI's own symbols
// are one cell, CJK is two, combining/ZWJ/VS16 and controls are zero, and
// ambiguous-width characters stay one.
func TestRuneWidth(t *testing.T) {
	cases := []struct {
		name string
		r    rune
		want int
	}{
		{"ascii", 'a', 1},
		{"thick corner", '┏', 1},
		{"thick horizontal", '━', 1},
		{"thick vertical", '┃', 1},
		{"check", '✓', 1},
		{"ballot", '☐', 1},
		{"selection", '❯', 1},
		{"arrow", '↑', 1},
		{"cjk han", '中', 2},
		{"hiragana", 'あ', 2},
		{"hangul", '한', 2},
		{"fullwidth", 'Ａ', 2},
		{"emoji", 0x1f600, 2},
		{"combining acute", 0x0301, 0},
		{"zwj", 0x200d, 0},
		{"variation selector 16", 0xfe0f, 0},
		{"skin tone", 0x1f3fb, 0},
		{"nul", 0x00, 0},
		{"bell", 0x07, 0},
		{"delete", 0x7f, 0},
		{"ambiguous bullet", 0x2022, 1},
	}
	for _, c := range cases {
		if got := RuneWidth(c.r); got != c.want {
			t.Errorf("%s: RuneWidth(%U) = %d, want %d", c.name, c.r, got, c.want)
		}
	}
	if got := StringWidth("a中b"); got != 4 {
		t.Errorf("StringWidth(a中b) = %d, want 4", got)
	}
	if got := Truncate("a中b", 2); got != "a" {
		t.Errorf("Truncate split a wide rune: %q", got)
	}
	if got := Truncate("abc", 0); got != "" {
		t.Errorf("Truncate(0) = %q, want empty", got)
	}
}

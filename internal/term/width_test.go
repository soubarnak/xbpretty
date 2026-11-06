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

// TestEmojiBlocksAreWide is BF-33: the transport, geometric and colour-circle
// emoji blocks measure as two cells, the same as the emoji around them.
func TestEmojiBlocksAreWide(t *testing.T) {
	cases := []struct {
		name string
		r    rune
	}{
		{"rocket", 0x1f680},
		{"octagonal sign", 0x1f6d1},
		{"green circle", 0x1f7e2},
		{"luggage", 0x1f6c5},
		{"sleeping accommodation", 0x1f6cc},
		{"place of worship", 0x1f6d0},
		{"shopping trolley", 0x1f6d2},
		{"hindu temple", 0x1f6d5},
		{"elevator", 0x1f6d7},
		{"wireless", 0x1f6dc},
		{"ring buoy", 0x1f6df},
		{"airplane departure", 0x1f6eb},
		{"airplane arriving", 0x1f6ec},
		{"kick scooter", 0x1f6f4},
		{"roller skate", 0x1f6fc},
		{"large orange circle", 0x1f7e0},
		{"large brown square", 0x1f7eb},
		{"heavy equals sign", 0x1f7f0},
	}
	for _, c := range cases {
		if got := RuneWidth(c.r); got != 2 {
			t.Errorf("%s: RuneWidth(%U) = %d, want 2", c.name, c.r, got)
		}
	}
}

// TestRangeTablesAreSortedAndDisjoint guards the binary search in inRanges: every
// table must be ascending, with each range non-empty and starting after the
// previous range ends.
func TestRangeTablesAreSortedAndDisjoint(t *testing.T) {
	tables := []struct {
		name   string
		ranges []runeRange
	}{
		{"wideRanges", wideRanges},
		{"zeroWidthRanges", zeroWidthRanges},
	}
	for _, table := range tables {
		for i, r := range table.ranges {
			if r.lo > r.hi {
				t.Errorf("%s[%d] = {%U,%U} is empty", table.name, i, r.lo, r.hi)
			}
			if i > 0 && r.lo <= table.ranges[i-1].hi {
				t.Errorf("%s[%d] = {%U,%U} is not after {%U,%U}", table.name, i, r.lo, r.hi, table.ranges[i-1].lo, table.ranges[i-1].hi)
			}
		}
	}
}

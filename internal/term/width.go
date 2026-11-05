package term

import "unicode"

// RuneWidth implements ADR-0017's display-width policy without a dependency:
// combining and zero-width -> 0, East Asian Wide/Fullwidth and emoji -> 2,
// ambiguous -> 1, controls -> 0. No grapheme clustering.
func RuneWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if inRanges(r, zeroWidthRanges) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if inRanges(r, wideRanges) {
		return 2
	}
	return 1
}

// StringWidth is the sum of rune widths.
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

type runeRange struct{ lo, hi rune }

var zeroWidthRanges = []runeRange{
	{0x200b, 0x200f},   // ZWSP..RLM
	{0x2028, 0x202e},   // line/paragraph separators, bidi controls
	{0x2060, 0x2064},   // word joiner..invisible times
	{0xfe00, 0xfe0f},   // variation selectors
	{0xfeff, 0xfeff},   // BOM
	{0x1f3fb, 0x1f3ff}, // skin tone modifiers
}

// East Asian Wide/Fullwidth, the subset that matters for package data. The
// ambiguous-width box glyphs and arrows deliberately stay width 1.
var wideRanges = []runeRange{
	{0x1100, 0x115f},   // Hangul Jamo
	{0x231a, 0x231b},   // watch, hourglass
	{0x2329, 0x232a},   // angle brackets
	{0x23e9, 0x23ec},   // media controls
	{0x23f0, 0x23f0},   // alarm clock
	{0x23f3, 0x23f3},   // hourglass flowing
	{0x25fd, 0x25fe},   // small squares
	{0x2614, 0x2615},   // umbrella, hot beverage
	{0x2648, 0x2653},   // zodiac
	{0x267f, 0x267f},   // wheelchair
	{0x2693, 0x2693},   // anchor
	{0x26a1, 0x26a1},   // high voltage
	{0x26aa, 0x26ab},   // circles
	{0x26bd, 0x26be},   // soccer, baseball
	{0x26c4, 0x26c5},   // snowman, sun behind cloud
	{0x26ce, 0x26ce},   // ophiuchus
	{0x26d4, 0x26d4},   // no entry
	{0x26ea, 0x26ea},   // church
	{0x26f2, 0x26f3},   // fountain, golf
	{0x26f5, 0x26f5},   // sailboat
	{0x26fa, 0x26fa},   // tent
	{0x26fd, 0x26fd},   // fuel pump
	{0x2705, 0x2705},   // white heavy check mark
	{0x270a, 0x270b},   // fist
	{0x2728, 0x2728},   // sparkles
	{0x274c, 0x274c},   // cross mark
	{0x274e, 0x274e},   // negative cross mark
	{0x2753, 0x2755},   // question marks
	{0x2757, 0x2757},   // exclamation
	{0x2795, 0x2797},   // math symbols
	{0x27b0, 0x27b0},   // curly loop
	{0x27bf, 0x27bf},   // double curly loop
	{0x2b1b, 0x2b1c},   // large squares
	{0x2b50, 0x2b50},   // star
	{0x2b55, 0x2b55},   // heavy circle
	{0x2e80, 0x303e},   // CJK radicals..CJK symbols
	{0x3041, 0x33ff},   // Hiragana..CJK compatibility
	{0x3400, 0x4dbf},   // CJK extension A
	{0x4e00, 0x9fff},   // CJK unified
	{0xa000, 0xa4cf},   // Yi
	{0xa960, 0xa97f},   // Hangul Jamo extended A
	{0xac00, 0xd7a3},   // Hangul syllables
	{0xf900, 0xfaff},   // CJK compatibility ideographs
	{0xfe10, 0xfe19},   // vertical forms
	{0xfe30, 0xfe6f},   // CJK compatibility forms
	{0xff00, 0xff60},   // fullwidth forms
	{0xffe0, 0xffe6},   // fullwidth signs
	{0x16fe0, 0x16fe4}, // ideographic symbols
	{0x17000, 0x187f7}, // Tangut
	{0x18800, 0x18cd5}, // Tangut components
	{0x1b000, 0x1b2fb}, // Kana supplement
	{0x1f004, 0x1f004}, // mahjong
	{0x1f0cf, 0x1f0cf}, // playing card
	{0x1f18e, 0x1f18e}, // AB button
	{0x1f191, 0x1f19a}, // squared symbols
	{0x1f200, 0x1f2ff}, // enclosed ideographs
	{0x1f300, 0x1f64f}, // emoji
	{0x1f900, 0x1f9ff}, // supplemental emoji
	{0x1fa70, 0x1faff}, // symbols and pictographs extended A
	{0x20000, 0x3fffd}, // CJK extensions B..
}

func inRanges(r rune, rs []runeRange) bool {
	lo, hi := 0, len(rs)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if r < rs[mid].lo {
			hi = mid - 1
		} else if r > rs[mid].hi {
			lo = mid + 1
		} else {
			return true
		}
	}
	return false
}

// Truncate clips s to at most max cells, never splitting a width-2 rune's cell.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := RuneWidth(r)
		if w+rw > max {
			return s[:i]
		}
		w += rw
	}
	return s
}

// PadRight pads s with spaces to width w (clipping when wider).
func PadRight(s string, w int) string {
	sw := StringWidth(s)
	if sw >= w {
		return Truncate(s, w)
	}
	for sw < w {
		s += " "
		sw++
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

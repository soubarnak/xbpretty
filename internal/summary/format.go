package summary

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	resetSeq      = "\x1b[0m"
	boldSeq       = "\x1b[1m"
	dimSeq        = "\x1b[2m"
	greenSeq      = "\x1b[1;32m"
	blueSeq       = "\x1b[1;34m"
	redSeq        = "\x1b[1;31m"
	orangeSeq     = "\x1b[38;5;202m"
	boldOrangeSeq = "\x1b[1;38;5;202m"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// VisibleWidth returns the terminal display width in runes, ignoring ANSI codes.
func VisibleWidth(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
}

func visibleWidth(s string) int {
	return VisibleWidth(s)
}

// TruncateVisible truncates s so its display width (ignoring ANSI escape codes)
// does not exceed maxWidth. If s has ANSI styling and is truncated, a reset sequence is appended.
func TruncateVisible(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if VisibleWidth(s) <= maxWidth {
		return s
	}

	var buf strings.Builder
	width := 0
	hasColor := false

	i := 0
	b := []byte(s)
	for i < len(b) {
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '[' {
			end := i + 2
			for end < len(b) && !((b[end] >= 'a' && b[end] <= 'z') || (b[end] >= 'A' && b[end] <= 'Z')) {
				end++
			}
			if end < len(b) {
				end++
				buf.Write(b[i:end])
				hasColor = true
				i = end
				continue
			}
		}

		r, size := utf8.DecodeRune(b[i:])
		if width+1 > maxWidth {
			break
		}
		buf.WriteRune(r)
		width++
		i += size
	}

	if hasColor {
		buf.WriteString("\x1b[0m")
	}
	return buf.String()
}

// BoxChars holds the characters used to render container borders.
type BoxChars struct {
	TopLeft      string
	TopRight     string
	BottomLeft   string
	BottomRight  string
	Horizontal   string
	Vertical     string
	DividerLeft  string
	DividerRight string
}

// BoxCharacterSet returns the BoxChars for the specified box style
// ("rounded", "classic", "ascii"). Empty or unknown styles fall back to "rounded".
func BoxCharacterSet(style string) BoxChars {
	switch style {
	case "classic":
		return BoxChars{
			TopLeft:      "┌",
			TopRight:     "┐",
			BottomLeft:   "└",
			BottomRight:  "┘",
			Horizontal:   "─",
			Vertical:     "│",
			DividerLeft:  "├",
			DividerRight: "┤",
		}
	case "ascii":
		return BoxChars{
			TopLeft:      "+",
			TopRight:     "+",
			BottomLeft:   "+",
			BottomRight:  "+",
			Horizontal:   "-",
			Vertical:     "|",
			DividerLeft:  "+",
			DividerRight: "+",
		}
	case "rounded", "":
		fallthrough
	default:
		return BoxChars{
			TopLeft:      "╭",
			TopRight:     "╮",
			BottomLeft:   "╰",
			BottomRight:  "╯",
			Horizontal:   "─",
			Vertical:     "│",
			DividerLeft:  "├",
			DividerRight: "┤",
		}
	}
}

// VersionDiff highlights changed version components in ANSI 202 orange
// (\x1b[38;5;202m) when color is true.
func VersionDiff(oldVer, newVer string, color bool) string {
	shouldBypass := oldVer == "" || !color || oldVer == newVer
	if shouldBypass {
		return newVer
	}

	oldRevParts := strings.Split(oldVer, "_")
	newRevParts := strings.Split(newVer, "_")

	hasOldRev := len(oldRevParts) == 2
	hasNewRev := len(newRevParts) == 2
	bothHaveRevs := hasOldRev && hasNewRev
	sameUpstream := bothHaveRevs && oldRevParts[0] == newRevParts[0]

	// 1. Revision only change (e.g. 2.38_1 -> 2.38_2)
	if sameUpstream {
		revChanged := oldRevParts[1] != newRevParts[1]
		if revChanged {
			return fmt.Sprintf("%s_%s%s%s", newRevParts[0], orangeSeq, newRevParts[1], resetSeq)
		}
		return newVer
	}

	// 2. Upstream version comparison by '.'
	oldDots := strings.Split(oldRevParts[0], ".")
	newDots := strings.Split(newRevParts[0], ".")

	minLen := len(oldDots)
	if len(newDots) < minLen {
		minLen = len(newDots)
	}

	startColor := minLen
	for i := 0; i < minLen; i++ {
		if oldDots[i] != newDots[i] {
			startColor = i
			break
		}
	}

	// When new version has fewer segments than old, and all common segments matched
	// (e.g. 1.2.3 -> 1.2), highlight the changed trailing segment of new upstream.
	fewerSegments := len(oldDots) > len(newDots)
	matchedAllCommon := startColor >= len(newDots)
	if fewerSegments && matchedAllCommon {
		startColor = len(newDots) - 1
		if startColor < 0 {
			startColor = 0
		}
	}

	var coloredParts []string
	for i, part := range newDots {
		if i >= startColor {
			coloredParts = append(coloredParts, orangeSeq+part+resetSeq)
		} else {
			coloredParts = append(coloredParts, part)
		}
	}

	res := strings.Join(coloredParts, ".")
	if hasNewRev {
		revChanged := !hasOldRev || oldRevParts[1] != newRevParts[1]
		if revChanged {
			res += "_" + orangeSeq + newRevParts[1] + resetSeq
		} else {
			res += "_" + newRevParts[1]
		}
	}
	return res
}

func sprintf(format string, v ...any) string {
	return fmt.Sprintf(format, v...)
}

func itoa(n uint64) string {
	return strconv.FormatUint(n, 10)
}

// width is the display width of s in columns, ignoring ANSI escape sequences.
func width(s string) int {
	return visibleWidth(s)
}

// widest returns the display width of the longest of n values produced by f.
func widest(f func(int) string, n int) int {
	max := 0
	for i := 0; i < n; i++ {
		if w := width(f(i)); w > max {
			max = w
		}
	}
	return max
}

// PadRight fills s out to w columns.
func PadRight(s string, w int) string {
	cur := VisibleWidth(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

func padRight(s string, w int) string {
	return PadRight(s, w)
}

// PadLeft fills s from the left out to w columns.
func PadLeft(s string, w int) string {
	cur := VisibleWidth(s)
	if cur >= w {
		return s
	}
	return strings.Repeat(" ", w-cur) + s
}

func padLeft(s string, w int) string {
	return PadLeft(s, w)
}

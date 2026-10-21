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

// visibleWidth returns the terminal display width in runes, ignoring ANSI codes.
func visibleWidth(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
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
	if oldVer == "" || !color {
		return newVer
	}
	if oldVer == newVer {
		return newVer
	}

	// 1. Revision only change (e.g. 2.38_1 -> 2.38_2)
	oldRevParts := strings.Split(oldVer, "_")
	newRevParts := strings.Split(newVer, "_")
	if len(oldRevParts) == 2 && len(newRevParts) == 2 && oldRevParts[0] == newRevParts[0] {
		if oldRevParts[1] != newRevParts[1] {
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

	var coloredParts []string
	for i, part := range newDots {
		if i >= startColor {
			coloredParts = append(coloredParts, orangeSeq+part+resetSeq)
		} else {
			coloredParts = append(coloredParts, part)
		}
	}

	res := strings.Join(coloredParts, ".")
	if len(newRevParts) == 2 {
		if startColor < len(newDots) {
			// Upstream changed, revision is also highlighted
			res += "_" + orangeSeq + newRevParts[1] + resetSeq
		} else if len(oldRevParts) == 2 && oldRevParts[1] != newRevParts[1] {
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

// padRight fills s out to w columns.
func padRight(s string, w int) string {
	cur := visibleWidth(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

// padLeft fills s from the left out to w columns.
func padLeft(s string, w int) string {
	cur := visibleWidth(s)
	if cur >= w {
		return s
	}
	return strings.Repeat(" ", w-cur) + s
}

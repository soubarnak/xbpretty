package summary

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	boldSeq = "\x1b[1m"
	dimSeq  = "\x1b[2m"
	reset   = "\x1b[0m"
)

// groupColours are the per-group ANSI colours, keyed by group title.
var groupColours = map[string]string{
	"Install": "\x1b[32m",
	"Upgrade": "\x1b[33m",
	"Remove":  "\x1b[31m",
	"Hold":    "\x1b[35m",
}

// paint wraps text in sgr codes when colour is on, and returns it unchanged
// otherwise.
func (r Renderer) paint(code, text string) string {
	if !r.Color || code == "" {
		return text
	}
	return code + text + reset
}

func sprintf(format string, v any) string {
	return fmt.Sprintf(format, v)
}

func itoa(n uint64) string {
	return strconv.FormatUint(n, 10)
}

// width is the display width of s in columns. An upgrade's `old → new` cell
// is one column wider than the arrow's three bytes, so columns are measured
// in runes before they are padded (FR-012).
func width(s string) int {
	return utf8.RuneCountInString(s)
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

// pad fills s out to w columns.
func pad(s string, w int) string {
	return padRight(s, w)
}

// padRight fills s out to w columns.
func padRight(s string, w int) string {
	if width(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-width(s))
}

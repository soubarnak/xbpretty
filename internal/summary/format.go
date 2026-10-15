package summary

import (
	"fmt"
	"strconv"
	"strings"
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

// widest returns the display width of the longest of n values produced by f.
func widest(f func(int) string, n int) int {
	max := 0
	for i := 0; i < n; i++ {
		if w := len(f(i)); w > max {
			max = w
		}
	}
	return max
}

// pad right-aligns s in a field of w bytes.
func pad(s string, w int) string {
	return padRight(s, w)
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

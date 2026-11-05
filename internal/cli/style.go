package cli

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// progressRole is one of nala's nine PascalCase theme roles as the inline
// progress surfaces use them (ADR-0012 decision 2).
type progressRole int

const (
	rolePrimary progressRole = iota
	roleSecondary
	roleHighlight
	roleRegular
	roleProgressFilled
	roleProgressUnfilled
	roleNotice
	roleWarning
	roleError
)

// roleSGR is the exact SGR prefix nala emits for each role under the default
// theme (ADR-0012 decision 2). ProgressFilled and ProgressUnfilled share the
// default palette's bright green and bright red.
var roleSGR = map[progressRole]string{
	rolePrimary:          "\x1b[1;92m",
	roleSecondary:        "\x1b[1;94m",
	roleHighlight:        "\x1b[1;39m",
	roleRegular:          "\x1b[0;39m",
	roleProgressFilled:   "\x1b[1;92m",
	roleProgressUnfilled: "\x1b[1;91m",
	roleNotice:           "\x1b[1;93m",
	roleWarning:          "\x1b[1;93m",
	roleError:            "\x1b[1;91m",
}

const ansiResetSeq = "\x1b[0m"

// paintRole wraps s in the role's SGR prefix and a reset. With colour off it
// returns s untouched, so a --no-color run emits no SGR bytes at all.
func paintRole(color bool, role progressRole, s string) string {
	if !color || s == "" {
		return s
	}
	return roleSGR[role] + s + ansiResetSeq
}

// roleWidth and padRole are the ANSI-aware width helpers the widget needs.
func roleWidth(s string) int { return summary.VisibleWidth(s) }

func padRole(s string, width int) string {
	if n := roleWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func clipRole(s string, width int) string {
	return summary.TruncateVisible(s, width)
}

func repeatRole(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

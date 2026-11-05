package cli

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// progressRole is one of nala's nine PascalCase theme roles as the inline
// progress surfaces use it (ADR-0012 decision 2).
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

const ansiResetSeq = "\x1b[0m"

// rolePrefix returns the role's SGR prefix from the resolved [Color.theme].
// The zero Theme falls back to nala's default palette.
func rolePrefix(theme summary.Theme, role progressRole) string {
	if theme == (summary.Theme{}) {
		theme = summary.DefaultTheme()
	}
	switch role {
	case rolePrimary:
		return theme.Primary
	case roleSecondary:
		return theme.Secondary
	case roleHighlight:
		return theme.Highlight
	case roleRegular:
		return theme.Regular
	case roleProgressFilled:
		return theme.ProgressFilled
	case roleProgressUnfilled:
		return theme.ProgressUnfilled
	case roleNotice:
		return theme.Notice
	case roleWarning:
		return theme.Warning
	case roleError:
		return theme.Error
	}
	return theme.Regular
}

// paintRole wraps s in the role's SGR prefix and a reset. With colour off it
// returns s untouched, so a --no-color run emits no SGR bytes at all.
func paintRole(theme summary.Theme, color bool, role progressRole, s string) string {
	if !color || s == "" {
		return s
	}
	return rolePrefix(theme, role) + s + ansiResetSeq
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

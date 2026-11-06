package tui

import "github.com/soubarnak/xbpretty/internal/term"

// Role is one of nala's nine theme roles. The surfaces only need the subset
// their widgets paint; the remaining roles stay for the picker (#78).
type Role uint8

// The roles the S1/S2 widgets use.
const (
	RoleRegular Role = iota
	RolePrimary
	RoleSecondary
	RoleHighlight
	RoleNotice
	RoleWarning
	RoleError
)

// Theme maps roles to cell styles. The cli converts [Color.theme] into one of
// these before a surface opens, so a custom palette reaches the cell buffer
// without embedding SGR in text.
type Theme struct {
	Primary   term.Style
	Secondary term.Style
	Highlight term.Style
	Regular   term.Style
	Notice    term.Style
	Warning   term.Style
	Error     term.Style
}

// DefaultTheme is nala v0.18.2's nine-role default palette expressed as cell
// styles: Primary 1;92, Secondary 1;94, Highlight 1;39, Regular 0;39, Notice
// and Warning 1;93, Error 1;91.
func DefaultTheme() Theme {
	bold := func(n uint8) term.Style {
		return term.Style{FG: term.ANSI(n), Mods: term.Bold}
	}
	return Theme{
		Primary:   bold(10),
		Secondary: bold(12),
		Highlight: term.Style{FG: term.Default, Mods: term.Bold},
		Regular:   term.Style{FG: term.Default},
		Notice:    bold(11),
		Warning:   bold(11),
		Error:     bold(9),
	}
}

// style returns the role's cell style. A role that leaves its background at
// the terminal default takes the Regular background, so a custom Regular
// colour covers every cell of the frame instead of leaving holes (FR-072).
func (t Theme) style(r Role) term.Style {
	s := t.roleStyle(r)
	if s.BG == term.Default {
		s.BG = t.Regular.BG
	}
	return s
}

// roleStyle is the role's own cell style before the Regular background
// fallback.
func (t Theme) roleStyle(r Role) term.Style {
	switch r {
	case RolePrimary:
		return t.Primary
	case RoleSecondary:
		return t.Secondary
	case RoleHighlight:
		return t.Highlight
	case RoleNotice:
		return t.Notice
	case RoleWarning:
		return t.Warning
	case RoleError:
		return t.Error
	}
	return t.Regular
}

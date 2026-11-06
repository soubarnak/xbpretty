package summary

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const resetSeq = "\x1b[0m"

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// VisibleWidth returns the display width in runes, ignoring ANSI codes.
//
// This is deliberately rune-based, not East-Asian-width aware: xbps package
// names, versions, reasons and the summary's own glyphs are ASCII, so one
// rune is one terminal column for every inline surface.
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
		buf.WriteString(resetSeq)
	}
	return buf.String()
}

// Role is one of nala's nine theme roles.
type Role int

// The nine theme roles, in nala's order.
const (
	RolePrimary Role = iota
	RoleSecondary
	RoleHighlight
	RoleRegular
	RoleProgressFilled
	RoleProgressUnfilled
	RoleNotice
	RoleWarning
	RoleError
)

// Theme holds the SGR prefix each role paints with. The zero value is not
// useful; use DefaultTheme or a theme derived from [Color.theme].
type Theme struct {
	Primary          string
	Secondary        string
	Highlight        string
	Regular          string
	ProgressFilled   string
	ProgressUnfilled string
	Notice           string
	Warning          string
	Error            string
}

// DefaultTheme returns nala v0.18.2's nine-role default palette.
func DefaultTheme() Theme {
	return Theme{
		Primary:          "\x1b[1;92m",
		Secondary:        "\x1b[1;94m",
		Highlight:        "\x1b[1;39m",
		Regular:          "\x1b[0;39m",
		ProgressFilled:   "\x1b[1;92m",
		ProgressUnfilled: "\x1b[1;91m",
		Notice:           "\x1b[1;93m",
		Warning:          "\x1b[1;93m",
		Error:            "\x1b[1;91m",
	}
}

// prefix returns the role's SGR prefix, falling back to the default palette
// for a zero Theme.
func (t Theme) prefix(role Role) string {
	if t == (Theme{}) {
		t = DefaultTheme()
	}
	switch role {
	case RolePrimary:
		return t.Primary
	case RoleSecondary:
		return t.Secondary
	case RoleHighlight:
		return t.Highlight
	case RoleRegular:
		return t.Regular
	case RoleProgressFilled:
		return t.ProgressFilled
	case RoleProgressUnfilled:
		return t.ProgressUnfilled
	case RoleNotice:
		return t.Notice
	case RoleWarning:
		return t.Warning
	case RoleError:
		return t.Error
	}
	return ""
}

// VersionPart is one segment of FR-063's version diff. Changed marks the
// components the Notice role paints; unchanged text (including the dots
// between changed components) stays raw. Splitting the algorithm into
// segments lets the inline renderer and the TUI share it without either
// re-typing the boundaries.
type VersionPart struct {
	Text    string
	Changed bool
}

// VersionParts splits newVer into the raw and changed components of the
// version diff, split at the last '_' and then on '.'. A missing or identical
// old version yields one unchanged part.
func VersionParts(oldVer, newVer string) []VersionPart {
	if oldVer == "" || oldVer == newVer {
		return []VersionPart{{Text: newVer}}
	}
	if oldUp, _, ok := splitRevision(oldVer); ok {
		if newUp, newRev, ok2 := splitRevision(newVer); ok2 && oldUp == newUp {
			return []VersionPart{{Text: newUp + "_"}, {Text: newRev, Changed: true}}
		}
	}
	oldParts := strings.Split(oldVer, ".")
	newParts := strings.Split(newVer, ".")
	start := 0
	for i := range newParts {
		if i >= len(oldParts) || oldParts[i] != newParts[i] {
			start = i
			break
		}
	}
	var out []VersionPart
	for i, part := range newParts {
		if i > 0 {
			out = append(out, VersionPart{Text: "."})
		}
		out = append(out, VersionPart{Text: part, Changed: i >= start})
	}
	if len(out) == 0 {
		return []VersionPart{{Text: newVer}}
	}
	return out
}

// splitRevision splits an xbps version at its last '_', the revision
// separator (xbps uses '_' where Debian uses '-').
func splitRevision(v string) (upstream, revision string, ok bool) {
	i := strings.LastIndex(v, "_")
	if i <= 0 || i == len(v)-1 {
		return v, "", false
	}
	return v[:i], v[i+1:], true
}

func itoa(n uint64) string {
	return strconv.FormatUint(n, 10)
}

// PadRight fills s out to w columns.
func PadRight(s string, w int) string {
	cur := VisibleWidth(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

func padRight(s string, w int) string { return PadRight(s, w) }

// PadLeft fills s from the left out to w columns.
func PadLeft(s string, w int) string {
	cur := VisibleWidth(s)
	if cur >= w {
		return s
	}
	return strings.Repeat(" ", w-cur) + s
}

func padLeft(s string, w int) string { return PadLeft(s, w) }

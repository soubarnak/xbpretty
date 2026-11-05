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

// VersionDiff highlights the changed components of newVer with nala's Notice
// role. oldVer is never painted. The revision rides on the last highlighted
// component, and a '.' between two highlighted components stays plain.
func VersionDiff(oldVer, newVer string, color bool) string {
	if !color {
		return newVer
	}
	return versionDiffWith(oldVer, newVer, func(part string) string {
		return DefaultTheme().Notice + part + resetSeq
	})
}

// versionDiffWith is VersionDiff's algorithm with a caller-supplied painter,
// so a renderer can apply its resolved theme role.
func versionDiffWith(oldVer, newVer string, paint func(string) string) string {
	if oldVer == "" || oldVer == newVer {
		return newVer
	}
	if oldUp, _, ok := splitRevision(oldVer); ok {
		if newUp, newRev, ok2 := splitRevision(newVer); ok2 && oldUp == newUp {
			return newUp + "_" + paint(newRev)
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
	parts := make([]string, len(newParts))
	for i, part := range newParts {
		if i >= start {
			parts[i] = paint(part)
		} else {
			parts[i] = part
		}
	}
	return strings.Join(parts, ".")
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
			TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
			Horizontal: "─", Vertical: "│", DividerLeft: "├", DividerRight: "┤",
		}
	case "ascii":
		return BoxChars{
			TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
			Horizontal: "-", Vertical: "|", DividerLeft: "+", DividerRight: "+",
		}
	case "rounded", "":
		fallthrough
	default:
		return BoxChars{
			TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
			Horizontal: "─", Vertical: "│", DividerLeft: "├", DividerRight: "┤",
		}
	}
}

func itoa(n uint64) string {
	return strconv.FormatUint(n, 10)
}

// width is the display width of s in columns, ignoring ANSI escape sequences.
func width(s string) int { return visibleWidth(s) }

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

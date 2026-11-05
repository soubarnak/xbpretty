package cli

import (
	"strconv"
	"strings"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/summary"
)

// summaryTheme converts the resolved [Color.theme] roles into the SGR
// prefixes the inline renderer paints with.
func summaryTheme(t config.Theme) summary.Theme {
	return summary.Theme{
		Primary:          sgrPrefix(t.Primary),
		Secondary:        sgrPrefix(t.Secondary),
		Highlight:        sgrPrefix(t.Highlight),
		Regular:          sgrPrefix(t.Regular),
		ProgressFilled:   sgrPrefix(t.ProgressFilled),
		ProgressUnfilled: sgrPrefix(t.ProgressUnfilled),
		Notice:           sgrPrefix(t.Notice),
		Warning:          sgrPrefix(t.Warning),
		Error:            sgrPrefix(t.Error),
	}
}

// sgrPrefix reproduces nala's Style::ansi_prefix bytes:
// ESC[<modifiers>;<fg>[;<bg>]m, where an unset modifier list emits 0.
func sgrPrefix(style config.Style) string {
	mods := modifierCodes(style.Modifier)
	fg := colourCode(style.Fg, false)
	parts := append(mods, fg)
	if style.HasBg {
		parts = append(parts, colourCode(style.Bg, true))
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// modifierCodes returns the SGR modifier parameters in nala's bit order.
func modifierCodes(m config.Modifiers) []string {
	var out []string
	for _, entry := range []struct {
		bit  config.Modifiers
		code string
	}{
		{config.ModifierBold, "1"},
		{config.ModifierDim, "2"},
		{config.ModifierItalic, "3"},
		{config.ModifierUnderlined, "4"},
		{config.ModifierSlowBlink, "5"},
		{config.ModifierRapidBlink, "6"},
		{config.ModifierReversed, "7"},
		{config.ModifierHidden, "8"},
		{config.ModifierCrossedOut, "9"},
	} {
		if m.Has(entry.bit) {
			out = append(out, entry.code)
		}
	}
	if len(out) == 0 {
		return []string{"0"}
	}
	return out
}

// colourCode returns one colour parameter, foreground or background.
func colourCode(c config.Color, background bool) string {
	offset := 0
	if background {
		offset = 10
	}
	named := func(base int) string { return strconv.Itoa(base + offset) }
	switch c.Kind {
	case config.ColorReset:
		if background {
			return "49"
		}
		return "39"
	case config.ColorBlack:
		return named(30)
	case config.ColorRed:
		return named(31)
	case config.ColorGreen:
		return named(32)
	case config.ColorYellow:
		return named(33)
	case config.ColorBlue:
		return named(34)
	case config.ColorMagenta:
		return named(35)
	case config.ColorCyan:
		return named(36)
	case config.ColorGray:
		return named(37)
	case config.ColorDarkGray:
		return named(90)
	case config.ColorLightRed:
		return named(91)
	case config.ColorLightGreen:
		return named(92)
	case config.ColorLightYellow:
		return named(93)
	case config.ColorLightBlue:
		return named(94)
	case config.ColorLightMagenta:
		return named(95)
	case config.ColorLightCyan:
		return named(96)
	case config.ColorWhite:
		return named(97)
	case config.ColorIndexed:
		if background {
			return "48;5;" + strconv.Itoa(int(c.Index))
		}
		return "38;5;" + strconv.Itoa(int(c.Index))
	case config.ColorRGB:
		prefix := "38"
		if background {
			prefix = "48"
		}
		return prefix + ";2;" + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
	}
	return "39"
}

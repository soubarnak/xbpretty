package main

import "github.com/soubarnak/xbpretty/internal/term"

// Nala's nine theme roles, collapsed to the ANSI-16 styles #45 specifies.
var (
	Regular   = term.Style{}
	Primary   = term.Style{FG: term.ANSI(10), Mods: term.Bold} // bold bright green 92
	Highlight = term.Style{FG: term.Default, Mods: term.Bold}  // bold 39
	Secondary = term.Style{FG: term.ANSI(12), Mods: term.Bold} // bold bright blue 94
	Error     = term.Style{FG: term.ANSI(9), Mods: term.Bold}  // bold bright red 91
	Notice    = term.Style{FG: term.ANSI(11), Mods: term.Bold} // bold bright yellow 93
	Italic    = term.Style{Mods: term.Italic}
)

// Theme carries only the one switch the surfaces need: colour on/off.
type Theme struct{ Color bool }

// TooSmall is #50 §2.6's notice, centred in the body.
func TooSmall(w, h int) string {
	return "Terminal too small (60x18 minimum; now " + itoa(w) + "x" + itoa(h) + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

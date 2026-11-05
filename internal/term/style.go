package term

// Mods is the bitmask of SGR modifiers a theme role can request.
type Mods uint16

// The nine SGR modifiers, in nala's bit order.
const (
	Bold Mods = 1 << iota
	Dim
	Italic
	Underline
	Blink
	Reverse
	Hidden
	Strike
)

// ColorKind distinguishes the SGR colour spaces.
type ColorKind uint8

// The supported colour forms.
const (
	ColorDefault ColorKind = iota
	ColorANSI
	Color256
	ColorRGB
)

// Color is one terminal colour.
type Color struct {
	Kind    ColorKind
	N       uint8
	R, G, B uint8
}

// ANSI returns an ANSI-16 colour.
func ANSI(n uint8) Color { return Color{Kind: ColorANSI, N: n} }

// Default is the terminal's default colour.
var Default = Color{}

// Style is a cell's complete appearance.
type Style struct {
	FG, BG Color
	Mods   Mods
}

// ZeroStyle is the unstyled cell.
var ZeroStyle = Style{}

package config

import (
	"fmt"
	"strings"
)

// NalaConfig is nala's [Nala] table: the six behaviour switches.
type NalaConfig struct {
	AutoRemove         bool
	AutoUpdate         bool
	UpdateShowPackages bool
	FullUpgrade        bool
	Simple             bool
	AssumeYes          bool
}

// UIMode is the [Ui] mode enum. Values are exactly Auto, Plain and Tui.
type UIMode string

// The [Ui] mode values, case-sensitive.
const (
	ModeAuto  UIMode = "Auto"
	ModePlain UIMode = "Plain"
	ModeTui   UIMode = "Tui"
)

// Unit is the [Ui] unit enum. Values are exactly Binary and Decimal.
type Unit string

// The [Ui] unit values, case-sensitive.
const (
	UnitBinary  Unit = "Binary"
	UnitDecimal Unit = "Decimal"
)

// UiConfig is the [Ui] table.
type UiConfig struct {
	Mode UIMode
	Unit Unit
}

// ColorMode is the [Color] mode enum: Auto, Always or Never.
type ColorMode string

// The [Color] mode values, case-sensitive.
const (
	ColorAuto   ColorMode = "Auto"
	ColorAlways ColorMode = "Always"
	ColorNever  ColorMode = "Never"
)

// ColorKind identifies one form of the colour grammar.
type ColorKind uint8

// The supported colour forms. Named colours use their ANSI palette slot;
// Indexed and RGB carry the extended forms.
const (
	ColorReset ColorKind = iota
	ColorBlack
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorGray
	ColorDarkGray
	ColorLightRed
	ColorLightGreen
	ColorLightYellow
	ColorLightBlue
	ColorLightMagenta
	ColorLightCyan
	ColorWhite
	ColorIndexed
	ColorRGB
)

// Color is one parsed theme colour.
type Color struct {
	Kind  ColorKind
	Index uint8
	R     uint8
	G     uint8
	B     uint8
}

// Modifiers is a bit set of the nine text modifiers the theme accepts.
type Modifiers uint16

// The nine modifier bits.
const (
	ModifierBold Modifiers = 1 << iota
	ModifierDim
	ModifierItalic
	ModifierUnderlined
	ModifierSlowBlink
	ModifierRapidBlink
	ModifierReversed
	ModifierHidden
	ModifierCrossedOut
)

// Has reports whether every bit in other is set.
func (m Modifiers) Has(other Modifiers) bool { return m&other == other }

// Style is one theme role: { fg, bg?, modifier? }. An omitted modifier is
// BOLD; an empty modifier means none.
type Style struct {
	Fg       Color
	Bg       Color
	HasBg    bool
	Modifier Modifiers
}

// Theme holds the nine PascalCase theme roles.
type Theme struct {
	Primary          Style
	Secondary        Style
	Highlight        Style
	Regular          Style
	ProgressFilled   Style
	ProgressUnfilled Style
	Notice           Style
	Warning          Style
	Error            Style
}

// ColorConfig is the [Color] table and its [Color.theme] palette.
type ColorConfig struct {
	Mode  ColorMode
	Theme Theme
}

// XbpsConfig is the xbps-only [Xbps] table.
type XbpsConfig struct {
	InstallPath    string
	RemovePath     string
	QueryPath      string
	RindexPath     string
	UhelperPath    string
	PkgdbPath      string
	TransactionLog bool
}

// Config is the fully resolved rev 6 configuration.
type Config struct {
	Nala  NalaConfig
	Ui    UiConfig
	Color ColorConfig
	Xbps  XbpsConfig
}

// Defaults returns the built-in rev 6 settings: the values a missing file or
// a file that names only some keys resolves to.
func Defaults() Config {
	return Config{
		Nala: NalaConfig{
			AutoRemove: true,
			AutoUpdate: true,
		},
		Ui: UiConfig{
			Mode: ModeAuto,
			Unit: UnitBinary,
		},
		Color: ColorConfig{
			Mode:  ColorAuto,
			Theme: DefaultTheme(),
		},
		Xbps: XbpsConfig{
			InstallPath:    "xbps-install",
			RemovePath:     "xbps-remove",
			QueryPath:      "xbps-query",
			RindexPath:     "xbps-rindex",
			UhelperPath:    "xbps-uhelper",
			PkgdbPath:      "xbps-pkgdb",
			TransactionLog: true,
		},
	}
}

// DefaultTheme returns nala's default nine-role palette.
func DefaultTheme() Theme {
	bold := func(c ColorKind) Style { return Style{Fg: Color{Kind: c}, Modifier: ModifierBold} }
	none := func(c ColorKind) Style { return Style{Fg: Color{Kind: c}} }
	return Theme{
		Primary:          bold(ColorLightGreen),
		Secondary:        bold(ColorLightBlue),
		Highlight:        bold(ColorReset),
		Regular:          none(ColorReset),
		ProgressFilled:   bold(ColorLightGreen),
		ProgressUnfilled: bold(ColorLightRed),
		Notice:           bold(ColorLightYellow),
		Warning:          bold(ColorLightYellow),
		Error:            bold(ColorLightRed),
	}
}

// resolve applies the parsed document to the defaults.
func resolve(doc *tomlDoc) (Config, error) {
	cfg := Defaults()
	for _, entry := range doc.top {
		return cfg, &Error{Line: entry.line, Message: fmt.Sprintf(
			"unknown key %q; configuration keys belong in a table", entry.key)}
	}
	for _, table := range doc.tables {
		if err := applyTable(&cfg, table); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func applyTable(cfg *Config, table tomlTable) error {
	switch table.name {
	case "Nala":
		return applyNala(&cfg.Nala, table)
	case "Ui":
		return applyUi(&cfg.Ui, table)
	case "Color":
		return applyColor(&cfg.Color, table)
	case "Color.theme":
		return applyTheme(&cfg.Color.Theme, table)
	case "Xbps":
		return applyXbps(&cfg.Xbps, table)
	}
	return &Error{Line: table.line, Message: fmt.Sprintf("unknown table [%s]", table.name)}
}

func applyNala(cfg *NalaConfig, table tomlTable) error {
	for _, entry := range table.entries {
		value, err := boolValue(table.name, entry)
		if err != nil {
			return err
		}
		switch entry.key {
		case "auto_remove":
			cfg.AutoRemove = value
		case "auto_update":
			cfg.AutoUpdate = value
		case "update_show_packages":
			cfg.UpdateShowPackages = value
		case "full_upgrade":
			cfg.FullUpgrade = value
		case "simple":
			cfg.Simple = value
		case "assume_yes":
			cfg.AssumeYes = value
		default:
			return unknownKey(table.name, entry)
		}
	}
	return nil
}

func applyUi(cfg *UiConfig, table tomlTable) error {
	for _, entry := range table.entries {
		switch entry.key {
		case "mode":
			mode, err := enumValue[UIMode](table.name, entry, []UIMode{ModeAuto, ModePlain, ModeTui})
			if err != nil {
				return err
			}
			cfg.Mode = mode
		case "unit":
			unit, err := enumValue[Unit](table.name, entry, []Unit{UnitBinary, UnitDecimal})
			if err != nil {
				return err
			}
			cfg.Unit = unit
		default:
			return unknownKey(table.name, entry)
		}
	}
	return nil
}

func applyColor(cfg *ColorConfig, table tomlTable) error {
	for _, entry := range table.entries {
		switch entry.key {
		case "mode":
			mode, err := enumValue[ColorMode](table.name, entry, []ColorMode{ColorAuto, ColorAlways, ColorNever})
			if err != nil {
				return err
			}
			cfg.Mode = mode
		case "theme":
			// An alternate theme form: the roles live under [Color.theme].
			return &Error{Line: entry.line, Message: "[Color] theme is not supported; write roles in [Color.theme]"}
		default:
			return unknownKey(table.name, entry)
		}
	}
	return nil
}

func applyXbps(cfg *XbpsConfig, table tomlTable) error {
	for _, entry := range table.entries {
		if entry.key == "transaction_log" {
			value, err := boolValue(table.name, entry)
			if err != nil {
				return err
			}
			cfg.TransactionLog = value
			continue
		}
		var target *string
		switch entry.key {
		case "install_path":
			target = &cfg.InstallPath
		case "remove_path":
			target = &cfg.RemovePath
		case "query_path":
			target = &cfg.QueryPath
		case "rindex_path":
			target = &cfg.RindexPath
		case "uhelper_path":
			target = &cfg.UhelperPath
		case "pkgdb_path":
			target = &cfg.PkgdbPath
		default:
			return unknownKey(table.name, entry)
		}
		value, err := stringValue(table.name, entry)
		if err != nil {
			return err
		}
		if err := validatePath(entry.key, value); err != nil {
			return &Error{Line: entry.line, Message: err.Error()}
		}
		*target = value
	}
	return nil
}

// validatePath enforces FR-053's path rule: a non-empty bare program name or
// an absolute path, with no control characters and no arguments.
func validatePath(key, path string) error {
	if path == "" {
		return fmt.Errorf("[Xbps] %s must not be empty", key)
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("[Xbps] %s must not contain control characters", key)
		}
	}
	if strings.ContainsAny(path, " \t") {
		return fmt.Errorf("[Xbps] %s must be a bare program name or an absolute path, got %q (arguments are not allowed)", key, path)
	}
	if !strings.Contains(path, "/") {
		return nil
	}
	if path[0] != '/' {
		return fmt.Errorf("[Xbps] %s must be a bare program name or an absolute path, got %q", key, path)
	}
	return nil
}

var themeRoles = []string{
	"Primary", "Secondary", "Highlight", "Regular", "ProgressFilled",
	"ProgressUnfilled", "Notice", "Warning", "Error",
}

func isThemeRole(key string) bool {
	for _, role := range themeRoles {
		if role == key {
			return true
		}
	}
	return false
}

func applyTheme(theme *Theme, table tomlTable) error {
	for _, entry := range table.entries {
		if !isThemeRole(entry.key) {
			return &Error{Line: entry.line, Message: fmt.Sprintf("unknown theme role %q", entry.key)}
		}
		style, err := parseStyle(entry)
		if err != nil {
			return err
		}
		switch entry.key {
		case "Primary":
			theme.Primary = style
		case "Secondary":
			theme.Secondary = style
		case "Highlight":
			theme.Highlight = style
		case "Regular":
			theme.Regular = style
		case "ProgressFilled":
			theme.ProgressFilled = style
		case "ProgressUnfilled":
			theme.ProgressUnfilled = style
		case "Notice":
			theme.Notice = style
		case "Warning":
			theme.Warning = style
		case "Error":
			theme.Error = style
		}
	}
	return nil
}

func parseStyle(entry tomlEntry) (Style, error) {
	if entry.value.kind != tomlTableValue {
		return Style{}, &Error{Line: entry.line, Message: fmt.Sprintf(
			"[Color.theme] %s must be an inline table, got %s", entry.key, entry.value.kind)}
	}
	style := Style{Modifier: ModifierBold}
	var hasFg bool
	for _, field := range entry.value.table {
		switch field.key {
		case "fg", "bg":
			colour, err := parseColour(field.value)
			if err != nil {
				return Style{}, &Error{Line: field.line, Message: fmt.Sprintf(
					"[Color.theme] %s: %s: %s", entry.key, field.key, err.Error())}
			}
			if field.key == "fg" {
				style.Fg = colour
				hasFg = true
			} else {
				style.Bg = colour
				style.HasBg = true
			}
		case "modifier":
			mods, err := parseModifiers(field.value)
			if err != nil {
				return Style{}, &Error{Line: field.line, Message: fmt.Sprintf(
					"[Color.theme] %s: %s", entry.key, err.Error())}
			}
			style.Modifier = mods
		default:
			return Style{}, &Error{Line: field.line, Message: fmt.Sprintf(
				"[Color.theme] %s has unknown key %q", entry.key, field.key)}
		}
	}
	if !hasFg {
		return Style{}, &Error{Line: entry.line, Message: fmt.Sprintf(
			"[Color.theme] %s is missing 'fg'", entry.key)}
	}
	return style, nil
}

func parseColour(value tomlValue) (Color, error) {
	switch value.kind {
	case tomlString:
		return parseColourString(value.str)
	case tomlInteger:
		if value.integer < 0 || value.integer > 255 {
			return Color{}, fmt.Errorf("colour index %d is outside 0-255", value.integer)
		}
		return Color{Kind: ColorIndexed, Index: uint8(value.integer)}, nil
	case tomlTableValue:
		return parseColourTable(value)
	}
	return Color{}, fmt.Errorf("expected a colour (string, 0-255 or { Rgb | Indexed }), got %s", value.kind)
}

func parseColourString(s string) (Color, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Color{}, fmt.Errorf("unknown colour %q", s)
	}
	if s[0] == '#' {
		if len(s) != 7 {
			return Color{}, fmt.Errorf("invalid hexadecimal colour %q", s)
		}
		var rgb [3]uint8
		for i := 0; i < 3; i++ {
			hi, ok := hexNibble(s[1+i*2])
			if !ok {
				return Color{}, fmt.Errorf("invalid hexadecimal colour %q", s)
			}
			lo, ok := hexNibble(s[2+i*2])
			if !ok {
				return Color{}, fmt.Errorf("invalid hexadecimal colour %q", s)
			}
			rgb[i] = hi<<4 | lo
		}
		return Color{Kind: ColorRGB, R: rgb[0], G: rgb[1], B: rgb[2]}, nil
	}
	if isAllDigits(s) {
		index := 0
		for i := 0; i < len(s); i++ {
			index = index*10 + int(s[i]-'0')
			if index > 255 {
				return Color{}, fmt.Errorf("colour index %s is outside 0-255", s)
			}
		}
		return Color{Kind: ColorIndexed, Index: uint8(index)}, nil
	}
	if kind, ok := namedColour(s); ok {
		return Color{Kind: kind}, nil
	}
	return Color{}, fmt.Errorf("unknown colour %q", s)
}

func parseColourTable(value tomlValue) (Color, error) {
	if len(value.table) != 1 {
		return Color{}, fmt.Errorf("colour table must have exactly one of Rgb or Indexed")
	}
	field := value.table[0]
	switch field.key {
	case "Rgb":
		if field.value.kind != tomlArray {
			return Color{}, fmt.Errorf("Rgb must be an array of three integers, got %s", field.value.kind)
		}
		if len(field.value.array) != 3 {
			return Color{}, fmt.Errorf("Rgb must have exactly 3 components, got %d", len(field.value.array))
		}
		var rgb [3]uint8
		for i, component := range field.value.array {
			if component.kind != tomlInteger {
				return Color{}, fmt.Errorf("Rgb component must be an integer, got %s", component.kind)
			}
			if component.integer < 0 || component.integer > 255 {
				return Color{}, fmt.Errorf("Rgb component %d is outside 0-255", component.integer)
			}
			rgb[i] = uint8(component.integer)
		}
		return Color{Kind: ColorRGB, R: rgb[0], G: rgb[1], B: rgb[2]}, nil
	case "Indexed":
		if field.value.kind != tomlInteger {
			return Color{}, fmt.Errorf("Indexed must be an integer, got %s", field.value.kind)
		}
		if field.value.integer < 0 || field.value.integer > 255 {
			return Color{}, fmt.Errorf("colour index %d is outside 0-255", field.value.integer)
		}
		return Color{Kind: ColorIndexed, Index: uint8(field.value.integer)}, nil
	}
	return Color{}, fmt.Errorf("unknown colour form %q", field.key)
}

func hexNibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func namedColour(name string) (ColorKind, bool) {
	norm := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == ' ' || c == '\t' || c == '_' || c == '-' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		norm = append(norm, c)
	}
	switch string(norm) {
	case "reset":
		return ColorReset, true
	case "black":
		return ColorBlack, true
	case "red":
		return ColorRed, true
	case "green":
		return ColorGreen, true
	case "yellow":
		return ColorYellow, true
	case "blue":
		return ColorBlue, true
	case "magenta", "purple":
		return ColorMagenta, true
	case "cyan":
		return ColorCyan, true
	case "gray", "grey", "lightgray", "lightgrey":
		return ColorGray, true
	case "darkgray", "darkgrey", "brightblack":
		return ColorDarkGray, true
	case "lightred", "brightred":
		return ColorLightRed, true
	case "lightgreen", "brightgreen":
		return ColorLightGreen, true
	case "lightyellow", "brightyellow":
		return ColorLightYellow, true
	case "lightblue", "brightblue":
		return ColorLightBlue, true
	case "lightmagenta", "brightmagenta", "lightpurple", "brightpurple":
		return ColorLightMagenta, true
	case "lightcyan", "brightcyan":
		return ColorLightCyan, true
	case "white", "brightwhite", "lightwhite":
		return ColorWhite, true
	}
	return 0, false
}

func parseModifiers(value tomlValue) (Modifiers, error) {
	switch value.kind {
	case tomlString:
		return modifierString(value.str)
	case tomlArray:
		var mods Modifiers
		for _, element := range value.array {
			if element.kind != tomlString {
				return 0, fmt.Errorf("modifier must be a string or an array of strings, got array of %s", element.kind)
			}
			one, err := modifierToken(element.str)
			if err != nil {
				return 0, err
			}
			mods |= one
		}
		return mods, nil
	}
	return 0, fmt.Errorf("modifier must be a string or an array of strings, got %s", value.kind)
}

func modifierString(s string) (Modifiers, error) {
	var mods Modifiers
	for _, token := range strings.Split(s, "|") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		one, err := modifierToken(token)
		if err != nil {
			return 0, err
		}
		mods |= one
	}
	return mods, nil
}

func modifierToken(token string) (Modifiers, error) {
	norm := make([]byte, 0, len(token))
	for i := 0; i < len(token); i++ {
		c := token[i]
		if c == ' ' || c == '\t' || c == '_' || c == '-' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		norm = append(norm, c)
	}
	switch string(norm) {
	case "BOLD":
		return ModifierBold, nil
	case "DIM":
		return ModifierDim, nil
	case "ITALIC":
		return ModifierItalic, nil
	case "UNDERLINED":
		return ModifierUnderlined, nil
	case "SLOWBLINK":
		return ModifierSlowBlink, nil
	case "RAPIDBLINK":
		return ModifierRapidBlink, nil
	case "REVERSED":
		return ModifierReversed, nil
	case "HIDDEN":
		return ModifierHidden, nil
	case "CROSSEDOUT", "STRIKETHROUGH", "STRIKE":
		return ModifierCrossedOut, nil
	}
	return 0, fmt.Errorf("unknown modifier %q", token)
}

func boolValue(table string, entry tomlEntry) (bool, error) {
	if entry.value.kind != tomlBool {
		return false, typeError(table, entry, "boolean")
	}
	return entry.value.boolean, nil
}

func stringValue(table string, entry tomlEntry) (string, error) {
	if entry.value.kind != tomlString {
		return "", typeError(table, entry, "string")
	}
	return entry.value.str, nil
}

func enumValue[T ~string](table string, entry tomlEntry, allowed []T) (T, error) {
	var zero T
	if entry.value.kind != tomlString {
		return zero, typeError(table, entry, "string")
	}
	for _, candidate := range allowed {
		if string(candidate) == entry.value.str {
			return candidate, nil
		}
	}
	names := make([]string, len(allowed))
	for i, candidate := range allowed {
		names[i] = string(candidate)
	}
	return zero, &Error{Line: entry.line, Message: fmt.Sprintf(
		"[%s] %s must be exactly one of %s, got %q", table, entry.key, joinComma(names), entry.value.str)}
}

func typeError(table string, entry tomlEntry, want string) error {
	return &Error{Line: entry.line, Message: fmt.Sprintf(
		"[%s] %s must be a %s, got %s", table, entry.key, want, entry.value.kind)}
}

func unknownKey(table string, entry tomlEntry) error {
	return &Error{Line: entry.line, Message: fmt.Sprintf("unknown key %q in [%s]", entry.key, table)}
}

func joinComma(parts []string) string { return strings.Join(parts, ", ") }

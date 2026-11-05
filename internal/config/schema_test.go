package config

import (
	"os"
	"strings"
	"testing"
)

// TestExampleConfigEqualsDefaults pins AC-037: the canonical example parses
// to the built-in defaults, key for key and role for role.
func TestExampleConfigEqualsDefaults(t *testing.T) {
	f, err := os.Open("../../docs/config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := Parse("docs/config.toml.example", f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("example config does not equal defaults:\n got %+v\nwant %+v", cfg, Defaults())
	}
}

func TestDefaultsAreTheSpecDefaults(t *testing.T) {
	d := Defaults()
	if d.Nala != (NalaConfig{AutoRemove: true, AutoUpdate: true}) {
		t.Errorf("Nala defaults = %+v", d.Nala)
	}
	if d.Ui.Mode != ModeAuto || d.Ui.Unit != UnitBinary {
		t.Errorf("Ui defaults = %+v", d.Ui)
	}
	if d.Color.Mode != ColorAuto {
		t.Errorf("Color.Mode = %q", d.Color.Mode)
	}
	if d.Xbps != (XbpsConfig{
		InstallPath: "xbps-install", RemovePath: "xbps-remove", QueryPath: "xbps-query",
		RindexPath: "xbps-rindex", UhelperPath: "xbps-uhelper", PkgdbPath: "xbps-pkgdb",
		TransactionLog: true,
	}) {
		t.Errorf("Xbps defaults = %+v", d.Xbps)
	}
	theme := d.Color.Theme
	want := map[string]struct {
		kind ColorKind
		mods Modifiers
	}{
		"Primary":          {ColorLightGreen, ModifierBold},
		"Secondary":        {ColorLightBlue, ModifierBold},
		"Highlight":        {ColorReset, ModifierBold},
		"Regular":          {ColorReset, 0},
		"ProgressFilled":   {ColorLightGreen, ModifierBold},
		"ProgressUnfilled": {ColorLightRed, ModifierBold},
		"Notice":           {ColorLightYellow, ModifierBold},
		"Warning":          {ColorLightYellow, ModifierBold},
		"Error":            {ColorLightRed, ModifierBold},
	}
	got := map[string]struct {
		kind ColorKind
		mods Modifiers
	}{
		"Primary":          {theme.Primary.Fg.Kind, theme.Primary.Modifier},
		"Secondary":        {theme.Secondary.Fg.Kind, theme.Secondary.Modifier},
		"Highlight":        {theme.Highlight.Fg.Kind, theme.Highlight.Modifier},
		"Regular":          {theme.Regular.Fg.Kind, theme.Regular.Modifier},
		"ProgressFilled":   {theme.ProgressFilled.Fg.Kind, theme.ProgressFilled.Modifier},
		"ProgressUnfilled": {theme.ProgressUnfilled.Fg.Kind, theme.ProgressUnfilled.Modifier},
		"Notice":           {theme.Notice.Fg.Kind, theme.Notice.Modifier},
		"Warning":          {theme.Warning.Fg.Kind, theme.Warning.Modifier},
		"Error":            {theme.Error.Fg.Kind, theme.Error.Modifier},
	}
	for role, w := range want {
		if got[role] != w {
			t.Errorf("theme %s = %+v, want %+v", role, got[role], w)
		}
	}
}

// TestCanonicalNalaConfigParsesToDefaults pins AC-067's other half: the
// canonical nala v0.18.2 nala.conf parses unchanged and resolves to the same
// defaults xbpretty uses.
func TestCanonicalNalaConfigParsesToDefaults(t *testing.T) {
	f, err := os.Open("testdata/nala-v0.18.2.conf")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := Parse("nala.conf", f)
	if err != nil {
		t.Fatalf("Parse canonical nala.conf: %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("canonical nala.conf does not equal defaults:\n got %+v\nwant %+v", cfg, Defaults())
	}
}

// TestAliasesResolveLikeCanonicalTables pins FR-051's lowercase aliases.
func TestAliasesResolveLikeCanonicalTables(t *testing.T) {
	canonical := `[Nala]
assume_yes = true
[Ui]
unit = "Decimal"
[Color]
mode = "Never"
[Color.theme]
Primary = { fg = "Red" }
[Xbps]
install_path = "/opt/xbps-install"
transaction_log = false
`
	aliased := `[nala]
assume_yes = true
[ui]
unit = "Decimal"
[color]
mode = "Never"
[color.theme]
Primary = { fg = "Red" }
[xbps]
install_path = "/opt/xbps-install"
transaction_log = false
`
	want, err := Parse("cfg.toml", strings.NewReader(canonical))
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	got, err := Parse("cfg.toml", strings.NewReader(aliased))
	if err != nil {
		t.Fatalf("aliases: %v", err)
	}
	if got != want {
		t.Errorf("aliases resolved differently:\n got %+v\nwant %+v", got, want)
	}
}

func TestSchemaValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		line    int
		message string
	}{
		{"unknown table", "[Bogus]\nx = true\n", 1, "unknown table [Bogus]"},
		{"unknown nested table", "[Color.extra]\nx = true\n", 1, "unknown table [Color.extra]"},
		{"unknown top-level key", "assume_yes = true\n", 1, "unknown key \"assume_yes\""},
		{"unknown Nala key", "[Nala]\nnope = true\n", 2, `unknown key "nope" in [Nala]`},
		{"misspelled Nala key with integer", "[Nala]\nauto_remov = 1\n", 2, `unknown key "auto_remov" in [Nala]`},
		{"misspelled Nala key with string", "[Nala]\nassume_ye = \"true\"\n", 2, `unknown key "assume_ye" in [Nala]`},
		{"unknown Ui key", "[Ui]\ncolour = \"Auto\"\n", 2, `unknown key "colour" in [Ui]`},
		{"unknown Color key", "[Color]\nmode = \"Auto\"\nprimary = \"Red\"\n", 3, `unknown key "primary" in [Color]`},
		{"unknown Xbps key", "[Xbps]\ninstall = \"xbps-install\"\n", 2, `unknown key "install" in [Xbps]`},
		{"unknown theme role", "[Color.theme]\nPrimaryy = { fg = \"Red\" }\n", 2, `unknown theme role "Primaryy"`},
		{"bool type", "[Nala]\nassume_yes = \"true\"\n", 2, "[Nala] assume_yes must be a boolean, got string"},
		{"int type", "[Nala]\nassume_yes = 1\n", 2, "[Nala] assume_yes must be a boolean, got integer"},
		{"string type", "[Ui]\nmode = true\n", 2, "[Ui] mode must be a string, got boolean"},
		{"array type", "[Xbps]\ninstall_path = []\n", 2, "[Xbps] install_path must be a string, got array"},
		{"bad Ui mode", "[Ui]\nmode = \"auto\"\n", 2, `[Ui] mode must be exactly one of Auto, Plain, Tui, got "auto"`},
		{"bad Ui unit", "[Ui]\nunit = \"binary\"\n", 2, `[Ui] unit must be exactly one of Binary, Decimal, got "binary"`},
		{"bad Color mode", "[Color]\nmode = \"always\"\n", 2, `[Color] mode must be exactly one of Auto, Always, Never, got "always"`},
		{"relative path", "[Xbps]\ninstall_path = \"bin/xbps-install\"\n", 2, `[Xbps] install_path must be a bare program name or an absolute path, got "bin/xbps-install"`},
		{"dot-relative path", "[Xbps]\nquery_path = \"./xbps-query\"\n", 2, "[Xbps] query_path must be a bare program name or an absolute path"},
		{"home path", "[Xbps]\nremove_path = \"~/bin/xbps-remove\"\n", 2, "[Xbps] remove_path must be a bare program name or an absolute path"},
		{"empty path", "[Xbps]\nremove_path = \"\"\n", 2, "[Xbps] remove_path must not be empty"},
		{"control character in path", "[Xbps]\ninstall_path = \"xbps\\u0001\"\n", 2, "[Xbps] install_path must not contain control characters"},
		{"C1 control character in path", "[Xbps]\ninstall_path = \"xbps\\u0085\"\n", 2, "[Xbps] install_path must not contain control characters"},
		{"bare path with arguments", "[Xbps]\ninstall_path = \"xbps-install -S\"\n", 2, "[Xbps] install_path must be a bare program name or an absolute path"},
		{"absolute path with arguments", "[Xbps]\ninstall_path = \"/usr/bin/xbps-install -S\"\n", 2, "[Xbps] install_path must be a bare program name or an absolute path"},
		{"role is not a table", "[Color.theme]\nPrimary = \"Red\"\n", 2, "[Color.theme] Primary must be an inline table, got string"},
		{"missing fg", "[Color.theme]\nPrimary = { modifier = \"BOLD\" }\n", 2, "[Color.theme] Primary is missing 'fg'"},
		{"unknown style key", "[Color.theme]\nPrimary = { fg = \"Red\", color = \"Blue\" }\n", 2, `[Color.theme] Primary has unknown key "color"`},
		{"unknown colour", "[Color.theme]\nPrimary = { fg = \"Chartreuse\" }\n", 2, `[Color.theme] Primary: fg: unknown colour "Chartreuse"`},
		{"bad hex colour", "[Color.theme]\nPrimary = { fg = \"#12345\" }\n", 2, `[Color.theme] Primary: fg: invalid hexadecimal colour "#12345"`},
		{"indexed out of range", "[Color.theme]\nPrimary = { fg = 256 }\n", 2, "[Color.theme] Primary: fg: colour index 256 is outside 0-255"},
		{"indexed negative", "[Color.theme]\nPrimary = { fg = -1 }\n", 2, "[Color.theme] Primary: fg: colour index -1 is outside 0-255"},
		{"rgb wrong arity", "[Color.theme]\nPrimary = { fg = { Rgb = [1, 2] } }\n", 2, "[Color.theme] Primary: fg: Rgb must have exactly 3 components"},
		{"rgb component range", "[Color.theme]\nPrimary = { fg = { Rgb = [1, 2, 300] } }\n", 2, "[Color.theme] Primary: fg: Rgb component 300 is outside 0-255"},
		{"unknown colour form", "[Color.theme]\nPrimary = { fg = { Hsl = [1, 2, 3] } }\n", 2, `[Color.theme] Primary: fg: unknown colour form "Hsl"`},
		{"unknown modifier", "[Color.theme]\nPrimary = { fg = \"Red\", modifier = \"BLINKY\" }\n", 2, `[Color.theme] Primary: unknown modifier "BLINKY"`},
		{"modifier wrong type", "[Color.theme]\nPrimary = { fg = \"Red\", modifier = 1 }\n", 2, "[Color.theme] Primary: modifier must be a string or an array of strings, got integer"},
		{"bad bg colour", "[Color.theme]\nPrimary = { fg = \"Red\", bg = \"Nope\" }\n", 2, `[Color.theme] Primary: bg: unknown colour "Nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("cfg.toml", strings.NewReader(tt.in))
			if err == nil {
				t.Fatal("Parse succeeded, want error")
			}
			pe, ok := err.(*Error)
			if !ok {
				t.Fatalf("got %T %v, want *Error", err, err)
			}
			if pe.Line != tt.line {
				t.Errorf("line = %d, want %d (error: %v)", pe.Line, tt.line, pe)
			}
			if !strings.Contains(pe.Message, tt.message) {
				t.Errorf("message %q does not contain %q", pe.Message, tt.message)
			}
		})
	}
}

// TestStyleModifierForms pins the three accepted modifier spellings: a pipe
// string, an array and the empty forms that mean "no modifier".
func TestStyleModifierForms(t *testing.T) {
	tests := []struct {
		name string
		role string
		want Modifiers
	}{
		{"omitted means bold", `{ fg = "Reset" }`, ModifierBold},
		{"empty string means none", `{ fg = "Reset", modifier = "" }`, 0},
		{"empty array means none", `{ fg = "Reset", modifier = [] }`, 0},
		{"pipe string", `{ fg = "Reset", modifier = "BOLD | ITALIC | REVERSED" }`, ModifierBold | ModifierItalic | ModifierReversed},
		{"array", `{ fg = "Reset", modifier = ["DIM", "HIDDEN", "CROSSED_OUT"] }`, ModifierDim | ModifierHidden | ModifierCrossedOut},
		{"underscored and hyphenated tokens", `{ fg = "Reset", modifier = "slow_blink | rapid-blink" }`, ModifierSlowBlink | ModifierRapidBlink},
		{"strikethrough alias", `{ fg = "Reset", modifier = "STRIKETHROUGH" }`, ModifierCrossedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse("cfg.toml", strings.NewReader("[Color.theme]\nPrimary = "+tt.role+"\n"))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := cfg.Color.Theme.Primary.Modifier; got != tt.want {
				t.Errorf("modifier = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestColourGrammar covers every colour form FR-051 names.
func TestColourGrammar(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Color
	}{
		{"named", `"BrightGreen"`, Color{Kind: ColorLightGreen}},
		{"named with spaces and case", `" bright green "`, Color{Kind: ColorLightGreen}},
		{"named alias", `"brightblack"`, Color{Kind: ColorDarkGray}},
		{"hex", `"#1a2B3c"`, Color{Kind: ColorRGB, R: 0x1a, G: 0x2b, B: 0x3c}},
		{"indexed string", `"201"`, Color{Kind: ColorIndexed, Index: 201}},
		{"indexed integer", `201`, Color{Kind: ColorIndexed, Index: 201}},
		{"rgb table", `{ Rgb = [1, 2, 3] }`, Color{Kind: ColorRGB, R: 1, G: 2, B: 3}},
		{"indexed table", `{ Indexed = 9 }`, Color{Kind: ColorIndexed, Index: 9}},
		{"reset", `"Reset"`, Color{Kind: ColorReset}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse("cfg.toml", strings.NewReader("[Color.theme]\nPrimary = { fg = "+tt.value+" }\n"))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := cfg.Color.Theme.Primary.Fg; got != tt.want {
				t.Errorf("fg = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A table may appear once, but [Color.theme] implicitly creates [Color], so a
// later explicit [Color] is allowed exactly once.
func TestColorAndThemeTableInteraction(t *testing.T) {
	for _, in := range []string{
		"[Color.theme]\nPrimary = { fg = \"Red\" }\n[Color]\nmode = \"Never\"\n",
		"[Color]\nmode = \"Never\"\n[Color.theme]\nPrimary = { fg = \"Red\" }\n",
	} {
		cfg, err := Parse("cfg.toml", strings.NewReader(in))
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if cfg.Color.Mode != ColorNever || cfg.Color.Theme.Primary.Fg.Kind != ColorRed {
			t.Errorf("Parse(%q) = %+v", in, cfg.Color)
		}
	}
	if _, err := Parse("cfg.toml", strings.NewReader("[Color.theme]\nPrimary = { fg = \"Red\" }\n[Color]\n[Color]\n")); err == nil {
		t.Error("a second explicit [Color] was accepted")
	}
}

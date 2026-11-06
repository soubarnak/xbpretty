package config

import (
	"strings"
	"testing"
)

// The parser is tested through Parse, the same seam the loader uses: a TOML
// document in, a resolved Config or a path-and-line error out.
func TestParseSubsetConstructs(t *testing.T) {
	in := "# comment\r\n" +
		"\r\n" +
		"[Nala]\n" +
		"auto_remove = false # trailing comment\n" +
		"auto_update = true\n" +
		"\n" +
		"[Ui]\n" +
		`mode = "Plain"` + "\n" +
		"unit = 'Decimal'\n" +
		"\n" +
		"[Color]\n" +
		`mode = "Always"` + "\n" +
		"\n" +
		"[Color.theme]\n" +
		`Primary = { fg = "Black", bg = "#102030", modifier = "BOLD | ITALIC" }` + "\n" +
		`Secondary = { fg = 201 }` + "\n" +
		`Highlight = { fg = { Rgb = [1, 2, 3] } }` + "\n" +
		`Regular = { fg = "Reset", modifier = [] }` + "\n" +
		`ProgressFilled = { fg = { Indexed = 9 } }` + "\n" +
		`ProgressUnfilled = { fg = "BrightRed", modifier = ["DIM", "UNDERLINED"] }` + "\n" +
		`Notice = { fg = "BrightYellow" }` + "\n" +
		"Warning = { fg = \"BrightYellow\" }\n" +
		"Error = { fg = \"BrightRed\" }\n" +
		"\n" +
		"[Xbps]\n" +
		`install_path = "xbps-install"` + "\n" +
		`remove_path = "/usr/bin/xbps-remove"` + "\n" +
		`query_path = 'xbps-query'` + "\n" +
		`rindex_path = "xbps-rindex"` + "\n" +
		`uhelper_path = "xbps-uhelper"` + "\n" +
		`pkgdb_path = "xbps-pkgdb"` + "\n" +
		"transaction_log = false\n"
	cfg, err := Parse("cfg.toml", strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Nala.AutoRemove || !cfg.Nala.AutoUpdate {
		t.Errorf("Nala = %+v", cfg.Nala)
	}
	if cfg.Ui.Mode != ModePlain || cfg.Ui.Unit != UnitDecimal {
		t.Errorf("Ui = %+v", cfg.Ui)
	}
	if cfg.Color.Mode != ColorAlways {
		t.Errorf("Color.Mode = %q", cfg.Color.Mode)
	}
	if got := cfg.Color.Theme.Primary; got.Fg.Kind != ColorBlack || !got.HasBg || got.Bg.Kind != ColorRGB || got.Bg.R != 0x10 || got.Modifier != ModifierBold|ModifierItalic {
		t.Errorf("Primary = %+v", got)
	}
	if got := cfg.Color.Theme.Secondary; got.Fg.Kind != ColorIndexed || got.Fg.Index != 201 || got.Modifier != ModifierBold {
		t.Errorf("Secondary = %+v", got)
	}
	if got := cfg.Color.Theme.Highlight; got.Fg.Kind != ColorRGB || got.Fg.R != 1 || got.Fg.G != 2 || got.Fg.B != 3 {
		t.Errorf("Highlight = %+v", got)
	}
	if got := cfg.Color.Theme.Regular; got.Modifier != 0 {
		t.Errorf("Regular modifier = %v, want none", got.Modifier)
	}
	if got := cfg.Color.Theme.ProgressFilled; got.Fg.Kind != ColorIndexed || got.Fg.Index != 9 {
		t.Errorf("ProgressFilled = %+v", got)
	}
	if got := cfg.Color.Theme.ProgressUnfilled; got.Modifier != ModifierDim|ModifierUnderlined {
		t.Errorf("ProgressUnfilled modifier = %v", got.Modifier)
	}
	if cfg.Xbps.InstallPath != "xbps-install" || cfg.Xbps.RemovePath != "/usr/bin/xbps-remove" ||
		cfg.Xbps.QueryPath != "xbps-query" || cfg.Xbps.RindexPath != "xbps-rindex" ||
		cfg.Xbps.UhelperPath != "xbps-uhelper" || cfg.Xbps.PkgdbPath != "xbps-pkgdb" ||
		cfg.Xbps.TransactionLog {
		t.Errorf("Xbps = %+v", cfg.Xbps)
	}
}

// TestParseAcceptsArrayTrailingCommaAndTableOrder pins the two TOML-shaped
// freedoms FR-052 grants: one-line arrays may end in a comma, and tables may
// appear in any order.
func TestParseAcceptsArrayTrailingCommaAndTableOrder(t *testing.T) {
	in := `[Xbps]
transaction_log = true

[Ui]
mode = "Auto"

[Color.theme]
Primary = { fg = "Reset", modifier = ["BOLD", "DIM",] }

[Color]
mode = "Never"

[Nala]
assume_yes = true
`
	cfg, err := Parse("cfg.toml", strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Nala.AssumeYes {
		t.Error("assume_yes was not read")
	}
	if cfg.Color.Theme.Primary.Modifier != ModifierBold|ModifierDim {
		t.Errorf("modifier = %v", cfg.Color.Theme.Primary.Modifier)
	}
	if cfg.Color.Mode != ColorNever {
		t.Errorf("Color.Mode = %q", cfg.Color.Mode)
	}
}

// TestParseEmptyAndCommentOnlyMeanDefaults pins FR-052's empty-file rule.
func TestParseEmptyAndCommentOnlyMeanDefaults(t *testing.T) {
	for _, in := range []string{"", "\n\n", "# only a comment\n\n"} {
		cfg, err := Parse("cfg.toml", strings.NewReader(in))
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if cfg != Defaults() {
			t.Errorf("Parse(%q) = %+v, want defaults", in, cfg)
		}
	}
}

func TestParseRejectedConstructs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		line    int
		message string
	}{
		{"float", "[Nala]\nassume_yes = 1.5\n", 2, "floats are not supported"},
		{"float exponent", "[Nala]\nassume_yes = 1e3\n", 2, "floats are not supported"},
		{"float leading dot", "[Nala]\nassume_yes = .5\n", 2, "bare values are not supported; strings must be quoted"},
		{"float leading dot with sign", "[Nala]\nassume_yes = -.5\n", 2, "floats are not supported"},
		{"float with integer part", "[Nala]\nassume_yes = 0.5\n", 2, "floats are not supported"},
		{"date", "[Nala]\nauto_remove = 2026-01-02\n", 2, "dates and times are not supported"},
		{"datetime", "[Nala]\nauto_remove = 2026-01-02T03:04:05Z\n", 2, "dates and times are not supported"},
		{"time", "[Nala]\nauto_remove = 03:04:05\n", 2, "dates and times are not supported"},
		{"multiline basic string", "[Nala]\nassume_yes = \"\"\"true\"\"\"\n", 2, "multiline strings are not supported"},
		{"multiline literal string", "[Nala]\nassume_yes = '''true'''\n", 2, "multiline strings are not supported"},
		{"dotted key", "[Nala]\nauto_remove = true\nauto.remove = true\n", 3, "dotted keys are not supported"},
		{"array of tables", "[[Nala]]\n", 1, "arrays of tables are not supported"},
		{"quoted key", "[Nala]\n\"assume_yes\" = true\n", 2, "quoted keys are not supported"},
		{"quoted table name", "[\"Nala\"]\nassume_yes = true\n", 1, "quoted keys are not supported"},
		{"inf", "[Nala]\nassume_yes = inf\n", 2, "floats are not supported"},
		{"nan", "[Nala]\nassume_yes = nan\n", 2, "floats are not supported"},
		{"literal quoted key", "[Nala]\n'assume_yes' = true\n", 2, "quoted keys are not supported"},
		{"non-ascii bare key", "[Nala]\naütf = true\n", 2, "non-ASCII bare keys are not supported"},
		{"hex integer", "[Xbps]\ninstall_path = 0x10\n", 2, "hexadecimal, octal and binary integers are not supported"},
		{"octal integer", "[Xbps]\ninstall_path = 0o10\n", 2, "hexadecimal, octal and binary integers are not supported"},
		{"binary integer", "[Xbps]\ninstall_path = 0b10\n", 2, "hexadecimal, octal and binary integers are not supported"},
		{"underscored integer", "[Xbps]\ninstall_path = 1_000\n", 2, "underscores in integers are not supported"},
		{"leading zero integer", "[Xbps]\ninstall_path = 01\n", 2, "leading zeros in integers are not supported"},
		{"duplicate key", "[Nala]\nauto_remove = true\nauto_remove = false\n", 3, `duplicate key "auto_remove"`},
		{"duplicate key in inline table", "[Color.theme]\nPrimary = { fg = \"Reset\", fg = \"Red\" }\n", 2, `duplicate key "fg"`},
		{"duplicate table", "[Nala]\n[Nala]\n", 2, "duplicate table [Nala]"},
		{"alternate theme form in Color", "[Color]\ntheme = { Primary = { fg = \"Reset\" } }\n", 2, "[Color] theme is not supported"},
		{"alternate theme subtable", "[Color.theme.Primary]\n", 1, "[Color.theme.Primary] is not supported"},
		{"alternate lowercase theme subtable", "[color.theme.Primary]\n", 1, "[Color.theme.Primary] is not supported"},
		{"missing equals", "[Nala]\nassume_yes\n", 2, "expected key = value"},
		{"missing key", "[Nala]\n= true\n", 2, "expected a key before '='"},
		{"missing value", "[Nala]\nassume_yes =\n", 2, "expected a value after '='"},
		{"unterminated basic string", "[Nala]\nassume_yes = \"true\n", 2, "unterminated string"},
		{"unterminated literal string", "[Nala]\nassume_yes = 'true\n", 2, "unterminated string"},
		{"invalid escape", "[Nala]\nassume_yes = \"tr\\q\"\n", 2, "invalid escape sequence"},
		{"raw control in basic string", "[Color.theme]\nPrimary = { fg = \"Red\x01\" }\n", 2, "control characters are not allowed in strings"},
		{"raw DEL in literal string", "[Color.theme]\nPrimary = { fg = 'Red\x7f' }\n", 2, "control characters are not allowed in strings"},
		{"raw carriage return in basic string", "[Nala]\nassume_yes = \"a\rb\"\n", 2, "control characters are not allowed in strings"},
		{"multi-line array", "[Color.theme]\nPrimary = { fg = [\n", 2, "arrays must be on one line"},
		{"multi-line inline table", "[Color.theme]\nPrimary = {\n", 2, "inline tables must be on one line"},
		{"unterminated array", "[Color.theme]\nPrimary = { fg = [1, 2 }\n", 2, "expected ',' or ']' in array"},
		{"array missing comma", "[Color.theme]\nPrimary = { fg = [1 2] }\n", 2, "expected ',' or ']' in array"},
		{"inline table trailing comma", "[Color.theme]\nPrimary = { fg = \"Reset\", }\n", 2, "inline tables must not have a trailing comma"},
		{"inline table missing comma", "[Color.theme]\nPrimary = { fg = \"Reset\" bg = \"Red\" }\n", 2, "expected ',' or '}' in inline table"},
		{"unterminated table header", "[Nala\nassume_yes = true\n", 1, "unterminated table header"},
		{"empty table header", "[]\n", 1, "expected a table name"},
		{"text after table header", "[Nala] true\n", 1, "unexpected text after table header"},
		{"text after value", "[Nala]\nassume_yes = true false\n", 2, "unexpected text after value"},
		{"bare value", "[Nala]\nassume_yes = maybe\n", 2, "bare values are not supported; strings must be quoted"},
		{"invalid bare key", "[Nala]\nassume$yes = true\n", 2, "invalid bare key"},
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
			if tt.message != "" && !strings.Contains(pe.Message, tt.message) {
				t.Errorf("message %q does not contain %q", pe.Message, tt.message)
			}
			if !strings.HasPrefix(err.Error(), "config cfg.toml:") {
				t.Errorf("error %q does not start with the config path", err)
			}
		})
	}
}

// TOML forbids raw control characters in strings except tab, but the escaped
// forms stay valid and a raw tab is still accepted.
func TestParseStringControlCharacterRules(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"raw tab in basic string", "\"a\tb\"", "a\tb"},
		{"raw tab in literal string", "'a\tb'", "a\tb"},
		{"escaped tab", `"a\tb"`, "a\tb"},
		{"escaped unicode control", `"a\u0001b"`, "a\x01b"},
		{"non-ASCII text", `"café"`, "café"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp := &valueParser{path: "cfg.toml", line: 1, input: tt.in}
			value, err := vp.parseValue()
			if err != nil {
				t.Fatalf("parseValue(%q): %v", tt.in, err)
			}
			if value.kind != tomlString || value.str != tt.want {
				t.Errorf("parseValue(%q) = %q, want %q", tt.in, value.str, tt.want)
			}
		})
	}
}

// The remaining rejections are file-level or size-level, so they carry no
// line number and use the file-level format.
func TestParseFileLevelErrors(t *testing.T) {
	tests := []struct {
		name    string
		in      []byte
		message string
		line    int
	}{
		{"BOM", append([]byte{0xef, 0xbb, 0xbf}, []byte("[Nala]\n")...), "byte-order mark (BOM) is not supported", 0},
		{"invalid UTF-8", []byte("[Nala]\n# \xff\n"), "configuration is not valid UTF-8", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("cfg.toml", strings.NewReader(string(tt.in)))
			pe, ok := err.(*Error)
			if !ok {
				t.Fatalf("got %T %v, want *Error", err, err)
			}
			if pe.Line != tt.line {
				t.Errorf("line = %d, want %d", pe.Line, tt.line)
			}
			if !strings.Contains(pe.Message, tt.message) {
				t.Errorf("message %q does not contain %q", pe.Message, tt.message)
			}
			if got, want := err.Error(), "config cfg.toml: "; !strings.HasPrefix(got, want) {
				t.Errorf("file-level error %q, want prefix %q", got, want)
			}
		})
	}
}

func TestParseRejectsOversizedFile(t *testing.T) {
	line := "# " + strings.Repeat("x", 512) + "\n"
	in := strings.Repeat(line, maxConfigBytes/len(line)+1)
	_, err := Parse("cfg.toml", strings.NewReader(in))
	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("got %T %v, want *Error", err, err)
	}
	if pe.Line != 0 || !strings.Contains(pe.Message, "larger than 1 MiB") {
		t.Errorf("error = %v", err)
	}
}

func TestParseRejectsOversizedLine(t *testing.T) {
	in := strings.Repeat(" ", maxLineBytes+1) + "\n"
	_, err := Parse("cfg.toml", strings.NewReader(in))
	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("got %T %v, want *Error", err, err)
	}
	if pe.Line != 1 || !strings.Contains(pe.Message, "longer than 1 MiB") {
		t.Errorf("error = %v", err)
	}
}

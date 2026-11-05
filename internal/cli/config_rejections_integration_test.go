package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestTOMLOutsideSubsetExitsTwo pins AC-067 at the built-binary seam: every
// construct outside the supported subset is a path-and-line config error with
// exit 2, and the canonical nala file still parses.
func TestTOMLOutsideSubsetExitsTwo(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	cases := []struct {
		name, body, message string
	}{
		{"float", "[Nala]\nauto_remove = 1.5\n", "floats are not supported"},
		{"date", "[Nala]\nauto_remove = 2026-01-02\n", "dates and times are not supported"},
		{"multiline string", "[Nala]\nauto_remove = \"\"\"true\"\"\"\n", "multiline strings are not supported"},
		{"dotted key", "[Nala]\nauto_remove = true\nauto.remove = true\n", "dotted keys are not supported"},
		{"array of tables", "[[Nala]]\n", "arrays of tables are not supported"},
		{"quoted key", "[Nala]\n\"auto_remove\" = true\n", "quoted keys are not supported"},
		{"theme table form", "[Color]\ntheme = { Primary = { fg = \"Reset\" } }\n", "[Color] theme is not supported"},
		{"theme subtable form", "[Color.theme.Primary]\n", "[Color.theme.Primary] is not supported"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-")+".toml")
			if err := writeFile(path, tt.body); err != nil {
				t.Fatal(err)
			}
			out, errs, code := runBinary(t, bin, env, "--config", path, "log")
			if code != 2 {
				t.Fatalf("exit = %d, want 2\n%s", code, errs)
			}
			if out != "" {
				t.Errorf("config error wrote to stdout:\n%s", out)
			}
			prefix := fmt.Sprintf("xbpretty: config %s:", path)
			if !strings.Contains(errs, prefix) {
				t.Errorf("error does not name the path:\n%s", errs)
			}
			if !strings.Contains(errs, tt.message) {
				t.Errorf("error does not contain %q:\n%s", tt.message, errs)
			}
		})
	}
}

func TestCanonicalExampleParsesAtTheBuiltBinary(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	example, err := filepath.Abs("../../docs/config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	_, errs, code := runBinary(t, bin, env, "--debug", "--config", example, "log")
	if code != 0 {
		t.Fatalf("the canonical example was rejected: exit %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "unit=Binary") || !strings.Contains(errs, "transaction_log=true") {
		t.Errorf("the canonical example did not resolve to the defaults:\n%s", errs)
	}
}

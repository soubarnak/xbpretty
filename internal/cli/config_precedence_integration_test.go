package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPrecedenceDebugReportsEffectiveValues drives the two halves of FR-057 at
// the built-binary seam through --debug's resolved-effective line.
func TestPrecedenceDebugReportsEffectiveValues(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	cfgPath := filepath.Join(dir, "cfg.toml")
	if err := writeFile(cfgPath, `[Nala]
assume_yes = true
auto_remove = false
full_upgrade = true

[Ui]
mode = "Tui"

[Color]
mode = "Always"

[Xbps]
transaction_log = true
`); err != nil {
		t.Fatal(err)
	}

	// No flags: the file decides.
	_, errs, code := runBinary(t, bin, env, "--debug", "--config", cfgPath, "log")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "resolved effective mode=Tui unit=Binary color=Always assume_yes=true auto_remove=false simple=false full=true sync=true log=true verbose=true history_dir=/var/lib/xbpretty/history") {
		t.Errorf("file values were not effective:\n%s", errs)
	}

	// Every applicable flag overrides its file value.
	_, errs, code = runBinary(t, bin, env,
		"--debug", "--config", cfgPath, "--no-color", "--no-tui", "--no-update",
		"--assume-no", "--no-full", "--no-log", "--history-dir", "/tmp/xbp-history", "log")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "resolved effective mode=Auto unit=Binary color=Never assume_yes=false auto_remove=false simple=false full=false sync=false log=false verbose=true history_dir=/tmp/xbp-history") {
		t.Errorf("CLI values did not override the file:\n%s", errs)
	}
}

// TestPrecedencePositiveFlags covers the CLI-over-file pairs whose flags only
// turn a value on.
func TestPrecedencePositiveFlags(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	cfgPath := filepath.Join(dir, "cfg.toml")
	if err := writeFile(cfgPath, `[Nala]
assume_yes = false
auto_remove = false
auto_update = false
simple = false

[Ui]
mode = "Auto"

[Color]
mode = "Never"
`); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runBinary(t, bin, env, "--debug", "--config", cfgPath,
		"-y", "--update", "--simple", "--auto-remove", "--no-tui", "install", "-n", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "resolved effective mode=Auto unit=Binary color=Never assume_yes=true auto_remove=true simple=true full=false sync=true log=true") {
		t.Errorf("positive CLI flags did not override the file:\n%s", errs)
	}
}

func TestColorNoColorConflict(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "--color=always", "--no-color", "log")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, errs)
	}
	if !strings.Contains(errs, "--color and --no-color are mutually exclusive") {
		t.Errorf("missing the FR-003 conflict line:\n%s", errs)
	}
}

func TestCountryFlagHint(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	tests := []struct {
		argv []string
		flag string
	}{
		{[]string{"install", "-c", "IN", "figlet"}, "unknown flag -c"},
		{[]string{"-c", "IN", "install", "figlet"}, "unknown flag -c"},
		{[]string{"install", "-c=IN", "figlet"}, "unknown flag -c"},
		{[]string{"install", "--country", "IN", "figlet"}, "unknown flag --country"},
		{[]string{"install", "-yc", "IN", "figlet"}, "unknown flag -c"},
		{[]string{"-yc", "IN", "install", "figlet"}, "unknown flag -c"},
	}
	for _, tt := range tests {
		_, errs, code := runBinary(t, bin, env, tt.argv...)
		if code != 2 {
			t.Fatalf("%v: exit = %d, want 2\n%s", tt.argv, code, errs)
		}
		if !strings.Contains(errs, tt.flag) {
			t.Errorf("%v: missing %q:\n%s", tt.argv, tt.flag, errs)
		}
		if !strings.Contains(errs, "xbpretty: -c is fetch's --country; config is --config") {
			t.Errorf("%v: missing the FR-059 hint:\n%s", tt.argv, errs)
		}
	}
}

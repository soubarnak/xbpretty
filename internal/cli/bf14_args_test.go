package cli

import (
	"strings"
	"testing"
)

// BF-14: a boolean long flag takes no value. `--assume-yes=false` used to set
// assume-yes and `--dry-run=no` turned a dry run on; both are now a usage error
// (exit 2) raised before any xbps child runs.
func TestBoolLongFlagRejectsValue(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		flag string
	}{
		{"yes=false", []string{"install", "--yes=false", "figlet"}, "yes"},
		{"assume-yes=false", []string{"install", "--assume-yes=false", "figlet"}, "assume-yes"},
		{"dry-run=1", []string{"install", "--dry-run=1", "figlet"}, "dry-run"},
		{"dry-run=no", []string{"remove", "--dry-run=no", "figlet"}, "dry-run"},
		{"all=0 on history clear", []string{"history", "clear", "--all=0"}, "all"},
		{"no-color= with an empty value", []string{"list", "--no-color="}, "no-color"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			code, out, errs := e.run(append([]string{"xbp"}, tt.argv...)...)
			if code != 2 || out != "" {
				t.Fatalf("code=%d out=%q err=%q, want exit 2 and empty stdout", code, out, errs)
			}
			want := "--" + tt.flag + " does not take a value"
			if !strings.Contains(errs, want) {
				t.Errorf("stderr = %q, want it to contain %q", errs, want)
			}
			if calls := e.commands(); len(calls) != 0 {
				t.Errorf("xbps ran: %v", calls)
			}
		})
	}
}

// The boolean flags keep working without a value, in both spellings of the
// short and long forms, and the value flags still take `=value`.
func TestBoolFlagsStillWorkWithoutAValue(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"long no-color before the command", []string{"xbp", "--no-color", "install", "--dry-run", "figlet"}},
		{"short -v before the command", []string{"xbp", "-v", "install", "--dry-run", "figlet"}},
		{"long dry-run after the command", []string{"xbp", "install", "--dry-run", "figlet"}},
		{"value flag keeps its equals form", []string{"xbp", "--color=never", "install", "--dry-run", "figlet"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			code, _, errs := e.run(tt.argv...)
			if code != 0 {
				t.Fatalf("code=%d, want 0\n%s", code, errs)
			}
		})
	}
}

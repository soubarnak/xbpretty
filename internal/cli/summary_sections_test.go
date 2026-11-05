package cli

import (
	"strings"
	"testing"
)

// TestRemoveFamilyPurgeLabels covers FR-009's equivalence: a remove-family
// command with --purge renders nala's Purge/AutoPurge section, not Remove.
func TestRemoveFamilyPurgeLabels(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"remove --purge", []string{"remove", "--purge", "--dry-run", "wget"}, "Purge"},
		{"autoremove --purge", []string{"autoremove", "--purge", "--dry-run"}, "AutoPurge"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			e.set("XB_STUB_STDOUT", removePlanFixture)
			code, out, errs := e.run(append([]string{"xbpretty"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if !strings.Contains(out, "\n "+tt.want+"\n") {
				t.Errorf("missing %s section:\n%s", tt.want, out)
			}
			if !strings.Contains(out, tt.want+" 1") {
				t.Errorf("footer is missing the %s count:\n%s", tt.want, out)
			}
			if strings.Contains(out, "\n Remove\n") {
				t.Errorf("--purge still renders the Remove section:\n%s", out)
			}
		})
	}
}

// TestRuleWidthFallsBackFromTTYToColumns pins FR-062's documented chain:
// terminal width, then a positive COLUMNS, then 80. The test harness reports
// a TTY but has no ioctl width, so COLUMNS must be honoured.
func TestRuleWidthFallsBackFromTTYToColumns(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.set("COLUMNS", "100")
	code, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	first, _, _ := strings.Cut(out, "\n")
	if first != strings.Repeat("=", 100) {
		t.Errorf("rule width = %d, want COLUMNS=100\n%s", len(first), out)
	}

	e.set("COLUMNS", "nonsense")
	code, out, errs = e.run("xbpretty", "install", "figlet", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	first, _, _ = strings.Cut(out, "\n")
	if first != strings.Repeat("=", 80) {
		t.Errorf("invalid COLUMNS: rule width = %d, want 80\n%s", len(first), out)
	}
}

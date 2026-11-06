package cli

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plainTestOutput removes the SGR prefixes the themed diagnostic helpers add,
// so a PTY test can compare the message text regardless of its colour setting.
func plainTestOutput(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// corpusPlans are the captured multi-row plans the cli harness renders, with
// the command each was captured from.
var corpusPlans = []struct{ capture, command string }{
	{"0607/upgrade.txt", "upgrade"},
	{"0607/configure.txt", "upgrade"},
	{"0607/hold.txt", "upgrade"},
	{"0607/autoremove.txt", "autoremove"},
	{"0591/upgrade.txt", "upgrade"},
}

// capturedSummary renders a captured plan through the cli harness.
func capturedSummary(t *testing.T, capture, command, color string) string {
	t.Helper()
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", planCapture(t, capture))
	code, out, errs := e.run("xbpretty", command, "--dry-run", "--color="+color)
	if code != 0 {
		t.Fatalf("%s: exit = %d, want 0\n%s", capture, code, errs)
	}
	return out
}

// holdPlanSummary renders the captured `hold` plan through the cli harness.
func holdPlanSummary(t *testing.T, color string) string {
	t.Helper()
	return capturedSummary(t, "0607/hold.txt", "upgrade", color)
}

// tokenCount counts the whitespace-separated tokens of out equal to name, so
// acl does not match a package called acl-devel or a word containing "acl".
func tokenCount(out, name string) int {
	n := 0
	for _, token := range strings.Fields(ansiEscape.ReplaceAllString(out, "")) {
		if token == name {
			n++
		}
	}
	return n
}

// A held package is listed once, under Held, and no `held:` note repeats it,
// with colour on or off (ADR-0007).
func TestHeldPackageAppearsOnceUnderHeld(t *testing.T) {
	for _, color := range []string{"never", "always"} {
		out := holdPlanSummary(t, color)
		if strings.Contains(out, "held:") {
			t.Errorf("color=%s: a held: note is printed:\n%s", color, out)
		}
		if n := tokenCount(out, "acl"); n != 1 {
			t.Errorf("color=%s: acl appears %d times, want 1:\n%s", color, n, out)
		}
		hold := strings.Index(out, "Held")
		if hold < 0 || strings.Index(out, "acl") < hold {
			t.Errorf("color=%s: acl is not under Held:\n%s", color, out)
		}
	}
}

// No captured plan prints a held: note, with colour on or off, so the check
// covers every shape of plan, not only the hold one.
func TestCorpusSummariesHaveNoHeldNote(t *testing.T) {
	for _, plan := range corpusPlans {
		for _, color := range []string{"never", "always"} {
			out := capturedSummary(t, plan.capture, plan.command, color)
			if strings.Contains(out, "held:") {
				t.Errorf("%s color=%s: a held: note is printed:\n%s", plan.capture, color, out)
			}

		}
	}
}

// Table rows carry exactly one trailing space (trailing cell padding), while
// labels, rules, counts and size lines carry none; the columns of a group
// still start in the same place on every row.
func TestSummaryTableLinesHaveOneTrailingSpace(t *testing.T) {
	out := holdPlanSummary(t, "never")
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, "  ") {
			t.Errorf("line ends in more than one space: %q", line)
		}
		if isTableLine(line) {
			if !strings.HasSuffix(line, " ") {
				t.Errorf("table row has no trailing space: %q", line)
			}
		} else if strings.HasSuffix(line, " ") && line != "" {
			t.Errorf("non-table line ends in whitespace: %q", line)
		}
	}
	// libxcrypt and sqlite are Install rows; their version column must align.
	a, b := rowLine(out, "libxcrypt"), rowLine(out, "sqlite")
	if a == "" || b == "" {
		t.Fatalf("install rows missing:\n%s", out)
	}
	if strings.Index(a, "4.4.38_1") != strings.Index(b, "3.53.4_1") {
		t.Errorf("version columns do not align:\n%q\n%q", a, b)
	}
}

// isTableLine reports whether a summary line is a borderless table row: it
// starts with the table's one-cell left padding and is not a label, the
// Summary heading, a count or a size line.
func isTableLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(line, " ") {
		return false
	}
	if strings.HasPrefix(trimmed, "Package:") {
		return true
	}
	switch trimmed {
	case "Summary", "Remove", "AutoRemove", "Purge", "AutoPurge", "Install", "ReInstall", "Upgrade", "Configure", "Held":
		return false
	}
	for _, prefix := range []string{"Total download size:", "Disk space required:", "Disk space to free:"} {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 2 {
		if _, err := strconv.Atoi(fields[1]); err == nil {
			return false
		}
	}
	return true
}

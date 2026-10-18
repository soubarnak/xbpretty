package cli

import (
	"regexp"
	"strings"
	"testing"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

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
	code, out, errs := e.run("xbpretty", command, "-n", "--color="+color)
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

// A held package is listed once, under Hold, and no `held:` note repeats it,
// with colour on or off (ADR-0007).
func TestHeldPackageAppearsOnceUnderHold(t *testing.T) {
	for _, color := range []string{"never", "always"} {
		out := holdPlanSummary(t, color)
		if strings.Contains(out, "held:") {
			t.Errorf("color=%s: a held: note is printed:\n%s", color, out)
		}
		if n := tokenCount(out, "acl"); n != 1 {
			t.Errorf("color=%s: acl appears %d times, want 1:\n%s", color, n, out)
		}
		hold := strings.Index(out, "Hold")
		if hold < 0 || strings.Index(out, "acl") < hold {
			t.Errorf("color=%s: acl is not under Hold:\n%s", color, out)
		}
	}
}

// No captured plan prints a held: note or a line that ends in whitespace,
// with colour on or off, so the check covers every shape of plan, not only the
// hold one.
func TestCorpusSummariesHaveNoHeldNoteOrTrailingWhitespace(t *testing.T) {
	for _, plan := range corpusPlans {
		for _, color := range []string{"never", "always"} {
			out := capturedSummary(t, plan.capture, plan.command, color)
			if strings.Contains(out, "held:") {
				t.Errorf("%s color=%s: a held: note is printed:\n%s", plan.capture, color, out)
			}
			for _, text := range []string{out, ansiEscape.ReplaceAllString(out, "")} {
				for _, line := range strings.Split(text, "\n") {
					if line != strings.TrimRight(line, " \t") {
						t.Errorf("%s color=%s: line ends in whitespace: %q", plan.capture, color, line)
					}
				}
			}
		}
	}
}

// No summary line ends in whitespace, and the columns of a group still start
// in the same place on every row.
func TestSummaryLinesHaveNoTrailingWhitespace(t *testing.T) {
	out := holdPlanSummary(t, "never")
	for _, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line ends in whitespace: %q", line)
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

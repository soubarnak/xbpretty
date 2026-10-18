package cli

import (
	"strings"
	"testing"
)

// holdPlanSummary renders the captured `hold` plan through the cli harness.
func holdPlanSummary(t *testing.T, color string) string {
	t.Helper()
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", planCapture(t, "0607/hold.txt"))
	code, out, errs := e.run("xbpretty", "upgrade", "-n", "--color="+color)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	return out
}

// A held package is listed once, under Hold, and no `held:` note repeats it,
// with colour on or off (ADR-0007).
func TestHeldPackageAppearsOnceUnderHold(t *testing.T) {
	for _, color := range []string{"never", "always"} {
		out := holdPlanSummary(t, color)
		if strings.Contains(out, "held:") {
			t.Errorf("color=%s: a held: note is printed:\n%s", color, out)
		}
		if n := strings.Count(out, "acl"); n != 1 {
			t.Errorf("color=%s: acl appears %d times, want 1:\n%s", color, n, out)
		}
		hold := strings.Index(out, "Hold")
		if hold < 0 || strings.Index(out, "acl") < hold {
			t.Errorf("color=%s: acl is not under Hold:\n%s", color, out)
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

package hints

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Hint is a prepared follow-up line for a known xbps failure. A hint fires
// only when both its exit code and its stderr substring match; a failure that
// matches nothing gets no hint at all.
type Hint struct {
	// Name is the hint's identifier, used by the fixture tests.
	Name string
	// ExitCodes are the exit codes this failure appears with.
	ExitCodes []int
	// StderrContains is a short substring of xbps stderr, never a whole line.
	StderrContains string
	// Text is the one-line hint printed after xbps's own output.
	Text string
}

// Table is every xbps failure xbpretty knows how to explain, in match order:
// the first hint whose exit code and substring both match wins.
var Table = []Hint{
	{
		// A stale index fails an install (exit 2) but leaves an upgrade with an
		// empty plan and exit 0, so both exit codes are recorded: either way the
		// error text is real and the index needs refreshing.
		Name:           "stale-index",
		ExitCodes:      []int{0, 2},
		StderrContains: "failed to open repodata",
		Text:           "the repository index could not be read; run `xbp update` to refresh it.",
	},
	{
		Name:           "xbps-unsigned-update",
		ExitCodes:      []int{16},
		StderrContains: "must be updated, please run",
		Text:           "xbps itself must be updated first: `xbps-install -u xbps`.",
	},
	{
		Name:           "not-found",
		ExitCodes:      []int{2},
		StderrContains: "not found in repository pool",
		Text:           "the package is not in any repository; check the name, then `xbp update` to refresh the index.",
	},
	{
		Name:           "not-installed",
		ExitCodes:      []int{2},
		StderrContains: "is not currently installed",
		Text:           "the package is not installed, so there is nothing to remove.",
	},
	{
		Name:           "lock-wait",
		ExitCodes:      []int{0},
		StderrContains: "package database locked",
		Text:           "another xbps process is using the package database; xbpretty is waiting for it to finish.",
	},
}

// Lookup returns the first hint matching an xbps exit code and its stderr, or
// nil when nothing matches. Both must match: neither alone fires a hint.
func Lookup(exitCode int, stderr string) *Hint {
	if stderr == "" {
		return nil
	}
	for i := range Table {
		h := &Table[i]
		if !containsExit(h.ExitCodes, exitCode) {
			continue
		}
		if strings.Contains(stderr, h.StderrContains) {
			return h
		}
	}
	return nil
}

func containsExit(codes []int, want int) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

// ParseRevdeps parses the output of `xbps-query -X <pkg>`: one package
// version per line. Blank lines are skipped; any line that is not a package
// version is an error naming the line.
func ParseRevdeps(r io.Reader) ([]string, error) {
	var dependents []string
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if _, _, ok := strings.Cut(line, "-"); !ok {
			return nil, fmt.Errorf("reverse dependency line %d: %q is not a package version", lineNo, line)
		}
		dependents = append(dependents, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading reverse dependencies: %w", err)
	}
	return dependents, nil
}

// ForceRemoveHint renders the FR-018 follow-up line for a failed remove:
// which packages depend on the target, and what forcing the removal breaks.
func ForceRemoveHint(pkg string, dependents []string) string {
	if len(dependents) == 0 {
		return ""
	}
	return fmt.Sprintf("%d package(s) depend on %s: %s. "+
		"Use `xbp remove -- -F %s` to force removal; that will break those dependents.",
		len(dependents), pkg, strings.Join(dependents, ", "), pkg)
}

package hints

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Stream is one of the two output streams of an xbps command.
type Stream int

const (
	Stderr Stream = iota
	Stdout
)

// Output is what one xbps command printed, kept per stream because xbps does
// not write every failure to the same one.
type Output struct {
	Stdout string
	Stderr string
}

func (o Output) on(s Stream) string {
	if s == Stdout {
		return o.Stdout
	}
	return o.Stderr
}

// Hint is a prepared follow-up line for a known xbps failure. A hint fires
// only when both its exit code and its substring match, on a stream xbps
// writes that failure to; a failure that matches nothing gets no hint at all.
type Hint struct {
	// Name is the hint's identifier, used by the fixture tests.
	Name string
	// ExitCodes are the exit codes this failure appears with.
	ExitCodes []int
	// Streams are the streams xbps has been measured writing this failure to.
	// The substring is matched on those and no others, so a package name in a
	// plan on stdout cannot fire a hint that only ever appears on stderr.
	Streams []Stream
	// Contains is a short substring of the xbps message, never a whole line.
	Contains string
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
		Name:      "stale-index",
		ExitCodes: []int{0, 2},
		Streams:   []Stream{Stderr},
		Contains:  "failed to open repodata",
		Text:      "the repository index could not be read; run `xbp update` to refresh it.",
	},
	{
		// xbps 0.59.1 writes this to stderr when asked to install named
		// packages and to stdout when asked to upgrade (ADR-0006).
		Name:      "xbps-unsigned-update",
		ExitCodes: []int{16},
		Streams:   []Stream{Stderr, Stdout},
		Contains:  "must be updated, please run",
		Text:      "xbps itself must be updated first: `xbps-install -u xbps`.",
	},
	{
		Name:      "not-found",
		ExitCodes: []int{2},
		Streams:   []Stream{Stderr},
		Contains:  "not found in repository pool",
		Text:      "the package is not in any repository; check the name, then `xbp update` to refresh the index.",
	},
	{
		// xbps-remove prints this on stdout and nothing on stderr (ADR-0006).
		Name:      "not-installed",
		ExitCodes: []int{2},
		Streams:   []Stream{Stdout},
		Contains:  "is not currently installed",
		Text:      "the package is not installed, so there is nothing to remove.",
	},
	{
		Name:      "lock-wait",
		ExitCodes: []int{0},
		Streams:   []Stream{Stderr},
		Contains:  "package database locked",
		Text:      "another xbps process is using the package database; xbpretty is waiting for it to finish.",
	},
}

// Lookup returns the first hint matching an xbps exit code and its output, or
// nil when nothing matches. Both must match: neither alone fires a hint. A
// caller that never saw a stream leaves it empty, and the hints that live
// only on that stream stay silent.
func Lookup(exitCode int, out Output) *Hint {
	for i := range Table {
		h := &Table[i]
		if !containsExit(h.ExitCodes, exitCode) {
			continue
		}
		for _, stream := range h.Streams {
			if strings.Contains(out.on(stream), h.Contains) {
				return h
			}
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

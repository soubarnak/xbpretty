// Package completion generates the shell completion scripts from the shared
// command table in internal/spec (FR-005).
package completion

import (
	"github.com/soubarnak/xbpretty/internal/spec"
)

// Subcommands are the visible commands, in help order.
var Subcommands = func() []string {
	var names []string
	for _, c := range spec.VisibleCommands() {
		names = append(names, c.Name)
	}
	return names
}()

// Flags is every accepted, advertised flag spelling. The script generator
// places each spelling under the commands that accept it; N/A, no-op and
// hidden flags are deliberately absent.
var Flags = advertisedSpellings()

func advertisedSpellings() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, f := range spec.Flags {
		if f.Hidden || f.Refusal != "" {
			continue
		}
		for _, s := range spec.FlagSpellings(f) {
			add(s)
		}
	}
	return out
}

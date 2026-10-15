// Package completion generates the shell completion scripts for FR-024.
package completion

import (
	"os/exec"
	"strings"
)

// Subcommands xbpretty dispatches on, in help order.
var Subcommands = []string{
	"install", "remove", "upgrade", "autoremove", "update", "completion", "link",
}

// Flags accepted by xbpretty's own argument parser.
var Flags = []string{
	"-n", "--dry-run", "-y", "--yes", "-v", "--verbose",
	"--no-sync", "--no-log", "--color", "--version",
}

// List runs an xbps-query command and returns its package names, one per
// line. Any failure yields no names rather than an error: completion must
// never make the shell complain.
func List(args ...string) []string {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := stripVersion(packageField(line))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// packageField extracts the name-version token from an xbps-query line, which
// is either its second column or, for property output, the only column.
func packageField(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		return fields[1]
	}
	return fields[0]
}

// stripVersion removes the trailing -<version> from a name-version token.
func stripVersion(token string) string {
	cut := strings.LastIndex(token, "-")
	if cut <= 0 {
		return ""
	}
	return token[:cut]
}

// Installed lists the packages installed on the system, for `remove`.
func Installed() []string {
	return List("xbps-query", "-l")
}

// Available lists the packages available in the repositories, for `install`.
// The prefix is the word being completed, so the search stays scoped.
func Available(prefix string) []string {
	return List("xbps-query", "-Rs", prefix)
}

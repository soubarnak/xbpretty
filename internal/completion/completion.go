// Package completion generates the shell completion scripts for FR-024.
package completion

// Subcommands xbpretty dispatches on, in help order.
var Subcommands = []string{
	"install", "remove", "upgrade", "autoremove", "update", "completion", "link", "log",
}

// Flags accepted by xbpretty's own argument parser.
var Flags = []string{
	"-n", "--dry-run", "-y", "--yes", "-v", "--verbose",
	"--no-sync", "--no-log", "--color", "--version",
}

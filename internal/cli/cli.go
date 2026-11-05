// Package cli holds xbpretty's argument dispatch and command implementations.
package cli

import (
	"path/filepath"
	"strings"

	"github.com/soubarnak/xbpretty/internal/spec"
)

// Version is xbpretty's own version string. Release builds set it with
// -ldflags "-X github.com/soubarnak/xbpretty/internal/cli.Version=X.Y.Z".
var Version = "dev"

// Commands lists the visible subcommands, in help order. The table itself is
// internal/spec; this stays for callers that only need the names.
var Commands = visibleCommandNames()

func visibleCommandNames() []string {
	var names []string
	for _, c := range spec.VisibleCommands() {
		names = append(names, c.Name)
	}
	return names
}

// commandFromArgv reads the command from argv[0]. Only the closed six-name
// set resolves: xbp is the dispatcher and the five xbp-<verb> links fix
// their verb. No other xbp-* name is a command (FR-001).
func commandFromArgv(argv0 string) (cmd string, ok bool) {
	base := filepath.Base(argv0)
	base = strings.TrimSuffix(base, ".exe")
	if base == "xbpretty" || base == "xbp" {
		return "", false
	}
	switch base {
	case "xbp-install":
		return "install", true
	case "xbp-remove":
		return "remove", true
	case "xbp-upgrade":
		return "upgrade", true
	case "xbp-autoremove":
		return "autoremove", true
	case "xbp-update":
		return "update", true
	}
	return "", false
}

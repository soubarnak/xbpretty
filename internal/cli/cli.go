// Package cli holds xbpretty's argument dispatch and command implementations.
package cli

import (
	"path/filepath"
	"strings"
)

// Version is xbpretty's own version string. Release builds set it with
// -ldflags "-X github.com/soubarnak/xbpretty/internal/cli.Version=vX.Y.Z".
var Version = "dev"

// Commands are the subcommands xbpretty dispatches on.
var Commands = []string{"install", "remove", "upgrade", "autoremove", "update", "completion", "link"}

// aliases maps a subcommand name to the words that select it.
var aliases = map[string]string{
	"install":    "install",
	"remove":     "remove",
	"upgrade":    "upgrade",
	"autoremove": "autoremove",
	"update":     "update",
	"up":         "upgrade",
	"dist-up":    "upgrade",
	"clean":      "autoremove",
}

// commandFromArgv reads the command from argv[0], honouring the xbp and
// xbp-* link names. ok is false when the binary name names no command.
func commandFromArgv(argv0 string) (cmd string, ok bool) {
	base := filepath.Base(argv0)
	base = strings.TrimSuffix(base, ".exe")
	if base == "xbpretty" || base == "xbp" {
		return "", false
	}
	if strings.HasSuffix(base, "-completion") || base == "completion" {
		return "completion", true
	}
	if strings.HasSuffix(base, "-link") {
		return "link", true
	}
	if !strings.HasPrefix(base, "xbp-") {
		return "", false
	}
	name := strings.TrimPrefix(base, "xbp-")
	cmd, ok = aliases[name]
	return cmd, ok
}

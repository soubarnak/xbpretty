// Package spec is the single source of truth for xbpretty's command surface:
// the canonical command names, their aliases, the closed set of argv[0] link
// names, and every flag with the commands that accept it. Parsing, --help and
// the shell completions all derive from this table.
package spec

import (
	"fmt"
	"strings"
)

// ArgMode says how many positional arguments a command takes.
type ArgMode int

const (
	NoArgs ArgMode = iota
	OptionalArgs
	RequiredArgs
)

// FlagKind is how a flag consumes its value.
type FlagKind int

const (
	BoolFlag FlagKind = iota
	ValueFlag
	OptionalValueFlag
)

// Flag is one command-line flag. A flag is either global (accepted on every
// command) or listed by the canonical commands in Commands.
type Flag struct {
	Long      string   // canonical long name, without the leading --
	Short     rune     // optional short name, 0 when there is none
	Aliases   []string // extra long names, without the leading --
	Kind      FlagKind
	ValueName string   // FILE, WHEN, N, ... for the help text
	Values    []string // allowed values for an enum flag
	Summary   string
	Hidden    bool     // not advertised in help or completions
	Global    bool     // accepted on every command
	Commands  []string // canonical commands that accept a non-global flag
	Refusal   string   // non-empty: accepted here, refused before dispatch
	NoOp      bool     // accepted and silently does nothing (FR-019)
}

// Command is one canonical command.
type Command struct {
	Name      string
	Aliases   []string
	Summary   string
	UsageArgs string // e.g. "PKG..." or "[DIR]"
	Args      ArgMode
	Group     string
	Hidden    bool
	Flags     []string // canonical flag long names accepted in addition to globals
}

// Groups keep the help and completions in a stable, readable order.
const (
	GroupTransaction = "Transaction"
	GroupDiscovery   = "Discovery"
	GroupHistory     = "History"
	GroupFetch       = "Fetch"
	GroupTooling     = "Tooling"
)

func transactionNames() []string {
	return []string{"install", "remove", "purge", "autoremove", "autopurge", "upgrade", "full-upgrade", "safe-upgrade"}
}

func upgradeNames() []string {
	return []string{"upgrade", "full-upgrade", "safe-upgrade"}
}

// Commands is the canonical command table, in help order.
var Commands = []Command{
	{Name: "install", Summary: "install packages from repositories", UsageArgs: "PKG...", Args: RequiredArgs, Group: GroupTransaction,
		Flags: commandFlags("install")},
	{Name: "remove", Aliases: []string{"uninstall"}, Summary: "remove installed packages", UsageArgs: "PKG...", Args: RequiredArgs, Group: GroupTransaction,
		Flags: commandFlags("remove")},
	{Name: "purge", Summary: "remove packages and their configuration files", UsageArgs: "PKG...", Args: RequiredArgs, Group: GroupTransaction,
		Flags: commandFlags("purge")},
	{Name: "autoremove", Summary: "remove orphaned packages", Group: GroupTransaction,
		Flags: commandFlags("autoremove")},
	{Name: "autopurge", Summary: "remove orphaned packages and their configuration files", Group: GroupTransaction,
		Flags: commandFlags("autopurge")},
	{Name: "upgrade", Aliases: []string{"up", "dist-up"}, Summary: "upgrade all installed packages", Group: GroupTransaction,
		Flags: commandFlags("upgrade")},
	{Name: "full-upgrade", Aliases: []string{"dist-upgrade"}, Summary: "upgrade all installed packages (same as upgrade)", Group: GroupTransaction,
		Flags: commandFlags("full-upgrade")},
	{Name: "safe-upgrade", Summary: "upgrade all installed packages (same as upgrade)", Group: GroupTransaction,
		Flags: commandFlags("safe-upgrade")},
	{Name: "update", Summary: "refresh the repository index", Group: GroupTransaction},
	{Name: "download", Summary: "download package archives into the current directory", UsageArgs: "PKG...", Args: RequiredArgs, Group: GroupTransaction},
	{Name: "clean", Summary: "clean the package cache", Group: GroupTransaction, Flags: []string{"lists", "fetch"}},
	{Name: "list", Summary: "list packages matching the given globs", UsageArgs: "[PKG...]", Args: OptionalArgs, Group: GroupDiscovery,
		Flags: []string{"all-versions", "all-arches", "installed", "upgradable", "virtual", "description", "summary", "machine"}},
	{Name: "search", Summary: "search package names and descriptions", UsageArgs: "PATTERN...", Args: RequiredArgs, Group: GroupDiscovery,
		Flags: []string{"all-versions", "all-arches", "installed", "upgradable", "virtual", "description", "summary", "machine", "names"}},
	{Name: "show", Aliases: []string{"info"}, Summary: "show package details", UsageArgs: "PKG...", Args: RequiredArgs, Group: GroupDiscovery,
		Flags: []string{"all-versions", "all-arches", "machine"}},
	{Name: "policy", Summary: "show repository precedence and pinned packages", UsageArgs: "[PKG...]", Args: OptionalArgs, Group: GroupDiscovery,
		Flags: []string{"machine"}},
	{Name: "history", Summary: "list, inspect, undo, redo or clear transaction history", UsageArgs: "[ID|last|info|show|undo|redo|clear]", Args: OptionalArgs, Group: GroupHistory,
		Flags: []string{"all"}},
	{Name: "undo", Summary: "revert a recorded transaction", UsageArgs: "[ID|last]", Args: OptionalArgs, Group: GroupHistory},
	{Name: "redo", Summary: "reapply a recorded transaction", UsageArgs: "[ID|last]", Args: OptionalArgs, Group: GroupHistory},
	{Name: "fetch", Summary: "rank Void mirrors and configure repositories", Args: NoArgs, Group: GroupFetch,
		Flags: []string{"country", "auto", "https-only", "non-free", "dry-run", "sources", "debian", "ubuntu", "devuan"}},
	{Name: "completion", Summary: "print a shell completion script", UsageArgs: "bash|zsh|fish", Args: RequiredArgs, Group: GroupTooling},
	{Name: "link", Summary: "create the six xbp symlinks in a directory", UsageArgs: "[DIR]", Args: OptionalArgs, Group: GroupTooling},
	{Name: "log", Summary: "print the transaction log", UsageArgs: "[PKG...]", Args: OptionalArgs, Group: GroupTooling,
		Flags: []string{"limit"}},
	{Name: "moo", Summary: "moo.", Hidden: true, Group: GroupTooling, Flags: []string{"update", "no-update"}},
}

func commandFlags(name string) []string {
	common := []string{
		"dry-run", "yes", "assume-no", "download-only", "simple", "update", "no-update", "no-sync",
		"purge", "reinstall", "auto-remove", "no-auto-remove",
		"full", "no-full", "safe", "no-log",
		"box-style", "progress-bar", "no-progress", "no-progress-bar",
		"allow-unauthenticated", "option", "fix-broken", "target-release",
		"install-recommends", "install-suggests", "remove-essential", "remove-config",
		"exclude", "no-fix-broken", "no-install-recommends", "no-install-suggests",
	}
	for _, upgrade := range upgradeNames() {
		if name == upgrade {
			common = append(common, "print-uris")
		}
	}
	return common
}

// Flags is the flag table. Global flags are accepted everywhere; the rest
// name the canonical commands that accept them.
var Flags = []Flag{
	{Long: "help", Short: 'h', Kind: BoolFlag, Summary: "print this help", Global: true},
	{Long: "version", Short: 'V', Kind: BoolFlag, Summary: "print version information", Global: true},
	{Long: "verbose", Short: 'v', Kind: BoolFlag, Summary: "print every xbps command before running it", Global: true},
	{Long: "debug", Kind: BoolFlag, Summary: "dump the resolved configuration", Global: true},
	{Long: "config", Kind: ValueFlag, ValueName: "FILE", Summary: "read FILE instead of the user config file", Global: true},
	{Long: "color", Kind: ValueFlag, ValueName: "WHEN", Values: []string{"auto", "always", "never"}, Summary: "colour output: auto, always or never", Global: true},
	{Long: "no-color", Kind: BoolFlag, Summary: "never emit ANSI colour", Global: true},
	{Long: "tui", Kind: BoolFlag, Summary: "force the full-screen interface", Global: true},
	{Long: "no-tui", Kind: BoolFlag, Summary: "disable the full-screen interface", Global: true},
	{Long: "history-dir", Kind: ValueFlag, ValueName: "DIR", Summary: "read history from DIR", Global: true, Hidden: true},

	{Long: "virtual", Short: 'V', Kind: BoolFlag, Summary: "list virtual packages", Commands: []string{"list", "search"}},
	{Long: "country", Short: 'c', Kind: ValueFlag, ValueName: "COUNTRY", Summary: "restrict mirrors to a country or region (repeatable)", Commands: []string{"fetch"}},

	{Long: "dry-run", Kind: BoolFlag, Summary: "show the plan without changing anything", Commands: append(transactionNames(), "fetch")},
	{Long: "yes", Short: 'y', Aliases: []string{"assume-yes"}, Kind: BoolFlag, Summary: "answer yes and skip the prompt", Commands: transactionNames()},
	{Long: "assume-no", Short: 'n', Kind: BoolFlag, Summary: "refuse at the prompt", Commands: transactionNames()},
	{Long: "download-only", Short: 'd', Kind: BoolFlag, Summary: "download the plan's archives and stop", Commands: transactionNames()},
	{Long: "simple", Kind: BoolFlag, Summary: "print the simple summary", Commands: transactionNames()},
	{Long: "update", Kind: BoolFlag, Summary: "refresh the repository index first", Commands: append(transactionNames(), "moo")},
	{Long: "no-update", Kind: BoolFlag, Summary: "do not refresh the repository index", Commands: append(transactionNames(), "moo")},
	{Long: "no-sync", Kind: BoolFlag, Summary: "do not refresh the repository index", Commands: transactionNames()},
	{Long: "purge", Kind: BoolFlag, Summary: "also remove configuration files", Commands: transactionNames()},
	{Long: "reinstall", Kind: BoolFlag, Summary: "reinstall packages that are already installed", Commands: transactionNames()},
	{Long: "auto-remove", Aliases: []string{"autoremove"}, Kind: BoolFlag, Summary: "force the implicit orphan removal", Commands: transactionNames()},
	{Long: "no-auto-remove", Aliases: []string{"no-autoremove"}, Kind: BoolFlag, Summary: "skip the implicit orphan removal", Commands: transactionNames()},
	{Long: "full", Kind: BoolFlag, Summary: "accepted no-op; xbps -u never removes packages", Commands: transactionNames()},
	{Long: "no-full", Kind: BoolFlag, Summary: "accepted no-op; wins over --full", Commands: transactionNames()},
	{Long: "safe", Kind: BoolFlag, Summary: "accepted no-op for nala compatibility", Commands: transactionNames()},
	{Long: "print-uris", Kind: BoolFlag, Summary: "print the plan's download URLs and stop", Commands: upgradeNames()},
	{Long: "no-log", Kind: BoolFlag, Summary: "do not append to the transaction log", Commands: transactionNames()},
	// Superset flags kept from earlier revisions.
	{Long: "box-style", Kind: ValueFlag, ValueName: "STYLE", Values: []string{"rounded", "classic", "ascii"}, Summary: "summary border style", Hidden: true, Commands: transactionNames()},
	{Long: "progress-bar", Kind: OptionalValueFlag, ValueName: "BOOL", Summary: "force the progress widget on or off", Hidden: true, Commands: transactionNames()},
	{Long: "no-progress", Kind: BoolFlag, Summary: "disable the progress widget", Hidden: true, Commands: transactionNames()},
	{Long: "no-progress-bar", Kind: BoolFlag, Summary: "disable the progress widget", Hidden: true, Commands: transactionNames()},

	// FR-019: accepted by the parser, refused before any child runs.
	{Long: "allow-unauthenticated", Kind: BoolFlag, Commands: transactionNames(),
		Refusal: "--allow-unauthenticated is not available on xbps: xbps verifies repository signatures and has no unauthenticated mode."},
	{Long: "option", Short: 'o', Kind: ValueFlag, ValueName: "OPTION", Commands: transactionNames(),
		Refusal: "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf."},
	{Long: "fix-broken", Short: 'f', Kind: BoolFlag, Commands: transactionNames(),
		Refusal: "-f/--fix-broken is not available on xbps: xbps has no broken-dependency repair mode. Check the pkgdb with xbps-pkgdb --checks dependencies, or force a reinstall with xbp install -- -f PKG."},
	{Long: "target-release", Short: 't', Kind: ValueFlag, ValueName: "RELEASE", Commands: transactionNames(),
		Refusal: "-t/--target-release is not available on xbps: xbps has no default-release targeting; pin a repository with a repository= line in /etc/xbps.d."},
	{Long: "install-recommends", Kind: BoolFlag, Commands: transactionNames(),
		Refusal: "--install-recommends is not available on xbps: xbps has no recommends metadata; every declared run_depends dependency is installed."},
	{Long: "install-suggests", Kind: BoolFlag, Commands: transactionNames(),
		Refusal: "--install-suggests is not available on xbps: xbps has no suggests metadata."},
	{Long: "remove-essential", Kind: BoolFlag, Commands: transactionNames(),
		Refusal: "--remove-essential is not available on xbps: xbps has no essential-package protection to override."},
	{Long: "remove-config", Kind: BoolFlag, Commands: transactionNames(),
		Refusal: "--remove-config is not available on xbps: xbps has no config-files state; xbps-remove -f removes files, it does not create a config-only removal."},
	{Long: "exclude", Kind: ValueFlag, ValueName: "PATTERN", Commands: transactionNames(),
		Refusal: "--exclude is not available on xbps: xbps has no upgrade exclusion; hold the package with xbps-pkgdb -m hold PKG (release with -m unhold)."},
	{Long: "sources", Kind: BoolFlag, Commands: []string{"fetch"},
		Refusal: "--sources is not available: Void repositories contain no source packages"},
	{Long: "debian", Kind: BoolFlag, Commands: []string{"fetch"},
		Refusal: "--debian is not available: xbpretty targets Void Linux only"},
	{Long: "ubuntu", Kind: BoolFlag, Commands: []string{"fetch"},
		Refusal: "--ubuntu is not available: xbpretty targets Void Linux only"},
	{Long: "devuan", Kind: BoolFlag, Commands: []string{"fetch"},
		Refusal: "--devuan is not available: xbpretty targets Void Linux only"},

	// FR-019's three negative compatibility no-ops.
	{Long: "no-fix-broken", Kind: BoolFlag, NoOp: true, Commands: transactionNames()},
	{Long: "no-install-recommends", Kind: BoolFlag, NoOp: true, Commands: transactionNames()},
	{Long: "no-install-suggests", Kind: BoolFlag, NoOp: true, Commands: transactionNames()},

	// Discovery (FR-023 .. FR-027).
	{Long: "all-versions", Short: 'a', Kind: BoolFlag, Summary: "show every version of a package", Commands: []string{"list", "search", "show"}},
	{Long: "all-arches", Short: 'A', Kind: BoolFlag, Summary: "accepted no-op; xbps covers all architectures", NoOp: true, Commands: []string{"list", "search", "show"}},
	{Long: "installed", Short: 'i', Kind: BoolFlag, Summary: "show installed packages only", Commands: []string{"list", "search"}},
	{Long: "upgradable", Short: 'u', Kind: BoolFlag, Summary: "show upgradable packages only", Commands: []string{"list", "search"}},
	{Long: "description", Kind: BoolFlag, Summary: "print the long description under each row", Commands: []string{"list", "search"}},
	{Long: "summary", Kind: BoolFlag, Summary: "print the short summary under each row", Commands: []string{"list", "search"}},
	{Long: "machine", Short: 'm', Kind: BoolFlag, Summary: "emit NDJSON schema 1", Commands: []string{"list", "search", "show", "policy"}},
	{Long: "names", Short: 'n', Aliases: []string{"names-only"}, Kind: BoolFlag, Summary: "search names only", Commands: []string{"search"}},

	{Long: "limit", Kind: ValueFlag, ValueName: "N", Summary: "keep only the last N matching log entries", Commands: []string{"log"}},
	{Long: "lists", Kind: BoolFlag, Summary: "delete cached repodata files", Commands: []string{"clean"}},
	{Long: "fetch", Kind: BoolFlag, Summary: "restore the repository configuration written by fetch", Commands: []string{"clean"}},
	{Long: "all", Kind: BoolFlag, Summary: "with history clear: delete every entry", Commands: []string{"history"}},
	{Long: "auto", Kind: OptionalValueFlag, ValueName: "N", Summary: "validate the N fastest mirrors (default 3)", Commands: []string{"fetch"}},
	{Long: "https-only", Kind: BoolFlag, Summary: "probe and write https mirrors only", Commands: []string{"fetch"}},
	{Long: "non-free", Kind: BoolFlag, Summary: "include the non-free repository paths", Commands: []string{"fetch"}},
}

// LookupCommand resolves a canonical command or alias to its canonical name.
func LookupCommand(name string) (string, bool) {
	for _, c := range Commands {
		if c.Name == name {
			return c.Name, true
		}
		for _, alias := range c.Aliases {
			if alias == name {
				return c.Name, true
			}
		}
	}
	return "", false
}

// ResolveCommand is LookupCommand under the name parser call sites use.
func ResolveCommand(name string) (string, bool) { return LookupCommand(name) }

// CommandByName returns the Command record for a canonical name or alias.
func CommandByName(name string) (Command, bool) {
	canonical, ok := LookupCommand(name)
	if !ok {
		return Command{}, false
	}
	for _, c := range Commands {
		if c.Name == canonical {
			return c, true
		}
	}
	return Command{}, false
}

// NormalizeHistory rewrites the history subcommands FR-002 aliases: history
// undo/redo are the top-level verbs; clear/info/show stay under history.
func NormalizeHistory(command string, args []string) (cmd, sub string, rest []string) {
	if command != "history" {
		return command, "", args
	}
	if len(args) == 0 {
		return "history", "", nil
	}
	switch args[0] {
	case "undo", "redo":
		return args[0], "", args[1:]
	case "clear", "info", "show":
		return "history", args[0], args[1:]
	}
	return "history", "", args
}

// LookupFlag finds a flag by its canonical long name or a long alias.
func LookupFlag(name string) (Flag, bool) {
	for _, f := range Flags {
		if f.Long == name {
			return f, true
		}
		for _, alias := range f.Aliases {
			if alias == name {
				return f, true
			}
		}
	}
	return Flag{}, false
}

// FlagsFor returns every flag accepted by a canonical command: the global
// flags plus the flags the command declares.
func FlagsFor(command string) []Flag {
	var out []Flag
	for _, f := range Flags {
		if f.Global {
			out = append(out, f)
			continue
		}
		if contains(f.Commands, command) {
			out = append(out, f)
		}
	}
	return out
}

// Accepts reports whether command accepts the canonical flag long name.
func Accepts(command, long string) bool {
	f, ok := LookupFlag(long)
	if !ok {
		return false
	}
	if f.Global {
		return true
	}
	return contains(f.Commands, command)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// VisibleCommands returns the commands advertised in help and completions.
func VisibleCommands() []Command {
	var out []Command
	for _, c := range Commands {
		if !c.Hidden {
			out = append(out, c)
		}
	}
	return out
}

// LinkNames is the closed set of argv[0] names the package installs.
func LinkNames() []string {
	return []string{"xbp", "xbp-install", "xbp-remove", "xbp-upgrade", "xbp-autoremove", "xbp-update"}
}

// IsTransaction reports whether cmd is one of the transaction verbs.
func IsTransaction(cmd string) bool { return contains(transactionNames(), cmd) }

// FlagSpellings returns the way a flag is written in help and completions.
func FlagSpellings(f Flag) []string {
	var out []string
	if f.Short != 0 {
		out = append(out, "-"+string(f.Short))
	}
	out = append(out, "--"+f.Long)
	for _, alias := range f.Aliases {
		out = append(out, "--"+alias)
	}
	return out
}

// Usage renders the command table as xbpretty's --help output.
func Usage() string {
	var b strings.Builder
	b.WriteString("xbpretty - a friendlier front end for xbps\n\n")
	b.WriteString("Usage:\n  xbpretty <command> [flags] [packages...]\n\n")
	b.WriteString("Commands:\n")
	lastGroup := ""
	for _, c := range VisibleCommands() {
		if c.Group != lastGroup {
			if lastGroup != "" {
				b.WriteString("\n")
			}
			b.WriteString("  " + c.Group + ":\n")
			lastGroup = c.Group
		}
		name := c.Name
		if c.UsageArgs != "" {
			name += " " + c.UsageArgs
		}
		fmt.Fprintf(&b, "  %-28s %s\n", name, c.Summary)
		if len(c.Aliases) > 0 {
			fmt.Fprintf(&b, "  %-28s alias: %s\n", "", strings.Join(c.Aliases, ", "))
		}
	}
	b.WriteString("\nGlobal flags:\n")
	for _, f := range Flags {
		if !f.Global || f.Hidden {
			continue
		}
		writeFlagLine(&b, f)
	}
	b.WriteString("\nCommand flags:\n")
	for _, f := range Flags {
		if f.Global || f.Hidden || f.Refusal != "" || f.NoOp {
			continue
		}
		fmt.Fprintf(&b, "  %-28s %s (%s)\n", spelling(f), f.Summary, strings.Join(f.Commands, ", "))
	}
	b.WriteString("\nArguments after -- are passed to xbps unchanged, in both the dry run and\n")
	b.WriteString("the real run, e.g. xbpretty install -- -f pkg.\n")
	return b.String()
}

func writeFlagLine(b *strings.Builder, f Flag) {
	line := spelling(f)
	if f.Kind != BoolFlag {
		line += " " + f.ValueName
		if len(f.Values) > 0 {
			line += " (" + strings.Join(f.Values, "|") + ")"
		}
	}
	fmt.Fprintf(b, "  %-28s %s\n", line, f.Summary)
}

func spelling(f Flag) string {
	var parts []string
	if f.Short != 0 {
		parts = append(parts, "-"+string(f.Short))
	}
	parts = append(parts, "--"+f.Long)
	if len(f.Aliases) > 0 {
		parts = append(parts, "--"+strings.Join(f.Aliases, ", --"))
	}
	return strings.Join(parts, ", ")
}

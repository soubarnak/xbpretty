// Package manpage generates xbpretty's manual pages from the shared command
// table in internal/spec (FR-076, AC-059). The pages are committed because an
// xbps-src cross build cannot run the binary it just built; `make manpages`
// regenerates them and a freshness test keeps them current.
package manpage

import (
	"fmt"
	"strings"

	"github.com/soubarnak/xbpretty/internal/spec"
)

// Date is the manual-page date. It is fixed so regeneration is reproducible.
const Date = "November 5, 2026"

// File is one generated manual page.
type File struct {
	Name    string
	Content string
}

// Files returns both manual pages in install order.
func Files() []File {
	return []File{
		{Name: "man/xbpretty.8", Content: Man8()},
		{Name: "man/xbpretty.conf.5", Content: Conf5()},
	}
}

// AliasLinks returns the six man8 link names, one per installed argv[0] alias.
func AliasLinks() []string {
	names := spec.LinkNames()
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name+".8")
	}
	return out
}

func newPage(title, section, name, purpose string) *strings.Builder {
	b := &strings.Builder{}
	fmt.Fprintf(b, ".Dd %s\n", Date)
	fmt.Fprintf(b, ".Dt %s %s\n", title, section)
	b.WriteString(".Os\n")
	b.WriteString(".Sh NAME\n")
	fmt.Fprintf(b, ".Nm %s\n", name)
	fmt.Fprintf(b, ".Nd %s\n", purpose)
	return b
}

func flagTag(f spec.Flag) string {
	var b strings.Builder
	b.WriteString(".It Cm ")
	first := true
	add := func(s string) {
		if !first {
			b.WriteString(" , ")
		}
		b.WriteString(s)
		first = false
	}
	if f.Short != 0 {
		add("-" + string(f.Short))
	}
	add("--" + f.Long)
	for _, alias := range f.Aliases {
		add("--" + alias)
	}
	if f.Kind != spec.BoolFlag {
		b.WriteString(" Ar ")
		if f.ValueName != "" {
			b.WriteString(f.ValueName)
		} else {
			b.WriteString("VALUE")
		}
	}
	return b.String()
}

func flagDescription(f spec.Flag) string {
	var parts []string
	if f.Summary != "" {
		parts = append(parts, f.Summary+".")
	}
	if f.Global {
		parts = append(parts, "Accepted on every command.")
	} else if len(f.Commands) > 0 {
		parts = append(parts, "Accepted by: "+strings.Join(f.Commands, ", ")+".")
	}
	if len(f.Values) > 0 {
		parts = append(parts, "Values: "+strings.Join(f.Values, ", ")+".")
	}
	if f.NoOp {
		parts = append(parts, "Accepted and ignored.")
	}
	if f.Refusal != "" {
		parts = append(parts, "Not available: "+strings.ReplaceAll(f.Refusal, "--", `\-\-`))
	}
	if f.Hidden {
		parts = append(parts, "Hidden from --help.")
	}
	return strings.Join(parts, " ")
}

func writeFlagList(b *strings.Builder, flags []spec.Flag) {
	b.WriteString(".Bl -tag -width Ds\n")
	for _, f := range flags {
		b.WriteString(flagTag(f) + "\n")
		writeText(b, flagDescription(f))
	}
	b.WriteString(".El\n")
}

// writeText emits prose as roff text lines. Each sentence starts a new input
// line and every line stays under 80 bytes, so mandoc -T lint is silent about
// long lines and "new sentence, new line" (AC-059).
func writeText(b *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	for text != "" {
		cut := len(text)
		for _, mark := range []string{". ", "! ", "? "} {
			if i := strings.Index(text, mark); i >= 0 && i+1 < cut {
				cut = i + 1
			}
		}
		sentence := strings.TrimSpace(text[:cut])
		if cut < len(text) {
			text = strings.TrimLeft(text[cut:], " ")
		} else {
			text = ""
		}
		for _, line := range wrapText(sentence, 78) {
			b.WriteString(line + "\n")
		}
	}
}

func sentence(text string) string {
	if text == "" {
		return ""
	}
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") {
		return text
	}
	return text + "."
}

func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) <= width {
			line += " " + word
			continue
		}
		lines = append(lines, line)
		line = word
	}
	return append(lines, line)
}

// Man8 renders xbpretty(8): the command surface, options, streams, files,
// exit codes, security and SEE ALSO.
func Man8() string {
	b := newPage("XBPRETTY", "8", "xbpretty", "friendlier front end for the xbps package manager")

	b.WriteString(".Sh SYNOPSIS\n")
	b.WriteString(".Nm\n")
	b.WriteString(".Op Ar command\n")
	b.WriteString(".Op Ar options\n")
	b.WriteString(".Op Ar arguments ...\n")
	links := spec.LinkNames()
	for _, name := range links {
		fmt.Fprintf(b, ".Nm %s\n", name)
		b.WriteString(".Op Ar options\n")
		b.WriteString(".Op Ar arguments ...\n")
	}

	b.WriteString(".Sh DESCRIPTION\n")
	b.WriteString(".Nm\n")
	writeText(b, "previews an xbps transaction, prints a nala-style summary, asks once, and then runs the underlying xbps command. It wraps xbps-install, xbps-remove and xbps-query; it does not replace them. One binary is installed under seven names: xbpretty plus the six aliases xbp, xbp-install, xbp-remove, xbp-upgrade, xbp-autoremove and xbp-update. The argv[0] name selects or fixes the command.")
	b.WriteString(".Pp\n")
	writeText(b, "Durable output is written to standard output. Live, interactive and diagnostic output, including the confirmation prompt, the progress widget, forwarded child output, hints, warnings and errors, is written to standard error. Standard input is read only for the confirmation prompt and for history selectors that a subcommand accepts.")
	b.WriteString(".Pp\n")
	writeText(b, "The command surface, the flag table and the shell completions all derive from one internal table, so a command that is not listed here does not exist. Arguments after the option terminator are passed to xbps unchanged in both the dry and the real run.")

	b.WriteString(".Sh COMMANDS\n")
	writeText(b, "Commands are grouped as transaction, discovery, history, fetch and tooling commands. Global options are accepted before or after the command, up to the option terminator.")
	b.WriteString(".Bl -tag -width Ds\n")
	for _, c := range spec.Commands {
		fmt.Fprintf(b, ".It Cm %s", c.Name)
		if c.UsageArgs != "" {
			fmt.Fprintf(b, " %s", c.UsageArgs)
		}
		b.WriteString("\n")
		writeText(b, sentence(c.Summary))
		if len(c.Aliases) > 0 {
			writeText(b, "Aliases: "+strings.Join(c.Aliases, ", ")+".")
		}
		if len(c.UndocumentedAliases) > 0 {
			writeText(b, "Undocumented aliases: "+strings.Join(c.UndocumentedAliases, ", ")+".")
		}
		if c.Hidden {
			writeText(b, "Hidden easter egg; not advertised by --help.")
		}
	}
	b.WriteString(".El\n")

	b.WriteString(".Sh OPTIONS\n")
	writeText(b, "Options are either global or accepted by the commands named in their description. A flag passed to a command that does not accept it is a usage error and exits 2.")
	b.WriteString(".Ss Global options\n")
	var globals []spec.Flag
	var commandFlags []spec.Flag
	for _, f := range spec.Flags {
		if f.Global {
			globals = append(globals, f)
		} else {
			commandFlags = append(commandFlags, f)
		}
	}
	writeFlagList(b, globals)
	b.WriteString(".Ss Command options\n")
	writeFlagList(b, commandFlags)

	b.WriteString(".Sh STREAMS\n")
	b.WriteString(".Bl -tag -width Ds\n")
	b.WriteString(".It standard output\n")
	writeText(b, "Durable output only: the summary, Nothing to do., update counts, discovery rows and NDJSON, download results, the fetch table, --help, --version and completion scripts.")
	b.WriteString(".It standard error\n")
	writeText(b, "Everything live, interactive or diagnostic: the prompt, progress widget, verbose lines, forwarded child output, plan output, notices, warnings, hints and errors.")
	b.WriteString(".It standard input\n")
	writeText(b, "Answers the confirmation prompt. A non-terminal stdin is never read as a prompt; use -y to proceed.")
	b.WriteString(".El\n")

	b.WriteString(".Sh ENVIRONMENT\n")
	b.WriteString(".Bl -tag -width Ds\n")
	b.WriteString(".It Ev NO_COLOR\n")
	writeText(b, "When set and non-empty, disables ANSI colour under color mode Auto.")
	b.WriteString(".It Ev XDG_CONFIG_HOME\n")
	writeText(b, "Absolute base for the user configuration layer. Ignored for an invoking sudo/doas user.")
	b.WriteString(".It Ev XDG_STATE_HOME\n")
	writeText(b, "Base for the transaction log. Ignored for an invoking sudo/doas user.")
	b.WriteString(".It Ev XBPRETTY_HISTORY_DIR\n")
	writeText(b, "History directory, overridden by --history-dir.")
	b.WriteString(".It Ev SUDO_USER , Ev DOAS_USER\n")
	writeText(b, "Selects the invoking user's home for configuration and state.")
	b.WriteString(".It Ev TERM , Ev COLUMNS\n")
	writeText(b, "TERM=dumb disables the live UI; COLUMNS supplies the summary width when standard output is not a terminal.")
	b.WriteString(".It Ev HTTP_PROXY , Ev HTTPS_PROXY , Ev NO_PROXY\n")
	writeText(b, "Proxy settings for predownload and fetch.")
	b.WriteString(".El\n")

	b.WriteString(".Sh FILES\n")
	b.WriteString(".Bl -tag -width Ds\n")
	b.WriteString(".It Pa /etc/xbpretty/config.toml\n")
	writeText(b, "Optional system configuration layer, read before the user layer. It is absent by default and the package never creates it.")
	b.WriteString(".It Pa ~/.config/xbpretty/config.toml\n")
	writeText(b, "User configuration layer, or")
	b.WriteString(".Pa $XDG_CONFIG_HOME/xbpretty/config.toml\n")
	writeText(b, "when XDG_CONFIG_HOME is an absolute path. Under sudo or doas the invoking user's home is used.")
	b.WriteString(".It Pa /var/lib/xbpretty/history/\n")
	writeText(b, "Replayable history entries, one JSON file per transaction. Override with --history-dir or XBPRETTY_HISTORY_DIR.")
	b.WriteString(".It Pa $XDG_STATE_HOME/xbpretty/transactions.log\n")
	writeText(b, "Append-only audit log, falling back to ~/.local/state/xbpretty/transactions.log.")
	b.WriteString(".It Pa /var/cache/xbps\n")
	writeText(b, "The package cache used by predownload, download-only and undo/redo restores.")
	b.WriteString(".It Pa /etc/xbps.d\n")
	writeText(b, "xbps repository configuration written by fetch and clean --fetch, not xbpretty configuration.")
	b.WriteString(".It Pa /usr/share/bash-completion/completions/xbpretty\n")
	writeText(b, "Installed bash completion.")
	b.WriteString(".It Pa /usr/share/zsh/site-functions/_xbpretty\n")
	writeText(b, "Installed zsh completion.")
	b.WriteString(".It Pa /usr/share/fish/vendor_completions.d/xbpretty.fish\n")
	writeText(b, "Installed fish completion.")
	b.WriteString(".It Pa /usr/share/doc/xbpretty/config.toml.example\n")
	writeText(b, "Canonical configuration example; it parses to the built-in defaults.")
	b.WriteString(".It Pa /usr/share/doc/xbpretty/migration-rev6.md\n")
	writeText(b, "Rev 5 to rev 6 migration guide.")
	b.WriteString(".El\n")

	b.WriteString(".Sh EXIT STATUS\n")
	b.WriteString(".Bl -tag -width Ds\n")
	b.WriteString(".It Cm 0\n")
	writeText(b, "Success, nothing to do, a dry run, a completed download-only run, clean, update, discovery with no matches, history list/detail, or a successful fetch.")
	b.WriteString(".It Cm 1\n")
	writeText(b, "Refused confirmation, non-terminal stdin, non-root execution, missing package arguments, runtime failure, missing or corrupt history, an unreplayable row, a missing archive, an invalid prompt answer, a cancelled fetch, or a partial download.")
	b.WriteString(".It Cm 2\n")
	writeText(b, "Usage error, conflicting flags, an N/A flag, invalid configuration, --print-uris combined with another stop flag, or a TUI conflict. Configuration errors reported by the loader also exit 2.")
	b.WriteString(".It Cm 130\n")
	writeText(b, "Interrupted by SIGINT.")
	b.WriteString(".It other\n")
	writeText(b, "An xbps child's exit status is passed through unchanged.")
	b.WriteString(".El\n")

	b.WriteString(".Sh SECURITY\n")
	writeText(b, "When effective UID is 0, a privileged [Xbps] path from a configuration layer is honoured only when the supplying file is a regular file owned by UID 0 with no group or world write bit, checked on the descriptor that is read. An untrusted path is ignored and never executed; one warning per key is printed. Non-privileged keys from the same layer still apply.")
	b.WriteString(".Pp\n")
	writeText(b, "Repository signatures are always verified by xbps. xbpretty never disables signature checking and never installs packages itself. A local undo/redo replay repository verifies only the SHA-256 of a cached archive, relying on the signature verified when xbps downloaded it.")
	b.WriteString(".Pp\n")
	writeText(b, "The package installs no configuration file under /etc and no conf_files entry; the optional system layer is administrator-created. fetch writes repository lines under /etc/xbps.d as package-manager state.")

	b.WriteString(".Sh SEE ALSO\n")
	writeText(b, "xbpretty.conf(5)")
	b.WriteString(".Pp\n")
	writeText(b, "https://github.com/soubarnak/xbpretty")
	return b.String()
}

type configKey struct {
	Name    string
	Default string
	Summary string
}

func writeConfigKeys(b *strings.Builder, keys []configKey) {
	b.WriteString(".Bl -tag -width Ds\n")
	for _, key := range keys {
		fmt.Fprintf(b, ".It Cm %s\n", key.Name)
		line := key.Summary
		if key.Default != "" {
			line += " Default: " + key.Default + "."
		}
		writeText(b, line)
	}
	b.WriteString(".El\n")
}

var (
	nalaKeys = []configKey{
		{"auto_remove", "true", "Plan and offer the implicit orphan removal after a successful transaction."},
		{"auto_update", "true", "Refresh the repository index before the upgrade family."},
		{"update_show_packages", "false", "With update, list each upgradable package instead of only the count."},
		{"full_upgrade", "false", "Accepted compatibility switch; xbps -u never removes packages."},
		{"simple", "false", "Use the one-line-per-operation simple summary."},
		{"assume_yes", "false", "Skip the confirmation prompt, as -y does."},
	}
	uiKeys = []configKey{
		{"mode", `"Auto"`, "Interface mode: Auto, Plain or Tui."},
		{"unit", `"Binary"`, "Size unit: Binary or Decimal."},
	}
	colorKeys = []configKey{
		{"mode", `"Auto"`, "Colour mode: Auto, Always or Never."},
	}
	xbpsKeys = []configKey{
		{"install_path", `"xbps-install"`, "Path to xbps-install; privileged and trust-gated under root."},
		{"remove_path", `"xbps-remove"`, "Path to xbps-remove; privileged and trust-gated under root."},
		{"query_path", `"xbps-query"`, "Path to xbps-query; privileged and trust-gated under root."},
		{"rindex_path", `"xbps-rindex"`, "Path to xbps-rindex; privileged and trust-gated under root."},
		{"uhelper_path", `"xbps-uhelper"`, "Path to xbps-uhelper; privileged and trust-gated under root."},
		{"pkgdb_path", `"xbps-pkgdb"`, "Path to xbps-pkgdb; privileged and trust-gated under root."},
		{"transaction_log", "true", "Append a line to the transaction log after a successful run."},
	}
	themeKeys = []configKey{
		{"Primary", "", "Section rules, labels and headings."},
		{"Secondary", "", "Secondary labels."},
		{"Highlight", "", "Package names and highlighted values."},
		{"Regular", "", "Regular body text."},
		{"ProgressFilled", "", "Filled part of the progress bar."},
		{"ProgressUnfilled", "", "Unfilled part of the progress bar."},
		{"Notice", "", "Notices and version highlights."},
		{"Warning", "", "Warnings."},
		{"Error", "", "Errors."},
	}
	legacyKeys = []configKey{
		{"assume_yes", "", "Rename to [Nala] assume_yes."},
		{"sync", "", "Rename to [Nala] auto_update."},
		{"log", "", "Rename to [Xbps] transaction_log."},
		{"xbps_install_path", "", "Rename to [Xbps] install_path."},
		{"xbps_remove_path", "", "Rename to [Xbps] remove_path."},
		{"xbps_query_path", "", "Rename to [Xbps] query_path."},
		{"color", "", "Rename to [Color] mode and map auto/always/never to Auto/Always/Never."},
		{"verbose", "", "Retired; pass -v on the command line."},
		{"box_style", "", "Retired; the summary has no box."},
		{"progress_bar", "", "Retired; use [Ui] mode = \"Plain\"."},
	}
)

// Conf5 renders xbpretty.conf(5): keys, layers, trust, legacy keys and the
// security rules around the optional system layer.
func Conf5() string {
	b := newPage("XBPRETTY.CONF", "5", "xbpretty.conf", "configuration file for xbpretty")

	b.WriteString(".Sh SYNOPSIS\n")
	b.WriteString(".Pa /etc/xbpretty/config.toml\n")
	b.WriteString(".Pa ~/.config/xbpretty/config.toml\n")
	b.WriteString(".Pa $XDG_CONFIG_HOME/xbpretty/config.toml\n")
	b.WriteString(".Pa FILE\n")

	b.WriteString(".Sh DESCRIPTION\n")
	b.WriteString(".Nm\n")
	writeText(b, "is an optional TOML configuration file for")
	writeText(b, "xbpretty(8). It uses nala's schema: the [Nala], [Ui], [Color] and [Color.theme] tables plus the xbps-only [Xbps] table. Every key is optional. Missing files and missing keys resolve to the built-in defaults, and command-line flags override configuration values.")
	b.WriteString(".Pp\n")
	writeText(b, "A rev 6 run never writes, rewrites, converts or backs up a configuration file. It writes no marker and no cache. Configuration warnings never change the command's exit status.")

	b.WriteString(".Sh LAYERS\n")
	writeText(b, "Layers are read in this order; later layers win per key, and theme styles merge role by role.")
	b.WriteString(".Bl -tag -width Ds\n")
	b.WriteString(".It system layer\n")
	writeText(b, "/etc/xbpretty/config.toml, optional and absent by default. The package never creates this file or its directory.")
	b.WriteString(".It user layer\n")
	writeText(b, "$XDG_CONFIG_HOME/xbpretty/config.toml when XDG_CONFIG_HOME is absolute, else ~/.config/xbpretty/config.toml. Under sudo or doas the invoking user's passwd home is used and XDG_CONFIG_HOME is ignored.")
	b.WriteString(".It Cm --config Ar FILE\n")
	writeText(b, "A long-only global option that replaces both default layers for the run. It is accepted before or after the subcommand. The short form -c is fetch's --country and is a usage error on every other command.")
	b.WriteString(".El\n")
	writeText(b, "Each existing layer is parsed and validated independently. A missing default layer is silent; an existing unreadable or invalid layer is a hard error and exits 2. A missing explicit --config file is also a hard error. XBPRETTY_CONFIG is not supported.")

	b.WriteString(".Sh KEYS\n")
	b.WriteString(".Ss Nala\n")
	writeConfigKeys(b, nalaKeys)
	b.WriteString(".Ss Ui\n")
	writeConfigKeys(b, uiKeys)
	b.WriteString(".Ss Color\n")
	writeConfigKeys(b, colorKeys)
	b.WriteString(".Ss Color.theme\n")
	writeText(b, "Each theme role is an inline table of the form { fg = \"BrightGreen\" }, optionally with bg and modifier. An omitted modifier means bold; an empty modifier means none. The nine roles are:")
	writeConfigKeys(b, themeKeys)
	b.WriteString(".Ss Xbps\n")
	writeConfigKeys(b, xbpsKeys)

	b.WriteString(".Sh VALUES\n")
	b.WriteString(".Ss Colours\n")
	writeText(b, "A colour is a named ANSI colour, an indexed colour 0-255, or an RGB triplet. Named colours are Reset, Black, Red, Green, Yellow, Blue, Magenta, Cyan, Gray, DarkGray, LightRed, LightGreen, LightYellow, LightBlue, LightMagenta, LightCyan and White. Indexed colours use an integer; RGB colours use rgb(r, g, b).")
	b.WriteString(".Ss Modifiers\n")
	writeText(b, "The accepted modifiers are bold, dim, italic, underlined, slow_blink, rapid_blink, reversed, hidden and crossed_out.")
	b.WriteString(".Ss Enumerations\n")
	writeText(b, "[Ui] mode is Auto, Plain or Tui; [Ui] unit is Binary or Decimal; [Color] mode is Auto, Always or Never. Values are case-sensitive.")

	b.WriteString(".Sh LEGACY KEYS\n")
	writeText(b, "Rev 6 accepts ten rev-5 top-level flat keys before the first table header. They are mapped where possible and never rewritten. Alias keys warn once per key; the three retired keys warn and are ignored. All legacy keys are removed in rev 7, where they become unknown keys and exit 2.")
	writeConfigKeys(b, legacyKeys)
	writeText(b, "Within a file the canonical key wins. Across layers the later layer wins regardless of spelling. Rev 6 also accepts rev-5 value forms for the ten legacy keys only: quoted booleans and bare strings at top level. Everything else remains strict TOML, and duplicate keys are an error.")
	b.WriteString(".Pp\n")
	writeText(b, "When any legacy key is seen, one aggregate warning is printed once per run. --debug adds one line per legacy key and a summary. Warnings never change the exit code.")

	b.WriteString(".Sh TRUST\n")
	writeText(b, "When effective UID is 0, a [Xbps] path key is honoured only if the supplying layer is a regular file owned by UID 0 with no group or world write permission. The check reads owner, mode and regular-file status from the same opened descriptor that supplies the contents, so a path swap cannot defeat it. Symlinks are followed and judged on the target inode.")
	b.WriteString(".Pp\n")
	writeText(b, "An untrusted path key is ignored, never executed, and produces one warning naming the path, the key and the observed owner and mode. Non-privileged keys from the same layer still apply. The system layer is not automatically trusted. A legacy xbps_*_path key is gated identically after alias resolution. Non-root runs honour every layer.")

	b.WriteString(".Sh EXIT STATUS\n")
	b.WriteString(".Bl -tag -width Ds\n")
	b.WriteString(".It Cm 0\n")
	writeText(b, "The configuration parsed and the command ran successfully.")
	b.WriteString(".It Cm 2\n")
	writeText(b, "An existing layer is unreadable, invalid TOML, contains an unknown key, contains an out-of-range value, or an explicit --config file is missing.")
	b.WriteString(".El\n")
	writeText(b, "An xbps child's exit status is passed through unchanged.")

	b.WriteString(".Sh SECURITY\n")
	writeText(b, "The package installs no file under /etc, creates no /etc/xbpretty directory, and has no conf_files entry. The system layer is optional and administrator-created. Do not make a privileged configuration layer group- or world-writable: its [Xbps] binary paths are ignored under root.")
	b.WriteString(".Pp\n")
	writeText(b, "The file is parsed as a strict TOML subset. It cannot execute code, include another file, or fetch remote content. xbpretty never writes the file, so a successful run leaves the administrator's or user's contents byte-identical.")

	b.WriteString(".Sh SEE ALSO\n")
	writeText(b, "xbpretty(8)")
	b.WriteString(".Pp\n")
	writeText(b, "https://github.com/soubarnak/xbpretty")
	return b.String()
}

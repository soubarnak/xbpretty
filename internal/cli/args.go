package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/soubarnak/xbpretty/internal/spec"
)

// options is one parsed command line: xbpretty's own flags, the command, the
// package arguments, and everything after `--` for xbps.
type options struct {
	// Transaction state kept from rev 5 and used by the transaction core.
	dryRun     bool
	yes        bool
	assumeNo   bool // --assume-no; -n is its short form
	verbose    bool
	noSync     bool
	noUpdate   bool
	noLog      bool
	color      string // "", "auto", "always" or "never"
	version    bool
	help       bool
	completion bool
	command    string
	subcommand string
	args       []string
	passthru   []string
	dashDash   bool   // a `--` was given, even with nothing after it
	limit      int    // --limit N for `log`; 0 means not given
	configPath string // --config FILE; empty means the default layers

	// Rev 6 globals and resolved settings inputs.
	debug         bool
	noColor       bool
	update        bool
	simple        bool
	full          bool
	noFull        bool
	autoRemove    *bool // nil when neither flag was given
	autoRemovePos bool
	autoRemoveNeg bool
	historyDir    string

	// Command flags.
	country              []string
	countrySpelling      string
	countryBeforeCommand bool
	downloadOnly         bool
	purge                bool
	reinstall            bool
	safe                 bool
	printURIs            bool
	machine              bool
	allVersions          bool
	allArches            bool
	installed            bool
	upgradable           bool
	virtual              bool
	description          bool
	summaryFlag          bool
	namesOnly            bool
	lists                bool
	cleanFetch           bool
	clearAll             bool
	auto                 int
	httpsOnly            bool
	nonFree              bool
	refusals             []string
	explicit             map[string]bool
}

// usageError is a parser error with an optional second stderr line, used for
// the -c/--config collision (FR-059).
type usageError struct {
	msg  string
	hint string
}

func (e *usageError) Error() string { return e.msg }

func errCountryHint() error {
	return &usageError{
		msg:  "unknown flag -c",
		hint: "-c is fetch's --country; config is --config",
	}
}

// occurrence is one flag seen on the command line, before its command is
// known or its value is validated.
type occurrence struct {
	flag          spec.Flag
	short         rune
	value         string
	hasValue      bool
	beforeCommand bool // seen before the command word; -V then means --version
}

// parseArgs reads xbpretty's flags, the command and the package arguments.
// Flags may appear before or after the command; everything after `--` is
// passed verbatim to xbps. An already known command, taken from argv[0] for
// the xbp-* link names, means the first remaining argument is a package.
func parseArgs(argv []string, knownCommand string) (*options, error) {
	o := &options{explicit: map[string]bool{}, command: knownCommand}
	command := knownCommand
	commandKnown := knownCommand != ""

	var occurrences []occurrence
	var positional []string
	var passthru []string

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--":
			o.dashDash = true
			passthru = append(passthru, argv[i+1:]...)
			i = len(argv)
		case strings.HasPrefix(arg, "--"):
			occ, next, err := parseLongFlag(argv, i)
			if err != nil {
				return nil, err
			}
			occ.beforeCommand = !commandKnown
			i = next
			occurrences = append(occurrences, occ)
		case len(arg) > 1 && arg[0] == '-':
			occs, next, err := parseShortFlags(argv, i, commandKnown)
			if err != nil {
				return nil, err
			}
			i = next
			occurrences = append(occurrences, occs...)
		default:
			if commandKnown {
				positional = append(positional, arg)
				continue
			}
			canonical, ok := spec.LookupCommand(arg)
			if !ok {
				return nil, fmt.Errorf("unknown command %q", arg)
			}
			command, commandKnown = canonical, true
		}
	}

	o.passthru = passthru
	o.dashDash = o.dashDash || len(passthru) > 0

	if commandKnown {
		o.command, o.subcommand, o.args = spec.NormalizeHistory(command, positional)
	} else {
		o.args = positional
	}

	if err := applyOccurrences(o, occurrences); err != nil {
		return nil, err
	}
	if o.command == "completion" {
		o.completion = true
	}
	return o, nil
}

// parseLongFlag reads one --flag[=value] and returns the index of the last
// argv element it consumed.
func parseLongFlag(argv []string, i int) (occurrence, int, error) {
	arg := argv[i]
	name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	f, ok := spec.LookupFlag(name)
	if !ok {
		return occurrence{}, i, fmt.Errorf("unknown flag --%s", name)
	}
	occ := occurrence{flag: f, value: value, hasValue: hasValue}
	switch f.Kind {
	case spec.ValueFlag:
		if !hasValue {
			if i+1 >= len(argv) {
				if f.Long == "country" {
					return occurrence{}, i, fmt.Errorf("-c/--country needs a value")
				}
				return occurrence{}, i, fmt.Errorf("--%s needs a value%s", name, valueHint(f))
			}
			i++
			occ.value = argv[i]
			occ.hasValue = true
		}
	case spec.OptionalValueFlag:
		if !hasValue && f.Long == "auto" && i+1 < len(argv) && isDigits(argv[i+1]) {
			i++
			occ.value = argv[i]
			occ.hasValue = true
		}
	}
	return occ, i, nil
}

// parseShortFlags reads one short cluster such as -yn or -c IN. A value-taking
// short consumes the rest of the cluster or the next argument.
func parseShortFlags(argv []string, i int, commandKnown bool) ([]occurrence, int, error) {
	body := strings.TrimPrefix(argv[i], "-")
	var out []occurrence
	for j := 0; j < len(body); j++ {
		c := rune(body[j])
		switch c {
		case 'c', 'o', 't':
			if c == 'c' && !commandKnown {
				return nil, i, errCountryHint()
			}
			value := strings.TrimPrefix(body[j+1:], "=")
			hasValue := value != ""
			if !hasValue {
				if i+1 >= len(argv) {
					if c == 'c' {
						return nil, i, fmt.Errorf("-c/--country needs a value")
					}
					return nil, i, fmt.Errorf("-%c needs a value", c)
				}
				i++
				value = argv[i]
				hasValue = true
			}
			out = append(out, occurrence{short: c, value: value, hasValue: hasValue, beforeCommand: !commandKnown})
			return out, i, nil
		default:
			if !globalShortOrFlag(c) {
				return nil, i, fmt.Errorf("unknown flag -%c", c)
			}
			out = append(out, occurrence{short: c, beforeCommand: !commandKnown})
		}
	}
	return out, i, nil
}

func valueHint(f spec.Flag) string {
	if len(f.Values) > 0 {
		return ": " + strings.Join(f.Values, ", ")
	}
	if f.ValueName != "" {
		return " " + f.ValueName
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// globalShortOrFlag reports whether c names a short flag anywhere in the
// table. Command-specific shorts are validated once the command is known.
func globalShortOrFlag(c rune) bool {
	for _, f := range spec.Flags {
		if f.Short == c {
			return true
		}
	}
	return false
}

// applyOccurrences validates each flag against the resolved command and folds
// it into options, then enforces FR-003's conflict pairs and the -c scope.
func applyOccurrences(o *options, occurrences []occurrence) error {
	for _, occ := range occurrences {
		f, err := resolveOccurrence(o.command, occ)
		if err != nil {
			return err
		}
		if f.Refusal != "" {
			o.refusals = append(o.refusals, f.Refusal)
		}
		if err := applyFlag(o, f, occ); err != nil {
			return err
		}
	}
	if err := o.checkCountry(); err != nil {
		return err
	}
	return checkConflicts(o)
}

// checkCountry reserves -c/--country for fetch after the command: on any
// other command, or before the command word, it is an unknown flag with the
// FR-059 hint.
func (o *options) checkCountry() error {
	if len(o.country) == 0 {
		return nil
	}
	if o.command != "fetch" || o.countryBeforeCommand {
		msg := "unknown flag -c"
		if o.countrySpelling == "country" {
			msg = "unknown flag --country"
		}
		return &usageError{
			msg:  msg,
			hint: "-c is fetch's --country; config is --config",
		}
	}
	return nil
}

// resolveOccurrence maps a short flag to its table entry for this command.
// V and n change meaning with the command, exactly as FR-004 and FR-027 say.
func resolveOccurrence(command string, occ occurrence) (spec.Flag, error) {
	if occ.flag.Long != "" {
		if !spec.Accepts(command, occ.flag.Long) && !mooAccepts(command, occ.flag.Long) {
			if occ.flag.Long == "country" && command != "fetch" {
				return spec.Flag{}, &usageError{
					msg:  "unknown flag --country",
					hint: "-c is fetch's --country; config is --config",
				}
			}
			return spec.Flag{}, fmt.Errorf("unknown flag --%s", occ.flag.Long)
		}
		return occ.flag, nil
	}

	c := occ.short
	// -c is fetch's --country after the command and nothing else. The hint
	// is the FR-059 answer both before the command and on every other
	// command, so it cannot be derived from the accepted-after-command set.
	if c == 'c' && (occ.beforeCommand || command != "fetch") {
		return spec.Flag{}, errCountryHint()
	}
	if f, ok := shortFlagFor(command, c, occ.beforeCommand); ok {
		return f, nil
	}
	return spec.Flag{}, fmt.Errorf("unknown flag -%c", c)
}

// shortFlagFor resolves a short from the shared table. A command-specific
// flag wins after the command (list/search turn -V into --virtual and search
// turns -n into --names); before the command a global short wins (-V is
// --version). The table, not this function, decides which commands accept
// which short.
func shortFlagFor(command string, c rune, beforeCommand bool) (spec.Flag, bool) {
	var global, specific spec.Flag
	var hasGlobal, hasSpecific bool
	for _, f := range spec.FlagsFor(command) {
		if f.Short != c {
			continue
		}
		if f.Global {
			global, hasGlobal = f, true
		} else {
			specific, hasSpecific = f, true
		}
	}
	if !beforeCommand && hasSpecific {
		return specific, true
	}
	if hasGlobal {
		return global, true
	}
	if hasSpecific {
		return specific, true
	}
	return spec.Flag{}, false
}

// mooAccepts keeps the hidden moo easter egg's two joke flags out of the
// shared table's advertised command lists.
func mooAccepts(command, long string) bool {
	return command == "moo" && (long == "update" || long == "no-update")
}

// applyFlag folds one validated flag into options.
func applyFlag(o *options, f spec.Flag, occ occurrence) error {
	switch f.Long {
	case "help":
		o.help = true
	case "version":
		o.version = true
	case "verbose":
		o.verbose = true
		o.explicit["verbose"] = true
	case "debug":
		o.debug = true
		o.verbose = true
		o.explicit["debug"] = true
	case "config":
		if err := validateConfigPath(occ.value); err != nil {
			return err
		}
		o.configPath = occ.value
		o.explicit["config"] = true
	case "color":
		if !spec.AcceptsValue("color", occ.value) {
			f, _ := spec.LookupFlag("color")
			return fmt.Errorf("--color must be %s, got %q", strings.Join(f.Values, ", "), occ.value)
		}
		o.color = occ.value
		o.explicit["color"] = true
	case "no-color":
		o.noColor = true
		o.explicit["no_color"] = true
	case "tui":
		o.explicit["tui"] = true
	case "no-tui":
		o.explicit["no_tui"] = true
	case "history-dir":
		if occ.value == "" {
			return fmt.Errorf("--history-dir needs a directory")
		}
		o.historyDir = occ.value
		o.explicit["history_dir"] = true
	case "dry-run":
		o.dryRun = true
		o.explicit["dry-run"] = true
	case "assume-no":
		// FR-015: -n is --assume-no only; the dry run is the long-only
		// --dry-run, so the two can be combined to print the plan and stop.
		o.assumeNo = true
		o.explicit["assume_no"] = true
	case "yes":
		o.yes = true
		o.explicit["yes"] = true
	case "download-only":
		o.downloadOnly = true
		o.explicit["download-only"] = true
	case "simple":
		o.simple = true
		o.explicit["simple"] = true
	case "update":
		o.update = true
		o.noSync = false
		o.explicit["update"] = true
		o.explicit["sync"] = true
	case "no-update":
		o.noUpdate = true
		o.noSync = true
		o.explicit["sync"] = true
	case "no-sync":
		o.noUpdate = true
		o.noSync = true
		o.explicit["sync"] = true
	case "purge":
		o.purge = true
		o.explicit["purge"] = true
	case "reinstall":
		o.reinstall = true
		o.explicit["reinstall"] = true
	case "auto-remove":
		val := true
		o.autoRemove = &val
		o.autoRemovePos = true
		o.explicit["auto_remove"] = true
	case "no-auto-remove":
		val := false
		o.autoRemove = &val
		o.autoRemoveNeg = true
		o.explicit["auto_remove"] = true
	case "full":
		o.full = true
		o.explicit["full"] = true
	case "no-full":
		o.noFull = true
		o.explicit["no_full"] = true
	case "safe":
		o.safe = true
		o.full = true
		o.explicit["full"] = true
	case "print-uris":
		o.printURIs = true
		o.explicit["print-uris"] = true
	case "no-log":
		o.noLog = true
		o.explicit["log"] = true
	case "virtual":
		o.virtual = true
		o.explicit["virtual"] = true
	case "country":
		if occ.value == "" {
			return fmt.Errorf("-c/--country needs a value")
		}
		o.country = append(o.country, occ.value)
		if occ.short == 'c' {
			o.countrySpelling = "c"
		} else {
			o.countrySpelling = "country"
		}
		o.countryBeforeCommand = occ.beforeCommand
	case "all-versions":
		o.allVersions = true
		o.explicit["all-versions"] = true
	case "all-arches":
		o.allArches = true
		o.explicit["all-arches"] = true
	case "installed":
		o.installed = true
		o.explicit["installed"] = true
	case "upgradable":
		o.upgradable = true
		o.explicit["upgradable"] = true
	case "description":
		o.description = true
		o.explicit["description"] = true
	case "summary":
		o.summaryFlag = true
		o.explicit["summary"] = true
	case "machine":
		o.machine = true
		o.explicit["machine"] = true
	case "names":
		o.namesOnly = true
		o.explicit["names"] = true
	case "limit":
		n, err := strconv.Atoi(occ.value)
		if err != nil || n < 1 {
			return fmt.Errorf("--limit must be a positive integer, got %q", occ.value)
		}
		o.limit = n
		o.explicit["limit"] = true
	case "lists":
		o.lists = true
		o.explicit["lists"] = true
	case "fetch":
		o.cleanFetch = true
		o.explicit["clean-fetch"] = true
	case "all":
		o.clearAll = true
		o.explicit["all"] = true
	case "auto":
		n := 3
		if occ.hasValue {
			parsed, err := strconv.Atoi(occ.value)
			if err != nil {
				return fmt.Errorf("--auto must be between 1 and 255")
			}
			n = parsed
		}
		if n < 1 || n > 255 {
			return fmt.Errorf("--auto must be between 1 and 255")
		}
		o.auto = n
		o.explicit["auto"] = true
	case "https-only":
		o.httpsOnly = true
		o.explicit["https-only"] = true
	case "non-free":
		o.nonFree = true
		o.explicit["non-free"] = true
	case "no-fix-broken", "no-install-recommends", "no-install-suggests":
		o.explicit[f.Long] = true
	}
	return nil
}

func validateConfigPath(value string) error {
	if value == "" {
		return fmt.Errorf("--config needs a file")
	}
	if value == "-" || value == "/dev/stdin" {
		return fmt.Errorf("--config %s is not supported; use a regular file", value)
	}
	return nil
}

// checkConflicts enforces FR-003's conflict pairs.
func checkConflicts(o *options) error {
	if o.noColor && o.explicit["color"] {
		return fmt.Errorf("--color and --no-color are mutually exclusive")
	}
	if o.explicit["tui"] && o.explicit["no_tui"] {
		return fmt.Errorf("--tui and --no-tui are mutually exclusive")
	}
	if o.yes && o.assumeNo {
		return fmt.Errorf("-y and --assume-no are mutually exclusive")
	}
	if o.update && o.noUpdate {
		return fmt.Errorf("--update and --no-update/--no-sync are mutually exclusive")
	}
	if o.autoRemovePos && o.autoRemoveNeg {
		return fmt.Errorf("--auto-remove and --no-auto-remove are mutually exclusive")
	}
	if o.printURIs && (o.dryRun || o.downloadOnly) {
		return fmt.Errorf("--print-uris cannot be combined with --dry-run or --download-only")
	}
	return nil
}

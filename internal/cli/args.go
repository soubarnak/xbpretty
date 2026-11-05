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
	dryRun      bool
	yes         bool
	assumeNo    bool // -n's rev 6 meaning; the dry run is --dry-run (FR-015)
	verbose     bool
	noSync      bool
	noLog       bool
	color       string // "", "auto", "always" or "never"
	version     bool
	help        bool
	completion  bool
	command     string
	subcommand  string
	args        []string
	passthru    []string
	dashDash    bool // a `--` was given, even with nothing after it
	limit       int  // --limit N for `log`; 0 means not given
	boxStyle    string
	progressBar *bool
	configPath  string // --config FILE; empty means the default user layer

	// Rev 6 globals.
	debug      bool
	noColor    bool
	tuiSet     bool
	tui        bool
	noTui      bool
	historyDir string

	// Command flags.
	country      []string
	downloadOnly bool
	simple       bool
	updateFlag   bool
	purge        bool
	reinstall    bool
	autoRemove   bool
	noAutoRemove bool
	full         bool
	noFull       bool
	safe         bool
	printURIs    bool
	machine      bool
	allVersions  bool
	allArches    bool
	installed    bool
	upgradable   bool
	virtual      bool
	description  bool
	summaryFlag  bool
	namesOnly    bool
	lists        bool
	cleanFetch   bool
	clearAll     bool
	auto         int
	httpsOnly    bool
	nonFree      bool
	refusals     []string
	explicit     map[string]bool
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

	if !commandKnown && len(passthru) > 0 {
		// `xbpretty -- pkg` carries no command: treat it as an install.
		command, commandKnown = "install", true
		positional = append(positional, passthru...)
		passthru = nil
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
			value := body[j+1:]
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
// it into options, then enforces FR-003's conflict pairs.
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
	return checkConflicts(o)
}

// resolveOccurrence maps a short flag to its table entry for this command.
// V and n change meaning with the command, exactly as FR-004 and FR-027 say.
func resolveOccurrence(command string, occ occurrence) (spec.Flag, error) {
	if occ.flag.Long != "" {
		if !spec.Accepts(command, occ.flag.Long) {
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
	switch c {
	case 'V':
		name := "version"
		if !occ.beforeCommand && (command == "list" || command == "search") {
			name = "virtual"
		}
		f, _ := spec.LookupFlag(name)
		return f, nil
	case 'n':
		switch {
		case command == "search":
			f, _ := spec.LookupFlag("names")
			return f, nil
		case command == "fetch":
			// FR-046: fetch has --dry-run but not the short -n.
			return spec.Flag{}, fmt.Errorf("unknown flag -n")
		case spec.IsTransaction(command):
			f, _ := spec.LookupFlag("assume-no")
			return f, nil
		default:
			return spec.Flag{}, fmt.Errorf("unknown flag -n")
		}
	case 'c':
		if command != "fetch" {
			return spec.Flag{}, errCountryHint()
		}
	}

	for _, f := range spec.FlagsFor(command) {
		if f.Short == c {
			return f, nil
		}
	}
	return spec.Flag{}, fmt.Errorf("unknown flag -%c", c)
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
		if !validColor(occ.value) {
			return fmt.Errorf("--color must be auto, always or never, got %q", occ.value)
		}
		o.color = occ.value
		o.explicit["color"] = true
	case "no-color":
		o.noColor = true
		o.explicit["no-color"] = true
	case "tui":
		o.tuiSet = true
		o.tui = true
		o.explicit["tui"] = true
	case "no-tui":
		o.tuiSet = true
		o.noTui = true
		o.explicit["no-tui"] = true
	case "history-dir":
		if occ.value == "" {
			return fmt.Errorf("--history-dir needs a directory")
		}
		o.historyDir = occ.value
		o.explicit["history-dir"] = true
	case "dry-run":
		o.dryRun = true
		o.explicit["dry-run"] = true
	case "assume-no":
		o.assumeNo = true
		o.explicit["assume-no"] = true
		// Compatibility until the transaction core (#62) implements the
		// refusal: rev 5 callers used -n for the dry run, so -n still selects
		// the plan while rev 6's meaning is recorded for #62.
		o.dryRun = true
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
		o.updateFlag = true
		o.noSync = false
		o.explicit["update"] = true
		o.explicit["sync"] = true
	case "no-update":
		o.noSync = true
		o.explicit["no-update"] = true
		o.explicit["sync"] = true
	case "no-sync":
		o.noSync = true
		o.explicit["no-sync"] = true
		o.explicit["sync"] = true
	case "purge":
		o.purge = true
		o.explicit["purge"] = true
	case "reinstall":
		o.reinstall = true
		o.explicit["reinstall"] = true
	case "auto-remove":
		o.autoRemove = true
		o.explicit["auto-remove"] = true
	case "no-auto-remove":
		o.noAutoRemove = true
		o.explicit["no-auto-remove"] = true
	case "full":
		o.full = true
		o.explicit["full"] = true
	case "no-full":
		o.noFull = true
		o.explicit["no-full"] = true
	case "safe":
		o.safe = true
		o.explicit["safe"] = true
	case "print-uris":
		o.printURIs = true
		o.explicit["print-uris"] = true
	case "no-log":
		o.noLog = true
		o.explicit["log"] = true
	case "box-style":
		if !validBoxStyle(occ.value) {
			return fmt.Errorf("--box-style must be rounded, classic or ascii, got %q", occ.value)
		}
		o.boxStyle = occ.value
		o.explicit["box_style"] = true
	case "progress-bar":
		val := true
		if occ.hasValue {
			b, err := parseBool(occ.value)
			if err != nil {
				return fmt.Errorf("--progress-bar value error: %w", err)
			}
			val = b
		}
		o.progressBar = &val
		o.explicit["progress_bar"] = true
	case "no-progress", "no-progress-bar":
		val := false
		o.progressBar = &val
		o.explicit["progress_bar"] = true
	case "virtual":
		o.virtual = true
		o.explicit["virtual"] = true
	case "country":
		if occ.value == "" {
			return fmt.Errorf("-c/--country needs a value")
		}
		o.country = append(o.country, occ.value)
		o.explicit["country"] = true
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
	// Refusal flags only need to be recorded; ensure the parser still accepts
	// a value they carry.
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
	pairs := []struct {
		a, b string
		msg  string
	}{
		{"color", "no-color", "--color and --no-color cannot be used together"},
		{"tui", "no-tui", "--tui and --no-tui cannot be used together"},
		{"yes", "assume-no", "--yes and --assume-no cannot be used together"},
		{"auto-remove", "no-auto-remove", "--auto-remove and --no-auto-remove cannot be used together"},
	}
	for _, p := range pairs {
		if o.explicit[p.a] && o.explicit[p.b] {
			return fmt.Errorf("%s", p.msg)
		}
	}
	if o.explicit["update"] && (o.explicit["no-update"] || o.explicit["no-sync"]) {
		return fmt.Errorf("--update and --no-update/--no-sync cannot be used together")
	}
	return nil
}

func validColor(value string) bool {
	switch value {
	case "auto", "always", "never":
		return true
	}
	return false
}

func validBoxStyle(value string) bool {
	switch value {
	case "rounded", "classic", "ascii":
		return true
	}
	return false
}

func parseBool(value string) (bool, error) {
	switch value {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	}
	return false, fmt.Errorf("expected true or false, got %q", value)
}

package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// options is one parsed command line: xbpretty's own flags, the command, the
// package arguments, and everything after `--` for xbps.
type options struct {
	dryRun               bool
	yes                  bool
	verbose              bool
	noSync               bool
	noLog                bool
	color                string // "", "auto", "always" or "never"
	version              bool
	help                 bool
	completion           bool
	command              string
	args                 []string
	passthru             []string
	dashDash             bool // a `--` was given, even with nothing after it
	limit                int  // --limit N for `log`; 0 means not given
	boxStyle             string
	progressBar          *bool
	configPath           string // --config FILE; empty means the default layers
	debug                bool   // --debug: report layers, legacy keys and the resolved config
	noColor              bool   // --no-color: stronger than --color
	update               bool   // --update: force the sync step
	noUpdate             bool   // --no-update / --no-sync: suppress the sync step
	simple               bool   // --simple
	full                 bool   // --full / --safe
	noFull               bool   // --no-full
	autoRemove           *bool  // --auto-remove / --no-auto-remove
	autoRemovePos        bool
	autoRemoveNeg        bool
	assumeNo             bool     // --assume-no
	historyDir           string   // --history-dir DIR
	country              []string // fetch's -c/--country
	countryBeforeCommand bool
	explicit             map[string]bool
}

// usageError is a usage rejection that also prints a hint line. FR-059's -c
// collision uses it.
type usageError struct {
	msg  string
	hint string
}

func (e *usageError) Error() string { return e.msg }

// parseArgs reads xbpretty's flags, the command and the package arguments.
// Flags may appear before or after the command; everything after `--` is
// passed verbatim to xbps. An already known command, taken from argv[0] for
// the xbp-* link names, means the first remaining argument is a package.
func parseArgs(argv []string, knownCommand string) (*options, error) {
	o := &options{explicit: map[string]bool{}, color: "", command: knownCommand}
	var rest []string

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--":
			o.dashDash = true
			o.passthru = append(o.passthru, argv[i+1:]...)
			i = len(argv)
		case strings.HasPrefix(arg, "--"):
			name, value, hasValue := strings.Cut(arg[2:], "=")
			switch name {
			case "dry-run":
				o.dryRun = true
			case "yes":
				o.yes = true
				o.explicit["yes"] = true
			case "verbose":
				o.verbose = true
				o.explicit["verbose"] = true
			case "no-sync":
				o.noSync = true
				o.noUpdate = true
				o.explicit["sync"] = true
			case "no-log":
				o.noLog = true
				o.explicit["log"] = true
			case "color":
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("--color needs a value: auto, always or never")
					}
					i++
					value = argv[i]
				}
				if !validColor(value) {
					return nil, fmt.Errorf("--color must be auto, always or never, got %q", value)
				}
				o.color = value
				o.explicit["color"] = true
			case "config":
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("--config needs a file")
					}
					i++
					value = argv[i]
				}
				if value == "" {
					return nil, fmt.Errorf("--config needs a file")
				}
				if value == "-" || value == "/dev/stdin" {
					return nil, fmt.Errorf("--config %s is not supported; use a regular file", value)
				}
				o.configPath = value
				o.explicit["config"] = true
			case "limit":
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("--limit needs a positive integer")
					}
					i++
					value = argv[i]
				}
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("--limit must be a positive integer, got %q", value)
				}
				o.limit = n
				o.explicit["limit"] = true
			case "debug":
				o.debug = true
				o.explicit["debug"] = true
			case "no-color":
				o.noColor = true
				o.explicit["no_color"] = true
			case "tui":
				o.explicit["tui"] = true
			case "no-tui":
				o.explicit["no_tui"] = true
			case "update":
				o.update = true
				o.explicit["update"] = true
			case "no-update":
				o.noUpdate = true
				o.explicit["sync"] = true
			case "simple":
				o.simple = true
				o.explicit["simple"] = true
			case "full", "safe":
				o.full = true
				o.explicit["full"] = true
			case "no-full":
				o.noFull = true
				o.explicit["no_full"] = true
			case "auto-remove", "autoremove":
				val := true
				o.autoRemove = &val
				o.autoRemovePos = true
				o.explicit["auto_remove"] = true
			case "no-auto-remove", "no-autoremove":
				val := false
				o.autoRemove = &val
				o.autoRemoveNeg = true
				o.explicit["auto_remove"] = true
			case "assume-no":
				o.assumeNo = true
				o.explicit["assume_no"] = true
			case "history-dir":
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("--history-dir needs a directory")
					}
					i++
					value = argv[i]
				}
				if value == "" {
					return nil, fmt.Errorf("--history-dir needs a directory")
				}
				o.historyDir = value
				o.explicit["history_dir"] = true
			case "country":
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("-c/--country needs a value")
					}
					i++
					value = argv[i]
				}
				o.country = append(o.country, value)
				if knownCommand == "" && len(rest) == 0 {
					o.countryBeforeCommand = true
				}
			case "version":
				o.version = true
			case "help":
				o.help = true
			case "box-style":
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("--box-style needs a value: rounded, classic or ascii")
					}
					i++
					value = argv[i]
				}
				if !validBoxStyle(value) {
					return nil, fmt.Errorf("--box-style must be rounded, classic or ascii, got %q", value)
				}
				o.boxStyle = value
				o.explicit["box_style"] = true
			case "progress-bar":
				val := true
				if hasValue {
					b, err := parseBool(value)
					if err != nil {
						return nil, fmt.Errorf("--progress-bar value error: %w", err)
					}
					val = b
				}
				o.progressBar = &val
				o.explicit["progress_bar"] = true
			case "no-progress", "no-progress-bar":
				val := false
				o.progressBar = &val
				o.explicit["progress_bar"] = true
			default:
				return nil, fmt.Errorf("unknown flag --%s", name)
			}
		case len(arg) > 1 && arg[0] == '-':
			// -c is fetch's --country; it is never --config.
			if arg == "-c" || strings.HasPrefix(arg, "-c=") {
				value := ""
				if strings.HasPrefix(arg, "-c=") {
					value = strings.TrimPrefix(arg, "-c=")
				} else {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("-c/--country needs a value")
					}
					i++
					value = argv[i]
				}
				o.country = append(o.country, value)
				if knownCommand == "" && len(rest) == 0 {
					o.countryBeforeCommand = true
				}
				continue
			}
			// A cluster such as -yn, or a long flag written short (-color=never).
			if name, value, _ := strings.Cut(strings.TrimPrefix(arg, "-"), "="); name == "color" {
				if !validColor(value) {
					return nil, fmt.Errorf("--color must be auto, always or never, got %q", value)
				}
				o.color = value
				o.explicit["color"] = true
				continue
			}
			for _, c := range strings.TrimPrefix(arg, "-") {
				switch c {
				case 'n':
					o.dryRun = true
				case 'y':
					o.yes = true
					o.explicit["yes"] = true
				case 'v':
					o.verbose = true
					o.explicit["verbose"] = true
				case 'h':
					o.help = true
				default:
					return nil, fmt.Errorf("unknown flag -%c", c)
				}
			}
		default:
			rest = append(rest, arg)
		}
	}

	if err := o.checkConflicts(); err != nil {
		return nil, err
	}
	if len(rest) == 0 {
		if err := o.checkCountry(); err != nil {
			return nil, err
		}
		if knownCommand == "" && len(o.passthru) > 0 {
			// `xbpretty -- pkg` carries no command: treat it as an install.
			o.command = "install"
			o.args = o.passthru
			o.passthru = nil
			return o, nil
		}
		// A command from argv[0] survives a pass-through that leaves no
		// positional argument, so `xbp-remove -- -F pkg` stays a remove
		// (FR-018, FR-022).
		return o, nil
	}

	switch {
	case knownCommand != "":
		// argv[0] already named the command: the rest are packages.
		o.args = rest
	case rest[0] == "completion":
		o.command = "completion"
		o.completion = true
		o.args = rest[1:]
	case rest[0] == "link":
		o.command = "link"
		o.args = rest[1:]
	case rest[0] == "log":
		// `xbp log` only: no xbp-log link name, so argv[0] never reaches here.
		o.command = "log"
		o.args = rest[1:]
	default:
		cmd, ok := aliases[rest[0]]
		if !ok {
			return nil, fmt.Errorf("unknown command %q", rest[0])
		}
		o.command = cmd
		o.args = rest[1:]
	}
	if err := o.checkCountry(); err != nil {
		return nil, err
	}
	return o, nil
}

// checkConflicts enforces the flag pairs FR-003 makes mutually exclusive.
func (o *options) checkConflicts() error {
	// FR-057 gives --no-color stronger precedence than --color, so both may
	// be present; the resolver picks Never.
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
	return nil
}

// checkCountry reserves -c for fetch: on any other command (or before the
// command) it is an unknown flag with the FR-059 hint.
func (o *options) checkCountry() error {
	if len(o.country) == 0 {
		return nil
	}
	if o.command != "fetch" || o.countryBeforeCommand {
		return &usageError{
			msg:  "unknown flag -c",
			hint: "xbpretty: -c is fetch's --country; config is --config",
		}
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

const usageText = `xbpretty - a friendlier front end for xbps

Usage:
  xbpretty <command> [flags] [packages...]

Commands:
  install     install packages        (wraps xbps-install -S)
  remove      remove packages         (wraps xbps-remove)
  upgrade     upgrade everything      (wraps xbps-install -Su)
  autoremove  remove orphaned packages (wraps xbps-remove -o)
  update      refresh the index only  (wraps xbps-install -S)
  completion  print a completion script for bash, zsh or fish
  link        create the xbp symlinks in a directory
  log         print the transaction log (log [--limit N] [package...])

Flags:
  -n, --dry-run     show the plan without changing anything
  -y, --yes         proceed without prompting
  -v, --verbose     print every xbps command before running it
      --no-sync     do not refresh the repository index
      --no-log      do not append to the transaction log
      --limit N     with log: keep only the last N matching lines
      --color=WHEN  colour the summary: auto, always or never
      --config FILE read FILE instead of the user config file
      --version     print version information

Arguments after -- are passed to xbps unchanged, in both the dry run and the
real run, e.g. xbpretty install -- -f pkg.
`

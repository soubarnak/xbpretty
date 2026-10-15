package cli

import (
	"fmt"
	"strings"
)

// options is one parsed command line: xbpretty's own flags, the command, the
// package arguments, and everything after `--` for xbps.
type options struct {
	dryRun     bool
	yes        bool
	verbose    bool
	noSync     bool
	noLog      bool
	color      string // "", "auto", "always" or "never"
	version    bool
	help       bool
	completion bool
	command    string
	args       []string
	passthru   []string
	explicit   map[string]bool
}

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
			case "version":
				o.version = true
			case "help":
				o.help = true
			default:
				return nil, fmt.Errorf("unknown flag --%s", name)
			}
		case len(arg) > 1 && arg[0] == '-':
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

	if len(rest) == 0 {
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
	default:
		cmd, ok := aliases[rest[0]]
		if !ok {
			return nil, fmt.Errorf("unknown command %q", rest[0])
		}
		o.command = cmd
		o.args = rest[1:]
	}
	return o, nil
}

func validColor(value string) bool {
	switch value {
	case "auto", "always", "never":
		return true
	}
	return false
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

Flags:
  -n, --dry-run     show the plan without changing anything
  -y, --yes         proceed without prompting
  -v, --verbose     print every xbps command before running it
      --no-sync     do not refresh the repository index
      --no-log      do not append to the transaction log
      --color=WHEN  colour the summary: auto, always or never
      --version     print version information

Arguments after -- are passed to xbps unchanged, in both the dry run and the
real run, e.g. xbpretty install -- -f pkg.
`

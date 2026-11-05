package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/soubarnak/xbpretty/internal/completion"
	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/spec"
	"github.com/soubarnak/xbpretty/internal/summary"
)

// Exit codes, per NFR-003.
const (
	exitOK          = 0
	exitRuntime     = 1
	exitUsage       = 2
	exitNothingToDo = 0
)

// isTerminalFn reports whether a file is a terminal. Tests replace it to
// exercise both the interactive and the piped paths.
var isTerminalFn = isTerminal

// euidFn returns the effective user id. Tests replace it to exercise the
// root and non-root paths.
var euidFn = os.Geteuid

// stdinFn is the reader the prompt asks. Tests replace it to script answers.
var stdinFn = func() io.Reader { return os.Stdin }

// runner executes xbps commands, honouring the verbose flag and the
// configured binary paths.
type runner struct {
	cfg config.Config
	// out receives interactive text and verbose lines, always stderr.
	out        io.Writer
	verbose    bool
	lastStderr string
	lastStdout string
	quiet      bool

	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
	parser *StreamParser
	widget *ProgressWidget

	// verboseMu serialises the verbose line written by the discovery worker
	// pool, where several xbps-query children run at once.
	verboseMu sync.Mutex
}

// state is everything one invocation of xbpretty needs.
type state struct {
	opts        *options
	stdout      io.Writer
	stderr      io.Writer
	stdin       io.Reader
	r           *runner
	color       bool
	autoUpdate  bool
	showPkgs    bool
	log         bool
	boxStyle    string
	progressBar bool
	unit        summary.SizeUnit
	mode        config.UIMode
	assumeYes   bool
	autoRemove  bool
	simple      bool
	full        bool
	historyDir  string
}

// Main runs xbpretty with the process arguments and returns its exit code.
func Main(argv []string) int {
	return Run(argv, stdinFn(), os.Stdout, os.Stderr)
}

// Run executes xbpretty with the specified arguments, standard input,
// standard output, and standard error streams, returning the exit code.
func Run(argv []string, in io.Reader, out, err io.Writer) int {
	// FR-074: a closed stdout pipe must not kill the run before it can keep
	// its transaction and exit code.
	ignoreSIGPIPE()
	if in == nil {
		in = stdinFn()
	}
	if out == nil {
		out = os.Stdout
	}
	if err == nil {
		err = os.Stderr
	}

	knownCmd, args := splitArgv(argv)

	opts, parseErr := parseArgs(args, knownCmd)
	if parseErr != nil {
		printUsageError(err, parseErr)
		return exitUsage
	}
	// FR-019: an N/A flag is accepted by the parser and refused before config
	// resolution, any child, and any summary.
	if len(opts.refusals) > 0 {
		for _, tail := range opts.refusals {
			fmt.Fprintf(err, "xbpretty: %s\n", tail)
		}
		return exitUsage
	}
	// FR-018 points --print-uris at §3.3.7, which #71 owns. Until it lands the
	// flag must be refused before config resolution, any child and any
	// prompt, so it can never fall through to a real transaction.
	if opts.printURIs {
		fmt.Fprintln(err, "xbpretty: --print-uris is not implemented in this build")
		return exitRuntime
	}

	st := &state{stdout: newResilientWriter(out), stderr: err, stdin: in}

	// moo is hidden and never touches config or xbps.
	if opts.command == "moo" {
		return runMoo(out, opts)
	}
	if opts.help {
		usage(out)
		return exitOK
	}
	if opts.version {
		printVersion(out, err)
		return exitOK
	}
	if opts.command == "" {
		usage(out)
		fmt.Fprintln(err, "xbpretty: no command given; run 'xbpretty --help' for usage")
		return exitRuntime
	}

	// Positional validation runs before config, so completion and link still
	// bypass config while their arity is checked.
	if code, stop := validateInvocation(opts, err); stop {
		return code
	}

	// FR-004 / FR-053: completion and link bypass config.
	if opts.command == "completion" {
		return st.runCompletion(opts)
	}
	if opts.command == "link" {
		return st.runLink(opts)
	}

	// FR-053: every remaining command validates the selected config layer.
	res, loadErr := loadConfig(opts)
	if loadErr != nil {
		fmt.Fprintf(err, "xbpretty: %v\n", loadErr)
		return exitUsage
	}
	cfg := res.Config
	resolved := resolveSettings(cfg, opts)
	reportConfig(err, opts, res, resolved)

	st.opts = opts
	st.mode = resolved.mode
	st.assumeYes = resolved.assumeYes
	st.autoRemove = resolved.autoRemove
	st.simple = resolved.simple
	st.full = resolved.full
	st.historyDir = resolved.historyDir
	st.autoUpdate = resolved.sync
	st.showPkgs = resolved.showPkgs
	st.log = resolved.log
	st.color = wantColor(resolved.color, opts)
	st.boxStyle = "rounded"
	if opts.explicit["box_style"] {
		st.boxStyle = opts.boxStyle
	}
	st.progressBar = resolved.mode != config.ModePlain
	if opts.explicit["progress_bar"] && opts.progressBar != nil {
		st.progressBar = *opts.progressBar
	}
	st.unit = summary.SizeUnit(resolved.unit)
	st.r = &runner{
		cfg:     cfg,
		out:     err,
		verbose: resolved.verbose,
		stdout:  out,
		stderr:  err,
		stdin:   in,
	}

	if opts.command == "log" {
		return st.runLog(opts)
	}
	return st.dispatch(opts)
}

// printUsageError prints a parse error and the optional FR-059 hint.
func printUsageError(err io.Writer, parseErr error) {
	fmt.Fprintf(err, "xbpretty: %v\n", parseErr)
	if ue, ok := parseErr.(*usageError); ok && ue.hint != "" {
		fmt.Fprintf(err, "xbpretty: %s\n", ue.hint)
	}
	usage(err)
}

// dispatch routes a parsed command to its implementation. Commands whose
// owning ticket has not landed yet go to the single runUnimplemented seam.
func (st *state) dispatch(opts *options) int {
	switch opts.command {
	case "log":
		return st.runLog(opts)
	case "install", "remove", "purge", "autoremove", "autopurge", "upgrade", "update":
		return st.runTransaction(opts.command, opts)
	case "full-upgrade", "safe-upgrade":
		// The upgrade family is an equivalence in FR-011.
		return st.runTransaction("upgrade", opts)
	case "list":
		return st.runList(opts)
	case "search":
		return st.runSearch(opts)
	case "show":
		return st.runShow(opts)
	case "fetch":
		return st.runFetch(opts)
	case "download":
		// TODO(#72): the download command's archive fetch belongs to #72.
		return st.runUnimplemented(opts.command, opts)
	case "clean":
		// TODO(#64): cache and repodata cleaning belongs to #64.
		return st.runUnimplemented(opts.command, opts)
	case "policy", "history", "undo", "redo":
		return st.runUnimplemented(opts.command, opts)
	}
	fmt.Fprintf(st.stderr, "xbpretty: unknown command %q\n", opts.command)
	return exitUsage
}

// validateInvocation applies FR-021's positional rules before config is read.
func validateInvocation(opts *options, err io.Writer) (int, bool) {
	args := opts.args
	switch opts.command {
	case "install", "remove", "purge", "download":
		if !hasPackageWord(opts) {
			fmt.Fprintf(err, "xbpretty: %s needs at least one package\n", opts.command)
			return exitRuntime, true
		}
	case "show":
		if len(args) == 0 {
			fmt.Fprintf(err, "xbpretty: %s needs at least one package\n", opts.command)
			return exitRuntime, true
		}
	case "search":
		if len(args) == 0 {
			fmt.Fprintln(err, "xbpretty: search needs at least one pattern")
			return exitRuntime, true
		}
	case "fetch":
		if len(args) > 0 || opts.dashDash {
			// FR-042: no positional arguments and no -- passthrough.
			fmt.Fprintln(err, "xbpretty: fetch takes no arguments and no -- passthrough")
			return exitUsage, true
		}
	case "update":
		// update has no transaction to pass `--` arguments to: it is a sync
		// plus one count dry run. Reject the passthrough rather than silently
		// dropping it (FR-012, FR-021).
		if opts.dashDash {
			fmt.Fprintln(err, "xbpretty: update takes no arguments and no -- passthrough")
			return exitUsage, true
		}
		if len(args) > 0 {
			fmt.Fprintf(err, "xbpretty: %s takes no package arguments\n", opts.command)
			return exitUsage, true
		}
	case "upgrade", "full-upgrade", "safe-upgrade", "autoremove", "autopurge", "clean":
		if len(args) > 0 {
			fmt.Fprintf(err, "xbpretty: %s takes no package arguments\n", opts.command)
			return exitUsage, true
		}
	case "completion":
		if len(args) == 0 || len(args) > 1 {
			fmt.Fprintln(err, "usage: xbpretty completion bash|zsh|fish")
			return exitUsage, true
		}
	case "link":
		if len(args) > 1 {
			fmt.Fprintln(err, "xbpretty: link takes at most one directory")
			return exitUsage, true
		}
	case "history":
		if opts.subcommand == "" || opts.subcommand == "info" || opts.subcommand == "show" {
			if len(args) > 1 {
				fmt.Fprintln(err, "xbpretty: history takes no package arguments")
				return exitUsage, true
			}
		}
		if (opts.subcommand == "info" || opts.subcommand == "show") && len(args) == 0 {
			fmt.Fprintln(err, "xbpretty: history info needs an entry id or 'last'")
			return exitUsage, true
		}
		if opts.subcommand == "clear" && len(args) > 1 {
			fmt.Fprintln(err, "xbpretty: history takes no package arguments")
			return exitUsage, true
		}
	case "undo", "redo":
		if len(args) > 1 {
			fmt.Fprintf(err, "xbpretty: %s takes at most one entry id or 'last'\n", opts.command)
			return exitUsage, true
		}
	}
	return exitOK, false
}

// hasPackageWord reports whether the invocation names a package. Arguments
// after -- count when they are package words; a bare `-- -f` is only an xbps
// flag and does not satisfy FR-021's at-least-one-package rule.
func hasPackageWord(opts *options) bool {
	if len(opts.args) > 0 {
		return true
	}
	for _, word := range opts.passthru {
		if word != "" && !strings.HasPrefix(word, "-") {
			return true
		}
	}
	return false
}

// loadConfig resolves the configuration layers for this run: the system and
// user defaults, or --config replacing both (FR-054).
func loadConfig(opts *options) (config.Result, error) {
	return config.LoadSelected(opts.configPath)
}

// splitArgv takes the command out of argv[0] for the xbp-* link names.
func splitArgv(argv []string) (cmd string, args []string) {
	if len(argv) == 0 {
		return "", nil
	}
	if fromName, ok := commandFromArgv(argv[0]); ok {
		return fromName, argv[1:]
	}
	return "", argv[1:]
}

// wantColor decides whether the summary carries ANSI colour, per FR-057: an
// explicit --color wins over [Color] mode, and Auto means colour only on a
// terminal with a non-empty NO_COLOR unset.
func wantColor(mode config.ColorMode, opts *options) bool {
	value := mode
	if opts.noColor {
		value = config.ColorNever
	} else if opts.explicit["color"] {
		value = colorModeFromFlag(opts.color)
	}
	switch value {
	case config.ColorAlways:
		return true
	case config.ColorNever:
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminalFn(os.Stdout)
}

// colorModeFromFlag maps the lowercase command-line spelling to the
// PascalCase [Color] mode enum.
func colorModeFromFlag(value string) config.ColorMode {
	switch value {
	case "always":
		return config.ColorAlways
	case "never":
		return config.ColorNever
	}
	return config.ColorAuto
}

func (st *state) runCompletion(opts *options) int {
	if len(opts.args) == 0 {
		fmt.Fprintln(st.stderr, "usage: xbpretty completion bash|zsh|fish")
		return exitUsage
	}
	script, err := completion.Script(opts.args[0])
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitUsage
	}
	fmt.Fprint(st.stdout, script)
	return exitOK
}

func (st *state) runLink(opts *options) int {
	dir := ""
	if len(opts.args) > 0 {
		dir = opts.args[0]
	}
	target, err := os.Executable()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: cannot locate this binary: %v\n", err)
		return exitRuntime
	}
	if dir == "" {
		dir = filepath.Dir(target)
	}
	status := exitOK
	for _, name := range spec.LinkNames() {
		link := filepath.Join(dir, name)
		info, lerr := os.Lstat(link)
		if lerr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				existing, _ := os.Readlink(link)
				if sameSymlinkTarget(dir, existing, target) {
					continue
				}
				fmt.Fprintf(st.stderr, "xbpretty: %s already exists and points to %s, not overwriting\n", link, existing)
			} else {
				fmt.Fprintf(st.stderr, "xbpretty: %s already exists and points to %s, not overwriting\n", link, describeExisting(info))
			}
			status = exitRuntime
			continue
		}
		if !os.IsNotExist(lerr) {
			fmt.Fprintf(st.stderr, "xbpretty: cannot create %s: %v\n", link, lerr)
			status = exitRuntime
			continue
		}
		if err := os.Symlink(target, link); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: cannot create %s: %v\n", link, err)
			status = exitRuntime
		}
	}
	return status
}

// sameSymlinkTarget reports whether an existing link already names this
// binary, whether it was written absolute or relative to DIR.
func sameSymlinkTarget(dir, existing, target string) bool {
	if existing == target {
		return true
	}
	resolved := existing
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(dir, resolved)
	}
	got, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return false
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false
	}
	return got == want
}

func describeExisting(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "a directory"
	case info.Mode().IsRegular():
		return "a regular file"
	default:
		return "a non-symlink"
	}
}

// printVersion prints xbpretty's version and the detected xbps version.
func printVersion(out, errOut io.Writer) {
	fmt.Fprintf(out, "xbpretty %s\n", Version)
	path, err := detectXBPS()
	if err != nil {
		fmt.Fprintf(errOut, "xbpretty: xbps version unknown: %v\n", err)
		return
	}
	fmt.Fprintf(out, "%s\n", strings.TrimSpace(path))
}

func detectXBPS() (string, error) {
	r := &runner{out: io.Discard}
	res, err := r.run("xbps-query", "-p", "pkgver", "xbps")
	if err != nil {
		return "", err
	}
	if res.exitCode != 0 {
		return "", fmt.Errorf("xbps-query exited %d: %s", res.exitCode, strings.TrimSpace(res.stderr))
	}
	return strings.TrimSpace(res.stdout), nil
}

func usage(w io.Writer) {
	fmt.Fprint(w, spec.Usage())
}

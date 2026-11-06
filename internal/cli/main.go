package cli

import (
	"bufio"
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
	exitInterrupted = 130
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

	stdout       io.Writer
	stderr       io.Writer
	stdin        io.Reader
	parser       *StreamParser
	live         bool
	liveProgress *liveProgress

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
	color       bool // stdout
	colorErr    bool // stderr diagnostics
	autoUpdate  bool
	showPkgs    bool
	log         bool
	progressBar bool
	unit        summary.SizeUnit
	theme       summary.Theme
	mode        config.UIMode
	assumeYes   bool
	autoRemove  bool
	simple      bool
	historyDir  string
	// downloadSink receives FR-041's event stream. It is created lazily by the
	// predownload pass so a run that never downloads allocates nothing.
	downloadSink downloadSink

	// promptScanner is the one reader over stdin. A transaction can ask twice
	// (primary then implicit orphan), so a single scanner keeps the answers
	// the first read may have buffered.
	promptScanner *bufio.Scanner

	// commandLine is argv without the program name, as typed, for FR-030's
	// history `command` field. pendingHistory carries one real transaction's
	// recording context between the prompt and a zero exit.
	commandLine    []string
	pendingHistory *historyPending
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
	// BF-07: stderr is the diagnostic stream, so every write error stops the
	// display and none reaches the child as SIGPIPE or EIO (FR-074). Every
	// writer in the run shares this one wrapper. asFile unwraps it wherever a
	// terminal check needs the *os.File.
	errW := newLossyWriter(err)

	knownCmd, args := splitArgv(argv)

	opts, parseErr := parseArgs(args, knownCmd)
	if parseErr != nil {
		printUsageError(errW, parseErr, args)
		return exitUsage
	}
	// FR-019: an N/A flag is accepted by the parser and refused before config
	// resolution, any child, and any summary.
	if len(opts.refusals) > 0 {
		for _, tail := range opts.refusals {
			fmt.Fprintf(errW, "xbpretty: %s\n", tail)
		}
		return exitUsage
	}
	st := &state{stdout: newResilientWriter(out), stderr: errW, stdin: in}
	// FR-057: before config resolution a usage error still has the default
	// palette and any explicit colour flag, so its prefix is themed too.
	defaults := config.Defaults()
	st.theme = summaryTheme(defaults.Color.Theme)
	st.color = wantColor(defaults.Color.Mode, opts, isTTY(out))
	st.colorErr = wantColor(defaults.Color.Mode, opts, isTTY(errW))
	// FR-030: the entry's command is the invocation without the program name.
	// A link invocation (xbp-install ...) prepends its canonical verb.
	if knownCmd != "" {
		st.commandLine = append([]string{knownCmd}, args...)
	} else {
		st.commandLine = append([]string{}, args...)
	}

	// moo is hidden and never touches config or xbps.
	if opts.command == "moo" {
		return runMoo(out, opts)
	}
	if opts.help {
		usage(out)
		return exitOK
	}
	if opts.version {
		printVersion(out, errW)
		return exitOK
	}
	if opts.command == "" {
		usage(out)
		if len(opts.passthru) > 0 || len(opts.args) > 0 {
			fmt.Fprintln(errW, "xbpretty: no command given; name the command before the arguments (for example 'xbpretty install -- <xbps args>')")
		} else {
			fmt.Fprintln(errW, "xbpretty: no command given; run 'xbpretty --help' for usage")
		}
		return exitUsage
	}

	// Positional validation runs before config, so completion and link still
	// bypass config while their arity is checked.
	if code, stop := validateInvocation(opts, st); stop {
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
		st.errorf("%v\n", loadErr)
		return exitUsage
	}
	cfg := res.Config
	resolved := resolveSettings(cfg, opts)
	reportConfig(errW, opts, res, resolved)

	st.opts = opts
	st.mode = resolved.mode
	st.assumeYes = resolved.assumeYes
	st.autoRemove = resolved.autoRemove
	st.simple = resolved.simple
	st.historyDir = resolved.historyDir
	st.autoUpdate = resolved.sync
	st.showPkgs = resolved.showPkgs
	st.log = resolved.log
	st.color = wantColor(resolved.color, opts, isTTY(out))
	st.colorErr = wantColor(resolved.color, opts, isTTY(errW))
	st.progressBar = resolved.mode != config.ModePlain
	st.unit = summary.SizeUnit(resolved.unit)
	st.theme = summaryTheme(cfg.Color.Theme)
	st.r = &runner{
		cfg:     cfg,
		out:     errW,
		verbose: resolved.verbose,
		stdout:  out,
		stderr:  errW,
		stdin:   in,
	}

	if opts.command == "log" {
		return st.runLog(opts)
	}
	return st.dispatch(opts)
}

// printUsageError prints a parse error and the optional FR-059 hint. No
// options exist when parsing fails, so the colour flag is read back from argv
// and the default palette themes the prefix like every other diagnostic.
func printUsageError(err io.Writer, parseErr error, args []string) {
	mode, explicit, noColor := usageColor(args)
	color := wantColor(mode, &options{
		color:    colorSpelling(mode),
		noColor:  noColor,
		explicit: map[string]bool{"color": explicit},
	}, isTTY(err))
	prefix := paintRole(summaryTheme(config.Defaults().Color.Theme), color, roleError, "xbpretty: ")
	fmt.Fprint(err, prefix)
	fmt.Fprintf(err, "%v\n", parseErr)
	if ue, ok := parseErr.(*usageError); ok && ue.hint != "" {
		fmt.Fprint(err, prefix)
		fmt.Fprintf(err, "%s\n", ue.hint)
	}
	usage(err)
}

// usageColor scans argv for the colour flags a failed parse could not apply.
func usageColor(args []string) (config.ColorMode, bool, bool) {
	mode := config.ColorAuto
	explicit := false
	noColor := false
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--no-color":
			noColor = true
		case strings.HasPrefix(arg, "--color="):
			mode = colorModeFromFlag(strings.TrimPrefix(arg, "--color="))
			explicit = true
		case arg == "--color" && i+1 < len(args):
			mode = colorModeFromFlag(args[i+1])
			explicit = true
			i++
		}
	}
	if noColor {
		mode = config.ColorNever
	}
	return mode, explicit, noColor
}

// colorSpelling maps the resolved enum back to the lowercase flag value.
func colorSpelling(mode config.ColorMode) string {
	switch mode {
	case config.ColorAlways:
		return "always"
	case config.ColorNever:
		return "never"
	}
	return "auto"
}

// dispatch routes a parsed command to its implementation.
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
	case "policy":
		return st.runPolicy(opts)
	case "fetch":
		return st.runFetch(opts)
	case "download":
		return st.runDownload(opts)
	case "clean":
		return st.runClean(opts)
	case "history":
		if opts.subcommand == "clear" {
			return st.runHistoryClear(opts)
		}
		return st.runHistory(opts)
	case "undo", "redo":
		// FR-033..FR-035: replays derive pinned before/after targets, preflight
		// every archive, and route their repository-served restores through
		// the same predownload path as a real transaction.
		return st.runReplay(opts.command, opts)
	}
	st.errorf("unknown command %q\n", opts.command)
	return exitUsage
}

// validateInvocation applies FR-021's positional rules before config is read.
func validateInvocation(opts *options, st *state) (int, bool) {
	args := opts.args
	switch opts.command {
	case "install", "remove", "purge", "download":
		if !hasPackageWord(opts) {
			st.errorf("%s needs at least one package\n", opts.command)
			return exitRuntime, true
		}
	case "show":
		if len(args) == 0 {
			st.errorf("%s needs at least one package\n", opts.command)
			return exitRuntime, true
		}
	case "search":
		if len(args) == 0 {
			st.errorf("%s\n", "search needs at least one pattern")
			return exitRuntime, true
		}
	case "fetch":
		if len(args) > 0 || opts.dashDash {
			// FR-042: no positional arguments and no -- passthrough.
			st.errorf("%s\n", "fetch takes no arguments and no -- passthrough")
			return exitUsage, true
		}
	case "update":
		// update has no transaction to pass `--` arguments to: it is a sync
		// plus one count dry run. Reject the passthrough rather than silently
		// dropping it (FR-012, FR-021).
		if opts.dashDash {
			st.errorf("%s\n", "update takes no arguments and no -- passthrough")
			return exitUsage, true
		}
		if len(args) > 0 {
			st.errorf("%s takes no package arguments\n", opts.command)
			return exitUsage, true
		}
	case "upgrade", "full-upgrade", "safe-upgrade", "autoremove", "autopurge", "clean":
		if len(args) > 0 {
			st.errorf("%s takes no package arguments\n", opts.command)
			return exitUsage, true
		}
	case "completion":
		if len(args) == 0 || len(args) > 1 {
			fmt.Fprintln(st.stderr, "usage: xbpretty completion bash|zsh|fish")
			return exitUsage, true
		}
	case "link":
		if len(args) > 1 {
			st.errorf("%s\n", "link takes at most one directory")
			return exitUsage, true
		}
	case "history":
		if opts.clearAll && opts.subcommand != "clear" {
			st.errorf("%s\n", "--all is only for history clear")
			return exitUsage, true
		}
		if opts.subcommand == "" || opts.subcommand == "info" || opts.subcommand == "show" {
			if len(args) > 1 {
				st.errorf("%s\n", "history takes no package arguments")
				return exitUsage, true
			}
		}
		if (opts.subcommand == "info" || opts.subcommand == "show") && len(args) == 0 {
			st.errorf("%s\n", "history info needs an entry id or 'last'")
			return exitUsage, true
		}
		if opts.subcommand == "clear" {
			if len(args) > 1 {
				st.errorf("%s\n", "history takes no package arguments")
				return exitUsage, true
			}
			if opts.clearAll && len(args) > 0 {
				st.errorf("%s\n", "history clear takes no package arguments")
				return exitUsage, true
			}
			if !opts.clearAll && len(args) == 0 {
				st.errorf("%s\n", "history clear needs an entry id, 'last' or --all")
				return exitUsage, true
			}
		}
	case "undo", "redo":
		if opts.dashDash {
			// Replays build their own pinned argv; a passthrough has no place
			// in it (FR-033, FR-035).
			st.errorf("%s takes no arguments and no -- passthrough\n", opts.command)
			return exitUsage, true
		}
		if len(args) > 1 {
			st.errorf("%s takes at most one entry id or 'last'\n", opts.command)
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

// wantColor decides whether one stream carries ANSI colour, per FR-057: an
// explicit --color wins over [Color] mode, and Auto means colour only when that
// stream is a terminal and NO_COLOR is unset/empty. It is called once per
// stream, so a TTY stdout and a piped stderr resolve independently.
func wantColor(mode config.ColorMode, opts *options, streamTTY bool) bool {
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
	return streamTTY
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
		st.errorf("%v\n", err)
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
		st.errorf("cannot locate this binary: %v\n", err)
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
				st.errorf("%s already exists and points to %s, not overwriting\n", link, existing)
			} else {
				st.errorf("%s already exists and points to %s, not overwriting\n", link, describeExisting(info))
			}
			status = exitRuntime
			continue
		}
		if !os.IsNotExist(lerr) {
			st.errorf("cannot create %s: %v\n", link, lerr)
			status = exitRuntime
			continue
		}
		if err := os.Symlink(target, link); err != nil {
			st.errorf("cannot create %s: %v\n", link, err)
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

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/soubarnak/xbpretty/internal/completion"
	"github.com/soubarnak/xbpretty/internal/config"
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
	quiet      bool
}

// state is everything one invocation of xbpretty needs.
type state struct {
	opts   *options
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
	r      *runner
	color  bool
	sync   bool
	log    bool
}

// Main runs xbpretty with the process arguments and returns its exit code.
func Main(argv []string) int {
	cmd, args := splitArgv(argv)

	opts, err := parseArgs(args, cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: %v\n", err)
		usage(os.Stderr)
		return exitUsage
	}
	if opts.help {
		usage(os.Stdout)
		return exitOK
	}
	if opts.version {
		printVersion(os.Stdout, os.Stderr)
		return exitOK
	}

	st := &state{
		stdout: os.Stdout,
		stderr: os.Stderr,
		stdin:  stdinFn(),
	}
	if opts.completion || cmd == "completion" {
		return st.runCompletion(opts)
	}
	if cmd == "link" || opts.command == "link" {
		return st.runLink(opts)
	}

	if opts.explicit["limit"] && opts.command != "log" {
		fmt.Fprintln(os.Stderr, "xbpretty: --limit only applies to the log command")
		usage(os.Stderr)
		return exitUsage
	}
	if opts.command == "log" && cmd == "" {
		return st.runLog(opts)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: %v\n", err)
		return exitUsage
	}
	verbose := cfg.Verbose
	if opts.explicit["verbose"] {
		verbose = opts.verbose
	}
	st.opts = opts
	st.sync = cfg.Sync
	if opts.explicit["sync"] {
		st.sync = !opts.noSync
	}
	st.log = cfg.Log
	if opts.explicit["log"] {
		st.log = !opts.noLog
	}
	st.color = wantColor(cfg.Color, opts)
	st.r = &runner{cfg: cfg, out: os.Stderr, verbose: verbose}

	if cmd == "" {
		cmd = opts.command
	}
	if cmd == "" {
		usage(os.Stderr)
		return exitUsage
	}
	if err := cfg.Resolve(); err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	return st.runTransaction(cmd, opts)
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

// wantColor decides whether the summary carries ANSI colour, per FR-020.
func wantColor(configValue string, opts *options) bool {
	// FR-020: an explicit --color wins over the config key, and `auto` means
	// what it says: colour only on a terminal with NO_COLOR unset.
	value := configValue
	if opts.explicit["color"] {
		value = opts.color
	}
	switch value {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminalFn(os.Stdout)
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
	names := []string{"xbp", "xbp-install", "xbp-remove", "xbp-upgrade", "xbp-autoremove", "xbp-update"}
	status := exitOK
	for _, name := range names {
		link := filepath.Join(dir, name)
		if existing, err := os.Readlink(link); err == nil {
			if existing == target {
				continue
			}
			fmt.Fprintf(st.stderr, "xbpretty: %s already exists and points to %s, not overwriting\n", link, existing)
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
	fmt.Fprint(w, usageText)
}

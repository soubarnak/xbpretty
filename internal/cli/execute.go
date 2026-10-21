package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/hints"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// runTransaction drives one command from the plan through the prompt to the
// real execution, and returns xbpretty's exit code.
func (st *state) runTransaction(cmd string, opts *options) int {
	install := st.binaryFor(cmd)

	// FR-006: update is index sync only, streamed, with no summary and no
	// prompt.
	if cmd == "update" {
		args := []string{}
		if st.sync && !opts.noSync {
			args = append(args, "-S")
		}
		args = append(args, opts.args...)
		args = append(args, opts.passthru...)
		if euidFn() != 0 {
			fmt.Fprintln(st.stderr, "xbpretty: update needs root; rerun with doas or sudo")
			return exitRuntime
		}
		return st.execute(cmd, install, args)
	}

	// Everything else previews first.
	wantSync := false
	rows, code := st.plan(cmd, opts, &wantSync)
	if code >= 0 {
		return code
	}
	planSynced := wantSync

	if len(rows) == 0 {
		// FR-015: nothing to do is not an error.
		fmt.Fprintln(st.stdout, "Nothing to do.")
		return exitNothingToDo
	}

	sum := st.planToSummary(rows)
	isPlain := !st.color || !isTTY(st.stdout)
	if err := (summary.Renderer{
		Color:       st.color,
		Plain:       isPlain,
		BoxStyle:    st.boxStyle,
		ProgressBar: st.progressBar,
	}).Render(st.stdout, sum); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}

	if opts.dryRun {
		// FR-005 / AC-005: the plan is the whole run.
		return exitOK
	}

	// FR-010: never block on a pipe.
	if !isTTY(st.stdin) && !(opts.yes || st.r.cfg.AssumeYes) {
		fmt.Fprintln(st.stderr, "xbpretty: stdin is not a terminal, so xbpretty will not prompt; pass -y to proceed")
		return exitRuntime
	}
	// FR-016: only root may execute.
	if euidFn() != 0 {
		st.privHint(cmd, opts)
		return exitRuntime
	}
	if !(opts.yes || st.r.cfg.AssumeYes) {
		if !st.prompt() {
			// FR-009 / AC-003: declining is not an error.
			return exitOK
		}
	}

	args := st.realCommand(cmd, opts, planSynced)
	return st.execute(cmd, install, args, rows...)
}

// execute streams one xbps command live (FR-014) and handles its outcome.
func (st *state) execute(cmd, bin string, args []string, rows ...xbps.Row) int {
	code, err := st.r.stream(bin, args...)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	stderr := st.r.lastStderr
	if code == 0 {
		// FR-013: every real execution that exits 0 is logged, and `update`
		// is one of them even though it produces no rows of its own.
		if st.log {
			st.writeLog(cmd, args, rows)
		}
		if hint := rebootHint(rows); hint != "" {
			fmt.Fprintf(st.stderr, "hint: %s\n", hint)
		}
		return exitOK
	}

	// FR-017: the raw xbps output stays, then one matching hint if any. Only
	// stderr is kept from a real run: stdout goes straight to the terminal so
	// xbps still sees one. A failure that lives on stdout alone is only caught
	// when the dry run that came first already printed it (ADR-0006).
	if h := hints.Lookup(code, hints.Output{Stderr: stderr}); h != nil {
		fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
	}
	if len(rows) > 0 && rows[0].Action == "remove" {
		st.revdepsHint(optsArgs(rows))
	}
	return code
}

// optsArgs returns the package names targeted by a remove plan.
func optsArgs(rows []xbps.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

// revdepsHint prints the FR-018 follow-up after a failed remove: the
// installed packages that depend on the target, not the repository-wide set
// `-R` would name.
func (st *state) revdepsHint(packages []string) {
	for _, pkg := range packages {
		res, err := st.r.run(st.r.cfg.XBPSQuery, "-X", pkg)
		if err != nil || res.exitCode != 0 {
			continue
		}
		dependents, err := hints.ParseRevdeps(strings.NewReader(res.stdout))
		if err != nil || len(dependents) == 0 {
			continue
		}
		if text := hints.ForceRemoveHint(pkg, dependents); text != "" {
			fmt.Fprintf(st.stderr, "hint: %s\n", text)
		}
	}
}

// writeLog appends one line for FR-013. A failure here is a warning, never
// an error. The line names the xbpretty command, not xbps's own argv, so the
// log reads back as the user ran it.
func (st *state) writeLog(cmd string, args []string, rows []xbps.Row) {
	path, err := config.TransactionLog()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: transaction log unavailable: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: transaction log unavailable: %v\n", err)
		return
	}
	defer f.Close()

	// AC-012: under sudo or doas the log belongs to the invoking user, whose
	// state home it sits in, not to root. A failed chown is a warning, like
	// every other log failure.
	if uid, gid, ok := config.Invoker(); ok {
		for _, p := range []string{filepath.Dir(path), path} {
			if err := os.Chown(p, uid, gid); err != nil {
				fmt.Fprintf(st.stderr, "xbpretty: transaction log ownership not changed: %v\n", err)
			}
		}
	}

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		// A held package is in the plan but the transaction leaves it alone.
		if row.Action == xbps.ActionHold {
			continue
		}
		names = append(names, row.Pkgver())
	}
	// The package list is the last field, and `update` has none: no
	// trailing space for a line that names only the command.
	fields := []string{config.FormatTimestamp(time.Now()), cmd}
	if len(names) > 0 {
		fields = append(fields, strings.Join(names, " "))
	}
	if _, err := f.WriteString(strings.Join(fields, " ") + "\n"); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: transaction log unavailable: %v\n", err)
	}
}

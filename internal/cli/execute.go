package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/hints"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// execute streams one xbps command live (FR-014) and handles its outcome.
func (st *state) execute(cmd, bin string, args []string, rows ...xbps.Row) int {
	var totalDl int64
	for _, row := range rows {
		if row.DownloadSize > 0 {
			totalDl += int64(row.DownloadSize)
		}
	}
	if st.r.parser == nil {
		st.r.parser = NewStreamParser(len(rows), totalDl)
	}
	// FR-041: predownload completions seed the child's parser so xbps's own
	// fallback transfers continue the same counters.
	if st.downloadSink != nil {
		st.downloadSink.Seed(st.r.parser)
	}

	isInteractive := isTTY(st.stdout) && isTTY(st.stderr)
	useWidget := st.progressBar && !st.r.verbose && isInteractive
	var widget *ProgressWidget
	if useWidget {
		termWidth := terminalWidth(st.stderr)
		widget = NewProgressWidget(st.stderr, st.boxStyle, st.color, termWidth, len(rows), totalDl)
		widget.AttachToParser(st.r.parser)
		widget.Start()
		st.r.widget = widget
	}

	code, err := st.r.stream(bin, args...)
	if widget != nil {
		widget.Finish(code, st.r.lastStderr, rows, cmd)
		st.r.widget = nil
	}
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

	// On failure:
	// If widget was active, raw output was suppressed during run; print it now (FR-017).
	if widget != nil {
		if st.r.lastStdout != "" {
			fmt.Fprint(st.stdout, st.r.lastStdout)
		}
		if stderr != "" {
			fmt.Fprint(st.stderr, stderr)
		}
	}
	// FR-017: the raw xbps output stays, then one matching hint if any.
	if h := hints.Lookup(code, hints.Output{Stdout: st.r.lastStdout, Stderr: stderr}); h != nil {
		fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
	}
	isRemovePlan := len(rows) > 0 && rows[0].Action == "remove"
	if isRemovePlan {
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
		res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-X", pkg)
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

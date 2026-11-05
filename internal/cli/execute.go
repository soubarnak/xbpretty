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

// liveMode applies FR-064/FR-065's suppression matrix. The widget needs both
// streams on a terminal; the plain line needs only stderr. Plain mode, -v,
// TERM=dumb and a redirected stderr all turn live output off entirely.
func (st *state) liveMode() liveMode {
	if !st.progressBar || st.r == nil || st.r.verbose {
		return liveOff
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return liveOff
	}
	if !isTTY(st.stderr) {
		return liveOff
	}
	if !isTTY(st.stdout) {
		return livePlainMode
	}
	return liveWidgetMode
}

// execute streams one xbps command live (FR-014) and handles its outcome. It
// applies no post-success side effects: the driver's commit step
// (executePhases/completeTransaction) runs them once the whole command, which
// may be several phases, has succeeded.
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

	// FR-064: the widget or FR-065's plain line draws from the same telemetry
	// the parser already reads. No live view means the child output is
	// forwarded verbatim instead.
	var live *liveProgress
	if mode := st.liveMode(); mode != liveOff {
		model := newProgressModel(cmd, rows, nil)
		live = newLiveProgress(st.stderr, st.color, terminalWidth(st.stderr), mode, model, st.theme)
		st.r.live = true
		st.r.liveProgress = live
		defer live.Finish()
		parser := st.r.parser
		parser.OnDownloadTick = func(tick DownloadTick) {
			live.Apply(false, func(m *progressModel) { m.downloadTick(tick) })
		}
		parser.OnDownloadComplete = func(done DownloadComplete) {
			// A completion is also a cache hit and forces a redraw.
			live.Apply(true, func(m *progressModel) { m.downloadDone(done) })
		}
		parser.OnPhaseChange = func(phase string) {
			live.Apply(true, func(m *progressModel) { m.phase(phase) })
		}
		parser.OnPackageEvent = func(event PackageEvent) {
			live.Apply(true, func(m *progressModel) { m.packageEvent(event) })
		}
		live.Request(true)
	}

	code, err := st.r.stream(bin, args...)
	if live != nil {
		live.Finish()
		st.r.live = false
		st.r.liveProgress = nil
	}
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	stderr := st.r.lastStderr
	if code == 0 {
		return exitOK
	}

	// On failure the live surface clears, then the captured child output is
	// printed on stderr before the one matching hint (FR-064, FR-066).
	if live != nil {
		if st.r.lastStdout != "" {
			fmt.Fprint(st.stderr, st.r.lastStdout)
		}
		if st.r.lastStderr != "" {
			fmt.Fprint(st.stderr, st.r.lastStderr)
		}
	}
	if h := hints.Lookup(code, hints.Output{Stdout: st.r.lastStdout, Stderr: stderr}); h != nil {
		fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
	}
	isRemovePlan := len(rows) > 0 && rows[0].Action == "remove"
	if isRemovePlan {
		st.revdepsHint(optsArgs(rows))
	}
	return code
}

// xbpsPhase is one child invocation of a command. A replay has two phases
// (restore, then remove); every other command has one.
type xbpsPhase struct {
	bin  string
	args []string
	rows []xbps.Row
}

// executePhases runs a command's xbps work in order and applies the
// post-success side effects exactly once, after every phase exits 0. A replay
// must never publish a history entry between its restore and removal phases
// (FR-031, FR-033).
func (st *state) executePhases(cmd string, logArgs []string, allRows []xbps.Row, phases ...xbpsPhase) int {
	for _, phase := range phases {
		if code := st.execute(cmd, phase.bin, phase.args, phase.rows...); code != 0 {
			return code
		}
	}
	st.completeTransaction(cmd, logArgs, allRows)
	return exitOK
}

// completeTransaction is the one post-success step: history first, then the
// line log, then the reboot hint (FR-031). It runs only when the whole
// command succeeded.
func (st *state) completeTransaction(cmd string, logArgs []string, rows []xbps.Row) {
	st.finishHistory()
	if st.log {
		st.writeLog(cmd, logArgs, rows)
	}
	if hint := rebootHint(rows); hint != "" {
		fmt.Fprintf(st.stderr, "hint: %s\n", hint)
	}
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

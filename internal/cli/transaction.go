package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/soubarnak/xbpretty/internal/hints"
	"github.com/soubarnak/xbpretty/internal/spec"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// binaryFor picks the xbps binary a command wraps (FR-002 .. FR-006).
func (st *state) binaryFor(cmd string) string {
	switch cmd {
	case "remove", "purge", "autoremove", "autopurge", "clean":
		return st.r.cfg.Xbps.RemovePath
	}
	return st.r.cfg.Xbps.InstallPath
}

// isUpgradeCommand reports whether cmd belongs to FR-011's upgrade family.
func isUpgradeCommand(cmd string) bool {
	switch cmd {
	case "upgrade", "full-upgrade", "safe-upgrade":
		return true
	}
	return false
}

// commandUsesSync reports whether a command may run the separate `xbps-install
// -S` step (FR-013). Every transaction verb can: the step is its own
// xbps-install call, never a flag on the transaction's own binary.
func commandUsesSync(cmd string) bool {
	return spec.IsTransaction(cmd)
}

// purgeRequested reports whether the command's transaction uses `-f` to delete
// modified files (FR-009). --purge is an accepted no-op on install/upgrade.
func purgeRequested(cmd string, opts *options) bool {
	if cmd == "purge" || cmd == "autopurge" {
		return true
	}
	if cmd == "remove" || cmd == "autoremove" {
		return opts.purge
	}
	return false
}

// actionFlags are the xbps flags that select the command's action, in the
// order §3.2's argv table writes them. The index refresh is never one of them:
// `-S` is a separate step, and the dry run's `-n` is prepended by the caller.
func actionFlags(cmd string, opts *options) []string {
	var args []string
	switch {
	case isUpgradeCommand(cmd):
		args = append(args, "-u")
	case cmd == "autoremove" || cmd == "autopurge":
		args = append(args, "-o")
	}
	if purgeRequested(cmd, opts) {
		args = append(args, "-f")
	}
	if cmd == "install" && opts.reinstall {
		args = append(args, "-f")
	}
	return args
}

// dryRunArgv builds the spec's dry-run argv: `-n`, then action flags, then the
// package arguments and the `--` passthrough. `-S` is never in it.
func dryRunArgv(cmd string, opts *options, args []string) []string {
	argv := []string{"-n"}
	argv = append(argv, actionFlags(cmd, opts)...)
	argv = append(argv, args...)
	argv = append(argv, opts.passthru...)
	return argv
}

// realArgv builds the spec's real argv: `-y`, then action flags, then the
// package arguments and the `--` passthrough. `-S` is never in it.
func realArgv(cmd string, opts *options, args []string) []string {
	argv := []string{"-y"}
	argv = append(argv, actionFlags(cmd, opts)...)
	argv = append(argv, args...)
	argv = append(argv, opts.passthru...)
	return argv
}

// wantsSync decides the separate index step (FR-013): the upgrade family
// syncs by default under [Nala] auto_update, the other commands only when
// --update asks; --no-update/--no-sync always suppress it.
func (st *state) wantsSync(cmd string, opts *options) bool {
	if !commandUsesSync(cmd) {
		return false
	}
	if opts.noUpdate {
		return false
	}
	if opts.update {
		return true
	}
	return isUpgradeCommand(cmd) && st.autoUpdate
}

// syncStep runs `xbps-install -S` as its own step, with its output on stderr
// and never inside the transaction argv. #63 owns the sync internals
// (repository configuration and mirror behaviour); this is the seam it
// replaces. A non-zero code aborts the run with
// xbps's own code and a hint; xbps exits 0 when every fetch fails, so an exit
// of 0 continues to the dry run (FR-013). It returns -1 when the policy allows
// the caller to continue.
func (st *state) syncStep() int {
	code, output, err := st.r.streamStderr(st.r.cfg.Xbps.InstallPath, "-S")
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if code != 0 {
		if h := hints.Lookup(code, output); h != nil {
			fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
		}
		return code
	}
	return -1
}

// runTransaction drives one command from the plan through the prompt to the
// real execution, and returns xbpretty's exit code. FR-014 fixes the order:
// parse/N/A/conflict checks already ran, then config and binary resolution,
// the sync step, the dry run, the implicit-autoremove check, Nothing to do,
// the summary, the dry-run stop, the download-only stop, the prompt/root/
// non-TTY checks, the real transaction, and finally hints and logging.
func (st *state) runTransaction(cmd string, opts *options) int {
	if cmd == "update" {
		return st.runUpdate(opts)
	}

	// FR-013: the index step is separate and root-only. It uses
	// xbps-install even for the remove family, so that binary joins the
	// per-command existence check when the step is requested.
	syncWanted := st.wantsSync(cmd, opts)
	binaries := commandBinaries(cmd)
	if syncWanted && euidFn() == 0 {
		binaries = append(binaries, "install")
	}
	// FR-053: check only the binaries this command actually uses, at runtime.
	if err := st.r.cfg.ResolveFor(binaries...); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}

	// FR-028: --print-uris stops before the sync step, any summary, prompt or
	// download. It only needs the config and the plan, and never needs root.
	if opts.printURIs {
		return st.runPrintURIs(cmd, opts)
	}

	// The invocation-level guards below deliberately run before the index
	// step. A command that is already going to refuse (a missing --reinstall
	// target, or a remove whose every target is absent) must not refresh the
	// index first: the refusal would be obscured by network work and by a
	// stale-index warning that changes nothing. FR-014 puts the sync step
	// before the dry run, not before these checks, and the dry run still
	// starts only after the sync.

	// FR-007: --reinstall is guarded before the dry run.
	if cmd == "install" && opts.reinstall {
		if code := st.reinstallGuard(opts); code >= 0 {
			return code
		}
	}

	// FR-008: not-installed remove targets are reported and filtered before
	// the dry run. Purge and the orphan commands use the same binary but do
	// not filter their named targets.
	//
	// The all-filtered case keeps FR-008's early Nothing to do. and does not
	// check orphans: the command has no target rather than an empty plan, and
	// FR-008 places its Nothing to do. before the dry run (and so before
	// FR-016's implicit-autoremove check).
	args := opts.args
	if cmd == "remove" {
		var code int
		args, code = st.filterRemoveTargets(args)
		if code >= 0 {
			return code
		}
	}

	// FR-013: a non-root sync request prints the stale-index warning and
	// continues against the cached index.
	if syncWanted {
		if euidFn() != 0 {
			fmt.Fprintln(st.stderr, "warning: not running as root, so the repository index was not refreshed; it may be stale")
		} else if code := st.syncStep(); code >= 0 {
			return code
		}
	}

	rows, code := st.planTransaction(cmd, opts, args)
	if code >= 0 {
		return code
	}

	if len(rows) == 0 {
		// FR-014/FR-016: the orphan check still runs before Nothing to do,
		// and its plan drives the normal summary, prompt and execution path.
		orphans, orphanCode := st.planImplicitAutoremove(cmd, opts)
		if len(orphans) > 0 {
			return st.runOrphanOnly(cmd, opts, orphans)
		}
		if orphanCode >= 0 {
			return orphanCode
		}
		// FR-017: nothing to do is not an error, and it prints only when
		// both the primary and the orphan plan are empty.
		fmt.Fprintln(st.stdout, "Nothing to do.")
		return exitNothingToDo
	}

	summaryCmd := summaryCommand(cmd, opts)
	// FR-067: Tui mode may replace this transaction's stdout with the S1
	// full-screen summary. A refusal or a signal ends the run here; an
	// unavailable screen leaves the normal text path untouched.
	confirmedByScreen := false
	if outcome, code := st.maybeRunSummaryScreen(opts, summaryCmd, rows); outcome != screenUnavailable {
		if outcome == screenExited {
			return code
		}
		confirmedByScreen = true
	}
	if !confirmedByScreen {
		if code := st.renderSummary(summaryCmd, rows); code >= 0 {
			return code
		}
	}

	// FR-017: a hold-only plan is read-only. Its summary prints, but there is
	// no prompt and no execution.
	if allHeld(rows) {
		return exitOK
	}

	if opts.dryRun {
		// FR-015: the plan is the whole run.
		return exitOK
	}

	// FR-015: -n takes the refusal path with no prompt, before any of the
	// checks a real transaction would need.
	if opts.assumeNo {
		return st.refuse()
	}
	autoAccept := st.assumeYes || confirmedByScreen
	// FR-073: the non-root check is unconditional and comes before the
	// non-TTY check, so a non-root run always gets the summary and the
	// elevation hint.
	if euidFn() != 0 {
		st.privHint(cmd, opts)
		return exitRuntime
	}
	if !autoAccept && !isTTY(st.stdin) {
		// FR-015: never block on a pipe.
		fmt.Fprintln(st.stderr, "xbpretty: stdin is not a terminal, so xbpretty will not prompt; pass -y to proceed")
		return exitRuntime
	}
	if !autoAccept {
		result := st.prompt()
		if !result.proceed {
			return result.code
		}
	}

	// FR-040: the download-only stop runs after its prompt/root/non-TTY
	// checks. #72 owns the predownload; the seam must never fall through to
	// a real transaction.
	if opts.downloadOnly {
		return st.downloadOnlyStop(cmd, opts, rows)
	}

	// FR-030/FR-031: the before snapshot is taken after the prompt and
	// before the real child. executePhases commits the entry once the whole
	// command (every phase for a replay) exited 0, before the line log, the
	// reboot hint and any implicit autoremove.
	st.beginHistory(cmd, opts, rows)
	code = st.executePhases(cmd, realArgv(cmd, opts, args), rows,
		xbpsPhase{bin: st.binaryFor(cmd), args: realArgv(cmd, opts, args), rows: rows})
	if code == 0 {
		// FR-016: a successful real transaction runs the implicit orphan
		// transaction. Its failure replaces the primary result.
		if orphanCode := st.runImplicitAutoremoveAfterSuccess(cmd, opts); orphanCode >= 0 {
			return orphanCode
		}
	}
	return code
}

// commandBinaries returns the configured program names a command uses. The
// command table owns the list.
func commandBinaries(cmd string) []string {
	c, ok := spec.CommandByName(cmd)
	if !ok {
		return nil
	}
	return append([]string(nil), c.Binaries...)
}

// renderSummary draws the read-only summary for a non-empty plan. The
// command, not the rows, supplies FR-062's operation label: an implicit
// orphan transaction must render as AutoRemove/AutoPurge even though it
// follows an install or purge.
func (st *state) renderSummary(cmd string, rows []xbps.Row) int {
	sum := st.planToSummary(cmd, rows)
	stdoutTTY := isTTY(st.stdout)
	// FR-062: the rule width is the stdout terminal width, else a positive
	// COLUMNS, else 80, and it is used even when piped. Table expansion is a
	// separate decision: full width on a TTY, natural widths when piped.
	width := 0
	if stdoutTTY {
		width = terminalWidth(st.stdout)
	}
	if width <= 0 {
		width = columnsFromEnv()
	}
	if width <= 0 {
		width = 80
	}
	if err := (summary.Renderer{
		Color:  st.color,
		Simple: st.simple,
		Width:  width,
		Piped:  !stdoutTTY,
		Unit:   st.unit,
		Theme:  st.theme,
	}).Render(st.stdout, sum); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	return -1
}

// columnsFromEnv reads COLUMNS, ignoring missing or malformed values.
func columnsFromEnv() int {
	n, err := strconv.Atoi(os.Getenv("COLUMNS"))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// allHeld reports whether every row of the plan is a hold row.
func allHeld(rows []xbps.Row) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if row.Action != xbps.ActionHold {
			return false
		}
	}
	return true
}

// planTransaction runs the dry run and returns the parsed rows, or a
// non-negative exit code when the run should stop.
func (st *state) planTransaction(cmd string, opts *options, args []string) ([]xbps.Row, int) {
	return st.planWith(st.binaryFor(cmd), dryRunArgv(cmd, opts, args)...)
}

// planWith runs one dry-run argv and parses its plan, forwarding xbps's
// stderr and any matching hint before the summary (§3.2.3, FR-073).
func (st *state) planWith(bin string, argv ...string) ([]xbps.Row, int) {
	res, err := st.r.run(bin, argv...)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return nil, exitRuntime
	}
	if res.exitCode != 0 {
		fmt.Fprint(st.stderr, res.stderr)
		fmt.Fprint(st.stdout, res.stdout)
		if h := hints.Lookup(res.exitCode, res.output()); h != nil {
			fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
		}
		return nil, res.exitCode
	}

	// §3.2.3: a successful plan still forwards xbps's stderr before the
	// summary, so an already-installed or stale-index message is never
	// silent. Hints print after the forwarded stderr.
	fmt.Fprint(st.stderr, res.stderr)
	if h := hints.Lookup(res.exitCode, res.output()); h != nil {
		fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
	}

	rows, err := xbps.ParsePlan(strings.NewReader(res.stdout))
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: cannot read the xbps plan: %v\n", err)
		return nil, exitRuntime
	}
	return rows, -1
}

// isInstalled reports whether a package name is in the local pkgdb. The
// lookup uses `xbps-query -p pkgver`, which reads the installed database
// only, so a repository package with the same name is not mistaken for one.
func (st *state) isInstalled(name string) bool {
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-p", "pkgver", name)
	if err != nil || res.exitCode != 0 {
		return false
	}
	return strings.TrimSpace(res.stdout) != ""
}

// reinstallGuard applies FR-007 before the dry run: every named package must
// be installed. It returns -1 when the run may continue.
func (st *state) reinstallGuard(opts *options) int {
	for _, name := range opts.args {
		if st.isInstalled(name) {
			continue
		}
		fmt.Fprintf(st.stderr, "xbpretty: %s is not installed, so it cannot be reinstalled\n", name)
		return exitRuntime
	}
	return -1
}

// filterRemoveTargets applies FR-008: report every named package that is not
// installed on stderr and drop it. When nothing remains, print Nothing to do.
// on stdout and stop with exit 0.
func (st *state) filterRemoveTargets(args []string) ([]string, int) {
	if len(args) == 0 {
		return args, -1
	}
	kept := make([]string, 0, len(args))
	for _, name := range args {
		if st.isInstalled(name) {
			kept = append(kept, name)
			continue
		}
		fmt.Fprintf(st.stderr, "xbpretty: %s is not installed\n", name)
	}
	if len(kept) == 0 {
		fmt.Fprintln(st.stdout, "Nothing to do.")
		return nil, exitNothingToDo
	}
	return kept, -1
}

// runUpdate implements FR-012: root, its own `-S` step, then `-n -u` only to
// count upgradable rows. No summary and no prompt; the count line (or one
// line per package) is the durable output.
func (st *state) runUpdate(opts *options) int {
	if err := st.r.cfg.ResolveFor(commandBinariesForUpdate(st.showPkgs)...); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if euidFn() != 0 {
		st.privHint("update", opts)
		return exitRuntime
	}
	// update never nests a sync: this is its one index step, whatever the
	// --update/--no-update flags say.
	if code := st.syncStep(); code >= 0 {
		return code
	}
	rows, code := st.planUpdate()
	if code >= 0 {
		return code
	}
	upgradable := make([]xbps.Row, 0, len(rows))
	for _, row := range rows {
		if row.Action != xbps.ActionHold {
			upgradable = append(upgradable, row)
		}
	}
	if len(upgradable) > 0 {
		if st.showPkgs {
			st.printUpgradable(upgradable)
		} else {
			fmt.Fprintf(st.stdout, "%d package(s) can be upgraded. Run 'xbp list --upgradable' to see them.\n", len(upgradable))
		}
	}
	if st.log {
		st.writeLog("update", nil, nil)
	}
	return exitOK
}

// planUpdate runs FR-012's count-only dry run. Its argv is exactly
// `xbps-install -n -u`: update takes no positionals, and validateInvocation
// has already rejected a `--` passthrough, so none can be appended.
func (st *state) planUpdate() ([]xbps.Row, int) {
	return st.planTransaction("upgrade", &options{}, nil)
}

// commandBinariesForUpdate keeps update's binary rule in the command table
// and adds the query binary only when update_show_packages needs it.
func commandBinariesForUpdate(showPackages bool) []string {
	c, ok := spec.CommandByName("update")
	if !ok {
		return nil
	}
	names := append([]string(nil), c.Binaries...)
	if showPackages {
		names = append(names, "query")
	}
	return names
}

// printUpgradable prints one `<name> <installed> -> <candidate>` line per
// package (FR-012 with update_show_packages = true).
func (st *state) printUpgradable(rows []xbps.Row) {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	old := st.lookupOld(names)
	for _, row := range rows {
		installed := "?"
		if o, ok := old[row.Name]; ok && o.Version != "" {
			installed = o.Version
		}
		fmt.Fprintf(st.stdout, "%s %s -> %s\n", row.Name, installed, row.Version)
	}
}

// downloadOnlyStop is the #72 seam. #72 owns the predownload and its prompt
// rules; until it lands the flag must never fall through to a real
// transaction.
func (st *state) downloadOnlyStop(cmd string, opts *options, rows []xbps.Row) int {
	// TODO(#72): predownload the plan's archives, then stop with no pkgdb
	// write, no log and no history entry.
	fmt.Fprintf(st.stderr, "xbpretty: --download-only for %s is not implemented in this build\n", cmd)
	return exitRuntime
}

// implicitAutoremoveEligible reports whether the implicit orphan transaction
// may run for cmd (FR-016). Only a successful real install, remove, purge or
// upgrade-family run applies, never replay, update, clean, download or fetch.
// [Nala] auto_remove must be on, and with -y/assume_yes the run is skipped
// unless --auto-remove was explicit.
func (st *state) implicitAutoremoveEligible(cmd string, opts *options) bool {
	switch cmd {
	case "install", "remove", "purge", "upgrade":
	default:
		return false
	}
	if !st.autoRemove {
		return false
	}
	if st.assumeYes && !opts.autoRemovePos {
		return false
	}
	return true
}

// summaryCommand is FR-062's operation label for the command that produced a
// plan. A remove-family command carrying -f renders as its purge form, so the
// label follows the argv rather than the command spelling alone.
func summaryCommand(cmd string, opts *options) string {
	if !purgeRequested(cmd, opts) {
		return cmd
	}
	switch cmd {
	case "remove":
		return "purge"
	case "autoremove":
		return "autopurge"
	}
	return cmd
}

// orphanActionFlags is the orphan transaction's action set: -o, plus -f when
// the primary command purged (FR-009, §3.2's argv table).
func orphanActionFlags(cmd string, opts *options) []string {
	flags := []string{"-o"}
	if purgeRequested(cmd, opts) {
		flags = append(flags, "-f")
	}
	return flags
}

// orphanDryArgv is `xbps-remove -n -o [-f]`.
func orphanDryArgv(cmd string, opts *options) []string {
	return append([]string{"-n"}, orphanActionFlags(cmd, opts)...)
}

// orphanRealArgv is `xbps-remove -y -o [-f]`.
func orphanRealArgv(cmd string, opts *options) []string {
	return append([]string{"-y"}, orphanActionFlags(cmd, opts)...)
}

// orphanCommand names the orphan transaction for its log and history entry:
// autopurge when it carries -f, autoremove otherwise.
func orphanCommand(cmd string, opts *options) string {
	if purgeRequested(cmd, opts) {
		return "autopurge"
	}
	return "autoremove"
}

// planImplicitAutoremove plans the second `xbps-remove -n -o [-f]`
// transaction when FR-016 applies. It returns the parsed rows and -1 to
// continue, or a non-negative exit code when the check itself failed.
func (st *state) planImplicitAutoremove(cmd string, opts *options) ([]xbps.Row, int) {
	if !st.implicitAutoremoveEligible(cmd, opts) {
		return nil, -1
	}
	if err := st.r.cfg.ResolveFor("remove"); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return nil, exitRuntime
	}
	return st.planWith(st.r.cfg.Xbps.RemovePath, orphanDryArgv(cmd, opts)...)
}

// runOrphanOnly runs the orphan transaction when the primary plan was empty:
// the orphan plan is the whole run, so its refusal is the normal exit-1
// refusal and Nothing to do was never printed.
func (st *state) runOrphanOnly(cmd string, opts *options, rows []xbps.Row) int {
	summaryCmd := orphanCommand(cmd, opts)
	confirmedByScreen := false
	if outcome, code := st.maybeRunSummaryScreen(opts, summaryCmd, rows); outcome != screenUnavailable {
		if outcome == screenExited {
			return code
		}
		confirmedByScreen = true
	}
	if !confirmedByScreen {
		if code := st.renderSummary(summaryCmd, rows); code >= 0 {
			return code
		}
	}
	if allHeld(rows) {
		return exitOK
	}
	if opts.dryRun {
		return exitOK
	}
	if opts.assumeNo {
		return st.refuse()
	}
	autoAccept := st.assumeYes || confirmedByScreen
	if euidFn() != 0 {
		st.privHint(cmd, opts)
		return exitRuntime
	}
	if !autoAccept && !isTTY(st.stdin) {
		fmt.Fprintln(st.stderr, "xbpretty: stdin is not a terminal, so xbpretty will not prompt; pass -y to proceed")
		return exitRuntime
	}
	if !autoAccept {
		result := st.prompt()
		if !result.proceed {
			return result.code
		}
	}
	if opts.downloadOnly {
		// FR-016: --download-only never runs the orphan transaction.
		return st.downloadOnlyStop(cmd, opts, rows)
	}
	return st.executeOrphan(cmd, opts, rows)
}

// runImplicitAutoremoveAfterSuccess is FR-016's second real transaction. It
// returns -1 when it did not run, so the caller keeps the primary result; a
// declined prompt also returns -1 (the primary result stands), while a failed
// orphan transaction returns its own code.
func (st *state) runImplicitAutoremoveAfterSuccess(cmd string, opts *options) int {
	rows, code := st.planImplicitAutoremove(cmd, opts)
	if code >= 0 {
		return code
	}
	if len(rows) == 0 {
		return -1
	}
	summaryCmd := orphanCommand(cmd, opts)
	confirmedByScreen := false
	if outcome, code := st.maybeRunSummaryScreen(opts, summaryCmd, rows); outcome != screenUnavailable {
		if outcome == screenExited {
			return code
		}
		confirmedByScreen = true
	}
	if !confirmedByScreen {
		if code := st.renderSummary(summaryCmd, rows); code >= 0 {
			return code
		}
	}
	// A hold-only orphan plan changes nothing: the summary is read-only.
	if allHeld(rows) {
		return -1
	}
	if !st.assumeYes && !confirmedByScreen {
		result := st.prompt()
		if !result.proceed {
			if result.invalid {
				// An invalid answer is FR-015's error, not a decline.
				return result.code
			}
			// Declining the orphan prompt leaves the primary result and
			// exits 0; the primary transaction already committed.
			return -1
		}
	}
	return st.executeOrphan(cmd, opts, rows)
}

// executeOrphan runs the confirmed orphan transaction. Its history entry is
// begun before the child starts with implicit true; executePhases publishes
// the pending entry from finishHistory once the orphan committed, before its
// line log and reboot hint.
func (st *state) executeOrphan(cmd string, opts *options, rows []xbps.Row) int {
	orphan := orphanCommand(cmd, opts)
	argv := orphanRealArgv(cmd, opts)
	st.beginImplicitAutoremoveHistory(orphan, opts, rows)
	return st.executePhases(orphan, argv, rows,
		xbpsPhase{bin: st.r.cfg.Xbps.RemovePath, args: argv, rows: rows})
}

// beginImplicitAutoremoveHistory begins FR-030's entry for the implicit
// orphan transaction. It is called BEFORE the child runs, so #69's
// finishHistory inside executePhases can publish it after a successful
// commit.
func (st *state) beginImplicitAutoremoveHistory(cmd string, opts *options, rows []xbps.Row) {
	st.beginHistoryEntry(cmd, opts, rows, st.commandLine,
		requestedTargets(cmd, opts), true, nil, "")
}

// lookupOld fills in the pre-upgrade version and installed size the summary
// needs (FR-011). Lookups are batched per package: xbps-query accepts one
// package per call, so they run through a bounded pool to stay inside
// NFR-002's overhead budget.
func (st *state) lookupOld(names []string) map[string]summary.Old {
	out := make(map[string]summary.Old, len(names))
	if len(names) == 0 {
		return out
	}
	type job struct{ name string }
	jobs := make(chan job)
	results := make(chan struct {
		name string
		old  summary.Old
	})
	workers := 8
	if len(names) < workers {
		workers = len(names)
	}
	for i := 0; i < workers; i++ {
		go func() {
			for j := range jobs {
				res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-p", "pkgver,installed_size", j.name)
				old := summary.Old{}
				if err == nil && res.exitCode == 0 {
					old = parseOld(res.stdout)
				}
				results <- struct {
					name string
					old  summary.Old
				}{j.name, old}
			}
		}()
	}
	go func() {
		for _, n := range names {
			jobs <- job{n}
		}
		close(jobs)
	}()
	for i := 0; i < len(names); i++ {
		r := <-results
		out[r.name] = r.old
	}
	return out
}

// parseOld reads what `xbps-query -p pkgver,installed_size <pkg>` prints: the
// package's pkgver, then its installed size in xbps's human units --
//
//	xbps-0.60.7_1
//	521KB
//
// (testdata/lookups). The version the summary shows is the bare one, so the
// name is split back off the pkgver (FR-011).
func parseOld(stdout string) summary.Old {
	old := summary.Old{}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 {
		return old
	}
	if _, version := xbps.SplitPkgver(strings.TrimSpace(lines[0])); version != "" {
		old.Version = version
	}
	if len(lines) > 1 {
		if n, err := parseSize(strings.TrimSpace(lines[1])); err == nil {
			old.InstalledSize, old.HasSize = n, true
		}
	}
	return old
}

// heldPackages lists the packages on hold (FR-011's Hold group).
func (st *state) heldPackages() map[string]bool {
	held := map[string]bool{}
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-H")
	if err != nil || res.exitCode != 0 {
		return held
	}
	for _, line := range strings.Split(res.stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// `xbps-query -H` prints one pkgver per held package, and a package
		// name carries hyphens of its own, so the version comes off the last
		// hyphen: base-files-0.146_1 is held as base-files, not base.
		name, version := xbps.SplitPkgver(line)
		if version == "" {
			continue
		}
		held[name] = true
	}
	return held
}

// parseSize reads the human form xbps-query prints, e.g. 32MB or 657KB.
func parseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	multiplier := uint64(1)
	switch {
	case strings.HasSuffix(s, "GiB"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GiB")
	case strings.HasSuffix(s, "GB"):
		multiplier = 1000 * 1000 * 1000
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MiB"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MiB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1000 * 1000
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KiB"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "KiB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1000
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	default:
		return 0, fmt.Errorf("unknown size unit in %q", s)
	}
	var n uint64
	var seenDot bool
	var frac float64
	var div float64 = 1
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			if seenDot {
				frac = frac*10 + float64(c-'0')
				div *= 10
			} else {
				n = n*10 + uint64(c-'0')
			}
		case c == '.' && !seenDot:
			seenDot = true
		default:
			return 0, fmt.Errorf("unknown size unit in %q", s)
		}
	}
	total := float64(n)*float64(multiplier) + (frac/div)*float64(multiplier)
	return uint64(total + 0.5), nil
}

// isTerminal reports whether f is attached to a terminal. A character-device
// check is not enough: /dev/null is one too. TCGETS succeeds only on a tty.
func isTerminal(f *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}

// promptResult is one answer to FR-015's question. proceed is false when the
// run stops; invalid separates a malformed answer (an error) from a refusal
// (a decision), which the implicit orphan prompt treats differently.
type promptResult struct {
	proceed bool
	code    int
	invalid bool
}

// prompt asks FR-015's question once. An invalid answer is never re-prompted.
func (st *state) prompt() promptResult {
	if st.promptScanner == nil {
		st.promptScanner = bufio.NewScanner(st.stdin)
	}
	scanner := st.promptScanner
	fmt.Fprint(st.stderr, "Do you want to continue? [Y/n] ")
	if !scanner.Scan() {
		return promptResult{code: st.refuse()}
	}
	answer := strings.TrimSpace(scanner.Text())
	switch strings.ToLower(answer) {
	case "", "y", "yes":
		return promptResult{proceed: true, code: exitOK}
	case "n", "no":
		return promptResult{code: st.refuse()}
	default:
		fmt.Fprintf(st.stderr, "xbpretty: '%s' is not a valid response\n", answer)
		return promptResult{code: exitRuntime, invalid: true}
	}
}

// refuse is FR-015's refusal path. A refusal is exit 1, never 0.
func (st *state) refuse() int {
	fmt.Fprintln(st.stderr, "xbpretty: user refused confirmation")
	return exitRuntime
}

// privHint prints the doas/sudo advice for a non-root real execution (FR-073).
func (st *state) privHint(cmd string, opts *options) {
	elevate := "sudo"
	if _, err := os.Stat("/usr/bin/doas"); err == nil {
		elevate = "doas"
	}
	line := "xbp " + cmd
	if len(opts.args) > 0 {
		line += " " + strings.Join(opts.args, " ")
	}
	fmt.Fprintf(st.stderr, "hint: rerun as root: %s %s\n", elevate, line)
}

// rebootHint reports packages whose update needs a reboot (FR-075).
func rebootHint(rows []xbps.Row) string {
	for _, row := range rows {
		if row.Action != "update" && row.Action != "install" {
			continue
		}
		name := row.Name
		if strings.HasPrefix(name, "linux") || name == "glibc" || strings.HasPrefix(name, "glibc-") {
			return "a kernel or glibc update was applied; a reboot is recommended"
		}
	}
	return ""
}

// planToSummary converts parsed rows into a summary, looking up the old
// versions and hold state the summary needs, and carrying the command that
// labels the Remove family section.
func (st *state) planToSummary(cmd string, rows []xbps.Row) *summary.Summary {
	held := st.heldPackages()
	srows := make([]summary.Row, 0, len(rows))
	var oldNames []string
	for _, row := range rows {
		srows = append(srows, summary.Row{
			Name: row.Name, Version: row.Version, Action: row.Action,
			Repo: row.Repo, InstalledSize: row.InstalledSize,
			DownloadSize: row.DownloadSize, HasInstalled: row.HasInstalled,
			HasDownload: row.HasDownload,
		})
		// The version diff applies to upgrades, reinstalls and held rows
		// whose installed version differs (FR-063).
		switch row.Action {
		case "update", "reinstall", "hold":
			oldNames = append(oldNames, row.Name)
		default:
			if held[row.Name] {
				oldNames = append(oldNames, row.Name)
			}
		}
	}
	old := st.lookupOld(oldNames)
	sum := summary.Build(srows, held, old)
	sum.Command = cmd
	return sum
}

// isTTY reports whether v is attached to a terminal. A resilient writer wraps
// the real stream, so it is unwrapped first.
func isTTY(v any) bool {
	f, ok := asFile(v)
	if !ok {
		return false
	}
	return isTerminalFn(f)
}

// asFile unwraps the writers xbpretty puts around a stream to the underlying
// *os.File, or reports that there is none.
func asFile(v any) (*os.File, bool) {
	switch w := v.(type) {
	case *os.File:
		return w, true
	case *resilientWriter:
		return asFile(w.w)
	}
	return nil, false
}

// relativeDir is the directory used when resolving a link target.
func relativeDir(path string) string {
	return filepath.Dir(path)
}

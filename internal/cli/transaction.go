package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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

	// FR-007: --reinstall is guarded before the dry run.
	if cmd == "install" && opts.reinstall {
		if code := st.reinstallGuard(opts); code >= 0 {
			return code
		}
	}

	// FR-008: not-installed remove targets are reported and filtered before
	// the dry run. Purge and the orphan commands use the same binary but do
	// not filter their named targets.
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
		// FR-014/FR-016: the implicit-autoremove check still runs before
		// Nothing to do. #64 owns that second transaction; the seam reports
		// that it has nothing to add for now.
		if extra, ok := st.implicitAutoremovePlan(cmd, opts); ok && len(extra) > 0 {
			rows = extra
		}
	}
	if len(rows) == 0 {
		// FR-017: nothing to do is not an error.
		fmt.Fprintln(st.stdout, "Nothing to do.")
		return exitNothingToDo
	}

	if code := st.renderSummary(rows); code >= 0 {
		return code
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
	autoAccept := st.assumeYes
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
		ok, refused := st.prompt()
		if !ok {
			return refused
		}
	}

	// FR-040: the download-only stop runs after its prompt/root/non-TTY
	// checks. #72 owns the predownload; the seam must never fall through to
	// a real transaction.
	if opts.downloadOnly {
		return st.downloadOnlyStop(cmd, opts, rows)
	}

	code = st.execute(cmd, st.binaryFor(cmd), realArgv(cmd, opts, args), rows...)
	if code == 0 {
		st.implicitAutoremoveAfterSuccess(cmd, opts, rows)
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

// renderSummary draws the read-only summary for a non-empty plan.
func (st *state) renderSummary(rows []xbps.Row) int {
	sum := st.planToSummary(rows)
	isPlain := !isTTY(st.stdout)
	termWidth := terminalWidth(st.stdout)
	if err := (summary.Renderer{
		Color:       st.color,
		Plain:       isPlain,
		Width:       termWidth,
		BoxStyle:    st.boxStyle,
		ProgressBar: st.progressBar,
		Unit:        st.unit,
	}).Render(st.stdout, sum); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	return -1
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
	bin := st.binaryFor(cmd)
	res, err := st.r.run(bin, dryRunArgv(cmd, opts, args)...)
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
	rows, code := st.planTransaction("upgrade", opts, nil)
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

// implicitAutoremovePlan is the #64 seam. #64 owns planning the second orphan
// transaction; it returns ok=false until then, so Nothing to do still prints
// only when both plans are empty.
func (st *state) implicitAutoremovePlan(cmd string, opts *options) ([]xbps.Row, bool) {
	// TODO(#64): plan `xbps-remove -n -o` (with -f when the primary purged)
	// when auto_remove applies, and hand the rows back to the normal summary,
	// prompt and execution path.
	return nil, false
}

// implicitAutoremoveAfterSuccess is the other #64 seam: the second confirmed
// transaction after a successful primary one.
func (st *state) implicitAutoremoveAfterSuccess(cmd string, opts *options, rows []xbps.Row) {
	// TODO(#64): plan, confirm and run the orphan transaction after success;
	// skip under assume_yes unless --auto-remove was explicit.
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

// prompt asks FR-015's question once. It returns ok=false and the exit code
// when the user refuses, answers something else, or sends EOF; an invalid
// answer is never re-prompted.
func (st *state) prompt() (bool, int) {
	scanner := bufio.NewScanner(st.stdin)
	fmt.Fprint(st.stderr, "Do you want to continue? [Y/n] ")
	if !scanner.Scan() {
		return false, st.refuse()
	}
	answer := strings.TrimSpace(scanner.Text())
	switch strings.ToLower(answer) {
	case "", "y", "yes":
		return true, exitOK
	case "n", "no":
		return false, st.refuse()
	default:
		fmt.Fprintf(st.stderr, "xbpretty: '%s' is not a valid response\n", answer)
		return false, exitRuntime
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
// versions and hold state the summary needs.
func (st *state) planToSummary(rows []xbps.Row) *summary.Summary {
	srows := make([]summary.Row, 0, len(rows))
	var upgradeNames []string
	for _, row := range rows {
		srows = append(srows, summary.Row{
			Name: row.Name, Version: row.Version, Action: row.Action,
			Repo: row.Repo, InstalledSize: row.InstalledSize,
			DownloadSize: row.DownloadSize, HasInstalled: row.HasInstalled,
			HasDownload: row.HasDownload,
		})
		if row.Action == "update" || row.Action == "reinstall" {
			upgradeNames = append(upgradeNames, row.Name)
		}
	}
	old := st.lookupOld(upgradeNames)
	held := st.heldPackages()
	return summary.Build(srows, held, old)
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

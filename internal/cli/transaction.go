package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/soubarnak/xbpretty/internal/hints"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// binaryFor picks the xbps binary a command wraps (FR-002 .. FR-006).
func (st *state) binaryFor(cmd string) string {
	if cmd == "remove" || cmd == "autoremove" {
		return st.r.cfg.XBPSRemove
	}
	return st.r.cfg.XBPSInstall
}

// planCommand builds the dry run as one flag cluster followed by the package
// arguments: `-Sn`/`-n` for install and remove, `-Sun`/`-un` for upgrade, `-on`
// for autoremove. `-S` is only usable as root, so an unprivileged plan runs
// against the cached index and says so (FR-007).
func (st *state) planCommand(cmd string, opts *options) []string {
	wantsSync := st.sync && !opts.noSync && cmdUsesSync(cmd)
	if wantsSync && euidFn() != 0 {
		fmt.Fprintln(st.stderr, "warning: not running as root, so the repository index was not refreshed; it may be stale")
		wantsSync = false
	}
	args := []string{planFlags(cmd, wantsSync)}
	args = append(args, opts.args...)
	args = append(args, opts.passthru...)
	return args
}

// planFlags writes the dry run's flags as one token, the form xbps documents
// and the acceptance criteria pin: -Sn, -un, -Sun, -on.
func planFlags(cmd string, syncing bool) string {
	flags := "-"
	if syncing {
		flags += "S"
	}
	return flags + actionLetters(cmd) + "n"
}

// actionLetters are the command's own action flags, without the leading dash:
// upgrade updates every package, autoremove removes orphans.
func actionLetters(cmd string) string {
	switch cmd {
	case "upgrade":
		return "u"
	case "autoremove":
		return "o"
	}
	return ""
}

// xbpsAction returns the action arguments for a command. The index refresh is
// not one of them: FR-007 and FR-008 add and drop `-S` on the dry run and the
// real run.
func xbpsAction(cmd string) []string {
	switch cmd {
	case "upgrade":
		return []string{"-u"}
	case "autoremove":
		return []string{"-o"}
	case "update":
		return []string{"-S"}
	}
	return nil
}

// cmdUsesSync reports whether a command's xbps binary understands `-S`.
// xbps-remove takes no such flag, so remove and autoremove never carry it
// (FR-005).
func cmdUsesSync(cmd string) bool {
	switch cmd {
	case "install", "upgrade", "update":
		return true
	}
	return false
}

// realCommand builds the execution command, dropping `-S` when the plan
// already refreshed the index (FR-008).
func (st *state) realCommand(cmd string, opts *options, planSynced bool) []string {
	args := []string{}
	if st.sync && !opts.noSync && cmdUsesSync(cmd) && !planSynced {
		args = append(args, "-S")
	}
	args = append(args, "-y")
	args = append(args, xbpsAction(cmd)...)
	args = append(args, opts.args...)
	args = append(args, opts.passthru...)
	return args
}

// plan runs the dry run and returns the parsed rows, or a non-negative exit
// code when the run should stop. wantSync reports whether the plan actually
// refreshed the index, so the real run can skip it (FR-008).
func (st *state) plan(cmd string, opts *options, wantSync *bool) ([]xbps.Row, int) {
	install := st.binaryFor(cmd)
	if st.sync && !opts.noSync && cmdUsesSync(cmd) && euidFn() == 0 {
		*wantSync = true
	}
	args := st.planCommand(cmd, opts)
	res, err := st.r.run(install, args...)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return nil, exitRuntime
	}
	if res.exitCode != 0 {
		fmt.Fprint(st.stderr, res.stderr)
		fmt.Fprint(st.stdout, res.stdout)
		if h := hints.Lookup(res.exitCode, res.stderr); h != nil {
			fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
		}
		return nil, res.exitCode
	}
	rows, err := xbps.ParsePlan(strings.NewReader(res.stdout))
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: cannot read the xbps plan: %v\n", err)
		return nil, exitUsage
	}
	return rows, -1
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
				res, err := st.r.run(st.r.cfg.XBPSQuery, "-p", "pkgver,installed_size", j.name)
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
	res, err := st.r.run(st.r.cfg.XBPSQuery, "-H")
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

// isTerminal reports whether f is attached to a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// prompt asks the FR-009 [Y/n] question until the answer is clear. It
// returns false only when the user declines.
func (st *state) prompt() bool {
	scanner := bufio.NewScanner(st.stdin)
	for {
		fmt.Fprint(st.stderr, "Proceed with this transaction? [Y/n] ")
		if !scanner.Scan() {
			fmt.Fprintln(st.stderr)
			return false
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "", "y":
			return true
		case "n":
			return false
		}
	}
}

// privHint prints the doas/sudo advice for FR-016.
func (st *state) privHint(cmd string, opts *options) {
	elevate := "sudo"
	if _, err := os.Stat("/usr/bin/doas"); err == nil {
		elevate = "doas"
	}
	line := "xbpretty " + cmd
	if len(opts.args) > 0 {
		line += " " + strings.Join(opts.args, " ")
	}
	fmt.Fprintf(st.stderr, "hint: rerun as root: %s %s\n", elevate, line)
}

// rebootHint reports packages whose update needs a reboot (FR-015a).
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

// isTTY reports whether r is the process's terminal.
func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return isTerminalFn(f)
}

// relativeDir is the directory used when resolving a link target.
func relativeDir(path string) string {
	return filepath.Dir(path)
}

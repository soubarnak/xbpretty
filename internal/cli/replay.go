package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// Replay is FR-033..FR-035: undo and redo replay a history entry's pinned
// before/after state through the normal summary, prompt and commit path. The
// derivation and preflight never mutate anything; the only mutating children
// are the restore phase, the removal phase and the best-effort automatic-bit
// restore (FR-034).

const (
	// replayTmpPrefix names a cache-only replay repository.
	replayTmpPrefix = "xbpretty-replay-"
	// replayTmpMaxAge is how old a leftover replay repository must be before
	// the next replay sweeps it.
	replayTmpMaxAge = 24 * time.Hour
	// cacheDirDisplay is FR-033's literal cache path in the fail-closed
	// message, even when a test hook points the cache elsewhere.
	cacheDirDisplay = "/var/cache/xbps"
)

// replayTmpBase is where cache-only replay repositories live. The
// xbpretty_testroot hook may point it at a fixture.
var replayTmpBase = "/var/tmp"

// replayTarget is one package the replay will restore or remove.
type replayTarget struct {
	name          string
	arch          string
	repository    string
	version       string // empty for a removal
	state         *history.PackageState
	forceRemove   bool
	size          uint64
	installedSize uint64
	cacheHit      bool
	// action is the summary/plan action: install or update for a restore,
	// remove for a removal.
	action string
}

// replayPlan is the derived undo/redo plan. Restores always run before
// removals.
type replayPlan struct {
	restores []replayTarget
	removes  []replayTarget
}

func (p replayPlan) empty() bool { return len(p.restores) == 0 && len(p.removes) == 0 }

func (p replayPlan) hasCacheRestore() bool {
	for _, target := range p.restores {
		if target.cacheHit {
			return true
		}
	}
	return false
}

func (p replayPlan) forceRemoval() bool {
	for _, target := range p.removes {
		if target.forceRemove {
			return true
		}
	}
	return false
}

// rows returns the combined plan in execution order: restores first, then
// removals. It feeds the summary, predownload, history and the line log.
func (p replayPlan) rows() []xbps.Row {
	rows := make([]xbps.Row, 0, len(p.restores)+len(p.removes))
	for _, target := range p.restores {
		rows = append(rows, xbps.Row{
			Name:          target.name,
			Version:       target.version,
			Action:        target.action,
			Arch:          target.arch,
			Repo:          target.repository,
			InstalledSize: target.installedSize,
			DownloadSize:  target.size,
			HasInstalled:  true,
			HasDownload:   target.size > 0,
		})
	}
	for _, target := range p.removes {
		rows = append(rows, xbps.Row{
			Name:          target.name,
			Version:       target.version,
			Action:        xbps.ActionRemove,
			Arch:          target.arch,
			Repo:          target.repository,
			InstalledSize: target.installedSize,
			HasInstalled:  true,
			HasDownload:   false,
		})
	}
	return rows
}

// downloadRows is FR-036's replay download set: repository-served restores
// only. A cache-only archive is already present and must not be fetched again.
func (p replayPlan) downloadRows() []xbps.Row {
	var rows []xbps.Row
	for _, target := range p.restores {
		if target.cacheHit {
			continue
		}
		rows = append(rows, xbps.Row{
			Name:          target.name,
			Version:       target.version,
			Action:        target.action,
			Arch:          target.arch,
			Repo:          target.repository,
			InstalledSize: target.installedSize,
			DownloadSize:  target.size,
			HasInstalled:  true,
			HasDownload:   target.size > 0,
		})
	}
	return rows
}

// runReplay implements the undo/redo driver.
func (st *state) runReplay(cmd string, opts *options) int {
	// FR-053: only the binaries the derived plan actually uses are checked.
	if err := st.r.cfg.ResolveFor("query"); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	entry, code := st.resolveReplayEntry(cmd, opts)
	if code >= 0 {
		return code
	}
	current, err := st.installedVersions()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	plan, err := st.buildReplayPlan(entry, cmd, current)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if plan.empty() {
		fmt.Fprintln(st.stdout, "Nothing to do.")
		return exitNothingToDo
	}
	if code := st.preflightReplay(&plan, cmd); code >= 0 {
		return code
	}
	if len(plan.restores) > 0 {
		if err := st.r.cfg.ResolveFor("install"); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
	}
	if len(plan.removes) > 0 {
		if err := st.r.cfg.ResolveFor("remove"); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
	}
	if plan.hasCacheRestore() {
		if err := st.r.cfg.ResolveFor("rindex"); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
	}
	rows := plan.rows()
	if code := st.renderSummary(cmd, rows); code >= 0 {
		return code
	}
	// FR-015: an explicit refusal stops before the root and non-TTY checks.
	if opts.assumeNo {
		return st.refuse()
	}
	// FR-073: the non-root check is unconditional and comes before the
	// non-TTY check, so a non-root replay always gets the summary and hint.
	if euidFn() != 0 {
		st.privHint(cmd, opts)
		return exitRuntime
	}
	if !st.assumeYes && !isTTY(st.stdin) {
		fmt.Fprintln(st.stderr, "xbpretty: stdin is not a terminal, so xbpretty will not prompt; pass -y to proceed")
		return exitRuntime
	}
	if !st.assumeYes {
		result := st.prompt()
		if !result.proceed {
			return result.code
		}
	}

	// FR-036: a replay's repository-served restores fill the cache through
	// the same predownload path as any real transaction (#72's seam).
	if code := st.predownload(plan.downloadRows(), false); code >= 0 {
		return code
	}

	// A replay's new entry records the actual transitions. The history
	// derivation understands `-f` through opts.passthru; the phase argv is
	// built independently so no user passthrough can reach xbps.
	historyOpts := &options{}
	if plan.forceRemoval() {
		historyOpts.passthru = []string{"-f"}
	}
	st.beginHistoryEntry(cmd, historyOpts, rows, st.commandLine, []string{}, false, &entry.ID, cmd)

	var phases []xbpsPhase
	if len(plan.restores) > 0 {
		restoreFlags := []string{"-y", "-f"}
		var repo *replayRepo
		if plan.hasCacheRestore() {
			repo, code = st.prepareReplayRepo(plan)
			if code >= 0 {
				return code
			}
			defer repo.Cleanup()
			restoreFlags = append(restoreFlags, "-R", repo.Dir())
		}
		specs := make([]string, 0, len(plan.restores))
		for _, target := range plan.restores {
			specs = append(specs, target.name+"-"+target.version)
		}
		restoreFlags = append(restoreFlags, specs...)
		phases = append(phases, xbpsPhase{
			bin:  st.r.cfg.Xbps.InstallPath,
			args: restoreFlags,
			rows: rows[:len(plan.restores)],
		})
	}
	if len(plan.removes) > 0 {
		removeFlags := []string{"-y"}
		if plan.forceRemoval() {
			removeFlags = append(removeFlags, "-f")
		}
		for _, target := range plan.removes {
			removeFlags = append(removeFlags, target.name)
		}
		phases = append(phases, xbpsPhase{
			bin:  st.r.cfg.Xbps.RemovePath,
			args: removeFlags,
			rows: rows[len(plan.restores):],
		})
	}

	partial := fmt.Sprintf("%s is partial: the restore phase completed and the removal phase failed", cmd)
	return st.executePhasesAfter(cmd, realReplayLogArgs(cmd, opts), rows, partial,
		func() { st.restoreAutomaticFlags(plan) }, phases...)
}

// realReplayLogArgs is the argv the line log would name. writeLog ignores the
// argv and derives the package list from rows, so the value is documentary.
func realReplayLogArgs(cmd string, opts *options) []string {
	return append([]string{cmd}, opts.args...)
}

// resolveReplayEntry loads the entry a replay targets, applying FR-035's
// selector rules: bare undo/redo target the newest entry, except bare redo
// with a newest `undo` entry, which targets that undo's source. Explicit
// `redo last` is always the newest entry literally.
func (st *state) resolveReplayEntry(cmd string, opts *options) (history.Entry, int) {
	store := history.New(st.historyDir)
	selector := ""
	if len(opts.args) > 0 {
		selector = opts.args[0]
	}
	if cmd == "redo" && selector == "" {
		newest, present, err := store.Last()
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return history.Entry{}, exitRuntime
		}
		if !present {
			fmt.Fprintln(st.stderr, "xbpretty: history is empty")
			return history.Entry{}, exitRuntime
		}
		if newest.ReplayKind != nil && *newest.ReplayKind == "undo" && newest.ReplayOf != nil {
			source, err := store.Load(*newest.ReplayOf)
			if errors.Is(err, history.ErrNotFound) {
				fmt.Fprintf(st.stderr, "xbpretty: history entry %d not found\n", *newest.ReplayOf)
				return history.Entry{}, exitRuntime
			}
			if err != nil {
				fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
				return history.Entry{}, exitRuntime
			}
			return st.replayableEntry(source)
		}
		return st.replayableEntry(newest)
	}
	if selector == "" {
		selector = "last"
	}
	id, last, ok := parseHistorySelector(selector)
	if !ok {
		fmt.Fprintf(st.stderr, "xbpretty: invalid history selector %q; use an integer id or 'last'\n", selector)
		return history.Entry{}, exitUsage
	}
	if last {
		entry, present, err := store.Last()
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return history.Entry{}, exitRuntime
		}
		if !present {
			fmt.Fprintln(st.stderr, "xbpretty: history is empty")
			return history.Entry{}, exitRuntime
		}
		return st.replayableEntry(entry)
	}
	entry, err := store.Load(id)
	if errors.Is(err, history.ErrNotFound) {
		fmt.Fprintf(st.stderr, "xbpretty: history entry %d not found\n", id)
		return history.Entry{}, exitRuntime
	}
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return history.Entry{}, exitRuntime
	}
	return st.replayableEntry(entry)
}

// replayableEntry refuses a read-only entry before any plan or child.
func (st *state) replayableEntry(entry history.Entry) (history.Entry, int) {
	if entry.Status != history.StatusApplied {
		fmt.Fprintf(st.stderr, "xbpretty: history entry %d is not replayable\n", entry.ID)
		return history.Entry{}, exitRuntime
	}
	return entry, -1
}

// installedVersions reads the current installed pkgvers once. The bulk `-l`
// pass is the same snapshot the recorder uses, so replay and recording agree
// on what "present" means.
func (st *state) installedVersions() (map[string]string, error) {
	stdout, err := st.historyBulkQuery("-l")
	if err != nil {
		return nil, err
	}
	snap := newSnapshot()
	snap.parseInstalled(stdout)
	versions := make(map[string]string, len(snap.installed))
	for name, info := range snap.installed {
		versions[name] = info.Version
	}
	return versions, nil
}

// buildReplayPlan derives FR-033/FR-035's targets. Configure/Held rows are
// filtered, an undo of a Reinstall is refused, before/after absence becomes a
// removal, and a target already equal to the current state is skipped.
func (st *state) buildReplayPlan(entry history.Entry, cmd string, current map[string]string) (replayPlan, error) {
	var plan replayPlan
	replayable := 0
	for _, transition := range entry.Packages {
		if transition.Operation == history.OpConfigure || transition.Operation == history.OpHeld {
			continue
		}
		replayable++
		var targetState *history.PackageState
		forceRemove := false
		if cmd == "undo" {
			if transition.Operation == history.OpReinstall {
				return replayPlan{}, fmt.Errorf("history entry %d contains a reinstall, which has no inverse; undo cannot continue", entry.ID)
			}
			targetState = transition.Before
			if targetState == nil || targetState.Version == nil {
				// Absent before: the transaction installed the package, so
				// undoing it removes (forced) when present.
				forceRemove = true
			}
		} else {
			switch transition.Operation {
			case history.OpRemove, history.OpAutoRemove:
				targetState = nil
			case history.OpPurge, history.OpAutoPurge:
				targetState = nil
				forceRemove = true
			default:
				targetState = transition.After
			}
		}

		currentVersion, present := current[transition.Name]
		// The recorded after state is what the current state should match;
		// a mismatch is a warning, never a reason to refuse (FR-033).
		recordedAfter := stateVersion(transition.After)
		currentDisplay := presentVersion(present, currentVersion)
		if currentDisplay != recordedAfter {
			target := stateVersion(targetState)
			fmt.Fprintf(st.stderr, "xbpretty: warning: %s is %s but history entry %d recorded %s; %s still restores %s\n",
				transition.Name, currentDisplay, entry.ID, recordedAfter, cmd, target)
		}

		targetVersion := stateVersion(targetState)
		if targetState == nil || targetState.Version == nil || targetVersion == "" {
			if !present {
				continue
			}
			plan.removes = append(plan.removes, replayTarget{
				name:       transition.Name,
				arch:       transition.Arch,
				repository: transition.Repository,
				// The installed pkgver names the audit line; the removal argv
				// itself takes only names.
				version:       currentVersion,
				forceRemove:   forceRemove,
				installedSize: transition.InstalledSize,
				action:        xbps.ActionRemove,
			})
			continue
		}
		if present && currentVersion == targetVersion {
			// Already equal: leave it untouched, including the automatic bit.
			continue
		}
		action := xbps.ActionInstall
		if present {
			action = xbps.ActionUpdate
		}
		plan.restores = append(plan.restores, replayTarget{
			name:          transition.Name,
			arch:          transition.Arch,
			repository:    transition.Repository,
			version:       targetVersion,
			state:         targetState,
			size:          transition.Size,
			installedSize: transition.InstalledSize,
			action:        action,
		})
	}
	if replayable == 0 {
		return replayPlan{}, fmt.Errorf("history entry %d has no package changes to replay", entry.ID)
	}
	return plan, nil
}

// stateVersion renders a recorded state's version, or "absent".
func stateVersion(state *history.PackageState) string {
	if state == nil || state.Version == nil {
		return "absent"
	}
	return *state.Version
}

// presentVersion is the current-state rendering used by the warning.
func presentVersion(present bool, version string) string {
	if !present {
		return "absent"
	}
	return version
}

// preflightReplay proves every restore archive is available before any
// mutating child (FR-033). The cache is checked first; an absent archive asks
// the configured repositories for the exact version. Every unrestorable
// package prints its own line and the replay exits 1 without touching xbps.
func (st *state) preflightReplay(plan *replayPlan, cmd string) int {
	cacheDir := cacheDirFn()
	failures := make([]string, 0)
	for i := range plan.restores {
		target := &plan.restores[i]
		archive := target.name + "-" + target.version + "." + target.arch + ".xbps"
		if info, err := os.Stat(filepath.Join(cacheDir, archive)); err == nil && info.Mode().IsRegular() {
			target.cacheHit = true
			continue
		}
		served, err := st.repositoryServes(target.name + "-" + target.version)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		if !served {
			failures = append(failures, fmt.Sprintf(
				"xbpretty: %s cannot restore %s-%s.%s: not in %s and no configured repository serves that version",
				cmd, target.name, target.version, target.arch, cacheDirDisplay))
			continue
		}
	}
	if len(failures) > 0 {
		for _, line := range failures {
			fmt.Fprintln(st.stderr, line)
		}
		return exitRuntime
	}
	return -1
}

// repositoryServes asks `xbps-query -R -p pkgver <pkgver>` for the exact
// recorded version. A non-zero exit or a different pkgver means no configured
// repository serves it.
func (st *state) repositoryServes(pkgver string) (bool, error) {
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-R", "-p", "pkgver", pkgver)
	if err != nil {
		return false, err
	}
	if res.exitCode != 0 {
		return false, nil
	}
	for _, line := range strings.Split(res.stdout, "\n") {
		if strings.TrimSpace(line) == pkgver {
			return true, nil
		}
	}
	return false, nil
}

// replayRepo is the temporary cache-only repository FR-034 describes.
type replayRepo struct {
	dir string
}

// Dir returns the repository path passed to -R.
func (r *replayRepo) Dir() string { return r.dir }

// Cleanup removes the temporary repository.
func (r *replayRepo) Cleanup() {
	if r == nil || r.dir == "" {
		return
	}
	_ = os.RemoveAll(r.dir)
}

// prepareReplayRepo builds the temporary repository for cache-only archives:
// sweep, MkdirTemp, link/copy every archive preserving its filename, then
// xbps-rindex it. It returns before any mutating xbps child.
func (st *state) prepareReplayRepo(plan replayPlan) (*replayRepo, int) {
	sweepReplayRepos(replayTmpBase)
	dir, err := os.MkdirTemp(replayTmpBase, replayTmpPrefix)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: cannot create the replay repository: %v\n", err)
		return nil, exitRuntime
	}
	repo := &replayRepo{dir: dir}
	fail := func(format string, args ...any) (*replayRepo, int) {
		fmt.Fprintf(st.stderr, format+"\n", args...)
		repo.Cleanup()
		return nil, exitRuntime
	}

	cacheDir := cacheDirFn()
	seen := map[string]bool{}
	var archives []string
	for _, target := range plan.restores {
		if !target.cacheHit {
			continue
		}
		archive := target.name + "-" + target.version + "." + target.arch + ".xbps"
		if seen[archive] {
			continue
		}
		seen[archive] = true
		src := filepath.Join(cacheDir, archive)
		dst := filepath.Join(dir, archive)
		if err := linkOrCopy(src, dst); err != nil {
			return fail("xbpretty: cannot stage %s for the replay repository: %v", archive, err)
		}
		archives = append(archives, dst)
	}
	if len(archives) == 0 {
		return fail("xbpretty: the replay repository has no archives")
	}

	args := append([]string{"-a"}, archives...)
	res, err := st.r.run(st.r.cfg.Xbps.RindexPath, args...)
	if err != nil {
		return fail("xbpretty: cannot index the replay repository: %v", err)
	}
	if res.exitCode != 0 {
		// An index entry that already exists is reindexed with -f. This is
		// the only place a replay repo adds -f; the transaction argv still
		// carries exactly one.
		forceArgs := append([]string{"-f", "-a"}, archives...)
		forced, err := st.r.run(st.r.cfg.Xbps.RindexPath, forceArgs...)
		if err != nil {
			return fail("xbpretty: cannot index the replay repository: %v", err)
		}
		if forced.exitCode != 0 {
			return fail("xbpretty: cannot index the replay repository: xbps-rindex exited %d", forced.exitCode)
		}
	}
	return repo, -1
}

// linkOrCopy hard-links src to dst, falling back to a copy across devices.
func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

// sweepReplayRepos removes leftover replay repositories older than a day.
func sweepReplayRepos(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-replayTmpMaxAge)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), replayTmpPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(base, entry.Name()))
	}
}

// restoreAutomaticFlags applies FR-034's best-effort bulk pkgdb restoration.
// It runs after both phases and before the entry's after-snapshot, so the new
// entry records the corrected bit. Every failure is a warning per package.
func (st *state) restoreAutomaticFlags(plan replayPlan) {
	byAuto := map[bool][]string{}
	for _, target := range plan.restores {
		if target.state == nil || target.state.AutoInstalled == nil {
			continue
		}
		auto := *target.state.AutoInstalled
		byAuto[auto] = append(byAuto[auto], target.name)
	}
	if len(byAuto) == 0 {
		return
	}
	if err := st.r.cfg.ResolveFor("pkgdb"); err != nil {
		for _, target := range plan.restores {
			if target.state != nil && target.state.AutoInstalled != nil {
				fmt.Fprintf(st.stderr, "xbpretty: could not restore the automatic-install flag for %s\n", target.name)
			}
		}
		return
	}
	for _, auto := range []bool{true, false} {
		group := byAuto[auto]
		if len(group) == 0 {
			continue
		}
		mode := "manual"
		if auto {
			mode = "auto"
		}
		args := append([]string{"-m", mode}, group...)
		res, err := st.r.run(st.r.cfg.Xbps.PkgdbPath, args...)
		if err != nil || res.exitCode != 0 {
			for _, name := range group {
				fmt.Fprintf(st.stderr, "xbpretty: could not restore the automatic-install flag for %s\n", name)
			}
		}
	}
}

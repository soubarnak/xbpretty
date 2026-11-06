package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/fetch"
)

// Fetch seams. They are package variables only so the tests can inject a local
// fixture; nothing in the release binary can change them. A built-binary test
// hook that needs them is compiled only under the xbpretty_testroot tag.
var (
	fetchMirrorListURL = fetch.MirrorListURL
	fetchXBPSDDirs     = []string{"/etc/xbps.d", "/usr/share/xbps.d"}
	fetchWriteDir      = "/etc/xbps.d"
	fetchManifestPath  = fetch.DefaultManifestPath
	fetchHTTPClient    = fetch.NewHTTPClient
	// fetchArch, when non-empty, stands in for the detected architecture
	// (see xbpsArch). Only the test hooks set it.
	fetchArch          = ""
	fetchPreviousHosts = func() []string { return fetch.ManifestMirrorURLs(fetchManifestPath) }
	// pickMirror is #78's picker seam. Nil means the real enhanced-gate
	// picker; the non-TTY top-3 shortlist is the fallback below the floor or
	// when the screen cannot open.
	pickMirror func(ranked []fetch.Result) (int, bool)
	// fetchRankedHook is a test-only seam (xbpretty_testroot): the PTY golden
	// needs deterministic latency/speed/score columns. Release builds leave it
	// nil, so Rank's real ordering always reaches the picker.
	fetchRankedHook func([]fetch.Result) []fetch.Result
	// mirrorScoreEvents receives the MirrorScore progress events. #66 renders
	// them; this ticket owns only the emission.
	mirrorScoreEvents = func(fetch.ProgressEvent) {}
)

const fetchMirrorListTimeout = 10 * time.Second

// fetchWritePlan is everything #74's write step needs from the read-only
// pipeline: the one chosen mirror, the repository suffixes it was validated
// against, and whether --non-free asked for derived files.
type fetchWritePlan struct {
	Mirror       fetch.Result
	Mirrors      []fetch.Mirror // the live list (or snapshot) after filtering
	Repositories fetch.RepositorySet
	Suffixes     []string
	Arch         string
	NonFree      bool
	WrittenAt    time.Time
}

// runFetch is FR-042's read-only pipeline plus the #74 write seam.
func (st *state) runFetch(opts *options) int {
	cfg, err := fetch.DiscoverRepositories(fetchXBPSDDirs)
	if err != nil {
		st.errorf("%v\n", err)
		return exitRuntime
	}

	client := fetchHTTPClient()
	userAgent := "xbpretty/" + Version
	ctx, cancel := context.WithTimeout(context.Background(), fetchMirrorListTimeout)
	mirrors, warnings, err := fetch.FetchMirrorsDetailed(ctx, client, fetchMirrorListURL, userAgent)
	cancel()
	if err != nil {
		st.errorf("mirror list unavailable; using the built-in snapshot from %s\n", fetch.SnapshotDate)
		mirrors, err = fetch.Snapshot()
		if err != nil {
			st.errorf("%s\n", "no mirror list available")
			return exitRuntime
		}
		warnings = nil
	}
	if st.r != nil && st.r.verbose {
		for _, warning := range warnings {
			fmt.Fprintf(st.stderr, "verbose: mirror list: %s\n", warning)
		}
	}

	repos, err := fetch.ClassifyRepositories(cfg, mirrorBaseURLs(mirrors), fetchPreviousHosts())
	if err != nil {
		st.errorf("%v\n", err)
		return exitRuntime
	}
	arch := st.xbpsArch(cfg)

	mirrors = fetch.FilterMirrors(mirrors, opts.country)
	if len(mirrors) == 0 {
		st.errorf("%s\n", "no mirrors match the requested country or region")
		return exitRuntime
	}
	if opts.httpsOnly {
		for i := range mirrors {
			mirrors[i] = fetch.RewriteToHTTPS(mirrors[i])
		}
	}

	// FR-064's Testing Mirrors view is fed from #73's ProgressEvent stream.
	// Ctrl-C cancels the round: xbpretty owns these requests, so it tears the
	// view down, prints the interruption and exits 130 (FR-072).
	//
	// #79 scope: MirrorScore keeps #66's in-place renderer rather than the S4
	// inline viewport. It is a childless probe round with no completed
	// download/unpack lines, so there is nothing to insert into scrollback;
	// the transaction phases (predownload and the commit child) carry the
	// viewport.
	var live *liveProgress
	progress := mirrorScoreEvents
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	var (
		sigMu     sync.Mutex
		delivered os.Signal
	)
	go func() {
		select {
		case sig := <-sigCh:
			sigMu.Lock()
			delivered = sig
			sigMu.Unlock()
			cancel()
		case <-ctx.Done():
		}
	}()
	if mode := st.mirrorLiveMode(opts); mode != liveOff {
		model := newProgressModel("mirror", nil, nil)
		live = newLiveProgress(st.stderr, st.colorErr, terminalWidth(st.stderr), mode, model, st.theme)
		st.r.live = true
		st.r.liveProgress = live
		defer live.Finish()
		// Seed the known total before the first frame so it never renders as
		// an empty or every-mirror-done box.
		live.Apply(true, func(m *progressModel) { m.mirrorTotal = len(mirrors) })
		progress = func(event fetch.ProgressEvent) {
			mirrorScoreEvents(event)
			live.Apply(true, func(m *progressModel) {
				m.mirrorEvent(event.Finished, event.Done, event.Total, event.Elapsed)
			})
		}
	}
	report := fetch.ProbeAll(ctx, fetch.ProbeOptions{
		Client:      client,
		Mirrors:     mirrors,
		PrimaryPath: repos.Primary,
		Arch:        arch,
		UserAgent:   userAgent,
		Progress:    progress,
	})
	if live != nil {
		live.Finish()
		st.r.live = false
		st.r.liveProgress = nil
	}
	if ctx.Err() != nil {
		sigMu.Lock()
		interruptedBy := delivered
		sigMu.Unlock()
		st.errorf("%s\n", "interrupted")
		if interruptedBy == syscall.SIGTERM {
			return 128 + int(syscall.SIGTERM)
		}
		return exitInterrupted
	}
	if len(report.Failures) > 0 {
		if st.r != nil && st.r.verbose {
			for _, failure := range report.Failures {
				fmt.Fprintf(st.stderr, "verbose: %s: %v\n", failure.Mirror.URL, failure.Err)
			}
		}
		st.errorf("%d of %d mirrors did not respond\n", len(report.Failures), len(mirrors))
	}

	ranked := fetch.Rank(report.Results)
	if fetchRankedHook != nil {
		ranked = fetchRankedHook(ranked)
	}
	if len(ranked) == 0 {
		st.errorf("%s\n", "no mirrors responded")
		return exitRuntime
	}

	suffixes := append([]string(nil), repos.Suffixes...)
	if opts.nonFree {
		suffixes = appendUnique(suffixes, fetch.NonFreeSuffixes(repos.Suffixes)...)
	}

	chosen, code, ok := st.chooseMirror(client, ranked, suffixes, arch, userAgent, opts, st.fetchPickerAllowed(opts))
	if !ok {
		return code
	}

	if err := fetch.RenderTable(st.stdout, ranked, chosen.URL); err != nil {
		st.errorf("%v\n", err)
		return exitRuntime
	}

	if opts.dryRun {
		fmt.Fprintln(st.stdout, "Dry run: no repository configuration was written.")
		return exitOK
	}
	if euidFn() != 0 {
		st.hintf("%s\n", "run with sudo to write the repository configuration")
		return exitRuntime
	}
	// FR-053: prove the configured sync binary before the first byte is
	// written, so a missing xbps-install cannot leave a half-done fetch.
	if err := st.r.cfg.ResolveFor("install"); err != nil {
		st.errorf("%v\n", err)
		return exitRuntime
	}

	paths, code, err := st.writeRepositoryConfiguration(fetchWritePlan{
		Mirror:       chosen,
		Mirrors:      mirrors,
		Repositories: repos,
		Suffixes:     suffixes,
		Arch:         arch,
		NonFree:      opts.nonFree,
		WrittenAt:    time.Now(),
	})
	if err != nil {
		st.errorf("%v\n", err)
		return code
	}
	for _, path := range paths {
		fmt.Fprintf(st.stdout, "Sources have been written to %s\n", path)
	}
	return code
}

// fetchPickerAllowed is FR-067's S3 gate: the enhanced gate (mode != Plain,
// TTY stdout, TERM != dumb) plus a TTY stdin, a UTF-8 locale, root and no
// --auto/--dry-run. --no-tui demotes Tui to Auto, where the picker still
// appears; Plain never does.
func (st *state) fetchPickerAllowed(opts *options) bool {
	return !opts.dryRun && !opts.explicit["auto"] &&
		euidFn() == 0 &&
		isTTY(st.stdin) && isTTY(st.stdout) &&
		st.mode != config.ModePlain &&
		!strings.EqualFold(os.Getenv("TERM"), "dumb") &&
		utf8Locale()
}

// chooseMirror implements FR-045's shortlist rule. --auto N validates the N
// fastest in rank order. Without --auto the picker returns one index (or, when
// the screen is unavailable, the top-3 shortlist runs); the picked row is
// validated once: a failed validation is an error, never a silent second
// choice.
func (st *state) chooseMirror(client *http.Client, ranked []fetch.Result, suffixes []string, arch, userAgent string, opts *options, allowPicker bool) (fetch.Result, int, bool) {
	validate := func(candidate fetch.Result) error {
		return fetch.Validate(context.Background(), fetch.ValidationOptions{
			Client:    client,
			BaseURL:   candidate.URL,
			Suffixes:  suffixes,
			Arch:      arch,
			UserAgent: userAgent,
		})
	}

	validatePick := func(index int) (fetch.Result, int, bool) {
		if index < 0 || index >= len(ranked) {
			st.errorf("%s\n", "no candidate mirror serves all configured repositories")
			return fetch.Result{}, exitRuntime, false
		}
		if err := validate(ranked[index]); err != nil {
			if st.r != nil && st.r.verbose {
				fmt.Fprintf(st.stderr, "verbose: %s: %v\n", ranked[index].URL, err)
			}
			st.errorf("%s\n", "no candidate mirror serves all configured repositories")
			return fetch.Result{}, exitRuntime, false
		}
		return ranked[index], exitOK, true
	}

	if allowPicker && pickMirror != nil {
		index, ok := pickMirror(ranked)
		if !ok {
			st.errorf("%s\n", "no mirror was selected")
			return fetch.Result{}, exitRuntime, false
		}
		return validatePick(index)
	}
	if allowPicker && pickMirror == nil {
		index, ok, code := st.runFetchPicker(ranked)
		switch {
		case code < 0:
			// The screen could not open or the terminal is below the floor.
			// Warn (if any) already printed; fall through to the shortlist.
		case !ok:
			if code > 0 {
				return fetch.Result{}, code, false
			}
			st.errorf("%s\n", "no mirror was selected")
			return fetch.Result{}, exitRuntime, false
		default:
			return validatePick(index)
		}
	}

	depth := 3
	if opts.explicit["auto"] {
		depth = opts.auto
	}
	if depth > len(ranked) {
		depth = len(ranked)
	}
	for _, candidate := range ranked[:depth] {
		if err := validate(candidate); err != nil {
			if st.r != nil && st.r.verbose {
				fmt.Fprintf(st.stderr, "verbose: %s: %v\n", candidate.URL, err)
			}
			continue
		}
		return candidate, exitOK, true
	}
	st.errorf("%s\n", "no candidate mirror serves all configured repositories")
	return fetch.Result{}, exitRuntime, false
}

// writeRepositoryConfiguration is #74's single write seam: one same-basename
// masking file per managed repository file, a first-write backup, the schema-1
// manifest, then xbps-install -S as the proof. FR-049 owns the failure split:
// only an ERROR: for a repository this run wrote reverts; a foreign repository
// error is forwarded with a warning and leaves the new configuration alone.
func (st *state) writeRepositoryConfiguration(plan fetchWritePlan) ([]string, int, error) {
	writes, err := fetch.PlanWrites(plan.Repositories, plan.Mirror.URL, plan.WrittenAt, fetchWriteDir, plan.NonFree)
	if err != nil {
		return nil, exitRuntime, err
	}
	if len(writes) == 0 {
		return nil, exitRuntime, fetch.ErrNoManagedRepo
	}

	previous, prevErr := fetch.ReadManifestFor(fetchManifestPath, fetchWriteDir)
	if prevErr != nil && !os.IsNotExist(prevErr) {
		// A corrupt previous manifest cannot be carried over. The current
		// write is still atomic; the operator's next clean --fetch will name
		// the broken file.
		previous = fetch.Manifest{}
	}

	_, preImages, createdBackups, err := fetch.ApplyWrites(writes, previous, fetchManifestPath, plan.WrittenAt, plan.Mirror.URL, plan.Arch)
	if err != nil {
		return nil, exitRuntime, err
	}

	paths := make([]string, 0, len(writes))
	for _, write := range writes {
		paths = append(paths, write.Path)
	}

	code, output, runErr := st.r.streamStderr(st.r.cfg.Xbps.InstallPath, "-S")
	if runErr != nil {
		st.revertFetchWrites(preImages, previous, createdBackups...)
		return nil, exitRuntime, runErr
	}

	failed, foreign := fetch.ScanSyncErrors(output.Stdout, output.Stderr, fetch.WrittenURLs(writes))
	if code != 0 || failed {
		st.revertFetchWrites(preImages, previous, createdBackups...)
		if code != 0 {
			return nil, code, nil
		}
		return nil, exitRuntime, nil
	}
	if foreign {
		st.errorf("%s\n", "repository sync reported errors; review the output above")
	}
	return paths, exitOK, nil
}

// revertFetchWrites restores the configuration that was live immediately
// before this run, not the pre-first-fetch backup, then puts the previous
// manifest back (or removes it on a first write) so carried-over entries and an
// earlier fetch stay revertable. A failed revert step keeps the current
// manifest for a later clean --fetch. createdBackups are the backups this run
// wrote; once the previous manifest is back nothing needs them, so they go,
// except one the previous manifest still names.
func (st *state) revertFetchWrites(preImages []fetch.PreImage, previous fetch.Manifest, createdBackups ...string) {
	restored := true
	for _, pre := range preImages {
		var revertErr error
		if pre.Existed {
			revertErr = fetch.WriteAtomic(pre.Path, pre.Content)
		} else {
			revertErr = os.Remove(pre.Path)
			if os.IsNotExist(revertErr) {
				revertErr = nil
			}
		}
		if revertErr != nil {
			st.errorf("reverting %s: %v\n", pre.Path, revertErr)
			restored = false
		}
	}
	if !restored {
		return
	}
	if len(previous.Files) > 0 {
		if err := fetch.WriteManifest(fetchManifestPath, previous); err != nil {
			st.errorf("reverting %s: %v\n", fetchManifestPath, err)
			return
		}
	} else {
		_ = os.Remove(fetchManifestPath)
	}
	st.removeCreatedBackups(createdBackups, previous)
	st.errorf("%s\n", "repository sync failed; previous configuration restored")
}

// removeCreatedBackups deletes the backups a reverted run wrote, except any the
// restored previous manifest still names: those are needed for a later
// clean --fetch.
func (st *state) removeCreatedBackups(createdBackups []string, previous fetch.Manifest) {
	referenced := map[string]bool{}
	for _, entry := range previous.Files {
		referenced[entry.Backup] = true
	}
	for _, backup := range createdBackups {
		if referenced[backup] {
			continue
		}
		if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
			st.errorf("reverting: removing backup %s: %v\n", backup, err)
		}
	}
}

// mirrorBaseURLs returns the live mirror URLs, whose paths are the bases a
// repository= suffix is relative to.
func mirrorBaseURLs(mirrors []fetch.Mirror) []string {
	out := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		out = append(out, mirror.URL)
	}
	return out
}

// fileExists reports whether path exists, without following a broken link's
// target.
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// appendUnique appends values that are not already present, preserving order.
func appendUnique(dst []string, values ...string) []string {
	seen := map[string]bool{}
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			dst = append(dst, v)
		}
	}
	return dst
}

// utf8Locale reports whether the effective locale is UTF-8. LC_ALL wins over
// LC_CTYPE, which wins over LANG; an unset environment is the C locale.
func utf8Locale() bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		return strings.Contains(lower, "utf-8") || strings.Contains(lower, "utf8")
	}
	return false
}

// runtimeXBPSArch maps Go's architecture name to xbps's.
func runtimeXBPSArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "i686"
	case "arm64":
		return "aarch64"
	case "arm":
		// Void packages 32-bit ARM as armv6l or armv7l. GOARM is a
		// compile-time setting, so read it back from the build info.
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "GOARM" && setting.Value == "6" {
					return "armv6l"
				}
			}
		}
		return "armv7l"
	default:
		return runtime.GOARCH
	}
}

// mirrorLiveMode applies the suppression matrix to the fetch probe: the
// MirrorScore view is part of a real run, not a dry run.
func (st *state) mirrorLiveMode(opts *options) liveMode {
	if opts.dryRun {
		return liveOff
	}
	return st.liveMode()
}

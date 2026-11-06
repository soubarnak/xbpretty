package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/download"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// defaultCacheDir is where xbps keeps downloaded archives (FR-038). xbps has
// no configuration key for it: every supported Void layout uses this path.
const defaultCacheDir = "/var/cache/xbps"

// cacheDirFn resolves the cache directory. Tests replace it; the release
// binary always uses defaultCacheDir.
var cacheDirFn = func() string { return defaultCacheDir }

// preflightFn is FR-038's disk-space check. Tests replace it to exercise the
// skip path without filling a filesystem.
var preflightFn = download.Preflight

// downloadSink is FR-041's replaceable seam. The live Download view consumes
// the same events through downloadEventView; this sink also records
// completions so the child's own fallback stream continues the same counters.
type downloadSink interface {
	Event(download.Event)
	Seed(*StreamParser)
	Completed() map[string]int64
}

// newDownloadSink builds the completion sink for one run; the live view is
// attached separately and never touches the engine.
var newDownloadSink = func(*state) downloadSink {
	return &cacheSeedSink{completed: map[string]int64{}}
}

// cacheSeedSink accumulates predownload completions for StreamParser. `.sig2`
// ticks and completions are ignored (FR-041).
type cacheSeedSink struct {
	mu        sync.Mutex
	completed map[string]int64
}

func (s *cacheSeedSink) Event(ev download.Event) {
	if ev.File == "" || strings.HasSuffix(ev.File, ".sig2") {
		return
	}
	switch ev.Kind {
	case download.EventCacheHit, download.EventFileDone:
		size := ev.Total
		if size < 0 {
			size = ev.Bytes
		}
		s.mu.Lock()
		s.completed[ev.File] = size
		s.mu.Unlock()
	}
}

func (s *cacheSeedSink) Seed(p *StreamParser) {
	if p == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for file, size := range s.completed {
		p.SeedDownloaded(file, size)
	}
}

// Completed returns a copy of the archives the fill finished, keyed by filename.
func (s *cacheSeedSink) Completed() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.completed))
	for file, size := range s.completed {
		out[file] = size
	}
	return out
}

// seedPredownloaded counts the archives the predownload fill finished into the
// commit view's model before its child starts (BF-18). Without it the view
// restarts at Packages 0/N and xbps's fallback transfers count from zero.
func (st *state) seedPredownloaded(model *progressModel) {
	if st.downloadSink == nil {
		return
	}
	completed := st.downloadSink.Completed()
	files := make([]string, 0, len(completed))
	for file := range completed {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		model.markCached(file)
	}
}

// predownloadOutcome is one cache-fill attempt.
type predownloadOutcome struct {
	attempted   int
	failed      int
	interrupted bool
	signal      os.Signal // the SIGINT or SIGTERM that stopped the fill (FR-074)
	skipped     error     // the pass could not start; xbps must do the work
	invalid     error     // repository metadata yielded an unusable filename
}

// interruptExitCode is FR-074's 128-plus-signal rule. A fill can only be stopped
// by SIGINT or SIGTERM; any other value keeps the interrupted code, 130.
func interruptExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return exitInterrupted
}

// predownload fills the cache from the plan's download set (FR-036, FR-038)
// and returns -1 when the run may continue, or an exit code when it must stop.
// failClosed is the --download-only contract: no child takes over, so a
// failure is final.
func (st *state) predownload(rows []xbps.Row, failClosed bool) int {
	outcome := st.fillCache(rows, failClosed)
	switch {
	case outcome.interrupted:
		return interruptExitCode(outcome.signal)
	case outcome.invalid != nil:
		// FR-037 names the offending metadata; there is nothing to fetch for
		// that row. A real transaction keeps FR-038's skip shape and lets
		// xbps decide; a fail-closed surface has no child and ends here.
		if failClosed {
			st.errorf("%v\n", outcome.invalid)
			return exitRuntime
		}
		st.errorf("predownload skipped: %v; xbps will download the archives\n", outcome.invalid)
		return -1
	case outcome.skipped != nil:
		if failClosed {
			st.errorf("predownload: %v\n", outcome.skipped)
			return exitRuntime
		}
		st.errorf("predownload skipped: %v; xbps will download the archives\n", outcome.skipped)
		return -1
	case outcome.failed > 0:
		if failClosed {
			return exitRuntime
		}
		st.errorf("predownload: %d of %d archives could not be fetched; xbps will download them\n",
			outcome.failed, outcome.attempted)
		return -1
	}
	return -1
}

// fillCache does the metadata pass, the disk preflight and the parallel cache
// fill. It prints the per-package metadata warnings and the fail-closed
// per-file errors; the caller owns the pass-level outcome.
func (st *state) fillCache(rows []xbps.Row, failClosed bool) predownloadOutcome {
	var outcome predownloadOutcome

	if st.predownloadDisabledByPassthru() {
		if st.r != nil && st.r.verbose {
			fmt.Fprintln(st.stderr, "verbose: predownload disabled by the -c/-r passthrough")
		}
		return outcome
	}

	// FR-036's download set: downloading actions on http(s) repositories with
	// a reported size (or an unreported download action). ftp and local rows
	// are counted, never fetched; one predicate decides both the targets and
	// the FR-037 warning counts.
	remote, excludedFTP, _ := download.PartitionDownloadRows(rows)
	if excludedFTP > 0 {
		fmt.Fprintf(st.stderr, "%d archive(s) from ftp:// repositories were not predownloaded; xbps will fetch them\n", excludedFTP)
	}
	if len(remote) == 0 {
		return outcome
	}

	dir := cacheDirFn()
	if err := prepareCacheDir(dir); err != nil {
		outcome.skipped = fmt.Errorf("the xbps cache %s is not writable: %w", dir, err)
		return outcome
	}

	// FR-037: one bulk offline filename-sha256 pass per run.
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-R", "-s", "", "-p", "filename-sha256")
	if err != nil {
		outcome.skipped = fmt.Errorf("repository metadata unavailable: %w", err)
		return outcome
	}
	if res.exitCode != 0 {
		outcome.skipped = fmt.Errorf("repository metadata unavailable: xbps-query exited %d", res.exitCode)
		return outcome
	}
	hashes := download.ParseHashes(res.stdout)

	targets, _, _, err := download.TargetsFromRows(remote, hashes)
	if err != nil {
		outcome.invalid = err
		return outcome
	}
	if len(targets) == 0 {
		return outcome
	}
	for _, target := range targets {
		if target.SHA256 == "" {
			fmt.Fprintf(st.stderr, "%s: no SHA-256 in repository metadata; xbps will verify it at install\n", target.Pkgver)
		}
	}

	// FR-038: preflight the plan's sizes against statvfs so a transfer never
	// starts on a full cache filesystem. A fully cached plan asks for nothing.
	var need uint64
	for _, target := range targets {
		need += download.ArchiveNeedSize(target, dir)
	}
	if err := preflightFn(dir, need); err != nil {
		outcome.skipped = fmt.Errorf("disk space check failed: %w", err)
		return outcome
	}

	engineOpts := download.Options{DestDir: dir, Mode: download.ModeCache}
	if st.downloadSink == nil {
		st.downloadSink = newDownloadSink(st)
	}
	// FR-041/FR-072/FR-074: Ctrl-C or SIGTERM cancels the engine, which removes
	// `.part`, then the run exits 128 plus the signal before any child is started.
	// The handler is armed before cbreak so a signal cannot catch the terminal
	// mid-change. The signal is queued in caught before cancel runs, so a
	// cancelled context always has its signal ready to read.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	caught := make(chan os.Signal, 1)
	go func() {
		select {
		case sig := <-sigs:
			caught <- sig
			cancel()
		case <-ctx.Done():
		}
	}()
	// FR-041: the fill feeds the seven-row Download view from the engine's
	// event stream. The view is created only when the live surface is allowed
	// (TTY, no -v, not plain); the thin sink still records completions for
	// the child's parser either way.
	var view *downloadEventView
	if mode := st.liveMode(); mode != liveOff {
		live := newLiveProgress(st.stderr, st.colorErr, terminalWidth(st.stderr), mode,
			newProgressModel("download", targetsAsRows(targets), nil), st.theme)
		// xbpretty owns these transfers: the box and cbreak stay up until the
		// fill finishes or Ctrl-C cancels it (FR-072).
		inline := st.startInline(live)
		defer inline.Close()
		live.Request(true)
		view = &downloadEventView{live: live, written: map[string]int64{}}
		defer view.finish()
	}
	engineOpts.OnEvent = func(ev download.Event) {
		st.downloadSink.Event(ev)
		if view != nil {
			view.apply(ev)
		}
	}
	if st.r.verbose {
		engineOpts.Log = func(format string, args ...any) {
			fmt.Fprintf(st.stderr, "verbose: "+format+"\n", args...)
		}
	}
	engine := download.New(engineOpts)

	var results []download.Result
	if failClosed {
		// FR-038/ADR-0014: an explicit download aborts on the first exhausted
		// file or hash mismatch. One target at a time keeps "first" literal
		// and guarantees that every file verified before it stays published;
		// there is no child to cover the remaining transfers.
		for _, target := range targets {
			if ctx.Err() != nil {
				break
			}
			result := engine.FetchOne(ctx, target)
			results = append(results, result)
			if result.Err != nil {
				break
			}
		}
	} else {
		results = engine.Fetch(ctx, targets)
	}
	if ctx.Err() != nil {
		if view != nil {
			view.finish()
		}
		st.errorf("%s\n", "interrupted")
		outcome.interrupted = true
		select {
		case outcome.signal = <-caught:
		default:
		}
		return outcome
	}
	// FR-041: on success clear; on failure clear before the per-file errors.
	if view != nil {
		view.finish()
	}

	outcome.attempted = len(results)
	for _, result := range results {
		if result.Err == nil {
			continue
		}
		outcome.failed++
		if failClosed {
			st.errorf("%v\n", result.Err)
		}
	}
	return outcome
}

// downloadEventView adapts the engine's FR-041 events to the Download view
// model. It tracks cumulative bytes per file because the engine reports the
// size of each write, while the model wants the file's own percentage.
type downloadEventView struct {
	live    *liveProgress
	written map[string]int64
}

func (v *downloadEventView) apply(ev download.Event) {
	switch ev.Kind {
	case download.EventFileTick:
		if strings.HasSuffix(ev.File, ".sig2") {
			return
		}
		v.written[ev.File] += ev.Bytes
		percent := 0
		if ev.Total > 0 {
			percent = int(float64(v.written[ev.File]) / float64(ev.Total) * 100)
			if percent > 100 {
				percent = 100
			}
		}
		v.live.Apply(false, func(m *progressModel) {
			m.downloadTick(DownloadTick{Filename: ev.File, FilePercent: percent})
		})
	case download.EventCacheHit, download.EventFileDone:
		v.live.Apply(true, func(m *progressModel) {
			m.downloadDone(DownloadComplete{Filename: ev.File})
		})
	case download.EventFileStart, download.EventRetry:
		// A new attempt starts the archive's byte count again (BF-20).
		v.written[ev.File] = 0
		v.live.Request(true)
	case download.EventFileFailed:
		// A failed file gives back its in-flight bytes, and its next attempt
		// starts from zero (BF-20).
		v.written[ev.File] = 0
		v.live.Apply(true, func(m *progressModel) {
			m.clearInFlight(ev.File)
		})
	case download.EventFinished:
		v.live.Request(true)
	}
}

func (v *downloadEventView) finish() {
	v.live.Finish()
}

// targetsAsRows turns the fill's targets back into the row shape the progress
// model seeds itself from: the exact plan bytes are what the view counts.
func targetsAsRows(targets []download.Target) []xbps.Row {
	rows := make([]xbps.Row, 0, len(targets))
	for _, target := range targets {
		name, version := xbps.SplitPkgver(target.Pkgver)
		if name == "" {
			name = target.Package
		}
		rows = append(rows, xbps.Row{
			Name:         name,
			Version:      version,
			Action:       xbps.ActionInstall,
			Arch:         target.Arch,
			Repo:         target.Repository,
			DownloadSize: target.Size,
			HasDownload:  true,
		})
	}
	return rows
}

// predownloadDisabledByPassthru applies FR-038's passthrough rule: `-c` or
// `-r` sends xbps to a cache or root xbpretty did not model, so the fill is
// silently off apart from the verbose line.
func (st *state) predownloadDisabledByPassthru() bool {
	if st.opts == nil {
		return false
	}
	for _, token := range st.opts.passthru {
		// xbps spells the pair both ways: -c/-r and --cachedir/--rootdir,
		// with the directory either as the next argument or after `=`.
		switch {
		case token == "--cachedir", strings.HasPrefix(token, "--cachedir="),
			token == "--rootdir", strings.HasPrefix(token, "--rootdir="):
			return true
		}
		if len(token) < 2 || token[0] != '-' || strings.HasPrefix(token, "--") {
			continue
		}
		if strings.ContainsAny(token[1:], "cr") {
			return true
		}
	}
	return false
}

// prepareCacheDir makes the cache directory usable and proves it is writable,
// so the metadata pass never runs for a cache that could not receive a file.
func prepareCacheDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".xbpretty-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

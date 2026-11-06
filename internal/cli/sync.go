package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
	"github.com/soubarnak/xbpretty/internal/hints"
	"github.com/soubarnak/xbpretty/internal/summary"
)

// repoStatus is one repository's outcome in the sync (BF-03).
type repoStatus int

const (
	repoNoChange repoStatus = iota
	repoUpdated
	repoError
)

// syncTracker turns the sync's repository events into nala's per-repository
// lines (BF-03): `No Change:`, `Updated: <desc> [<size>]`, and `Error:` with the
// reason on its own line. It is fed from the stdout and stderr readers, so it
// is locked.
type syncTracker struct {
	mu    sync.Mutex
	theme summary.Theme
	color bool
	emit  func(line string)

	active  bool
	current string
	status  repoStatus
	size    int64
	errText string

	failedRepos  int
	fetchedRepos int
	fetchedBytes int64
}

func newSyncTracker(theme summary.Theme, color bool, emit func(line string)) *syncTracker {
	return &syncTracker{theme: theme, color: color, emit: emit}
}

// start begins a repository. The previous one is finished first, so its line
// comes out before this one's.
func (t *syncTracker) start(url string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.finishLocked()
	t.begin(repoDescription(url))
}

func (t *syncTracker) begin(desc string) {
	t.active = true
	t.current = desc
	t.status = repoNoChange
	t.size = 0
	t.errText = ""
}

// fetched marks the current repository Updated with the size its repodata
// transfer completed at.
func (t *syncTracker) fetched(size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fetchedBytes += size
	if !t.active {
		// A completion with no repository in front of it has no line of its
		// own; it still counts toward the Fetched line.
		t.fetchedRepos++
		return
	}
	if t.status != repoUpdated {
		t.fetchedRepos++
	}
	t.status = repoUpdated
	t.size += size
}

// failed marks a repository Error with xbps's reason. A failure for a URL that
// is not the current repository starts that repository first.
func (t *syncTracker) failed(url, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	desc := repoDescription(url)
	if !t.active || t.current != desc {
		t.finishLocked()
		t.begin(desc)
	}
	t.status = repoError
	t.errText = reason
	t.failedRepos++
}

// finish emits the last repository's line at the end of the sync.
func (t *syncTracker) finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.finishLocked()
}

// finishLocked renders the active repository's line or lines.
func (t *syncTracker) finishLocked() {
	if !t.active {
		return
	}
	t.active = false
	switch t.status {
	case repoUpdated:
		line := paintRole(t.theme, t.color, roleSecondary, "Updated:") + " " + t.current
		if t.size > 0 {
			line += " [" + summary.WidgetSize(uint64(t.size)) + "]"
		}
		t.emit(line)
	case repoError:
		t.emit(paintRole(t.theme, t.color, roleError, "Error:") + " " + t.current)
		if t.errText != "" {
			t.emit(t.errText)
		}
	default:
		t.emit(paintRole(t.theme, t.color, rolePrimary, "No Change:") + " " + t.current)
	}
}

// completion is the line after a successful sync (BF-03, nala's progress-fetched
// and progress-nothing): `Fetched <size> in <time> (<rate>/s)` when a repository
// was fetched, else `Nothing to fetch`. Highlight carries it.
func (t *syncTracker) completion(elapsed time.Duration) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failedRepos > 0 && t.fetchedRepos == 0 {
		// xbps exits 0 when every repository fails; claiming "Nothing to fetch"
		// after Error lines would be wrong.
		return ""
	}
	text := "Nothing to fetch"
	if t.fetchedRepos > 0 {
		seconds := elapsed.Seconds()
		rate := ceilRate(uint64(t.fetchedBytes), seconds)
		text = "Fetched " + summary.WidgetSize(uint64(t.fetchedBytes)) +
			" in " + widgetTime(ceilSeconds(seconds)) +
			" (" + summary.WidgetSize(rate) + "/s)"
	}
	return paintRole(t.theme, t.color, roleHighlight, text)
}

// repoDescription is the repository as nala names it: the base URL and the
// arch-specific index file, separated by a space (BF-03). xbps's start line
// ends its URL in `/<arch>-repodata`.
func repoDescription(url string) string {
	i := strings.LastIndex(url, "/")
	if i < 0 || !isRepodataFile(url[i+1:]) {
		return url
	}
	return url[:i] + " " + url[i+1:]
}

// updateCountLine is nala's count line, plural-aware, with the command in
// Primary (BF-03, update-upgradable).
func updateCountLine(count int, color bool, theme summary.Theme) string {
	command := paintRole(theme, color, rolePrimary, "xbp list --upgradable")
	if count == 1 {
		return "1 package can be upgraded. Run '" + command + "' to see it."
	}
	return fmt.Sprintf("%d packages can be upgraded. Run '%s' to see them.", count, command)
}

// keyImportStartRegex matches the first line of xbps's new-key block: the
// signer line, or the fingerprint line (BF-02).
var keyImportStartRegex = regexp.MustCompile(`has been RSA signed by|^Fingerprint: `)

// runSync is the one repository sync (FR-013) for update, the upgrade family and
// --update. It returns -1 when the caller may continue, and otherwise xbps's
// code, with the captured output for the hint table.
//
// The live view is gated on liveMode alone, never on the command name (BF-02):
// a live terminal gets nala's Updating Package Lists widget, while a redirected
// or verbose run forwards xbps's raw lines as FR-013 always did. The caller
// prints the hint for a non-zero code.
func (st *state) runSync(caller string) (int, hints.Output, error) {
	_ = caller
	mode := st.liveMode()
	if mode == liveOff {
		code, output, err := st.r.streamStderr(st.r.cfg.Xbps.InstallPath, "-S")
		if err != nil {
			return exitRuntime, output, err
		}
		if code != 0 {
			return code, output, nil
		}
		return -1, output, nil
	}

	started := time.Now()
	model := newProgressModel("update", nil, nil)
	model.setRepositoryTotal(st.repositoryTotal())
	live := newLiveProgress(st.stderr, st.colorErr, terminalWidth(st.stderr), mode, model, st.theme)
	// The status lines go above the box as each repository finishes (BF-03).
	tracker := newSyncTracker(st.theme, st.colorErr, live.InsertAbove)
	parser := NewStreamParser(0, 0)
	parser.OnRepoUpdate = func(url string) {
		live.Apply(true, func(m *progressModel) { m.repository(url) })
		tracker.start(url)
	}
	parser.OnRepoTick = func(tick DownloadTick) {
		live.Apply(false, func(m *progressModel) { m.repoTick(tick) })
	}
	parser.OnRepoFetched = func(file string, size int64) {
		live.Apply(true, func(m *progressModel) { m.repoFetched(file, size) })
		tracker.fetched(size)
	}
	parser.OnRepoError = func(url, reason string) {
		tracker.failed(url, reason)
	}
	st.r.parser = parser
	st.r.live = true
	st.r.liveProgress = live
	live.Request(true)

	code, err := st.r.stream(st.r.cfg.Xbps.InstallPath, "-S")
	live.Apply(true, func(m *progressModel) { m.repositoriesFinished() })
	tracker.finish()
	// Clear the widget before anything captured is printed (FR-064): the
	// teardown and the failure dump must not interleave.
	st.endLive(live)
	st.r.parser = nil
	output := hints.Output{Stdout: st.r.lastStdout, Stderr: st.r.lastStderr}
	if err != nil {
		st.printCaptured(output)
		return exitRuntime, output, err
	}
	if code != 0 {
		st.printCaptured(output)
		return code, output, nil
	}
	if line := tracker.completion(time.Since(started)); line != "" {
		fmt.Fprintln(st.stderr, line)
	}
	return -1, output, nil
}

// printCaptured prints a failed child's captured output on stderr, stdout first,
// after the live view cleared (FR-064, FR-066). A line the live view already
// showed above its box is skipped, so it prints once (BF-16).
func (st *state) printCaptured(output hints.Output) {
	shown := st.r.shownCopy()
	printUnshownCaptured(st.stderr, output.Stdout, shown)
	printUnshownCaptured(st.stderr, output.Stderr, shown)
}

// printUnshownCaptured writes captured child output, leaving out each line that
// shown still counts. A shown line is matched once, so a repeated line prints
// its extra copies.
func printUnshownCaptured(w io.Writer, captured string, shown map[string]int) {
	for _, part := range strings.SplitAfter(captured, "\n") {
		if part == "" {
			continue
		}
		if text := cleanLine(part); text != "" && shown[text] > 0 {
			shown[text]--
			continue
		}
		fmt.Fprint(w, part)
	}
}

// keyImportGate tracks xbps's new-key block on the stdout stream. While the
// block lasts the stream is raw: the surface is suspended and the child's bytes
// reach the terminal unboxed, so the prompt can be answered (BF-02). The block
// ends at the next repository line. A nil gate is a no-op.
type keyImportGate struct {
	live *liveProgress
	raw  bool
}

// newKeyImportGate returns the gate for a live stream, or nil without one.
func newKeyImportGate(live *liveProgress) *keyImportGate {
	if live == nil {
		return nil
	}
	return &keyImportGate{live: live}
}

// isRaw reports whether the stream is inside the key-import block.
func (g *keyImportGate) isRaw() bool {
	return g != nil && g.raw
}

// observe applies one complete stdout line to the gate.
func (g *keyImportGate) observe(line string) {
	if g == nil {
		return
	}
	text := cleanLine(line)
	switch {
	case !g.raw && keyImportStartRegex.MatchString(text):
		g.raw = true
		g.live.Suspend()
	case g.raw && repoSyncRegex.MatchString(text):
		g.raw = false
		g.live.Resume()
	}
}

// finish ends a block the stream closed inside, so the box can be torn down.
func (g *keyImportGate) finish() {
	if g != nil && g.raw {
		g.raw = false
		g.live.Resume()
	}
}

// repositoryTotal is the number of remote repositories `xbps-install -S` will
// sync: the Updating view's denominator (BF-04). xbps-query -L lists exactly the
// repositories xbps uses, after /etc has masked /usr/share by basename and with
// any -R/-r/-C passthrough applied. When that call gives no remote repository,
// the count comes from a scan of every xbps.d .conf file. A zero result means
// the total is unknown and the view shows `n/?`.
func (st *state) repositoryTotal() int {
	argv := []string{"-L"}
	if st.opts != nil {
		argv = append(argv, repoFlagArgs(st.opts.passthru)...)
	}
	if st.r.cfg.ResolveFor("query") == nil {
		res, err := st.r.run(st.r.cfg.Xbps.QueryPath, argv...)
		if err == nil && res.exitCode == 0 {
			if urls := remoteRepositoryURLs(res.stdout); len(urls) > 0 {
				return len(urls)
			}
		}
	}
	return len(fetch.AllRepositoryLines(fetchXBPSDDirs))
}

// repoFlagArgs keeps the passthrough arguments that choose a repository set:
// -R (repository), -r (rootdir) and -C (config), with their values. Anything
// else is an install-only option that xbps-query -L does not take.
func repoFlagArgs(passthru []string) []string {
	var out []string
	for i := 0; i < len(passthru); i++ {
		arg := passthru[i]
		switch {
		case arg == "-R" || arg == "-r" || arg == "-C":
			out = append(out, arg)
			if i+1 < len(passthru) {
				i++
				out = append(out, passthru[i])
			}
		case strings.HasPrefix(arg, "-R") || strings.HasPrefix(arg, "-r") || strings.HasPrefix(arg, "-C"):
			out = append(out, arg)
		}
	}
	return out
}

// remoteRepositoryURLs reads the URLs out of `xbps-query -L` output, one
// ` <n> <url> (<signature>)` row per repository. A row counts when its
// location has a scheme and is not file://; duplicates count once.
func remoteRepositoryURLs(listing string) []string {
	seen := map[string]bool{}
	var urls []string
	for _, line := range strings.Split(listing, "\n") {
		for _, field := range strings.Fields(line) {
			if !strings.Contains(field, "://") {
				continue
			}
			if !strings.HasPrefix(field, "file://") && !seen[field] {
				seen[field] = true
				urls = append(urls, field)
			}
			break
		}
	}
	return urls
}

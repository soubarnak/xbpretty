package cli

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// plainLineWidth is FR-065's fixed 40-cell bar.
const plainLineWidth = 40

// flushInterval is FR-041/FR-064's coalescing window: live redraws happen at
// most once per 150 ms, with an immediate redraw for completion, cache-hit,
// failure and phase-change events.
const flushInterval = 150 * time.Millisecond

// progressModel turns the xbps telemetry callbacks into the five fixed views.
// It is the only place the spec's formulas live. Every mutation goes through
// liveProgress.Apply, which holds the renderer's mutex: the stdout scanner and
// the stderr parser call it from different goroutines.
type progressModel struct {
	op    string
	start time.Time
	now   func() time.Time

	// Download state. archives maps an exact archive filename to its plan
	// bytes; byPkgver is the same data keyed by pkgver for streams whose
	// arch suffix this build does not know.
	archives       map[string]uint64
	byPkgver       map[string]uint64
	totalArchives  int
	totalBytes     uint64
	completed      map[string]bool
	completedBytes uint64
	completedCount int
	lastCompleted  string
	inFlight       map[string]uint64

	// inserts holds completed lines the renderer pushes above the box on the
	// next draw (FR-072, BF-05). The viewport or the in-place renderer places them.
	inserts []string

	// Install state: U = 2·A unpack units for install/update rows, one configure
	// unit for each configure-only row and one unit for each removal (BF-19).
	// unpacked, configured and removed are keyed by package name (BF-17).
	installTotal   int
	configureTotal int
	unpacked       map[string]bool
	configured     map[string]bool
	removed        map[string]bool
	removeTotal    int
	currentPkg     string
	status         string

	// Update and MirrorScore state. A repository counts as done when the next one
	// starts or at the end of the sync (BF-03), so reposDone never runs ahead.
	reposDone, reposTotal int
	reposTotalKnown       bool
	repoURL               string
	repoStarted           bool
	repoEnded             bool
	// repoSeen records the repodata transfers whose size already joined totalBytes.
	repoSeen      map[string]bool
	mirrorDone    int
	mirrorTotal   int
	mirrorURL     string
	mirrorElapsed time.Duration

	kind      progressKind
	verifying bool
}

// newProgressModel seeds the download counters from the exact plan bytes
// (never the humanised tick sizes) and chooses the first view for cmd.
func newProgressModel(op string, rows []xbps.Row, now func() time.Time) *progressModel {
	if now == nil {
		now = time.Now
	}
	m := &progressModel{
		op:         op,
		start:      now(),
		now:        now,
		archives:   map[string]uint64{},
		byPkgver:   map[string]uint64{},
		completed:  map[string]bool{},
		inFlight:   map[string]uint64{},
		unpacked:   map[string]bool{},
		configured: map[string]bool{},
		removed:    map[string]bool{},
		repoSeen:   map[string]bool{},
	}
	switch op {
	case "remove", "purge", "autoremove", "autopurge":
		m.kind = viewRemove
	case "update":
		m.kind = viewUpdate
	case "mirror", "fetch":
		m.kind = viewMirrorScore
	}
	for _, row := range rows {
		switch row.Action {
		case xbps.ActionRemove:
			m.removeTotal++
		case xbps.ActionConfigure:
			m.configureTotal++
		case xbps.ActionInstall, xbps.ActionUpdate, xbps.ActionReinstall:
			m.installTotal++
			pkgver := row.Pkgver()
			if _, seen := m.byPkgver[pkgver]; !seen {
				m.byPkgver[pkgver] = row.DownloadSize
				m.totalBytes += row.DownloadSize
				m.totalArchives++
			}
			if row.Arch != "" {
				m.archives[pkgver+"."+row.Arch+".xbps"] = row.DownloadSize
			}
		}
	}
	if m.kind != viewRemove && m.kind != viewUpdate && m.kind != viewMirrorScore {
		if m.totalArchives > 0 {
			m.kind = viewDownload
		} else {
			m.kind = viewInstall
		}
	}
	return m
}

// elapsed is the wall clock since the model started. Tests replace now.
func (m *progressModel) elapsed() time.Duration {
	return m.now().Sub(m.start)
}

func (m *progressModel) elapsedSeconds() float64 {
	return m.elapsed().Seconds()
}

// position is the sum of completed exact plan bytes and the in-flight
// archive's plan bytes times its own percent, clamped to total.
func (m *progressModel) position() uint64 {
	position := m.completedBytes
	for _, bytes := range m.inFlight {
		position += bytes
	}
	if m.totalBytes > 0 && position > m.totalBytes {
		position = m.totalBytes
	}
	return position
}

func (m *progressModel) downloadSpeed() uint64 {
	return ceilRate(m.position(), m.elapsedSeconds())
}

// archiveBytes resolves one telemetry filename to the plan's exact bytes.
// The exact pkgver.arch.xbps key wins; otherwise the pkgver alone matches,
// which also covers musl and future arch suffixes a build does not know.
func (m *progressModel) archiveBytes(filename string) (string, uint64, bool) {
	if bytes, ok := m.archives[filename]; ok {
		return filename, bytes, true
	}
	stem := strings.TrimSuffix(strings.TrimSuffix(filename, ".sig2"), ".xbps")
	if bytes, ok := m.byPkgver[stem]; ok {
		return stem, bytes, true
	}
	pkgver := packageNameFromFilename(filename)
	if bytes, ok := m.byPkgver[pkgver]; ok {
		return pkgver, bytes, true
	}
	return "", 0, false
}

// DownloadTick applies one live tick. A .sig2 signature file never moves the
// Packages, Data, Speed or Remaining counters.
func (m *progressModel) downloadTick(tick DownloadTick) {
	if strings.HasSuffix(tick.Filename, ".sig2") {
		return
	}
	key, size, ok := m.archiveBytes(tick.Filename)
	if !ok || m.completed[key] {
		return
	}
	m.inFlight[key] = uint64(float64(size) * float64(tick.FilePercent) / 100.0)
}

// downloadDone applies one archive completion. Completion lines are the cache
// hit too: an archive that finished without a tick still counts its exact plan
// bytes.
func (m *progressModel) downloadDone(done DownloadComplete) {
	if strings.HasSuffix(done.Filename, ".sig2") {
		return
	}
	key, size, ok := m.archiveBytes(done.Filename)
	if !ok || m.completed[key] {
		return
	}
	m.completed[key] = true
	m.completedBytes += size
	m.completedCount++
	m.lastCompleted = done.Filename
	m.inserts = append(m.inserts, "Last completed: "+done.Filename)
	delete(m.inFlight, key)
}

// removeOnly reports whether the plan has no unpack or configure work. Only a
// remove-only plan owns the Remove view; a plan that also installs or updates
// keeps the Install view and counts its removals there (BF-19).
func (m *progressModel) removeOnly() bool {
	return m.installTotal == 0 && m.configureTotal == 0
}

// actionKind is the view an unpack, configure or removal step draws.
func (m *progressModel) actionKind() progressKind {
	if m.removeOnly() {
		return viewRemove
	}
	return viewInstall
}

// eventKey is the package name an event belongs to. xbps prints the old pkgver
// on an update and the new pkgver on its unpack, so counters key on the name
// and count the package once (BF-17).
func eventKey(pkgver string) string {
	if name, _ := xbps.SplitPkgver(pkgver); name != "" {
		return name
	}
	return pkgver
}

// phase applies an xbps phase header. Verification snaps the Download view to
// 100% for exactly one rendered frame; the following phase switches views.
func (m *progressModel) phase(phase string) {
	switch phase {
	case "Verifying":
		if m.kind == viewDownload {
			m.verifying = true
		}
	case "Unpacking", "Configuring", "Collecting":
		if m.kind == viewDownload {
			m.kind = m.actionKind()
		}
	}
}

// markCached records an archive the predownload fill finished before the commit
// child started (BF-18). It counts like a completion but queues no scrollback
// line, and a plan whose every archive is cached starts on the Install view.
func (m *progressModel) markCached(filename string) {
	if strings.HasSuffix(filename, ".sig2") {
		return
	}
	key, size, ok := m.archiveBytes(filename)
	if !ok || m.completed[key] {
		return
	}
	m.completed[key] = true
	m.completedBytes += size
	m.completedCount++
	m.lastCompleted = filename
	if m.totalArchives > 0 && m.completedCount == m.totalArchives {
		m.kind = viewInstall
	}
}

// clearInFlight drops one archive's in-flight bytes when its transfer fails, so
// a dead attempt leaves the position (BF-20).
func (m *progressModel) clearInFlight(filename string) {
	if key, _, ok := m.archiveBytes(filename); ok {
		delete(m.inFlight, key)
	}
}

func (m *progressModel) packageEvent(event PackageEvent) {
	key := eventKey(event.Package)
	switch event.Action {
	case "unpacking", "updating":
		m.kind = viewInstall
		m.currentPkg = event.Package
		if event.Action == "updating" && event.Details != "" {
			// The status names the version being installed, not the old pkgver.
			m.currentPkg = key + "-" + event.Details
		}
		m.status = "Unpacking"
		m.unpacked[key] = true
	case "configuring":
		m.kind = viewInstall
		m.currentPkg = event.Package
		m.status = "Configuring"
		m.configured[key] = true
	case "removing":
		m.kind = m.actionKind()
		m.currentPkg = event.Package
		m.status = "Removing"
	case "removed":
		m.kind = m.actionKind()
		m.removed[key] = true
	}
	// A finished package action is a completed child line; the viewport
	// inserts it into scrollback above the box (FR-072).
	switch event.Action {
	case "installed", "updated", "configured", "removed":
		m.inserts = append(m.inserts, event.Package+": "+event.Action+" successfully.")
	}
}

// repository records one `[*] Updating repository '<url>' ...` line. The
// previous repository is finished now (BF-03): nala counts a repository done
// when the next one starts, not when it starts itself, so the first start
// shows 0/N.
func (m *progressModel) repository(url string) {
	if m.repoStarted {
		m.reposDone++
	}
	m.repoStarted = true
	m.repoURL = url
}

// repositoriesFinished counts the last repository done at the end of the sync.
func (m *progressModel) repositoriesFinished() {
	if m.repoStarted && !m.repoEnded {
		m.reposDone++
		m.repoEnded = true
	}
}

// countRepoTransfer adds a repodata transfer's size to the Data total the first
// time it is seen. nala's total grows as items start.
func (m *progressModel) countRepoTransfer(file string, size uint64) {
	if m.repoSeen[file] {
		return
	}
	m.repoSeen[file] = true
	m.totalBytes += size
}

// repoTick applies one repodata transfer tick to the Updating view.
func (m *progressModel) repoTick(tick DownloadTick) {
	if m.completed[tick.Filename] {
		return
	}
	m.countRepoTransfer(tick.Filename, uint64(tick.TotalSizeBytes))
	m.inFlight[tick.Filename] = uint64(tick.DownloadedBytes)
}

// repoFetched applies one repodata completion: the file's exact size is done.
func (m *progressModel) repoFetched(file string, size int64) {
	if m.completed[file] {
		return
	}
	m.completed[file] = true
	m.countRepoTransfer(file, uint64(size))
	m.completedBytes += uint64(size)
	m.completedCount++
	delete(m.inFlight, file)
}

// setRepositoryTotal seeds the repository count from xbps-query -L. Until it is
// known the ratio follows the bytes and the plain line shows `n/?`.
func (m *progressModel) setRepositoryTotal(total int) {
	if total > 0 {
		m.reposTotal = total
		m.reposTotalKnown = true
	}
}

func (m *progressModel) mirrorEvent(finished string, done, total int, elapsed time.Duration) {
	m.kind = viewMirrorScore
	m.mirrorDone = done
	m.mirrorTotal = total
	if finished != "" {
		m.mirrorURL = finished
	}
	m.mirrorElapsed = elapsed
}

func clampRatio(ratio float64) float64 {
	if math.IsNaN(ratio) || ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// ratio is the fraction each view draws. Install is the two-unit model:
// (unpacked + configured + removed) / (2·A + C + R). A remove-only plan has
// A = C = 0, so the same formula gives removed / R for the Remove view.
func (m *progressModel) ratio() float64 {
	switch m.kind {
	case viewDownload:
		if m.totalBytes == 0 {
			return 0
		}
		return clampRatio(float64(m.position()) / float64(m.totalBytes))
	case viewInstall, viewRemove:
		denominator := 2*m.installTotal + m.configureTotal + m.removeTotal
		if denominator == 0 {
			return 0
		}
		done := len(m.unpacked) + len(m.configured) + len(m.removed)
		return clampRatio(float64(done) / float64(denominator))
	case viewUpdate:
		// The bar follows the repositories when xbps-query -L named them, and
		// the transferred bytes otherwise (BF-03, BF-04).
		if m.reposTotalKnown {
			return clampRatio(float64(m.reposDone) / float64(m.reposTotal))
		}
		if m.totalBytes == 0 {
			return 0
		}
		return clampRatio(float64(m.position()) / float64(m.totalBytes))
	case viewMirrorScore:
		if m.mirrorTotal == 0 {
			return 0
		}
		return clampRatio(float64(m.mirrorDone) / float64(m.mirrorTotal))
	}
	return 0
}

func (m *progressModel) elapsedText() string {
	return widgetTime(ceilSeconds(m.elapsedSeconds()))
}

// view builds the renderable frame. It consumes the one verification frame:
// the first call after a Verifying header draws the Download view at 100%,
// and later calls draw the Install view.
func (m *progressModel) view() progressView {
	if m.verifying {
		m.verifying = false
		m.kind = m.actionKind()
		return progressView{
			kind:      viewDownload,
			msgHeader: "Last completed",
			msgText:   m.lastCompleted,
			msgRole:   rolePrimary,
			left:      []progressInfo{{"Packages", pairCount(m.totalArchives, m.totalArchives)}, {"Remaining", "0s"}},
			right: []progressInfo{
				{"Data", summary.WidgetSize(m.totalBytes) + "/" + summary.WidgetSize(m.totalBytes)},
				{"Speed", summary.WidgetSize(m.downloadSpeed()) + "/s"},
			},
			ratio: 1,
		}
	}
	switch m.kind {
	case viewDownload:
		position := m.position()
		rate := m.downloadSpeed()
		remaining := "—"
		if eta, ok := etaSeconds(position, m.totalBytes, rate); ok {
			remaining = widgetTime(eta)
		}
		return progressView{
			kind:      viewDownload,
			msgHeader: "Last completed",
			msgText:   m.lastCompleted,
			msgRole:   rolePrimary,
			left: []progressInfo{
				{"Packages", pairCount(m.completedCount, m.totalArchives)},
				{"Remaining", remaining},
			},
			right: []progressInfo{
				{"Data", summary.WidgetSize(position) + "/" + summary.WidgetSize(m.totalBytes)},
				{"Speed", summary.WidgetSize(rate) + "/s"},
			},
			ratio: m.ratio(),
		}
	case viewInstall:
		return progressView{
			kind:      viewInstall,
			msgHeader: "Status",
			msgText:   strings.TrimSpace(m.status + " " + m.currentPkg),
			msgRole:   rolePrimary,
			left:      []progressInfo{{"Package", m.currentPkg}},
			right:     []progressInfo{{"Elapsed", m.elapsedText()}},
			ratio:     m.ratio(),
		}
	case viewRemove:
		return progressView{
			kind:      viewRemove,
			msgHeader: "Status",
			msgText:   strings.TrimSpace(m.status + " " + m.currentPkg),
			msgRole:   rolePrimary,
			left:      []progressInfo{{"Package", m.currentPkg}},
			right:     []progressInfo{{"Elapsed", m.elapsedText()}},
			ratio:     m.ratio(),
		}
	case viewUpdate:
		// nala's Updating view (BF-03): the repodata transfer in progress, its
		// Data and Remaining, and Speed and Elapsed. The repository count lives
		// in the plain line only.
		position := m.position()
		rate := m.downloadSpeed()
		remaining := "—"
		if eta, ok := etaSeconds(position, m.totalBytes, rate); ok {
			remaining = widgetTime(eta)
		}
		return progressView{
			kind:      viewUpdate,
			msgHeader: "Downloading",
			msgText:   m.repoURL,
			msgRole:   roleRegular,
			left: []progressInfo{
				{"Data", summary.WidgetSize(position) + "/" + summary.WidgetSize(m.totalBytes)},
				{"Remaining", remaining},
			},
			right: []progressInfo{
				{"Speed", summary.WidgetSize(rate) + "/s"},
				{"Elapsed", m.elapsedText()},
			},
			ratio: m.ratio(),
		}
	case viewMirrorScore:
		return progressView{
			kind:      viewMirrorScore,
			msgHeader: "Finished",
			msgText:   m.mirrorURL,
			msgRole:   rolePrimary,
			left:      []progressInfo{{"Mirrors", pairCount(m.mirrorDone, m.mirrorTotal)}},
			right:     []progressInfo{{"Elapsed", widgetTime(ceilSeconds(m.mirrorElapsed.Seconds()))}},
			ratio:     m.ratio(),
		}
	}
	return progressView{kind: m.kind, msgHeader: "Status", msgText: "Working...", msgRole: rolePrimary, ratio: m.ratio()}
}

// plainState is the 40-cell fallback's data. Its grammar is byte-for-byte
// nala's PlainProgress line (FR-065).
type plainState struct {
	ratio       float64
	message     string
	hasTransfer bool
	position    uint64
	total       uint64
	rate        uint64
	hasItem     bool
	item        string
	hasItems    bool
	itemsDone   int
	itemsTotal  int
	hasRepos    bool
	reposDone   int
	reposTotal  int
	hasMirrors  bool
	mirrorDone  int
	mirrorTotal int
}

func (m *progressModel) plain() plainState {
	state := plainState{ratio: m.ratio()}
	switch m.kind {
	case viewDownload:
		state.message = "Working..."
		if m.lastCompleted != "" {
			state.message = "Last completed: " + m.lastCompleted
		}
		position := m.position()
		rate := m.downloadSpeed()
		state.hasTransfer = true
		state.position = position
		state.total = m.totalBytes
		state.rate = rate
		if m.totalArchives > 0 {
			state.hasItems = true
			state.itemsDone = m.completedCount
			state.itemsTotal = m.totalArchives
		}
	case viewInstall, viewRemove:
		body := strings.TrimSpace(m.status + " " + m.currentPkg)
		state.message = "Working..."
		if body != "" {
			state.message = "Status: " + body
		}
		if m.currentPkg != "" {
			state.hasItem = true
			state.item = m.currentPkg
		}
	case viewUpdate:
		state.message = "Working..."
		if m.repoURL != "" {
			state.message = "Updating: " + m.repoURL
			state.hasRepos = true
			state.reposDone = m.reposDone
			state.reposTotal = m.reposTotal
		}
	case viewMirrorScore:
		state.message = "Working..."
		if m.mirrorURL != "" {
			state.message = "Finished: " + m.mirrorURL
		}
		if m.mirrorDone > 0 {
			state.hasMirrors = true
			state.mirrorDone = m.mirrorDone
			state.mirrorTotal = m.mirrorTotal
		}
	}
	if state.message == "" {
		state.message = "Working..."
	}
	return state
}

// repositoryPair is `done/total`, or `done/?` while xbps-query -L has not named
// the total (BF-03, BF-04).
func repositoryPair(done, total int) string {
	if total <= 0 {
		return fmt.Sprintf("%d/?", done)
	}
	return pairCount(done, total)
}

// renderPlainLine reproduces nala's PlainProgress grammar: a 40-cell bar of
// `=`, the truncated `%3d` percent, the message, then the view's filters.
func renderPlainLine(state plainState) string {
	filled := widgetFill(plainLineWidth, state.ratio)
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(strings.Repeat("=", filled))
	b.WriteString(strings.Repeat(" ", plainLineWidth-filled))
	b.WriteString("] ")
	b.WriteString(fmt.Sprintf("%3d", widgetPercent(state.ratio)))
	b.WriteString("% ")
	b.WriteString(state.message)
	if state.hasTransfer {
		b.WriteString(" " + summary.WidgetSize(state.position) + "/" + summary.WidgetSize(state.total))
		b.WriteString(" " + summary.WidgetSize(state.rate) + "/s")
	}
	if state.hasItem {
		b.WriteString(" | Package: " + state.item)
	}
	if state.hasItems {
		b.WriteString(" | Packages: " + pairCount(state.itemsDone, state.itemsTotal))
	}
	if state.hasRepos {
		b.WriteString(" | Repositories: " + repositoryPair(state.reposDone, state.reposTotal))
	}
	if state.hasMirrors {
		b.WriteString(" | Mirrors: " + pairCount(state.mirrorDone, state.mirrorTotal))
	}
	return b.String()
}

// liveMode is the suppression matrix's outcome for one execution.
type liveMode int

const (
	liveOff liveMode = iota
	liveWidgetMode
	livePlainMode
)

// liveProgress draws either the rounded widget or the 40-cell line, in place,
// at most once per flushInterval unless an event forces a redraw.
type liveProgress struct {
	out    io.Writer
	color  bool
	mode   liveMode
	width  int
	theme  summary.Theme
	model  *progressModel
	mu     sync.Mutex
	drawn  int
	closed bool

	// Inline viewport (FR-072). viewport is set only for the widget; the
	// plain line keeps its 40-cell in-place form. When guard is set the
	// latch leaves cbreak for the duration of a child's alternate screen.
	viewport *tui.Viewport
	guard    *term.Guard
	cbreak   bool
	latch    tui.AltScreenLatch

	// suspended is set while a child owns the terminal below the box (BF-02's
	// key-import prompt). Nothing draws until Resume.
	suspended bool

	lastDraw time.Time
	timer    *time.Timer
	now      func() time.Time
	after    func(time.Duration, func()) *time.Timer
}

// newLiveProgress constructs the renderer. Width is clamped by the renderer
// through widgetWidth.
func newLiveProgress(out io.Writer, color bool, width int, mode liveMode, model *progressModel, theme summary.Theme) *liveProgress {
	return &liveProgress{
		out:   out,
		color: color && mode == liveWidgetMode,
		mode:  mode,
		width: width,
		theme: theme,
		model: model,
		now:   time.Now,
		after: time.AfterFunc,
	}
}

// SetInline attaches the inline viewport that replaces the renderer's own
// in-place drawing (FR-072). guard is optional: it is the cbreak session the
// child's `?1049h` latch must leave and later re-enter.
func (l *liveProgress) SetInline(vp *tui.Viewport, guard *term.Guard, cbreak bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.viewport = vp
	l.guard = guard
	l.cbreak = cbreak
}

// SetCbreak records whether cbreak is currently held. The commit child runs
// with the user's termios, so its `?1049l` must not re-enter cbreak.
func (l *liveProgress) SetCbreak(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cbreak = on
}

// ObserveChildOutput feeds one chunk of child output to the two-state
// alt-screen latch. On `?1049h` the box is hidden and termios restored; on
// `?1049l` cbreak is re-entered and the box redrawn on the same tick.
func (l *liveProgress) ObserveChildOutput(chunk string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer l.guardRenderPanic()
	if l.viewport == nil || l.closed {
		return
	}
	hide, show := l.latch.Observe(chunk)
	if hide {
		l.viewport.Hide()
		if l.guard != nil && l.cbreak {
			l.guard.RestoreTermios()
		}
	}
	if show {
		if l.guard != nil && l.cbreak {
			_ = l.guard.EnterInline()
		}
		l.viewport.Show(l.renderLinesLocked())
	}
}

// Apply serializes one model mutation with the renderer. The stdout scanner
// and the stderr parser call it from different goroutines, so every model
// access must go through here; drawLocked already holds the same lock.
func (l *liveProgress) Apply(force bool, update func(*progressModel)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer l.guardRenderPanic()
	if l.closed {
		return
	}
	update(l.model)
	if l.flushInsertsLocked() {
		// A completion always repaints: the box must re-anchor below the
		// line that was just pushed into scrollback.
		force = true
	}
	l.requestLocked(force)
}

// SetWidth applies a SIGWINCH width change; the next redraw uses it. The box
// reflows horizontally and keeps its fixed view height.
func (l *liveProgress) SetWidth(columns int) {
	if columns <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	defer l.guardRenderPanic()
	l.width = columns
	if l.viewport != nil {
		l.viewport.SetWidth(columns)
	}
	if !l.closed && l.drawn > 0 {
		l.drawLocked()
	}
}

// Request redraws now on a forced event, otherwise coalesces to one draw per
// flushInterval.
func (l *liveProgress) Request(force bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requestLocked(force)
}

func (l *liveProgress) requestLocked(force bool) {
	if l.closed {
		return
	}
	now := l.now()
	if !force && l.drawn > 0 && now.Sub(l.lastDraw) < flushInterval {
		if l.timer == nil {
			wait := flushInterval - now.Sub(l.lastDraw)
			l.timer = l.after(wait, func() {
				l.mu.Lock()
				defer l.mu.Unlock()
				l.timer = nil
				l.drawLocked()
			})
		}
		return
	}
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	l.drawLocked()
}

func (l *liveProgress) drawLocked() {
	defer l.guardRenderPanic()
	if l.closed || l.suspended {
		return
	}
	if l.mode == livePlainMode {
		fmt.Fprintf(l.out, "\r\x1b[2K%s", renderPlainLine(l.model.plain()))
		l.drawn = 1
		l.lastDraw = l.now()
		return
	}
	lines := l.renderLinesLocked()
	if l.viewport != nil {
		if len(lines) > 0 {
			l.viewport.Draw(lines)
		}
		l.drawn = len(lines)
		l.lastDraw = l.now()
		return
	}
	if l.drawn == 0 {
		fmt.Fprint(l.out, strings.Join(lines, "\n"))
	} else {
		var b strings.Builder
		b.WriteString("\r")
		if l.drawn > 1 {
			fmt.Fprintf(&b, "\x1b[%dA", l.drawn-1)
		}
		for i, line := range lines {
			b.WriteString("\x1b[K")
			b.WriteString(line)
			if i < len(lines)-1 {
				b.WriteByte('\n')
			}
		}
		b.WriteString("\x1b[J")
		fmt.Fprint(l.out, b.String())
	}
	l.drawn = len(lines)
	l.lastDraw = l.now()
}

// guardRenderPanic restores the terminal before a render panic is re-raised:
// a broken draw must never leave the inline box painted or cbreak held. It is
// also called from the deferred coalescing timer, which has no caller to
// unwind through.
func (l *liveProgress) guardRenderPanic() {
	r := recover()
	if r == nil {
		return
	}
	if l.viewport != nil {
		l.viewport.Teardown()
	}
	if l.guard != nil {
		l.guard.Restore()
	}
	panic(r)
}

// renderLinesLocked renders the current frame with the configured theme.
func (l *liveProgress) renderLinesLocked() []string {
	return renderProgressViewThemed(l.model.view(), l.width, l.color, l.theme)
}

// flushInsertsLocked writes every queued line above the box and reports whether
// the caller must redraw. With an inline viewport the viewport places the lines
// (FR-072). Without one the renderer does it itself (BF-05): the in-place box
// is erased, the lines are written, and the caller redraws the box below them.
func (l *liveProgress) flushInsertsLocked() bool {
	if len(l.model.inserts) == 0 {
		return false
	}
	lines := l.model.inserts
	l.model.inserts = nil
	if l.viewport != nil {
		for _, line := range lines {
			l.viewport.Insert(line)
		}
		return true
	}
	if l.mode == livePlainMode {
		// The plain line is one row: overwrite it with each line, which leaves
		// the cursor on a fresh row for the caller's redraw.
		for _, line := range lines {
			fmt.Fprintf(l.out, "\r\x1b[2K%s\n", line)
		}
		return true
	}
	var b strings.Builder
	if l.drawn > 0 {
		b.WriteString("\r")
		if l.drawn > 1 {
			fmt.Fprintf(&b, "\x1b[%dA", l.drawn-1)
		}
		b.WriteString("\x1b[J")
		l.drawn = 0
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	fmt.Fprint(l.out, b.String())
	return true
}

// InsertAbove queues one finished child line for the scrollback above the box
// and redraws it below (FR-072, BF-05). It does nothing once the surface has
// finished, because no box is left to place the line against.
func (l *liveProgress) InsertAbove(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer l.guardRenderPanic()
	if l.closed {
		return
	}
	l.model.inserts = append(l.model.inserts, line)
	l.flushInsertsLocked()
	l.requestLocked(true)
}

// Finish tears the live surface down. Success clears without any completion
// line (FR-064); failure also clears, leaving the caller to print the captured
// child output. Finish is idempotent so a deferred safety call is harmless.
func (l *liveProgress) Finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	if l.viewport != nil {
		l.viewport.Teardown()
		l.drawn = 0
		return
	}
	l.eraseLocked()
}

// eraseLocked clears the in-place box (or plain line) it drew last.
func (l *liveProgress) eraseLocked() {
	if l.drawn == 0 {
		return
	}
	if l.mode == livePlainMode {
		fmt.Fprint(l.out, "\r\x1b[2K")
	} else {
		var b strings.Builder
		b.WriteString("\r")
		if l.drawn > 1 {
			fmt.Fprintf(&b, "\x1b[%dA", l.drawn-1)
		}
		b.WriteString("\x1b[J")
		fmt.Fprint(l.out, b.String())
	}
	l.drawn = 0
}

// Suspend clears the box and stops drawing until Resume (BF-02). A child that
// prompts below the box needs the rows the box occupied.
func (l *liveProgress) Suspend() {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer l.guardRenderPanic()
	if l.closed || l.suspended {
		return
	}
	l.suspended = true
	if l.viewport != nil {
		l.viewport.Hide()
		return
	}
	l.eraseLocked()
}

// Resume draws the box again after Suspend.
func (l *liveProgress) Resume() {
	l.mu.Lock()
	defer l.mu.Unlock()
	defer l.guardRenderPanic()
	if l.closed || !l.suspended {
		return
	}
	l.suspended = false
	if l.viewport != nil {
		l.viewport.Show(l.renderLinesLocked())
		return
	}
	l.drawLocked()
}

// WriteRaw writes child bytes to the terminal while the surface is suspended.
// It takes the renderer's lock, so the bytes never interleave with a draw.
func (l *liveProgress) WriteRaw(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	_, _ = l.out.Write(b)
}

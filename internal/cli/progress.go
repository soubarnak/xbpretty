package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// plainLineWidth is FR-065's fixed 40-cell bar.
const plainLineWidth = 40

// flushInterval is FR-041/FR-064's coalescing window: live redraws happen at
// most once per 150 ms, with an immediate redraw for completion, cache-hit,
// failure and phase-change events.
const flushInterval = 150 * time.Millisecond

// progressModel turns the xbps telemetry callbacks into the five fixed views.
// It is the only place the spec's formulas live.
type progressModel struct {
	op    string
	start time.Time
	now   func() time.Time

	// Download state. Archives are keyed by package name, the same key
	// packageNameFromFilename produces for a tick or completion.
	archives       map[string]uint64
	totalArchives  int
	totalBytes     uint64
	completed      map[string]bool
	completedBytes uint64
	completedCount int
	lastCompleted  string
	inFlight       map[string]uint64

	// Install state: U = 2·A units, unpack and configure each count once.
	installTotal int
	unpacked     map[string]bool
	configured   map[string]bool
	removed      map[string]bool
	removeTotal  int
	currentPkg   string
	status       string

	// Update and MirrorScore state.
	reposDone, reposTotal int
	repoURL               string
	mirrorDone            int
	mirrorTotal           int
	mirrorURL             string
	mirrorElapsed         time.Duration

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
		completed:  map[string]bool{},
		inFlight:   map[string]uint64{},
		unpacked:   map[string]bool{},
		configured: map[string]bool{},
		removed:    map[string]bool{},
	}
	switch op {
	case "remove", "purge", "autoremove", "autopurge":
		m.kind = viewRemove
	case "update":
		m.kind = viewUpdate
	}
	for _, row := range rows {
		switch row.Action {
		case xbps.ActionRemove:
			m.removeTotal++
		case xbps.ActionInstall, xbps.ActionUpdate, xbps.ActionReinstall, xbps.ActionConfigure:
			m.installTotal++
			if row.Action == xbps.ActionConfigure {
				continue
			}
			key := row.Pkgver()
			if _, seen := m.archives[key]; !seen {
				m.archives[key] = row.DownloadSize
				m.totalBytes += row.DownloadSize
				m.totalArchives++
			}
		}
	}
	if m.kind != viewRemove && m.kind != viewUpdate {
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

// DownloadTick applies one live tick. A .sig2 signature file never moves the
// Packages, Data, Speed or Remaining counters.
func (m *progressModel) downloadTick(tick DownloadTick) {
	if strings.HasSuffix(tick.Filename, ".sig2") {
		return
	}
	name := packageNameFromFilename(tick.Filename)
	size, ok := m.archives[name]
	if !ok || m.completed[name] {
		return
	}
	m.inFlight[name] = uint64(float64(size) * float64(tick.FilePercent) / 100.0)
}

// downloadDone applies one archive completion. Completion lines are the cache
// hit too: an archive that finished without a tick still counts its exact plan
// bytes.
func (m *progressModel) downloadDone(done DownloadComplete) {
	if strings.HasSuffix(done.Filename, ".sig2") {
		return
	}
	name := packageNameFromFilename(done.Filename)
	if m.completed[name] {
		return
	}
	size, ok := m.archives[name]
	if !ok {
		return
	}
	m.completed[name] = true
	m.completedBytes += size
	m.completedCount++
	m.lastCompleted = done.Filename
	delete(m.inFlight, name)
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
			if m.removeTotal > 0 {
				m.kind = viewRemove
			} else if m.installTotal > 0 {
				m.kind = viewInstall
			}
		}
	}
}

func (m *progressModel) packageEvent(event PackageEvent) {
	switch event.Action {
	case "unpacking", "updating":
		m.kind = viewInstall
		m.currentPkg = event.Package
		m.status = "Unpacking"
		m.unpacked[event.Package] = true
	case "configuring":
		m.kind = viewInstall
		m.currentPkg = event.Package
		m.status = "Configuring"
		m.configured[event.Package] = true
	case "removing":
		m.kind = viewRemove
		m.currentPkg = event.Package
		m.status = "Removing"
	case "removed":
		m.kind = viewRemove
		m.removed[event.Package] = true
	}
}

// repository records one `[*] Updating repository '<url>' ...` line. xbps
// does not announce the repository count first, so the total grows as the
// lines arrive (and can be seeded from the xbps.d scan by the caller).
func (m *progressModel) repository(url string) {
	m.reposDone++
	m.repoURL = url
	if m.reposTotal < m.reposDone {
		m.reposTotal = m.reposDone
	}
}

func (m *progressModel) setRepositoryTotal(total int) {
	if total > m.reposTotal {
		m.reposTotal = total
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

// ratio is the fraction each view draws. Install is the two-unit model:
// (unpacked + configured) / (2·A).
func (m *progressModel) ratio() float64 {
	switch m.kind {
	case viewDownload:
		if m.totalBytes == 0 {
			return 0
		}
		return float64(m.position()) / float64(m.totalBytes)
	case viewInstall:
		if m.installTotal == 0 {
			return 0
		}
		return float64(len(m.unpacked)+len(m.configured)) / float64(2*m.installTotal)
	case viewRemove:
		if m.removeTotal == 0 {
			return 0
		}
		return float64(len(m.removed)) / float64(m.removeTotal)
	case viewUpdate:
		if m.reposTotal == 0 {
			return 0
		}
		return float64(m.reposDone) / float64(m.reposTotal)
	case viewMirrorScore:
		if m.mirrorTotal == 0 {
			return 0
		}
		return float64(m.mirrorDone) / float64(m.mirrorTotal)
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
		ratio := 1.0
		m.kind = viewInstall
		if m.removeTotal > 0 {
			m.kind = viewRemove
		}
		return progressView{
			kind:      viewDownload,
			msgHeader: "Last completed",
			msgText:   m.lastCompleted,
			msgRole:   rolePrimary,
			left:      []progressInfo{{"Packages", pairCount(m.totalArchives, m.totalArchives)}, {"Remaining", "0s"}},
			right: []progressInfo{
				{"Data", widgetSize(m.totalBytes) + "/" + widgetSize(m.totalBytes)},
				{"Speed", widgetSize(m.downloadSpeed()) + "/s"},
			},
			ratio: ratio,
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
				{"Data", widgetSize(position) + "/" + widgetSize(m.totalBytes)},
				{"Speed", widgetSize(rate) + "/s"},
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
		text := strings.TrimSpace(m.status + " " + m.currentPkg)
		return progressView{
			kind:      viewRemove,
			msgHeader: "Status",
			msgText:   text,
			msgRole:   rolePrimary,
			left:      []progressInfo{{"Package", m.currentPkg}},
			right:     []progressInfo{{"Elapsed", m.elapsedText()}},
			ratio:     m.ratio(),
		}
	case viewUpdate:
		return progressView{
			kind:      viewUpdate,
			msgHeader: "Updating",
			msgText:   m.repoURL,
			msgRole:   roleRegular,
			left:      []progressInfo{{"Repositories", pairCount(m.reposDone, m.reposTotal)}},
			right:     []progressInfo{{"Elapsed", m.elapsedText()}},
			ratio:     m.ratio(),
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
		state.hasItems = true
		state.itemsDone = m.completedCount
		state.itemsTotal = m.totalArchives
	case viewInstall:
		state.message = strings.TrimSpace(m.status + " " + m.currentPkg)
		if state.message == "" {
			state.message = "Working..."
		}
		state.hasItem = true
		state.item = m.currentPkg
	case viewRemove:
		state.message = strings.TrimSpace(m.status + " " + m.currentPkg)
		if state.message == "" {
			state.message = "Working..."
		}
		state.hasItem = true
		state.item = m.currentPkg
	case viewUpdate:
		state.message = "Updating: " + m.repoURL
		state.hasRepos = true
		state.reposDone = m.reposDone
		state.reposTotal = m.reposTotal
	case viewMirrorScore:
		state.message = "Finished: " + m.mirrorURL
		state.hasMirrors = true
		state.mirrorDone = m.mirrorDone
		state.mirrorTotal = m.mirrorTotal
	}
	if state.message == "" {
		state.message = "Working..."
	}
	return state
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
		b.WriteString(" " + widgetSize(state.position) + "/" + widgetSize(state.total))
		b.WriteString(" " + widgetSize(state.rate) + "/s")
	}
	if state.hasItem {
		b.WriteString(" | Package: " + state.item)
	}
	if state.hasItems {
		b.WriteString(" | Packages: " + pairCount(state.itemsDone, state.itemsTotal))
	}
	if state.hasRepos {
		b.WriteString(" | Repositories: " + pairCount(state.reposDone, state.reposTotal))
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
	model  *progressModel
	mu     sync.Mutex
	drawn  int
	closed bool

	lastDraw time.Time
	timer    *time.Timer
	now      func() time.Time
	after    func(time.Duration, func()) *time.Timer
}

// newLiveProgress constructs the renderer. Width is clamped by the renderer
// through widgetWidth.
func newLiveProgress(out io.Writer, color bool, width int, mode liveMode, model *progressModel) *liveProgress {
	return &liveProgress{
		out:   out,
		color: color && mode == liveWidgetMode,
		mode:  mode,
		width: width,
		model: model,
		now:   time.Now,
		after: time.AfterFunc,
	}
}

// SetWidth applies a SIGWINCH width change; the next redraw uses it.
func (l *liveProgress) SetWidth(columns int) {
	if columns <= 0 {
		return
	}
	l.mu.Lock()
	l.width = columns
	l.mu.Unlock()
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
	if l.closed {
		return
	}
	if l.mode == livePlainMode {
		fmt.Fprintf(l.out, "\r\x1b[2K%s", renderPlainLine(l.model.plain()))
		l.drawn = 1
	} else {
		lines := renderProgressView(l.model.view(), l.width, l.color)
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
	}
	l.lastDraw = l.now()
}

// Finish tears the live surface down. Success clears without any completion
// line (FR-064); failure also clears, leaving the caller to print the captured
// child output. No ANSI is left on the line.
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
	if l.drawn == 0 {
		return
	}
	if l.mode == livePlainMode {
		fmt.Fprint(l.out, "\r\x1b[2K")
		l.drawn = 0
		return
	}
	var b strings.Builder
	b.WriteString("\r")
	if l.drawn > 1 {
		fmt.Fprintf(&b, "\x1b[%dA", l.drawn-1)
	}
	b.WriteString("\x1b[J")
	fmt.Fprint(l.out, b.String())
	l.drawn = 0
}

// repositoryCount is the repository total the Update view can know before the
// sync starts. xbps prints one `[*] Updating repository ...` line per repo and
// never announces the count, so the xbps.d scan seeds it where possible; when
// the scan fails the model grows the total as lines arrive.
func repositoryCount() int {
	cfg, err := fetch.DiscoverRepositories(fetchXBPSDDirs)
	if err != nil {
		return 0
	}
	return len(cfg.Lines)
}

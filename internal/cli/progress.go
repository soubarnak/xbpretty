package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

const (
	resetSeq    = "\x1b[0m"
	boldSeq     = "\x1b[1m"
	dimSeq      = "\x1b[2m"
	redSeq      = "\x1b[1;31m"
	greenSeq    = "\x1b[1;32m"
	blueSeq     = "\x1b[1;34m"
	cyanSeq     = "\x1b[1;36m"
	redTrackSeq = "\x1b[31m"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func progressBar(percent float64, width int, color bool, boxStyle string) string {
	if width < 5 {
		width = 20
	}
	filledCount := int(float64(width) * (percent / 100.0))
	if filledCount > width {
		filledCount = width
	}
	if filledCount < 0 {
		filledCount = 0
	}
	unfilledCount := width - filledCount

	var fillChar, emptyChar string
	if boxStyle == "ascii" {
		fillChar = "#"
		emptyChar = "-"
	} else {
		fillChar = "━"
		emptyChar = "─"
	}

	if !color {
		return strings.Repeat(fillChar, filledCount) + strings.Repeat(emptyChar, unfilledCount)
	}
	// Cyan fill over red track (Nala signature)
	return cyanSeq + strings.Repeat(fillChar, filledCount) + resetSeq +
		redTrackSeq + strings.Repeat(emptyChar, unfilledCount) + resetSeq
}

// ProgressWidget manages an in-place 5-line framed execution progress widget.
type ProgressWidget struct {
	mu sync.Mutex

	out      io.Writer
	boxStyle string
	color    bool
	width    int

	// State
	phase           string // "Downloading", "Unpacking", "Configuring", "Removing"
	currentPackage  string
	totalPackages   int
	packageIndex    int
	downloadedBytes int64
	totalDownload   int64
	downloadRate    string
	timeRemaining   string
	downloadPercent float64

	// Action phase tracking
	completedActions int
	totalActions     int
	activeAction     string

	// Spinner
	spinIdx    int
	stopTicker chan struct{}
	tickerDone chan struct{}

	// Lifecycle
	started      bool
	renderedOnce bool
	cleared      bool
	startTime    time.Time
	summary      *TransactionSummary
}

// NewProgressWidget constructs a new ProgressWidget.
func NewProgressWidget(out io.Writer, boxStyle string, color bool, width int, totalPackages int, totalDownloadBytes int64) *ProgressWidget {
	if width <= 0 {
		width = 80
	}
	totalActions := totalPackages * 2
	if totalActions <= 0 {
		totalActions = 1
	}
	w := &ProgressWidget{
		out:           out,
		boxStyle:      boxStyle,
		color:         color,
		width:         width,
		totalPackages: totalPackages,
		totalDownload: totalDownloadBytes,
		totalActions:  totalActions,
		timeRemaining: "-:--:--",
		downloadRate:  "0.0 B/s",
		startTime:     time.Now(),
		stopTicker:    make(chan struct{}),
		tickerDone:    make(chan struct{}),
	}
	if totalDownloadBytes > 0 {
		w.phase = "Downloading"
	} else {
		w.phase = "Unpacking"
	}
	return w
}

// Start begins the background spinner ticker for live animation during unpack/configure.
func (w *ProgressWidget) Start() {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.renderLocked()
	w.mu.Unlock()

	go func() {
		defer close(w.tickerDone)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-w.stopTicker:
				return
			case <-ticker.C:
				w.mu.Lock()
				if !w.cleared && w.isActionPhase() {
					w.spinIdx++
					w.renderLocked()
				}
				w.mu.Unlock()
			}
		}
	}()
}

// IsRendered reports whether the widget box has been rendered at least once.
func (w *ProgressWidget) IsRendered() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.renderedOnce
}

// SetWidth updates the progress widget width dynamically (e.g. on terminal resize / SIGWINCH).
func (w *ProgressWidget) SetWidth(width int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if width > 0 {
		w.width = width
	}
}

// Stop stops the background ticker if started.
func (w *ProgressWidget) Stop() {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return
	}
	w.started = false
	close(w.stopTicker)
	w.mu.Unlock()
	<-w.tickerDone
}

func (w *ProgressWidget) isActionPhase() bool {
	switch w.phase {
	case "RepoSync", "Verifying", "Collecting", "Unpacking", "Configuring", "Removing":
		return true
	default:
		return false
	}
}

// AttachToParser registers ProgressWidget listener callbacks on the StreamParser.
func (w *ProgressWidget) AttachToParser(p *StreamParser) {
	p.OnDownloadTick = func(tick DownloadTick) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.phase = "Downloading"
		w.currentPackage = tick.Filename
		if tick.Rate != "" {
			w.downloadRate = tick.Rate
		}
		if tick.ETA != "" {
			w.timeRemaining = tick.ETA
		}
		st := p.State()
		w.packageIndex = st.PackageIndex
		if st.TotalPackages > w.totalPackages {
			w.totalPackages = st.TotalPackages
		}
		w.downloadedBytes = st.DownloadedBytes
		if st.TotalDownload > 0 {
			w.totalDownload = st.TotalDownload
		}
		w.downloadPercent = st.Percent
		w.renderLocked()
	}

	p.OnDownloadComplete = func(comp DownloadComplete) {
		w.mu.Lock()
		defer w.mu.Unlock()
		st := p.State()
		w.downloadedBytes = st.DownloadedBytes
		w.currentPackage = comp.Filename
		if comp.AvgRate != "" {
			w.downloadRate = comp.AvgRate
		}
		w.renderLocked()
	}

	p.OnPhaseChange = func(phase string) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.phase = phase
		w.activeAction = phase
		w.renderLocked()
	}

	p.OnPackageEvent = func(event PackageEvent) {
		w.mu.Lock()
		defer w.mu.Unlock()
		switch event.Action {
		case "unpacking", "updating":
			w.phase = "Unpacking"
			w.activeAction = "Unpacking"
			w.completedActions++
		case "configuring":
			w.phase = "Configuring"
			w.activeAction = "Configuring"
			w.completedActions++
		case "removing":
			w.phase = "Removing"
			w.activeAction = "Removing"
			w.completedActions++
		}
		if w.completedActions > w.totalActions {
			w.totalActions = w.completedActions
		}
		w.currentPackage = event.Package
		w.renderLocked()
	}

	p.OnSummary = func(sum TransactionSummary) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.summary = &sum
	}
}

func (w *ProgressWidget) buildTopBorder(title string, chars summary.BoxChars, colorSeq string) string {
	maxTitleW := w.width - 4
	if maxTitleW < 0 {
		maxTitleW = 0
	}
	title = summary.TruncateVisible(title, maxTitleW)
	titleWidth := summary.VisibleWidth(title)
	fillWidth := w.width - titleWidth - 4
	if fillWidth < 0 {
		fillWidth = 0
	}
	if !w.color {
		return chars.TopLeft + strings.Repeat(chars.Horizontal, 2) + title + strings.Repeat(chars.Horizontal, fillWidth) + chars.TopRight
	}
	return colorSeq + chars.TopLeft + strings.Repeat(chars.Horizontal, 2) + resetSeq +
		boldSeq + title + resetSeq +
		colorSeq + strings.Repeat(chars.Horizontal, fillWidth) + chars.TopRight + resetSeq
}

func (w *ProgressWidget) buildBottomBorder(chars summary.BoxChars, colorSeq string) string {
	fillWidth := w.width - 2
	if fillWidth < 0 {
		fillWidth = 0
	}
	if !w.color {
		return chars.BottomLeft + strings.Repeat(chars.Horizontal, fillWidth) + chars.BottomRight
	}
	return colorSeq + chars.BottomLeft + strings.Repeat(chars.Horizontal, fillWidth) + chars.BottomRight + resetSeq
}

func (w *ProgressWidget) frameLine(content string, chars summary.BoxChars, colorSeq string) string {
	innerW := w.width - 2
	if innerW < 0 {
		innerW = 0
	}
	content = summary.TruncateVisible(content, innerW)
	padded := summary.PadRight(content, innerW)
	if !w.color {
		return chars.Vertical + padded + chars.Vertical
	}
	return colorSeq + chars.Vertical + resetSeq + padded + colorSeq + chars.Vertical + resetSeq
}

func (w *ProgressWidget) buildDownloadLine1() string {
	pkgLabel := "Total Packages:"
	if w.color {
		pkgLabel = greenSeq + pkgLabel + resetSeq
	}
	return fmt.Sprintf("  %s %d/%d", pkgLabel, w.packageIndex, w.totalPackages)
}

func (w *ProgressWidget) buildDownloadLine2() string {
	dlLabel := "Downloading:"
	if w.color {
		dlLabel = greenSeq + dlLabel + resetSeq
	}
	pkgName := w.currentPackage
	if pkgName == "" {
		pkgName = "starting..."
	}
	return fmt.Sprintf("  %s %s", dlLabel, pkgName)
}

func (w *ProgressWidget) buildDownloadLine3() string {
	pct := w.downloadPercent
	innerW := w.width - 2
	if innerW < 0 {
		innerW = 0
	}

	dlStr := summary.Human(uint64(w.downloadedBytes))
	var totStr string
	if w.totalDownload > 0 {
		totStr = summary.Human(uint64(w.totalDownload))
	} else {
		totStr = dlStr
	}

	rate := w.downloadRate
	if rate == "" {
		rate = "0.0 B/s"
	}
	eta := w.timeRemaining
	if eta == "" {
		eta = "-:--:--"
	}

	label := "  Time Remaining: "
	if w.width < 75 {
		label = "  ETA: "
	}

	baseW := summary.VisibleWidth(label) + summary.VisibleWidth(eta) + 17 + summary.VisibleWidth(dlStr) + summary.VisibleWidth(totStr) + summary.VisibleWidth(rate)
	barWidth := innerW - baseW
	if barWidth > 20 {
		barWidth = 20
	}
	if barWidth < 6 {
		barWidth = 6
	}
	bar := progressBar(pct, barWidth, w.color, w.boxStyle)

	if w.color {
		return fmt.Sprintf("%s%s  [%s] %s%.1f%%%s • %s%s/%s%s • %s%s%s",
			label, eta, bar, boldSeq+blueSeq, pct, resetSeq, boldSeq+greenSeq, dlStr, totStr, resetSeq, boldSeq+greenSeq, rate, resetSeq)
	}
	return fmt.Sprintf("%s%s  [%s] %.1f%% • %s/%s • %s",
		label, eta, bar, pct, dlStr, totStr, rate)
}

func (w *ProgressWidget) buildActionLine1() string {
	sp := spinnerFrames[w.spinIdx%len(spinnerFrames)]
	phase := w.activeAction
	if phase == "" {
		phase = w.phase
	}
	if phase == "" {
		phase = "Unpacking"
	}
	if w.color {
		return fmt.Sprintf("  Phase:           %s %s packages", cyanSeq+sp+resetSeq, boldSeq+phase+resetSeq)
	}
	return fmt.Sprintf("  Phase:           %s %s packages", sp, phase)
}

func (w *ProgressWidget) buildActionLine2() string {
	target := w.currentPackage
	if target == "" {
		target = "starting..."
	}
	if w.color {
		return fmt.Sprintf("  Current Target:  %s", boldSeq+target+resetSeq)
	}
	return fmt.Sprintf("  Current Target:  %s", target)
}

func (w *ProgressWidget) buildActionLine3() string {
	var pct float64
	if w.totalActions > 0 {
		pct = (float64(w.completedActions) / float64(w.totalActions)) * 100.0
	}
	if pct > 100.0 {
		pct = 100.0
	}
	if pct < 0.0 {
		pct = 0.0
	}
	innerW := w.width - 2
	if innerW < 0 {
		innerW = 0
	}
	barWidth := innerW - 44
	if barWidth > 24 {
		barWidth = 24
	}
	if barWidth < 8 {
		barWidth = 8
	}
	bar := progressBar(pct, barWidth, w.color, w.boxStyle)
	if w.color {
		return fmt.Sprintf("  Status:          [%s] %s%.1f%%%s • %d/%d actions",
			bar, boldSeq+blueSeq, pct, resetSeq, w.completedActions, w.totalActions)
	}
	return fmt.Sprintf("  Status:          [%s] %.1f%% • %d/%d actions",
		bar, pct, w.completedActions, w.totalActions)
}

func (w *ProgressWidget) renderLocked() {
	if w.cleared {
		return
	}
	if f, ok := w.out.(*os.File); ok {
		if tw := terminalWidth(f); tw > 0 {
			w.width = tw
		}
	}
	var lines [5]string
	chars := summary.BoxCharacterSet(w.boxStyle)

	if w.isActionPhase() {
		lines[0] = w.buildTopBorder(" Running xbps... ", chars, blueSeq)
		lines[1] = w.frameLine(w.buildActionLine1(), chars, blueSeq)
		lines[2] = w.frameLine(w.buildActionLine2(), chars, blueSeq)
		lines[3] = w.frameLine(w.buildActionLine3(), chars, blueSeq)
		lines[4] = w.buildBottomBorder(chars, blueSeq)
	} else {
		lines[0] = w.buildTopBorder(" Downloading... ", chars, greenSeq)
		lines[1] = w.frameLine(w.buildDownloadLine1(), chars, greenSeq)
		lines[2] = w.frameLine(w.buildDownloadLine2(), chars, greenSeq)
		lines[3] = w.frameLine(w.buildDownloadLine3(), chars, greenSeq)
		lines[4] = w.buildBottomBorder(chars, greenSeq)
	}

	if w.renderedOnce {
		fmt.Fprintf(w.out, "\x1b[5A\r")
	}
	for i := 0; i < 5; i++ {
		fmt.Fprintf(w.out, "%s\n", lines[i])
	}
	w.renderedOnce = true
}

func (w *ProgressWidget) formatCompletionSummary(rows []xbps.Row, cmd string) string {
	elapsed := time.Since(w.startTime)
	if w.summary != nil && (w.summary.Installed > 0 || w.summary.Updated > 0 || w.summary.Removed > 0) {
		return fmt.Sprintf("%d installed, %d updated, %d removed in %.1fs",
			w.summary.Installed, w.summary.Updated, w.summary.Removed, elapsed.Seconds())
	}
	if len(rows) > 0 {
		var inst, up, rem int
		for _, r := range rows {
			switch r.Action {
			case "install", "reinstall":
				inst++
			case "update":
				up++
			case "remove":
				rem++
			}
		}
		return fmt.Sprintf("%d installed, %d updated, %d removed in %.1fs",
			inst, up, rem, elapsed.Seconds())
	}
	if cmd == "update" {
		return fmt.Sprintf("updated repositories in %.1fs", elapsed.Seconds())
	}
	return fmt.Sprintf("in %.1fs", elapsed.Seconds())
}

// Finish cleans up the progress box and outputs the teardown summary or prepares for error output.
func (w *ProgressWidget) Finish(exitCode int, rawStderr string, rows []xbps.Row, cmd string) {
	w.Stop()

	w.mu.Lock()
	defer w.mu.Unlock()
	w.cleared = true

	if w.renderedOnce {
		fmt.Fprintf(w.out, "\x1b[5A\r\x1b[0J")
		w.renderedOnce = false
	}

	if exitCode == 0 {
		summaryText := w.formatCompletionSummary(rows, cmd)
		if w.color {
			fmt.Fprintf(w.out, "%s%s✔ Transaction complete:%s %s\n", boldSeq, greenSeq, resetSeq, summaryText)
		} else {
			fmt.Fprintf(w.out, "✔ Transaction complete: %s\n", summaryText)
		}
	}
}

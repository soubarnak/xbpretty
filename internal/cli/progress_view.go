package cli

import (
	"math"
	"strconv"
	"strings"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// progressKind selects one of FR-064's five fixed views.
type progressKind int

const (
	viewDownload progressKind = iota
	viewInstall
	viewRemove
	viewUpdate
	viewMirrorScore
)

// progressInfo is one label/value cell of the widget's two-column info band.
type progressInfo struct {
	label string
	value string
}

// progressView is everything one frame of the rounded widget needs. The model
// fills it; the renderer only lays it out.
type progressView struct {
	kind      progressKind
	title     string
	msgHeader string
	msgText   string
	msgRole   progressRole
	left      []progressInfo
	right     []progressInfo
	ratio     float64
}

// Fixed view heights: Download carries two info rows, the others one.
const (
	heightDownload = 7
	heightOther    = 6
)

func (v progressView) height() int {
	if v.kind == viewDownload {
		return heightDownload
	}
	return heightOther
}

func (v progressView) titleText() string {
	switch v.kind {
	case viewDownload:
		return "Downloading Packages"
	case viewInstall:
		return "Installing Packages"
	case viewRemove:
		return "Removing Packages"
	case viewUpdate:
		return "Updating Package Lists"
	case viewMirrorScore:
		return "Testing Mirrors"
	}
	if v.title != "" {
		return v.title
	}
	return "Working"
}

// widgetPercent truncates ratio*100, clamped to [0,100]. The bar fill rounds;
// the displayed percent does not (ADR-0012 decision 5).
func widgetPercent(ratio float64) int {
	if ratio < 0 || math.IsNaN(ratio) {
		return 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return int(ratio * 100)
}

// widgetFill rounds width*ratio, clamped to [0,width].
func widgetFill(width int, ratio float64) int {
	if math.IsNaN(ratio) {
		ratio = 0
	}
	n := int(math.Round(float64(width) * ratio))
	if n < 0 {
		n = 0
	}
	if n > width {
		n = width
	}
	return n
}

// widgetWidth applies FR-064's width rule: min(columns, 80). The 8-column
// floor is an extension: below it the frame cannot hold its own borders, and
// the renderer drops the Progress label so the divider stays rectangular.
func widgetWidth(columns int) int {
	if columns <= 0 {
		columns = 80
	}
	if columns > 80 {
		return 80
	}
	if columns < 8 {
		return 8
	}
	return columns
}

// halfLabelWidth is max(len(label)) over one info half; the colon is not
// counted.
func halfLabelWidth(rows []progressInfo) int {
	w := 0
	for _, r := range rows {
		if n := len(r.label); n > w {
			w = n
		}
	}
	return w
}

// renderInfoCell is " " + "{label}:" + (labelWidth - len(label) + 2) spaces +
// value, label Primary and value Regular.
func renderInfoCell(theme summary.Theme, color bool, row progressInfo, labelWidth int) string {
	gap := labelWidth - len(row.label) + 2
	return " " + paintRole(theme, color, rolePrimary, row.label+":") + repeatRole(" ", gap) +
		paintRole(theme, color, roleRegular, row.value)
}

// renderProgressView lays one view out at columns columns and returns its
// lines. Height comes from the view, never the content.
func renderProgressView(v progressView, columns int, color bool) []string {
	return renderProgressViewThemed(v, columns, color, summary.DefaultTheme())
}

// renderProgressViewThemed lays one view out with the resolved theme roles.
func renderProgressViewThemed(v progressView, columns int, color bool, theme summary.Theme) []string {
	width := widgetWidth(columns)
	inner := width - 4
	if inner < 0 {
		inner = 0
	}
	half := inner / 2

	// Top border: "  <title>  " centred in the frame.
	title := "  " + v.titleText() + "  "
	pad := width - 2 - roleWidth(title)
	if pad < 0 {
		pad = 0
		title = clipRole(title, width-2)
	}
	leftPad := pad / 2
	rightPad := pad - leftPad
	top := "╭" + paintRole(theme, color, rolePrimary, repeatRole("─", leftPad)) +
		paintRole(theme, color, roleHighlight, title) +
		paintRole(theme, color, rolePrimary, repeatRole("─", rightPad)) + "╮"

	// Message row: " " + "{Header}: " + text. The text falls back to an
	// em dash when the stream has not named anything yet.
	header := v.msgHeader
	if header == "" {
		header = "Status"
	}
	text := v.msgText
	if text == "" {
		text = "—"
	}
	message := " " + paintRole(theme, color, v.msgRole, header+": ") + paintRole(theme, color, roleRegular, text)
	msgRow := "│ " + padRole(clipRole(message, inner), inner) + " │"

	// Divider: full frame width, centred " Progress ", right " NN% ".
	frameWidth := width - 2
	label := " Progress "
	percent := " " + strconv.Itoa(widgetPercent(v.ratio)) + "% "
	pctStart := 1 + frameWidth - roleWidth(percent)
	// Below a usable width the label is dropped rather than overflowing; the
	// percent stays right-aligned so the divider still spans the frame.
	if frameWidth < roleWidth(label)+roleWidth(percent)+2 {
		label = ""
	}
	labelStart := 1
	if label != "" {
		labelStart = 1 + (frameWidth-roleWidth(label))/2
		if pctStart < labelStart+roleWidth(label) {
			labelStart = 1
		}
	}
	var divider strings.Builder
	divider.WriteString("├")
	x := 1
	writeRuns := func(upto int, role progressRole, glyph string) {
		if upto > x {
			divider.WriteString(paintRole(theme, color, role, repeatRole(glyph, upto-x)))
			x = upto
		}
	}
	writeRuns(labelStart, rolePrimary, "─")
	if label != "" {
		divider.WriteString(paintRole(theme, color, roleHighlight, label))
		x += roleWidth(label)
	}
	writeRuns(pctStart, rolePrimary, "─")
	divider.WriteString(paintRole(theme, color, roleHighlight, percent))
	x += roleWidth(percent)
	writeRuns(1+frameWidth, rolePrimary, "─")
	divider.WriteString("┤")

	// Bar row: W-4 glyphs, filled and unfilled differ by colour only.
	filled := widgetFill(inner, v.ratio)
	bar := paintRole(theme, color, roleProgressFilled, repeatRole("━", filled)) +
		paintRole(theme, color, roleProgressUnfilled, repeatRole("━", inner-filled))
	barRow := "│ " + bar + " │"

	lines := []string{top, msgRow, divider.String(), barRow}
	leftLabels := halfLabelWidth(v.left)
	rightLabels := halfLabelWidth(v.right)
	rows := v.height() - 5
	for i := 0; i < rows; i++ {
		var left, right string
		if i < len(v.left) {
			left = renderInfoCell(theme, color, v.left[i], leftLabels)
		}
		if i < len(v.right) {
			right = renderInfoCell(theme, color, v.right[i], rightLabels)
		}
		content := padRole(left, half) + right
		lines = append(lines, "│ "+padRole(clipRole(content, inner), inner)+" │")
	}
	lines = append(lines, "╰"+paintRole(theme, color, rolePrimary, repeatRole("─", width-2))+"╯")
	return lines
}

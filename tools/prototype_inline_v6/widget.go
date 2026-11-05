package main

import (
	"strconv"
)

// InfoRow is one label/value cell of the widget's info band.
type InfoRow struct {
	Label string
	Value string
}

// Widget is one rendered frame of the rounded progress box (#45 §6).
type Widget struct {
	Title     string
	MsgHeader string // role-coloured header, e.g. "Last completed"
	MsgText   string
	MsgRole   Role
	Left      []InfoRow
	Right     []InfoRow
	Ratio     float64
}

const (
	boxHorizontal = "─"
	boxVertical   = "│"
	boxTopLeft    = "╭"
	boxTopRight   = "╮"
	boxBotLeft    = "╰"
	boxBotRight   = "╯"
	boxDivLeft    = "├"
	boxDivRight   = "┤"
	barGlyph      = "━"
)

// halfLabelWidth is max(len(label)) over one info half; the colon is not
// counted (#45 §6.4).
func halfLabelWidth(rows []InfoRow) int {
	w := 0
	for _, r := range rows {
		if n := len(r.Label); n > w {
			w = n
		}
	}
	return w
}

// renderInfoCell is " " + "{label}:" + (labelWidth − len(label) + 2) spaces +
// value, with the label Primary and the value Regular (#45 §6.4/§2.2).
func renderInfoCell(r InfoRow, labelWidth int, th Theme) string {
	gap := labelWidth - len(r.Label) + 2
	return " " + th.Paint(RolePrimary, r.Label+":") + repeat(" ", gap) + th.Paint(RoleRegular, r.Value)
}

// renderWidget lays the box out at cols columns. W = min(cols, 80), inner
// width W-4, height fixed by the view (the caller supplies the right number
// of info rows) (#45 §6.2).
func renderWidget(w Widget, cols int, th Theme) []string {
	W := cols
	if W > 80 {
		W = 80
	}
	if W < 8 {
		W = 8
	}
	inner := W - 4
	half := inner / 2

	// Top border with the title centred: "  {title}  " inside the frame.
	title := "  " + w.Title + "  "
	pad := W - 2 - visibleWidth(title)
	leftPad := pad / 2
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := pad - leftPad
	if rightPad < 0 {
		rightPad = 0
	}
	top := boxTopLeft + th.Paint(RolePrimary, repeat(boxHorizontal, leftPad)) +
		th.Paint(RoleHighlight, clip(title, W-2)) +
		th.Paint(RolePrimary, repeat(boxHorizontal, rightPad)) + boxTopRight

	// Divider: " Progress " centred on the full frame width (W-2, ignoring
	// horizontal padding), percent right-aligned (#45 §6.2).
	frameWidth := W - 2
	label := " Progress "
	pct := " " + strconv.Itoa(percent(w.Ratio)) + "% "
	divPad := frameWidth - visibleWidth(label)
	divLeft := divPad / 2
	if divLeft < 0 {
		divLeft = 0
	}
	divMid := frameWidth - divLeft - visibleWidth(label) - visibleWidth(pct)
	if divMid < 0 {
		divMid = 0
	}
	divider := boxDivLeft + th.Paint(RolePrimary, repeat(boxHorizontal, divLeft)) +
		th.Paint(RoleHighlight, label) + th.Paint(RolePrimary, repeat(boxHorizontal, divMid)) +
		th.Paint(RoleHighlight, pct) + boxDivRight

	// Bar row: W-4 glyphs, split by colour only.
	filled := fillCount(inner, w.Ratio)
	bar := th.Paint(RoleProgressFilled, repeat(barGlyph, filled)) +
		th.Paint(RoleProgressUnfilled, repeat(barGlyph, inner-filled))
	barRow := boxVertical + " " + bar + " " + boxVertical

	// Message row: " " + "{Header}: " + text, leading space plain.
	var msg string
	if w.MsgText != "" {
		msg = " " + th.Paint(w.MsgRole, w.MsgHeader+": ") + th.Paint(RoleRegular, w.MsgText)
	} else {
		msg = " " + th.Paint(w.MsgRole, w.MsgHeader+": ") + th.Paint(RoleRegular, "—")
	}
	msgRow := boxVertical + " " + padRight(clip(msg, inner), inner) + " " + boxVertical

	bottom := boxBotLeft + th.Paint(RolePrimary, repeat(boxHorizontal, W-2)) + boxBotRight

	lines := []string{top, msgRow, divider, barRow}
	lw, rw := halfLabelWidth(w.Left), halfLabelWidth(w.Right)
	for i := 0; i < len(w.Left) || i < len(w.Right); i++ {
		var l, r string
		if i < len(w.Left) {
			l = renderInfoCell(w.Left[i], lw, th)
		}
		if i < len(w.Right) {
			r = renderInfoCell(w.Right[i], rw, th)
		}
		lines = append(lines, boxVertical+" "+padRight(padRight(l, half)+r, inner)+" "+boxVertical)
	}
	lines = append(lines, bottom)
	return lines
}

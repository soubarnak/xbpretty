package tui

import "github.com/soubarnak/xbpretty/internal/term"

// InfoField is one label/value pair in the package-info view.
type InfoField struct {
	Label string
	Value string
}

// PackageInfoModel is the metadata view `s` opens from S1/S2. It shares S1's
// frame and title; the caller decides which fields the selected row exposes.
type PackageInfoModel struct {
	Title  string
	Fields []InfoField
	Help   []string
}

// RenderPackageInfo draws the package-info view in the summary frame. It is
// read-only: the key loop returns to the summary on q/Q/Esc.
func RenderPackageInfo(m *PackageInfoModel, w, h int, t Theme) *Buffer {
	buf := NewBuffer(w, h)
	buf.Fill(0, 0, w, h, t.Regular)
	border := &block{X: 0, Y: 0, W: w, H: h}
	border.fill(buf, t.Primary)
	border.title(buf, m.Title, t.style(RoleHighlight))
	if !Fits(w, h) {
		centerText(buf, h/2, TooSmall(w, h), t.style(RoleNotice))
		return buf
	}

	bx, by, bw, bh := bodyRect(w, h)
	bodyBottom := by + bh
	y := by
	for _, f := range m.Fields {
		label := f.Label + ":"
		labelW := term.StringWidth(label)
		buf.DrawText(bx, y, label, t.style(RoleHighlight))
		valueX := bx + labelW + 1
		valueW := bw - (labelW + 1)
		if valueW < 1 {
			valueW = 1
		}
		lines := wrapText(f.Value, valueW)
		for _, line := range lines {
			if y >= bodyBottom {
				break
			}
			buf.DrawText(valueX, y, line, t.style(RoleRegular))
			y++
		}
		if y >= bodyBottom {
			break
		}
	}
	if len(m.Help) > 0 {
		helpY := bodyBottom - 1
		for i, line := range m.Help {
			if helpY-i < y {
				break
			}
			buf.DrawTextCenter(bx, helpY-i, bw, line, t.style(RoleSecondary))
		}
	}
	return buf
}

package cli

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// summaryGate is the S1 full-screen gate (FR-067, AC-049). It is a plain
// struct so the matrix is unit-testable without a terminal.
type summaryGate struct {
	Mode       config.UIMode
	StdoutTTY  bool
	StdinTTY   bool
	Term       string
	UTF8Locale bool
	Root       bool
	Rows       int
	HoldOnly   bool
	AssumeYes  bool
	AssumeNo   bool
	Simple     bool
	DryRun     bool
}

// summaryScreenAllowed is FR-067's S1 predicate. Tui plus every terminal and
// command condition is required; Auto and Plain never qualify.
func summaryScreenAllowed(g summaryGate) bool {
	return g.Mode == config.ModeTui &&
		g.StdoutTTY && g.StdinTTY &&
		!strings.EqualFold(g.Term, "dumb") &&
		g.UTF8Locale && g.Root &&
		g.Rows > 0 && !g.HoldOnly &&
		!g.AssumeYes && !g.AssumeNo && !g.Simple && !g.DryRun
}

// gateFor builds the predicate from the resolved state and the plan.
func (st *state) gateFor(opts *options, rows []xbps.Row) summaryGate {
	termName := os.Getenv("TERM")
	return summaryGate{
		Mode:       st.mode,
		StdoutTTY:  isTTY(st.stdout),
		StdinTTY:   isTTY(st.stdin),
		Term:       termName,
		UTF8Locale: utf8Locale(),
		Root:       euidFn() == 0,
		Rows:       len(rows),
		HoldOnly:   allHeld(rows),
		AssumeYes:  st.assumeYes,
		AssumeNo:   opts.assumeNo,
		Simple:     st.simple,
		DryRun:     opts.dryRun,
	}
}

// screenOutcome is what the S1 loop decided.
type screenOutcome int

const (
	// screenUnavailable means init or render failed; the text path runs.
	screenUnavailable screenOutcome = iota
	// screenConfirmed means y/Y accepted the transaction.
	screenConfirmed
	// screenRefused means the user declined; the caller decides the exit code
	// (1 for the primary transaction, "primary result stands" for #64's
	// implicit orphan transaction).
	screenRefused
	// screenExited means a signal or an unrecoverable render failure; the
	// caller returns the supplied code.
	screenExited
)

// screenPanic is a test-only seam: the production binary never sets it. It
// lets the CLI loop exercise its panic restore path directly.
var screenPanic = func() bool { return false }

// The screen hooks are test-only seams. Production builds keep tui.Open,
// tui.RenderSummary and no frame dump; the xbpretty_testroot hook swaps them
// so a PTY test can force an init/render failure and inspect each drawn frame.
var (
	openScreenFn    = tui.Open
	renderSummaryFn = tui.RenderSummary
	drawScreenFn    = func(scr *tui.Screen, buf *tui.Buffer) error { return scr.Draw(buf) }
	screenDumpFn    func(*tui.Buffer)
)

// summaryScreenPolicy is the reusable interaction policy. #76 runs S1 with
// confirmation enabled; #77 reuses the loop read-only (Confirm false), where
// y/Y and n/N are inert and every quit key returns screenRefused for its
// caller to map to exit 0.
type summaryScreenPolicy struct {
	// Confirm enables y/Y to confirm and n/N to refuse.
	Confirm bool
}

// maybeRunSummaryScreen opens S1 when every gate and the entry floor pass. It
// returns the outcome; the caller keeps the text path when it is unavailable.
func (st *state) maybeRunSummaryScreen(opts *options, cmd string, rows []xbps.Row) (screenOutcome, int) {
	if !summaryScreenAllowed(st.gateFor(opts, rows)) {
		return screenUnavailable, -1
	}
	out, ok := asFile(st.stdout)
	if !ok {
		return screenUnavailable, -1
	}
	// FR-067: below 60x18 at entry the text path is used with no screen and
	// no warning.
	if w, h, err := term.Size(out); err != nil || w < 60 || h < 18 {
		return screenUnavailable, -1
	}
	return st.runSummaryScreen(out, st.planToTui(cmd, rows))
}

// runSummaryScreen owns the S1 loop: enter, draw, key/resize/signal handling,
// and restore on every return path. The interaction policy here is S1's: y
// confirms and the cancellation keys refuse. #77 reuses the same loop with
// Confirm false for the read-only history detail.
func (st *state) runSummaryScreen(out *os.File, model *tui.SummaryModel) (screenOutcome, int) {
	return st.runSummaryScreenPolicy(out, model, summaryScreenPolicy{Confirm: true})
}

// runSummaryScreenPolicy is the reusable full-screen summary loop. The
// package-info view is part of the same loop and shares the frame; keys other
// than the documented bindings are ignored.
func (st *state) runSummaryScreenPolicy(out *os.File, model *tui.SummaryModel, policy summaryScreenPolicy) (screenOutcome, int) {
	in, ok := asFile(st.stdin)
	if !ok {
		return screenUnavailable, -1
	}
	scr, err := openScreenFn(in, out, st.color, true)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: full-screen interface unavailable (%v); showing the text summary\n", err)
		return screenUnavailable, -1
	}
	defer scr.Close()
	// AC-054: a panic restores the terminal before it is re-raised, and the
	// one warning line names the failed render.
	defer func() {
		if r := recover(); r != nil {
			scr.Close()
			fmt.Fprintf(st.stderr, "xbpretty: full-screen interface failed (%v)\n", r)
			panic(r)
		}
	}()

	var info *tui.PackageInfoModel
	draw := func(w, h int) error {
		var buf *tui.Buffer
		if info != nil {
			buf = tui.RenderPackageInfo(info, w, h, screenTheme(st.r.cfg.Color.Theme))
		} else {
			buf = renderSummaryFn(model, w, h, screenTheme(st.r.cfg.Color.Theme))
		}
		if screenDumpFn != nil {
			screenDumpFn(buf)
		}
		return drawScreenFn(scr, buf)
	}
	fail := func(err error) (screenOutcome, int) {
		// Restore before warning: on the alternate screen the line would be
		// wiped when the guard leaves it. A failed render warns once and
		// lets the text path finish the transaction.
		scr.Close()
		fmt.Fprintf(st.stderr, "xbpretty: full-screen interface failed (%v); showing the text summary\n", err)
		return screenUnavailable, -1
	}
	w, h := scr.Size()
	if err := draw(w, h); err != nil {
		return fail(err)
	}
	if screenPanic() {
		panic("injected full-screen panic")
	}

	quit := func() (screenOutcome, int) {
		scr.Close()
		return screenRefused, -1
	}
	redraw := func() (screenOutcome, int, bool) {
		w, h := scr.Size()
		if err := draw(w, h); err != nil {
			outcome, code := fail(err)
			return outcome, code, true
		}
		return screenUnavailable, -1, false
	}

	for {
		ev := <-scr.Events()
		switch ev.Kind {
		case tui.EventResize:
			if err := draw(ev.Width, ev.Height); err != nil {
				return fail(err)
			}
		case tui.EventSignal:
			scr.Close()
			if sig, ok := ev.Signal.(syscall.Signal); ok {
				return screenExited, 128 + int(sig)
			}
			return screenExited, exitRuntime
		case tui.EventEOF:
			return quit()
		case tui.EventKey:
			if info != nil {
				// The package-info view returns to the summary; it never
				// confirms or refuses the transaction, and every other key
				// is ignored.
				changed := false
				switch ev.Key.Type {
				case term.KeyEsc:
					info = nil
					changed = true
				case term.KeyRune:
					if ev.Key.Rune == 'q' || ev.Key.Rune == 'Q' {
						info = nil
						changed = true
					}
				}
				if !changed {
					continue
				}
				if outcome, code, failed := redraw(); failed {
					return outcome, code
				}
				continue
			}
			beforeActive, beforeSel := model.Active, model.Sel
			handled := true
			switch ev.Key.Type {
			case term.KeyRune:
				switch ev.Key.Rune {
				case 'j':
					model.MoveRow(1)
				case 'k':
					model.MoveRow(-1)
				case 'h':
					model.MoveTab(-1)
				case 'l':
					model.MoveTab(1)
				case 's':
					info = summaryPackageInfo(model)
				case 'y', 'Y':
					if policy.Confirm {
						scr.Close()
						return screenConfirmed, -1
					}
				case 'n', 'N':
					if policy.Confirm {
						return quit()
					}
				case 'q', 'Q':
					return quit()
				default:
					handled = false
				}
			case term.KeyDown:
				model.MoveRow(1)
			case term.KeyUp:
				model.MoveRow(-1)
			case term.KeyLeft:
				model.MoveTab(-1)
			case term.KeyRight:
				model.MoveTab(1)
			case term.KeyHome:
				model.SetSelection(0)
			case term.KeyEnd:
				model.SetSelection(model.RowCount() - 1)
			case term.KeyPgUp:
				model.MoveRow(-10)
			case term.KeyPgDn:
				model.MoveRow(10)
			case term.KeyWheelUp:
				model.MoveRow(-1)
			case term.KeyWheelDown:
				model.MoveRow(1)
			case term.KeyEsc, term.KeyCtrlC:
				return quit()
			default:
				// Enter is deliberately unbound (FR-068: changelog N/A).
				handled = false
			}
			changed := model.Active != beforeActive || model.Sel != beforeSel
			if info != nil {
				changed = true
			}
			if !handled || !changed {
				continue
			}
			if outcome, code, failed := redraw(); failed {
				return outcome, code
			}
		}
	}
}

// summaryPackageInfo builds the read-only package-info view for the selected
// summary row. It uses only the model's data so the history detail (#77) can
// reuse it without another xbps query.
func summaryPackageInfo(model *tui.SummaryModel) *tui.PackageInfoModel {
	row, ok := model.SelectedRow()
	if !ok {
		return nil
	}
	tab, _ := model.ActiveTab()
	info := &tui.PackageInfoModel{
		Title: model.Title,
		Help:  []string{"(q) back to summary"},
		Fields: []tui.InfoField{
			{Label: "Package", Value: row.Name},
			{Label: "Operation", Value: tab.Label},
		},
	}
	add := func(label, value string) {
		if value != "" {
			info.Fields = append(info.Fields, tui.InfoField{Label: label, Value: value})
		}
	}
	if row.Old != "" && row.Old != row.New {
		add("Installed", row.Old)
	}
	if row.New != "" {
		add("Available", row.New)
	} else {
		add("Version", row.Old)
	}
	if row.Reason != "" {
		add("Reason", row.Reason)
	}
	add("Size", row.Size)
	return info
}

// planToTui converts the grouping and lookups the text summary already needs
// into the S1 model. #77 reuses the renderer with History = true.
func (st *state) planToTui(cmd string, rows []xbps.Row) *tui.SummaryModel {
	return tuiModelFromSummary(st.planToSummary(cmd, rows), cmd, st.unit)
}

// tuiModelFromSummary is the testable conversion; planToTui is the state-bound
// wrapper that also performs the lookups.
func tuiModelFromSummary(sum *summary.Summary, command string, unit summary.SizeUnit) *tui.SummaryModel {
	model := &tui.SummaryModel{
		Title: "xbpretty " + screenTitle(command),
		Help:  screenHelp(command),
	}
	for _, sec := range screenSections(sum, command) {
		if len(sec.rows) == 0 {
			continue
		}
		tab := tui.Tab{Label: sec.label}
		for _, row := range sec.rows {
			tab.Rows = append(tab.Rows, tuiRow(sum, sec.label, row, unit))
		}
		model.Tabs = append(model.Tabs, tab)
	}
	if sum.Download > 0 {
		model.Download = summary.WidgetUnit(sum.Download, unit)
	}
	delta := sum.NetDiskBytes()
	if delta >= 0 {
		model.DiskLabel = "Disk space required:"
		model.Disk = summary.WidgetUnit(uint64(delta), unit)
	} else {
		model.DiskLabel = "Disk space to free:"
		model.Disk = summary.WidgetUnit(uint64(-delta), unit)
	}
	return model
}

// screenSection is one tab's source rows.
type screenSection struct {
	label string
	rows  []summary.Row
}

// screenSections returns the non-empty sections in nala's fixed order, the
// same order the inline summary uses.
func screenSections(s *summary.Summary, command string) []screenSection {
	var out []screenSection
	if len(s.Remove) > 0 {
		out = append(out, screenSection{label: screenRemoveLabel(command), rows: s.Remove})
	}
	if len(s.Install) > 0 {
		out = append(out, screenSection{label: "Install", rows: s.Install})
	}
	if len(s.ReInstall) > 0 {
		out = append(out, screenSection{label: "ReInstall", rows: s.ReInstall})
	}
	if len(s.Upgrade) > 0 {
		out = append(out, screenSection{label: "Upgrade", rows: s.Upgrade})
	}
	if len(s.Configure) > 0 {
		out = append(out, screenSection{label: "Configure", rows: s.Configure})
	}
	if len(s.Hold) > 0 {
		out = append(out, screenSection{label: "Held", rows: s.Hold})
	}
	return out
}

func screenRemoveLabel(command string) string {
	switch command {
	case "autoremove":
		return "AutoRemove"
	case "purge":
		return "Purge"
	case "autopurge":
		return "AutoPurge"
	}
	return "Remove"
}

// tuiRow converts one plan row, applying the operation's Size: semantics and
// the version diff.
func tuiRow(s *summary.Summary, label string, row summary.Row, unit summary.SizeUnit) tui.PkgRow {
	out := tui.PkgRow{Name: row.Name, Reason: row.Reason}
	var size uint64
	hasSize := false
	switch label {
	case "Remove", "AutoRemove", "Purge", "AutoPurge", "Configure":
		size, hasSize = row.InstalledSize, row.HasInstalled
	default:
		if row.HasDownload {
			size, hasSize = row.DownloadSize, true
		} else {
			size, hasSize = row.InstalledSize, row.HasInstalled
		}
	}
	if hasSize {
		out.Size = summary.HumanUnit(size, unit)
	} else {
		out.Size = "—"
	}
	old := s.Old[row.Name]
	switch label {
	case "Upgrade":
		if old.Version != "" {
			out.Old = old.Version
		} else {
			out.Old = "—"
		}
		out.New = row.Version
		if old.Version != "" && old.Version != row.Version {
			out.NewSpans = tui.VersionSpans(old.Version, row.Version)
		}
	case "Held":
		out.Old = row.Version
		out.New = row.Version
		if old.Version != "" {
			out.Old = old.Version
		}
		if out.Old != out.New {
			out.NewSpans = tui.VersionSpans(out.Old, out.New)
		}
	default:
		out.New = row.Version
	}
	return out
}

// screenTitle maps the command to FR-068's operation-derived title.
func screenTitle(command string) string {
	switch command {
	case "install":
		return "Install"
	case "remove":
		return "Remove"
	case "purge":
		return "Purge"
	case "autoremove":
		return "Autoremove"
	case "autopurge":
		return "Autopurge"
	case "upgrade", "full-upgrade", "safe-upgrade":
		return "Upgrade"
	case "undo":
		return "Undo"
	case "redo":
		return "Redo"
	case "history":
		return "History"
	}
	return "Upgrade"
}

// screenHelp is FR-068's help block for S1.
func screenHelp(command string) []string {
	word := command
	if word == "full-upgrade" || word == "safe-upgrade" {
		word = "upgrade"
	}
	return []string{
		"(↑) move up | (↓) move down",
		"(→) next tab | (←) previous tab",
		"(s) package info | (q) quit | (y) start " + word,
	}
}

// screenTheme converts [Color.theme]'s nine roles into cell styles, so the
// TUI honours a custom palette and --color never falls out of the flush.
func screenTheme(t config.Theme) tui.Theme {
	return tui.Theme{
		Primary:   screenStyle(t.Primary),
		Secondary: screenStyle(t.Secondary),
		Highlight: screenStyle(t.Highlight),
		Regular:   screenStyle(t.Regular),
		Notice:    screenStyle(t.Notice),
		Warning:   screenStyle(t.Warning),
		Error:     screenStyle(t.Error),
	}
}

func screenStyle(s config.Style) term.Style {
	out := term.Style{FG: screenColor(s.Fg), Mods: screenMods(s.Modifier)}
	if s.HasBg {
		out.BG = screenColor(s.Bg)
	}
	return out
}

func screenMods(m config.Modifiers) term.Mods {
	var out term.Mods
	for _, entry := range []struct {
		bit config.Modifiers
		mod term.Mods
	}{
		{config.ModifierBold, term.Bold},
		{config.ModifierDim, term.Dim},
		{config.ModifierItalic, term.Italic},
		{config.ModifierUnderlined, term.Underline},
		{config.ModifierSlowBlink, term.Blink},
		{config.ModifierRapidBlink, term.Blink},
		{config.ModifierReversed, term.Reverse},
		{config.ModifierHidden, term.Hidden},
		{config.ModifierCrossedOut, term.Strike},
	} {
		if m.Has(entry.bit) {
			out |= entry.mod
		}
	}
	return out
}

func screenColor(c config.Color) term.Color {
	switch c.Kind {
	case config.ColorReset:
		return term.Default
	case config.ColorBlack:
		return term.ANSI(0)
	case config.ColorRed:
		return term.ANSI(1)
	case config.ColorGreen:
		return term.ANSI(2)
	case config.ColorYellow:
		return term.ANSI(3)
	case config.ColorBlue:
		return term.ANSI(4)
	case config.ColorMagenta:
		return term.ANSI(5)
	case config.ColorCyan:
		return term.ANSI(6)
	case config.ColorGray:
		return term.ANSI(7)
	case config.ColorDarkGray:
		return term.ANSI(8)
	case config.ColorLightRed:
		return term.ANSI(9)
	case config.ColorLightGreen:
		return term.ANSI(10)
	case config.ColorLightYellow:
		return term.ANSI(11)
	case config.ColorLightBlue:
		return term.ANSI(12)
	case config.ColorLightMagenta:
		return term.ANSI(13)
	case config.ColorLightCyan:
		return term.ANSI(14)
	case config.ColorWhite:
		return term.ANSI(15)
	case config.ColorIndexed:
		return term.Color{Kind: term.Color256, N: c.Index}
	case config.ColorRGB:
		return term.Color{Kind: term.ColorRGB, R: c.R, G: c.G, B: c.B}
	}
	return term.Default
}

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
	// screenExited means the caller must return immediately (refusal, signal).
	screenExited
)

// maybeRunSummaryScreen opens S1 when every gate and the entry floor pass. It
// returns the outcome; the caller keeps the text path when it is unavailable.
func (st *state) maybeRunSummaryScreen(opts *options, rows []xbps.Row) (screenOutcome, int) {
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
	return st.runSummaryScreen(out, st.planToTui(rows))
}

// runSummaryScreen owns the S1 loop: enter, draw, key/resize/signal handling,
// and restore on every return path. The interaction policy here is the engine
// baseline: y confirms and the cancellation keys refuse. Navigation, tabs, the
// package-info view and the remaining bindings are #76's seam.
func (st *state) runSummaryScreen(out *os.File, model *tui.SummaryModel) (screenOutcome, int) {
	in, ok := asFile(st.stdin)
	if !ok {
		return screenUnavailable, -1
	}
	scr, err := tui.Open(in, out, st.color, true)
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

	draw := func(w, h int) error {
		return scr.Draw(tui.RenderSummary(model, w, h, screenTheme(st.r.cfg.Color.Theme)))
	}
	w, h := scr.Size()
	if err := draw(w, h); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: full-screen interface failed (%v); showing the text summary\n", err)
		return screenUnavailable, -1
	}

	for {
		ev := <-scr.Events()
		switch ev.Kind {
		case tui.EventResize:
			if err := draw(ev.Width, ev.Height); err != nil {
				// FR-067: a failed render warns once and lets the text path
				// finish the transaction with its own exit code.
				fmt.Fprintf(st.stderr, "xbpretty: full-screen interface failed (%v); showing the text summary\n", err)
				return screenUnavailable, -1
			}
		case tui.EventSignal:
			scr.Close()
			if sig, ok := ev.Signal.(syscall.Signal); ok {
				return screenExited, 128 + int(sig)
			}
			return screenExited, exitRuntime
		case tui.EventEOF:
			scr.Close()
			st.refuseScreen()
			return screenExited, exitRuntime
		case tui.EventKey:
			switch {
			case ev.Key.Type == term.KeyRune && (ev.Key.Rune == 'y' || ev.Key.Rune == 'Y'):
				// TODO(#76): the remaining bindings (s package info, tabs,
				// navigation) live behind this seam.
				scr.Close()
				return screenConfirmed, -1
			case ev.Key.Type == term.KeyRune && strings.ContainsRune("qQnN", ev.Key.Rune),
				ev.Key.Type == term.KeyEsc, ev.Key.Type == term.KeyCtrlC:
				scr.Close()
				st.refuseScreen()
				return screenExited, exitRuntime
			}
		}
	}
}

func (st *state) refuseScreen() {
	fmt.Fprintln(st.stderr, "xbpretty: user refused confirmation")
}

// planToTui converts the grouping and lookups the text summary already needs
// into the S1 model. #77 reuses the renderer with History = true.
func (st *state) planToTui(rows []xbps.Row) *tui.SummaryModel {
	command := ""
	if st.opts != nil {
		command = st.opts.command
	}
	return tuiModelFromSummary(st.planToSummary(rows), command, st.unit)
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
		model.Download = summary.WidgetSize(sum.Download)
	}
	delta := sum.NetDiskBytes()
	if delta >= 0 {
		model.DiskLabel = "Disk space required:"
		model.Disk = summary.WidgetSize(uint64(delta))
	} else {
		model.DiskLabel = "Disk space to free:"
		model.Disk = summary.WidgetSize(uint64(-delta))
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

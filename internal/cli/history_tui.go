package cli

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// historyScreenAllowed is FR-067's S2 predicate: the full-screen gate with no
// root, plan-size or transaction-flag condition. The history detail is
// read-only, so -y/-n/--simple/--dry-run have no meaning here.
func historyScreenAllowed(g summaryGate) bool {
	return g.Mode == config.ModeTui &&
		g.StdoutTTY && g.StdinTTY &&
		!strings.EqualFold(g.Term, "dumb") &&
		g.UTF8Locale
}

// historyScreenHelp is FR-069's help block: the S1 navigation help without the
// confirmation line the read-only surface does not offer.
func historyScreenHelp() []string {
	return []string{
		"(↑) move up | (↓) move down",
		"(→) next tab | (←) previous tab",
		"(s) package info | (q) quit",
	}
}

// historyToTui builds the read-only summary model over one parsed entry: the
// non-empty operation groups in FR-032's fixed order, and no download/disk
// data (the renderer's History footer is counts only).
func historyToTui(entry history.Entry, unit summary.SizeUnit) *tui.SummaryModel {
	model := &tui.SummaryModel{
		Title:   "xbpretty History",
		History: true,
		Help:    historyScreenHelp(),
	}
	for _, operation := range history.GroupOrder {
		tab := tui.Tab{Label: operation}
		for _, transition := range entry.Packages {
			if transition.Operation != operation {
				continue
			}
			tab.Rows = append(tab.Rows, historyPkgRow(operation, transition, unit))
		}
		if len(tab.Rows) == 0 {
			continue
		}
		model.Tabs = append(model.Tabs, tab)
	}
	return model
}

// historyPkgRow maps one recorded transition to the widget's row. Before is
// the old version, After the new one; the renderer picks the version column
// its operation shape uses. The size follows S1's per-operation semantics:
// installed size for removal/configure, download size first for the rest.
func historyPkgRow(operation string, transition history.Transition, unit summary.SizeUnit) tui.PkgRow {
	row := tui.PkgRow{
		Name:   transition.Name,
		Old:    historyStateVersion(transition.Before),
		New:    historyStateVersion(transition.After),
		Reason: heldReasonLabel(transition.HeldReason),
		Size:   historyRowSize(operation, transition, unit),
	}
	switch operation {
	case history.OpUpgrade, history.OpDowngrade, history.OpHeld:
		if row.Old != "" && row.New != "" && row.Old != row.New {
			row.NewSpans = tui.VersionSpans(row.Old, row.New)
		}
	}
	return row
}

// historyStateVersion is the recorded version, or empty when the state is absent (a
// removal has no After, an install no Before).
func historyStateVersion(state *history.PackageState) string {
	if state == nil || state.Version == nil {
		return ""
	}
	return *state.Version
}

// historyRowSize applies S1's Size: semantics to the recorded sizes. A zero
// size was not recorded, so the row shows the em dash.
func historyRowSize(operation string, transition history.Transition, unit summary.SizeUnit) string {
	switch operation {
	case history.OpRemove, history.OpAutoRemove, history.OpPurge, history.OpAutoPurge, history.OpConfigure:
		if transition.InstalledSize > 0 {
			return summary.HumanUnit(transition.InstalledSize, unit)
		}
	default:
		if transition.Size > 0 {
			return summary.HumanUnit(transition.Size, unit)
		}
		if transition.InstalledSize > 0 {
			return summary.HumanUnit(transition.InstalledSize, unit)
		}
	}
	return "—"
}

// maybeRunHistoryScreen opens S2 when the gate, the entry floor and a
// non-empty entry allow it. ok is false when the caller keeps the text detail.
func (st *state) maybeRunHistoryScreen(opts *options, entry history.Entry) (int, bool) {
	model := historyToTui(entry, st.unit)
	if len(model.Tabs) == 0 {
		// FR-069: an entry with no group rows is always the text detail.
		return 0, false
	}
	if !historyScreenAllowed(st.gateFor(opts, nil)) {
		return 0, false
	}
	out, ok := asFile(st.stdout)
	if !ok {
		return 0, false
	}
	// FR-067: below 60x18 at entry the text path is used with no warning.
	if w, h, err := term.Size(out); err != nil || !tui.Fits(w, h) {
		return 0, false
	}
	outcome, code := st.runSummaryScreenPolicy(out, model, summaryScreenPolicy{Confirm: false})
	switch outcome {
	case screenRefused:
		// FR-069: q/Q/Esc/Ctrl-C/EOF leave the read-only screen and exit 0.
		return exitOK, true
	case screenExited:
		// A signal keeps its 128+n code.
		return code, true
	case screenConfirmed:
		// Confirmation is disabled for S2; a caller that reported it still
		// leaves read-only.
		return exitOK, true
	}
	// Init/render failure: runSummaryScreenPolicy warned once and the text
	// detail finishes the read.
	return 0, false
}

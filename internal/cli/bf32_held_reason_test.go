package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/summary"
)

// TestHistoryHeldReasonDisplay pins BF-32: the stored ManualHold reason is
// shown as nala's Manual hold in the full-screen S2 row and the text detail.
// Any other reason passes through unchanged, and the stored JSON keeps
// ManualHold (FR-030).
func TestHistoryHeldReasonDisplay(t *testing.T) {
	cases := map[string]string{
		"ManualHold": "Manual hold",
		"Excluded":   "Excluded",
		"":           "",
	}
	for raw, want := range cases {
		if got := heldReasonLabel(raw); got != want {
			t.Errorf("heldReasonLabel(%q) = %q, want %q", raw, got, want)
		}
	}

	entry := history.Entry{
		ID:               7,
		Status:           history.StatusApplied,
		RequestedBy:      "tester (0)",
		Command:          []string{"upgrade"},
		RequestedTargets: []string{},
		Packages: []history.Transition{{
			Name:          "bash",
			Operation:     history.OpHeld,
			Before:        historyState("5.2_1"),
			After:         historyState("5.2_1"),
			InstalledSize: 1024 * 1024,
			HeldReason:    "ManualHold",
		}},
	}

	model := historyToTui(entry, summary.SizeBinary)
	var held []string
	for _, tab := range model.Tabs {
		if tab.Label != history.OpHeld {
			continue
		}
		for _, row := range tab.Rows {
			held = append(held, row.Reason)
		}
	}
	if len(held) != 1 || held[0] != "Manual hold" {
		t.Errorf("S2 Held reasons = %q, want [Manual hold]", held)
	}

	var detail bytes.Buffer
	writeHistoryDetail(&detail, entry)
	text := detail.String()
	if !strings.Contains(text, "Manual hold") || strings.Contains(text, "ManualHold") {
		t.Errorf("text detail does not show Manual hold:\n%s", text)
	}
}

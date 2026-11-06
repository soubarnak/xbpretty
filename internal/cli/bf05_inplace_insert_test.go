package cli

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// TestInPlaceInsertAboveRedrawsBox is BF-05: without an inline viewport the
// in-place renderer must write queued lines above the box instead of dropping
// them. The bytes are: carriage return, cursor up to the box top, clear to the
// end of the screen, each line, then the box again at the same height.
func TestInPlaceInsertAboveRedrawsBox(t *testing.T) {
	var out bytes.Buffer
	theme := summary.DefaultTheme()
	model := newProgressModel("install", nil, nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, theme)
	live.Request(true)

	lines := renderProgressViewThemed(model.view(), 80, false, theme)
	height := len(lines)
	if height < 2 {
		t.Fatalf("box height = %d, want a drawn box", height)
	}

	before := out.Len()
	live.Apply(true, func(m *progressModel) {
		m.inserts = append(m.inserts, "first", "second")
	})
	got := out.String()[before:]

	want := "\r" + "\x1b[" + strconv.Itoa(height-1) + "A" + "\x1b[J" +
		"first\r\nsecond\r\n" +
		strings.Join(lines, "\n")
	if got != want {
		t.Errorf("insert bytes:\n got %q\nwant %q", got, want)
	}
}

// TestPlainInsertAboveKeepsTheLineAndRedrawsThePlainLine covers the 40-cell
// line: the inserted line overwrites the plain line's row, which then moves
// down, and the plain line is drawn on the new row.
func TestPlainInsertAboveKeepsTheLineAndRedrawsThePlainLine(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("update", nil, nil)
	live := newLiveProgress(&out, false, 80, livePlainMode, model, summary.DefaultTheme())
	live.Request(true)

	before := out.Len()
	live.InsertAbove("No Change: https://repo.example/current x86_64-repodata")
	got := out.String()[before:]

	wantLine := "\r\x1b[2KNo Change: https://repo.example/current x86_64-repodata\n"
	if !strings.HasPrefix(got, wantLine) {
		t.Fatalf("plain insert = %q, want prefix %q", got, wantLine)
	}
	if !strings.Contains(got, "\r\x1b[2K[") {
		t.Errorf("plain line not redrawn after the insert: %q", got)
	}
}

// TestInsertAboveAfterFinishIsIgnored: a queued line after the surface closed
// has nowhere to go, so it writes nothing.
func TestInsertAboveAfterFinishIsIgnored(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("install", nil, nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	live.Request(true)
	live.Finish()
	before := out.Len()
	live.InsertAbove("late")
	if out.Len() != before {
		t.Errorf("a finished surface wrote %q", out.String()[before:])
	}
}

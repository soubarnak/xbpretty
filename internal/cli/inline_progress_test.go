package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// TestCompletionQueuesInsertLines pins the lines that become scrollback above
// the box: one per completed archive, one per completed package action, and
// none for a signature tick, a duplicate completion or an in-progress event.
func TestCompletionQueuesInsertLines(t *testing.T) {
	model := newProgressModel("install", downloadPlan(), nil)
	model.downloadDone(DownloadComplete{Filename: "alpha-1_1.x86_64.xbps"})
	model.downloadDone(DownloadComplete{Filename: "alpha-1_1.x86_64.xbps"})
	model.downloadDone(DownloadComplete{Filename: "alpha-1_1.x86_64.xbps.sig2"})
	want := []string{"Last completed: alpha-1_1.x86_64.xbps"}
	if strings.Join(model.inserts, "|") != strings.Join(want, "|") {
		t.Fatalf("download inserts = %q, want %q", model.inserts, want)
	}

	model.inserts = nil
	model.packageEvent(PackageEvent{Package: "alpha-1_1", Action: "unpacking"})
	if len(model.inserts) != 0 {
		t.Fatalf("an in-progress unpack queued %q", model.inserts)
	}
	for _, tc := range []struct{ action, want string }{
		{"installed", "alpha-1_1: installed successfully."},
		{"updated", "alpha-1_1: updated successfully."},
		{"configured", "alpha-1_1: configured successfully."},
		{"removed", "alpha-1_1: removed successfully."},
	} {
		model.inserts = nil
		model.packageEvent(PackageEvent{Package: "alpha-1_1", Action: tc.action})
		if len(model.inserts) != 1 || model.inserts[0] != tc.want {
			t.Errorf("action %q inserts = %q, want %q", tc.action, model.inserts, tc.want)
		}
	}
}

// TestLiveProgressInsertsAboveTheViewport joins the model's completion lines
// to #79's viewport: the completed line is written above the box and the box
// re-anchors below it on the same forced redraw.
func TestLiveProgressInsertsAboveTheViewport(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	vp := tui.NewViewport(&out, 80)
	vp.Start(tui.Anchor{OK: true})
	live.SetInline(vp, nil, false)
	live.Request(true)

	live.Apply(true, func(m *progressModel) {
		m.downloadDone(DownloadComplete{Filename: "alpha-1_1.x86_64.xbps"})
	})
	got := out.String()
	if strings.Count(got, "Last completed: alpha-1_1.x86_64.xbps\n") != 1 {
		t.Fatalf("completion line was not inserted exactly once:\n%q", got)
	}
	insertAt := strings.Index(got, "Last completed: alpha-1_1.x86_64.xbps\n")
	if boxAt := strings.LastIndex(got, "╭"); boxAt < insertAt {
		t.Fatalf("box redraw at %d is not below the inserted line at %d:\n%q", boxAt, insertAt, got)
	}
	// The insertion erases the box first, so the previous frame is gone.
	if !strings.Contains(got[:insertAt], term.MoveUp(heightDownload-1)+"\r"+"\x1b[J") {
		t.Errorf("insertion did not clear the previous box:\n%q", got)
	}
	live.Finish()
	if strings.Contains(out.String(), "Transaction complete") {
		t.Error("teardown printed a completion line")
	}
}

// TestLiveProgressAltScreenLatch pins FR-072's child latch: an enter marker
// hides the box without xbpretty writing one, a leave marker redraws it.
func TestLiveProgressAltScreenLatch(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	vp := tui.NewViewport(&out, 80)
	vp.Start(tui.Anchor{OK: true})
	live.SetInline(vp, nil, false)
	live.Request(true)

	live.ObserveChildOutput("child: entering full screen " + term.AltEnter)
	if !vp.Hidden() {
		t.Fatal("AltEnter did not hide the viewport")
	}
	if strings.Contains(out.String(), term.AltEnter) {
		t.Error("xbpretty echoed the child's alternate-screen enter")
	}
	hiddenLen := out.Len()
	live.ObserveChildOutput(term.AltEnter)
	if out.Len() != hiddenLen {
		t.Error("a repeated AltEnter drew something")
	}
	live.ObserveChildOutput(term.AltLeave)
	if vp.Hidden() {
		t.Fatal("AltLeave did not show the viewport")
	}
	if !strings.Contains(out.String()[hiddenLen:], "╭") {
		t.Errorf("AltLeave did not redraw the box:\n%q", out.String()[hiddenLen:])
	}
	if strings.Contains(out.String(), term.AltLeave) {
		t.Error("xbpretty echoed the child's alternate-screen leave")
	}
}

// TestStartInlineOnlyForTheWidget keeps FR-065's plain line untouched: the
// 40-cell form never grows a box, and a stream that is not a file cannot be
// anchored or switched to cbreak.
func TestStartInlineOnlyForTheWidget(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("download", downloadPlan(), nil)
	plain := newLiveProgress(&out, false, 80, livePlainMode, model, summary.DefaultTheme())
	st := &state{stderr: &out, stdin: strings.NewReader("")}
	if got := st.startInline(plain); got != nil {
		t.Fatalf("startInline built a viewport for the plain line")
	}

	widget := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	if got := st.startInline(widget); got != nil {
		t.Fatalf("startInline built a viewport for a non-file stream")
	}
	if strings.Contains(out.String(), "╭") || strings.Contains(out.String(), term.AltEnter) {
		t.Errorf("startInline drew without a viewport: %q", out.String())
	}
}

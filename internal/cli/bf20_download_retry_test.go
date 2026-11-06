package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/download"
	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// TestDownloadEventViewRetryResets pins BF-20: a retried attempt starts its byte
// count again, so the bytes of the dead attempt are not counted twice, and a
// failed file leaves the position instead of staying in it.
func TestDownloadEventViewRetryResets(t *testing.T) {
	var out bytes.Buffer
	file := "alpha-1.0_1.x86_64.xbps"
	rows := []xbps.Row{{Name: "alpha", Version: "1.0_1", Action: xbps.ActionInstall, Arch: "x86_64", HasDownload: true, DownloadSize: 1000}}
	model := newProgressModel("download", rows, nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	now := time.Unix(0, 0)
	live.now = func() time.Time { return now }
	live.after = func(time.Duration, func()) *time.Timer { return time.NewTimer(time.Hour) }
	defer live.Finish()
	view := &downloadEventView{live: live, written: map[string]int64{}}

	view.apply(download.Event{Kind: download.EventFileStart, File: file, Total: 1000})
	view.apply(download.Event{Kind: download.EventFileTick, File: file, Bytes: 800, Total: 1000})
	if got, want := model.position(), uint64(800); got != want {
		t.Fatalf("position at 80%% = %d, want %d", got, want)
	}

	view.apply(download.Event{Kind: download.EventRetry, File: file, Attempt: 1})
	view.apply(download.Event{Kind: download.EventFileTick, File: file, Bytes: 100, Total: 1000})
	if got, want := model.position(), uint64(100); got != want {
		t.Errorf("position after a retry at 10%% = %d, want %d (the retry restarts the count)", got, want)
	}

	view.apply(download.Event{Kind: download.EventFileFailed, File: file, Attempt: 4})
	if got := model.position(); got != 0 {
		t.Errorf("position after the file failed = %d, want 0", got)
	}
	if got := model.view().left[0].value; got != "0/1" {
		t.Errorf("Packages after a failed file = %q, want 0/1", got)
	}
}

package cli

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/xbps"
)

// forcePTYFailure makes the hybrid engine fall back to the direct path, the
// way it does where no PTY can be opened (for example a chroot without
// devpts). The original opener is restored when the test ends.
func forcePTYFailure(t *testing.T) {
	t.Helper()
	saved := openPtyFn
	openPtyFn = func() (*os.File, *os.File, error) {
		return nil, nil, errors.New("pty unavailable")
	}
	t.Cleanup(func() { openPtyFn = saved })
}

// TestDirectLiveFeedsParser is BF-46: when the PTY cannot be opened, the live
// run used to fall back to the direct path, which never fed the telemetry
// parser, so the widget stayed at 0% for the whole run. The child reports one
// archive at 100% on stderr; the progress model built from the same plan must
// reach 100% too.
func TestDirectLiveFeedsParser(t *testing.T) {
	forcePTYFailure(t)

	row := xbps.Row{
		Name:         "pkg",
		Version:      "1.0_1",
		Arch:         "x86_64",
		Action:       xbps.ActionUpdate,
		DownloadSize: 1024,
	}
	model := newProgressModel("upgrade", []xbps.Row{row}, nil)

	r := &runner{live: true, stdout: io.Discard, stderr: io.Discard, stdin: strings.NewReader("")}
	r.parser = NewStreamParser(1, 1024)
	r.parser.OnDownloadTick = func(tick DownloadTick) {
		model.downloadTick(tick)
	}

	tick := "[100%] pkg-1.0_1.x86_64.xbps: [1KB 100%] 1MB/s ETA: 00m00s"
	code, err := r.stream("/bin/sh", "-c", "echo '"+tick+"' >&2")
	if err != nil {
		t.Fatalf("stream returned %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	if got := r.parser.State().Percent; got != 100 {
		t.Errorf("parser percent = %v, want 100", got)
	}
	if got := model.ratio(); got != 1 {
		t.Errorf("progress ratio = %v, want 1", got)
	}
}

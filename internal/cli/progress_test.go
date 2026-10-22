package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

func TestProgressWidgetDownloadPhaseRender(t *testing.T) {
	var buf bytes.Buffer
	w := NewProgressWidget(&buf, "rounded", true, 80, 5, 1024*1024*10)

	parser := NewStreamParser(5, 1024*1024*10)
	w.AttachToParser(parser)

	// Feed download tick
	parser.FeedStderr("[ 20%] pkg-1.0_1.x86_64.xbps: [2048KB 20%] 5.2MB/s ETA: 00m02s\x1b[K\r")

	output := buf.String()
	if !strings.Contains(output, "Downloading...") {
		t.Errorf("expected 'Downloading...' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Total Packages:") {
		t.Errorf("expected 'Total Packages:' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Downloading:") {
		t.Errorf("expected 'Downloading:' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Time Remaining:") {
		t.Errorf("expected 'Time Remaining:' in output, got:\n%s", output)
	}
	// Cyan fill (\x1b[1;36m or \x1b[36m) and Red track (\x1b[31m or \x1b[1;31m)
	if !strings.Contains(output, "\x1b[1;36m") && !strings.Contains(output, "\x1b[36m") {
		t.Errorf("expected cyan progress bar in output, got:\n%s", output)
	}
	if !strings.Contains(output, "\x1b[31m") && !strings.Contains(output, "\x1b[1;31m") {
		t.Errorf("expected red track in output, got:\n%s", output)
	}
	// ETA and Speed
	if !strings.Contains(output, "00m02s") {
		t.Errorf("expected ETA '00m02s' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "5.2MB/s") {
		t.Errorf("expected rate '5.2MB/s' in output, got:\n%s", output)
	}
}

func TestProgressWidgetBoxStyles(t *testing.T) {
	tests := []struct {
		style     string
		wantTop   string
		wantBot   string
		wantRune  string
		avoidRune string
	}{
		{
			style:     "rounded",
			wantTop:   "╭── Downloading...",
			wantBot:   "╰",
			wantRune:  "╭",
			avoidRune: "┌",
		},
		{
			style:     "classic",
			wantTop:   "┌── Downloading...",
			wantBot:   "└",
			wantRune:  "┌",
			avoidRune: "╭",
		},
		{
			style:     "ascii",
			wantTop:   "+-- Downloading...",
			wantBot:   "+",
			wantRune:  "+",
			avoidRune: "╭",
		},
	}

	for _, tt := range tests {
		t.Run(tt.style, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewProgressWidget(&buf, tt.style, false, 80, 2, 1000)
			p := NewStreamParser(2, 1000)
			w.AttachToParser(p)
			p.FeedStderr("[ 10%] test-1.0_1.x86_64.xbps: [100B 10%] 1MB/s ETA: 00m01s\n")

			out := buf.String()
			if !strings.Contains(out, tt.wantTop) {
				t.Errorf("style %s missing top header %q, got:\n%s", tt.style, tt.wantTop, out)
			}
			if !strings.Contains(out, tt.wantBot) {
				t.Errorf("style %s missing bottom corner %q, got:\n%s", tt.style, tt.wantBot, out)
			}
			if strings.Contains(out, tt.avoidRune) {
				t.Errorf("style %s contains unwanted rune %q, got:\n%s", tt.style, tt.avoidRune, out)
			}
		})
	}
}

func TestProgressWidgetUnpackConfigurePhaseRender(t *testing.T) {
	var buf bytes.Buffer
	w := NewProgressWidget(&buf, "rounded", true, 80, 2, 0)

	parser := NewStreamParser(2, 0)
	w.AttachToParser(parser)

	// Trigger unpack phase
	parser.FeedStdout("[*] Unpacking packages\n")
	parser.FeedStdout("pkg-a-1.0_1: unpacking ...\n")

	output := buf.String()
	if !strings.Contains(output, "Running xbps...") {
		t.Errorf("expected 'Running xbps...' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Phase:") {
		t.Errorf("expected 'Phase:' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Current Target:") {
		t.Errorf("expected 'Current Target:' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "pkg-a-1.0_1") {
		t.Errorf("expected target pkg name in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Status:") {
		t.Errorf("expected 'Status:' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "actions") {
		t.Errorf("expected 'actions' in output, got:\n%s", output)
	}
}

func TestProgressWidgetTeardownSuccess(t *testing.T) {
	var buf bytes.Buffer
	w := NewProgressWidget(&buf, "rounded", true, 80, 1, 1000)
	parser := NewStreamParser(1, 1000)
	w.AttachToParser(parser)

	parser.FeedStderr("[ 50%] test-1.0_1.x86_64.xbps: [500B 50%] 1MB/s ETA: 00m01s\n")
	parser.FeedStdout("1 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.\n")

	rows := []xbps.Row{{Name: "test", Version: "1.0_1", Action: "install"}}
	w.Finish(0, "", rows, "install")

	out := buf.String()
	// Should contain atomic clear escape sequence \x1b[5A\r\x1b[0J
	if !strings.Contains(out, "\x1b[5A\r\x1b[0J") {
		t.Errorf("expected atomic clear in teardown, got:\n%s", out)
	}
	// Should contain permanent completion line
	if !strings.Contains(out, "✔ Transaction complete:") {
		t.Errorf("expected completion line in teardown, got:\n%s", out)
	}
	if !strings.Contains(out, "1 installed") {
		t.Errorf("expected '1 installed' in summary, got:\n%s", out)
	}
}

func TestProgressWidgetTeardownFailure(t *testing.T) {
	var buf bytes.Buffer
	w := NewProgressWidget(&buf, "rounded", true, 80, 1, 1000)
	parser := NewStreamParser(1, 1000)
	w.AttachToParser(parser)

	parser.FeedStderr("[ 50%] test-1.0_1.x86_64.xbps: [500B 50%] 1MB/s ETA: 00m01s\n")

	rawErr := "ERROR: failed to verify signature\n"
	w.Finish(1, rawErr, nil, "install")

	out := buf.String()
	// Should clear the 5 lines with atomic clear sequence \x1b[5A\r\x1b[0J
	if !strings.Contains(out, "\x1b[5A\r\x1b[0J") {
		t.Errorf("expected atomic clear in teardown, got:\n%s", out)
	}
	// Should NOT contain completion banner on failure
	if strings.Contains(out, "✔ Transaction complete:") {
		t.Errorf("did not expect completion line on failure, got:\n%s", out)
	}
}

func TestProgressWidgetExactLineWidths(t *testing.T) {
	widths := []int{60, 80, 100, 120, 180}
	styles := []string{"rounded", "classic", "ascii"}
	colors := []bool{true, false}

	for _, width := range widths {
		for _, style := range styles {
			for _, color := range colors {
				// 1. Download phase
				var dlBuf bytes.Buffer
				wDl := NewProgressWidget(&dlBuf, style, color, width, 5, 1024*1024*50)
				pDl := NewStreamParser(5, 1024*1024*50)
				wDl.AttachToParser(pDl)
				pDl.FeedStderr("[ 40%] firefox-158.0.1_1.x86_64.xbps: [20MB 40%] 12.5MB/s ETA: 00m03s\r")

				dlLines := strings.Split(strings.TrimSuffix(dlBuf.String(), "\n"), "\n")
				if len(dlLines) != 5 {
					t.Fatalf("expected 5 lines in dl box, got %d for width %d, style %s, color %v", len(dlLines), width, style, color)
				}
				for i, line := range dlLines {
					vw := summary.VisibleWidth(line)
					if vw != width {
						t.Errorf("dl width %d %s color=%v: line %d visible width = %d, want %d:\n%q",
							width, style, color, i, vw, width, line)
					}
				}

				// 2. Action / Unpack phase
				var actBuf bytes.Buffer
				wAct := NewProgressWidget(&actBuf, style, color, width, 5, 0)
				pAct := NewStreamParser(5, 0)
				wAct.AttachToParser(pAct)
				pAct.FeedStdout("[*] Unpacking packages\n")
				actBuf.Reset()
				pAct.FeedStdout("firefox-158.0.1_1: unpacking ...\n")

				actOutput := strings.TrimPrefix(actBuf.String(), "\x1b[5A\r")
				actLines := strings.Split(strings.TrimSuffix(actOutput, "\n"), "\n")
				if len(actLines) != 5 {
					t.Fatalf("expected 5 lines in act box, got %d for width %d, style %s, color %v", len(actLines), width, style, color)
				}
				for i, line := range actLines {
					vw := summary.VisibleWidth(line)
					if vw != width {
						t.Errorf("act width %d %s color=%v: line %d visible width = %d, want %d:\n%q",
							width, style, color, i, vw, width, line)
					}
				}
			}
		}
	}
}

func TestProgressWidgetDynamicResize(t *testing.T) {
	var buf bytes.Buffer
	w := NewProgressWidget(&buf, "rounded", false, 80, 2, 1000)
	p := NewStreamParser(2, 1000)
	w.AttachToParser(p)

	// Initial render at 80 cols
	p.FeedStderr("[ 10%] pkg-1.0_1.x86_64.xbps: [100B 10%] 1MB/s ETA: 00m01s\r")
	lines80 := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	for i, l := range lines80 {
		if vw := summary.VisibleWidth(l); vw != 80 {
			t.Errorf("line %d before resize visible width = %d, want 80", i, vw)
		}
	}

	// Resize to 120 cols
	w.SetWidth(120)
	buf.Reset()
	p.FeedStderr("[ 20%] pkg-1.0_1.x86_64.xbps: [200B 20%] 1MB/s ETA: 00m01s\r")
	out120 := strings.TrimPrefix(buf.String(), "\x1b[5A\r")
	lines120 := strings.Split(strings.TrimSuffix(out120, "\n"), "\n")
	for i, l := range lines120 {
		if vw := summary.VisibleWidth(l); vw != 120 {
			t.Errorf("line %d after resize visible width = %d, want 120", i, vw)
		}
	}

	// Resize down to 60 cols
	w.SetWidth(60)
	buf.Reset()
	p.FeedStderr("[ 30%] pkg-1.0_1.x86_64.xbps: [300B 30%] 1MB/s ETA: 00m01s\r")
	out60 := strings.TrimPrefix(buf.String(), "\x1b[5A\r")
	lines60 := strings.Split(strings.TrimSuffix(out60, "\n"), "\n")
	for i, l := range lines60 {
		if vw := summary.VisibleWidth(l); vw != 60 {
			t.Errorf("line %d after shrink visible width = %d, want 60", i, vw)
		}
	}
}

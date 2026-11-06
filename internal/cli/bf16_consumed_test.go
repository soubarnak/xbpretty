package cli

import (
	"bytes"
	"testing"
)

// TestTelemetryConsumed is BF-16's table: a line is consumed when the live view
// draws it (a phase, package event, transfer, repository line or summary). Any
// other child line is shown above the box, so it must report not consumed.
func TestTelemetryConsumed(t *testing.T) {
	stdout := []struct {
		line     string
		consumed bool
	}{
		{"[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...", true},
		{"[*] Downloading packages", true},
		{"[*] Verifying package integrity", true},
		{"[*] Unpacking packages", true},
		{"curl-8.22.0_1: unpacking ...", true},
		{"curl-8.22.0_1: configuring ...", true},
		{"curl-8.22.0_1: installed successfully.", true},
		{"2 downloaded, 2 installed, 0 updated, 0 configured, 0 removed, 0 on hold.", true},
		{"[ 40%] curl-8.22.0_1.x86_64.xbps: [1MB 40%] 2MB/s ETA: 00m01s", true},
		{"curl-8.22.0_1.x86_64.xbps: 1MB [avg rate: 2MB/s]", true},
		{"x86_64-repodata: 1854KB [avg rate: 2391KB/s]", true},
		{"[*] curl-8.22.0_1 install message:", false},
		{"Run update-grub to refresh the boot menu", false},
		{"Do you want to import this public key? [Y/n] ", false},
		{"", false},
	}
	for _, tc := range stdout {
		t.Run("stdout "+tc.line, func(t *testing.T) {
			if got := NewStreamParser(0, 0).FeedStdout(tc.line); got != tc.consumed {
				t.Errorf("FeedStdout(%q) consumed = %v, want %v", tc.line, got, tc.consumed)
			}
		})
	}

	stderr := []struct {
		line     string
		consumed bool
	}{
		{"[ 40%] x86_64-repodata: [1854KB 40%] 2391KB/s ETA: 00m01s", true},
		{"curl-8.22.0_1.x86_64.xbps: 1MB [avg rate: 2MB/s]", true},
		{"ERROR: [reposync] failed to fetch file `https://repo-default.voidlinux.org/current/x86_64-repodata': Connection refused", true},
		{"WARNING: x", false},
		{"ERROR: failed to open repodata: /var/db/xbps/x86_64-repodata", false},
	}
	for _, tc := range stderr {
		t.Run("stderr "+tc.line, func(t *testing.T) {
			if got := NewStreamParser(0, 0).FeedStderr(tc.line); got != tc.consumed {
				t.Errorf("FeedStderr(%q) consumed = %v, want %v", tc.line, got, tc.consumed)
			}
		})
	}
}

// TestControlOnlyLinesAreNotShown: a child's alternate-screen switch and erase
// sequences are controls. Cleaned, they are empty, so the live view never
// inserts them above the box (BF-16).
func TestControlOnlyLinesAreNotShown(t *testing.T) {
	for _, line := range []string{"\x1b[?1049h", "\x1b[?1049l\r", "\x1b[K", "\x1b[?25l\x1b[?2004h"} {
		if got := cleanLine(line); got != "" {
			t.Errorf("cleanLine(%q) = %q, want empty", line, got)
		}
	}
	if got := cleanLine("\x1b[1;92mWARNING:\x1b[0m x"); got != "WARNING: x" {
		t.Errorf("cleanLine kept the text wrong: %q", got)
	}
}

// TestCapturedDumpSkipsLinesAlreadyShown: a failed child's captured output is
// printed except the lines the live view already inserted above its box, each
// shown line matched once. Everything else prints in the captured order.
func TestCapturedDumpSkipsLinesAlreadyShown(t *testing.T) {
	captured := "[*] curl-8.22.0_1 install message:\nRun update-grub\n"
	shown := map[string]int{"[*] curl-8.22.0_1 install message:": 1}
	var out bytes.Buffer
	printUnshownCaptured(&out, captured, shown)
	if want := "Run update-grub\n"; out.String() != want {
		t.Errorf("dump = %q, want %q", out.String(), want)
	}

	var twice bytes.Buffer
	printUnshownCaptured(&twice, "WARNING: x\nWARNING: x\n", map[string]int{"WARNING: x": 1})
	if want := "WARNING: x\n"; twice.String() != want {
		t.Errorf("a line shown once printed twice in the dump: %q, want %q", twice.String(), want)
	}
}

package cli

import (
	"strings"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"512B", 512, false},
		{"87KB", 87 * 1024, false},
		{"8956KB", 8956 * 1024, false},
		{"12MB", 12 * 1024 * 1024, false},
		{"1.5GB", int64(1.5 * 1024 * 1024 * 1024), false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseDownloadTick(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantOverall int
		wantFile    string
		wantPkg     string
		wantSize    string
		wantPct     int
		wantRate    string
		wantETA     string
	}{
		{
			name:        "tty mode signature tick",
			line:        "[ 0%] libxcrypt-compat-4.4.38_1.x86_64.xbps.sig2: [512B 100%] 54MB/s ETA: 00m00s\x1b[K\r",
			wantOverall: 0,
			wantFile:    "libxcrypt-compat-4.4.38_1.x86_64.xbps.sig2",
			wantPkg:     "libxcrypt-compat-4.4.38_1",
			wantSize:    "512B",
			wantPct:     100,
			wantRate:    "54MB/s",
			wantETA:     "00m00s",
		},
		{
			name:        "tty mode package tick",
			line:        "[81%] glibc-2.41_1.x86_64.xbps: [8956KB 82%] 5628KB/s ETA: 00m06s\x1b[K\r",
			wantOverall: 81,
			wantFile:    "glibc-2.41_1.x86_64.xbps",
			wantPkg:     "glibc-2.41_1",
			wantSize:    "8956KB",
			wantPct:     82,
			wantRate:    "5628KB/s",
			wantETA:     "00m06s",
		},
		{
			name:        "piped mode tick without overall percent",
			line:        "glibc-2.41_1.x86_64.xbps: [8956KB 82%] 5628KB/s ETA: 00m06s\n",
			wantOverall: -1,
			wantFile:    "glibc-2.41_1.x86_64.xbps",
			wantPkg:     "glibc-2.41_1",
			wantSize:    "8956KB",
			wantPct:     82,
			wantRate:    "5628KB/s",
			wantETA:     "00m06s",
		},
		{
			name:        "stalled rate tick",
			line:        "[10%] pkg-1.0_1.x86_64.xbps: [10MB 10%] -- stalled -- ETA: unknown\x1b[K\r",
			wantOverall: 10,
			wantFile:    "pkg-1.0_1.x86_64.xbps",
			wantPkg:     "pkg-1.0_1",
			wantSize:    "10MB",
			wantPct:     10,
			wantRate:    "-- stalled --",
			wantETA:     "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tick, ok := ParseDownloadTick(tt.line)
			if !ok {
				t.Fatalf("ParseDownloadTick(%q) failed", tt.line)
			}
			if tick.OverallPercent != tt.wantOverall {
				t.Errorf("OverallPercent = %d, want %d", tick.OverallPercent, tt.wantOverall)
			}
			if tick.Filename != tt.wantFile {
				t.Errorf("Filename = %q, want %q", tick.Filename, tt.wantFile)
			}
			if tick.PackageName != tt.wantPkg {
				t.Errorf("PackageName = %q, want %q", tick.PackageName, tt.wantPkg)
			}
			if tick.TotalSizeStr != tt.wantSize {
				t.Errorf("TotalSizeStr = %q, want %q", tick.TotalSizeStr, tt.wantSize)
			}
			if tick.FilePercent != tt.wantPct {
				t.Errorf("FilePercent = %d, want %d", tick.FilePercent, tt.wantPct)
			}
			if tick.Rate != tt.wantRate {
				t.Errorf("Rate = %q, want %q", tick.Rate, tt.wantRate)
			}
			if tick.ETA != tt.wantETA {
				t.Errorf("ETA = %q, want %q", tick.ETA, tt.wantETA)
			}
		})
	}
}

func TestParseDownloadComplete(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantFile string
		wantPkg  string
		wantSize string
		wantRate string
	}{
		{
			name:     "tty mode complete",
			line:     "libxcrypt-compat-4.4.38_1.x86_64.xbps: 87KB [avg rate: 2848MB/s]\x1b[K\n",
			wantFile: "libxcrypt-compat-4.4.38_1.x86_64.xbps",
			wantPkg:  "libxcrypt-compat-4.4.38_1",
			wantSize: "87KB",
			wantRate: "2848MB/s",
		},
		{
			name:     "piped mode complete",
			line:     "glibc-2.41_1.x86_64.xbps: 8956KB [avg rate: 6834KB/s]\n",
			wantFile: "glibc-2.41_1.x86_64.xbps",
			wantPkg:  "glibc-2.41_1",
			wantSize: "8956KB",
			wantRate: "6834KB/s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp, ok := ParseDownloadComplete(tt.line)
			if !ok {
				t.Fatalf("ParseDownloadComplete(%q) failed", tt.line)
			}
			if comp.Filename != tt.wantFile {
				t.Errorf("Filename = %q, want %q", comp.Filename, tt.wantFile)
			}
			if comp.PackageName != tt.wantPkg {
				t.Errorf("PackageName = %q, want %q", comp.PackageName, tt.wantPkg)
			}
			if comp.SizeStr != tt.wantSize {
				t.Errorf("SizeStr = %q, want %q", comp.SizeStr, tt.wantSize)
			}
			if comp.AvgRate != tt.wantRate {
				t.Errorf("AvgRate = %q, want %q", comp.AvgRate, tt.wantRate)
			}
		})
	}
}

func TestParseStdoutEvents(t *testing.T) {
	t.Run("phase headers", func(t *testing.T) {
		phases := []struct {
			line string
			want string
		}{
			{"[*] Updating repository 'https://repo.voidlinux.org/current' ...", "RepoSync"},
			{"[*] Downloading packages", "Downloading"},
			{"[*] Verifying package integrity", "Verifying"},
			{"[*] Collecting package files", "Collecting"},
			{"[*] Unpacking packages", "Unpacking"},
			{"[*] Configuring unpacked packages", "Configuring"},
		}

		for _, p := range phases {
			got, ok := ParsePhaseHeader(p.line)
			if !ok || got != p.want {
				t.Errorf("ParsePhaseHeader(%q) = (%q, %v), want (%q, true)", p.line, got, ok, p.want)
			}
		}
	})

	t.Run("package events", func(t *testing.T) {
		events := []struct {
			line       string
			wantPkg    string
			wantAction string
		}{
			{"glibc-2.41_1: verifying RSA signature...", "glibc-2.41_1", "verifying"},
			{"glibc-2.41_1: collecting files...", "glibc-2.41_1", "collecting"},
			{"glibc-2.41_1: unpacking ...", "glibc-2.41_1", "unpacking"},
			{"glibc-2.41_1: configuring ...", "glibc-2.41_1", "configuring"},
			{"glibc-2.41_1: installed successfully.", "glibc-2.41_1", "installed"},
			{"curl-8.2.1_1: updating to 8.2.1_2 ...", "curl-8.2.1_1", "updating"},
			{"curl-8.2.1_1: updated successfully.", "curl-8.2.1_1", "updated"},
			{"Removing 'wget-1.25.0_1' ...", "wget-1.25.0_1", "removing"},
			{"Removed 'wget-1.25.0_1' successfully.", "wget-1.25.0_1", "removed"},
		}

		for _, ev := range events {
			got, ok := ParsePackageEvent(ev.line)
			if !ok {
				t.Fatalf("ParsePackageEvent(%q) failed", ev.line)
			}
			if got.Package != ev.wantPkg {
				t.Errorf("Package = %q, want %q", got.Package, ev.wantPkg)
			}
			if got.Action != ev.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, ev.wantAction)
			}
		}
	})

	t.Run("transaction summary", func(t *testing.T) {
		line := "3 downloaded, 1 installed, 4 updated, 5 configured, 0 removed, 1 on hold."
		sum, ok := ParseTransactionSummary(line)
		if !ok {
			t.Fatalf("ParseTransactionSummary(%q) failed", line)
		}
		if sum.Downloaded != 3 || sum.Installed != 1 || sum.Updated != 4 || sum.Configured != 5 || sum.Removed != 0 || sum.OnHold != 1 {
			t.Errorf("summary = %+v, unexpected counts", sum)
		}
	})
}

func TestStreamParserState(t *testing.T) {
	// Initialize parser with expected total 2 packages and 9043KB total download.
	totalBytes := int64((87 + 8956) * 1024)
	parser := NewStreamParser(2, totalBytes)

	// Feed stderr download ticks
	stderrLines := []string{
		"[ 0%] libxcrypt-compat-4.4.38_1.x86_64.xbps: [87KB 50%] 10MB/s ETA: 00m01s\x1b[K\r",
		"libxcrypt-compat-4.4.38_1.x86_64.xbps: 87KB [avg rate: 10MB/s]\x1b[K\n",
		"[80%] glibc-2.41_1.x86_64.xbps: [8956KB 80%] 5000KB/s ETA: 00m02s\x1b[K\r",
	}

	for _, line := range stderrLines {
		parser.FeedStderr(line)
	}

	st := parser.State()
	if st.CurrentPackage != "glibc-2.41_1" {
		t.Errorf("CurrentPackage = %q, want glibc-2.41_1", st.CurrentPackage)
	}
	if st.TotalPackages != 2 {
		t.Errorf("TotalPackages = %d, want 2", st.TotalPackages)
	}
	if st.DownloadRate != "5000KB/s" {
		t.Errorf("DownloadRate = %q, want 5000KB/s", st.DownloadRate)
	}
	if st.TimeRemaining != "00m02s" {
		t.Errorf("TimeRemaining = %q, want 00m02s", st.TimeRemaining)
	}
	if st.TotalDownload != totalBytes {
		t.Errorf("TotalDownload = %d, want %d", st.TotalDownload, totalBytes)
	}
	// Downloaded bytes should be 87KB (completed file 1) + 80% of 8956KB (file 2)
	wantDownloaded := int64(87*1024) + int64(8956*1024*80/100)
	if st.DownloadedBytes != wantDownloaded {
		t.Errorf("DownloadedBytes = %d, want %d", st.DownloadedBytes, wantDownloaded)
	}

	// Feed stdout phase and package events
	stdoutLines := []string{
		"[*] Unpacking packages",
		"glibc-2.41_1: unpacking ...",
		"[*] Configuring unpacked packages",
		"glibc-2.41_1: configuring ...",
	}

	parser.FeedStdout(stdoutLines[0])
	parser.FeedStdout(stdoutLines[1])
	st = parser.State()
	if st.Phase != "Unpacking" {
		t.Errorf("Phase = %q, want Unpacking", st.Phase)
	}
	if st.CurrentPackage != "glibc-2.41_1" {
		t.Errorf("CurrentPackage = %q, want glibc-2.41_1", st.CurrentPackage)
	}

	parser.FeedStdout(stdoutLines[2])
	parser.FeedStdout(stdoutLines[3])
	st = parser.State()
	if st.Phase != "Configuring" {
		t.Errorf("Phase = %q, want Configuring", st.Phase)
	}
	if st.CurrentPackage != "glibc-2.41_1" {
		t.Errorf("CurrentPackage = %q, want glibc-2.41_1", st.CurrentPackage)
	}
}

func TestScanLinesOrCR(t *testing.T) {
	input := "line1\r\nline2\rline3\nline4\x1b[K\r"
	r := strings.NewReader(input)
	lines, err := readLinesWithCR(r)
	if err != nil {
		t.Fatalf("readLinesWithCR failed: %v", err)
	}

	want := []string{"line1", "line2", "line3", "line4\x1b[K"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i, l := range lines {
		if l != want[i] {
			t.Errorf("line %d = %q, want %q", i, l, want[i])
		}
	}
}

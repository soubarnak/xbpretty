package cli

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	// Stderr download regexes:
	// TTY tick: [ 0%] libxcrypt-compat-4.4.38_1.x86_64.xbps.sig2: [512B 100%] 54MB/s ETA: 00m00s\x1b[K\r
	downloadTickTTYRegex = regexp.MustCompile(`^\[\s*(\d+)%\]\s+([\w\.\-]+):\s+\[([\d\.]+[A-Za-z]+)\s+(\d+)%\]\s+(.+?)\s+ETA:\s+(\S+)`)
	// Piped tick: glibc-2.41_1.x86_64.xbps: [8956KB 82%] 5628KB/s ETA: 00m06s\n
	downloadTickPipedRegex = regexp.MustCompile(`^([\w\.\-]+):\s+\[([\d\.]+[A-Za-z]+)\s+(\d+)%\]\s+(.+?)\s+ETA:\s+(\S+)`)
	// Complete line: libxcrypt-compat-4.4.38_1.x86_64.xbps: 87KB [avg rate: 2848MB/s]\x1b[K\n
	downloadCompleteRegex = regexp.MustCompile(`^([\w\.\-]+):\s+([\d\.]+[A-Za-z]+)\s+\[avg rate:\s+(\S+)\]`)

	// Stdout phase header regexes:
	repoSyncRegex  = regexp.MustCompile(`^\[\*\] Updating repository ['"\x60]([^'"\x60]+)['"\x60] \.\.\.$`)
	phaseHeadRegex = regexp.MustCompile(`^\[\*\] (Downloading packages|Verifying package integrity|Collecting package files|Unpacking packages|Configuring unpacked packages)$`)

	// Stdout per-package action regexes:
	verifyRegex    = regexp.MustCompile(`^([\w\.\-]+):\s+verifying (RSA signature|SHA256 hash)\.\.\.$`)
	collectRegex   = regexp.MustCompile(`^([\w\.\-]+):\s+collecting files\.\.\.$`)
	unpackRegex    = regexp.MustCompile(`^([\w\.\-]+):\s+unpacking \.\.\.$`)
	configureRegex = regexp.MustCompile(`^([\w\.\-]+):\s+configuring \.\.\.$`)
	installOkRegex = regexp.MustCompile(`^([\w\.\-]+):\s+installed successfully\.$`)
	updateRunRegex = regexp.MustCompile(`^([\w\.\-]+):\s+updating to ([\w\.\-]+) \.\.\.$`)
	updateOkRegex  = regexp.MustCompile(`^([\w\.\-]+):\s+updated successfully\.$`)
	configOkRegex  = regexp.MustCompile(`^([\w\.\-]+):\s+configured successfully\.$`)
	removeRunRegex = regexp.MustCompile(`^Removing ['"\x60]([\w\.\-]+)['"\x60] \.\.\.$`)
	removeOkRegex  = regexp.MustCompile(`^Removed ['"\x60]([\w\.\-]+)['"\x60] successfully\.$`)

	// Transaction summary regex:
	summaryRegex = regexp.MustCompile(`^(\d+)\s+downloaded,\s+(\d+)\s+installed,\s+(\d+)\s+updated,\s+(\d+)\s+configured,\s+(\d+)\s+removed,\s+(\d+)\s+on hold\.$`)

	// Size regex:
	sizeRegex = regexp.MustCompile(`^([\d\.]+)\s*([A-Za-z]+)$`)
)

// TelemetryState represents the snapshot of current execution progress.
type TelemetryState struct {
	Phase           string  // "RepoSync", "Downloading", "Verifying", "Collecting", "Unpacking", "Configuring", "Removing"
	CurrentPackage  string  // Active package name or target
	TotalPackages   int     // Total packages in transaction
	PackageIndex    int     // Current package index
	DownloadedBytes int64   // Cumulative downloaded bytes
	TotalDownload   int64   // Total download size in bytes
	DownloadRate    string  // Current download rate (e.g. "5628KB/s")
	TimeRemaining   string  // ETA string (e.g. "00m06s")
	Percent         float64 // Overall progress percentage
}

// DownloadTick represents one parsed download tick event.
type DownloadTick struct {
	OverallPercent  int // -1 if not available (piped mode)
	Filename        string
	PackageName     string
	TotalSizeStr    string
	TotalSizeBytes  int64
	FilePercent     int
	DownloadedBytes int64
	Rate            string
	ETA             string
}

// DownloadComplete represents a file download completion event.
type DownloadComplete struct {
	Filename    string
	PackageName string
	SizeStr     string
	SizeBytes   int64
	AvgRate     string
}

// PackageEvent represents an action on a package.
type PackageEvent struct {
	Package string
	Action  string // "verifying", "collecting", "unpacking", "configuring", "installed", "updating", "updated", "configured", "removing", "removed"
	Details string
}

// TransactionSummary represents the final summary line counts.
type TransactionSummary struct {
	Downloaded int
	Installed  int
	Updated    int
	Configured int
	Removed    int
	OnHold     int
}

// cleanLine strips ANSI escape codes and trims trailing carriage returns, newlines, and spaces.
func cleanLine(s string) string {
	s = ansiRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(strings.TrimRight(s, "\r\n"))
}

// packageNameFromFilename extracts the clean package name from a .xbps archive filename.
func packageNameFromFilename(fn string) string {
	fn = strings.TrimSuffix(fn, ".sig2")
	fn = strings.TrimSuffix(fn, ".xbps")
	if idx := strings.LastIndex(fn, "."); idx != -1 {
		arch := fn[idx+1:]
		switch arch {
		case "x86_64", "i686", "armv7l", "armv6l", "aarch64", "noarch":
			fn = fn[:idx]
		}
	}
	return fn
}

// ParseSize converts a humanized size string (e.g. 512B, 87KB, 8956KB, 12MB) into bytes.
func ParseSize(s string) (int64, error) {
	m := sizeRegex.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) < 3 {
		return 0, fmt.Errorf("invalid size format: %q", s)
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q in size %q: %w", m[1], s, err)
	}

	unit := strings.ToUpper(m[2])
	var multiplier int64
	switch unit {
	case "B":
		multiplier = 1
	case "K", "KB", "KIB":
		multiplier = 1024
	case "M", "MB", "MIB":
		multiplier = 1024 * 1024
	case "G", "GB", "GIB":
		multiplier = 1024 * 1024 * 1024
	case "T", "TB", "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit %q in %q", m[2], s)
	}

	return int64(val * float64(multiplier)), nil
}

// ParseDownloadTick parses a download tick line from stderr (or stdout when piped).
func ParseDownloadTick(line string) (*DownloadTick, bool) {
	clean := cleanLine(line)

	// Check TTY mode: [ %d%] file: [size pct%] rate ETA: eta
	if m := downloadTickTTYRegex.FindStringSubmatch(clean); len(m) >= 7 {
		overallPct, _ := strconv.Atoi(m[1])
		filename := m[2]
		sizeStr := m[3]
		filePct, _ := strconv.Atoi(m[4])
		rate := m[5]
		eta := m[6]

		szBytes, _ := ParseSize(sizeStr)
		dlBytes := int64(float64(szBytes) * float64(filePct) / 100.0)

		return &DownloadTick{
			OverallPercent:  overallPct,
			Filename:        filename,
			PackageName:     packageNameFromFilename(filename),
			TotalSizeStr:    sizeStr,
			TotalSizeBytes:  szBytes,
			FilePercent:     filePct,
			DownloadedBytes: dlBytes,
			Rate:            rate,
			ETA:             eta,
		}, true
	}

	// Check piped mode: file: [size pct%] rate ETA: eta
	if m := downloadTickPipedRegex.FindStringSubmatch(clean); len(m) >= 6 {
		filename := m[1]
		sizeStr := m[2]
		filePct, _ := strconv.Atoi(m[3])
		rate := m[4]
		eta := m[5]

		szBytes, _ := ParseSize(sizeStr)
		dlBytes := int64(float64(szBytes) * float64(filePct) / 100.0)

		return &DownloadTick{
			OverallPercent:  -1,
			Filename:        filename,
			PackageName:     packageNameFromFilename(filename),
			TotalSizeStr:    sizeStr,
			TotalSizeBytes:  szBytes,
			FilePercent:     filePct,
			DownloadedBytes: dlBytes,
			Rate:            rate,
			ETA:             eta,
		}, true
	}

	return nil, false
}

// ParseDownloadComplete parses a download completion line.
func ParseDownloadComplete(line string) (*DownloadComplete, bool) {
	clean := cleanLine(line)
	if m := downloadCompleteRegex.FindStringSubmatch(clean); len(m) >= 4 {
		filename := m[1]
		sizeStr := m[2]
		rate := m[3]
		szBytes, _ := ParseSize(sizeStr)
		return &DownloadComplete{
			Filename:    filename,
			PackageName: packageNameFromFilename(filename),
			SizeStr:     sizeStr,
			SizeBytes:   szBytes,
			AvgRate:     rate,
		}, true
	}
	return nil, false
}

// ParsePhaseHeader parses a phase transition header line on stdout.
func ParsePhaseHeader(line string) (string, bool) {
	clean := cleanLine(line)
	if m := repoSyncRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return "RepoSync", true
	}
	if m := phaseHeadRegex.FindStringSubmatch(clean); len(m) >= 2 {
		switch m[1] {
		case "Downloading packages":
			return "Downloading", true
		case "Verifying package integrity":
			return "Verifying", true
		case "Collecting package files":
			return "Collecting", true
		case "Unpacking packages":
			return "Unpacking", true
		case "Configuring unpacked packages":
			return "Configuring", true
		}
	}
	return "", false
}

// ParsePackageEvent parses a per-package action line on stdout.
func ParsePackageEvent(line string) (*PackageEvent, bool) {
	clean := cleanLine(line)

	if m := verifyRegex.FindStringSubmatch(clean); len(m) >= 3 {
		return &PackageEvent{Package: m[1], Action: "verifying", Details: m[2]}, true
	}
	if m := collectRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "collecting"}, true
	}
	if m := unpackRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "unpacking"}, true
	}
	if m := configureRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "configuring"}, true
	}
	if m := installOkRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "installed"}, true
	}
	if m := updateRunRegex.FindStringSubmatch(clean); len(m) >= 3 {
		return &PackageEvent{Package: m[1], Action: "updating", Details: m[2]}, true
	}
	if m := updateOkRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "updated"}, true
	}
	if m := configOkRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "configured"}, true
	}
	if m := removeRunRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "removing"}, true
	}
	if m := removeOkRegex.FindStringSubmatch(clean); len(m) >= 2 {
		return &PackageEvent{Package: m[1], Action: "removed"}, true
	}

	return nil, false
}

// ParseTransactionSummary parses the final transaction summary counts.
func ParseTransactionSummary(line string) (*TransactionSummary, bool) {
	clean := cleanLine(line)
	m := summaryRegex.FindStringSubmatch(clean)
	if len(m) < 7 {
		return nil, false
	}
	dl, _ := strconv.Atoi(m[1])
	in, _ := strconv.Atoi(m[2])
	up, _ := strconv.Atoi(m[3])
	cf, _ := strconv.Atoi(m[4])
	rm, _ := strconv.Atoi(m[5])
	hd, _ := strconv.Atoi(m[6])

	return &TransactionSummary{
		Downloaded: dl,
		Installed:  in,
		Updated:    up,
		Configured: cf,
		Removed:    rm,
		OnHold:     hd,
	}, true
}

// StreamParser manages cumulative state while reading stdout and stderr telemetry streams.
type StreamParser struct {
	mu             sync.RWMutex
	state          TelemetryState
	completedFiles map[string]int64
	seenPackages   map[string]bool

	OnDownloadTick     func(tick DownloadTick)
	OnDownloadComplete func(comp DownloadComplete)
	OnPhaseChange      func(phase string)
	OnPackageEvent     func(event PackageEvent)
	OnSummary          func(sum TransactionSummary)
}

// NewStreamParser creates a stream parser with optional known totals.
func NewStreamParser(totalPackages int, totalDownloadBytes int64) *StreamParser {
	return &StreamParser{
		state: TelemetryState{
			TotalPackages: totalPackages,
			TotalDownload: totalDownloadBytes,
		},
		completedFiles: make(map[string]int64),
		seenPackages:   make(map[string]bool),
	}
}

// FeedStderr parses one line from child stderr.
func (p *StreamParser) FeedStderr(line string) {
	p.mu.Lock()

	if tick, ok := ParseDownloadTick(line); ok {
		p.state.Phase = "Downloading"
		p.state.CurrentPackage = tick.PackageName
		p.state.DownloadRate = tick.Rate
		p.state.TimeRemaining = tick.ETA

		if !strings.HasSuffix(tick.Filename, ".sig2") {
			if !p.seenPackages[tick.PackageName] {
				p.seenPackages[tick.PackageName] = true
				p.state.PackageIndex = len(p.seenPackages)
				if p.state.PackageIndex > p.state.TotalPackages {
					p.state.TotalPackages = p.state.PackageIndex
				}
			}
		}

		var completedSum int64
		for fn, sz := range p.completedFiles {
			if fn != tick.Filename {
				completedSum += sz
			}
		}
		p.state.DownloadedBytes = completedSum + tick.DownloadedBytes

		if p.state.TotalDownload == 0 && tick.OverallPercent > 0 {
			p.state.TotalDownload = int64(float64(p.state.DownloadedBytes) / (float64(tick.OverallPercent) / 100.0))
		}

		if tick.OverallPercent >= 0 {
			p.state.Percent = float64(tick.OverallPercent)
		} else if p.state.TotalDownload > 0 {
			p.state.Percent = (float64(p.state.DownloadedBytes) / float64(p.state.TotalDownload)) * 100.0
		}

		cb := p.OnDownloadTick
		p.mu.Unlock()
		if cb != nil {
			cb(*tick)
		}
		return
	}

	if comp, ok := ParseDownloadComplete(line); ok {
		p.completedFiles[comp.Filename] = comp.SizeBytes
		var completedSum int64
		for _, sz := range p.completedFiles {
			completedSum += sz
		}
		p.state.DownloadedBytes = completedSum
		p.state.CurrentPackage = comp.PackageName
		p.state.DownloadRate = comp.AvgRate

		cb := p.OnDownloadComplete
		p.mu.Unlock()
		if cb != nil {
			cb(*comp)
		}
		return
	}
	p.mu.Unlock()
}

// FeedStdout parses one line from child stdout.
func (p *StreamParser) FeedStdout(line string) {
	p.mu.Lock()

	if phase, ok := ParsePhaseHeader(line); ok {
		p.state.Phase = phase
		cb := p.OnPhaseChange
		p.mu.Unlock()
		if cb != nil {
			cb(phase)
		}
		return
	}

	if event, ok := ParsePackageEvent(line); ok {
		switch event.Action {
		case "unpacking", "updating":
			p.state.Phase = "Unpacking"
		case "configuring":
			p.state.Phase = "Configuring"
		case "removing":
			p.state.Phase = "Removing"
		}
		p.state.CurrentPackage = event.Package

		cb := p.OnPackageEvent
		p.mu.Unlock()
		if cb != nil {
			cb(*event)
		}
		return
	}

	if sum, ok := ParseTransactionSummary(line); ok {
		cb := p.OnSummary
		p.mu.Unlock()
		if cb != nil {
			cb(*sum)
		}
		return
	}
	p.mu.Unlock()
}

// State returns a copy of the current telemetry state.
func (p *StreamParser) State() TelemetryState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// scanLinesOrCR splits input on \r\n, \r, or \n, enabling immediate consumption of
// unbuffered carriage-return-delimited download telemetry lines.
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[0:i], nil
			}
			return i + 1, data[0:i], nil
		}
		if data[i] == '\n' {
			return i + 1, data[0:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// readLinesWithCR reads all lines from r splitting on \r\n, \r, or \n.
func readLinesWithCR(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Split(scanLinesOrCR)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

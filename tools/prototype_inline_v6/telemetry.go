package main

import (
	"fmt"
	"regexp"
	"strconv"
)

// dlTickRe matches xbps's live download tick (#26 §4.1):
//
//	[57%] glibc-2.41_1.x86_64.xbps: [2.0MB 50%] 5.4MB/s ETA: 00m03s
var dlTickRe = regexp.MustCompile(`^\[\s*(\d+)%\]\s+(\S+):\s+\[(.+?)\s+(\d+)%\]\s+(.+?)\s+ETA:\s+(\S+)$`)

// dlDoneRe matches xbps's archive-completed line:
//
//	glibc-2.41_1.x86_64.xbps: 1919KB [avg rate: 5.4MB/s]
var dlDoneRe = regexp.MustCompile(`^(\S+):\s+(.+?)\s+\[avg rate:\s+(.+?)\]$`)

// unpackRe / removeRe / repoRe are the phase and lifecycle lines the widget's
// install, remove and update states are driven from (#45 §6.3, #26 §4.2-4.3).
var (
	unpackRe = regexp.MustCompile(`^([\w.+~-]+):\s+unpacking \.\.\.$`)
	removeRe = regexp.MustCompile(`^Removing ['` + "`" + `]([\w.+~-]+)['` + "`" + `] \.\.\.$`)
	repoRe   = regexp.MustCompile(`^\[\*\] Updating repository ['` + "`" + `]([^'` + "`" + `]+)['` + "`" + `] \.\.\.$`)
)

// dlRow is one archive in the synthetic download plan.
type dlRow struct {
	name string
	size uint64
}

// syntheticDownloadPlan builds an 80-archive, 33 MiB plan whose first 43
// archives total 18 MiB and whose 44th is a 2 MiB archive at 50 %, so the
// snapshot lands on the same 19.0 MiB/33.0 MiB and 43/80 as nala's research
// frame. The numbers are data, not layout.
func syntheticDownloadPlan() []dlRow {
	rows := make([]dlRow, 0, 80)
	for i := 0; i < 41; i++ {
		rows = append(rows, dlRow{fmt.Sprintf("pkg%02d-1.0_1.x86_64.xbps", i), 400 * kib})
	}
	rows = append(rows,
		dlRow{"libfoo-2.3_1.x86_64.xbps", 1016 * kib},
		dlRow{"glibc-2.41_1.x86_64.xbps", 1016 * kib},
		dlRow{"vim-runtime-9.1.1300_1.x86_64.xbps", 2048 * kib},
	)
	rest := 36
	remaining := uint64(33792)*kib - 18432*kib - 2048*kib // 13312 KiB
	for i := 0; i < rest; i++ {
		size := remaining / uint64(rest-i)
		remaining -= size
		rows = append(rows, dlRow{fmt.Sprintf("tail%02d-1.0_1.x86_64.xbps", i), size})
	}
	return rows
}

// downloadEvents produces the synthetic xbps stream: the first 43 archives
// complete, then archive 44 ticks at 50 %.
func downloadEvents(plan []dlRow) []string {
	var events []string
	for _, row := range plan[:43] {
		events = append(events, fmt.Sprintf("%s: %d KiB [avg rate: 5.4MB/s]", row.name, row.size/kib))
	}
	events = append(events, fmt.Sprintf("[57%%] %s: [2.0MB 50%%] 5.4MB/s ETA: 00m03s", plan[43].name))
	return events
}

// downloadSnapshot is the state the Download view renders from.
type downloadSnapshot struct {
	completedBytes uint64
	completedPkgs  int
	position       uint64
	total          uint64
	lastFile       string
}

// applyDownloadEvents replays the telemetry: completion lines add exact plan
// bytes (never the humanised figure), ticks add the in-flight archive's plan
// size times its own percent, and .sig2 files are ignored (#45 §6.3).
func applyDownloadEvents(plan []dlRow, events []string) downloadSnapshot {
	sizeOf := map[string]uint64{}
	var total uint64
	for _, row := range plan {
		sizeOf[row.name] = row.size
		total += row.size
	}
	snap := downloadSnapshot{total: total}
	var inFlight uint64
	for _, ev := range events {
		if m := dlDoneRe.FindStringSubmatch(ev); m != nil {
			name := m[1]
			if size, ok := sizeOf[name]; ok {
				snap.completedBytes += size
				snap.completedPkgs++
				snap.lastFile = name
			}
			continue
		}
		if m := dlTickRe.FindStringSubmatch(ev); m != nil {
			name := m[2]
			pct, _ := strconv.ParseUint(m[4], 10, 64)
			if size, ok := sizeOf[name]; ok {
				inFlight = size * pct / 100
			}
		}
	}
	snap.position = snap.completedBytes + inFlight
	if snap.position > snap.total {
		snap.position = snap.total
	}
	return snap
}

// downloadWidget renders the Download view's data (7 rows: message, divider,
// bar, two info rows) (#45 §6.3).
func downloadWidget(plan []dlRow, elapsed float64) Widget {
	events := downloadEvents(plan)
	snap := applyDownloadEvents(plan, events)
	rate := ceilRate(snap.position, elapsed)
	ratio := 0.0
	if snap.total > 0 {
		ratio = float64(snap.position) / float64(snap.total)
	}
	remaining := "—"
	if eta, ok := etaSeconds(snap.position, snap.total, rate); ok {
		remaining = timeStr(eta)
	}
	last := snap.lastFile
	if last == "" {
		last = ""
	}
	data := widgetSize(snap.position) + "/" + widgetSize(snap.total)
	return Widget{
		Title:     "Downloading Packages",
		MsgHeader: "Last completed",
		MsgText:   last,
		MsgRole:   RolePrimary,
		Left: []InfoRow{
			{"Packages", pairCount(snap.completedPkgs, len(plan))},
			{"Remaining", remaining},
		},
		Right: []InfoRow{
			{"Data", data},
			{"Speed", widgetSize(rate) + "/s"},
		},
		Ratio: ratio,
	}
}

// installWidget replays a synthetic install stream through #45 §6.5's
// two-unit model: U = 2·A (8 packages), and seven unpacking lines mean
// unpackCompleted = 7, i.e. 7/16 = 43 %.
func installWidget(elapsed float64) Widget {
	pkgs := []string{
		"glibc-2.41_1", "bash-5.3_1", "zlib-1.3.1_1", "openssl-3.5.1_1",
		"curl-8.14.1_1", "git-2.50.1_1", "python3-3.13.5_1", "glibc-2.41_1",
	}
	var events []string
	unpacked := 0
	lastPkg := ""
	for _, pkg := range pkgs {
		events = append(events, fmt.Sprintf("%s: unpacking ...", pkg))
		unpacked++
		lastPkg = pkg
	}
	const A = 8
	U := 2 * A
	completed := unpacked - 1 // #45 §6.5: packages before the current one
	ratio := float64(completed) / float64(U)
	return Widget{
		Title:     "Installing Packages",
		MsgHeader: "Status",
		MsgText:   "Unpacking " + lastPkg,
		MsgRole:   RolePrimary,
		Left: []InfoRow{
			{"Package", lastPkg},
		},
		Right: []InfoRow{
			{"Elapsed", timeStr(ceilSeconds(elapsed))},
		},
		Ratio: ratio,
	}
}

// removeWidget renders the Removing Packages view (same geometry as Install).
func removeWidget(elapsed float64) Widget {
	pkgs := []string{"nano-9.2_1", "wget-1.25.0_1", "lsof-4.99.7_1", "cups-2.4.19_1"}
	var events []string
	removed := 0
	lastPkg := ""
	for i, pkg := range pkgs {
		events = append(events, fmt.Sprintf("Removing '%s' ...", pkg))
		if i < 2 {
			events = append(events, fmt.Sprintf("Removed '%s' successfully.", pkg))
			removed++
		}
		lastPkg = pkg
	}
	_ = events
	return Widget{
		Title:     "Removing Packages",
		MsgHeader: "Status",
		MsgText:   "Removing " + lastPkg,
		MsgRole:   RolePrimary,
		Left: []InfoRow{
			{"Package", lastPkg},
		},
		Right: []InfoRow{
			{"Elapsed", timeStr(ceilSeconds(elapsed))},
		},
		Ratio: float64(removed) / float64(len(pkgs)),
	}
}

// updateWidget renders the adapted Updating Package Lists view: repository
// URLs instead of sync bytes (#45 D10), height 6.
func updateWidget(done, total int, url string, elapsed float64) Widget {
	// Drive the count from the telemetry line so the view has the same shape
	// as a live run.
	events := make([]string, 0, total)
	for i := 0; i < done; i++ {
		events = append(events, fmt.Sprintf("[*] Updating repository '%s' ...", url))
	}
	seen := 0
	for _, ev := range events {
		if repoRe.MatchString(ev) {
			seen++
		}
	}
	return Widget{
		Title:     "Updating Package Lists",
		MsgHeader: "Updating",
		MsgText:   url,
		MsgRole:   RoleRegular, // nala's update message uses .regular() (#45 §6.3)
		Left: []InfoRow{
			{"Repositories", pairCount(seen, total)},
		},
		Right: []InfoRow{
			{"Elapsed", timeStr(ceilSeconds(elapsed))},
		},
		Ratio: float64(seen) / float64(total),
	}
}

// scoreWidget renders nala's Testing Mirrors view (#45 §6.3).
func scoreWidget(done, total int, url string, elapsed float64) Widget {
	return Widget{
		Title:     "Testing Mirrors",
		MsgHeader: "Finished",
		MsgText:   url,
		MsgRole:   RolePrimary,
		Left: []InfoRow{
			{"Mirrors", pairCount(done, total)},
		},
		Right: []InfoRow{
			{"Elapsed", timeStr(ceilSeconds(elapsed))},
		},
		Ratio: float64(done) / float64(total),
	}
}

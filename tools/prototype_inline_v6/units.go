package main

import (
	"fmt"
	"math"
	"strings"
)

// UnitSystem mirrors nala's [Ui] unit (#45 §3.5).
type UnitSystem int

const (
	Binary UnitSystem = iota
	Decimal
)

var (
	binaryUnits  = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	decimalUnits = []string{"B", "kB", "MB", "GB", "TB", "PB"}
)

// formatUnit reproduces nala's UnitStr::str (src/util/units.rs:24-47): a
// strict '>' unit switch, so exactly 1 MiB renders as "1024 KiB" in Binary
// (preferred over 1 MiB) — an upstream quirk kept on purpose (#45 §3.5).
func formatUnit(bytes uint64, sys UnitSystem, precision int) string {
	units := binaryUnits
	divisor := 1024.0
	if sys == Decimal {
		units = decimalUnits
		divisor = 1000.0
	}
	value := float64(bytes)
	unit := 0
	for unit < len(units)-1 && value > divisor {
		value /= divisor
		unit++
	}
	return fmt.Sprintf("%.*f %s", precision, value, units[unit])
}

// summarySize is the summary's unit rendering: [Ui] unit, precision 0.
func summarySize(bytes uint64, sys UnitSystem) string {
	return formatUnit(bytes, sys, 0)
}

// widgetSize is the widget's unit rendering: always Binary, precision 1,
// independent of [Ui] unit (#45 §6.6).
func widgetSize(bytes uint64) string {
	return formatUnit(bytes, Binary, 1)
}

// timeStr is rust-apt's time_str (0.11.3 src/util.rs:145-167) with the
// strict '>' boundaries nala inherits (#45 §6.6).
func timeStr(seconds uint64) string {
	switch {
	case seconds > 86400:
		days := seconds / 86400
		hours := (seconds % 86400) / 3600
		mins := (seconds % 3600) / 60
		return fmt.Sprintf("%dd %dh %dmin %ds", days, hours, mins, seconds%60)
	case seconds > 3600:
		return fmt.Sprintf("%dh %dmin %ds", seconds/3600, (seconds%3600)/60, seconds%60)
	case seconds > 60:
		return fmt.Sprintf("%dmin %ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// percent truncates (ratio*100) — nala truncates for the displayed percent
// while rounding the bar fill (#45 §6.2; src/tui/progress.rs:482,509).
func percent(ratio float64) int {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return int(ratio * 100)
}

// fillCount rounds width*ratio, clamped to [0, width] — nala rounds here
// (#45 §6.2: "fill uses round, not truncation. Both are separate.").
func fillCount(width int, ratio float64) int {
	n := int(math.Round(float64(width) * ratio))
	if n < 0 {
		n = 0
	}
	if n > width {
		n = width
	}
	return n
}

// ceilSeconds turns a float elapsed into the integer nala formats.
func ceilSeconds(elapsed float64) uint64 {
	if elapsed <= 0 {
		return 0
	}
	return uint64(math.Ceil(elapsed))
}

// ceilRate is ceil(position/elapsed_seconds) (#45 §6.6).
func ceilRate(position uint64, elapsed float64) uint64 {
	if elapsed <= 0 {
		return 0
	}
	return uint64(math.Ceil(float64(position) / elapsed))
}

// etaSeconds is ceil((total-position)/rate); ok=false means "—" (#45 §6.6).
func etaSeconds(position, total uint64, rate uint64) (uint64, bool) {
	if position == 0 || position >= total || rate == 0 {
		return 0, false
	}
	return uint64(math.Ceil(float64(total-position) / float64(rate))), true
}

// pluralCount renders nala's integer counters ("43/80").
func pairCount(done, total int) string { return fmt.Sprintf("%d/%d", done, total) }

// repeat is strings.Repeat with a guard so a negative width cannot panic.
func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

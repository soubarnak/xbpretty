package cli

import (
	"fmt"
	"math"
)

var (
	binarySizeUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
)

// widgetSize renders a byte count with the widget's fixed unit rule: Binary,
// precision 1, independent of [Ui] unit, with nala's strict '>' switch so
// exactly 1 MiB prints as 1024.0 KiB (ADR-0012 decision 4).
func widgetSize(bytes uint64) string {
	value := float64(bytes)
	unit := 0
	for unit < len(binarySizeUnits)-1 && value > 1024 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", value, binarySizeUnits[unit])
}

// widgetTime is rust-apt's time_str with nala's strict '>' boundaries.
func widgetTime(seconds uint64) string {
	switch {
	case seconds > 86400:
		return fmt.Sprintf("%dd %dh %dmin %ds", seconds/86400, (seconds%86400)/3600, (seconds%3600)/60, seconds%60)
	case seconds > 3600:
		return fmt.Sprintf("%dh %dmin %ds", seconds/3600, (seconds%3600)/60, seconds%60)
	case seconds > 60:
		return fmt.Sprintf("%dmin %ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// ceilSeconds turns an elapsed float into the integer time_str formats.
func ceilSeconds(elapsed float64) uint64 {
	if elapsed <= 0 {
		return 0
	}
	return uint64(math.Ceil(elapsed))
}

// ceilRate is ceil(position/elapsed) with elapsed in seconds.
func ceilRate(position uint64, elapsed float64) uint64 {
	if elapsed <= 0 {
		return 0
	}
	return uint64(math.Ceil(float64(position) / elapsed))
}

// etaSeconds is ceil((total-position)/rate); ok=false means the view shows an
// em dash.
func etaSeconds(position, total, rate uint64) (uint64, bool) {
	if position == 0 || position >= total || rate == 0 {
		return 0, false
	}
	return uint64(math.Ceil(float64(total-position) / float64(rate))), true
}

func pairCount(done, total int) string {
	return fmt.Sprintf("%d/%d", done, total)
}

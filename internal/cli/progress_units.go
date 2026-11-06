package cli

import (
	"fmt"
	"math"
)

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

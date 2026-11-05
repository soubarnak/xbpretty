// Command snapshotage checks the age of the embedded mirror list (FR-043,
// FR-076). The release workflow runs it when the snapshot is older than 90
// days and tells the release manager to run `make mirrors`.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	date := flag.String("date", "", "snapshot date (YYYY-MM-DD)")
	maxDays := flag.Int("max-days", 90, "maximum allowed age in days")
	flag.Parse()
	if err := check(*date, *maxDays, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "snapshotage: %v\n", err)
		os.Exit(1)
	}
}

func check(date string, maxDays int, now time.Time) error {
	if date == "" {
		return fmt.Errorf("no snapshot date given")
	}
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return fmt.Errorf("invalid snapshot date %q: %w", date, err)
	}
	ageDays := int(now.Sub(parsed).Hours() / 24)
	if ageDays > maxDays {
		return fmt.Errorf("mirror snapshot is %d days old (limit %d); run make mirrors", ageDays, maxDays)
	}
	return nil
}

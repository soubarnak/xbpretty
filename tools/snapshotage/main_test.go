package main

import (
	"strings"
	"testing"
	"time"
)

func TestCheckAcceptsRecentSnapshot(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	for _, date := range []string{"2026-11-05", "2026-08-08", "2026-11-06"} {
		if err := check(date, 90, now); err != nil {
			t.Errorf("check(%q) = %v, want nil", date, err)
		}
	}
}

func TestCheckRejectsStaleSnapshot(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	err := check("2026-08-07", 90, now)
	if err == nil {
		t.Fatal("check accepted a snapshot older than 90 days")
	}
	if !strings.Contains(err.Error(), "run make mirrors") {
		t.Errorf("stale error = %v, want the make mirrors hint", err)
	}
}

func TestCheckRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	for _, date := range []string{"", "2026-13-01", "yesterday"} {
		if err := check(date, 90, now); err == nil {
			t.Errorf("check(%q) accepted an invalid date", date)
		}
	}
}

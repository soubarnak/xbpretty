package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
)

// TestSyncTrackerStates is nala's per-repository status, in xbps's order: a
// repository with no fetch is No Change, one with a fetched repodata is Updated
// with its size, and a failed fetch is Error followed by the reason.
func TestSyncTrackerStates(t *testing.T) {
	var lines []string
	tracker := newSyncTracker(summary.DefaultTheme(), false, func(line string) {
		lines = append(lines, line)
	})
	const (
		repoA = "https://repo-default.voidlinux.org/current/x86_64-repodata"
		repoB = "https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata"
		repoC = "https://mirror.example/void/multilib/x86_64-repodata"
	)
	tracker.start(repoA)
	tracker.start(repoB)
	tracker.fetched(1854 * 1024)
	tracker.start(repoC)
	tracker.failed(repoC, "Connection refused")
	tracker.finish()

	want := []string{
		"No Change: https://repo-default.voidlinux.org/current x86_64-repodata",
		"Updated: https://repo-default.voidlinux.org/current/nonfree x86_64-repodata [1.8 MiB]",
		"Error: https://mirror.example/void/multilib x86_64-repodata",
		"Connection refused",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// TestSyncTrackerNothingFetched: a sync that fetched nothing ends with nala's
// `Nothing to fetch`, and one that fetched a repodata ends with the Fetched
// line: its size, the elapsed time and the rate (BF-03).
func TestSyncTrackerNothingFetched(t *testing.T) {
	tracker := newSyncTracker(summary.DefaultTheme(), false, func(string) {})
	tracker.start("https://repo-default.voidlinux.org/current/x86_64-repodata")
	tracker.finish()
	if got := tracker.completion(3 * time.Second); got != "Nothing to fetch" {
		t.Errorf("completion = %q, want %q", got, "Nothing to fetch")
	}

	fetched := newSyncTracker(summary.DefaultTheme(), false, func(string) {})
	fetched.start("https://repo-default.voidlinux.org/current/x86_64-repodata")
	fetched.fetched(1854 * 1024)
	fetched.finish()
	got := fetched.completion(3 * time.Second)
	if !strings.HasPrefix(got, "Fetched 1.8 MiB in 3s (") || !strings.HasSuffix(got, "/s)") {
		t.Errorf("completion = %q, want Fetched 1.8 MiB in 3s (rate/s)", got)
	}
}

// TestSyncTrackerColourUsesRoles: with colour on, each label carries its role
// (Primary, Secondary or Error) and the description stays Regular.
func TestSyncTrackerColourUsesRoles(t *testing.T) {
	theme := summary.DefaultTheme()
	var lines []string
	tracker := newSyncTracker(theme, true, func(line string) { lines = append(lines, line) })
	tracker.start("https://repo-default.voidlinux.org/current/x86_64-repodata")
	tracker.start("https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata")
	tracker.failed("https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata", "boom")
	tracker.finish()
	if len(lines) != 3 {
		t.Fatalf("lines = %q, want three", lines)
	}
	if !strings.HasPrefix(lines[0], paintRole(theme, true, rolePrimary, "No Change:")) {
		t.Errorf("No Change label is not Primary: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], paintRole(theme, true, roleError, "Error:")) {
		t.Errorf("Error label is not Error: %q", lines[1])
	}
}

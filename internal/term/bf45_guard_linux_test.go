//go:build linux

package term

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestGuardShowsTheTrackedCursorOnRestore is BF-45's guard half: when the
// inline viewport has hidden the cursor, Restore shows it again even though
// the guard itself never wrote the hide.
func TestGuardShowsTheTrackedCursorOnRestore(t *testing.T) {
	master, slave := openPty(t)
	g := NewGuard(slave, slave)
	if err := g.EnterInline(); err != nil {
		t.Skipf("no cbreak: %v", err)
	}
	g.TrackCursorHidden()
	g.Restore()
	out, err := readUntil(master, CursorShow, 5*time.Second)
	if err != nil {
		t.Fatalf("reading the guard bytes: %v\n%q", err, out)
	}
	if !strings.Contains(out, CursorShow) {
		t.Fatalf("Restore did not show the tracked cursor: %q", out)
	}
	if g.Active() {
		t.Error("guard is still active after Restore")
	}
}

// TestGuardRestoreWithoutTrackingLeavesTheCursor keeps the full-screen and
// plain paths unchanged: a guard that never hid the cursor writes no show.
func TestGuardRestoreWithoutTrackingLeavesTheCursor(t *testing.T) {
	master, slave := openPty(t)
	g := NewGuard(slave, slave)
	if err := g.EnterInline(); err != nil {
		t.Skipf("no cbreak: %v", err)
	}
	g.Restore()
	out := readThroughMarker(t, master, slave)
	if strings.Contains(out, CursorShow) {
		t.Fatalf("Restore wrote a cursor show without a tracked hide: %q", out)
	}
}

// readThroughMarker writes an end marker to the slave after the guard's bytes
// and reads the master up to it, so every byte written before the marker is
// captured regardless of how the pty chunks its output.
func readThroughMarker(t *testing.T, master, slave *os.File) string {
	t.Helper()
	const marker = "\x01END"
	if _, err := slave.WriteString(marker); err != nil {
		t.Fatalf("writing the end marker: %v", err)
	}
	out, err := readUntil(master, marker, 5*time.Second)
	if err != nil {
		t.Fatalf("reading the guard bytes: %v\n%q", err, out)
	}
	return out
}

// TestGuardTrackedCursorSurvivesAPanic proves the panic path restores the
// cursor: the deferred Restore runs while the panic unwinds, as the inline
// surface's Close does.
func TestGuardTrackedCursorSurvivesAPanic(t *testing.T) {
	master, slave := openPty(t)
	func() {
		g := NewGuard(slave, slave)
		defer g.Restore()
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		if err := g.EnterInline(); err != nil {
			t.Skipf("no cbreak: %v", err)
		}
		g.TrackCursorHidden()
		panic("boom")
	}()
	out, err := readUntil(master, CursorShow, 5*time.Second)
	if err != nil {
		t.Fatalf("the panic path did not show the cursor: %v\n%q", err, out)
	}
}

// TestGuardCursorShowIsIdempotent proves a second Restore, as a signal path
// and a deferred path may both issue, does not show the cursor twice.
func TestGuardCursorShowIsIdempotent(t *testing.T) {
	master, slave := openPty(t)
	g := NewGuard(slave, slave)
	if err := g.EnterInline(); err != nil {
		t.Skipf("no cbreak: %v", err)
	}
	g.TrackCursorHidden()
	g.Restore()
	g.Restore()
	out := readThroughMarker(t, master, slave)
	if got := strings.Count(out, CursorShow); got != 1 {
		t.Fatalf("cursor shows after two Restores = %d, want 1: %q", got, out)
	}
}

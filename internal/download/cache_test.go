package download

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchiveNeedSizeIsAPreflightEstimate pins FR-038's disk preflight input:
// the plan size when the archive is absent, nothing when it is present, and
// nothing extra when only the signature is missing.
func TestArchiveNeedSizeIsAPreflightEstimate(t *testing.T) {
	target := Target{Filename: "pkg-1.0_1.x86_64.xbps", Size: 4096}

	t.Run("missing archive", func(t *testing.T) {
		if got := ArchiveNeedSize(target, t.TempDir()); got != 4096 {
			t.Errorf("ArchiveNeedSize = %d, want the plan size 4096", got)
		}
	})

	t.Run("present archive", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, filepath.Join(dir, target.Filename), []byte("archive"))
		if got := ArchiveNeedSize(target, dir); got != 0 {
			t.Errorf("ArchiveNeedSize = %d, want 0 for a present archive", got)
		}
	})

	t.Run("signature only is not archive bytes", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, filepath.Join(dir, target.Filename), []byte("archive"))
		if got := ArchiveNeedSize(target, dir); got != 0 {
			t.Errorf("ArchiveNeedSize = %d, want 0 when only the .sig2 is missing", got)
		}
	})
}

// TestPreflight compares the plan's need against statvfs.
func TestPreflight(t *testing.T) {
	dir := t.TempDir()
	if _, err := FreeBytes(dir); err != nil {
		t.Fatalf("FreeBytes(%s): %v", dir, err)
	}
	if err := Preflight(dir, 0); err != nil {
		t.Errorf("a zero need must always pass, got %v", err)
	}
	if err := Preflight(dir, 1); err != nil {
		t.Errorf("one byte must fit in a fresh temp dir, got %v", err)
	}
	err := Preflight(dir, 1<<62)
	if err == nil {
		t.Fatal("an impossible need must fail the preflight")
	}
	if !strings.Contains(err.Error(), "not enough free space") || !strings.Contains(err.Error(), dir) {
		t.Errorf("error = %q, want the directory and the shortage", err)
	}
}

func writeCacheFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

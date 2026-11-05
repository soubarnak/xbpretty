package download

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNeedsFetchIsTheReadOnlyScan pins FR-038's scan rules as the disk
// preflight consumes them: a valid pair needs nothing, a missing signature or
// a mismatching archive needs a transfer.
func TestNeedsFetchIsTheReadOnlyScan(t *testing.T) {
	content := []byte("archive bytes")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	target := Target{Filename: "pkg-1.0_1.x86_64.xbps", SHA256: sha}

	t.Run("empty directory", func(t *testing.T) {
		if !NeedsFetch(target, t.TempDir()) {
			t.Error("an empty cache needs a fetch")
		}
	})

	t.Run("valid pair is a hit", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, filepath.Join(dir, target.Filename), content)
		writeCacheFile(t, filepath.Join(dir, target.Filename+".sig2"), []byte("sig"))
		if NeedsFetch(target, dir) {
			t.Error("a valid archive and signature is a cache hit")
		}
	})

	t.Run("missing signature", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, filepath.Join(dir, target.Filename), content)
		if !NeedsFetch(target, dir) {
			t.Error("a missing .sig2 needs a fetch")
		}
	})

	t.Run("hash mismatch", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, filepath.Join(dir, target.Filename), []byte("tampered"))
		writeCacheFile(t, filepath.Join(dir, target.Filename+".sig2"), []byte("sig"))
		if !NeedsFetch(target, dir) {
			t.Error("a mismatching archive needs a refetch")
		}
	})

	t.Run("no metadata hash trusts the files", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, filepath.Join(dir, target.Filename), content)
		writeCacheFile(t, filepath.Join(dir, target.Filename+".sig2"), []byte("sig"))
		target := Target{Filename: "pkg-1.0_1.x86_64.xbps"}
		if NeedsFetch(target, dir) {
			t.Error("without a metadata hash the pair is a cache hit")
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

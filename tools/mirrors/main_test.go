package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateSnapshotDate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.go")
	if err := os.WriteFile(path, []byte("package fetch\n\nconst SnapshotDate = \"2026-11-05\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateSnapshotDate(path, "2026-12-01"); err != nil {
		t.Fatalf("updateSnapshotDate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `const SnapshotDate = "2026-12-01"`) {
		t.Errorf("date was not updated:\n%s", data)
	}
}

func TestUpdateSnapshotDateRejectsMissingConstant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.go")
	if err := os.WriteFile(path, []byte("package fetch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateSnapshotDate(path, "2026-12-01"); err == nil {
		t.Fatal("updateSnapshotDate accepted a file without SnapshotDate")
	}
}

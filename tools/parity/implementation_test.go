package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCitedTestMustExist pins AC-063's renamed/deleted-test clause: a cited
// test that no *_test.go declares fails with the exact parity: message. This
// is the implementation-phase half of the contract; the spec phase has no
// TestParity_* names, so the check is dormant until one exists.
func TestCitedTestMustExist(t *testing.T) {
	m := &Matrix{Matrix: []MatrixRow{
		{ID: "P-CMD-001", Test: "TestParity_P_CMD_001_gone"},
	}}
	err := m.checkTestCells(map[string]bool{})
	want := "parity: P-CMD-001 cites test TestParity_P_CMD_001_gone, which no *_test.go declares"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want exactly %q", err, want)
	}
}

// TestImplementationModeRequiresEveryCell pins the release half: once
// internal/spec exists, a test cell may not stay at the spec-phase em dash.
// A release: gate is the only accepted non-test value.
func TestImplementationModeRequiresEveryCell(t *testing.T) {
	m := &Matrix{Matrix: []MatrixRow{
		{ID: "P-CMD-001", Test: "—"},
		{ID: "P-CMD-002", Test: "release: signed 9-arch repository"},
	}}
	err := m.requireTestCells()
	want := "parity: P-CMD-001 has no test cell; implementation mode requires a TestParity_ name or a release: gate"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want exactly %q", err, want)
	}
}

// TestImplementationModeDetection ties the mode to the presence of
// internal/spec, as ADR-0020 records: the workflow forbids the spec-phase skip
// once the packaged spec exists.
func TestImplementationModeDetection(t *testing.T) {
	dir := t.TempDir()
	if implementationMode(dir) {
		t.Fatal("implementation mode without internal/spec")
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !implementationMode(dir) {
		t.Fatal("spec phase reported after internal/spec exists")
	}
}

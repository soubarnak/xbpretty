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

// TestReleaseGateIsAccepted pins the release-gate branch of the
// implementation-phase rule: a gate needs no declared test.
func TestReleaseGateIsAccepted(t *testing.T) {
	m := &Matrix{Matrix: []MatrixRow{
		{ID: "P-PKG-003", Test: "release: 9-arch signed repository"},
	}}
	if err := m.requireTestCells(); err != nil {
		t.Fatalf("release gate rejected: %v", err)
	}
	if err := m.checkTestCells(map[string]bool{}); err != nil {
		t.Fatalf("release gate failed test resolution: %v", err)
	}
}

// TestCitedTestMustCarryItsRowID pins the anti-copy rule: a TestParity_ cell
// copied from another row is rejected even when the function exists.
func TestCitedTestMustCarryItsRowID(t *testing.T) {
	m := &Matrix{Matrix: []MatrixRow{
		{ID: "P-CMD-001", Test: "TestParity_P_CMD_002_search"},
	}}
	err := m.checkTestCells(map[string]bool{"TestParity_P_CMD_002_search": true})
	want := "parity: P-CMD-001 cites test TestParity_P_CMD_002_search, which does not carry its id prefix TestParity_P_CMD_001_"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want exactly %q", err, want)
	}
}

// TestMatrixOrderMustFollowInventory pins AC-061's no-renumbered-row clause.
func TestMatrixOrderMustFollowInventory(t *testing.T) {
	m := &Matrix{
		Inventory: []InventoryRow{{ID: "P-CMD-001"}, {ID: "P-CMD-002"}},
		Matrix:    []MatrixRow{{ID: "P-CMD-002"}, {ID: "P-CMD-001"}},
	}
	err := m.checkOrder()
	want := "parity: matrix row 1 is P-CMD-002, inventory row 1 is P-CMD-001; the matrix must follow the inventory order"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want exactly %q", err, want)
	}
}

// TestGenericNAPhraseIsRejected pins AC-062's vague-prose rejection.
func TestGenericNAPhraseIsRejected(t *testing.T) {
	err := checkNABehaviour(MatrixRow{ID: "P-VIS-999", Behaviour: "not applicable on xbps"})
	want := `parity: P-VIS-999 is n/a but its behaviour is the generic phrase "not applicable"; name the exact refusal or absence`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want exactly %q", err, want)
	}
	if err := checkNABehaviour(MatrixRow{ID: "P-VIS-999", Behaviour: "`Downgrade` never renders"}); err != nil {
		t.Errorf("a concrete absence was rejected: %v", err)
	}
}

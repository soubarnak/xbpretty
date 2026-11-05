package main

import (
	"testing"
)

func TestParityMatrix(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(root); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckFresh(root); err != nil {
		t.Fatal(err)
	}
	if len(m.Inventory) != 189 || len(m.Matrix) != 189 {
		t.Fatalf("parity: want 189/189 rows, got %d/%d", len(m.Inventory), len(m.Matrix))
	}
	if m.NACount() != 37 {
		t.Fatalf("parity: want 37 n/a rows, got %d", m.NACount())
	}
}

// TestParity_* wrappers for FR-077's implementation-phase matrix. Each row's
// test cell names one thin seam; keep the two in sync when coverage moves.

package main

import "testing"

func TestParity_P_BEH_031_the_parity_matrix_itself(t *testing.T) {
	TestParityMatrix(t)
}

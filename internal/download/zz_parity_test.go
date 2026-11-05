// TestParity_* wrappers for FR-077's implementation-phase matrix. Each cell
// names one thin seam; keep the cell and the wrapper in sync.

package download

import "testing"

func TestParity_P_BEH_012_predownload_engine(t *testing.T) {
	TestFetchVerifiesSHA256AndRemovesPart(t)
}

func TestParity_P_BEH_036_hash_algorithm(t *testing.T) {
	parityHashOnlySHA256(t)
}

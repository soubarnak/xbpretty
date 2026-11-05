// TestParity_* wrappers for FR-077's implementation-phase matrix. Each row's
// test cell names one thin seam; keep the two in sync when coverage moves.

package download

import "testing"

func TestParity_P_BEH_012_predownload_engine(t *testing.T) {
	TestFetchVerifiesSHA256AndRemovesPart(t)
}

func TestParity_P_BEH_035_per_domain_mirror_rotation(t *testing.T) {
	TestTargetFromRowCarriesPlanMetadata(t)
}

func TestParity_P_BEH_036_hash_algorithm(t *testing.T) {
	TestParseHashesKeysByRepositoryAndPkgver(t)
}

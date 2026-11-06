// TestParity_* wrappers for FR-077's implementation-phase matrix. Each cell
// names one thin seam; keep the cell and the wrapper in sync.

package fetch_test

import "testing"

func TestParity_P_BEH_014_fetch_mirror_scoring(t *testing.T) {
	TestProbeSendsRangeAndValidatesZstd(t)
}

func TestParity_P_BEH_015_fetch_selection_rule(t *testing.T) {
	TestValidateUsesHEADForEverySuffix(t)
}

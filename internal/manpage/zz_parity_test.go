// TestParity_* wrappers for FR-077's implementation-phase matrix. Each cell
// names one thin seam; keep the cell and the wrapper in sync.

package manpage

import "testing"

func TestParity_P_PKG_008_mandoc_manpage_freshness(t *testing.T) {
	parityManpageFreshness(t)
}

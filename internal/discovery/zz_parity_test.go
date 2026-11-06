// TestParity_* wrappers for FR-077's implementation-phase matrix. Each cell
// names one thin seam; keep the cell and the wrapper in sync.

package discovery

import "testing"

func TestParity_P_BEH_017_machine_output_for_list(t *testing.T) {
	TestWriteNDJSONIsCompactAndNotHTMLEscaped(t)
}

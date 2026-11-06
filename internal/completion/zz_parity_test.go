// TestParity_* wrappers for FR-077's implementation-phase matrix. Each cell
// names one thin seam; keep the cell and the wrapper in sync.

package completion

import "testing"

func TestParity_P_PKG_007_shipped_completion_files_and(t *testing.T) {
	TestShippedScriptsAreCurrent(t)
}

func TestParity_P_PKG_009_package_argument_completion_source(t *testing.T) {
	TestCompletionQueryRules(t)
}

func TestParity_P_PKG_010_installed_only_completion(t *testing.T) {
	TestCompletionQueryRules(t)
}

func TestParity_P_PKG_011_history_id_completion(t *testing.T) {
	TestCompletionPerCommandFlagsAndHistory(t)
}

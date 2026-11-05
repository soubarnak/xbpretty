// TestParity_* wrappers for FR-077's implementation-phase matrix. Each row's
// test cell names one thin seam; keep the two in sync when coverage moves.

package summary

import "testing"

func TestParity_P_VIS_003_section_label_line(t *testing.T) {
	TestRendererGoldens(t)
}

func TestParity_P_VIS_004_table_geometry(t *testing.T) {
	TestRendererGoldens(t)
}

func TestParity_P_VIS_005_package_name_role(t *testing.T) {
	TestRolesUseNalasPrefixes(t)
}

func TestParity_P_VIS_006_old_and_new_version(t *testing.T) {
	TestVersionDiffNala(t)
}

func TestParity_P_VIS_007_held_reason_cell(t *testing.T) {
	TestHeldRowWithChangedVersionDiffs(t)
}

func TestParity_P_VIS_008_size_column_and_right(t *testing.T) {
	TestOnlySizeColumnIsRightAligned(t)
}

func TestParity_P_VIS_009_summary_heading_and_footer(t *testing.T) {
	TestRendererGoldens(t)
}

func TestParity_P_VIS_010_size_block(t *testing.T) {
	TestFooterCountsOnlyNonEmptySections(t)
}

func TestParity_P_VIS_027_summary_and_widget_size(t *testing.T) {
	TestSizeUnitsUseStrictGreater(t)
}

func TestParity_P_VIS_028_downgrade_section(t *testing.T) {
	parityAbsentDowngrade(t)
}

func TestParity_P_VIS_029_held_reason_vocabulary_beyond(t *testing.T) {
	parityAbsentExcluded(t)
}

func TestParity_P_VIS_034_apt_essential_package_removal(t *testing.T) {
	parityAbsentEssentialWarning(t)
}

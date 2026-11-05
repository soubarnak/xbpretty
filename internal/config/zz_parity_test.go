// TestParity_* wrappers for FR-077's implementation-phase matrix. Each cell
// names one thin seam; keep the cell and the wrapper in sync.

package config

import "testing"

func TestParity_P_CFG_010_color_theme_primary(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_011_color_theme_secondary(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_012_color_theme_highlight(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_013_color_theme_regular(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_014_color_theme_progressfilled(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_015_color_theme_progressunfilled(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_016_color_theme_notice(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_017_color_theme_warning(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_018_color_theme_error(t *testing.T) {
	TestDefaultsAreTheSpecDefaults(t)
}

func TestParity_P_CFG_019_color_theme_style_grammar(t *testing.T) {
	TestStyleModifierForms(t)
}

func TestParity_P_CFG_020_color_theme_modifier_vocabulary(t *testing.T) {
	TestStyleModifierForms(t)
}

func TestParity_P_CFG_021_toml_outside_the_supported(t *testing.T) {
	parityConfigRejectsWithPathAndLine(t)
}

func TestParity_P_BEH_037_apt_acquire_proxy_keys(t *testing.T) {
	parityNoAcquireProxyConfig(t)
}

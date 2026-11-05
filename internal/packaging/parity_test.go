package packaging

import (
	"strings"
	"testing"
)

// parityTemplateInstallsDocs proves P-PKG-005: the package installs the
// example and migration guide, and still installs no system configuration.
func parityTemplateInstallsDocs(t *testing.T) {
	TestTemplateInstallsTheCompleteSurface(t)
	template := read(t, "srcpkgs/xbpretty/template")
	for _, want := range []string{
		"vdoc docs/config.toml.example",
		"vdoc docs/migration-rev6.md",
		"vman man/xbpretty.8",
		"vman man/xbpretty.conf.5",
	} {
		if !strings.Contains(template, want) {
			t.Errorf("template is missing install line %q", want)
		}
	}
	if strings.Contains(template, "/etc/xbpretty") || strings.Contains(template, "conf_files") {
		t.Error("template installs a system config or declares conf_files")
	}
}

package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/spec"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestTemplateInstallsTheCompleteSurface pins FR-076's install list: the
// binary, six aliases, three completions, two manpages plus six man8 links,
// the docs and the licence; and no /etc or conf_files entry.
func TestTemplateInstallsTheCompleteSurface(t *testing.T) {
	template := read(t, "srcpkgs/xbpretty/template")

	for _, want := range []string{
		"build_style=go",
		`go_import_path="github.com/soubarnak/xbpretty"`,
		`go_package="${go_import_path}/cmd/xbpretty"`,
		`go_ldflags="-X ${go_import_path}/internal/cli.Version=${version}"`,
		`depends="xbps>=0.60.0"`,
		"export CGO_ENABLED=0",
		`export GOFLAGS="-trimpath -buildvcs=false"`,
		"vcompletion completions/xbpretty.bash bash xbpretty",
		"vcompletion completions/_xbpretty zsh xbpretty",
		"vcompletion completions/xbpretty.fish fish xbpretty",
		"vman man/xbpretty.8",
		"vman man/xbpretty.conf.5",
		"vdoc docs/config.toml.example",
		"vdoc docs/migration-rev6.md",
		"vlicense LICENSE",
	} {
		if !strings.Contains(template, want) {
			t.Errorf("template is missing %q", want)
		}
	}
	if strings.Contains(template, "-s -w") {
		t.Error("go_ldflags must be version-only, never -s/-w")
	}
	for _, alias := range spec.LinkNames() {
		if !strings.Contains(template, alias) {
			t.Errorf("template does not install alias %q", alias)
		}
		link := alias + ".8"
		if !strings.Contains(template, link) {
			t.Errorf("template does not install man8 link %q", link)
		}
	}
	if strings.Contains(template, "conf_files") {
		t.Error("template has a conf_files entry")
	}
	if strings.Contains(template, "/etc") {
		t.Error("template mentions /etc; the package must install no system config")
	}
}

// TestBuildScriptMatchesTheTemplate pins the offline path: the same version
// stamp, the same install paths and the same no-/etc rule.
func TestBuildScriptMatchesTheTemplate(t *testing.T) {
	script := read(t, "tools/build-xbps.sh")
	for _, want := range []string{
		`-X $import_path/internal/cli.Version=$version`,
		"GOOS=linux CGO_ENABLED=0",
		"completions/xbpretty.bash",
		"completions/_xbpretty",
		"completions/xbpretty.fish",
		"man/xbpretty.8",
		"man/xbpretty.conf.5",
		"docs/config.toml.example",
		"docs/migration-rev6.md",
		"LICENSE",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("build script is missing %q", want)
		}
	}
	if strings.Contains(script, "-s -w") {
		t.Error("build script strips the binary; FR-076 makes go_ldflags version-only")
	}
	for _, alias := range spec.LinkNames() {
		if !strings.Contains(script, alias) {
			t.Errorf("build script does not install alias %q", alias)
		}
	}
	if strings.Contains(script, "conf_files") || strings.Contains(script, "/etc/xbpretty") {
		t.Error("build script installs an /etc/xbpretty config")
	}
}

// TestMakefileExposesGenerationAndReleaseChecks keeps the drift gates wired
// into the release path (FR-005, FR-076, AC-003, AC-059).
func TestMakefileExposesGenerationAndReleaseChecks(t *testing.T) {
	makefile := read(t, "Makefile")
	for _, want := range []string{
		"completions:",
		"manpages:",
		"release-check:",
		"tools/parity -check",
		"mandoc -T lint",
		"bash -n completions/xbpretty.bash",
		"zsh -n completions/_xbpretty",
		"fish -n completions/xbpretty.fish",
	} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile is missing %q", want)
		}
	}
}

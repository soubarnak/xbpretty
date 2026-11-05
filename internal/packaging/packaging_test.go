package packaging

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/spec"
)

var installedFiles = []string{
	"/usr/share/bash-completion/completions/xbpretty",
	"/usr/share/zsh/site-functions/_xbpretty",
	"/usr/share/fish/vendor_completions.d/xbpretty.fish",
	"/usr/share/man/man8/xbpretty.8",
	"/usr/share/man/man5/xbpretty.conf.5",
	"/usr/share/doc/xbpretty/config.toml.example",
	"/usr/share/doc/xbpretty/migration-rev6.md",
	"/usr/share/licenses/xbpretty/LICENSE",
}

var installedSources = []string{
	"completions/xbpretty.bash",
	"completions/_xbpretty",
	"completions/xbpretty.fish",
	"man/xbpretty.8",
	"man/xbpretty.conf.5",
	"docs/config.toml.example",
	"docs/migration-rev6.md",
	"LICENSE",
}

func aliasLoopList(source string) []string {
	marker := "for alias in "
	start := strings.Index(source, marker)
	if start < 0 {
		return nil
	}
	rest := source[start+len(marker):]
	end := strings.Index(rest, ";")
	if end < 0 {
		return nil
	}
	return strings.Fields(rest[:end])
}

var man8LinkPattern = regexp.MustCompile(`ln -s xbpretty\.8 "[^"]*/([^/"]+)"`)

func man8LinkSet(source string) []string {
	var links []string
	for _, match := range man8LinkPattern.FindAllStringSubmatch(source, -1) {
		if strings.HasSuffix(match[1], ".8") {
			links = append(links, match[1])
		}
	}
	sort.Strings(links)
	return links
}

func wantMan8Links() []string {
	links := make([]string, 0, len(spec.LinkNames()))
	for _, alias := range spec.LinkNames() {
		links = append(links, alias+".8")
	}
	sort.Strings(links)
	return links
}

func sortedCopy(items []string) []string {
	out := append([]string(nil), items...)
	sort.Strings(out)
	return out
}

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

func TestAliasSetsAreExactlySix(t *testing.T) {
	wantBinary := sortedCopy(spec.LinkNames())
	wantMan8 := wantMan8Links()
	for _, source := range []struct {
		name string
		path string
	}{
		{"template", "srcpkgs/xbpretty/template"},
		{"build script", "tools/build-xbps.sh"},
	} {
		text := read(t, source.path)
		if got := sortedCopy(aliasLoopList(text)); strings.Join(got, ",") != strings.Join(wantBinary, ",") {
			t.Errorf("%s binary alias set = %v, want exactly %v", source.name, got, wantBinary)
		}
		if got := man8LinkSet(text); strings.Join(got, ",") != strings.Join(wantMan8, ",") {
			t.Errorf("%s man8 alias set = %v, want exactly %v", source.name, got, wantMan8)
		}
	}
}

func TestTemplateAndBuildScriptInstallTheSameFiles(t *testing.T) {
	template := read(t, "srcpkgs/xbpretty/template")
	script := read(t, "tools/build-xbps.sh")
	man8 := read(t, "man/xbpretty.8")
	if len(installedFiles) != len(installedSources) {
		t.Fatal("installed file and source lists disagree")
	}
	for i, path := range installedFiles {
		if !strings.Contains(script, path) {
			t.Errorf("build script does not install %s", path)
		}
		if !strings.Contains(template, installedSources[i]) {
			t.Errorf("template does not install source %s", installedSources[i])
		}
		if !strings.Contains(man8, path) {
			t.Errorf("xbpretty.8 FILES does not document installed path %s", path)
		}
	}
	if !strings.Contains(script, "$root/usr/bin/$alias") {
		t.Error("build script does not install the binary aliases")
	}
	if !strings.Contains(template, "${DESTDIR}/usr/bin/${alias}") {
		t.Error("template does not install the binary aliases")
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
	for _, alias := range spec.LinkNames() {
		if !strings.Contains(script, alias) {
			t.Errorf("build script does not install alias %q", alias)
		}
	}
	if strings.Contains(script, "conf_files") || strings.Contains(script, "/etc") {
		t.Error("build script installs or mentions an /etc path")
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
		"RELEASE_STRICT",
		"skipped",
	} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile is missing %q", want)
		}
	}
}

package packaging

// Release-path tests for FR-076 / AC-058–AC-060: the version and alias sets
// drift across every shipped artifact, the workflow builds and gates the nine
// architectures, signing refuses an unset key, and the size/static/content
// gates fail when a budget or an installed file is violated.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	releaseVersion = "0.4.0"
	releaseTag     = "xbpretty-v0.4.0"
	// These literals come from FR-001 / AC-058, not from spec.LinkNames(), so
	// a change to the implementation constant cannot make the drift test
	// agree with itself.
	releaseAliases = "xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update"
)

var (
	aliasTokenPattern    = regexp.MustCompile(`\bxbp(?:-[a-z]+)?\b`)
	backtickTokenPattern = regexp.MustCompile("`([^`]+)`")
)

func repoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

func aliasTokens(text string) []string {
	seen := map[string]bool{}
	for _, match := range aliasTokenPattern.FindAllString(text, -1) {
		seen[match] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func wantAliases(t *testing.T) []string {
	t.Helper()
	want := strings.Fields(releaseAliases)
	sort.Strings(want)
	return want
}

func TestAliasSetDriftsAcrossEveryArtifact(t *testing.T) {
	want := wantAliases(t)
	for _, artifact := range []string{
		"srcpkgs/xbpretty/template",
		"tools/build-xbps.sh",
		"completions/xbpretty.bash",
		"completions/_xbpretty",
		"completions/xbpretty.fish",
		"man/xbpretty.8",
		"README.md",
	} {
		got := aliasTokens(read(t, artifact))
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s alias set = %v, want exactly %v", artifact, got, want)
		}
	}

	// The spec is the source the other artifacts must agree with: FR-001
	// names the closed six, and no generated surface may add another name.
	spec := read(t, "xbpretty-spec.md")
	fr001 := ""
	for _, line := range strings.Split(spec, "\n") {
		if strings.HasPrefix(line, "| FR-001 |") {
			fr001 = line
			break
		}
	}
	if fr001 == "" {
		t.Fatal("spec has no FR-001 row")
	}
	_, mentioned, ok := strings.Cut(fr001, "exactly six alias names:")
	if !ok {
		t.Fatal("FR-001 does not name the closed six-alias set")
	}
	mentioned = strings.SplitN(mentioned, ".", 2)[0]
	var specAliases []string
	for _, match := range backtickTokenPattern.FindAllStringSubmatch(mentioned, -1) {
		if aliasTokenPattern.MatchString(match[1]) {
			specAliases = append(specAliases, match[1])
		}
	}
	sort.Strings(specAliases)
	if strings.Join(specAliases, " ") != strings.Join(want, " ") {
		t.Errorf("FR-001 alias set = %v, want exactly %v", specAliases, want)
	}
}

func TestVersionIsSingleSourcedFromTheTemplate(t *testing.T) {
	template := read(t, "srcpkgs/xbpretty/template")
	for _, want := range []string{
		"version=" + releaseVersion,
		"revision=1",
		`go_ldflags="-X ${go_import_path}/internal/cli.Version=${version}"`,
	} {
		if !strings.Contains(template, want) {
			t.Errorf("template is missing %q", want)
		}
	}

	script := read(t, "tools/build-xbps.sh")
	for _, want := range []string{
		`version=$(sed -n 's/^version=//p' "$here/srcpkgs/xbpretty/template")`,
		`-X $import_path/internal/cli.Version=$version`,
		"GOOS=linux CGO_ENABLED=0",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("build script is missing %q", want)
		}
	}

	workflow := read(t, ".github/workflows/release.yml")
	for _, want := range []string{
		`tags: ["xbpretty-v*"]`,
		`srcpkgs/xbpretty/template`,
		`[ "${GITHUB_REF_NAME}" = "xbpretty-v${version}" ]`,
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow is missing %q", want)
		}
	}

	spec := read(t, "xbpretty-spec.md")
	for _, want := range []string{"**" + releaseVersion + "**", releaseTag} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec is missing %q", want)
		}
	}
	for _, artifact := range []string{"README.md", "docs/release-notes-" + releaseVersion + ".md"} {
		if text := read(t, artifact); !strings.Contains(text, releaseVersion) {
			t.Errorf("%s does not name version %s", artifact, releaseVersion)
		}
	}
}

func TestNoStaleVersionOnAnyShippedSurface(t *testing.T) {
	// Every surface a user can read must agree with the template identity:
	// none may still name the previous release. Generated completions and
	// manpages carry the alias set rather than a version string, so this also
	// catches a partial bump there.
	for _, artifact := range []string{
		"srcpkgs/xbpretty/template",
		"tools/build-xbps.sh",
		"tools/release-sign.sh",
		"tools/release-gates.sh",
		".github/workflows/release.yml",
		"xbpretty-spec.md",
		"README.md",
		"docs/release-notes-" + releaseVersion + ".md",
		"completions/xbpretty.bash",
		"completions/_xbpretty",
		"completions/xbpretty.fish",
		"man/xbpretty.8",
		"man/xbpretty.conf.5",
	} {
		if text := read(t, artifact); strings.Contains(text, "0.3.2") {
			t.Errorf("%s still names the previous release 0.3.2", artifact)
		}
	}
}

func TestVersionStampedBinaryPrintsReleaseVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the release binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "xbpretty")
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false",
		"-ldflags", "-X github.com/soubarnak/xbpretty/internal/cli.Version="+releaseVersion,
		"-o", bin, "./cmd/xbpretty")
	cmd.Dir = repoPath()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build version-stamped binary: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.HasPrefix(string(out), "xbpretty "+releaseVersion+"\n") {
		t.Errorf("--version stdout = %q, want it to start with %q", out, "xbpretty "+releaseVersion+"\n")
	}
}

func TestReleaseWorkflowBuildsAndGatesTheNineArchitectures(t *testing.T) {
	workflow := read(t, ".github/workflows/release.yml")
	for _, want := range []string{
		"ARCHES: x86_64 x86_64-musl i686 aarch64 aarch64-musl armv7l armv7l-musl armv6l armv6l-musl",
		"tools/build-xbps.sh",
		"tools/release-sign.sh",
		"go test ./...",
		"go vet ./...",
		"make release-check",
		"tools/release-gates.sh binary",
		"tools/release-gates.sh package",
		"tools/release-gates.sh contents",
		"tools/release-gates.sh assets",
		"tools/release-gates.sh snapshot",
		"xbps-install",
		"--rootdir",
		"XBPS_PRIVKEY: ${{ secrets.XBPS_PRIVKEY }}",
		"checksum=",
		"s/^checksum=.*/checksum=${sum}/",
		"docs/release-notes-${VERSION}.md",
		"--notes-file",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow is missing %q", want)
		}
	}

	script := read(t, "tools/build-xbps.sh")
	scriptArches := ""
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "archs=${*:-") {
			scriptArches = strings.TrimSuffix(strings.TrimPrefix(line, "archs=${*:-"), "}")
		}
	}
	if scriptArches != "x86_64 x86_64-musl i686 aarch64 aarch64-musl armv7l armv7l-musl armv6l armv6l-musl" {
		t.Errorf("build script default architecture list = %q, want all nine", scriptArches)
	}
}

func TestReleaseGatesEnforceBudgetsAndStaticLink(t *testing.T) {
	gate := repoPath("tools", "release-gates.sh")
	big := filepath.Join(t.TempDir(), "xbpretty")
	if err := os.WriteFile(big, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, 8_000_001); err != nil {
		t.Fatal(err)
	}
	if out, err := runGate(gate, "binary", big); err == nil {
		t.Fatalf("binary gate accepted a %d-byte binary:\n%s", 8_000_001, out)
	} else if !strings.Contains(out, "8000000") {
		t.Errorf("binary gate failure does not name the 8,000,000-byte budget:\n%s", out)
	}

	bigPkg := filepath.Join(t.TempDir(), "xbpretty-0.4.0_1.x86_64.xbps")
	if err := os.WriteFile(bigPkg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(bigPkg, 3_600_001); err != nil {
		t.Fatal(err)
	}
	if out, err := runGate(gate, "package", bigPkg); err == nil {
		t.Fatalf("package gate accepted a %d-byte package:\n%s", 3_600_001, out)
	} else if !strings.Contains(out, "3600000") {
		t.Errorf("package gate failure does not name the 3,600,000-byte budget:\n%s", out)
	}

	dynamic := "/bin/sh"
	if _, err := os.Stat(dynamic); err == nil {
		if out, err := runGate(gate, "binary", dynamic); err == nil {
			t.Errorf("static-link gate accepted dynamically linked %s:\n%s", dynamic, out)
		}
	}

	static := buildStaticFixture(t)
	if out, err := runGate(gate, "binary", static); err != nil {
		t.Errorf("static-link gate rejected a static binary: %v\n%s", err, out)
	}
}

func TestReleaseContentsGateChecksTheInstalledSurface(t *testing.T) {
	gate := repoPath("tools", "release-gates.sh")
	root := contentFixture(t)
	if out, err := runGate(gate, "contents", root); err != nil {
		t.Fatalf("contents gate rejected the complete surface: %v\n%s", err, out)
	}

	missingDocs := contentFixture(t)
	if err := os.Remove(filepath.Join(missingDocs, "usr/share/doc/xbpretty/migration-rev6.md")); err != nil {
		t.Fatal(err)
	}
	if out, err := runGate(gate, "contents", missingDocs); err == nil {
		t.Fatalf("contents gate accepted a missing migration guide:\n%s", out)
	}

	withEtc := contentFixture(t)
	if err := os.MkdirAll(filepath.Join(withEtc, "etc/xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runGate(gate, "contents", withEtc); err == nil {
		t.Fatalf("contents gate accepted /etc/xbpretty:\n%s", out)
	}
}

func TestReleaseSnapshotGateRejectsAStaleEmbeddedList(t *testing.T) {
	gate := repoPath("tools", "release-gates.sh")
	if out, err := runGate(gate, "snapshot", "2020-01-01", "90"); err == nil {
		t.Fatalf("snapshot gate accepted a stale list:\n%s", out)
	}
	today := time.Now().UTC().Format("2006-01-02")
	if out, err := runGate(gate, "snapshot", today, "90"); err != nil {
		t.Errorf("snapshot gate rejected today's date: %v\n%s", err, out)
	}
}

func TestReleaseSigningFailsWithoutKey(t *testing.T) {
	sign := repoPath("tools", "release-sign.sh")
	cmd := exec.Command("sh", sign, t.TempDir(), releaseVersion)
	cmd.Env = envWithout("XBPS_PRIVKEY")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("release-sign.sh accepted an unset XBPS_PRIVKEY")
	}
	const want = "secret XBPS_PRIVKEY is not set; refusing to publish an unsigned repository"
	if !strings.Contains(string(out), want) {
		t.Errorf("missing-key failure = %q, want it to contain %q", out, want)
	}
}

func TestReleaseSignsEveryArchitecture(t *testing.T) {
	assets := t.TempDir()
	archs := strings.Fields("x86_64 x86_64-musl i686 aarch64 aarch64-musl armv7l armv7l-musl armv6l armv6l-musl")
	for _, arch := range archs {
		name := "xbpretty-" + releaseVersion + "_1." + arch + ".xbps"
		if err := os.WriteFile(filepath.Join(assets, name), []byte("package"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stubDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "rindex.log")
	stub := `#!/bin/sh
printf '%s %s\n' "$XBPS_ARCH" "$*" >> "$SIGN_LOG"
case " $* " in
  *" -s "*) touch "$XBPS_ARCH-repodata" ;;
esac
case " $* " in
  *" -S "*) last=""; for arg in "$@"; do last=$arg; done; touch "$last.sig2" ;;
esac
`
	if err := os.WriteFile(filepath.Join(stubDir, "xbps-rindex"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", repoPath("tools", "release-sign.sh"), assets, releaseVersion)
	cmd.Env = append(envWithout("XBPS_PRIVKEY"), "XBPS_PRIVKEY=dummy", "SIGN_LOG="+logPath, "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release-sign.sh failed: %v\n%s", err, out)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, arch := range archs {
		if !strings.Contains(string(logged), arch+" ") {
			t.Errorf("signing never ran for %s:\n%s", arch, logged)
		}
		if _, err := os.Stat(filepath.Join(assets, arch+"-repodata")); err != nil {
			t.Errorf("no repodata for %s: %v", arch, err)
		}
		if _, err := os.Stat(filepath.Join(assets, "xbpretty-"+releaseVersion+"_1."+arch+".xbps.sig2")); err != nil {
			t.Errorf("no signature for %s: %v", arch, err)
		}
	}
	if out, err := runGate(repoPath("tools", "release-gates.sh"), "assets", assets, releaseVersion); err != nil {
		t.Errorf("assets gate rejected signed artifacts: %v\n%s", err, out)
	}
	if err := os.Remove(filepath.Join(assets, "xbpretty-"+releaseVersion+"_1.x86_64.xbps.sig2")); err != nil {
		t.Fatal(err)
	}
	if out, err := runGate(repoPath("tools", "release-gates.sh"), "assets", assets, releaseVersion); err == nil {
		t.Errorf("assets gate accepted a missing signature:\n%s", out)
	}
}

func TestReleaseNotesCarryInstallFeaturesAndMigration(t *testing.T) {
	notes := read(t, "docs/release-notes-"+releaseVersion+".md")
	for _, want := range []string{
		"# xbpretty " + releaseVersion,
		"releases/latest/download",
		"sudo xbps-install -S xbpretty",
		"fa:d1:00:32:ce:b1:b9:fd:73:05:05:76:75:54:37:25",
		"history",
		"undo",
		"redo",
		"TUI",
		"https://github.com/soubarnak/xbpretty/blob/main/docs/migration-rev6.md",
		"https://github.com/soubarnak/xbpretty/blob/main/docs/config.toml.example",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("release notes are missing %q", want)
		}
	}
}

func TestMirrorSnapshotIsDatedAndRefreshable(t *testing.T) {
	source := read(t, "internal/fetch/snapshot.go")
	if !strings.Contains(source, `const SnapshotDate = "`) {
		t.Fatal("snapshot.go does not embed a SnapshotDate")
	}
	makefile := read(t, "Makefile")
	for _, want := range []string{"mirrors:", "tools/mirrors", "MIRRORS_URL"} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile is missing %q", want)
		}
	}
	workflow := read(t, ".github/workflows/release.yml")
	if !strings.Contains(workflow, "tools/release-gates.sh snapshot") {
		t.Error("release workflow does not gate the mirror snapshot age")
	}
}

func runGate(script string, args ...string) (string, error) {
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func envWithout(name string) []string {
	var env []string
	prefix := name + "="
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, prefix) {
			env = append(env, entry)
		}
	}
	return env
}

func buildStaticFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/static\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "static")
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build static fixture: %v\n%s", err, out)
	}
	return bin
}

func contentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(path string) {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		"usr/bin/xbpretty",
		"usr/share/bash-completion/completions/xbpretty",
		"usr/share/zsh/site-functions/_xbpretty",
		"usr/share/fish/vendor_completions.d/xbpretty.fish",
		"usr/share/man/man8/xbpretty.8",
		"usr/share/man/man5/xbpretty.conf.5",
		"usr/share/doc/xbpretty/config.toml.example",
		"usr/share/doc/xbpretty/migration-rev6.md",
		"usr/share/licenses/xbpretty/LICENSE",
	} {
		write(path)
	}
	link := func(path, target string) {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
	for _, alias := range strings.Fields(releaseAliases) {
		link("usr/bin/"+alias, "xbpretty")
	}
	for _, alias := range strings.Fields(releaseAliases) {
		link("usr/share/man/man8/"+alias+".8", "xbpretty.8")
	}
	return root
}

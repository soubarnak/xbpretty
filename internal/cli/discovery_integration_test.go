package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Discovery fixtures exercise the fixed offline pass set through the built
// binary. They are deliberately one repository and one installed set so the
// expected rows can be written out byte for byte.
const (
	discInstalled = `ii alpha-1.0_1 A first package
ii heldpkg-1.0_1 Held package
ii local-only-1.0_1 Local only package
`
	discManual = `alpha-1.0_1
heldpkg-1.0_1
`
	discRepo = `[-] alpha-2.0_1 A first package
[-] beta-1.0_1 Beta package
[-] virtual-provider-1.0_1 Virtual provider
`
	discRepoProp = `alpha-2.0_1: https://repo-de.voidlinux.org/current
beta-1.0_1: https://repo-de.voidlinux.org/current
virtual-provider-1.0_1: https://repo-de.voidlinux.org/current
`
	discArch = `alpha-2.0_1: x86_64 (https://repo-de.voidlinux.org/current)
beta-1.0_1: x86_64 (https://repo-de.voidlinux.org/current)
virtual-provider-1.0_1: x86_64 (https://repo-de.voidlinux.org/current)
`
	discProvides = `virtual-provider-1.0_1: cmd:thing-1.0_1 (https://repo-de.voidlinux.org/current)
virtual-provider-1.0_1: shlib:libthing.so.1 (https://repo-de.voidlinux.org/current)
`
	discPlan = `alpha-2.0_1 update x86_64 https://repo-de.voidlinux.org/current 1000 500
`
	discShowInstalled = `architecture: x86_64
automatic-install: yes
filename-size: 2KB
homepage: https://example.test/alpha
install-date: 2026-01-01 00:00 UTC
installed_size: 10KB
license: MIT
maintainer: A <a@example.test>
pkgname: alpha
pkgver: alpha-1.0_1
repository: https://repo-de.voidlinux.org/current
run_depends:
	dep-one>=1
	dep-two>=2
	dep-three>=3
	dep-four>=4
short_desc: A first package
state: installed
`
	discShowRepo = `architecture: x86_64
filename-size: 3KB
installed_size: 12KB
pkgname: alpha
pkgver: alpha-2.0_1
repository: https://repo-de.voidlinux.org/current
short_desc: A first package
`
)

// discoveryEnv builds the binary and the controlled environment every test
// below shares. The map values are copied into the child through envWith, so
// a host XDG_CONFIG_HOME cannot leak in.
func discoveryEnv(t *testing.T) (bin, cfgDir, logPath string, env map[string]string) {
	t.Helper()
	bin = buildBinary(t)
	cfgDir, _ = integrationConfig(t, "Never")
	logPath = filepath.Join(t.TempDir(), "commands.log")
	env = map[string]string{
		"XDG_CONFIG_HOME":   cfgDir,
		"XDG_STATE_HOME":    filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":       logPath,
		"NO_COLOR":          "",
		"XB_STUB_LIST":      discInstalled,
		"XB_STUB_MANUAL":    discManual,
		"XB_STUB_REPO":      discRepo,
		"XB_STUB_REPO_PROP": discRepoProp,
		"XB_STUB_ARCH":      discArch,
		"XB_STUB_PROVIDES":  discProvides,
		"XB_STUB_HOLD":      "heldpkg-1.0_1\n",
		"XB_STUB_ORPHANS":   "",
		"XB_STUB_REPOLOCK":  "",
		"XB_STUB_STDOUT":    discPlan,
		"XB_STUB_STDERR":    "",
		"XB_STUB_SHOW":      discShowInstalled,
		"XB_STUB_SHOW_REPO": discShowRepo,
	}
	return bin, cfgDir, logPath, env
}

// envWith replaces the override keys instead of appending duplicates, which
// makes each subprocess see exactly one value per variable.
func envWith(overrides map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, replaced := overrides[k]; replaced {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func runDiscovery(t *testing.T, bin string, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = envWith(env)
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("running %v: %v", args, err)
	return 0, "", ""
}

func commandsFromLog(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading command log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestAC019ListRowsFormatSortLocalAndAttributes(t *testing.T) {
	bin, _, logPath, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "list")
	if code != 0 {
		t.Fatalf("list exit = %d, want 0\nstderr:\n%s", code, errs)
	}
	want := "" +
		"alpha (2.0_1) [repo-de.voidlinux.org/current] [Upgradable from: 1.0_1]\n" +
		"beta (1.0_1) [repo-de.voidlinux.org/current] []\n" +
		"heldpkg (1.0_1) [local] [Installed, Held]\n" +
		"local-only (1.0_1) [local] [Installed, Automatic]\n" +
		"virtual-provider (1.0_1) [repo-de.voidlinux.org/current] []\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want empty", errs)
	}

	cmds := commandsFromLog(t, logPath)
	wantCalls := []string{
		"-l",
		"-m",
		"-R -s ",
		"-R -s  -p repository",
		"-H",
		"-O",
		"--list-repolock-pkgs",
		"-n -u",
	}
	for _, wantCall := range wantCalls {
		found := false
		for _, got := range cmds {
			if got == wantCall {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing fixed offline call %q in:\n%s", wantCall, strings.Join(cmds, "\n"))
		}
	}
	for _, got := range cmds {
		for _, banned := range []string{"-M", "-R -o", "-S"} {
			if strings.Contains(got, banned) {
				t.Errorf("forbidden discovery call %q (contains %q)", got, banned)
			}
		}
	}
}

func TestAC019AllVersionsInstalledUpgradableVirtual(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)

	code, out, errs := runDiscovery(t, bin, env, "list", "-a")
	if code != 0 {
		t.Fatalf("list -a exit = %d\n%s", code, errs)
	}
	wantA := "" +
		"alpha (1.0_1) [local] [Installed]\n" +
		"alpha (2.0_1) [repo-de.voidlinux.org/current] [Upgradable from: 1.0_1]\n" +
		"beta (1.0_1) [repo-de.voidlinux.org/current] []\n" +
		"heldpkg (1.0_1) [local] [Installed, Held]\n" +
		"local-only (1.0_1) [local] [Installed, Automatic]\n" +
		"virtual-provider (1.0_1) [repo-de.voidlinux.org/current] []\n"
	if out != wantA {
		t.Errorf("list -a stdout =\n%s\nwant:\n%s", out, wantA)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "-i")
	if code != 0 {
		t.Fatalf("list -i exit = %d\n%s", code, errs)
	}
	wantI := "" +
		"alpha (2.0_1) [repo-de.voidlinux.org/current] [Upgradable from: 1.0_1]\n" +
		"heldpkg (1.0_1) [local] [Installed, Held]\n" +
		"local-only (1.0_1) [local] [Installed, Automatic]\n"
	if out != wantI {
		t.Errorf("list -i stdout =\n%s\nwant:\n%s", out, wantI)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "-u")
	if code != 0 {
		t.Fatalf("list -u exit = %d\n%s", code, errs)
	}
	wantU := "alpha (2.0_1) [repo-de.voidlinux.org/current] [Upgradable from: 1.0_1]\n"
	if out != wantU {
		t.Errorf("list -u stdout =\n%s\nwant:\n%s", out, wantU)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "-V")
	if code != 0 {
		t.Fatalf("list -V exit = %d\n%s", code, errs)
	}
	wantV := "" +
		"cmd:thing (Virtual) [virtual-provider]\n" +
		"shlib:libthing.so.1 (Virtual) [virtual-provider]\n"
	if out != wantV {
		t.Errorf("list -V stdout =\n%s\nwant:\n%s", out, wantV)
	}
}

func TestAC019NonMatchingGlobNoticeExitZero(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "list", "zzz*")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if errs != "Notice: 'zzz*' was not found\n" {
		t.Errorf("stderr = %q", errs)
	}
}

func TestAC019SearchMatchesNamesThenShortDescUnlessNames(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "search", "first")
	if code != 0 {
		t.Fatalf("search first exit = %d\n%s", code, errs)
	}
	want := "alpha (2.0_1) [repo-de.voidlinux.org/current] [Upgradable from: 1.0_1]\n"
	if out != want {
		t.Errorf("search first stdout =\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(errs, "Notice:") {
		t.Errorf("search wrote a notice:\n%s", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "search", "--names", "first")
	if code != 0 || out != "" {
		t.Errorf("search --names first = %d, %q; want empty success\n%s", code, out, errs)
	}

	code, out, _ = runDiscovery(t, bin, env, "search", "--names", "alpha")
	if code != 0 || !strings.HasPrefix(out, "alpha (2.0_1)") {
		t.Errorf("search --names alpha = %d, %q", code, out)
	}
}

func TestAC022MachineRowsAreNDJSONSchemaOne(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "list", "-m", "--color=always")
	if code != 0 {
		t.Fatalf("list -m exit = %d\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("machine output carried ANSI:\n%q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d machine rows, want 5:\n%s", len(lines), out)
	}
	for _, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		if obj["schema"] != float64(1) || obj["kind"] != "package" {
			t.Errorf("line %q lacks schema 1 package identity", line)
		}
	}
	var alpha map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &alpha); err != nil {
		t.Fatal(err)
	}
	if alpha["name"] != "alpha" || alpha["pkgver"] != "alpha-2.0_1" {
		t.Errorf("alpha row = %v", alpha)
	}
	if alpha["installed_size"] != nil || alpha["download_size"] != nil {
		t.Errorf("non-upgradable sizes = %v/%v, want null", alpha["installed_size"], alpha["download_size"])
	}
	if alpha["installed"] != false || alpha["installed_version"] != "1.0_1" {
		t.Errorf("alpha installed state = %v/%v", alpha["installed"], alpha["installed_version"])
	}
	if _, ok := alpha["repository"]; !ok || alpha["repository"] == "" {
		t.Errorf("alpha repository missing: %v", alpha)
	}
	var local map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &local); err != nil {
		t.Fatal(err)
	}
	if local["name"] != "heldpkg" || local["installed"] != true {
		t.Fatalf("third machine row = %v, want heldpkg", local)
	}
	if _, ok := local["repository"]; ok {
		t.Errorf("local row carries repository: %v", local)
	}
	if !strings.Contains(lines[0], `"schema":1`) {
		t.Errorf("row is not compact schema 1: %q", lines[0])
	}

	code, out, _ = runDiscovery(t, bin, env, "list", "-u", "-m")
	if code != 0 {
		t.Fatalf("list -u -m exit = %d", code)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &alpha); err != nil {
		t.Fatal(err)
	}
	if alpha["installed_size"] != float64(1000) || alpha["download_size"] != float64(500) {
		t.Errorf("upgradable exact sizes = %v/%v, want 1000/500", alpha["installed_size"], alpha["download_size"])
	}

	code, out, _ = runDiscovery(t, bin, env, "list", "-V", "-m")
	if code != 0 {
		t.Fatalf("list -V -m exit = %d", code)
	}
	lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("virtual machine rows = %d:\n%s", len(lines), out)
	}
	var virtual map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &virtual); err != nil {
		t.Fatal(err)
	}
	if virtual["kind"] != "virtual" || virtual["name"] != "cmd:thing" {
		t.Errorf("virtual row = %v", virtual)
	}
	if _, ok := virtual["installed_size"]; ok {
		t.Errorf("virtual row carries package-only fields: %v", virtual)
	}
}

func TestAC020ShowFieldOrderAbsentPropertiesAndLongListWrapping(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "show", "alpha")
	if code != 0 {
		t.Fatalf("show exit = %d\n%s", code, errs)
	}
	want := "" +
		"Package: alpha\n" +
		"Version: 1.0_1\n" +
		"Architecture: x86_64\n" +
		"Installed-Size: 10KB\n" +
		"Size: 2KB\n" +
		"Maintainer: A <a@example.test>\n" +
		"Homepage: https://example.test/alpha\n" +
		"License: MIT\n" +
		"Repository: https://repo-de.voidlinux.org/current\n" +
		"State: installed\n" +
		"Install-Date: 2026-01-01 00:00 UTC\n" +
		"Automatic: yes\n" +
		"Depends: dep-one>=1\n" +
		"    dep-two>=2\n" +
		"    dep-three>=3\n" +
		"    dep-four>=4\n" +
		"Description: A first package\n" +
		"Attributes: [Installed, Automatic]\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
	for _, absent := range []string{"Priority:", "Section:", "SHA256:", "Held:", "Repolocked:", "Unknown"} {
		if strings.Contains(out, absent) {
			t.Errorf("show printed %q:\n%s", absent, out)
		}
	}
	wantNotice := "Notice: There are 1 additional records. Please use the '-a' switch to see them.\n"
	if errs != wantNotice {
		t.Errorf("stderr = %q, want %q", errs, wantNotice)
	}
}

func TestAC020ShowAllPrintsInstalledThenVariants(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "show", "-a", "alpha")
	if code != 0 {
		t.Fatalf("show -a exit = %d\n%s", code, errs)
	}
	wantRepoBlock := "" +
		"Package: alpha\n" +
		"Version: 2.0_1\n" +
		"Architecture: x86_64\n" +
		"Installed-Size: 12KB\n" +
		"Size: 3KB\n" +
		"Repository: https://repo-de.voidlinux.org/current\n" +
		"Description: A first package\n" +
		"Attributes: []\n"
	if !strings.Contains(out, "Version: 1.0_1") || !strings.Contains(out, wantRepoBlock) {
		t.Errorf("show -a missing a block:\n%s", out)
	}
	if strings.Index(out, "Version: 1.0_1") > strings.Index(out, "Version: 2.0_1") {
		t.Errorf("variants printed before the installed block:\n%s", out)
	}
	if errs != "" {
		t.Errorf("show -a stderr = %q, want empty", errs)
	}
}

func TestAC020ShowMissingIsNoticeAndZeroNamesIsExitOne(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs := runDiscovery(t, bin, env, "show", "ghost")
	if code != 0 {
		t.Fatalf("show ghost exit = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if errs != "Notice: 'ghost' was not found\n" {
		t.Errorf("stderr = %q", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "show")
	if code != 1 {
		t.Errorf("show with zero names exit = %d, want 1", code)
	}
	if out != "" || errs != "xbpretty: show needs at least one package\n" {
		t.Errorf("zero names stdout = %q stderr = %q", out, errs)
	}
}

func TestAC022ShowMachineCarriesPropertiesAndNoANSI(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "show", "-m", "--color=always", "alpha")
	if code != 0 {
		t.Fatalf("show -m exit = %d\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("machine show carried ANSI:\n%q", out)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &obj); err != nil {
		t.Fatalf("show -m is not one JSON object: %v\n%s", err, out)
	}
	if obj["schema"] != float64(1) || obj["kind"] != "package" || obj["name"] != "alpha" {
		t.Errorf("show machine identity = %v", obj)
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is not a map: %T", obj["properties"])
	}
	if props["pkgname"] != "alpha" || props["state"] != "installed" {
		t.Errorf("properties = %v", props)
	}
	if obj["installed_size"] != nil || obj["download_size"] != nil {
		t.Errorf("show sizes = %v/%v, want null", obj["installed_size"], obj["download_size"])
	}
}

// TestAC018RepositoryAttributionFallsBackToArchitecturePass covers the host
// xbps 0.60.7 shape: `-R -s ” -p repository` is empty, while the architecture
// property carries the parenthesised repository. The fallback stays inside
// the fixed offline pass set and only runs when the repository pass is empty.
func TestAC018RepositoryAttributionFallsBackToArchitecturePass(t *testing.T) {
	bin, _, logPath, env := discoveryEnv(t)
	env["XB_STUB_REPO_PROP"] = ""
	code, out, errs := runDiscovery(t, bin, env, "list", "beta")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	want := "beta (1.0_1) [repo-de.voidlinux.org/current] []\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	foundArch := false
	for _, cmd := range commandsFromLog(t, logPath) {
		if cmd == "-R -s  -p architecture" {
			foundArch = true
		}
		if strings.Contains(cmd, "-M") || strings.Contains(cmd, "-R -o") || cmd == "-S" {
			t.Errorf("forbidden discovery call %q", cmd)
		}
	}
	if !foundArch {
		t.Errorf("architecture fallback did not run:\n%s", strings.Join(commandsFromLog(t, logPath), "\n"))
	}
}

func TestAC022ShowAllMachineIsOneObjectPerLine(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "show", "-a", "-m", "alpha")
	if code != 0 {
		t.Fatalf("show -a -m exit = %d\n%s", code, errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%q", len(lines), out)
	}
	for _, line := range lines {
		if line == "" {
			t.Errorf("machine output contains a blank line: %q", out)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %q is not JSON: %v", line, err)
		}
		if _, ok := obj["properties"]; !ok {
			t.Errorf("show machine row missing properties: %v", obj)
		}
	}
}

func TestAC020ShowAllMissingStillNotice(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs := runDiscovery(t, bin, env, "show", "-a", "ghost")
	if code != 0 || out != "" {
		t.Errorf("show -a ghost = %d, %q; want 0, empty", code, out)
	}
	if errs != "Notice: 'ghost' was not found\n" {
		t.Errorf("stderr = %q", errs)
	}
}

func TestAC019SummaryDescriptionCompositionAndOrGlobs(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	env["XB_STUB_LONG_DESC"] = "alpha-2.0_1: The long alpha description (https://repo-de.voidlinux.org/current)\n"
	code, out, errs := runDiscovery(t, bin, env, "list", "--summary", "beta")
	if code != 0 {
		t.Fatalf("list --summary exit = %d\n%s", code, errs)
	}
	want := "beta (1.0_1) [repo-de.voidlinux.org/current] []\nBeta package\n"
	if out != want {
		t.Errorf("list --summary = %q, want %q", out, want)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "--summary", "--description", "alpha")
	if code != 0 {
		t.Fatalf("list --description exit = %d\n%s", code, errs)
	}
	want = "alpha (2.0_1) [repo-de.voidlinux.org/current] [Upgradable from: 1.0_1]\nThe long alpha description\n"
	if out != want {
		t.Errorf("list --description = %q, want %q", out, want)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "alpha", "beta")
	if code != 0 {
		t.Fatalf("list OR globs exit = %d\n%s", code, errs)
	}
	if !strings.Contains(out, "alpha (2.0_1)") || !strings.Contains(out, "beta (1.0_1)") {
		t.Errorf("OR-ed globs missing rows:\n%s", out)
	}
	if strings.Contains(errs, "Notice:") {
		t.Errorf("matched OR globs wrote a notice:\n%s", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "alpha", "zzz")
	if code != 0 {
		t.Fatalf("partial-match exit = %d\n%s", code, errs)
	}
	if !strings.Contains(out, "alpha (2.0_1)") || errs != "Notice: 'zzz' was not found\n" {
		t.Errorf("partial match stdout=%q stderr=%q", out, errs)
	}
}

func TestAC019FailedUpgradePlanForwardsStderrAndKeepsExitZero(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	env["XB_STUB_PLAN_EXIT"] = "2"
	env["XB_STUB_STDERR"] = "ERROR: failed to open repodata\n"
	code, out, errs := runDiscovery(t, bin, env, "list", "alpha")
	if code != 0 {
		t.Fatalf("list with failed plan exit = %d, want 0", code)
	}
	if strings.Contains(out, "Upgradable from:") {
		t.Errorf("failed plan still printed an upgrade:\n%s", out)
	}
	if !strings.Contains(out, "alpha (1.0_1) [local] [Installed]\n") {
		t.Errorf("installed row missing after failed plan:\n%s", out)
	}
	if errs != "ERROR: failed to open repodata\n" {
		t.Errorf("stderr = %q", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "-u")
	if code != 0 || out != "" {
		t.Errorf("list -u with failed plan = %d, %q; want empty success", code, out)
	}
	if errs != "ERROR: failed to open repodata\n" {
		t.Errorf("list -u stderr = %q", errs)
	}
}

func TestAC018ShowUsesOnlyOfflineRecordAndAttributePasses(t *testing.T) {
	bin, _, logPath, env := discoveryEnv(t)
	code, _, errs := runDiscovery(t, bin, env, "show", "alpha")
	if code != 0 {
		t.Fatalf("show exit = %d\n%s", code, errs)
	}
	cmds := commandsFromLog(t, logPath)
	want := []string{
		"-O",
		"-R -s ",
		"-R -s  -p repository",
		"-S alpha",
	}
	for _, wantCall := range want {
		found := false
		for _, got := range cmds {
			if got == wantCall {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing show call %q in:\n%s", wantCall, strings.Join(cmds, "\n"))
		}
	}
	for _, got := range cmds {
		if strings.Contains(got, "-M") || strings.Contains(got, "-R -o") || got == "-S" {
			t.Errorf("forbidden show call %q", got)
		}
		if strings.HasPrefix(got, "-n") || strings.HasPrefix(got, "-S ") {
			// `-S alpha` is the one-record query; -n is a transaction plan.
			if got != "-S alpha" {
				t.Errorf("show ran a transaction pass %q", got)
			}
		}
	}
}

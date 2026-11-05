package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// policyReposVerbose is the `xbps-query -L -v` shape measured on xbps
// 0.60.7: the first field is each repository's package count, not its
// precedence, and the signed entries add two indented lines.
const policyReposVerbose = `14832 https://repo-de.voidlinux.org/current (RSA signed)
      Signed-by: Void Linux
      4096 60:ae:0c:d6:f0:95:17:80:bc:93:46:7a:89:af:a3:2d
   21 https://repo-de.voidlinux.org/current/multilib (RSA signed)
      Signed-by: Void Linux
      4096 60:ae:0c:d6:f0:95:17:80:bc:93:46:7a:89:af:a3:2d
    1 /media/Toto/softs/linux/grok-bot/repo (RSA unsigned)
`

const policyReposPlain = `14832 https://repo-de.voidlinux.org/current (RSA signed)
   21 https://repo-de.voidlinux.org/current/multilib (RSA signed)
    1 /media/Toto/softs/linux/grok-bot/repo (RSA unsigned)
`

// policyEnv is the shared discovery environment with the repository, hold
// and repolock fixtures policy reads.
func policyEnv(t *testing.T) (bin string, env map[string]string, logPath string) {
	t.Helper()
	bin, _, logPath, env = discoveryEnv(t)
	env["XB_STUB_REPOS"] = policyReposPlain
	env["XB_STUB_REPOS_VERBOSE"] = policyReposVerbose
	env["XB_STUB_HOLD"] = "heldpkg-1.0_1\n"
	env["XB_STUB_REPOLOCK"] = "repopkg-2.0_1\n"
	return bin, env, logPath
}

func TestAC021PolicyGlobalViewRepositoryPrecedenceSignaturesAndPins(t *testing.T) {
	bin, env, _ := policyEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\nstderr:\n%s", code, errs)
	}
	want := "" +
		"Package files:\n" +
		" 1 https://repo-de.voidlinux.org/current (RSA signed)\n" +
		"     Signed-by: Void Linux\n" +
		"     4096 60:ae:0c:d6:f0:95:17:80:bc:93:46:7a:89:af:a3:2d\n" +
		" 2 https://repo-de.voidlinux.org/current/multilib (RSA signed)\n" +
		"     Signed-by: Void Linux\n" +
		"     4096 60:ae:0c:d6:f0:95:17:80:bc:93:46:7a:89:af:a3:2d\n" +
		" 3 /media/Toto/softs/linux/grok-bot/repo (RSA unsigned)\n" +
		"Pinned packages:\n" +
		"   heldpkg -> 1.0_1 (hold)\n" +
		"   repopkg -> 2.0_1 (repolock)\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want empty", errs)
	}
}

func TestAC021PolicyGlobalViewPinnedHeadingAlwaysPrints(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_HOLD"] = ""
	env["XB_STUB_REPOLOCK"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\n%s", code, errs)
	}
	if !strings.HasSuffix(out, "Pinned packages:\n") {
		t.Errorf("empty pinned heading missing:\n%s", out)
	}
	if strings.Contains(out, " -> ") {
		t.Errorf("empty pin sets printed a pin:\n%s", out)
	}
}

func TestAC022PolicyGlobalMachineObject(t *testing.T) {
	bin, env, _ := policyEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m", "--color=always")
	if code != 0 {
		t.Fatalf("policy -m exit = %d\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("machine policy carried ANSI:\n%q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("machine global policy = %d lines, want 1:\n%s", len(lines), out)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &obj); err != nil {
		t.Fatalf("machine global policy is not JSON: %v\n%s", err, out)
	}
	if obj["schema"] != float64(1) {
		t.Errorf("schema = %v, want 1", obj["schema"])
	}
	files, ok := obj["package_files"].([]any)
	if !ok || len(files) != 3 {
		t.Fatalf("package_files = %#v", obj["package_files"])
	}
	first := files[0].(map[string]any)
	if first["precedence"] != float64(1) ||
		first["repository"] != "https://repo-de.voidlinux.org/current" ||
		first["signed"] != true ||
		first["signed_by"] != "Void Linux" ||
		first["fingerprint"] != "4096 60:ae:0c:d6:f0:95:17:80:bc:93:46:7a:89:af:a3:2d" {
		t.Errorf("first package file = %#v", first)
	}
	unsigned := files[2].(map[string]any)
	if unsigned["signed"] != false {
		t.Errorf("unsigned repo signed = %v", unsigned["signed"])
	}
	if _, present := unsigned["signed_by"]; present {
		t.Errorf("unsigned repo carries signed_by: %#v", unsigned)
	}
	if _, present := unsigned["fingerprint"]; present {
		t.Errorf("unsigned repo carries fingerprint: %#v", unsigned)
	}
	pins, ok := obj["pinned_packages"].([]any)
	if !ok || len(pins) != 2 {
		t.Fatalf("pinned_packages = %#v", obj["pinned_packages"])
	}
	pin := pins[0].(map[string]any)
	if pin["name"] != "heldpkg" || pin["version"] != "1.0_1" || pin["reason"] != "hold" {
		t.Errorf("first pin = %#v", pin)
	}
}

func TestAC021PolicyPackageViewInstalledCandidateAndVersionTable(t *testing.T) {
	bin, env, _ := policyEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "policy", "alpha")
	if code != 0 {
		t.Fatalf("policy alpha exit = %d\nstderr:\n%s", code, errs)
	}
	want := "" +
		"alpha:\n" +
		"  Installed: 1.0_1\n" +
		"  Candidate: 2.0_1\n" +
		"  Version table:\n" +
		"  *** 2.0_1 1\n" +
		"      1 https://repo-de.voidlinux.org/current\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want empty", errs)
	}
}

func TestAC021PolicyPackageViewMissingNameIsNoticeAndNoFallback(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy", "ghost")
	if code != 0 {
		t.Fatalf("policy ghost exit = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("only-missing policy fell back to the global view:\n%s", out)
	}
	if errs != "Notice: 'ghost' was not found\n" {
		t.Errorf("stderr = %q", errs)
	}
}

func TestAC021PolicyPackageViewBlankLineBetweenPackages(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_SHOW"] = discShowInstalled + "\n"
	code, out, errs := runDiscovery(t, bin, env, "policy", "alpha", "beta")
	if code != 0 {
		t.Fatalf("policy alpha beta exit = %d\n%s", code, errs)
	}
	if !strings.Contains(out, "\n\nalpha:\n") && !strings.HasPrefix(out, "alpha:\n") {
		t.Errorf("package blocks are not separated by one blank line:\n%s", out)
	}
	if strings.Count(out, "\n\n") != 1 {
		t.Errorf("want exactly one blank line between two packages:\n%s", out)
	}
}

func TestAC022PolicyPackageMachineArray(t *testing.T) {
	bin, env, _ := policyEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m", "--color=always", "alpha")
	if code != 0 {
		t.Fatalf("policy -m alpha exit = %d\nstderr:\n%s", code, errs)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("machine package policy carried ANSI:\n%q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("machine package policy = %d lines, want one JSON array:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "[") || !strings.HasSuffix(lines[0], "]") {
		t.Fatalf("machine package policy is not an array: %q", lines[0])
	}
	var views []map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &views); err != nil {
		t.Fatalf("machine package policy is not JSON: %v\n%s", err, lines[0])
	}
	if len(views) != 1 {
		t.Fatalf("package array = %d entries, want 1", len(views))
	}
	view := views[0]
	if view["schema"] != float64(1) || view["package"] != "alpha" {
		t.Errorf("package identity = %#v", view)
	}
	if view["installed"] != "1.0_1" || view["candidate"] != "2.0_1" {
		t.Errorf("installed/candidate = %v/%v", view["installed"], view["candidate"])
	}
	versions, ok := view["versions"].([]any)
	if !ok || len(versions) != 1 {
		t.Fatalf("versions = %#v", view["versions"])
	}
	version := versions[0].(map[string]any)
	if version["version"] != "2.0_1" || version["precedence"] != float64(1) ||
		version["installed"] != false || version["candidate"] != true {
		t.Errorf("version = %#v", version)
	}
	repos, ok := version["repositories"].([]any)
	if !ok || len(repos) != 1 {
		t.Fatalf("repositories = %#v", version["repositories"])
	}
	repo := repos[0].(map[string]any)
	if repo["precedence"] != float64(1) || repo["repository"] != "https://repo-de.voidlinux.org/current" {
		t.Errorf("repository = %#v", repo)
	}
}

func TestAC022PolicyMachineNamesMissingIsEmptyArrayAndNotice(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m", "ghost")
	if code != 0 {
		t.Fatalf("policy -m ghost exit = %d", code)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("missing-name machine array = %q, want []", out)
	}
	if errs != "Notice: 'ghost' was not found\n" {
		t.Errorf("stderr = %q", errs)
	}
}

// policyAllowedCalls is FR-022's offline pass set as it applies to policy.
var policyAllowedCalls = map[string]bool{
	"-L -v":                  true,
	"-H":                     true,
	"--list-repolock-pkgs":   true,
	"-R -s ":                 true,
	"-R -s  -p architecture": true,
	"-S alpha":               true,
	"-R -S alpha":            true,
}

func TestAC018PolicyUsesOnlyFixedOfflinePasses(t *testing.T) {
	bin, env, logPath := policyEnv(t)

	code, _, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\n%s", code, errs)
	}
	for _, cmd := range commandsFromLog(t, logPath) {
		if !policyAllowedCalls[cmd] {
			t.Errorf("global policy ran a call outside the fixed set: %q", cmd)
		}
	}

	os.Remove(logPath)
	code, _, errs = runDiscovery(t, bin, env, "policy", "alpha")
	if code != 0 {
		t.Fatalf("policy alpha exit = %d\n%s", code, errs)
	}
	cmds := commandsFromLog(t, logPath)
	want := []string{"-L -v", "-R -s ", "-R -s  -p architecture", "-S alpha", "-R -S alpha"}
	for _, wantCall := range want {
		found := false
		for _, got := range cmds {
			if got == wantCall {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing policy call %q in:\n%s", wantCall, strings.Join(cmds, "\n"))
		}
	}
	for _, got := range cmds {
		if !policyAllowedCalls[got] {
			t.Errorf("package policy ran a call outside the fixed set: %q", got)
		}
		for _, banned := range []string{"-M", "-R -o", "-n -u"} {
			if strings.Contains(got, banned) {
				t.Errorf("forbidden policy call %q (contains %q)", got, banned)
			}
		}
	}
}

func TestAC018PolicyVerboseShowsPassesAndLocaleAndRunsRootFree(t *testing.T) {
	bin, env, _ := policyEnv(t)
	envLog := t.TempDir() + "/env.log"
	env["XB_STUB_ENV_LOG"] = envLog
	code, _, errs := runDiscovery(t, bin, env, "policy", "-v")
	if code != 0 {
		t.Fatalf("policy -v exit = %d\n%s", code, errs)
	}
	for _, want := range []string{"-L -v", "-H", "--list-repolock-pkgs"} {
		if !strings.Contains(errs, "verbose: ") || !strings.Contains(errs, want) {
			t.Errorf("verbose output missing %q:\n%s", want, errs)
		}
	}
	for _, word := range []string{"root", "sudo", "doas"} {
		if strings.Contains(strings.ToLower(errs), word) {
			t.Errorf("root-free policy printed %q:\n%s", word, errs)
		}
	}
	raw, err := os.ReadFile(envLog)
	if err != nil {
		t.Fatalf("reading env log: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line != "LC_ALL=C" {
			t.Errorf("child environment = %q, want LC_ALL=C", line)
		}
	}
}

func TestAC021PolicyInstalledOnlyPackageHasNoCandidate(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_SHOW"] = "pkgname: local-only\npkgver: local-only-1.0_1\n"
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy", "local-only")
	if code != 0 {
		t.Fatalf("policy local-only exit = %d\n%s", code, errs)
	}
	want := "" +
		"local-only:\n" +
		"  Installed: 1.0_1\n" +
		"  Candidate: none\n" +
		"  Version table:\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
	if errs != "" {
		t.Errorf("installed-only package was treated as missing: %q", errs)
	}
}

func TestAC021PolicyRepositoryOnlyPackageHasInstalledNone(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = "pkgname: beta\npkgver: beta-1.0_1\nrepository: https://repo-de.voidlinux.org/current\n"
	code, out, errs := runDiscovery(t, bin, env, "policy", "beta")
	if code != 0 {
		t.Fatalf("policy beta exit = %d\n%s", code, errs)
	}
	want := "" +
		"beta:\n" +
		"  Installed: none\n" +
		"  Candidate: 1.0_1\n" +
		"  Version table:\n" +
		"  *** 1.0_1 1\n" +
		"      1 https://repo-de.voidlinux.org/current\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
}

func TestAC021PolicySameVersionTwoRepositoriesAndPrecedenceOrder(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_REPO"] = "[-] dual-1.0_1 Dual package\n"
	// The architecture pass lists repo-b first; -L precedence says repo-a is
	// first, and policy must follow -L.
	env["XB_STUB_ARCH"] = "" +
		"dual-1.0_1: x86_64 (https://repo-b.example/current)\n" +
		"dual-1.0_1: x86_64 (https://repo-a.example/current)\n"
	env["XB_STUB_REPOS_VERBOSE"] = "" +
		"  10 https://repo-a.example/current (RSA signed)\n" +
		"      Signed-by: A\n" +
		"      4096 aa:bb\n" +
		"   5 https://repo-b.example/current (RSA unsigned)\n"
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = "pkgname: dual\npkgver: dual-1.0_1\nrepository: https://repo-b.example/current\n"

	code, out, errs := runDiscovery(t, bin, env, "policy", "dual")
	if code != 0 {
		t.Fatalf("policy dual exit = %d\n%s", code, errs)
	}
	want := "" +
		"dual:\n" +
		"  Installed: none\n" +
		"  Candidate: 1.0_1\n" +
		"  Version table:\n" +
		"      1.0_1 1\n" +
		"      1 https://repo-a.example/current\n" +
		"  *** 1.0_1 2\n" +
		"      2 https://repo-b.example/current\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
}

func TestAC022PolicyPackageMachineKeepsEachVariant(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_REPO"] = "[-] dual-1.0_1 Dual package\n"
	env["XB_STUB_ARCH"] = "" +
		"dual-1.0_1: x86_64 (https://repo-b.example/current)\n" +
		"dual-1.0_1: x86_64 (https://repo-a.example/current)\n"
	env["XB_STUB_REPOS_VERBOSE"] = "" +
		"  10 https://repo-a.example/current (RSA signed)\n" +
		"   5 https://repo-b.example/current (RSA unsigned)\n"
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = "pkgname: dual\npkgver: dual-1.0_1\nrepository: https://repo-b.example/current\n"
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m", "dual")
	if code != 0 {
		t.Fatalf("policy -m dual exit = %d\n%s", code, errs)
	}
	var views []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &views); err != nil {
		t.Fatalf("machine policy is not a JSON array: %v\n%s", err, out)
	}
	versions := views[0]["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want one per variant:\n%s", len(versions), out)
	}
	first := versions[0].(map[string]any)
	second := versions[1].(map[string]any)
	if first["precedence"] != float64(1) || first["candidate"] != false {
		t.Errorf("first variant = %#v", first)
	}
	if second["precedence"] != float64(2) || second["candidate"] != true {
		t.Errorf("second variant = %#v", second)
	}
}

func TestAC021PolicyPinWithBothBitsPrintsBothReasons(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_HOLD"] = "both-1.0_1\n"
	env["XB_STUB_REPOLOCK"] = "both-1.0_1\n"
	code, out, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\n%s", code, errs)
	}
	wantPins := "Pinned packages:\n   both -> 1.0_1 (hold)\n   both -> 1.0_1 (repolock)\n"
	if !strings.HasSuffix(out, wantPins) {
		t.Errorf("pins =\n%s\nwant suffix:\n%s", out, wantPins)
	}
}

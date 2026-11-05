package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/discovery"
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
	if signedBy, present := unsigned["signed_by"]; !present || signedBy != nil {
		t.Errorf("unsigned repo signed_by = %#v, want present and null", unsigned["signed_by"])
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

// policyReposEdge covers the measured `-L -v` shapes the parser must keep:
// a negative package count, a URI containing a space, a staged repository,
// maybe-signed, and two signed repositories with different signers.
const policyReposEdge = `-1 /media/My Repo/current (RSA maybe-signed)
  42 /media/Toto/softs/linux/grok-bot/repo (Staged RSA signed)
      Signed-by: Staged Signer
      4096 aa:bb:cc
   7 https://repo-a.example/current (RSA signed)
      Signed-by: Signer A
      4096 11:22:33
   9 https://repo-b.example/current (RSA signed)
      Signed-by: Signer B
      4096 44:55:66
   3 https://repo-c.example/current (RSA unsigned)
`

func TestAC021PolicyRepositoriesKeepOrdinalAndVariantShapes(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_REPOS_VERBOSE"] = policyReposEdge
	env["XB_STUB_HOLD"] = ""
	env["XB_STUB_REPOLOCK"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\n%s", code, errs)
	}
	want := "" +
		"Package files:\n" +
		" 1 /media/My Repo/current (RSA maybe-signed)\n" +
		" 2 /media/Toto/softs/linux/grok-bot/repo (RSA signed)\n" +
		"     Signed-by: Staged Signer\n" +
		"     4096 aa:bb:cc\n" +
		" 3 https://repo-a.example/current (RSA signed)\n" +
		"     Signed-by: Signer A\n" +
		"     4096 11:22:33\n" +
		" 4 https://repo-b.example/current (RSA signed)\n" +
		"     Signed-by: Signer B\n" +
		"     4096 44:55:66\n" +
		" 5 https://repo-c.example/current (RSA unsigned)\n" +
		"Pinned packages:\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
}

func TestAC022PolicyMachineRepositorySignatureShapes(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_REPOS_VERBOSE"] = policyReposEdge
	env["XB_STUB_HOLD"] = ""
	env["XB_STUB_REPOLOCK"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m")
	if code != 0 {
		t.Fatalf("policy -m exit = %d\n%s", code, errs)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &object); err != nil {
		t.Fatalf("machine policy is not JSON: %v\n%s", err, out)
	}
	files := object["package_files"].([]any)
	if len(files) != 5 {
		t.Fatalf("package_files = %d, want all five rows", len(files))
	}
	byRepo := map[string]map[string]any{}
	for _, entry := range files {
		file := entry.(map[string]any)
		byRepo[file["repository"].(string)] = file
	}
	maybe := byRepo["/media/My Repo/current"]
	if maybe["precedence"] != float64(1) || maybe["signed"] != false || maybe["signed_by"] != nil {
		t.Errorf("maybe-signed repo = %#v", maybe)
	}
	staged := byRepo["/media/Toto/softs/linux/grok-bot/repo"]
	if staged["signed"] != true || staged["signed_by"] != "Staged Signer" || staged["fingerprint"] != "4096 aa:bb:cc" {
		t.Errorf("staged signed repo = %#v", staged)
	}
	if byRepo["https://repo-a.example/current"]["signed_by"] != "Signer A" {
		t.Errorf("repo-a signer = %v", byRepo["https://repo-a.example/current"]["signed_by"])
	}
	if byRepo["https://repo-b.example/current"]["signed_by"] != "Signer B" {
		t.Errorf("repo-b signer = %v", byRepo["https://repo-b.example/current"]["signed_by"])
	}
	unsigned := byRepo["https://repo-c.example/current"]
	if unsigned["signed"] != false || unsigned["signed_by"] != nil {
		t.Errorf("unsigned repo = %#v", unsigned)
	}
	if _, present := unsigned["fingerprint"]; present {
		t.Errorf("unsigned repo carries fingerprint: %#v", unsigned)
	}
}

func TestAC021PolicyCanonicalisesResolvedRecordName(t *testing.T) {
	for _, token := range []string{"a*", "zstd>=1.0", "zstd-1.5.7_1", "cmd:sh"} {
		t.Run(token, func(t *testing.T) {
			bin, env, _ := policyEnv(t)
			env["XB_STUB_SHOW"] = "pkgname: alpha\npkgver: alpha-1.0_1\n"
			env["XB_STUB_SHOW_REPO"] = "pkgname: alpha\npkgver: alpha-2.0_1\nrepository: https://repo-de.voidlinux.org/current\n"
			code, out, errs := runDiscovery(t, bin, env, "policy", token)
			if code != 0 {
				t.Fatalf("policy %q exit = %d\n%s", token, code, errs)
			}
			if !strings.HasPrefix(out, "alpha:\n") {
				t.Errorf("policy %q heading is not the resolved name:\n%s", token, out)
			}
			if !strings.Contains(out, "  *** 2.0_1 1\n") || !strings.Contains(out, "      1 https://repo-de.voidlinux.org/current\n") {
				t.Errorf("policy %q did not look variants up under alpha:\n%s", token, out)
			}
			if errs != "" {
				t.Errorf("policy %q stderr = %q", token, errs)
			}
		})
	}
}

func TestAC018PolicyPlainRepositoryListFallback(t *testing.T) {
	bin, env, logPath := policyEnv(t)
	env["XB_STUB_REPOS_VERBOSE"] = ""
	env["XB_STUB_REPOS"] = policyReposPlain
	env["XB_STUB_HOLD"] = ""
	env["XB_STUB_REPOLOCK"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\n%s", code, errs)
	}
	want := "" +
		"Package files:\n" +
		" 1 https://repo-de.voidlinux.org/current (RSA signed)\n" +
		" 2 https://repo-de.voidlinux.org/current/multilib (RSA signed)\n" +
		" 3 /media/Toto/softs/linux/grok-bot/repo (RSA unsigned)\n" +
		"Pinned packages:\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", out, want)
	}
	cmds := commandsFromLog(t, logPath)
	for _, wantCall := range []string{"-L -v", "-L"} {
		found := false
		for _, got := range cmds {
			if got == wantCall {
				found = true
			}
		}
		if !found {
			t.Errorf("fallback did not run %q:\n%s", wantCall, strings.Join(cmds, "\n"))
		}
	}
}

func TestAC021PolicyPinsSortByNameInHumanAndMachine(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_HOLD"] = "zpkg-1.0_1\nheldpkg-1.0_1\n"
	env["XB_STUB_REPOLOCK"] = "apkg-2.0_1\n"

	code, out, errs := runDiscovery(t, bin, env, "policy")
	if code != 0 {
		t.Fatalf("policy exit = %d\n%s", code, errs)
	}
	wantPins := "" +
		"Pinned packages:\n" +
		"   apkg -> 2.0_1 (repolock)\n" +
		"   heldpkg -> 1.0_1 (hold)\n" +
		"   zpkg -> 1.0_1 (hold)\n"
	if !strings.HasSuffix(out, wantPins) {
		t.Errorf("human pins =\n%s\nwant suffix:\n%s", out, wantPins)
	}

	code, out, errs = runDiscovery(t, bin, env, "policy", "-m")
	if code != 0 {
		t.Fatalf("policy -m exit = %d\n%s", code, errs)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &object); err != nil {
		t.Fatalf("machine policy is not JSON: %v", err)
	}
	pins := object["pinned_packages"].([]any)
	got := make([]string, 0, len(pins))
	for _, entry := range pins {
		pin := entry.(map[string]any)
		got = append(got, pin["name"].(string)+"/"+pin["reason"].(string))
	}
	want := []string{"apkg/repolock", "heldpkg/hold", "zpkg/hold"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("machine pin order = %v, want %v", got, want)
	}
}

func TestAC022PolicyMachineNullAndEmptyShapes(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_REPOS_VERBOSE"] = ""
	env["XB_STUB_REPOS"] = ""
	env["XB_STUB_HOLD"] = ""
	env["XB_STUB_REPOLOCK"] = ""
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m")
	if code != 0 {
		t.Fatalf("empty global policy exit = %d\n%s", code, errs)
	}
	if got := strings.TrimSpace(out); got != `{"schema":1,"package_files":[],"pinned_packages":[]}` {
		t.Errorf("empty global machine = %q", got)
	}

	env = policyEnvOnly(t)
	env["XB_STUB_SHOW"] = "pkgname: local-only\npkgver: local-only-1.0_1\n"
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs = runDiscovery(t, bin, env, "policy", "-m", "local-only")
	if code != 0 {
		t.Fatalf("installed-only machine policy exit = %d\n%s", code, errs)
	}
	view := firstMachineView(t, out)
	if view["installed"] != "1.0_1" || view["candidate"] != nil {
		t.Errorf("installed-only machine view = %#v", view)
	}
	if versions, ok := view["versions"].([]any); !ok || len(versions) != 0 {
		t.Errorf("installed-only versions = %#v, want []", view["versions"])
	}

	bin, env, _ = policyEnv(t)
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = "pkgname: beta\npkgver: beta-1.0_1\nrepository: https://repo-de.voidlinux.org/current\n"
	code, out, errs = runDiscovery(t, bin, env, "policy", "-m", "beta")
	if code != 0 {
		t.Fatalf("repo-only machine policy exit = %d\n%s", code, errs)
	}
	view = firstMachineView(t, out)
	if view["installed"] != nil || view["candidate"] != "1.0_1" {
		t.Errorf("repo-only machine view = %#v", view)
	}
}

func TestAC022PolicyInstalledMarksOneVariant(t *testing.T) {
	bin, env, _ := policyEnv(t)
	env["XB_STUB_REPO"] = "[-] dual-1.0_1 Dual package\n"
	env["XB_STUB_ARCH"] = "" +
		"dual-1.0_1: x86_64 (https://repo-b.example/current)\n" +
		"dual-1.0_1: x86_64 (https://repo-a.example/current)\n"
	env["XB_STUB_REPOS_VERBOSE"] = "" +
		"  10 https://repo-a.example/current (RSA signed)\n" +
		"   5 https://repo-b.example/current (RSA unsigned)\n"
	env["XB_STUB_SHOW"] = "pkgname: dual\npkgver: dual-1.0_1\n"
	env["XB_STUB_SHOW_REPO"] = "pkgname: dual\npkgver: dual-1.0_1\nrepository: https://repo-b.example/current\n"
	code, out, errs := runDiscovery(t, bin, env, "policy", "-m", "dual")
	if code != 0 {
		t.Fatalf("policy -m dual exit = %d\n%s", code, errs)
	}
	view := firstMachineView(t, out)
	versions := view["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(versions))
	}
	first := versions[0].(map[string]any)
	second := versions[1].(map[string]any)
	if first["installed"] != true || second["installed"] != false {
		t.Errorf("installed flags = %v/%v; want only the first-precedence variant",
			first["installed"], second["installed"])
	}
	if first["candidate"] != false || second["candidate"] != true {
		t.Errorf("candidate flags = %v/%v", first["candidate"], second["candidate"])
	}
}

func TestAC021PolicyHumanWriteFailureIsCleanExit(t *testing.T) {
	if writePolicyGlobal(failWriter{}, []discovery.PolicyFile{{Precedence: 1, Repository: "https://repo.example/current", Signature: "signed"}}, nil) {
		t.Errorf("global write to a broken stream reported success")
	}
	if writePolicyPackages(failWriter{}, []discovery.MachinePackagePolicy{{Package: "alpha"}}) {
		t.Errorf("package write to a broken stream reported success")
	}

	// A broken stdout must not turn the global view into a runtime error.
	st := &state{
		stdout: failWriter{},
		stderr: &bytes.Buffer{},
		r: &runner{cfg: config.Config{Xbps: config.XbpsConfig{
			QueryPath: "true",
		}}},
	}
	if code := st.runPolicyGlobal(&options{}); code != exitOK {
		t.Errorf("global policy with broken stdout exit = %d, want 0", code)
	}
}

func firstMachineView(t *testing.T, out string) map[string]any {
	t.Helper()
	var views []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &views); err != nil {
		t.Fatalf("machine policy is not a JSON array: %v\n%s", err, out)
	}
	if len(views) != 1 {
		t.Fatalf("machine array = %d entries, want 1", len(views))
	}
	return views[0]
}

func policyEnvOnly(t *testing.T) map[string]string {
	t.Helper()
	_, env, _ := policyEnv(t)
	return env
}

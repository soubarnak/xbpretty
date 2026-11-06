package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// heldRepositoryNewer adds a repository candidate for heldpkg that is newer
// than the installed 1.0_1 copy the fixture holds.
func bf15HeldRepositoryNewer(env map[string]string) {
	env["XB_STUB_REPO"] = discRepo + "[-] heldpkg-2.0_1 Held package\n"
	env["XB_STUB_ARCH"] = discArch + "heldpkg-2.0_1: x86_64 (https://repo-de.voidlinux.org/current)\n"
}

// TestAC019HeldOlderThanRepositoryKeepsInstalledState pins BF-15: a held
// package whose repository candidate is newer keeps its installed state in
// text and machine output, and the row is the installed [local] copy.
func TestAC019HeldOlderThanRepositoryKeepsInstalledState(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	bf15HeldRepositoryNewer(env)

	code, out, errs := runDiscovery(t, bin, env, "list", "-i")
	if code != 0 {
		t.Fatalf("list -i exit = %d\n%s", code, errs)
	}
	want := "" +
		"alpha (2.0_1) [repo-de.voidlinux.org/current] [Installed, Upgradable from: 1.0_1]\n" +
		"heldpkg (1.0_1) [local] [Installed, Held]\n" +
		"local-only (1.0_1) [local] [Installed, Automatic]\n"
	if out != want {
		t.Errorf("list -i stdout =\n%s\nwant:\n%s", out, want)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "heldpkg")
	if code != 0 {
		t.Fatalf("list heldpkg exit = %d\n%s", code, errs)
	}
	if out != "heldpkg (1.0_1) [local] [Installed, Held]\n" {
		t.Errorf("list heldpkg stdout = %q", out)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "-m", "heldpkg")
	if code != 0 {
		t.Fatalf("list -m heldpkg exit = %d\n%s", code, errs)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &obj); err != nil {
		t.Fatalf("list -m heldpkg is not JSON: %v\n%s", err, out)
	}
	if obj["installed"] != true || obj["held"] != true || obj["installed_version"] != "1.0_1" {
		t.Errorf("held machine row installed/held/installed_version = %v/%v/%v, want true/true/1.0_1",
			obj["installed"], obj["held"], obj["installed_version"])
	}
	if obj["pkgver"] != "heldpkg-1.0_1" {
		t.Errorf("held machine pkgver = %v, want the installed heldpkg-1.0_1", obj["pkgver"])
	}
	if _, ok := obj["repository"]; ok {
		t.Errorf("held local row carries repository: %v", obj)
	}
}

// TestBF15LocallyNewerInstalledKeepsInstalledState covers the other half of
// BF-15: an installed copy newer than the repository candidate is shown as
// installed, not as the older repository row.
func TestBF15LocallyNewerInstalledKeepsInstalledState(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	env["XB_STUB_LIST"] = discInstalled + "ii beta-3.0_1 Locally built beta\n"
	env["XB_STUB_MANUAL"] = discManual + "beta-3.0_1\n"

	code, out, errs := runDiscovery(t, bin, env, "list", "beta")
	if code != 0 {
		t.Fatalf("list beta exit = %d\n%s", code, errs)
	}
	if out != "beta (3.0_1) [local] [Installed]\n" {
		t.Errorf("locally newer beta stdout = %q, want the installed local row", out)
	}
}

package cli

import (
	"strings"
	"testing"
)

// bf42ShowNotInstalled makes every installed -S answer xbps's silent "not found",
// so only the repository record query can answer a show token.
func bf42ShowNotInstalled(env map[string]string) {
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
}

// TestAC020ShowResolvesPkgverAndVirtualTokens pins BF-42: a pkgver, a pattern
// or a virtual name is answered by the repository record query and resolves
// to the package it names, instead of being reported as "not found".
func TestAC020ShowResolvesPkgverAndVirtualTokens(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	bf42ShowNotInstalled(env)

	wantAlpha := "" +
		"Package: alpha\n" +
		"Version: 2.0_1\n" +
		"Architecture: x86_64\n" +
		"Installed-Size: 12KB\n" +
		"Size: 3KB\n" +
		"Repository: https://repo-de.voidlinux.org/current\n" +
		"Description: A first package\n" +
		"Attributes: []\n"
	for _, token := range []string{"alpha-2.0_1", "alpha>=1.0"} {
		code, out, errs := runDiscovery(t, bin, env, "show", token)
		if code != 0 {
			t.Fatalf("show %s exit = %d\n%s", token, code, errs)
		}
		if out != wantAlpha {
			t.Errorf("show %s stdout =\n%s\nwant:\n%s", token, out, wantAlpha)
		}
		if errs != "" {
			t.Errorf("show %s stderr = %q, want empty", token, errs)
		}
	}

	// A virtual name resolves on the repository side to its provider.
	env["XB_STUB_SHOW_REPO"] = "pkgname: gawk\npkgver: gawk-5.3.0_1\narchitecture: x86_64\n" +
		"repository: https://repo-de.voidlinux.org/current\nshort_desc: AWK implementation\n"
	wantGawk := "" +
		"Package: gawk\n" +
		"Version: 5.3.0_1\n" +
		"Architecture: x86_64\n" +
		"Repository: https://repo-de.voidlinux.org/current\n" +
		"Description: AWK implementation\n" +
		"Attributes: []\n"
	code, out, errs := runDiscovery(t, bin, env, "show", "awk")
	if code != 0 {
		t.Fatalf("show awk exit = %d\n%s", code, errs)
	}
	if out != wantGawk {
		t.Errorf("show awk stdout =\n%s\nwant:\n%s", out, wantGawk)
	}
	if errs != "" {
		t.Errorf("show awk stderr = %q, want empty", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "show", "-a", "awk")
	if code != 0 {
		t.Fatalf("show -a awk exit = %d\n%s", code, errs)
	}
	if out != wantGawk {
		t.Errorf("show -a awk stdout =\n%s\nwant:\n%s", out, wantGawk)
	}
	if errs != "" {
		t.Errorf("show -a awk stderr = %q, want empty", errs)
	}
}

// TestAC020ShowTrulyMissingTokenStillNotice keeps the not-found path: a token
// neither query answers is still a notice with exit 0.
func TestAC020ShowTrulyMissingTokenStillNotice(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	bf42ShowNotInstalled(env)
	env["XB_STUB_SHOW_REPO_EXIT"] = "2"
	env["XB_STUB_SHOW_REPO"] = ""
	code, out, errs := runDiscovery(t, bin, env, "show", "ghost-9.9_9")
	if code != 0 || out != "" {
		t.Errorf("show ghost = %d, %q; want 0, empty", code, out)
	}
	if errs != "Notice: 'ghost-9.9_9' was not found\n" {
		t.Errorf("stderr = %q", errs)
	}
	if strings.Contains(errs, "alpha") {
		t.Errorf("missing token borrowed another package: %q", errs)
	}
}

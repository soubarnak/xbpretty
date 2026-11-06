package cli

import (
	"os"
	"strings"
	"testing"
)

// bf44GlobEnv adds virtual names with colons, a package whose name starts with
// `cmd`, and the repository and provides rows the BF-44 token cases read.
func bf44GlobEnv(env map[string]string) {
	env["XB_STUB_PROVIDES"] = discProvides +
		"virtual-provider-1.0_1: pc:zlib-ng-1.0_1 (https://repo-de.voidlinux.org/current)\n" +
		"virtual-provider-1.0_1: font:dejavu-2.0_1 (https://repo-de.voidlinux.org/current)\n"
	env["XB_STUB_REPO"] = discRepo + "[-] cmdpack-1.0_1 Command pack\n"
	env["XB_STUB_ARCH"] = discArch + "cmdpack-1.0_1: x86_64 (https://repo-de.voidlinux.org/current)\n"
}

// TestAC019GlobArchSuffixAndBracketNegation pins BF-44: a colon in a virtual
// name is part of the name unless the suffix is a real architecture, and
// `[!x]` negates.
func TestAC019GlobArchSuffixAndBracketNegation(t *testing.T) {
	bin, _, _, env := discoveryEnv(t)
	bf44GlobEnv(env)

	code, out, errs := runDiscovery(t, bin, env, "list", "-V", "pc:zlib-ng")
	if code != 0 || out != "pc:zlib-ng (Virtual) [virtual-provider]\n" {
		t.Errorf("list -V pc:zlib-ng = %d, %q\n%s", code, out, errs)
	}
	if errs != "" {
		t.Errorf("list -V pc:zlib-ng stderr = %q, want empty", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "-V", "font:*")
	if code != 0 || out != "font:dejavu (Virtual) [virtual-provider]\n" {
		t.Errorf("list -V font:* = %d, %q\n%s", code, out, errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "cmdpack:x86_64")
	if code != 0 || !strings.HasPrefix(out, "cmdpack (1.0_1)") {
		t.Errorf("list cmdpack:x86_64 = %d, %q\n%s", code, out, errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "list", "[!a]*")
	if code != 0 {
		t.Fatalf("list [!a]* exit = %d\n%s", code, errs)
	}
	if strings.Contains(out, "alpha (") {
		t.Errorf("list [!a]* matched alpha, which starts with a:\n%s", out)
	}
	if !strings.Contains(out, "beta (1.0_1)") {
		t.Errorf("list [!a]* missed beta:\n%s", out)
	}
}

// TestBF44UnbalancedBraceIsUsageErrorBeforeAnyChild pins the brace rule: an
// unclosed `{` is a usage error with exit 2, and no xbps query runs for it.
func TestBF44UnbalancedBraceIsUsageErrorBeforeAnyChild(t *testing.T) {
	bin, _, logPath, env := discoveryEnv(t)
	code, out, errs := runDiscovery(t, bin, env, "list", "{alpha,beta")
	if code != 2 {
		t.Errorf("list {alpha,beta exit = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(errs, "xbpretty: invalid pattern '{alpha,beta': ") {
		t.Errorf("stderr = %q", errs)
	}
	if raw, err := os.ReadFile(logPath); err == nil && len(raw) > 0 {
		t.Errorf("an xbps query ran for a usage error:\n%s", raw)
	}
}

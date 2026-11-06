package cli

import (
	"strings"
	"testing"
)

// bf43MultiVariantEnv adds two repository-only names that each have a variant in
// two repositories. A token that does not match them must not trigger their
// `-R -S` resolution (BF-43).
func bf43MultiVariantEnv(env map[string]string) {
	env["XB_STUB_REPO"] = discRepo +
		"[-] dual-1.0_1 Dual package\n" +
		"[-] multi-1.0_1 Multi package\n"
	env["XB_STUB_ARCH"] = discArch +
		"dual-1.0_1: x86_64 (https://repo-a.example/current)\n" +
		"dual-1.0_1: x86_64 (https://repo-b.example/current)\n" +
		"multi-1.0_1: x86_64 (https://repo-a.example/current)\n" +
		"multi-1.0_1: x86_64 (https://repo-b.example/current)\n"
}

// bf43ResolutionCalls counts the `-R -S <name>` record queries in a command log.
func bf43ResolutionCalls(cmds []string) []string {
	var out []string
	for _, cmd := range cmds {
		if strings.HasPrefix(cmd, "-R -S ") {
			out = append(out, cmd)
		}
	}
	return out
}

// TestAC018ResolutionLimitedToMatchedNames pins BF-43: a multi-variant name
// is resolved with `-R -S` only when the token filter keeps it, so a token
// that matches nothing costs no record queries.
func TestAC018ResolutionLimitedToMatchedNames(t *testing.T) {
	bin, _, logPath, env := discoveryEnv(t)
	bf43MultiVariantEnv(env)

	code, out, errs := runDiscovery(t, bin, env, "list", "beta")
	if code != 0 {
		t.Fatalf("list beta exit = %d\n%s", code, errs)
	}
	if out != "beta (1.0_1) [repo-de.voidlinux.org/current] []\n" {
		t.Errorf("list beta stdout = %q", out)
	}
	if calls := bf43ResolutionCalls(commandsFromLog(t, logPath)); len(calls) != 0 {
		t.Errorf("list beta resolved unmatched multi-variant names: %v", calls)
	}

	// A matched multi-variant name is still resolved. Resolution matches the
	// record's pkgver, so two repositories sharing one pkgver keep the first
	// variant, as before this fix.
	code, out, errs = runDiscovery(t, bin, env, "list", "dual")
	if code != 0 {
		t.Fatalf("list dual exit = %d\n%s", code, errs)
	}
	if out != "dual (1.0_1) [repo-a.example/current] []\n" {
		t.Errorf("list dual stdout = %q", out)
	}
	if calls := bf43ResolutionCalls(commandsFromLog(t, logPath)); len(calls) != 1 || calls[0] != "-R -S dual" {
		t.Errorf("list dual resolution calls = %v, want exactly [-R -S dual]", calls)
	}
}

// TestBF43SearchResolvesOnlyMatchedVariantNames checks the same limit under
// search, where the filter reads descriptions as well as names.
func TestBF43SearchResolvesOnlyMatchedVariantNames(t *testing.T) {
	bin, _, logPath, env := discoveryEnv(t)
	bf43MultiVariantEnv(env)

	code, out, errs := runDiscovery(t, bin, env, "search", "--names", "zzzz")
	if code != 0 || out != "" {
		t.Fatalf("search --names zzzz = %d, %q\n%s", code, out, errs)
	}
	if calls := bf43ResolutionCalls(commandsFromLog(t, logPath)); len(calls) != 0 {
		t.Errorf("search resolved names that match nothing: %v", calls)
	}
}

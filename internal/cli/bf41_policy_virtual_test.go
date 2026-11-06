package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bf41NamedQueryWrapper points the test config's query_path at a stand-in for
// xbps-query. The shared stub answers every `-S <name>` with one fixture, so a
// test that needs different installed answers per name uses this instead:
// an empty answer is xbps's silent "not installed" exit 2, and any other
// answer is the record that name's `-S` prints. Every other call runs the
// shared stub unchanged, and each wrapped call is logged the way the stub
// logs its own.
func bf41NamedQueryWrapper(t *testing.T, cfgDir string, installed map[string]string) {
	t.Helper()
	stub, err := filepath.Abs("../../testdata/stub/xbps-query")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var cases strings.Builder
	for name, answer := range installed {
		if answer == "" {
			fmt.Fprintf(&cases, "'-S %s')\n  printf 'xbps-query %%s\\n' \"$*\" >> \"${XB_STUB_LOG:-/dev/null}\"\n  exit 2 ;;\n", name)
			continue
		}
		recordPath := filepath.Join(dir, name+".rec")
		if err := os.WriteFile(recordPath, []byte(answer), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&cases, "'-S %s')\n  printf 'xbps-query %%s\\n' \"$*\" >> \"${XB_STUB_LOG:-/dev/null}\"\n  cat '%s'\n  exit 0 ;;\n", name, recordPath)
	}
	script := "#!/bin/sh\n" +
		"# Test stand-in for xbps-query: per-name -S answers, all else the shared stub.\n" +
		"case \"$*\" in\n" +
		cases.String() +
		"*)\n  exec '" + stub + "' \"$@\" ;;\n" +
		"esac\n"
	wrapper := filepath.Join(dir, "xbps-query")
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(cfgDir, "xbpretty", "config.toml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "query_path = ") {
			lines[i] = "query_path = \"" + wrapper + "\""
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("test config has no query_path line to redirect")
	}
	if err := os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAC021PolicyVirtualTokenUsesCanonicalInstalledRecord pins BF-41: a
// virtual token (awk) resolves on the repository side to its provider (gawk),
// and the installed record must be read under that canonical name. An
// installed provider must not print as "Installed: none".
func TestAC021PolicyVirtualTokenUsesCanonicalInstalledRecord(t *testing.T) {
	bin, env, _ := policyEnv(t)
	bf41NamedQueryWrapper(t, env["XDG_CONFIG_HOME"], map[string]string{
		"awk":  "",
		"gawk": "pkgname: gawk\npkgver: gawk-5.3.0_1\nrepository: https://repo-de.voidlinux.org/current\n",
	})
	env["XB_STUB_LIST"] = discInstalled + "ii gawk-5.3.0_1 AWK implementation\n"
	env["XB_STUB_REPO"] = discRepo + "[-] gawk-5.3.0_1 AWK implementation\n"
	env["XB_STUB_ARCH"] = discArch + "gawk-5.3.0_1: x86_64 (https://repo-de.voidlinux.org/current)\n"
	env["XB_STUB_SHOW_EXIT"] = "2"
	env["XB_STUB_SHOW"] = ""
	env["XB_STUB_SHOW_REPO"] = "pkgname: gawk\npkgver: gawk-5.3.0_1\nrepository: https://repo-de.voidlinux.org/current\n"

	want := "" +
		"gawk:\n" +
		"  Installed: 5.3.0_1\n" +
		"  Candidate: 5.3.0_1\n" +
		"  Version table:\n" +
		"  *** 5.3.0_1 1\n" +
		"      1 https://repo-de.voidlinux.org/current\n"

	code, out, errs := runDiscovery(t, bin, env, "policy", "awk")
	if code != 0 {
		t.Fatalf("policy awk exit = %d\n%s", code, errs)
	}
	if out != want {
		t.Errorf("policy awk stdout =\n%s\nwant:\n%s", out, want)
	}
	if errs != "" {
		t.Errorf("policy awk stderr = %q, want empty", errs)
	}

	code, out, errs = runDiscovery(t, bin, env, "policy", "gawk")
	if code != 0 {
		t.Fatalf("policy gawk exit = %d\n%s", code, errs)
	}
	if out != want {
		t.Errorf("policy gawk stdout =\n%s\nwant the same view as awk:\n%s", out, want)
	}
}

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// surfaceRun runs the built binary with extra environment variables and
// returns its exit code and streams.
func surfaceRun(t *testing.T, bin string, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = nil
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err == nil {
		return 0, out.String(), errb.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String(), errb.String()
	}
	t.Fatalf("running %s %v: %v", bin, args, err)
	return 0, "", ""
}

// invalidConfigDir makes the default config layer invalid, proving a command
// bypassed config resolution instead of parsing it.
func invalidConfigDir(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "xbpretty", "config.toml"), []byte("this is not toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"XDG_CONFIG_HOME": dir}
}

func emptyConfigDir(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{"XDG_CONFIG_HOME": t.TempDir(), "XDG_STATE_HOME": t.TempDir()}
}

// xbpsStubPath puts logging stubs for every xbps binary on PATH so a run can
// prove that no child was started.
func xbpsStubPath(t *testing.T) (env map[string]string, probe string) {
	t.Helper()
	dir := t.TempDir()
	probe = filepath.Join(t.TempDir(), "probe.log")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$0\" \"$*\" >> \"$XB_PROBE\"\nexit 0\n"
	for _, name := range []string{"xbps-install", "xbps-remove", "xbps-query", "xbps-rindex", "xbps-uhelper", "xbps-pkgdb"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := dir
	if old := os.Getenv("PATH"); old != "" {
		path += string(os.PathListSeparator) + old
	}
	return map[string]string{"PATH": path, "XB_PROBE": probe}, probe
}

func TestSurfaceHelpAndVersionBypassConfig(t *testing.T) {
	bin := buildBinary(t)
	env := invalidConfigDir(t)

	code, out, errs := surfaceRun(t, bin, env, "--help")
	if code != 0 {
		t.Fatalf("--help exit = %d\nstderr:\n%s", code, errs)
	}
	for _, cmd := range []string{"install", "remove", "purge", "autoremove", "autopurge", "upgrade", "full-upgrade", "safe-upgrade", "update", "download", "clean", "list", "search", "show", "policy", "history", "undo", "redo", "fetch", "completion", "link", "log"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("--help does not list %q:\n%s", cmd, out)
		}
	}
	if strings.Contains(out, "\n  moo") {
		t.Errorf("--help advertises the hidden moo command:\n%s", out)
	}
	if !strings.Contains(out, "--config") || !strings.Contains(out, "--color") {
		t.Errorf("--help does not list the global flags:\n%s", out)
	}
	if errs != "" {
		t.Errorf("--help wrote stderr:\n%s", errs)
	}

	for _, args := range [][]string{{"-V"}, {"--version"}} {
		code, out, errs = surfaceRun(t, bin, env, args...)
		if code != 0 {
			t.Fatalf("%v exit = %d\nstderr:\n%s", args, code, errs)
		}
		if !strings.HasPrefix(out, "xbpretty dev\n") {
			t.Errorf("%v stdout = %q, want the version line first", args, out)
		}
	}

	// Before the command, -V is still the root --version.
	code, out, errs = surfaceRun(t, bin, env, "-V", "list")
	if code != 0 || !strings.HasPrefix(out, "xbpretty dev\n") {
		t.Errorf("-V list = %d, %q, %q; want the root version output", code, out, errs)
	}
	// After list/search, -V is --virtual, not --version. The command is still
	// a stub, but it must not print the version.
	code, out, _ = surfaceRun(t, bin, emptyConfigDir(t), "list", "-V")
	if strings.Contains(out, "xbpretty dev") {
		t.Errorf("list -V printed the version:\n%s", out)
	}
	if code == 0 {
		t.Errorf("list -V exited 0; the discovery stub should have run")
	}
	code, out, _ = surfaceRun(t, bin, env, "list", "--version")
	if code != 0 || !strings.HasPrefix(out, "xbpretty dev\n") {
		t.Errorf("list --version = %d, %q; want version output", code, out)
	}
	code, out, _ = surfaceRun(t, bin, emptyConfigDir(t), "search", "-V")
	if strings.Contains(out, "xbpretty dev") || code == 0 {
		t.Errorf("search -V = %d, %q; want the virtual flag, not the version", code, out)
	}
	// completion bypasses an invalid config too.
	code, out, _ = surfaceRun(t, bin, env, "completion", "bash")
	if code != 0 || !strings.Contains(out, "_xbpretty") {
		t.Errorf("completion bash with invalid config = %d, %q", code, out)
	}
}

func TestSurfaceClosedAliasDispatch(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, _ := integrationConfig(t, "Never")
	env := map[string]string{"XDG_CONFIG_HOME": cfgDir, "XDG_STATE_HOME": filepath.Join(filepath.Dir(cfgDir), "state")}
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"install"}, 1, "xbpretty: install needs at least one package"},
		{[]string{"uninstall"}, 1, "xbpretty: remove needs at least one package"},
		{[]string{"info"}, 1, "xbpretty: show needs at least one package"},
		{[]string{"dist-upgrade", "foo"}, 2, "xbpretty: full-upgrade takes no package arguments"},
		{[]string{"up", "foo"}, 2, "xbpretty: upgrade takes no package arguments"},
		{[]string{"dist-up", "foo"}, 2, "xbpretty: upgrade takes no package arguments"},
		{[]string{"history", "undo"}, 1, "xbpretty: undo is not implemented"},
		{[]string{"history", "redo"}, 1, "xbpretty: redo is not implemented"},
		{[]string{"history", "clear"}, 1, "xbpretty: history is not implemented"},
		{[]string{"clear"}, 2, "unknown command"},
		{[]string{"xbp-history"}, 2, "unknown command"},
		{[]string{"xbp-fetch"}, 2, "unknown command"},
		{[]string{"xbp-undo"}, 2, "unknown command"},
	}
	for _, tt := range tests {
		code, out, errs := surfaceRun(t, bin, env, tt.args...)
		if code != tt.code {
			t.Errorf("%v exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", tt.args, code, tt.code, out, errs)
		}
		if !strings.Contains(errs, tt.want) {
			t.Errorf("%v stderr = %q, want %q", tt.args, errs, tt.want)
		}
	}
}

func TestSurfaceGlobalFlagsBeforeAndAfterCommandAndConflicts(t *testing.T) {
	bin := buildBinary(t)
	env := emptyConfigDir(t)
	conflicts := [][]string{
		{"install", "figlet", "--color=always", "--no-color"},
		{"install", "figlet", "--tui", "--no-tui"},
		{"install", "figlet", "-y", "-n"},
		{"install", "figlet", "--update", "--no-update"},
		{"install", "figlet", "--update", "--no-sync"},
		{"install", "figlet", "--auto-remove", "--no-auto-remove"},
		{"--tui", "--no-tui", "install", "figlet"},
	}
	for _, args := range conflicts {
		if code, _, errs := surfaceRun(t, bin, env, args...); code != 2 {
			t.Errorf("%v exit = %d, want 2\n%s", args, code, errs)
		}
	}
	for _, args := range [][]string{
		{"upgrade", "--print-uris", "--dry-run"},
		{"upgrade", "--print-uris", "--download-only"},
	} {
		if code, _, errs := surfaceRun(t, bin, env, args...); code != 2 {
			t.Errorf("%v exit = %d, want 2\n%s", args, code, errs)
		}
	}

	// --no-full wins over --full without a conflict.
	env2, probe := xbpsStubPath(t)
	_ = probe
	if code, _, errs := surfaceRun(t, bin, mergeEnv(env, env2), "install", "figlet", "--dry-run", "--full", "--no-full"); code != 0 {
		t.Errorf("--full --no-full exit = %d, want 0\n%s", code, errs)
	}

	// -c is fetch's country; elsewhere it is the exact FR-059 hint.
	for _, args := range [][]string{{"-c", "IN", "install", "figlet"}, {"install", "figlet", "-c", "IN"}, {"install", "figlet", "--country", "IN"}, {"--country", "IN", "fetch"}} {
		code, _, errs := surfaceRun(t, bin, env, args...)
		if code != 2 {
			t.Errorf("%v exit = %d, want 2\n%s", args, code, errs)
		}
		if !strings.Contains(errs, "-c is fetch's --country; config is --config") {
			t.Errorf("%v stderr lacks the exact -c hint:\n%s", args, errs)
		}
	}
	for _, args := range [][]string{{"fetch", "-c"}, {"fetch", "--country"}} {
		code, _, errs := surfaceRun(t, bin, env, args...)
		if code != 2 || !strings.Contains(errs, "-c/--country needs a value") {
			t.Errorf("%v = %d, %q; want the exact missing-country usage error", args, code, errs)
		}
	}
	// On fetch, -c is accepted and the command reaches its handler instead of
	// 2. The tagged binary and an empty xbps.d keep the built-binary test off
	// the network: discovery fails before the mirror list is requested.
	code, _, errs := surfaceRun(t, taggedBinary(t), mergeEnv(env, map[string]string{
		"XBPRETTY_TEST_FETCH_XBPSD": t.TempDir(),
	}), "fetch", "-c", "IN")
	if code == 2 || strings.Contains(errs, "unknown flag") {
		t.Errorf("fetch -c IN was rejected: %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "no repositories are configured") {
		t.Errorf("fetch -c IN did not reach discovery: %d\n%s", code, errs)
	}
	// A flag on the wrong command is a usage error.
	for _, args := range [][]string{{"list", "--fix-broken"}, {"list", "-y"}, {"install", "figlet", "--limit", "2"}, {"fetch", "-n"}, {"fetch", "--simple"}, {"fetch", "--update"}} {
		if code, _, _ := surfaceRun(t, bin, env, args...); code != 2 {
			t.Errorf("%v exit = %d, want 2", args, code)
		}
	}
}

func mergeEnv(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t *testing.T) {
	bin := buildBinary(t)
	stubEnv, probe := xbpsStubPath(t)
	env := mergeEnv(invalidConfigDir(t), stubEnv)

	cases := []struct {
		name string
		args []string
		tail string
	}{
		{"allow-unauthenticated", []string{"install", "figlet", "--allow-unauthenticated"}, "--allow-unauthenticated is not available on xbps: xbps verifies repository signatures and has no unauthenticated mode."},
		{"option long", []string{"install", "figlet", "--option", "Acquire::x=y"}, "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf."},
		{"option short", []string{"install", "figlet", "-o", "Acquire::x=y"}, "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf."},
		{"fix-broken", []string{"install", "figlet", "--fix-broken"}, "-f/--fix-broken is not available on xbps: xbps has no broken-dependency repair mode. Check the pkgdb with xbps-pkgdb --checks dependencies, or force a reinstall with xbp install -- -f PKG."},
		{"fix-broken short", []string{"install", "figlet", "-f"}, "-f/--fix-broken is not available on xbps: xbps has no broken-dependency repair mode. Check the pkgdb with xbps-pkgdb --checks dependencies, or force a reinstall with xbp install -- -f PKG."},
		{"target-release", []string{"upgrade", "--target-release", "stable"}, "-t/--target-release is not available on xbps: xbps has no default-release targeting; pin a repository with a repository= line in /etc/xbps.d."},
		{"install-recommends", []string{"install", "figlet", "--install-recommends"}, "--install-recommends is not available on xbps: xbps has no recommends metadata; every declared run_depends dependency is installed."},
		{"install-suggests", []string{"install", "figlet", "--install-suggests"}, "--install-suggests is not available on xbps: xbps has no suggests metadata."},
		{"remove-essential", []string{"remove", "figlet", "--remove-essential"}, "--remove-essential is not available on xbps: xbps has no essential-package protection to override."},
		{"remove-config", []string{"remove", "figlet", "--remove-config"}, "--remove-config is not available on xbps: xbps has no config-files state; xbps-remove -f removes files, it does not create a config-only removal."},
		{"exclude", []string{"upgrade", "--exclude", "linux*"}, "--exclude is not available on xbps: xbps has no upgrade exclusion; hold the package with xbps-pkgdb -m hold PKG (release with -m unhold)."},
		{"fetch sources", []string{"fetch", "--sources"}, "--sources is not available: Void repositories contain no source packages"},
		{"fetch debian", []string{"fetch", "--debian"}, "--debian is not available: xbpretty targets Void Linux only"},
		{"fetch ubuntu", []string{"fetch", "--ubuntu"}, "--ubuntu is not available: xbpretty targets Void Linux only"},
		{"fetch devuan", []string{"fetch", "--devuan"}, "--devuan is not available: xbpretty targets Void Linux only"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errs := surfaceRun(t, bin, env, tt.args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errs)
			}
			want := "xbpretty: " + tt.tail + "\n"
			if errs != want {
				t.Errorf("stderr = %q, want exactly %q\nstdout:\n%s", errs, want, out)
			}
			if out != "" {
				t.Errorf("refusal wrote stdout:\n%s", out)
			}
			if data, err := os.ReadFile(probe); err == nil && len(data) > 0 {
				t.Errorf("an xbps child ran before the refusal:\n%s", data)
			}
		})
	}
}

func TestSurfaceNoOpNegativesAddNothingToArgv(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	code, out, errs := surfaceRun(t, bin, map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":     logPath,
		"XB_STUB_STDOUT":  planFixture,
		"NO_COLOR":        "1",
	}, "install", "figlet", "-n", "--no-fix-broken", "--no-install-recommends", "--no-install-suggests")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, errs)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading stub log: %v", err)
	}
	for _, banned := range []string{"fix-broken", "recommends", "suggests"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("%s reached an xbps argv:\n%s", banned, raw)
		}
	}
	if out == "" {
		t.Error("the dry run printed no summary")
	}
}

func TestSurfaceLinkCreatesExactlySixAndRefusesConflicts(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	// link bypasses config, so an invalid default layer must not matter.
	code, out, errs := surfaceRun(t, bin, invalidConfigDir(t), "link", dir)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("link = %d, stdout %q, stderr %q; want silent success", code, out, errs)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("link created %d names, want 6: %v", len(entries), entries)
	}
	want := map[string]bool{"xbp": true, "xbp-install": true, "xbp-remove": true, "xbp-upgrade": true, "xbp-autoremove": true, "xbp-update": true}
	for _, e := range entries {
		if !want[e.Name()] {
			t.Errorf("link created unexpected name %q", e.Name())
		}
		got, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil || got != bin {
			t.Errorf("%s -> %q, %v; want %q", e.Name(), got, err, bin)
		}
	}

	code, out, errs = surfaceRun(t, bin, invalidConfigDir(t), "link", dir)
	if code != 0 || out != "" || errs != "" {
		t.Errorf("rerun = %d, %q, %q; want silent success", code, out, errs)
	}

	// A conflicting regular file is reported, not overwritten.
	if err := os.Remove(filepath.Join(dir, "xbp-remove")); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(dir, "xbp-remove")
	if err := os.WriteFile(conflict, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs = surfaceRun(t, bin, invalidConfigDir(t), "link", dir)
	if code != 1 {
		t.Errorf("conflict exit = %d, want 1", code)
	}
	wantConflict := "xbpretty: " + conflict + " already exists and points to a regular file, not overwriting\n"
	if errs != wantConflict {
		t.Errorf("conflict stderr = %q, want %q", errs, wantConflict)
	}
	if raw, _ := os.ReadFile(conflict); string(raw) != "mine\n" {
		t.Errorf("conflict file was overwritten: %q", raw)
	}
	if err := os.Remove(conflict); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, conflict); err != nil {
		t.Fatal(err)
	}
	code, _, errs = surfaceRun(t, bin, nil, "link", dir)
	if code != 1 {
		t.Errorf("symlink conflict exit = %d, want 1", code)
	}
	wantLinkConflict := "xbpretty: " + conflict + " already exists and points to " + other + ", not overwriting\n"
	if errs != wantLinkConflict {
		t.Errorf("symlink conflict stderr = %q, want %q", errs, wantLinkConflict)
	}

	if code, _, _ := surfaceRun(t, bin, nil, "link", dir, "extra"); code != 2 {
		t.Errorf("link with two directories exit = %d, want 2", code)
	}
	if code, _, _ := surfaceRun(t, bin, invalidConfigDir(t), "completion", "bash", "extra"); code != 2 {
		t.Errorf("completion with two shells exit = %d, want 2", code)
	}

	// A missing directory is a failure, and link creates no directories.
	missing := filepath.Join(t.TempDir(), "nope")
	code, _, errs = surfaceRun(t, bin, nil, "link", missing)
	if code != 1 || !strings.Contains(errs, "cannot create "+filepath.Join(missing, "xbp")) {
		t.Errorf("missing directory = %d, %q", code, errs)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("link created the missing directory")
	}
}

func TestSurfaceMooAndNoSubcommand(t *testing.T) {
	bin := buildBinary(t)
	env := invalidConfigDir(t)

	code, out, errs := surfaceRun(t, bin, env, "moo")
	if code != 0 || !strings.Contains(out, "(oo)") || errs != "" {
		t.Errorf("moo = %d, %q, %q", code, out, errs)
	}
	code, out, errs = surfaceRun(t, bin, env, "moo", "--help")
	if code != 0 || !strings.Contains(out, "moo:") || strings.Contains(out, "Commands:") {
		t.Errorf("moo --help = %d, %q, %q; want the joke, not the command table", code, out, errs)
	}
	_, up, _ := surfaceRun(t, bin, env, "moo", "--update")
	_, noUp, _ := surfaceRun(t, bin, env, "moo", "--no-update")
	if up == noUp || up == "" {
		t.Errorf("moo --update and --no-update did not add different joke lines")
	}

	code, out, errs = surfaceRun(t, bin, env)
	if code != 1 {
		t.Errorf("no subcommand exit = %d, want 1", code)
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "Commands:") {
		t.Errorf("no subcommand did not print help on stdout:\n%s", out)
	}
	if !strings.HasPrefix(errs, "xbpretty: ") {
		t.Errorf("no subcommand stderr = %q, want a xbpretty: line", errs)
	}
}

// TestSurfaceArgv0SetIsClosed pins FR-001: only xbp and the five xbp-<verb>
// names select a command, and the other xbp-* names do not.
func TestSurfaceArgv0SetIsClosed(t *testing.T) {
	want := map[string]string{
		"xbp-install": "install", "xbp-remove": "remove", "xbp-upgrade": "upgrade",
		"xbp-autoremove": "autoremove", "xbp-update": "update",
	}
	for name, command := range want {
		got, ok := commandFromArgv("/usr/bin/" + name)
		if !ok || got != command {
			t.Errorf("commandFromArgv(%s) = %q, %v; want %q", name, got, ok, command)
		}
	}
	for _, name := range []string{"xbpretty", "xbp"} {
		if got, ok := commandFromArgv("/usr/bin/" + name); ok {
			t.Errorf("commandFromArgv(%s) = %q, true; want the dispatcher", name, got)
		}
	}
	for _, name := range []string{"xbp-clean", "xbp-history", "xbp-fetch", "xbp-undo", "xbp-redo", "xbp-policy", "xbp-list", "xbp-log", "xbp-completion", "xbp-link", "xbp-purge", "xbp-moo"} {
		if got, ok := commandFromArgv("/usr/bin/" + name); ok {
			t.Errorf("commandFromArgv(%s) = %q, true; the set is closed", name, got)
		}
	}
}

func TestSurfaceArityCountsPackageWordsOnly(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	env := map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":     logPath,
		"XB_STUB_STDOUT":  planFixture,
		"NO_COLOR":        "1",
	}
	code, _, errs := surfaceRun(t, bin, env, "install", "--dry-run", "--", "-f")
	if code != 1 || !strings.Contains(errs, "xbpretty: install needs at least one package") {
		t.Errorf("install --dry-run -- -f = %d, %q; want the missing-package error", code, errs)
	}
	code, _, errs = surfaceRun(t, bin, env, "install", "--dry-run", "--", "-f", "figlet")
	if code != 0 {
		t.Errorf("install --dry-run -- -f figlet = %d, %q; want success", code, errs)
	}
}

func TestSurfaceFlagScopingFixes(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	env := map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":     logPath,
		"XB_STUB_STDOUT":  planFixture,
		"NO_COLOR":        "1",
	}
	if code, _, _ := surfaceRun(t, bin, env, "upgrade", "--reinstall"); code != 2 {
		t.Errorf("upgrade --reinstall exit = %d, want 2", code)
	}
	if code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "--reinstall", "--dry-run"); code != 0 {
		t.Errorf("install --reinstall = %d, %q; want success", code, errs)
	}
	if code, _, errs := surfaceRun(t, bin, env, "update", "--no-log"); code == 2 || strings.Contains(errs, "unknown flag") {
		t.Errorf("update --no-log was a usage error: %d, %q", code, errs)
	}
	if code, _, errs := surfaceRun(t, bin, env, "undo", "--no-log"); code == 2 {
		t.Errorf("undo --no-log was a usage error: %q", errs)
	}
}

func TestSurfaceFetchCountryAndConfigInEveryOrder(t *testing.T) {
	bin := taggedBinary(t)
	cfgDir, cfgPath := integrationConfig(t, "Never")
	env := map[string]string{
		"XDG_CONFIG_HOME":           cfgDir,
		"XDG_STATE_HOME":            filepath.Join(filepath.Dir(cfgDir), "state"),
		"NO_COLOR":                  "1",
		"XBPRETTY_TEST_FETCH_XBPSD": t.TempDir(),
	}
	args := [][]string{
		{"fetch", "-c", "IN", "--config", cfgPath},
		{"--config", cfgPath, "fetch", "-c", "IN"},
		{"fetch", "--config", cfgPath, "-c", "IN"},
	}
	for _, argv := range args {
		code, _, errs := surfaceRun(t, bin, env, argv...)
		if code == 2 || strings.Contains(errs, "unknown flag") || strings.Contains(errs, "usage:") {
			t.Errorf("%v did not parse: exit %d\n%s", argv, code, errs)
		}
		if !strings.Contains(errs, "xbpretty: no repositories are configured; add one in /etc/xbps.d") {
			t.Errorf("%v did not reach the fetch handler: exit %d\n%s", argv, code, errs)
		}
	}
}

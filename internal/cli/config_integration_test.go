package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// This file drives configuration behaviour through the built binary: layers,
// --config replacement, --debug reporting, the sudo/doas home rules, the
// privileged-path trust gate and the rev-5 migration warnings. Files that need
// root behaviour are built with the xbpretty_testroot tag; the hooks it adds
// are documented in internal/config/testroot_testhook.go and
// internal/cli/root_testhook.go.

const aggregateRev5Warning = "https://github.com/soubarnak/xbpretty/blob/main/docs/migration-rev6.md"

var (
	taggedBinOnce sync.Once
	taggedBinPath string
	taggedBinErr  error
)

// taggedBinary builds the binary with the test-only euid/system-path hook.
func taggedBinary(t *testing.T) string {
	t.Helper()
	taggedBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "xbp-tagged-")
		if err != nil {
			taggedBinErr = err
			return
		}
		bin := filepath.Join(dir, "xbpretty")
		cmd := exec.Command("go", "build", "-tags", "xbpretty_testroot", "-o", bin, "../../cmd/xbpretty")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			taggedBinErr = err
			t.Logf("tagged build output:\n%s", out)
			return
		}
		taggedBinPath = bin
	})
	if taggedBinErr != nil {
		t.Fatalf("building tagged test binary: %v", taggedBinErr)
	}
	return taggedBinPath
}

// testEnv is the environment of one built-binary run.
type testEnv map[string]string

// cleanEnv builds an environment from the current one, dropping the variables
// this package controls so no ambient value leaks into a test.
func cleanEnv(t *testing.T, dir string) testEnv {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	return testEnv{
		"HOME":            filepath.Join(dir, "home"),
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
		"PATH":            stubDir + ":" + os.Getenv("PATH"),
		"NO_COLOR":        "1",
		"XB_STUB_LOG":     filepath.Join(dir, "commands.log"),
		"XB_STUB_STDOUT":  planFixture,
		"XB_STUB_QUERY":   "figlet-2.2.5_6\n657KB\n",
		"XB_STUB_EXIT":    "0",
	}
}

func (e testEnv) with(key, value string) testEnv {
	e[key] = value
	return e
}

func (e testEnv) without(key string) testEnv {
	delete(e, key)
	return e
}

func (e testEnv) slice() []string {
	out := make([]string, 0, len(e))
	for k, v := range e {
		out = append(out, k+"="+v)
	}
	return out
}

// runBinary runs one command and returns stdout, stderr and the exit code.
func runBinary(t *testing.T, bin string, env testEnv, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env.slice()
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errs.String(), code
}

// writeLayer writes a config.toml under <root>/xbpretty/.
func writeLayer(t *testing.T, root, body string) string {
	t.Helper()
	dir := filepath.Join(root, "xbpretty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// dryRunArgs is the shortest command that loads config and prints a summary.
func dryRunArgs(extra ...string) []string {
	return append([]string{"install", "--dry-run", "--no-sync"}, append(extra, "figlet")...)
}

func TestConfigRelativeXDGFallsBackToHome(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir).with("XDG_CONFIG_HOME", "relative/config")
	homeCfg := writeLayer(t, filepath.Join(dir, "home", ".config"), "[Ui]\nunit = \"Decimal\"\n")
	_ = homeCfg

	out, errs, code := runBinary(t, bin, env, dryRunArgs()...)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(out, "KB") || strings.Contains(out, "KiB") {
		t.Errorf("relative XDG_CONFIG_HOME did not fall back to ~/.config:\n%s", out)
	}
}

func TestConfigSudoUserSuppressesXDG(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir).
		with("XDG_CONFIG_HOME", filepath.Join(dir, "xdg")).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_SUDO_HOME", filepath.Join(dir, "invoker")).
		with("SUDO_USER", "someone")
	// The invoker's passwd home holds the real layer.
	writeLayer(t, filepath.Join(dir, "invoker", ".config"), "[Ui]\nunit = \"Decimal\"\n")
	// The XDG file is malformed: reading it would be a hard error.
	writeLayer(t, filepath.Join(dir, "xdg"), "[Ui]\nmode = \"bogus\"\n")

	out, errs, code := runBinary(t, bin, env, dryRunArgs()...)
	if code != 0 {
		t.Fatalf("exit = %d, XDG was not ignored under sudo:\n%s", code, errs)
	}
	if !strings.Contains(out, "KB") {
		t.Errorf("invoking user's layer did not apply:\n%s", out)
	}
	if strings.Contains(errs, "xbpretty: config ") {
		t.Errorf("the XDG layer was read under sudo:\n%s", errs)
	}
}

func TestConfigLayersComposeAndLaterLayerWins(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	sysPath := filepath.Join(dir, "system.toml")
	if err := os.WriteFile(sysPath, []byte("[Ui]\nunit = \"Decimal\"\n\n[Color]\nmode = \"Never\"\n\n[Color.theme]\nPrimary = { fg = \"Red\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(dir, "config"), "[Ui]\nunit = \"Binary\"\n\n[Color.theme]\nSecondary = { fg = \"Blue\" }\n")
	env = env.with("XBPRETTY_TEST_SYSTEM_CONFIG", sysPath)

	out, errs, code := runBinary(t, bin, env, dryRunArgs()...)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(out, "KiB") || strings.Contains(out, "KB") {
		t.Errorf("user layer did not win the colliding [Ui] unit key:\n%s", out)
	}

	_, dbg, code := runBinary(t, bin, env, dryRunArgs("--debug")...)
	if code != 0 {
		t.Fatalf("debug exit = %d\n%s", code, dbg)
	}
	for _, want := range []string{
		"xbpretty: debug: config layer system " + sysPath + " (read",
		"xbpretty: debug: config layer user " + filepath.Join(dir, "config", "xbpretty", "config.toml") + " (read",
		"[Color.theme] Primary={fg=Red",
		"[Color.theme] Secondary={fg=Blue",
		"unit=Binary",
	} {
		if !strings.Contains(dbg, want) {
			t.Errorf("debug output missing %q:\n%s", want, dbg)
		}
	}
}

func TestConfigFlagReplacesBothLayers(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	sysPath := filepath.Join(dir, "system.toml")
	if err := os.WriteFile(sysPath, []byte("[Ui]\nmode = \"bogus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The default user layer is malformed too.
	writeLayer(t, filepath.Join(dir, "config"), "[Nala]\nauto_remove = \"not-a-bool\"\n")
	env := cleanEnv(t, dir).with("XBPRETTY_TEST_SYSTEM_CONFIG", sysPath)

	cfgPath := filepath.Join(dir, "selected.toml")
	if err := os.WriteFile(cfgPath, []byte("[Ui]\nunit = \"Decimal\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runBinary(t, bin, env, "--config", cfgPath, "install", "--dry-run", "--no-sync", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, --config did not replace both layers:\n%s", code, errs)
	}
	if !strings.Contains(out, "KB") {
		t.Errorf("selected layer did not apply:\n%s", out)
	}
	_, dbg, code := runBinary(t, bin, env, "--debug", "--config", cfgPath, "install", "--dry-run", "--no-sync", "figlet")
	if code != 0 {
		t.Fatalf("debug exit = %d\n%s", code, dbg)
	}
	if !strings.Contains(dbg, "config layer config "+cfgPath+" (read") {
		t.Errorf("debug does not report the selected layer:\n%s", dbg)
	}
	if strings.Contains(dbg, "config layer system") || strings.Contains(dbg, "config layer user") {
		t.Errorf("debug reported a replaced layer:\n%s", dbg)
	}
}

func TestConfigMissingSystemLayerIsSilent(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir).with("XBPRETTY_TEST_SYSTEM_CONFIG", filepath.Join(dir, "absent.toml"))
	_, errs, code := runBinary(t, bin, env, dryRunArgs()...)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if strings.Contains(errs, "xbpretty: config ") {
		t.Errorf("a missing system layer was not silent:\n%s", errs)
	}
}

func TestConfigMalformedSystemLayerIsHardError(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	sysPath := filepath.Join(dir, "system.toml")
	if err := os.WriteFile(sysPath, []byte("[Ui]\nmode = \"bogus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(dir, "config"), "[Ui]\nunit = \"Decimal\"\n")
	env := cleanEnv(t, dir).with("XBPRETTY_TEST_SYSTEM_CONFIG", sysPath)

	_, errs, code := runBinary(t, bin, env, dryRunArgs()...)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: config "+sysPath+":2:") {
		t.Errorf("system error missing its path and line:\n%s", errs)
	}
}

func TestConfigDebugImpliesVerboseAndDumpsResolvedConfig(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)
	path := writeLayer(t, filepath.Join(dir, "config"), "[Nala]\nassume_yes = true\n\n[Ui]\nmode = \"Plain\"\nunit = \"Decimal\"\n")
	_, errs, code := runBinary(t, bin, env, dryRunArgs("--debug")...)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	for _, want := range []string{
		"xbpretty: debug: config layer user " + path + " (read",
		"xbpretty: debug: resolved [Nala] auto_remove=true",
		"xbpretty: debug: resolved [Ui] mode=Plain unit=Decimal",
		"xbpretty: debug: resolved [Color] mode=Auto",
		"verbose: ",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("debug output missing %q:\n%s", want, errs)
		}
	}
}

// copyExecutable copies a stub so a config-only binary path can be proven
// honoured or ignored.
func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

// writePoison creates an executable that records being run and exits 42. A
// configured path pointing at it proves whether the trust gate honoured it.
func writePoison(t *testing.T, dir string) (path, marker string) {
	t.Helper()
	marker = filepath.Join(dir, "poison.marker")
	path = filepath.Join(dir, "poison-xbps-install")
	script := "#!/bin/sh\n: > \"$XB_POISON_MARKER\"\nprintf 'poison\\n' >> \"${XB_STUB_LOG:-/dev/null}\"\nexit 42\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, marker
}

// trustConfigWithInstall writes a selected config naming one install binary
// and a non-privileged key the trust gate must still apply.
func trustConfigWithInstall(t *testing.T, dir, install string) string {
	t.Helper()
	path := filepath.Join(dir, "poisoned.toml")
	body := "[Xbps]\ninstall_path = \"" + install + "\"\n\n[Ui]\nunit = \"Decimal\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func trustFixture(t *testing.T, dir string) (cfgPath string, stubBin string) {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var install string
	for _, name := range []string{"xbps-install", "xbps-remove", "xbps-query"} {
		dst := filepath.Join(binDir, name)
		copyExecutable(t, filepath.Join(stubDir, name), dst)
		if name == "xbps-install" {
			install = dst
		}
	}
	cfgPath = filepath.Join(dir, "selected.toml")
	body := "[Xbps]\n" +
		"install_path = \"" + install + "\"\n" +
		"remove_path = \"" + filepath.Join(binDir, "xbps-remove") + "\"\n" +
		"query_path = \"" + filepath.Join(binDir, "xbps-query") + "\"\n\n" +
		"[Ui]\nunit = \"Decimal\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath, install
}

func TestConfigTrustGateUnderRoot(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	cfgPath, _ := trustFixture(t, dir)
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_TRUST_UID", strconv.Itoa(os.Getuid()))

	t.Run("root-owned file is trusted", func(t *testing.T) {
		e := env.with("PATH", "/usr/bin:/bin")
		out, errs, code := runBinary(t, bin, e, "--config", cfgPath, "install", "--dry-run", "--no-sync", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, configured paths were not honoured:\n%s", code, errs)
		}
		if strings.Contains(errs, "ignoring [Xbps]") {
			t.Errorf("trusted file was ignored:\n%s", errs)
		}
		if !strings.Contains(out, "KB") {
			t.Errorf("non-privileged key did not apply:\n%s", out)
		}
	})

	t.Run("user-owned file is ignored", func(t *testing.T) {
		// The configured install path is a poison binary; the good stub is on
		// PATH. If the untrusted key were honoured the marker would appear and
		// the run would exit 42.
		poison, marker := writePoison(t, dir)
		poisonCfg := trustConfigWithInstall(t, dir, poison)
		e := env.without("XBPRETTY_TEST_TRUST_UID").
			with("PATH", stubDir+":/usr/bin:/bin").
			with("XB_POISON_MARKER", marker)
		out, errs, code := runBinary(t, bin, e, "--config", poisonCfg, "install", "--dry-run", "--no-sync", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, the run did not continue:\n%s", code, errs)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("the untrusted configured binary was executed")
		}
		if !strings.Contains(errs, "ignoring [Xbps] install_path: file is not owned by root or is group/world-writable (uid ") ||
			!strings.Contains(errs, "mode 0644)") {
			t.Errorf("missing the exact trust warning:\n%s", errs)
		}
		if !strings.Contains(out, "KB") {
			t.Errorf("non-privileged keys must still apply:\n%s", out)
		}
		log, err := os.ReadFile(filepath.Join(dir, "commands.log"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(log), "-n figlet") {
			t.Errorf("the PATH stub never ran:\n%s", log)
		}
		if strings.Contains(string(log), "poison") {
			t.Errorf("the poison binary ran:\n%s", log)
		}
	})

	t.Run("group or world writable file is ignored", func(t *testing.T) {
		if err := os.Chmod(cfgPath, 0o666); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(cfgPath, 0o644) })
		e := env.without("XBPRETTY_TEST_TRUST_UID").with("PATH", stubDir+":/usr/bin:/bin")
		_, errs, code := runBinary(t, bin, e, "--config", cfgPath, "install", "--dry-run", "--no-sync", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, errs)
		}
		if !strings.Contains(errs, "mode 0666)") {
			t.Errorf("writable mode was not named:\n%s", errs)
		}
	})
}

func TestConfigLegacyPathIsTrustGatedAfterAliasing(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "selected.toml")
	if err := os.WriteFile(cfgPath, []byte("xbps_install_path = /nonexistent/xbps-install\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("PATH", stubDir+":/usr/bin:/bin")
	_, errs, code := runBinary(t, bin, env, "--config", cfgPath, "install", "--dry-run", "--no-sync", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, the run did not continue:\n%s", code, errs)
	}
	deprecated := strings.Index(errs, "key 'xbps_install_path' is deprecated; use [Xbps] install_path")
	ignored := strings.Index(errs, "ignoring [Xbps] install_path:")
	if deprecated < 0 {
		t.Errorf("missing the deprecation warning:\n%s", errs)
	}
	if ignored < 0 {
		t.Errorf("missing the trust warning after aliasing:\n%s", errs)
	}
	if deprecated >= 0 && ignored >= 0 && deprecated > ignored {
		t.Errorf("trust warning printed before the deprecation warning:\n%s", errs)
	}
	// The ADR-0018 order for a legacy path under sudo: deprecation, then trust.
	if !strings.Contains(errs, "uid "+strconv.Itoa(os.Getuid())) {
		t.Errorf("trust warning does not name the real owner uid:\n%s", errs)
	}
}

func TestConfigSystemLayerIsNotAutomaticallyTrusted(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	sysPath := filepath.Join(dir, "system.toml")
	body := "[Xbps]\ninstall_path = \"/nonexistent/xbps-install\"\n\n[Ui]\nunit = \"Decimal\"\n"
	if err := os.WriteFile(sysPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_SYSTEM_CONFIG", sysPath).
		with("PATH", stubDir+":/usr/bin:/bin")
	out, errs, code := runBinary(t, bin, env, "install", "--dry-run", "--no-sync", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, the run did not continue:\n%s", code, errs)
	}
	if !strings.Contains(errs, "ignoring [Xbps] install_path:") {
		t.Errorf("the user-owned system layer was trusted:\n%s", errs)
	}
	if !strings.Contains(out, "KB") {
		t.Errorf("non-privileged system keys did not apply:\n%s", out)
	}
}

func TestConfigSymlinkIsJudgedOnItsTarget(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	cfgPath, _ := trustFixture(t, dir)
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(cfgPath, link); err != nil {
		t.Fatal(err)
	}
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_TRUST_UID", strconv.Itoa(os.Getuid())).
		with("PATH", "/usr/bin:/bin")
	_, errs, code := runBinary(t, bin, env, "--config", link, "install", "--dry-run", "--no-sync", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, the target inode was not trusted:\n%s", code, errs)
	}
	if strings.Contains(errs, "ignoring [Xbps]") {
		t.Errorf("the symlink was judged instead of its target:\n%s", errs)
	}
}

// TestConfigSudoUserBinaryPathIsIgnored is the real threat case: root reads the
// invoking user's layer, and a user-owned install_path must never execute.
func TestConfigSudoUserBinaryPathIsIgnored(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	poison, marker := writePoison(t, dir)
	invokerHome := filepath.Join(dir, "invoker")
	cfgPath := writeLayer(t, filepath.Join(invokerHome, ".config"),
		"[Xbps]\ninstall_path = \""+poison+"\"\n\n[Ui]\nunit = \"Decimal\"\n")
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_SUDO_HOME", invokerHome).
		with("SUDO_USER", "someone").
		with("PATH", stubDir+":/usr/bin:/bin").
		with("XB_POISON_MARKER", marker)
	out, errs, code := runBinary(t, bin, env, "install", "--dry-run", "--no-sync", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, the run did not continue:\n%s", code, errs)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("a user-owned install_path was executed as the invoking user's layer")
	}
	if !strings.Contains(errs, "config "+cfgPath+": ignoring [Xbps] install_path:") {
		t.Errorf("missing the trust warning for the invoking user's layer:\n%s", errs)
	}
	log, err := os.ReadFile(filepath.Join(dir, "commands.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "poison") {
		t.Errorf("the user-owned binary ran as root:\n%s", log)
	}
	if !strings.Contains(string(log), "-n figlet") {
		t.Errorf("the PATH stub never ran:\n%s", log)
	}
	if !strings.Contains(out, "KB") {
		t.Errorf("the invoking user's non-privileged keys did not apply:\n%s", out)
	}
}

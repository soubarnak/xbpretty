package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// migrationFixture copies a config-side migration fixture into a temp dir and
// returns its path, so the byte-identity check can compare the original to the
// copy after the run.
func migrationFixture(t *testing.T, name string) (original, copyPath string) {
	t.Helper()
	src := filepath.Join("..", "config", "testdata", "migration", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	original = string(data)
	copyPath = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(copyPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return original, copyPath
}

func legacyArguments(t *testing.T) []string {
	t.Helper()
	return []string{"install", "-n", "--no-sync", "figlet"}
}

func TestLegacyAllTenKeysRunWarnAndAreNeverRewritten(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	original, cfgPath := migrationFixture(t, "rev5-all.toml")
	env := cleanEnv(t, dir)

	_, errs, code := runBinary(t, bin, env, append([]string{"--debug", "--config", cfgPath}, legacyArguments(t)...)...)
	if code != 0 {
		t.Fatalf("exit = %d, a rev-5 file must run:\n%s", code, errs)
	}

	mapped := []struct{ key, target string }{
		{"assume_yes", "[Nala] assume_yes"},
		{"sync", "[Nala] auto_update"},
		{"log", "[Xbps] transaction_log"},
		{"xbps_install_path", "[Xbps] install_path"},
		{"xbps_remove_path", "[Xbps] remove_path"},
		{"xbps_query_path", "[Xbps] query_path"},
		{"color", "[Color] mode"},
	}
	for _, m := range mapped {
		want := "key '" + m.key + "' is deprecated; use " + m.target
		if !strings.Contains(errs, want) {
			t.Errorf("missing deprecation warning %q:\n%s", want, errs)
		}
		if got := strings.Count(errs, want); got != 1 {
			t.Errorf("%q printed %d times, want once", m.key, got)
		}
		if wantDebug := "legacy key '" + m.key + "' at " + cfgPath + ":"; !strings.Contains(errs, wantDebug) {
			t.Errorf("--debug does not list %s:\n%s", m.key, errs)
		}
		if wantDebug := "-> " + m.target; !strings.Contains(errs, wantDebug) {
			t.Errorf("--debug does not name the target %q:\n%s", wantDebug, errs)
		}
	}
	retired := []string{
		"key 'verbose' is retired; pass -v on the command line",
		"key 'box_style' is retired and has no effect",
		`key 'progress_bar' is retired; use [Ui] mode = "Plain"`,
	}
	for _, want := range retired {
		if !strings.Contains(errs, want) {
			t.Errorf("missing retired warning %q:\n%s", want, errs)
		}
	}
	if got := strings.Count(errs, aggregateRev5Warning); got != 1 {
		t.Errorf("aggregate warning printed %d times, want once:\n%s", got, errs)
	}
	if !strings.Contains(errs, "legacy keys: 10 seen (7 mapped, 3 retired)") {
		t.Errorf("missing the legacy summary:\n%s", errs)
	}

	// The mapped values apply.
	for _, want := range []string{
		"assume_yes=true",
		"auto_update=false",
		"transaction_log=false",
		"install_path=xbps-install",
		"remove_path=xbps-remove",
		"query_path=xbps-query",
		"mode=Always",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("resolved config missing %q:\n%s", want, errs)
		}
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("the config file was rewritten:\n before %q\n after  %q", original, after)
	}
}

func TestLegacyRetiredKeysHaveNoEffect(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "retired.toml")
	if err := os.WriteFile(cfgPath, []byte("verbose = true\nbox_style = ascii\nprogress_bar = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, append([]string{"--config", cfgPath}, legacyArguments(t)...)...)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if strings.Contains(errs, "verbose: ") {
		t.Errorf("retired verbose changed the run:\n%s", errs)
	}
}

func TestLegacyCanonicalKeyWinsWithinFile(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	_, cfgPath := migrationFixture(t, "rev5-mixed.toml")
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "--debug", "--config", cfgPath, "log")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "assume_yes=false") {
		t.Errorf("the legacy assume_yes beat [Nala] assume_yes:\n%s", errs)
	}
	if !strings.Contains(errs, "mode=Never") {
		t.Errorf("the legacy color beat [Color] mode:\n%s", errs)
	}
	// The legacy keys still warn even when the canonical key wins.
	for _, want := range []string{"key 'assume_yes' is deprecated", "key 'color' is deprecated", "key 'box_style' is retired"} {
		if !strings.Contains(errs, want) {
			t.Errorf("mixed file missing %q:\n%s", want, errs)
		}
	}
}

func TestLegacyLaterLayerWinsAcrossSpellings(t *testing.T) {
	bin := taggedBinary(t)
	dir := t.TempDir()
	env := cleanEnv(t, dir)

	sysPath := filepath.Join(dir, "system.toml")
	if err := os.WriteFile(sysPath, []byte("color = always\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(dir, "config"), "[Color]\nmode = \"Never\"\n")
	e := env.with("XBPRETTY_TEST_SYSTEM_CONFIG", sysPath)
	_, errs, code := runBinary(t, bin, e, "--debug", "log")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "mode=Never") {
		t.Errorf("canonical user layer did not beat the legacy system layer:\n%s", errs)
	}

	// Reverse the spellings: the system is canonical, the user is legacy.
	if err := os.WriteFile(sysPath, []byte("[Color]\nmode = \"Never\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(dir, "config"), "color = always\n")
	_, errs, code = runBinary(t, bin, e, "--debug", "log")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "mode=Always") {
		t.Errorf("legacy user layer did not beat the canonical system layer:\n%s", errs)
	}
}

func TestLegacyKeyAfterFirstTableIsRejected(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	_, cfgPath := migrationFixture(t, "rev5-after-table.toml")
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "--config", cfgPath, "log")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, errs)
	}
	if !strings.Contains(errs, `unknown key "color"; rev-5 keys must appear before the first table`) {
		t.Errorf("missing the exact after-table message:\n%s", errs)
	}
}

func TestLegacyBadValueIsRejected(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	_, cfgPath := migrationFixture(t, "rev5-bad-value.toml")
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "--config", cfgPath, "log")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, errs)
	}
	if !strings.Contains(errs, "key 'assume_yes'") || !strings.Contains(errs, "true or false") {
		t.Errorf("missing the legacy value error:\n%s", errs)
	}
}

func TestBareValueOnRev6KeyIsRejected(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	_, cfgPath := migrationFixture(t, "rev5-bare-rev6.toml")
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "--config", cfgPath, "log")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, errs)
	}
	if !strings.Contains(errs, "bare values are not supported") {
		t.Errorf("rev-6 keys accepted a bare value:\n%s", errs)
	}
}

func TestLegacyAggregateWarningPrintsOncePerRun(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	_, cfgPath := migrationFixture(t, "rev5-all.toml")
	env := cleanEnv(t, dir)
	_, errs, code := runBinary(t, bin, env, "--config", cfgPath, "log")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if got := strings.Count(errs, aggregateRev5Warning); got != 1 {
		t.Errorf("aggregate printed %d times:\n%s", got, errs)
	}
	if strings.Contains(errs, "xbpretty: debug:") {
		t.Errorf("debug output printed without --debug:\n%s", errs)
	}
}

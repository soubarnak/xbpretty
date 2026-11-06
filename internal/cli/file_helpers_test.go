package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes one test file.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// hermeticVars decide where a child reads and writes state. A child that
// inherits any of them from the host reads the host's history store, config
// or state, so every child environment names all four.
var hermeticVars = []string{"XBPRETTY_HISTORY_DIR", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "HOME"}

// hermeticEnv returns a copy of env in which every hermeticVars entry that is
// missing or empty points under one fresh t.TempDir(). Values the test sets are
// kept, so a test that names its own store still gets it. Every helper that
// builds a child environment for the built binary calls this last.
func hermeticEnv(t *testing.T, env map[string]string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(env)+len(hermeticVars))
	for key, value := range env {
		out[key] = value
	}
	var defaults map[string]string
	for _, key := range hermeticVars {
		if out[key] != "" {
			continue
		}
		if defaults == nil {
			defaults = hermeticDefaults(t.TempDir())
		}
		out[key] = defaults[key]
	}
	return out
}

// hermeticDefaults names the four hermetic locations under root.
func hermeticDefaults(root string) map[string]string {
	return map[string]string{
		"XBPRETTY_HISTORY_DIR": filepath.Join(root, "history"),
		"XDG_STATE_HOME":       filepath.Join(root, "state"),
		"XDG_CONFIG_HOME":      filepath.Join(root, "config"),
		"HOME":                 filepath.Join(root, "home"),
	}
}

// testTempRoot is the directory that every t.TempDir call of one test shares.
// Go numbers each call's directory under a single per-test parent.
func testTempRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(t.TempDir())
}

// environMap copies the test process's environment into a map.
func environMap() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		key, value, _ := strings.Cut(kv, "=")
		out[key] = value
	}
	return out
}

// assertHermeticVars fails unless each hermeticVars entry in env is set to a
// path under root.
func assertHermeticVars(t *testing.T, root string, env map[string]string) {
	t.Helper()
	for _, key := range hermeticVars {
		value := env[key]
		if !strings.HasPrefix(value, root+string(filepath.Separator)) {
			t.Errorf("%s = %q, want a path under the test temp root %s", key, value, root)
		}
	}
}

// assertEmptyHistoryListing runs `history` through the built binary with the
// given environment and requires the empty-store listing on stdout.
func assertEmptyHistoryListing(t *testing.T, bin string, env []string) {
	t.Helper()
	cmd := exec.Command(bin, "history")
	cmd.Env = env
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		t.Fatalf("running history: %v\nstderr:\n%s", err, errs.String())
	}
	if out.String() != "No history entries found.\n" {
		t.Errorf("history listed a store outside the test root:\nstdout:\n%s\nstderr:\n%s", out.String(), errs.String())
	}
}

// TestIntegrationEnvIsHermetic holds every env helper to the rule hermeticEnv
// enforces: the four state variables resolve under the test's own temp root,
// and a child started with that environment lists an empty history store
// instead of the host's /var/lib/xbpretty/history.
func TestIntegrationEnvIsHermetic(t *testing.T) {
	bin := buildBinary(t)

	t.Run("cleanEnv", func(t *testing.T) {
		root := testTempRoot(t)
		env := cleanEnv(t, t.TempDir())
		assertHermeticVars(t, root, env)
		assertEmptyHistoryListing(t, bin, env.slice())
	})

	t.Run("emptyConfigDir", func(t *testing.T) {
		root := testTempRoot(t)
		env := emptyConfigDir(t)
		assertHermeticVars(t, root, env)
		assertEmptyHistoryListing(t, bin, envWith(env))
	})

	// The config layer is invalid on purpose, so history stops at config and
	// never reaches a store; only the keys are checked here.
	t.Run("invalidConfigDir", func(t *testing.T) {
		root := testTempRoot(t)
		assertHermeticVars(t, root, invalidConfigDir(t))
	})

	t.Run("discoveryEnv", func(t *testing.T) {
		root := testTempRoot(t)
		_, _, _, env := discoveryEnv(t)
		assertHermeticVars(t, root, env)
		assertEmptyHistoryListing(t, bin, envWith(env))
	})

	t.Run("implicitEnv", func(t *testing.T) {
		root := testTempRoot(t)
		implicitBin, env := implicitEnv(t)
		assertHermeticVars(t, root, env)
		assertEmptyHistoryListing(t, implicitBin, envWith(env))
	})

	// The in-process helpers set the process environment, so the child
	// inherits exactly what the test process holds. The inline helpers are
	// Linux-only and are covered by TestInlineEnvIsHermetic.
	t.Run("newEnv", func(t *testing.T) {
		root := testTempRoot(t)
		newEnv(t)
		assertHermeticVars(t, root, environMap())
		assertEmptyHistoryListing(t, bin, os.Environ())
	})

	t.Run("newCleanFetchEnv", func(t *testing.T) {
		root := testTempRoot(t)
		newCleanFetchEnv(t)
		assertHermeticVars(t, root, environMap())
		assertEmptyHistoryListing(t, bin, os.Environ())
	})

	t.Run("newFetchEnv", func(t *testing.T) {
		root := testTempRoot(t)
		newFetchEnv(t, newFetchFixture(t, nil))
		assertHermeticVars(t, root, environMap())
		assertEmptyHistoryListing(t, bin, os.Environ())
	})
}

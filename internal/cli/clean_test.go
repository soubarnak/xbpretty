package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAC016Clean drives FR-020 through the built binary: `clean` runs
// `xbps-remove -O` and streams its output with no prompt.
func TestAC016Clean(t *testing.T) {
	t.Run("clean runs xbps-remove -O and streams its output", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_RUN_STDOUT"] = "Cleaning up package cache...\n"
		clearLog(t, implicitLog(env))
		code, out, errs := surfaceRun(t, bin, env, "clean")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-O"})
		if !strings.Contains(out, "Cleaning up package cache...") {
			t.Errorf("xbps output was not streamed:\n%s", out)
		}
		if strings.Contains(errs, "[Y/n]") {
			t.Errorf("clean prompted:\n%s", errs)
		}
		if strings.Contains(strings.Join(transactionArgv(t, implicitLog(env)), "|"), "-o") {
			t.Errorf("clean ran autoremove:\n%s", errs)
		}
	})

	t.Run("clean --lists deletes repodata and keeps keys", func(t *testing.T) {
		bin, env := implicitEnv(t)
		db := t.TempDir()
		repo := filepath.Join(db, "https--repo-default.voidlinux.org")
		keys := filepath.Join(db, "keys")
		for _, dir := range []string{repo, filepath.Join(repo, "nested"), keys} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		repodata := filepath.Join(repo, "x86_64-repodata")
		other := filepath.Join(repo, "index.plist")
		nested := filepath.Join(repo, "nested", "deep-repodata")
		key := filepath.Join(keys, "void-packages.gpg")
		for _, path := range []string{repodata, other, nested, key} {
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		env["XBPRETTY_TEST_XBPSDB"] = db
		clearLog(t, implicitLog(env))
		code, out, errs := surfaceRun(t, bin, env, "clean", "--lists")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if out != "" || errs != "" {
			t.Errorf("clean --lists printed: stdout %q stderr %q", out, errs)
		}
		if _, err := os.Stat(repodata); !os.IsNotExist(err) {
			t.Errorf("repodata was not deleted")
		}
		if _, err := os.Stat(nested); err != nil {
			t.Errorf("a repodata below the first directory level was deleted: %v", err)
		}
		if _, err := os.Stat(other); err != nil {
			t.Errorf("non-repodata file was deleted: %v", err)
		}
		if _, err := os.Stat(key); err != nil {
			t.Errorf("key file was deleted: %v", err)
		}
		if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
			t.Errorf("clean --lists ran a child: %q", calls)
		}
	})

	t.Run("--lists wins over --fetch", func(t *testing.T) {
		bin, env := implicitEnv(t)
		db := t.TempDir()
		repo := filepath.Join(db, "repo")
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		repodata := filepath.Join(repo, "x86_64-repodata")
		if err := os.WriteFile(repodata, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		env["XBPRETTY_TEST_XBPSDB"] = db
		clearLog(t, implicitLog(env))
		code, out, errs := surfaceRun(t, bin, env, "clean", "--lists", "--fetch")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if _, err := os.Stat(repodata); !os.IsNotExist(err) {
			t.Errorf("--lists did not win: repodata still exists")
		}
		if strings.Contains(errs, "not implemented") {
			t.Errorf("--fetch ran before --lists:\n%s", errs)
		}
		if out != "" {
			t.Errorf("stdout = %q, want nothing", out)
		}
	})

	t.Run("clean --fetch is the named #74 seam", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "clean", "--fetch")
		if code != 1 {
			t.Errorf("exit = %d, want 1 (the #74 seam)", code)
		}
		if !strings.Contains(errs, "clean --fetch is not implemented in this build") {
			t.Errorf("the #74 seam did not announce itself:\n%s", errs)
		}
		if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
			t.Errorf("clean --fetch ran a child: %q", calls)
		}
	})

	t.Run("clean is root-only", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XBPRETTY_TEST_EUID"] = "1000"
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "clean")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(errs, "hint: rerun as root: ") {
			t.Errorf("missing the elevation hint:\n%s", errs)
		}
		if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
			t.Errorf("a non-root clean ran a child: %q", calls)
		}
	})
}

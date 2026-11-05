package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file proves FR-012 and FR-013 at the built-binary seam: the stub xbps
// programs record every argv, so the tests assert the sync step and the
// transaction commands are separate processes with the spec's own argv.

// updatePlanFixture is three upgradable rows and one held row, the shape
// `xbps-install -n -u` prints.
const updatePlanFixture = "alpha-2.0_1 update x86_64 https://repo-default.voidlinux.org/current 1000 200\n" +
	"beta-2.0_1 update x86_64 https://repo-default.voidlinux.org/current 1000 200\n" +
	"gamma-2.0_1 update x86_64 https://repo-default.voidlinux.org/current 1000 200\n" +
	"held-9.0_1 hold x86_64 https://repo-default.voidlinux.org/current 1000 200\n"

// updateOldFixture answers the installed-version lookup the per-package list
// needs. One installed version is enough: the stub returns it for every name.
const updateOldFixture = "alpha-1.0_1\n657KB\n"

// syncRootEnv builds the tagged binary and an environment whose effective uid
// is euid. The config trust hook reads the same variable, so a layer written
// by the test process is trusted even when the test pretends to be root. The
// stub's real-run branches are silenced so only the plan and explicit
// overrides produce output: the sync step must not echo the plan fixture.
func syncRootEnv(t *testing.T, euid int) (string, testEnv) {
	t.Helper()
	bin := taggedBinary(t)
	env := cleanEnv(t, t.TempDir()).
		with("XBPRETTY_TEST_EUID", strconv.Itoa(euid)).
		with("XBPRETTY_TEST_TRUST_UID", strconv.Itoa(os.Getuid())).
		with("XB_STUB_RUN_STDOUT", "").
		with("XB_STUB_RUN_STDERR", "")
	return bin, env
}

// stubInstallArgs returns the argv of every xbps-install call, without the
// binary name. xbps-query lookups are ignored.
func stubInstallArgs(t *testing.T, env testEnv) []string {
	t.Helper()
	return stubArgs(t, env, "xbps-install")
}

// stubRemoveArgs returns the argv of every xbps-remove call, without the
// binary name.
func stubRemoveArgs(t *testing.T, env testEnv) []string {
	t.Helper()
	return stubArgs(t, env, "xbps-remove")
}

func stubArgs(t *testing.T, env testEnv, binary string) []string {
	t.Helper()
	data, err := os.ReadFile(env["XB_STUB_LOG"])
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != binary {
			continue
		}
		out = append(out, strings.Join(fields[1:], " "))
	}
	return out
}

func assertInstallArgv(t *testing.T, env testEnv, want ...string) {
	t.Helper()
	if got := stubInstallArgs(t, env); !equalStrings(got, want) {
		t.Errorf("xbps-install argv = %q, want %q", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestIntegrationAC008UpgradeSyncsSeparately pins the first half of AC-008: a
// root upgrade runs the index step as its own `xbps-install -S`, on stderr,
// before the dry run, and the transaction argv never carries `-S`.
func TestIntegrationAC008UpgradeSyncsSeparately(t *testing.T) {
	t.Run("root dry run still syncs", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_RUN_STDOUT", "Updating repository indexes...\n")
		out, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S", "-n -u")
		if !strings.Contains(errs, "Updating repository indexes...") {
			t.Errorf("the sync output is not on stderr:\n%s", errs)
		}
		if strings.Contains(out, "Updating repository indexes...") {
			t.Errorf("the sync output leaked to stdout:\n%s", out)
		}
	})

	t.Run("real run is -S then -n -u then -y -u", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		_, errs, code := runBinary(t, bin, env, "upgrade", "-y")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S", "-n -u", "-y -u")
	})

	for _, name := range []string{"full-upgrade", "dist-upgrade", "safe-upgrade"} {
		t.Run(name+" keeps the same argv", func(t *testing.T) {
			bin, env := syncRootEnv(t, 0)
			_, errs, code := runBinary(t, bin, env, name, "--dry-run")
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			assertInstallArgv(t, env, "-S", "-n -u")
		})
	}

	t.Run("[Nala] full_upgrade changes no argv", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		writeLayer(t, env["XDG_CONFIG_HOME"], "[Nala]\nfull_upgrade = true\n")
		_, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S", "-n -u")
	})
}

// TestIntegrationAC008SuppressAndConflict pins the second half of AC-008:
// --no-update/--no-sync suppress the step and --update with either exits 2.
func TestIntegrationAC008SuppressAndConflict(t *testing.T) {
	for _, flag := range []string{"--no-update", "--no-sync"} {
		t.Run(flag+" suppresses the step", func(t *testing.T) {
			bin, env := syncRootEnv(t, 0)
			_, errs, code := runBinary(t, bin, env, "upgrade", flag, "--dry-run")
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			assertInstallArgv(t, env, "-n -u")
		})
	}

	for _, args := range [][]string{
		{"upgrade", "--update", "--no-update"},
		{"upgrade", "--update", "--no-sync"},
		{"upgrade", "--update", "--no-update", "--no-sync"},
		{"install", "figlet", "--update", "--no-update"},
	} {
		t.Run(strings.Join(args, " ")+" conflicts", func(t *testing.T) {
			bin, env := syncRootEnv(t, 0)
			_, errs, code := runBinary(t, bin, env, append(args, "--dry-run")...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2\n%s", code, errs)
			}
			if !strings.Contains(errs, "mutually exclusive") {
				t.Errorf("missing the conflict line:\n%s", errs)
			}
			if got := stubInstallArgs(t, env); len(got) != 0 {
				t.Errorf("a child ran despite the conflict: %q", got)
			}
		})
	}
}

// TestIntegrationAC008CommandDefaults pins which commands sync by default:
// only the upgrade family, and only while [Nala] auto_update is true. CLI
// still wins over the file.
func TestIntegrationAC008CommandDefaults(t *testing.T) {
	t.Run("install does not sync by default", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		_, errs, code := runBinary(t, bin, env, "install", "figlet", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-n figlet")
	})

	t.Run("install --update adds the separate step", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		_, errs, code := runBinary(t, bin, env, "install", "figlet", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S", "-n figlet")
	})

	t.Run("remove --update syncs with xbps-install", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", removePlanFixture)
		_, errs, code := runBinary(t, bin, env, "remove", "wget", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S")
		if got := stubRemoveArgs(t, env); !equalStrings(got, []string{"-n wget"}) {
			t.Errorf("xbps-remove argv = %q, want one -n wget with no -S", got)
		}
	})

	t.Run("purge --update syncs and keeps -f", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", removePlanFixture)
		_, errs, code := runBinary(t, bin, env, "purge", "wget", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S")
		if got := stubRemoveArgs(t, env); !equalStrings(got, []string{"-n -f wget"}) {
			t.Errorf("xbps-remove argv = %q, want one -n -f wget with no -S", got)
		}
	})

	t.Run("autoremove does not sync", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", removePlanFixture)
		_, errs, code := runBinary(t, bin, env, "autoremove", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env)
		if got := stubRemoveArgs(t, env); !equalStrings(got, []string{"-n -o"}) {
			t.Errorf("xbps-remove argv = %q, want one -n -o with no -S", got)
		}
	})

	t.Run("autopurge --update syncs", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", removePlanFixture)
		_, errs, code := runBinary(t, bin, env, "autopurge", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S")
		if got := stubRemoveArgs(t, env); !equalStrings(got, []string{"-n -o -f"}) {
			t.Errorf("xbps-remove argv = %q, want one -n -o -f with no -S", got)
		}
	})

	t.Run("real remove --update keeps -S out of both transaction argv", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", removePlanFixture)
		_, errs, code := runBinary(t, bin, env, "remove", "wget", "--update", "-y")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S")
		if got := stubRemoveArgs(t, env); !equalStrings(got, []string{"-n wget", "-y wget"}) {
			t.Errorf("xbps-remove argv = %q, want -n wget then -y wget", got)
		}
	})

	t.Run("[Nala] auto_update=false flips the upgrade default", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		writeLayer(t, env["XDG_CONFIG_HOME"], "[Nala]\nauto_update = false\n")
		_, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-n -u")
	})

	t.Run("CLI --update still wins over auto_update=false", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		writeLayer(t, env["XDG_CONFIG_HOME"], "[Nala]\nauto_update = false\n")
		_, errs, code := runBinary(t, bin, env, "upgrade", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S", "-n -u")
	})
}

// TestIntegrationAC008NonRootSyncRequest pins FR-013's unprivileged path: the
// request warns and continues against the cached index, and no sync runs.
func TestIntegrationAC008NonRootSyncRequest(t *testing.T) {
	t.Run("upgrade default warns and continues", func(t *testing.T) {
		bin, env := syncRootEnv(t, 1000)
		_, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if !strings.Contains(errs, "not running as root") || !strings.Contains(errs, "stale") {
			t.Errorf("missing the stale-index warning:\n%s", errs)
		}
		assertInstallArgv(t, env, "-n -u")
	})

	t.Run("install --update warns and continues", func(t *testing.T) {
		bin, env := syncRootEnv(t, 1000)
		_, errs, code := runBinary(t, bin, env, "install", "figlet", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if !strings.Contains(errs, "stale") {
			t.Errorf("missing the stale-index warning:\n%s", errs)
		}
		assertInstallArgv(t, env, "-n figlet")
	})

	t.Run("--no-update prints no warning", func(t *testing.T) {
		bin, env := syncRootEnv(t, 1000)
		_, errs, code := runBinary(t, bin, env, "upgrade", "--no-update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if strings.Contains(errs, "stale") {
			t.Errorf("the suppressed sync still warned:\n%s", errs)
		}
	})
}

// TestIntegrationSyncFailureAborts pins FR-013's failure rule: xbps's own exit
// code passes through with the hint, and the dry run never starts.
func TestIntegrationSyncFailureAborts(t *testing.T) {
	t.Run("upgrade", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.
			with("XB_STUB_EXIT", "2").
			with("XB_STUB_RUN_STDERR", "ERROR: failed to open repodata: No such file or directory\n")
		out, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
		if code != 2 {
			t.Fatalf("exit = %d, want 2\n%s", code, errs)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if !strings.Contains(errs, "ERROR: failed to open repodata") {
			t.Errorf("the sync failure was not forwarded:\n%s", errs)
		}
		if !strings.Contains(errs, "hint:") || !strings.Contains(errs, "xbp update") {
			t.Errorf("missing the stale-index hint:\n%s", errs)
		}
		assertInstallArgv(t, env, "-S")
	})

	t.Run("update", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.
			with("XB_STUB_EXIT", "2").
			with("XB_STUB_RUN_STDERR", "ERROR: failed to open repodata: No such file or directory\n")
		out, errs, code := runBinary(t, bin, env, "update")
		if code != 2 {
			t.Fatalf("exit = %d, want 2\n%s", code, errs)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		assertInstallArgv(t, env, "-S")
	})
}

// TestIntegrationSyncSoftFailureContinues pins the measured xbps rule
// (ADR-0002): a fetch that fails everywhere still exits 0, so the run goes on
// to the dry run.
func TestIntegrationSyncSoftFailureContinues(t *testing.T) {
	bin, env := syncRootEnv(t, 0)
	env = env.
		with("XB_STUB_EXIT", "0").
		with("XB_STUB_RUN_STDERR", "ERROR: failed to fetch repository index\n")
	_, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(errs, "ERROR: failed to fetch repository index") {
		t.Errorf("the fetch failure was not forwarded:\n%s", errs)
	}
	assertInstallArgv(t, env, "-S", "-n -u")
}

// TestIntegrationAC009UpdateCount pins FR-012's count path at the binary seam.
func TestIntegrationAC009UpdateCount(t *testing.T) {
	t.Run("three upgradable packages", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", updatePlanFixture)
		out, errs, code := runBinary(t, bin, env, "update")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		want := "3 package(s) can be upgraded. Run 'xbp list --upgradable' to see them.\n"
		if out != want {
			t.Errorf("stdout = %q, want exactly %q", out, want)
		}
		// With the silent stubs, a clean update writes nothing to stderr:
		// no summary, no prompt, no sync noise.
		if errs != "" {
			t.Errorf("stderr = %q, want empty", errs)
		}
		if strings.Contains(errs, "Do you want to continue?") {
			t.Errorf("update prompted:\n%s", errs)
		}
		assertInstallArgv(t, env, "-S", "-n -u")
	})

	t.Run("zero prints nothing extra", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT", "")
		out, errs, code := runBinary(t, bin, env, "update")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		assertInstallArgv(t, env, "-S", "-n -u")
	})

	t.Run("hold rows are not counted", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		env = env.with("XB_STUB_STDOUT",
			"held-9.0_1 hold x86_64 https://repo-default.voidlinux.org/current 1000 200\n")
		out, _, code := runBinary(t, bin, env, "update")
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty for a hold-only plan", out)
		}
	})
}

// TestIntegrationAC009UpdateShowPackages pins FR-012's list form: one
// `name installed -> candidate` line per non-hold row and no count line.
func TestIntegrationAC009UpdateShowPackages(t *testing.T) {
	bin, env := syncRootEnv(t, 0)
	writeLayer(t, env["XDG_CONFIG_HOME"], "[Nala]\nupdate_show_packages = true\n")
	env = env.
		with("XB_STUB_STDOUT", updatePlanFixture).
		with("XB_STUB_OLD", updateOldFixture)
	out, errs, code := runBinary(t, bin, env, "update")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	want := "alpha 1.0_1 -> 2.0_1\nbeta 1.0_1 -> 2.0_1\ngamma 1.0_1 -> 2.0_1\n"
	if out != want {
		t.Errorf("stdout = %q, want exactly %q", out, want)
	}
	if strings.Contains(out, "package(s) can be upgraded") {
		t.Errorf("the count line was printed with update_show_packages = true:\n%q", out)
	}
	assertInstallArgv(t, env, "-S", "-n -u")
}

// TestIntegrationUpdateAlwaysSyncs pins FR-012's mandatory index step: neither
// a CLI flag nor [Nala] auto_update = false can suppress it, and the step runs
// exactly once (never nested).
func TestIntegrationUpdateAlwaysSyncs(t *testing.T) {
	t.Run("auto_update=false does not suppress update", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		writeLayer(t, env["XDG_CONFIG_HOME"], "[Nala]\nauto_update = false\n")
		env = env.with("XB_STUB_STDOUT", updatePlanFixture)
		_, errs, code := runBinary(t, bin, env, "update")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		assertInstallArgv(t, env, "-S", "-n -u")
	})

	t.Run("--no-update is not an update flag", func(t *testing.T) {
		bin, env := syncRootEnv(t, 0)
		_, _, code := runBinary(t, bin, env, "update", "--no-update")
		if code != 2 {
			t.Errorf("exit = %d, want 2: update does not accept --no-update", code)
		}
		if got := stubInstallArgs(t, env); len(got) != 0 {
			t.Errorf("children ran before the usage error: %q", got)
		}
	})
}

// TestIntegrationUpdateExitCodeFollowsXbps pins FR-012's exit rule: a failed
// count dry run passes xbps's code through and prints no count line.
func TestIntegrationUpdateExitCodeFollowsXbps(t *testing.T) {
	bin, env := syncRootEnv(t, 0)
	env = env.
		with("XB_STUB_STDOUT", updatePlanFixture).
		with("XB_STUB_PLAN_EXIT", "3")
	out, errs, code := runBinary(t, bin, env, "update")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (the count plan's code)\n%s", code, errs)
	}
	if strings.Contains(out, "package(s) can be upgraded") {
		t.Errorf("a failed count plan printed the count line:\n%q", out)
	}
	assertInstallArgv(t, env, "-S", "-n -u")
}

// TestIntegrationUpdateRejectsPassthrough pins FR-012/FR-021: update has no
// transaction to pass `--` arguments to, so the passthrough is a usage error
// and no child runs. Silently dropping it (or appending it to the count argv)
// would make `xbp update -- figlet` mean something it does not say.
func TestIntegrationUpdateRejectsPassthrough(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--", "figlet"},
		{"update", "--"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			bin, env := syncRootEnv(t, 0)
			env = env.with("XB_STUB_STDOUT", updatePlanFixture)
			out, errs, code := runBinary(t, bin, env, args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2\n%s", code, errs)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			want := "xbpretty: update takes no arguments and no -- passthrough\n"
			if !strings.Contains(errs, want) {
				t.Errorf("stderr = %q, want %q", errs, want)
			}
			if got := stubInstallArgs(t, env); len(got) != 0 {
				t.Errorf("a child ran despite the usage error: %q", got)
			}
		})
	}
}

// TestIntegrationInvocationGuardsPrecedeSync pins the intentional order: a
// command that is already going to refuse must not refresh the index, and a
// non-root run must not print the stale-index warning for a refusal that
// changes nothing. FR-014 puts the sync before the dry run; these guards are
// invocation checks that FR-007/FR-008 place before the dry run as well.
func TestIntegrationInvocationGuardsPrecedeSync(t *testing.T) {
	for _, euid := range []int{0, 1000} {
		name := "root"
		if euid != 0 {
			name = "non-root"
		}

		t.Run(name+" remove with no installed target", func(t *testing.T) {
			bin, env := syncRootEnv(t, euid)
			env = env.with("XB_STUB_NOT_INSTALLED", "nano")
			out, errs, code := runBinary(t, bin, env, "remove", "nano", "--update", "--dry-run")
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if out != "Nothing to do.\n" {
				t.Errorf("stdout = %q, want Nothing to do.", out)
			}
			if !strings.Contains(errs, "xbpretty: nano is not installed") {
				t.Errorf("the missing target was not reported:\n%s", errs)
			}
			if strings.Contains(errs, "stale") {
				t.Errorf("the refused run printed the stale-index warning:\n%s", errs)
			}
			if got := stubInstallArgs(t, env); len(got) != 0 {
				t.Errorf("the refused run performed the sync: %q", got)
			}
		})

		t.Run(name+" reinstall of a missing target", func(t *testing.T) {
			bin, env := syncRootEnv(t, euid)
			env = env.with("XB_STUB_NOT_INSTALLED", "figlet")
			out, errs, code := runBinary(t, bin, env,
				"install", "figlet", "--reinstall", "--update", "--dry-run")
			if code != 1 {
				t.Fatalf("exit = %d, want 1\n%s", code, errs)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if !strings.Contains(errs, "xbpretty: figlet is not installed, so it cannot be reinstalled") {
				t.Errorf("missing the reinstall refusal:\n%s", errs)
			}
			if strings.Contains(errs, "stale") {
				t.Errorf("the refused run printed the stale-index warning:\n%s", errs)
			}
			if got := stubInstallArgs(t, env); len(got) != 0 {
				t.Errorf("the refused run performed the sync: %q", got)
			}
		})
	}
}

// TestRootTestHookIsAbsentFromOrdinaryBuilds keeps the test-only euid override
// from becoming a production escape hatch: the tagged build carries the
// variable name, the ordinary build never does.
func TestRootTestHookIsAbsentFromOrdinaryBuilds(t *testing.T) {
	ordinary, err := os.ReadFile(buildBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ordinary, []byte("XBPRETTY_TEST_EUID")) {
		t.Error("the ordinary build contains XBPRETTY_TEST_EUID; the root hook leaked out of xbpretty_testroot")
	}
	tagged, err := os.ReadFile(taggedBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tagged, []byte("XBPRETTY_TEST_EUID")) {
		t.Error("the xbpretty_testroot build is missing XBPRETTY_TEST_EUID; the root hook did not compile in")
	}
}

// TestIntegrationUpdateRequiresRoot pins FR-012's root rule at the binary
// seam: no sync, no count plan, the elevation hint, exit 1.
func TestIntegrationUpdateRequiresRoot(t *testing.T) {
	bin, env := syncRootEnv(t, 1000)
	out, errs, code := runBinary(t, bin, env, "update")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errs, "hint: rerun as root:") {
		t.Errorf("missing the elevation hint:\n%s", errs)
	}
	if got := stubInstallArgs(t, env); len(got) != 0 {
		t.Errorf("a non-root update ran a child: %q", got)
	}
}

// TestIntegrationUpdateLogsPackageLessLine pins FR-035: update keeps its
// package-less line in the append-only transaction log.
func TestIntegrationUpdateLogsPackageLessLine(t *testing.T) {
	bin, env := syncRootEnv(t, 0)
	env = env.with("XB_STUB_STDOUT", updatePlanFixture)
	_, errs, code := runBinary(t, bin, env, "update")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	path := filepath.Join(env["XDG_STATE_HOME"], "xbpretty", "transactions.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the transaction log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %q, want one", lines)
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 || fields[1] != "update" {
		t.Errorf("log line = %q, want <timestamp> update with no package field", lines[0])
	}
}

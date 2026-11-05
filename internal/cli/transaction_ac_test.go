package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
)

// TestAC005InstallArgvAndReinstallGuard pins FR-007: the dry run is exactly
// `xbps-install -n PKG`, the real run `xbps-install -y PKG`, --reinstall adds
// -f after an installed guard, and install never adds -S.
func TestAC005InstallArgvAndReinstallGuard(t *testing.T) {
	t.Run("plain install", func(t *testing.T) {
		e := newEnv(t)
		e.disableAutoRemove()
		e.root = true
		e.tty = true
		e.setAnswers("\n")
		code, _, errs := e.run("xbpretty", "install", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if got := e.transactionCalls(); len(got) != 2 || got[0] != "-n figlet" || got[1] != "-y figlet" {
			t.Errorf("commands = %q, want -n figlet then -y figlet", got)
		}
	})

	t.Run("reinstall adds -f", func(t *testing.T) {
		e := newEnv(t)
		e.disableAutoRemove()
		e.root = true
		e.tty = true
		e.setAnswers("\n")
		code, _, errs := e.run("xbpretty", "install", "figlet", "--reinstall")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if got := e.transactionCalls(); len(got) != 2 || got[0] != "-n -f figlet" || got[1] != "-y -f figlet" {
			t.Errorf("commands = %q, want -n -f figlet then -y -f figlet", got)
		}
	})

	t.Run("a missing target refuses before the dry run", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.set("XB_STUB_NOT_INSTALLED", "figlet")
		code, out, errs := e.run("xbpretty", "install", "figlet", "--reinstall", "--dry-run")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		want := "xbpretty: figlet is not installed, so it cannot be reinstalled\n"
		if !strings.Contains(errs, want) {
			t.Errorf("stderr = %q, want %q", errs, want)
		}
		if out != "" {
			t.Errorf("stdout = %q, want nothing", out)
		}
		for _, c := range e.commands() {
			if isDryRun(c) || strings.HasPrefix(c, "-y") {
				t.Errorf("a transaction ran before the guard: %q", c)
			}
		}
	})
}

// TestAC006RemoveFiltersAndPurgeArgv pins FR-008 and FR-009.
func TestAC006RemoveFiltersAndPurgeArgv(t *testing.T) {
	t.Run("a missing target is reported and filtered", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.set("XB_STUB_NOT_INSTALLED", "nano")
		code, out, errs := e.run("xbpretty", "remove", "nano")
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if out != "Nothing to do.\n" {
			t.Errorf("stdout = %q, want Nothing to do.", out)
		}
		if !strings.Contains(errs, "xbpretty: nano is not installed") {
			t.Errorf("the missing target was not reported:\n%s", errs)
		}
		for _, c := range e.commands() {
			if isDryRun(c) || strings.HasPrefix(c, "-y") {
				t.Errorf("a transaction ran with no remaining target: %q", c)
			}
		}
	})

	t.Run("installed targets survive the filter", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.set("XB_STUB_NOT_INSTALLED", "nano")
		e.set("XB_STUB_STDOUT", removePlanFixture)
		code, out, errs := e.run("xbpretty", "remove", "nano", "wget", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if strings.Contains(out, "Nothing to do.") {
			t.Errorf("the installed target was filtered away too:\n%s", out)
		}
		if !strings.Contains(errs, "xbpretty: nano is not installed") {
			t.Errorf("the missing target was not reported:\n%s", errs)
		}
		if got := e.transactionCalls(); len(got) != 1 || got[0] != "-n wget" {
			t.Errorf("commands = %q, want -n wget", got)
		}
	})

	for _, tt := range []struct {
		name string
		argv []string
	}{
		{"purge", []string{"purge", "wget"}},
		{"remove --purge", []string{"remove", "--purge", "wget"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.disableAutoRemove()
			e.root = true
			e.tty = true
			e.setAnswers("\n")
			e.set("XB_STUB_STDOUT", removePlanFixture)
			code, _, errs := e.run(append([]string{"xbpretty"}, tt.argv...)...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if got := e.transactionCalls(); len(got) != 2 || got[0] != "-n -f wget" || got[1] != "-y -f wget" {
				t.Errorf("commands = %q, want -n -f wget then -y -f wget", got)
			}
		})
	}
}

// TestAC007AutoremoveAndAutopurgeArgv pins FR-010: -o, plus -f when purging,
// and no positionals.
func TestAC007AutoremoveAndAutopurgeArgv(t *testing.T) {
	for _, tt := range []struct {
		name string
		argv []string
		want string
	}{
		{"autoremove", []string{"autoremove"}, "-n -o"},
		{"autopurge", []string{"autopurge"}, "-n -o -f"},
		{"autoremove --purge", []string{"autoremove", "--purge"}, "-n -o -f"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			e.set("XB_STUB_STDOUT", removePlanFixture)
			code, _, errs := e.run(append(append([]string{"xbpretty"}, tt.argv...), "--dry-run")...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if got := e.transactionCalls(); len(got) != 1 || got[0] != tt.want {
				t.Errorf("commands = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAC008UpgradeFamilyArgv pins FR-011: the alias names and flags change no
// argv, and --update on install is a separate sync step.
func TestAC008UpgradeFamilyArgv(t *testing.T) {
	for _, argv := range [][]string{
		{"upgrade", "--no-sync"},
		{"full-upgrade", "--no-sync"},
		{"dist-upgrade", "--no-sync"},
		{"safe-upgrade", "--no-sync"},
		{"upgrade", "--no-sync", "--full"},
		{"upgrade", "--no-sync", "--safe"},
		{"upgrade", "--no-sync", "--full", "--no-full"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			code, _, errs := e.run(append(append([]string{"xbpretty"}, argv...), "--dry-run")...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if got := e.transactionCalls(); len(got) != 1 || got[0] != "-n -u" {
				t.Errorf("commands = %q, want one -n -u transaction", got)
			}
		})
	}

	t.Run("install --update syncs first", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		code, _, errs := e.run("xbpretty", "install", "figlet", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		got := e.transactionCalls()
		if len(got) != 2 || got[0] != "-S" || got[1] != "-n figlet" {
			t.Errorf("commands = %q, want -S then -n figlet", got)
		}
	})

	t.Run("remove --update syncs with xbps-install first", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.set("XB_STUB_STDOUT", removePlanFixture)
		code, _, errs := e.run("xbpretty", "remove", "wget", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		got := e.transactionCalls()
		if len(got) != 2 || got[0] != "-S" || got[1] != "-n wget" {
			t.Errorf("commands = %q, want -S then -n wget", got)
		}
	})

	t.Run("remove --update requires the install binary it now uses", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		missing := filepath.Join(e.dir, "no-such-xbps-install")
		e.pointInstallAt(missing)
		code, _, errs := e.run("xbpretty", "remove", "wget", "--update", "--dry-run")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(errs, "xbpretty: xbps binary not found: "+missing) {
			t.Errorf("missing the per-binary error:\n%s", errs)
		}
	})

	t.Run("non-root remove --update does not require xbps-install", func(t *testing.T) {
		e := newEnv(t)
		e.root = false
		missing := filepath.Join(e.dir, "no-such-xbps-install")
		e.pointInstallAt(missing)
		e.set("XB_STUB_STDOUT", removePlanFixture)
		code, _, errs := e.run("xbpretty", "remove", "wget", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0: a non-root run can never execute the sync\n%s", code, errs)
		}
		if !strings.Contains(errs, "stale") {
			t.Errorf("missing the stale-index warning:\n%s", errs)
		}
	})

	t.Run("install --update on a non-root run warns and continues", func(t *testing.T) {
		e := newEnv(t)
		e.root = false
		code, _, errs := e.run("xbpretty", "install", "figlet", "--update", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if !strings.Contains(errs, "not running as root") || !strings.Contains(errs, "stale") {
			t.Errorf("missing the stale-index warning:\n%s", errs)
		}
		if got := e.transactionCalls(); len(got) != 1 || got[0] != "-n figlet" {
			t.Errorf("commands = %q, want only the cached dry run", got)
		}
	})
}

// TestSyncFailureAbortsWithXbpsCode pins FR-013: a failed sync aborts with
// xbps's code and its hint, before the dry run.
func TestSyncFailureAbortsWithXbpsCode(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_EXIT", "2")
	e.set("XB_STUB_RUN_STDERR", "ERROR: failed to open repodata: No such file or directory\n")
	code, _, errs := e.run("xbpretty", "upgrade", "--dry-run")
	if code != 2 {
		t.Errorf("exit = %d, want 2 (the sync's code)", code)
	}
	if !strings.Contains(errs, "ERROR: failed to open repodata") {
		t.Errorf("raw sync stderr missing:\n%s", errs)
	}
	if !strings.Contains(errs, "hint:") || !strings.Contains(errs, "xbp update") {
		t.Errorf("sync hint missing:\n%s", errs)
	}
	for _, c := range e.transactionCalls() {
		if isDryRun(c) {
			t.Errorf("the dry run ran after a failed sync: %q", c)
		}
	}
}

// TestAC009UpdateShowPackages checks FR-012's second output form.
func TestAC009UpdateShowPackages(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cfg := filepath.Join(e.dir, "config", "xbpretty", "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(data, []byte("\n[Nala]\nupdate_show_packages = true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	e.set("XB_STUB_STDOUT", "figlet-2.2.5_7 update x86_64 https://repo-default.voidlinux.org/current 672593 94372\n")
	e.set("XB_STUB_OLD", "figlet-2.2.5_6\n657KB\n")
	code, out, errs := e.run("xbpretty", "update")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "figlet 2.2.5_6 -> 2.2.5_7\n") {
		t.Errorf("stdout is missing the per-package line:\n%q", out)
	}
	if strings.Contains(out, "package(s) can be upgraded") {
		t.Errorf("the count line was printed with update_show_packages = true:\n%q", out)
	}
}

// TestAC014EmptyHoldAndConfigurePlans pins FR-017.
func TestAC014EmptyHoldAndConfigurePlans(t *testing.T) {
	t.Run("empty prints Nothing to do and never prompts", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		e.set("XB_STUB_STDOUT", "")
		e.setAnswers("\n")
		code, out, errs := e.run("xbpretty", "install", "figlet")
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if out != "Nothing to do.\n" {
			t.Errorf("stdout = %q, want exactly Nothing to do.", out)
		}
		if strings.Contains(errs, "Do you want to continue?") {
			t.Errorf("the empty plan prompted:\n%s", errs)
		}
		for _, c := range e.commands() {
			if strings.HasPrefix(c, "-y") {
				t.Errorf("the empty plan executed a transaction: %q", c)
			}
		}
	})

	t.Run("hold-only prints the read-only summary and never prompts", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		e.set("XB_STUB_STDOUT", "acl-2.4.0_1 hold x86_64 https://repo-default.voidlinux.org/current 43080 20653\n")
		e.setAnswers("\n")
		code, out, errs := e.run("xbpretty", "upgrade")
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if !strings.Contains(out, "acl") {
			t.Errorf("the hold-only summary is missing:\n%s", out)
		}
		if strings.Contains(errs, "Do you want to continue?") {
			t.Errorf("the hold-only plan prompted:\n%s", errs)
		}
		for _, c := range e.commands() {
			if strings.HasPrefix(c, "-y") {
				t.Errorf("the hold-only plan executed a transaction: %q", c)
			}
		}
	})

	t.Run("configure-only is a real plan", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		e.set("XB_STUB_STDOUT", "figlet-2.2.5_6 configure x86_64 https://repo-default.voidlinux.org/current\n")
		e.setAnswers("\n")
		code, out, errs := e.run("xbpretty", "install", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if out == "Nothing to do.\n" {
			t.Errorf("the configure plan was treated as empty")
		}
		if !strings.Contains(errs, "Do you want to continue?") {
			t.Errorf("the configure plan did not prompt:\n%s", errs)
		}
		ranReal := false
		for _, c := range e.commands() {
			if strings.HasPrefix(c, "-y") {
				ranReal = true
			}
		}
		if !ranReal {
			t.Errorf("the configure plan never ran: %q", e.commands())
		}
	})
}

// TestAC011AssumeNo covers FR-015's assume-no paths.
func TestAC011AssumeNo(t *testing.T) {
	t.Run("refuses after the summary without prompting", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		code, out, errs := e.run("xbpretty", "install", "figlet", "-n")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(out, "figlet") {
			t.Errorf("the summary is missing from stdout:\n%s", out)
		}
		if strings.Contains(errs, "Do you want to continue?") {
			t.Errorf("-n prompted:\n%s", errs)
		}
		if !strings.Contains(errs, "xbpretty: user refused confirmation") {
			t.Errorf("missing the refusal line:\n%s", errs)
		}
		for _, c := range e.commands() {
			if strings.HasPrefix(c, "-y") {
				t.Errorf("-n executed the transaction: %q", c)
			}
		}
	})

	t.Run("with --dry-run prints the plan and exits 0", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		code, out, errs := e.run("xbpretty", "install", "figlet", "-n", "--dry-run")
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if !strings.Contains(out, "figlet") {
			t.Errorf("the plan is missing:\n%s", out)
		}
		if strings.Contains(errs, "user refused confirmation") {
			t.Errorf("-n --dry-run refused instead of printing the plan:\n%s", errs)
		}
	})

	t.Run("-n with -y is a usage error", func(t *testing.T) {
		e := newEnv(t)
		code, _, _ := e.run("xbpretty", "install", "figlet", "-n", "-y")
		if code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
	})
}

// TestAC012NonTTYExactMessage pins FR-015's piped-stdin refusal.
func TestAC012NonTTYExactMessage(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	code, out, errs := e.run("xbpretty", "install", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("the summary is missing:\n%s", out)
	}
	want := "xbpretty: stdin is not a terminal, so xbpretty will not prompt; pass -y to proceed\n"
	if !strings.HasSuffix(errs, want) {
		t.Errorf("stderr = %q, want the exact non-TTY line at the end", errs)
	}
	if strings.Contains(errs, "Do you want to continue?") {
		t.Errorf("the non-TTY run prompted:\n%s", errs)
	}
}

// TestNonRootPipedStdinGetsTheHint pins FR-073's unconditional priority over
// the non-TTY message, even when stdin is a pipe.
func TestNonRootPipedStdinGetsTheHint(t *testing.T) {
	e := newEnv(t)
	e.root = false
	e.tty = false
	code, out, errs := e.run("xbpretty", "install", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("the summary is missing:\n%s", out)
	}
	if !strings.Contains(errs, "hint: rerun as root: ") {
		t.Errorf("missing the elevation hint:\n%s", errs)
	}
	if strings.Contains(errs, "stdin is not a terminal") {
		t.Errorf("a non-root run took the root-only non-TTY path:\n%s", errs)
	}
	if strings.Contains(errs, "Do you want to continue?") {
		t.Errorf("the non-root run prompted:\n%s", errs)
	}
}

// TestExitCodePassThrough keeps FR-073's xbps pass-through over both runs.
func TestExitCodePassThrough(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.setAnswers("\n")
	e.set("XB_STUB_EXIT", "17")
	code, _, errs := e.run("xbpretty", "install", "figlet")
	if code != 17 {
		t.Errorf("exit = %d, want 17 (the real run's code)\n%s", code, errs)
	}
}

// TestAC056BinaryExistenceIsPerCommand pins FR-053: only the binaries the
// selected command uses are checked, at runtime.
func TestAC056BinaryExistenceIsPerCommand(t *testing.T) {
	e := newEnv(t)
	e.root = true
	missing := filepath.Join(e.dir, "no-such-xbps-install")
	e.pointInstallAt(missing)

	// install needs xbps-install, so it fails before any child runs.
	code, _, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
	if code != 1 {
		t.Errorf("install exit = %d, want 1", code)
	}
	if !strings.Contains(errs, "xbpretty: xbps binary not found: "+missing) {
		t.Errorf("missing the per-binary error:\n%s", errs)
	}

	// remove uses xbps-remove and xbps-query, both present, so the missing
	// install binary must not matter.
	e.set("XB_STUB_STDOUT", removePlanFixture)
	code, out, errs := e.run("xbpretty", "remove", "wget", "--dry-run")
	if code != 0 {
		t.Errorf("remove exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "wget") {
		t.Errorf("remove summary missing:\n%s", out)
	}
}

// TestDownloadOnlyChecksComeFirst pins FR-040's checks before the #72 seam:
// a non-root run gets the elevation hint, -n refuses, and a piped root run
// gets the non-TTY line, all without reaching the seam.
func TestDownloadOnlyChecksComeFirst(t *testing.T) {
	t.Run("non-root", func(t *testing.T) {
		e := newEnv(t)
		e.root = false
		e.tty = true
		code, _, errs := e.run("xbpretty", "install", "figlet", "--download-only")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(errs, "hint: rerun as root: ") {
			t.Errorf("missing the elevation hint:\n%s", errs)
		}
		if strings.Contains(errs, "download-only for install is not implemented") {
			t.Errorf("the #72 seam ran before the checks:\n%s", errs)
		}
	})
	t.Run("assume-no", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = true
		code, _, errs := e.run("xbpretty", "install", "figlet", "--download-only", "-n")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(errs, "user refused confirmation") {
			t.Errorf("missing the refusal:\n%s", errs)
		}
	})
	t.Run("non-tty", func(t *testing.T) {
		e := newEnv(t)
		e.root = true
		e.tty = false
		code, _, errs := e.run("xbpretty", "install", "figlet", "--download-only")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(errs, "stdin is not a terminal") {
			t.Errorf("missing the non-TTY line:\n%s", errs)
		}
	})
}

// ptyAnswers starts a Run with a real pseudo-terminal on stdin and writes
// answers to it after the prompt can be drawn. It returns the exit code and
// both streams.
func ptyAnswers(t *testing.T, e *env, answers string, argv ...string) (int, string, string) {
	t.Helper()
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	// Exercise the real TCGETS check on stdin rather than the test's
	// isTerminalFn switch.
	orig := isTerminalFn
	isTerminalFn = isTerminal
	t.Cleanup(func() { isTerminalFn = orig })
	defer master.Close()
	defer slave.Close()

	var out, errs strings.Builder
	done := make(chan int, 1)
	go func() { done <- Run(argv, slave, &out, &errs) }()
	// Give the prompt time to print before the answer arrives; a PTY buffers
	// either way, so this is only to keep the assertion order natural.
	time.Sleep(50 * time.Millisecond)
	if _, err := master.WriteString(answers); err != nil {
		t.Fatalf("writing the answer: %v", err)
	}
	select {
	case code := <-done:
		return code, out.String(), errs.String()
	case <-time.After(10 * time.Second):
		t.Fatal("the transaction never finished")
		return 0, "", ""
	}
}

// TestAC010PromptOverAPTYPins FR-015's exact prompt bytes and its answers.
func TestAC010PromptOverAPTY(t *testing.T) {
	proceed := []string{"\n", "y\n", "Y\n", "yes\n"}
	for _, answer := range proceed {
		t.Run(fmt.Sprintf("proceed %q", answer), func(t *testing.T) {
			e := newEnv(t)
			e.disableAutoRemove()
			e.root = true
			code, _, errs := ptyAnswers(t, e, answer, []string{"xbpretty", "install", "figlet"}...)
			if code != 0 {
				t.Errorf("exit = %d, want 0\n%s", code, errs)
			}
			if !strings.Contains(errs, "Do you want to continue? [Y/n] ") {
				t.Errorf("the prompt bytes are missing:\n%q", errs)
			}
			if strings.Contains(errs, "user refused confirmation") {
				t.Errorf("a valid yes was refused:\n%s", errs)
			}
		})
	}

	refuse := []string{"n\n", "N\n", "no\n"}
	for _, answer := range refuse {
		t.Run(fmt.Sprintf("refuse %q", answer), func(t *testing.T) {
			e := newEnv(t)
			e.disableAutoRemove()
			e.root = true
			code, _, errs := ptyAnswers(t, e, answer, []string{"xbpretty", "install", "figlet"}...)
			if code != 1 {
				t.Errorf("exit = %d, want 1\n%s", code, errs)
			}
			if !strings.Contains(errs, "Do you want to continue? [Y/n] xbpretty: user refused confirmation\n") {
				t.Errorf("the exact refusal bytes are missing:\n%q", errs)
			}
			for _, c := range e.commands() {
				if strings.HasPrefix(c, "-y") {
					t.Errorf("a refused run executed the transaction: %q", c)
				}
			}
		})
	}

	t.Run("invalid answer is not re-prompted", func(t *testing.T) {
		e := newEnv(t)
		e.disableAutoRemove()
		e.root = true
		code, _, errs := ptyAnswers(t, e, "x\n", []string{"xbpretty", "install", "figlet"}...)
		if code != 1 {
			t.Errorf("exit = %d, want 1\n%s", code, errs)
		}
		if !strings.Contains(errs, "Do you want to continue? [Y/n] xbpretty: 'x' is not a valid response\n") {
			t.Errorf("the invalid-answer bytes are missing:\n%q", errs)
		}
		if strings.Count(errs, "Do you want to continue? [Y/n] ") != 1 {
			t.Errorf("the prompt was shown more than once:\n%s", errs)
		}
	})
}

// TestAC056NonRootSummaryAndHint pins FR-073's non-root path, including the
// exact `xbp <cmd>` hint.
func TestAC056NonRootSummaryAndHint(t *testing.T) {
	e := newEnv(t)
	e.root = false
	e.tty = true
	code, out, errs := e.run("xbpretty", "install", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "figlet") {
		t.Errorf("the summary is missing:\n%s", out)
	}
	if !strings.Contains(errs, "hint: rerun as root: ") {
		t.Errorf("missing the elevation hint:\n%s", errs)
	}
	if !strings.Contains(errs, "xbp install figlet") {
		t.Errorf("the hint does not name `xbp install figlet`:\n%s", errs)
	}
	if strings.Contains(errs, "Do you want to continue?") {
		t.Errorf("the non-root run prompted:\n%s", errs)
	}
}

// TestAC011AssumeNoOnAPipeStillRefuses proves -n does not fall into the
// non-TTY path: it refuses without prompting, whatever stdin is.
func TestAC011AssumeNoOnAPipeStillRefuses(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = false
	code, _, errs := e.run("xbpretty", "install", "figlet", "-n")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errs, "xbpretty: user refused confirmation") {
		t.Errorf("missing the refusal line:\n%s", errs)
	}
	if strings.Contains(errs, "stdin is not a terminal") {
		t.Errorf("-n took the non-TTY path instead of refusing:\n%s", errs)
	}
}

// epipeWriter fails every write the way a closed stdout pipe does.
type epipeWriter struct{}

func (epipeWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

// TestEPIPEKeepsTheTransactionAndItsCode proves FR-074 end to end: the
// summary cannot be written, yet the real transaction still runs and the
// run's own exit code survives.
func TestEPIPEKeepsTheTransactionAndItsCode(t *testing.T) {
	e := newEnv(t)
	e.root = true
	var errs strings.Builder
	// The child exits 0 and writes its stdout; only the copy into the closed
	// stdout fails. The run must keep the child's code, not report the EPIPE.
	code := Run([]string{"xbpretty", "install", "figlet", "-y"}, strings.NewReader(""), epipeWriter{}, &errs)
	if code != 0 {
		t.Errorf("exit = %d, want the child's code 0\n%s", code, errs.String())
	}
	ranReal := false
	for _, c := range e.commands() {
		if strings.HasPrefix(c, "-y") {
			ranReal = true
		}
	}
	if !ranReal {
		t.Errorf("a failed stdout write aborted the transaction: %q", e.commands())
	}
}

// TestNoLogLineOnNonSuccessfulRuns pins ADR-0008/FR-013: a failed run, a
// refused run and a dry run write no transaction line.
func TestNoLogLineOnNonSuccessfulRuns(t *testing.T) {
	tests := []struct {
		name    string
		answers string
		setup   func(*env)
		argv    []string
		want    int
	}{
		{
			name:  "failure",
			setup: func(e *env) { e.set("XB_STUB_EXIT", "17") },
			argv:  []string{"xbpretty", "install", "figlet", "-y"},
			want:  17,
		},
		{
			name:    "decline",
			answers: "n\n",
			setup:   func(*env) {},
			argv:    []string{"xbpretty", "install", "figlet"},
			want:    1,
		},
		{
			name:  "dry run",
			setup: func(*env) {},
			argv:  []string{"xbpretty", "install", "figlet", "--dry-run"},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			e.tty = true
			tt.setup(e)
			if tt.answers != "" {
				e.setAnswers(tt.answers)
			}
			code, _, errs := e.run(tt.argv...)
			if code != tt.want {
				t.Fatalf("exit = %d, want %d\n%s", code, tt.want, errs)
			}
			path, err := config.TransactionLogPath()
			if err != nil {
				t.Fatalf("log path: %v", err)
			}
			if _, err := os.Stat(path); err == nil {
				t.Errorf("%s wrote a transaction log line", tt.name)
			}
		})
	}
}

// TestFullUpgradeConfigChangesNothing pins FR-011: [Nala] full_upgrade is an
// accepted no-op for argv.
func TestFullUpgradeConfigChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cfg := filepath.Join(e.dir, "config", "xbpretty", "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(data, []byte("\n[Nala]\nfull_upgrade = true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := e.run("xbpretty", "upgrade", "--no-sync", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got := e.transactionCalls(); len(got) != 1 || got[0] != "-n -u" {
		t.Errorf("commands = %q, want one -n -u transaction", got)
	}
}

// TestMalformedPlanIsARuntimeError pins NFR-003/FR-073: an unreadable plan is
// a runtime error (1), not a usage error (2).
func TestMalformedPlanIsARuntimeError(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "this is not an xbps plan\n")
	code, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run")
	if code != 1 {
		t.Errorf("exit = %d, want 1\n%s", code, errs)
	}
	if !strings.Contains(errs, "cannot read the xbps plan") {
		t.Errorf("missing the plan error:\n%s", errs)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
}

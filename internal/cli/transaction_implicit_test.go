package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// implicitPlan is an orphan plan in the shape `xbps-remove -n -o` prints.
const implicitPlan = "cups-2.4.19_1 remove x86_64 https://repo-default.voidlinux.org/current 13764009 5564250\n" +
	"lsof-4.99.7_1 remove x86_64 https://repo-default.voidlinux.org/current 341414 140795\n"

// implicitEnv is one built-binary run's environment: a config pointing at the
// stubs, a fake root, and separate fixtures for the install and remove plans.
func implicitEnv(t *testing.T, extra ...string) (bin string, env map[string]string) {
	t.Helper()
	bin = taggedBinary(t)
	cfgDir, _ := integrationConfig(t, "Never", extra...)
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
	return bin, map[string]string{
		"XDG_CONFIG_HOME":         cfgDir,
		"XDG_STATE_HOME":          filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":             logPath,
		"XB_STUB_STDOUT":          planFixture,
		"XB_STUB_REMOVE_STDOUT":   implicitPlan,
		"XB_STUB_RUN_STDOUT":      "",
		"XB_STUB_EXIT":            "0",
		"XB_STUB_PLAN_EXIT":       "0",
		"XBPRETTY_TEST_EUID":      "0",
		"XBPRETTY_TEST_TRUST_UID": strconv.Itoa(os.Getuid()),
		"NO_COLOR":                "1",
	}
}

// implicitLog is the stub log path inside implicitEnv's environment.
func implicitLog(env map[string]string) string { return env["XB_STUB_LOG"] }

// clearLog makes a run's calls unambiguous.
func clearLog(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// runPTYBinary starts the binary with a pseudo-terminal on stdin and writes
// the scripted answers to it, so FR-015's interactive prompt runs for real.
func runPTYBinary(t *testing.T, bin string, env map[string]string, answers string, args ...string) (int, string, string) {
	t.Helper()
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()

	cmd := exec.Command(bin, args...)
	cmd.Env = envWith(env)
	cmd.Stdin = slave
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		slave.Close()
		t.Fatal(err)
	}
	slave.Close()

	go func() {
		_, _ = io.WriteString(master, answers)
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("running %s %v: timed out waiting for the prompt", bin, args)
	}
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("running %s %v: %v", bin, args, err)
	return 0, "", ""
}

// transactionArgv filters the stub log down to xbps transaction calls, in
// order, without the binary name and without xbps-query lookups.
func transactionArgv(t *testing.T, logPath string) []string {
	t.Helper()
	var out []string
	for _, line := range stubCalls(t, logPath) {
		binary, args := splitStubLine(line)
		if binary == "xbps-query" {
			continue
		}
		out = append(out, args)
	}
	return out
}

func wantArgv(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("xbps argv = %q, want %q", got, want)
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func promptCount(stderr string) int {
	return strings.Count(stderr, "Do you want to continue? [Y/n] ")
}

// hasSummarySection reports whether the rendered summary has a section label
// line exactly equal to label. Rev 6 labels are one leading space plus the
// operation name between two rules, so a substring match would accept
// AutoRemove for Remove.
func hasSummarySection(out, label string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == label {
			return true
		}
	}
	return false
}

// TestAC013ImplicitAutoremove drives FR-016 through the built binary: a
// successful real install plans, summarises and prompts a second
// `xbps-remove -n -o` transaction and runs it.
func TestAC013ImplicitAutoremove(t *testing.T) {
	t.Run("default auto_remove prompts and runs the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "y\ny\n", "install", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n figlet", "-y figlet", "-n -o", "-y -o"})
		if !strings.Contains(out, "cups") {
			t.Errorf("the orphan summary is missing:\n%s", out)
		}
		if !hasSummarySection(out, "AutoRemove") {
			t.Errorf("the implicit orphan summary is not labelled AutoRemove:\n%s", out)
		}
		if hasSummarySection(out, "Remove") {
			t.Errorf("the implicit orphan summary reused the primary command's Remove label:\n%s", out)
		}
		if got := promptCount(errs); got != 2 {
			t.Errorf("prompts = %d, want 2 (primary then orphan)\n%s", got, errs)
		}
	})

	t.Run("config auto_remove=false skips the orphan check", func(t *testing.T) {
		bin, env := implicitEnv(t, "", "[Nala]", "auto_remove = false")
		clearLog(t, implicitLog(env))
		code, _, errs := runPTYBinary(t, bin, env, "y\n", "install", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-n figlet", "-y figlet"})
		if promptCount(errs) != 1 {
			t.Errorf("prompts = %d, want only the primary one\n%s", promptCount(errs), errs)
		}
	})

	t.Run("--no-auto-remove skips the orphan check", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := runPTYBinary(t, bin, env, "y\n", "install", "figlet", "--no-auto-remove")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-n figlet", "-y figlet"})
	})

	t.Run("--no-autoremove alias skips too", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := runPTYBinary(t, bin, env, "y\n", "install", "figlet", "--no-autoremove")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-n figlet", "-y figlet"})
	})

	t.Run("-y skips the implicit run unless --auto-remove is explicit", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, out, errs := surfaceRun(t, bin, env, "install", "figlet", "-y")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if strings.Contains(strings.Join(transactionArgv(t, implicitLog(env)), "|"), "-o") {
			t.Errorf("a -y run planned the implicit orphan transaction: %q", transactionArgv(t, implicitLog(env)))
		}
		if strings.Contains(errs, "[Y/n]") {
			t.Errorf("a -y run prompted:\n%s", errs)
		}
		if strings.Contains(out, "cups") {
			t.Errorf("a -y run printed the orphan summary:\n%s", out)
		}
	})

	t.Run("-y with --auto-remove runs the orphan transaction unprompted", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "-y", "--auto-remove")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n figlet", "-y figlet", "-n -o", "-y -o"})
		if strings.Contains(errs, "[Y/n]") {
			t.Errorf("an assume_yes run prompted:\n%s", errs)
		}
	})

	t.Run("--autoremove is the positive alias", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "-y", "--autoremove")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n figlet", "-y figlet", "-n -o", "-y -o"})
	})

	t.Run("--auto-remove forces the orphan run over auto_remove=false", func(t *testing.T) {
		bin, env := implicitEnv(t, "", "[Nala]", "auto_remove = false")
		clearLog(t, implicitLog(env))
		code, _, errs := runPTYBinary(t, bin, env, "y\ny\n", "install", "figlet", "--auto-remove")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n figlet", "-y figlet", "-n -o", "-y -o"})
	})

	t.Run("both flags are a usage error", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "--auto-remove", "--no-auto-remove")
		if code != 2 {
			t.Errorf("exit = %d, want 2\n%s", code, errs)
		}
		if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
			t.Errorf("a conflicting flag pair ran a child: %q", calls)
		}
	})

	t.Run("purge passes -f to the shared orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_STDOUT"] = removePlanFixture
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "y\ny\n", "purge", "wget")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n -f wget", "-y -f wget", "-n -o -f", "-y -o -f"})
		if !hasSummarySection(out, "AutoPurge") {
			t.Errorf("the implicit orphan summary is not labelled AutoPurge:\n%s", out)
		}
	})

	t.Run("remove runs the orphan transaction after success", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_STDOUT"] = removePlanFixture
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "y\ny\n", "remove", "wget")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n wget", "-y wget", "-n -o", "-y -o"})
		if !hasSummarySection(out, "AutoRemove") {
			t.Errorf("the implicit orphan summary is not labelled AutoRemove:\n%s", out)
		}
	})

	t.Run("remove --purge propagates -f to the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_STDOUT"] = removePlanFixture
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "y\ny\n", "remove", "--purge", "wget")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n -f wget", "-y -f wget", "-n -o -f", "-y -o -f"})
		if !hasSummarySection(out, "AutoPurge") {
			t.Errorf("the implicit orphan summary is not labelled AutoPurge:\n%s", out)
		}
	})

	t.Run("install --purge is a no-op and keeps the orphan transaction -o only", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := runPTYBinary(t, bin, env, "y\ny\n", "install", "figlet", "--purge")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n figlet", "-y figlet", "-n -o", "-y -o"})
	})

	t.Run("the upgrade family runs the orphan transaction after success", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := runPTYBinary(t, bin, env, "y\ny\n", "upgrade", "--no-sync")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n -u", "-y -u", "-n -o", "-y -o"})
	})

	t.Run("a decline leaves the primary result and exits 0", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "y\nn\n", "install", "figlet")
		if code != 0 {
			t.Errorf("exit = %d, want 0 (the primary result)\n%s", code, errs)
		}
		calls := transactionArgv(t, implicitLog(env))
		wantArgv(t, calls, []string{"-n figlet", "-y figlet", "-n -o"})
		if containsCall(calls, "-y -o") {
			t.Errorf("a declined orphan prompt still ran the transaction: %q", calls)
		}
		if !strings.Contains(out, "figlet") {
			t.Errorf("the primary summary is missing:\n%s", out)
		}
	})

	t.Run("an empty primary plan still checks and runs the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_STDOUT"] = ""
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "y\n", "install", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)),
			[]string{"-n figlet", "-n -o", "-y -o"})
		if strings.Contains(out, "Nothing to do.") {
			t.Errorf("Nothing to do printed although the orphan plan had rows:\n%s", out)
		}
		if !strings.Contains(out, "cups") {
			t.Errorf("the orphan summary is missing:\n%s", out)
		}
	})

	t.Run("Nothing to do prints only when both plans are empty", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_STDOUT"] = ""
		env["XB_STUB_REMOVE_STDOUT"] = ""
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "", "install", "figlet")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		if out != "Nothing to do.\n" {
			t.Errorf("stdout = %q, want Nothing to do.", out)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-n figlet", "-n -o"})
		if strings.Contains(errs, "[Y/n]") {
			t.Errorf("an empty plan prompted:\n%s", errs)
		}
	})

	t.Run("an empty primary plan with a refused orphan is exit 1", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_STDOUT"] = ""
		clearLog(t, implicitLog(env))
		code, out, errs := runPTYBinary(t, bin, env, "n\n", "install", "figlet")
		if code != 1 {
			t.Errorf("exit = %d, want 1 (the normal refusal)\n%s", code, errs)
		}
		if !strings.Contains(errs, "xbpretty: user refused confirmation") {
			t.Errorf("missing the refusal line:\n%s", errs)
		}
		if strings.Contains(out, "Nothing to do.") {
			t.Errorf("Nothing to do printed before the orphan refusal:\n%s", out)
		}
		if containsCall(transactionArgv(t, implicitLog(env)), "-y -o") {
			t.Errorf("a refused orphan prompt still ran the transaction")
		}
	})

	t.Run("dry run never executes either transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "--dry-run")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-n figlet"})
	})

	t.Run("download-only never plans or executes the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		// --auto-remove makes the implicit run eligible even under -y, so
		// this pins download-only's suppression rather than the -y rule.
		code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "--download-only", "-y", "--auto-remove")
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (the #72 seam)\n%s", code, errs)
		}
		calls := transactionArgv(t, implicitLog(env))
		for _, c := range calls {
			if strings.HasPrefix(c, "-y") {
				t.Errorf("download-only ran a real transaction: %q", c)
			}
			if c == "-n -o" {
				t.Errorf("download-only planned the orphan transaction: %q", calls)
			}
		}
	})

	t.Run("an orphan transaction failure becomes the run's code", func(t *testing.T) {
		bin, env := implicitEnv(t)
		env["XB_STUB_REMOVE_EXIT"] = "9"
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "install", "figlet", "-y", "--auto-remove")
		if code != 9 {
			t.Errorf("exit = %d, want 9 (the orphan transaction's code)\n%s", code, errs)
		}
	})

	// FR-016 limits the implicit transaction to install/remove/purge/upgrade.
	// These commands must never plan or run it even with --auto-remove.
	t.Run("update never runs the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "update")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, errs)
		}
		wantArgv(t, transactionArgv(t, implicitLog(env)), []string{"-S", "-n -u"})
	})

	t.Run("download never runs the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "download", "figlet")
		if code != 1 {
			t.Errorf("exit = %d, want 1 (the #72 seam)\n%s", code, errs)
		}
		for _, c := range transactionArgv(t, implicitLog(env)) {
			if strings.Contains(" "+c+" ", " -o ") {
				t.Errorf("download planned or ran the orphan transaction: %q", c)
			}
		}
	})

	t.Run("replay verbs never run the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		code, _, errs := surfaceRun(t, bin, env, "undo")
		if code != 1 {
			t.Errorf("exit = %d, want 1 (#69's unimplemented seam)\n%s", code, errs)
		}
		if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
			t.Errorf("undo ran a child: %q", calls)
		}
	})

	t.Run("fetch never runs the orphan transaction", func(t *testing.T) {
		bin, env := implicitEnv(t)
		clearLog(t, implicitLog(env))
		// fetch's only deterministic form here is its N/A refusal; either
		// way it never enters the transaction path.
		code, _, errs := surfaceRun(t, bin, env, "fetch", "--sources")
		if code != 2 {
			t.Errorf("exit = %d, want 2\n%s", code, errs)
		}
		if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
			t.Errorf("fetch ran a child: %q", calls)
		}
	})
}

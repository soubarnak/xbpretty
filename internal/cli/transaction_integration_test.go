package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// upgradeDryCalls is the upgrade dry-run argv: the sync step is root-only.
func upgradeDryCalls() []string {
	if os.Geteuid() == 0 {
		return []string{"-S", "-n -u"}
	}
	return []string{"-n -u"}
}

// stubCalls reads the shared stub log and returns its non-empty lines.
func stubCalls(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var calls []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return calls
}

// TestIntegrationTransactionArgv drives the built binary against the stub
// xbps programs and checks the exact argv §3.2's table names.
func TestIntegrationTransactionArgv(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	env := map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":     logPath,
		"XB_STUB_STDOUT":  planFixture,
		"NO_COLOR":        "1",
	}
	tests := []struct {
		name string
		args []string
		bin  string
		want []string
	}{
		{"install dry", []string{"install", "figlet", "--dry-run"}, "xbps-install", []string{"-n figlet"}},
		{"purge dry", []string{"purge", "figlet", "--dry-run"}, "xbps-remove", []string{"-n -f figlet"}},
		{"autopurge dry", []string{"autopurge", "--dry-run"}, "xbps-remove", []string{"-n -o -f"}},
		{"upgrade no-sync dry", []string{"upgrade", "--no-sync", "--dry-run"}, "xbps-install", []string{"-n -u"}},
		{"upgrade dry", []string{"upgrade", "--dry-run"}, "xbps-install", upgradeDryCalls()},
		{"autoremove dry", []string{"autoremove", "--dry-run"}, "xbps-remove", []string{"-n -o"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			code, out, errs := surfaceRun(t, bin, env, tt.args...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errs)
			}
			var got []string
			for _, line := range stubCalls(t, logPath) {
				binary, args := splitStubLine(line)
				if binary == "xbps-query" {
					continue
				}
				if binary != tt.bin {
					t.Errorf("child = %q, want %q (argv %q)", binary, tt.bin, args)
				}
				got = append(got, args)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("xbps argv = %q, want %q", got, tt.want)
			}
			for _, line := range stubCalls(t, logPath) {
				if strings.HasPrefix(line, "xbps-install -S") || line == "xbps-install -S" {
					continue
				}
				_, args := splitStubLine(line)
				if strings.Contains(" "+args+" ", " -S ") || strings.HasPrefix(args, "-S ") {
					t.Errorf("-S appeared inside a transaction argv: %q", args)
				}
			}
		})
	}
}

// TestIntegrationNonTTYExactMessage drives FR-015's piped-stdin refusal
// through the real binary, where there is no in-process terminal fake.
func TestIntegrationNonTTYExactMessage(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("AC-012's non-TTY line is the root path; the non-root path is FR-073's hint")
	}
	bin, logPath, cfgDir := integrationEnv(t)
	cmd := integrationCmd(bin, cfgDir, logPath, "install", "figlet")
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 1 {
		t.Fatalf("exit = %v, want 1\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "figlet") {
		t.Errorf("summary missing from stdout:\n%s", stdout.String())
	}
	want := "xbpretty: stdin is not a terminal, so xbpretty will not prompt; pass -y to proceed\n"
	if !strings.HasSuffix(stderr.String(), want) {
		t.Errorf("stderr = %q, want the exact non-TTY line at the end", stderr.String())
	}
	if strings.Contains(stderr.String(), "Do you want to continue?") {
		t.Errorf("the built binary prompted on a pipe:\n%s", stderr.String())
	}
}

// TestIntegrationEPIPEKeepsTheRun drives FR-074: a closed stdout makes the
// summary writes fail, but the dry run keeps its exit code and does not die
// from SIGPIPE.
func TestIntegrationEPIPEKeepsTheRun(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	cfgDir, _ = integrationConfig(t, "Never")
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	read.Close() // every write to the pipe is now EPIPE

	cmd := integrationCmd(bin, cfgDir, logPath, "upgrade", "--dry-run")
	cmd.Stdout = write
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		write.Close()
		t.Fatal(err)
	}
	write.Close()
	if err := cmd.Wait(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 0 {
			t.Fatalf("exit = %v, want 0 after EPIPE\n%s", err, stderr.String())
		}
	}
}

// TestIntegrationPrintURIsStopsBeforeAnyTransaction pins FR-028 through the
// built binary: one NDJSON object per planned download, no sync, no -y, no
// prompt, no log and no root.
func TestIntegrationPrintURIsStopsBeforeAnyTransaction(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	env := map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":     logPath,
		"XB_STUB_STDOUT":  planFixture,
		"NO_COLOR":        "1",
	}
	code, out, errs := surfaceRun(t, bin, env, "upgrade", "--print-uris", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	want := `{"schema":1,"package":"figlet","pkgver":"figlet-2.2.5_6","repository":"https://repo-default.voidlinux.org/current","url":"https://repo-default.voidlinux.org/current/figlet-2.2.5_6.x86_64.xbps","signature_url":"https://repo-default.voidlinux.org/current/figlet-2.2.5_6.x86_64.xbps.sig2","size":94372,"sha256":null}` + "\n"
	if out != want {
		t.Errorf("stdout = %q, want exactly %q", out, want)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want nothing", errs)
	}
	for _, call := range stubCalls(t, logPath) {
		if strings.HasPrefix(call, "xbps-install -S") || strings.HasPrefix(call, "xbps-install -y") {
			t.Errorf("--print-uris ran a sync or real transaction child: %q", call)
		}
	}
	logPathTransaction := filepath.Join(filepath.Dir(cfgDir), "state", "xbpretty", "transactions.log")
	if _, err := os.Stat(logPathTransaction); err == nil {
		t.Error("--print-uris wrote a transaction log line")
	}
}

// TestIntegrationNonRootTTYPrintsHint drives FR-073 through the built binary
// as an unprivileged user with a real terminal on stdin.
func TestIntegrationNonRootTTYPrintsHint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this test needs a non-root process")
	}
	bin, logPath, cfgDir := integrationEnv(t)
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
	defer slave.Close()

	cmd := integrationCmd(bin, cfgDir, logPath, "install", "figlet")
	cmd.Stdin = slave
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	err = cmd.Wait()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 1 {
		t.Fatalf("exit = %v, want 1\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "figlet") {
		t.Errorf("summary missing from stdout:\n%s", stdout.String())
	}
	errs := stderr.String()
	if !strings.Contains(errs, "hint: rerun as root: ") {
		t.Errorf("missing the elevation hint:\n%s", errs)
	}
	if !strings.Contains(errs, "xbp install figlet") {
		t.Errorf("the hint does not name the command:\n%s", errs)
	}
	if strings.Contains(errs, "Do you want to continue?") {
		t.Errorf("the non-root run prompted:\n%s", errs)
	}
	if strings.Contains(errs, "stdin is not a terminal") {
		t.Errorf("the non-root run took the non-TTY path:\n%s", errs)
	}
}

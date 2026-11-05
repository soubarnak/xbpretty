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
		want []string
	}{
		{"install dry", []string{"install", "figlet", "--dry-run"}, []string{"-n figlet"}},
		{"purge dry", []string{"purge", "figlet", "--dry-run"}, []string{"-n -f figlet"}},
		{"autopurge dry", []string{"autopurge", "--dry-run"}, []string{"-n -o -f"}},
		{"upgrade no-sync dry", []string{"upgrade", "--no-sync", "--dry-run"}, []string{"-n -u"}},
		{"upgrade dry", []string{"upgrade", "--dry-run"}, upgradeDryCalls()},
		{"autoremove dry", []string{"autoremove", "--dry-run"}, []string{"-n -o"}},
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
			for _, call := range stubCalls(t, logPath) {
				if strings.HasPrefix(call, "-p ") || call == "-H" || strings.HasPrefix(call, "-X ") {
					continue
				}
				got = append(got, call)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("xbps argv = %q, want %q", got, tt.want)
			}
			for _, call := range got {
				if call == "-S" {
					continue // the separate sync step, never part of the transaction
				}
				if strings.Contains(" "+call+" ", " -S ") || strings.HasPrefix(call, "-S ") {
					t.Errorf("-S appeared inside a transaction argv: %q", call)
				}
			}
		})
	}
}

// TestIntegrationNonTTYExactMessage drives FR-015's piped-stdin refusal
// through the real binary, where there is no in-process terminal fake.
func TestIntegrationNonTTYExactMessage(t *testing.T) {
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

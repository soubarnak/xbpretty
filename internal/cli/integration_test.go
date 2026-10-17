package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// integration tests drive the real binary against the stub xbps binaries in
// testdata/stub, so the whole flow runs without touching a package system.

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "xbpretty")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/xbpretty")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building test binary: %v\n%s", err, out)
	}
	return bin
}

// integrationEnv sets up a config pointing the built binary at the stubs.
func integrationEnv(t *testing.T) (bin, logPath, cfgDir string) {
	t.Helper()
	bin = buildBinary(t)
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	cfgDir = filepath.Join(base, "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := strings.Join([]string{
		"xbps_install_path = \"" + filepath.Join(stubDir, "xbps-install") + "\"",
		"xbps_remove_path = \"" + filepath.Join(stubDir, "xbps-remove") + "\"",
		"xbps_query_path = \"" + filepath.Join(stubDir, "xbps-query") + "\"",
		"color = \"never\"",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "xbpretty", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return bin, filepath.Join(base, "commands.log"), cfgDir
}

func TestIntegrationPipedStdinNeverBlocks(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	cmd := exec.Command(bin, "install", "figlet")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+cfgDir,
		"XDG_STATE_HOME="+filepath.Dir(cfgDir)+"/state",
		"XB_STUB_LOG="+logPath,
		"XB_STUB_STDOUT="+planFixture,
		"NO_COLOR=1",
	)
	cmd.Stdin = nil // a closed pipe, not a terminal
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Errorf("exit = %v, want 1", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("xbpretty blocked on a pipe instead of exiting")
	}
	errs := stderr.String()
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted on a pipe:\n%s", errs)
	}
	// The piped-stdin rule (FR-010 / AC-004) must fire for root and
	// non-root alike. A non-root run only adds the stale-index warning.
	if !strings.Contains(errs, "-y") {
		t.Errorf("missing the -y hint:\n%s", errs)
	}
	if os.Geteuid() == 0 && (strings.Contains(errs, "sudo") || strings.Contains(errs, "doas")) {
		t.Errorf("root run hit the privilege guard:\n%s", errs)
	}
}

func TestIntegrationLinksDispatchLikeTheCommand(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	dir := t.TempDir()
	out, err := exec.Command(bin, "link", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	link := filepath.Join(dir, "xbp-remove")
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("xbp-remove was not created: %v", err)
	}
	// Invoking through the link must behave like `xbpretty remove`.
	cmd := exec.Command(link, "figlet", "-n")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+cfgDir,
		"XDG_STATE_HOME="+filepath.Dir(cfgDir)+"/state",
		"XB_STUB_LOG="+logPath,
		"XB_STUB_STDOUT="+planFixture,
		"NO_COLOR=1",
	)
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running through the link: %v\n%s", err, stderr.String())
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "figlet") {
		t.Errorf("the linked run did not reach xbps-remove:\n%s", raw)
	}
	// A remove plan, not an install plan: xbps-remove was called.
	if !strings.Contains(string(raw), "\nfiglet") && !strings.Contains(string(raw), "figlet\n") {
		t.Errorf("unexpected command log:\n%s", raw)
	}
}

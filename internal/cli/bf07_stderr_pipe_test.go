package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBrokenStderrPipeDoesNotKillChild is BF-07: the reader of xbpretty's
// stderr is gone before the transaction starts. A closed stderr pipe must not
// kill the child (xbps) with SIGPIPE in the middle of a transaction: FR-074
// says a write failure stops the display and never aborts the run.
//
// The fake real run writes about 200 KiB to stderr, more than a pipe buffer
// holds, so the child is certain to write after xbpretty has stopped reading.
// It then leaves a marker, which only a child that ran to its end can create.
func TestBrokenStderrPipeDoesNotKillChild(t *testing.T) {
	// The reader end is closed before xbpretty starts, so every write to the
	// write end fails with EPIPE.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.Close(); err != nil {
		t.Fatal(err)
	}
	assertFloodingInstallCompletes(t, pw)
}

// TestBrokenStderrENOSPCDoesNotKillChild is the same check with a real kernel
// write error: /dev/full fails every write with ENOSPC, as a full disk does
// when stderr is redirected to a file on it.
func TestBrokenStderrENOSPCDoesNotKillChild(t *testing.T) {
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no /dev/full: %v", err)
	}
	assertFloodingInstallCompletes(t, full)
}

// assertFloodingInstallCompletes runs the test-root binary's real install
// against the flooding stand-in, with its stderr set to sink, and checks that
// the child ran to its end and that xbpretty exited 0.
func assertFloodingInstallCompletes(t *testing.T, sink *os.File) {
	t.Helper()
	bin := testrootBinary(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "finished")

	wrapperDir := floodingXbps(t, marker)
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_HISTORY_DIR", filepath.Join(dir, "history")).
		with("PATH", wrapperDir+":"+os.Getenv("PATH"))

	cmd := exec.Command(bin, "install", "-y", "--no-sync", "--no-auto-remove", "figlet")
	cmd.Env = env.slice()
	cmd.Stdout = io.Discard
	cmd.Stderr = sink
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = sink.Close()
	err := cmd.Wait()

	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running xbpretty: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d, want 0 (141 means the child was killed by SIGPIPE)", code)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the real run did not reach its last line: %v", err)
	}
}

// floodingXbps writes a stand-in xbps-install into a fresh directory and
// returns that directory. A plan (-n) goes to the stub unchanged. A real run
// (-y) writes about 200 KiB to stderr, more than a pipe buffer holds, so the
// child is certain to write after its reader has gone, and then creates
// marker, which only a child that ran to its end can do.
func floodingXbps(t *testing.T, marker string) string {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "${1:-}" in
  -*y*)
    line=$(head -c 1000 /dev/zero | tr '\0' x)
    i=0
    while [ "$i" -lt 200 ]; do
      echo "$line" >&2
      i=$((i+1))
    done
    : > '%s'
    exit 0 ;;
esac
exec '%s/xbps-install' "$@"
`, marker, stubDir)
	if err := os.WriteFile(filepath.Join(dir, "xbps-install"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

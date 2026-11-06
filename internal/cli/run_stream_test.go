package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestStreamDirectEPIPEReturnsChildStatus is FR-074's copy-goroutine case: the
// child exits 0 but writing its stdout to a non-*os.File writer fails with
// EPIPE. Wait's copy error must not be reported as a failed run.
func TestStreamDirectEPIPEReturnsChildStatus(t *testing.T) {
	r := &runner{
		stdout: epipeWriter{},
		stderr: io.Discard,
		stdin:  strings.NewReader(""),
	}
	code, err := r.streamDirect("/bin/sh", "-c", "printf hello; exit 0")
	if err != nil {
		t.Fatalf("streamDirect returned %v, want the child's status", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
}

// TestStreamStderrKeepsStreamsApart pins ADR-0006 at the sync step: both
// streams are displayed on stderr, but the hint table receives them
// separately.
func TestStreamStderrKeepsStreamsApart(t *testing.T) {
	var displayed bytes.Buffer
	r := &runner{
		stdout: io.Discard,
		stderr: &displayed,
		stdin:  strings.NewReader(""),
	}
	code, output, err := r.streamStderr("/bin/sh", "-c", "printf OUT; printf ERR >&2; exit 3")
	if err != nil {
		t.Fatalf("streamStderr returned %v", err)
	}
	if code != 3 {
		t.Errorf("code = %d, want 3", code)
	}
	if strings.TrimSpace(output.Stdout) != "OUT" {
		t.Errorf("stdout capture = %q, want OUT", output.Stdout)
	}
	if strings.TrimSpace(output.Stderr) != "ERR" {
		t.Errorf("stderr capture = %q, want ERR", output.Stderr)
	}
	if !strings.Contains(displayed.String(), "OUT") || !strings.Contains(displayed.String(), "ERR") {
		t.Errorf("both streams must be displayed on stderr, got %q", displayed.String())
	}
}

// TestStreamStderrSurvivesClosedDisplay covers the same EPIPE rule on the
// diagnostic step: a closed display writer must not hide the child's code.
func TestStreamStderrSurvivesClosedDisplay(t *testing.T) {
	r := &runner{
		stdout: io.Discard,
		stderr: epipeWriter{},
		stdin:  strings.NewReader(""),
	}
	code, output, err := r.streamStderr("/bin/sh", "-c", "printf OUT; printf ERR >&2; exit 3")
	if err != nil {
		t.Fatalf("streamStderr returned %v, want the child's status", err)
	}
	if code != 3 {
		t.Errorf("code = %d, want 3", code)
	}
	if strings.TrimSpace(output.Stdout) != "OUT" || strings.TrimSpace(output.Stderr) != "ERR" {
		t.Errorf("captures = %+v, want the two streams recorded separately", output)
	}
}

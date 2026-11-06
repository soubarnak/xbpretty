package cli

import (
	"io"
	"strings"
	"testing"
	"time"
)

// TestStreamHybridLongLinesDoNotDeadlock is BF-06: a child line longer than the
// scanner's 64 KiB token ceiling used to stop the telemetry scan, so the child
// blocked on a full pipe for as long as the transaction held the pkgdb lock.
// Each case writes one 70000-byte line, then 512 KiB more before a trailer. The
// tail is what makes the block certain: the pipe and PTY buffers absorb about
// 130 KiB, so a child whose reader has stopped cannot finish. The run must
// return with the child's status and the trailer must reach the captured
// stream. The direct cases run the same child through the no-PTY fallback
// (BF-46).
func TestStreamHybridLongLinesDoNotDeadlock(t *testing.T) {
	const stdoutScript = `head -c 70000 /dev/zero | tr '\0' x; echo; head -c 524288 /dev/zero | tr '\0' y; echo; echo done`
	const stderrScript = `head -c 70000 /dev/zero | tr '\0' x >&2; echo >&2; head -c 524288 /dev/zero | tr '\0' y >&2; echo >&2; echo done >&2`

	cases := []struct {
		name     string
		script   string
		onStderr bool
		ptyFails bool
	}{
		{name: "pty stdout", script: stdoutScript},
		{name: "pty stderr", script: stderrScript, onStderr: true},
		{name: "direct stdout", script: stdoutScript, ptyFails: true},
		{name: "direct stderr", script: stderrScript, onStderr: true, ptyFails: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ptyFails {
				forcePTYFailure(t)
			}
			r := &runner{live: true, stdout: io.Discard, stderr: io.Discard, stdin: strings.NewReader("")}

			type outcome struct {
				code int
				err  error
			}
			done := make(chan outcome, 1)
			go func() {
				code, err := r.stream("/bin/sh", "-c", tc.script)
				done <- outcome{code: code, err: err}
			}()

			var got outcome
			select {
			case got = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("stream did not return within 10s: the child is blocked on a full pipe")
			}
			if got.err != nil {
				t.Fatalf("stream returned %v", got.err)
			}
			if got.code != 0 {
				t.Errorf("code = %d, want 0", got.code)
			}

			captured := r.lastStdout
			if tc.onStderr {
				captured = r.lastStderr
			}
			if !strings.HasSuffix(captured, "done\n") {
				t.Errorf("captured stream does not end with the trailer: tail %q", lastBytes(captured, 16))
			}
		})
	}
}

// lastBytes returns the last n bytes of s, for failure messages.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

// breakingWriter accepts its first failAfter writes and fails every later one
// with err. It counts the writes that reached it.
type breakingWriter struct {
	mu        sync.Mutex
	failAfter int
	err       error
	writes    int
}

func (b *breakingWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes++
	if b.writes > b.failAfter {
		return 0, b.err
	}
	return len(p), nil
}

func (b *breakingWriter) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writes
}

// eioSink stands in for a terminal that has hung up: every write fails with EIO.
type eioSink struct{}

func (eioSink) Write([]byte) (int, error) { return 0, syscall.EIO }

// TestResilientWriterConcurrentWritesSurviveEPIPE: the live renderer, the
// serialized copy goroutines and the main goroutine all write stderr. When the
// stream breaks partway through, every concurrent write must still succeed,
// and nothing may reach the broken sink afterwards. Run under -race.
func TestResilientWriterConcurrentWritesSurviveEPIPE(t *testing.T) {
	sink := &breakingWriter{failAfter: 50, err: syscall.EPIPE}
	w := newResilientWriter(sink)

	const writers, writesEach = 32, 200
	const line = "line\n"
	var wg sync.WaitGroup
	var bad atomic.Int32
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesEach; j++ {
				if n, err := w.Write([]byte(line)); err != nil || n != len(line) {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if got := bad.Load(); got != 0 {
		t.Errorf("%d writes returned an error or a short count, want none", got)
	}
	if got := sink.count(); got != 51 {
		t.Errorf("sink saw %d writes, want 51: 50 accepted, the EPIPE, then none", got)
	}
}

// TestLossyWriterSwallowsEveryWriteError: a diagnostic stream marks itself dead
// on any write error, reports none to the caller, and discards what follows.
func TestLossyWriterSwallowsEveryWriteError(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"EIO", syscall.EIO},
		{"ENOSPC", syscall.ENOSPC},
		{"EPIPE", syscall.EPIPE},
		{"closed pipe", io.ErrClosedPipe},
		{"other", errors.New("other failure")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &breakingWriter{failAfter: 0, err: tc.err}
			w := newLossyWriter(sink)
			for i := 0; i < 3; i++ {
				if n, err := w.Write([]byte("abc")); n != 3 || err != nil {
					t.Fatalf("write %d = (%d, %v), want (3, nil)", i, n, err)
				}
			}
			if got := sink.count(); got != 1 {
				t.Errorf("sink saw %d writes, want 1: the first failure ends the stream", got)
			}
		})
	}
}

// TestResilientWriterReturnsOtherErrors pins stdout's contract: only a closed
// pipe is swallowed; any other write error still reaches the caller.
func TestResilientWriterReturnsOtherErrors(t *testing.T) {
	for _, want := range []error{syscall.EIO, syscall.ENOSPC} {
		w := newResilientWriter(&breakingWriter{failAfter: 0, err: want})
		if _, got := w.Write([]byte("abc")); !errors.Is(got, want) {
			t.Errorf("Write error = %v, want %v", got, want)
		}
	}
}

// TestBrokenStderrSinkEIODoesNotKillChild is the integration-style check: a child
// that writes far more stderr than a pipe holds runs to its end when the sink
// returns EIO. The runner gets the same lossy stderr that Run installs, and
// the child is the BF-07 flood, run directly rather than through a pipe.
func TestBrokenStderrSinkEIODoesNotKillChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "finished")
	xbps := filepath.Join(floodingXbps(t, marker), "xbps-install")

	r := &runner{stdout: io.Discard, stderr: newLossyWriter(eioSink{}), stdin: strings.NewReader("")}
	code, err := r.streamDirect(xbps, "-y", "figlet")
	if err != nil {
		t.Fatalf("streamDirect returned %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0 (141 means the child was killed by SIGPIPE)", code)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the real run did not reach its last line: %v", err)
	}
}

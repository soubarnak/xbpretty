package cli

import (
	"errors"
	"io"
	"sync"
)

// resilientWriter keeps a broken output stream from aborting a transaction
// (FR-074). The stream is written from several goroutines at once (the live
// renderer, the serialized copy goroutines and the main goroutine), so Write
// holds a lock throughout.
//
// The first write failure marks the stream dead and later writes are
// discarded, so the run keeps its transaction and its exit code. A
// resilientWriter from newResilientWriter swallows only a closed pipe and
// returns any other error unchanged; one from newLossyWriter swallows every
// error, because a diagnostic stream must never starve or SIGPIPE the child
// (for example a stderr whose terminal has hung up, or whose disk is full).
type resilientWriter struct {
	mu     sync.Mutex
	w      io.Writer
	lossy  bool
	broken bool
}

// newResilientWriter wraps stdout: a closed pipe ends the stream quietly, and
// any other write error reaches the caller.
func newResilientWriter(w io.Writer) *resilientWriter {
	return &resilientWriter{w: w}
}

// newLossyWriter wraps the diagnostic stream (stderr): any write error ends the
// stream quietly and nothing is reported to the caller.
func newLossyWriter(w io.Writer) *resilientWriter {
	return &resilientWriter{w: w, lossy: true}
}

func (r *resilientWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.broken {
		return len(p), nil
	}
	n, err := r.w.Write(p)
	if err != nil {
		if r.lossy || isBrokenPipe(err) || errors.Is(err, io.ErrClosedPipe) {
			r.broken = true
			return len(p), nil
		}
		return n, err
	}
	return n, nil
}

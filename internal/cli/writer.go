package cli

import (
	"errors"
	"io"
)

// resilientWriter keeps a broken output stream from aborting a transaction
// (FR-074). The first write failure — an EPIPE from a closed pipe, most
// often — marks the stream dead; later writes are discarded so the run keeps
// its transaction and its exit code. Writes that fail for another reason are
// returned unchanged.
type resilientWriter struct {
	w      io.Writer
	broken bool
}

func newResilientWriter(w io.Writer) *resilientWriter {
	return &resilientWriter{w: w}
}

func (r *resilientWriter) Write(p []byte) (int, error) {
	if r.broken {
		return len(p), nil
	}
	n, err := r.w.Write(p)
	if err != nil {
		if isBrokenPipe(err) || errors.Is(err, io.ErrClosedPipe) {
			r.broken = true
			return len(p), nil
		}
		return n, err
	}
	return n, nil
}

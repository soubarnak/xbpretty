//go:build linux

package term

import (
	"errors"
	"io"
	"testing"
	"time"
)

// TestDecoderReportsEOFWhenThePeerCloses is AC-050's EOF binding: closing the
// pty master makes ReadKey return io.EOF instead of spinning on the readable
// hangup (a zero-byte read is terminal EOF, not a retry).
func TestDecoderReportsEOFWhenThePeerCloses(t *testing.T) {
	master, slave := openPty(t)
	defer slave.Close()
	dec := NewDecoder(slave)
	done := make(chan error, 1)
	go func() {
		_, err := dec.ReadKey()
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	master.Close()
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("ReadKey after peer close = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadKey did not report EOF after the peer closed")
	}
}

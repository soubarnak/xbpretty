//go:build linux

package term

import (
	"errors"
	"io"
	"os"
	"strings"
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

// decodePipeKeys drains dec on its own goroutine, as the screen's key loop
// does, and delivers the keys once the decoder returns an error.
func decodePipeKeys(dec *Decoder) <-chan []Key {
	out := make(chan []Key, 1)
	go func() {
		var keys []Key
		for {
			k, err := dec.ReadKey()
			if err != nil {
				out <- keys
				return
			}
			keys = append(keys, k)
		}
	}()
	return out
}

func writePipe(t *testing.T, w *os.File, s string) {
	t.Helper()
	if _, err := w.WriteString(s); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}

// TestPasteWithGapStaysDiscarded is BF-08: a paste delivered in two chunks with
// a pause longer than the old 100 ms window must stay discarded, so the pasted
// y never reaches the key stream and cannot confirm a transaction (FR-072).
func TestPasteWithGapStaysDiscarded(t *testing.T) {
	for _, gap := range []time.Duration{150 * time.Millisecond, 250 * time.Millisecond} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		decoded := decodePipeKeys(NewDecoder(r))
		writePipe(t, w, "\x1b[200~line one\n")
		time.Sleep(gap)
		writePipe(t, w, "y\x1b[201~q")
		w.Close()
		select {
		case keys := <-decoded:
			got := traceKeys(keys)
			if strings.Join(got, ",") != "paste-discarded,rune:q" {
				t.Errorf("gap %v: keys = %v, want [paste-discarded rune:q]", gap, got)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("gap %v: decoder did not finish after EOF", gap)
		}
		r.Close()
	}
}

// TestPasteTerminatorSplitAcrossGapStaysDiscarded covers the same guarantee
// when the ESC[201~ terminator itself straddles the pause.
func TestPasteTerminatorSplitAcrossGapStaysDiscarded(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	decoded := decodePipeKeys(NewDecoder(r))
	writePipe(t, w, "\x1b[200~y\x1b[20")
	time.Sleep(150 * time.Millisecond)
	writePipe(t, w, "1~q")
	w.Close()
	select {
	case keys := <-decoded:
		got := traceKeys(keys)
		if strings.Join(got, ",") != "paste-discarded,rune:q" {
			t.Errorf("keys = %v, want [paste-discarded rune:q]", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("decoder did not finish after EOF")
	}
}

// TestPasteCancelReturns is BF-08's exit: an open paste keeps discarding input,
// and closing the cancel channel (what Screen.Close does) makes ReadKey return
// within 100 ms instead of waiting for a terminator that never comes.
func TestPasteCancelReturns(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	cancel := make(chan struct{})
	dec := NewDecoderWithCancel(r, cancel)
	writePipe(t, w, "\x1b[200~abc")
	first, err := dec.ReadKey()
	if err != nil || first.Type != KeyPaste {
		t.Fatalf("first key = %v, %v; want paste-discarded", traceKeys([]Key{first}), err)
	}
	returned := make(chan error, 1)
	go func() {
		_, err := dec.ReadKey()
		returned <- err
	}()
	select {
	case err := <-returned:
		t.Fatalf("ReadKey returned %v inside an open paste, want it to keep discarding", err)
	case <-time.After(150 * time.Millisecond):
	}
	start := time.Now()
	close(cancel)
	select {
	case err := <-returned:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("ReadKey after cancel = %v, want io.EOF", err)
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("ReadKey took %v to return after cancel, want <= 100ms", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadKey did not return after cancel")
	}
}

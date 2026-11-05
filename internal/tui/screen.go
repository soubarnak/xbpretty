package tui

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/term"
)

// EventKind is the kind of event a Screen delivers.
type EventKind int

// The screen events the surface loops handle.
const (
	EventKey EventKind = iota
	EventResize
	EventSignal
	EventEOF
)

// Event is one screen event.
type Event struct {
	Kind   EventKind
	Key    term.Key
	Width  int
	Height int
	Signal os.Signal
}

// Screen is one full-screen session: it owns the restore guard, the signal
// wiring (SIGWINCH, SIGINT, SIGTERM, SIGHUP), the key decoder and the dirty
// buffer. Closing it restores the terminal in reverse order.
type Screen struct {
	In, Out *os.File
	Color   bool

	guard *term.Guard
	sigs  chan os.Signal
	done  chan struct{}
	event chan Event

	mu   sync.Mutex
	prev *Buffer
	w, h int

	closeOnce sync.Once
}

// Open enters raw mode and the alternate screen. The caller must Close it on
// every path; Run additionally restores on panic.
func Open(in, out *os.File, color bool, mouse bool) (*Screen, error) {
	guard := term.NewGuard(in, out)
	if err := guard.EnterFullscreen(mouse); err != nil {
		return nil, err
	}
	w, h, err := term.Size(out)
	if err != nil {
		w, h = 80, 24
	}
	s := &Screen{
		In:    in,
		Out:   out,
		Color: color,
		guard: guard,
		sigs:  make(chan os.Signal, 4),
		done:  make(chan struct{}),
		event: make(chan Event, 16),
		w:     w,
		h:     h,
	}
	signal.Notify(s.sigs, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go s.watchSignals()
	go s.readKeys()
	return s, nil
}

// Size is the last known terminal size.
func (s *Screen) Size() (w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w, s.h
}

// Events is the ordered event stream.
func (s *Screen) Events() <-chan Event { return s.event }

// Invalidate forces the next Draw to repaint the whole frame, which is what a
// resize needs (FR-071: clear before repaint).
func (s *Screen) Invalidate() {
	s.mu.Lock()
	s.prev = nil
	s.mu.Unlock()
}

// Draw flushes the buffer's diff to the terminal.
func (s *Screen) Draw(b *Buffer) error {
	s.mu.Lock()
	payload := b.Flush(s.prev, s.Color)
	s.prev = b
	s.w, s.h = b.W, b.H
	s.mu.Unlock()
	if len(payload) == 0 {
		return nil
	}
	_, err := s.Out.Write(payload)
	return err
}

// Run owns one surface loop: Close always runs, and a panic restores the
// terminal before it is re-raised (AC-054).
func (s *Screen) Run(loop func(*Screen) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.Close()
			panic(r)
		}
	}()
	defer s.Close()
	return loop(s)
}

// Close restores the terminal exactly once.
func (s *Screen) Close() {
	s.closeOnce.Do(func() {
		signal.Stop(s.sigs)
		close(s.done)
		s.guard.Restore()
	})
}

// Active reports whether the screen still owns the terminal.
func (s *Screen) Active() bool { return s.guard.Active() }

func (s *Screen) send(ev Event) bool {
	select {
	case s.event <- ev:
		return true
	case <-s.done:
		return false
	}
}

func (s *Screen) watchSignals() {
	for {
		select {
		case <-s.done:
			return
		case sig := <-s.sigs:
			if sig == syscall.SIGWINCH {
				w, h, err := term.Size(s.Out)
				if err != nil {
					continue
				}
				s.mu.Lock()
				s.w, s.h = w, h
				s.prev = nil
				s.mu.Unlock()
				if !s.send(Event{Kind: EventResize, Width: w, Height: h}) {
					return
				}
				continue
			}
			if !s.send(Event{Kind: EventSignal, Signal: sig}) {
				return
			}
		}
	}
}

func (s *Screen) readKeys() {
	dec := term.NewDecoder(s.In)
	for {
		key, err := dec.ReadKey()
		if err != nil {
			s.send(Event{Kind: EventEOF})
			return
		}
		if !s.send(Event{Kind: EventKey, Key: key}) {
			return
		}
	}
}

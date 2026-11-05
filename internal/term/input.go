package term

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// KeyType is the decoder's vocabulary.
type KeyType int

// The key events the surfaces consume.
const (
	KeyUnknown KeyType = iota
	KeyRune
	KeyCtrlC
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyDelete
	KeyWheelUp
	KeyWheelDown
	KeyPaste
	KeyDSR
)

// Key is one decoded event.
type Key struct {
	Type KeyType
	Rune rune
	Row  int // DSR reply row
	Col  int // DSR reply column
}

// String is the trace form the PTY harness asserts on.
func (k Key) String() string {
	switch k.Type {
	case KeyRune:
		return "rune:" + string(k.Rune)
	case KeyCtrlC:
		return "ctrl-c"
	case KeyEnter:
		return "enter"
	case KeyTab:
		return "tab"
	case KeyBackspace:
		return "backspace"
	case KeyEsc:
		return "esc"
	case KeyUp:
		return "up"
	case KeyDown:
		return "down"
	case KeyLeft:
		return "left"
	case KeyRight:
		return "right"
	case KeyHome:
		return "home"
	case KeyEnd:
		return "end"
	case KeyPgUp:
		return "pgup"
	case KeyPgDn:
		return "pgdn"
	case KeyDelete:
		return "delete"
	case KeyWheelUp:
		return "wheel-up"
	case KeyWheelDown:
		return "wheel-down"
	case KeyPaste:
		return "paste-discarded"
	case KeyDSR:
		return fmt.Sprintf("dsr:%d;%d", k.Row, k.Col)
	}
	return "unknown"
}

// Decoder turns terminal bytes into Key events (FR-072). It deliberately does
// no key-repeat filtering.
type Decoder struct {
	fd      uintptr
	f       *os.File
	reader  io.Reader
	pending []byte
	// cancel makes a blocking tty read abandonable: the screen closes this
	// channel before waiting for the decoder goroutine, so no surface can
	// keep consuming stdin after Close (FR-072).
	cancel <-chan struct{}
}

// NewDecoder wraps a tty.
func NewDecoder(f *os.File) *Decoder { return NewDecoderWithCancel(f, nil) }

// NewDecoderWithCancel wraps a tty and abandons a blocking read when cancel
// is closed. The zero-timeout poll is bounded by pollIntervalMS.
func NewDecoderWithCancel(f *os.File, cancel <-chan struct{}) *Decoder {
	d := &Decoder{f: f, reader: f, cancel: cancel}
	if f != nil {
		d.fd = f.Fd()
	}
	return d
}

// newReaderDecoder is the in-memory seam used by the unit tests. Production
// input always comes through NewDecoder.
func newReaderDecoder(r io.Reader) *Decoder { return &Decoder{reader: r} }

// ReadKey blocks for one event.
func (d *Decoder) ReadKey() (Key, error) { return d.readKey() }

func (d *Decoder) readKey() (Key, error) {
	b, ok, err := d.nextByte(-1)
	if err != nil {
		return Key{}, err
	}
	if !ok {
		return Key{}, io.EOF
	}
	if b == 0x1b {
		return d.readEsc()
	}
	return d.controlOrRune(b)
}

// pollIntervalMS bounds how long a blocking read waits before re-checking the
// cancel channel.
const pollIntervalMS = 50

func (d *Decoder) cancelled() bool {
	if d.cancel == nil {
		return false
	}
	select {
	case <-d.cancel:
		return true
	default:
		return false
	}
}

func (d *Decoder) nextByte(timeoutMS int) (byte, bool, error) {
	if len(d.pending) > 0 {
		b := d.pending[0]
		d.pending = d.pending[1:]
		return b, true, nil
	}
	if d.cancelled() {
		return 0, false, io.EOF
	}
	if d.f == nil {
		var buf [1]byte
		n, err := d.reader.Read(buf[:])
		if n == 1 {
			return buf[0], true, nil
		}
		if err == nil {
			err = io.EOF
		}
		return 0, false, err
	}
	if timeoutMS < 0 {
		var buf [1]byte
		for {
			if d.cancelled() {
				return 0, false, io.EOF
			}
			ready, err := waitReadable(d.fd, pollIntervalMS)
			if err != nil {
				return 0, false, err
			}
			if !ready {
				continue
			}
			if d.cancelled() {
				return 0, false, io.EOF
			}
			n, err := readFD(d.f, buf[:])
			if n == 1 {
				return buf[0], true, nil
			}
			if err == nil {
				continue
			}
			if isRetryableRead(err) {
				continue
			}
			return 0, false, err
		}
	}
	ready, err := waitReadable(d.fd, timeoutMS)
	if err != nil || !ready {
		return 0, false, nil
	}
	if d.cancelled() {
		return 0, false, io.EOF
	}
	var chunk [256]byte
	n, readErr := readFD(d.f, chunk[:])
	if n <= 0 {
		if readErr != nil && !isRetryableRead(readErr) {
			return 0, false, readErr
		}
		return 0, false, io.EOF
	}
	d.pending = append(d.pending, chunk[:n]...)
	b := d.pending[0]
	d.pending = d.pending[1:]
	return b, true, nil
}

func (d *Decoder) controlOrRune(b byte) (Key, error) {
	switch b {
	case 3:
		return Key{Type: KeyCtrlC}, nil
	case 9:
		return Key{Type: KeyTab}, nil
	case 13, 10:
		return Key{Type: KeyEnter}, nil
	case 127, 8:
		return Key{Type: KeyBackspace}, nil
	}
	if b < 0x20 {
		return Key{Type: KeyUnknown}, nil
	}
	if b < 0x80 {
		return Key{Type: KeyRune, Rune: rune(b)}, nil
	}
	buf := []byte{b}
	want := 1
	switch {
	case b&0xe0 == 0xc0:
		want = 2
	case b&0xf0 == 0xe0:
		want = 3
	case b&0xf8 == 0xf0:
		want = 4
	}
	for len(buf) < want {
		nb, ok, _ := d.nextByte(25)
		if !ok {
			break
		}
		buf = append(buf, nb)
	}
	r, _ := utf8.DecodeRune(buf)
	return Key{Type: KeyRune, Rune: r}, nil
}

func (d *Decoder) readEsc() (Key, error) {
	b, ok, _ := d.nextByte(25)
	if !ok {
		return Key{Type: KeyEsc}, nil
	}
	switch b {
	case '[':
		return d.readCSI()
	case 'O':
		nb, ok, _ := d.nextByte(25)
		if !ok {
			return Key{Type: KeyEsc}, nil
		}
		switch nb {
		case 'A':
			return Key{Type: KeyUp}, nil
		case 'B':
			return Key{Type: KeyDown}, nil
		case 'C':
			return Key{Type: KeyRight}, nil
		case 'D':
			return Key{Type: KeyLeft}, nil
		case 'H':
			return Key{Type: KeyHome}, nil
		case 'F':
			return Key{Type: KeyEnd}, nil
		}
		return Key{Type: KeyUnknown}, nil
	default:
		// A lone ESC followed by a normal key: return Esc, keep the byte so
		// the next ReadKey decodes it instead of swallowing it.
		d.pending = append([]byte{b}, d.pending...)
		return Key{Type: KeyEsc}, nil
	}
}

func (d *Decoder) readCSI() (Key, error) {
	var seq []byte
	for {
		b, ok, _ := d.nextByte(25)
		if !ok {
			return Key{Type: KeyUnknown}, nil
		}
		seq = append(seq, b)
		if b >= 0x40 && b <= 0x7e {
			break
		}
		if len(seq) > 32 {
			return Key{Type: KeyUnknown}, nil
		}
	}
	final := seq[len(seq)-1]
	body := string(seq[:len(seq)-1])
	switch final {
	case 'A':
		return Key{Type: KeyUp}, nil
	case 'B':
		return Key{Type: KeyDown}, nil
	case 'C':
		return Key{Type: KeyRight}, nil
	case 'D':
		return Key{Type: KeyLeft}, nil
	case 'H':
		return Key{Type: KeyHome}, nil
	case 'F':
		return Key{Type: KeyEnd}, nil
	case 'R':
		parts := strings.Split(body, ";")
		if len(parts) == 2 {
			r, _ := strconv.Atoi(parts[0])
			c, _ := strconv.Atoi(parts[1])
			return Key{Type: KeyDSR, Row: r, Col: c}, nil
		}
		return Key{Type: KeyUnknown}, nil
	case 'M', 'm':
		// SGR mouse: ESC[<b;x;yM
		if strings.HasPrefix(body, "<") {
			parts := strings.Split(body[1:], ";")
			if len(parts) == 3 && final == 'M' {
				btn, _ := strconv.Atoi(parts[0])
				if btn&64 != 0 {
					if btn&1 == 0 {
						return Key{Type: KeyWheelUp}, nil
					}
					return Key{Type: KeyWheelDown}, nil
				}
			}
		}
		return Key{Type: KeyUnknown}, nil
	case '~':
		switch body {
		case "1", "7":
			return Key{Type: KeyHome}, nil
		case "4", "8":
			return Key{Type: KeyEnd}, nil
		case "5":
			return Key{Type: KeyPgUp}, nil
		case "6":
			return Key{Type: KeyPgDn}, nil
		case "3":
			return Key{Type: KeyDelete}, nil
		case "200":
			d.discardPaste()
			return Key{Type: KeyPaste}, nil
		}
		return Key{Type: KeyUnknown}, nil
	}
	return Key{Type: KeyUnknown}, nil
}

// discardPaste consumes until ESC[201~; the content is deliberately thrown
// away so a pasted "y" cannot confirm a transaction (FR-072).
func (d *Decoder) discardPaste() {
	var window []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.cancelled() {
			return
		}
		b, ok, _ := d.nextByte(100)
		if !ok {
			return
		}
		window = append(window, b)
		if len(window) > 6 {
			window = window[len(window)-6:]
		}
		if string(window) == "\x1b[201~" {
			return
		}
	}
}

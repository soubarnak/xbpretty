package term

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// KeyType is the decoder's vocabulary.
type KeyType int

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
	Row  int // DSR reply
	Col  int
}

// String is the trace form used by the PTY harness.
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

// Decoder turns terminal bytes into Key events (#50 §8.6). It deliberately
// does no key-repeat filtering.
type Decoder struct {
	fd      uintptr
	in      *os.File
	pending []byte
}

// NewDecoder wraps a tty fd.
func NewDecoder(f *os.File) *Decoder {
	return &Decoder{fd: f.Fd(), in: f}
}

// Push injects bytes ahead of the stream (e.g. stray bytes read during DSR).
func (d *Decoder) Push(b []byte) {
	if len(b) == 0 {
		return
	}
	d.pending = append(append([]byte(nil), b...), d.pending...)
}

func (d *Decoder) nextByte(timeoutMS int) (byte, bool) {
	if len(d.pending) > 0 {
		b := d.pending[0]
		d.pending = d.pending[1:]
		return b, true
	}
	if timeoutMS < 0 {
		var buf [1]byte
		for {
			n, err := syscall.Read(int(d.fd), buf[:])
			if n == 1 {
				return buf[0], true
			}
			if err == syscall.EINTR || err == syscall.EAGAIN {
				continue
			}
			return 0, false
		}
	}
	ready, err := Select(d.fd, timeoutMS)
	if err != nil || !ready {
		return 0, false
	}
	chunk, err := ReadAvailable(d.fd)
	if err != nil || len(chunk) == 0 {
		return 0, false
	}
	d.pending = append(d.pending, chunk...)
	b := d.pending[0]
	d.pending = d.pending[1:]
	return b, true
}

// ReadKey blocks for one event.
func (d *Decoder) ReadKey() (Key, error) {
	b, ok := d.nextByte(-1)
	if !ok {
		return Key{}, fmt.Errorf("stdin closed")
	}
	if b == 0x1b {
		return d.readEsc()
	}
	return d.controlOrRune(b)
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
	// UTF-8: collect continuation bytes.
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
		nb, ok := d.nextByte(25)
		if !ok {
			break
		}
		buf = append(buf, nb)
	}
	return Key{Type: KeyRune, Rune: []rune(string(buf))[0]}, nil
}

func (d *Decoder) readEsc() (Key, error) {
	b, ok := d.nextByte(25)
	if !ok {
		return Key{Type: KeyEsc}, nil
	}
	switch b {
	case '[':
		return d.readCSI()
	case 'O':
		nb, ok := d.nextByte(25)
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
		// A lone ESC followed by a normal key: return Esc, keep the byte.
		d.pending = append([]byte{b}, d.pending...)
		return Key{Type: KeyEsc}, nil
	}
}

func (d *Decoder) readCSI() (Key, error) {
	var seq []byte
	for {
		b, ok := d.nextByte(25)
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
			if len(parts) == 3 {
				b, _ := strconv.Atoi(parts[0])
				if final == 'M' {
					if b&64 != 0 {
						if b&1 == 0 {
							return Key{Type: KeyWheelUp}, nil
						}
						return Key{Type: KeyWheelDown}, nil
					}
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

// discardPaste consumes until ESC[201~ (bracketed paste); the content is
// deliberately thrown away so a pasted "y" cannot confirm a transaction.
func (d *Decoder) discardPaste() {
	var window []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, ok := d.nextByte(100)
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

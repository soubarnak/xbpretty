//go:build linux

// Package term is the throwaway zero-dependency terminal engine prototype for
// #52. It is deliberately rough: enough primitives to answer "is a stdlib Go
// full-screen TUI faithful enough to nala's shell?".
package term

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Winsize mirrors struct winsize from <termios.h>.
type Winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// IsTTY reports whether f is a terminal (TCGETS succeeds).
func IsTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	var t syscall.Termios
	return ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

// Size returns the terminal size, falling back to 80x24.
func Size(f *os.File) (w, h int, err error) {
	if f == nil {
		return 80, 24, errors.New("nil file")
	}
	var ws Winsize
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 80, 24, err
	}
	if ws.Col == 0 || ws.Row == 0 {
		return 80, 24, errors.New("zero winsize")
	}
	return int(ws.Col), int(ws.Row), nil
}

// SetSize sets the terminal size (used by tests through a pty).
func SetSize(f *os.File, w, h int) error {
	ws := Winsize{Row: uint16(h), Col: uint16(w)}
	return ioctl(f.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// GetTermios returns the current termios for f.
func GetTermios(f *os.File) (syscall.Termios, error) {
	var t syscall.Termios
	err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t))
	return t, err
}

// SetTermios installs t on f immediately.
func SetTermios(f *os.File, t syscall.Termios) error {
	return ioctl(f.Fd(), syscall.TCSETS, unsafe.Pointer(&t))
}

// rawTermios is §8.3's raw mode: everything off, ISIG included, 8-bit clean.
func rawTermios(t syscall.Termios) syscall.Termios {
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	return t
}

// cbreakTermios is §7.3: no canonical, no echo, no IEXTEN, but ISIG and OPOST
// stay on so Ctrl-C reaches the signal handler and output still gets \n -> \r\n.
func cbreakTermios(t syscall.Termios) syscall.Termios {
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.IEXTEN
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	return t
}

// Select waits until fd is readable or the timeout expires.
func Select(fd uintptr, timeoutMS int) (bool, error) {
	var set syscall.FdSet
	set.Bits[fd/64] |= 1 << (fd % 64)
	tv := syscall.Timeval{Sec: int64(timeoutMS / 1000), Usec: int64((timeoutMS % 1000) * 1000)}
	n, err := syscall.Select(int(fd)+1, &set, nil, nil, &tv)
	if err != nil {
		if err == syscall.EINTR {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

// ReadAvailable does one read from fd, expected to be readable already.
func ReadAvailable(fd uintptr) ([]byte, error) {
	var buf [256]byte
	n, err := syscall.Read(int(fd), buf[:])
	if n <= 0 {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

// ANSI primitives, exact bytes from #50 §8.3.

const (
	AltEnter   = "\x1b[?1049h"
	AltLeave   = "\x1b[?1049l"
	CursorHide = "\x1b[?25l"
	CursorShow = "\x1b[?25h"
	ClearAll   = "\x1b[2J\x1b[H"
	ClearDown  = "\x1b[0J"
	MouseOn    = "\x1b[?1000h\x1b[?1006h"
	MouseOff   = "\x1b[?1000l\x1b[?1006l"
	PasteOn    = "\x1b[?2004h"
	PasteOff   = "\x1b[?2004l"
	DSRQuery   = "\x1b[6n"
)

// MoveTo emits a 1-based absolute cursor move.
func MoveTo(row, col int) string { return fmt.Sprintf("\x1b[%d;%dH", row, col) }

// MoveUp emits a relative cursor-up.
func MoveUp(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dA", n)
}

// MoveDown emits a relative cursor-down.
func MoveDown(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dB", n)
}

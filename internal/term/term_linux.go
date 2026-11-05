//go:build linux

package term

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Winsize mirrors struct winsize from <termios.h>.
type Winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// State is the saved Linux termios. Equal compares every field, which is how
// AC-054 proves the flags came back exactly.
type State struct {
	t syscall.Termios
}

// Equal reports whether two saved states are identical.
func (s State) Equal(other State) bool { return s.t == other.t }

var errUnsupported = errors.New("terminal control is not supported on this platform")

// isTTY reports whether f is a terminal (TCGETS succeeds).
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	var t syscall.Termios
	return ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

// GetState returns the current termios for f.
func GetState(f *os.File) (State, error) {
	var t syscall.Termios
	err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t))
	return State{t: t}, err
}

// SetState installs s on f immediately.
func SetState(f *os.File, s State) error {
	return ioctl(f.Fd(), syscall.TCSETS, unsafe.Pointer(&s.t))
}

// rawState is ADR-0017's raw mode: everything off, ISIG included, 8-bit clean.
func rawState(s State) State {
	t := s.t
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	return State{t: t}
}

// cbreakState is the inline viewport's mode: no canonical, no echo, no
// IEXTEN, but ISIG and OPOST stay on so Ctrl-C is a real signal and output
// still translates LF to CRLF (FR-072).
func cbreakState(s State) State {
	t := s.t
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.IEXTEN
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	return State{t: t}
}

// Size returns the terminal size. A terminal that reports zero cells is not
// usable, so the caller sees the error as well as the 80x24 fallback.
func Size(f *os.File) (w, h int, err error) {
	if f == nil {
		return 80, 24, errors.New("term: nil file")
	}
	var ws Winsize
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 80, 24, err
	}
	if ws.Col == 0 || ws.Row == 0 {
		return 80, 24, errors.New("term: zero winsize")
	}
	return int(ws.Col), int(ws.Row), nil
}

// SetSize sets the terminal size. Tests use it through a pty.
func SetSize(f *os.File, w, h int) error {
	if f == nil {
		return errors.New("term: nil file")
	}
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

// waitReadable waits until fd is readable or the timeout expires.
func waitReadable(fd uintptr, timeoutMS int) (bool, error) {
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

// readFD reads whatever is available from fd.
func readFD(fd uintptr, buf []byte) (int, error) {
	return syscall.Read(int(fd), buf)
}

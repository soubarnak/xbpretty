//go:build !linux

package term

import (
	"errors"
	"os"
)

// State is an opaque placeholder on platforms without the Linux termios
// calls. Equal is always true because there is nothing to change.
type State struct{}

// Equal reports whether two saved states are identical.
func (s State) Equal(other State) bool { return true }

var errUnsupported = errors.New("terminal control is not supported on this platform")

func isTTY(f *os.File) bool { return false }

// GetState returns ErrUnsupported: the surfaces fall back to text.
func GetState(f *os.File) (State, error) { return State{}, errUnsupported }

// SetState returns ErrUnsupported.
func SetState(f *os.File, s State) error { return errUnsupported }

func rawState(s State) State    { return s }
func cbreakState(s State) State { return s }

// Size returns ErrUnsupported with the 80x24 fallback.
func Size(f *os.File) (w, h int, err error) { return 80, 24, errUnsupported }

// SetSize returns ErrUnsupported.
func SetSize(f *os.File, w, h int) error { return errUnsupported }

// Winsize mirrors the Linux type so callers can name it everywhere.
type Winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

func waitReadable(fd uintptr, timeoutMS int) (bool, error) { return false, errUnsupported }

func readFD(fd uintptr, buf []byte) (int, error) {
	f := os.NewFile(fd, "term-input")
	if f == nil {
		return 0, errUnsupported
	}
	return f.Read(buf)
}

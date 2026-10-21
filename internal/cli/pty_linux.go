//go:build linux

package cli

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

type winsize struct {
	ws_row    uint16
	ws_col    uint16
	ws_xpixel uint16
	ws_ypixel uint16
}

// openPty allocates a master and slave pseudo-terminal using pure standard library
// syscalls on Linux (/dev/ptmx, ioctl TIOCGPTN and TIOCSPTLCK).
func openPty() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	var ptn uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptn))); errno != 0 {
		master.Close()
		return nil, nil, fmt.Errorf("ioctl TIOCGPTN: %w", errno)
	}

	var unlock int32 = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		return nil, nil, fmt.Errorf("ioctl TIOCSPTLCK: %w", errno)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", ptn)
	slave, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("open pty slave %s: %w", slavePath, err)
	}

	return master, slave, nil
}

// inheritPtySize copies the window size from src to dst.
func inheritPtySize(src, dst *os.File) error {
	var ws winsize
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, src.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return errno
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, dst.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return errno
	}
	return nil
}

// terminalWidth returns the terminal column width of w using ioctl TIOCGWINSZ.
// If w is not an *os.File, is not a TTY, or the ioctl fails, it falls back to 80.
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 80
	}
	var ws winsize
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return 80
	}
	if ws.ws_col == 0 {
		return 80
	}
	return int(ws.ws_col)
}

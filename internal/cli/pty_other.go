//go:build !linux

package cli

import (
	"errors"
	"io"
	"os"
)

func openPty() (*os.File, *os.File, error) {
	return nil, nil, errors.New("pty not supported on non-linux systems")
}

func inheritPtySize(src, dst *os.File) error {
	return errors.New("pty not supported on non-linux systems")
}

// terminalWidth has no ioctl on this platform, so callers fall back to
// COLUMNS and then 80.
func terminalWidth(w io.Writer) int {
	return 0
}

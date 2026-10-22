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

func terminalWidth(w io.Writer) int {
	return 80
}

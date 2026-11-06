//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package cli

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// isBrokenPipe reports the EPIPE write error that a closed stdout produces.
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

// ignoreSIGPIPE keeps a closed stdout pipe from killing the process, so the
// write above can see EPIPE, stop writing and let the transaction finish
// (FR-074). SIGPIPE is caught rather than ignored, so a child still gets the
// default disposition across exec.
func ignoreSIGPIPE() {
	sigpipeOnce.Do(func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGPIPE)
		go func() {
			for range sigs {
			}
		}()
	})
}

var sigpipeOnce sync.Once

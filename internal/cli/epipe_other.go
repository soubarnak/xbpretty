//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package cli

// isBrokenPipe has no platform-specific error on this build.
func isBrokenPipe(error) bool { return false }

// ignoreSIGPIPE is a no-op where SIGPIPE is not part of the signal set.
func ignoreSIGPIPE() {}

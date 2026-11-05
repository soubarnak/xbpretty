//go:build !xbpretty_testroot

package cli

// This file exists so the build-tagged euid hook in root_testhook.go is
// visibly absent from ordinary builds: the production binary always asks
// os.Geteuid before running a root-gated step.

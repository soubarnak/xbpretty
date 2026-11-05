//go:build !xbpretty_testroot

package cli

// This file exists so the build-tagged root hook in history_testhook.go is
// visibly absent from ordinary builds: the production binary always uses
// os.Geteuid for the non-root guard.

//go:build !xbpretty_testroot

package cli

// This file exists so the build-tagged test hook in root_testhook.go is
// visibly absent from ordinary builds: the production binary always uses
// os.Geteuid for the privilege checks.

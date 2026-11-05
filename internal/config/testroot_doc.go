//go:build !xbpretty_testroot

package config

// This file exists so the build-tagged test hook in testroot_testhook.go is
// visibly absent from ordinary builds: the production binary always uses
// os.Geteuid, uid 0 as the only trusted owner, /etc/xbpretty/config.toml as the
// system layer and the real passwd lookup.

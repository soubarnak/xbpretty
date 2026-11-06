//go:build !xbpretty_testroot

package cli

// This file exists so the build-tagged clean hook in clean_testhook.go is
// visibly absent from ordinary builds: the production binary always cleans
// cached repodata under /var/db/xbps.

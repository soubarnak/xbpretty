package config

import (
	"strings"
	"testing"
)

// parityNoAcquireProxyConfig proves P-BEH-037: Acquire::* proxy keys never
// parse. Proxy selection is the Go HTTP stack's environment handling, not a
// config surface.
func parityNoAcquireProxyConfig(t *testing.T) {
	_, err := Parse("test.conf", strings.NewReader("[Acquire]\nproxy = \"http://proxy:3128\"\n"))
	if err == nil {
		t.Fatal("[Acquire] parsed; Acquire::* keys must be rejected")
	}
	if got, want := err.Error(), "config test.conf:1: unknown table [Acquire]"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

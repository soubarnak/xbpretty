package cli

import (
	"fmt"
	"os"
	"testing"
)

// TestRequestedByPrefersInvokingUID pins FR-030's requested_by: SUDO_UID and
// DOAS_UID are authoritative, with the passwd lookup only a fallback.
func TestRequestedByPrefersInvokingUID(t *testing.T) {
	t.Setenv("SUDO_USER", "someone")
	t.Setenv("SUDO_UID", "4242")
	if got, want := requestedBy(), "someone (4242)"; got != want {
		t.Errorf("requestedBy = %q, want %q", got, want)
	}

	t.Setenv("SUDO_USER", "")
	t.Setenv("SUDO_UID", "")
	t.Setenv("DOAS_USER", "other")
	t.Setenv("DOAS_UID", "7")
	if got, want := requestedBy(), "other (7)"; got != want {
		t.Errorf("requestedBy = %q, want %q", got, want)
	}

	t.Setenv("DOAS_USER", "")
	t.Setenv("DOAS_UID", "")
	t.Setenv("LOGNAME", "alice")
	t.Setenv("USER", "bob")
	if got, want := requestedBy(), fmt.Sprintf("alice (%d)", os.Getuid()); got != want {
		t.Errorf("requestedBy = %q, want %q", got, want)
	}
}

// TestEffectiveForceFromPassthrough pins the effective -f rule: a purge verb,
// --purge, or a passthrough flag cluster containing f all force.
func TestEffectiveForceFromPassthrough(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		opts *options
		want bool
	}{
		{"purge verb", "purge", &options{}, true},
		{"remove --purge", "remove", &options{purge: true}, true},
		{"remove plain", "remove", &options{}, false},
		{"remove -- -f", "remove", &options{passthru: []string{"-f"}}, true},
		{"remove -- -fy", "remove", &options{passthru: []string{"-fy"}}, true},
		{"autoremove -- -f", "autoremove", &options{passthru: []string{"-f"}}, true},
		{"long flag does not count", "remove", &options{passthru: []string{"--force"}}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveForce(tt.cmd, tt.opts); got != tt.want {
				t.Errorf("effectiveForce = %v, want %v", got, tt.want)
			}
		})
	}
}

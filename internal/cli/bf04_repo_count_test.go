package cli

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
)

// repoCountState builds a state whose query binary is the stub xbps-query.
// The stub answers `-L` from XB_STUB_REPOS, so the test controls the listing.
func repoCountState(t *testing.T) *state {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Xbps.QueryPath = filepath.Join(stubDir, "xbps-query")
	t.Setenv("XB_STUB_LOG", filepath.Join(t.TempDir(), "calls.log"))
	return &state{
		r: &runner{cfg: cfg, out: io.Discard, stdout: io.Discard, stderr: io.Discard},
	}
}

// TestRepositoryCountFromQueryL is BF-04's main path: xbps-query -L lists the
// repositories xbps will sync. Local entries and duplicates are not remote
// repositories, so five remote plus two local lines count as five.
func TestRepositoryCountFromQueryL(t *testing.T) {
	st := repoCountState(t)
	t.Setenv("XB_STUB_REPOS", ""+
		" 0 https://repo-default.voidlinux.org/current (RSA signed)\n"+
		" 1 https://repo-default.voidlinux.org/current/nonfree (RSA signed)\n"+
		" 2 https://mirror.example/void/current (RSA signed)\n"+
		" 3 https://mirror.example/void/current/multilib (unsigned)\n"+
		" 4 https://dms.example/repo (RSA signed)\n"+
		" 5 file:///home/me/local-repo (unsigned)\n"+
		" 6 /srv/packages (unsigned)\n"+
		" 7 https://dms.example/repo (RSA signed)\n")
	if got, want := st.repositoryTotal(), 5; got != want {
		t.Errorf("repositoryTotal() = %d, want %d", got, want)
	}
}

// TestRepositoryCountFallbackReadsAllConf: when xbps-query -L prints nothing,
// the count comes from every xbps.d .conf, not only *-repository-*.conf. An
// /etc file masks a same-named /usr/share file, and local URLs do not count.
func TestRepositoryCountFallbackReadsAllConf(t *testing.T) {
	st := repoCountState(t)
	t.Setenv("XB_STUB_REPOS", "")

	etc := t.TempDir()
	usr := t.TempDir()
	writeConf(t, etc, "20-foo.conf", "repository=https://foo.example/current\n")
	writeConf(t, usr, "20-foo.conf", "repository=https://masked.example/current\n")
	writeConf(t, usr, "00-repository-main.conf", "repository=https://repo-default.voidlinux.org/current\n")
	writeConf(t, usr, "99-local.conf", "repository=/srv/local\n")

	orig := fetchXBPSDDirs
	fetchXBPSDDirs = []string{etc, usr}
	t.Cleanup(func() { fetchXBPSDDirs = orig })

	if got, want := st.repositoryTotal(), 2; got != want {
		t.Errorf("fallback repositoryTotal() = %d, want %d", got, want)
	}
}

// TestRepositoryCountPassesRepoFlagsToQueryL: a -R/-r/-C passthrough reaches
// xbps-query -L, so the count follows the repositories the transaction uses.
func TestRepositoryCountPassesRepoFlagsToQueryL(t *testing.T) {
	st := repoCountState(t)
	st.opts = &options{passthru: []string{"-r", "/mnt"}}
	t.Setenv("XB_STUB_REPOS", " 0 https://only.example/current (RSA signed)\n")
	if got, want := st.repositoryTotal(), 1; got != want {
		t.Errorf("repositoryTotal() = %d, want %d", got, want)
	}
	calls, err := os.ReadFile(os.Getenv("XB_STUB_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "xbps-query -L -r /mnt"; !slices.Contains(strings.Split(string(calls), "\n"), want) {
		t.Errorf("xbps-query call log = %q, want a line %q", calls, want)
	}
}

func writeConf(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

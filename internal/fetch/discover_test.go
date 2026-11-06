package fetch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

func writeRepoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAndClassifyHonoursMaskingGlobAndArchitecture(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	usr := filepath.Join(root, "usr")

	writeRepoFile(t, etc, "xbps-arch.conf", "architecture=x86_64-musl\n")
	writeRepoFile(t, etc, "00-repository-main.conf", strings.Join([]string{
		"# packaged default",
		"repository=https://repo-default.voidlinux.org/current/",
		"ignorepkg=foo",
		"",
	}, "\n"))
	// Not a *-repository-*.conf file: its repository= line is not fetch's to
	// manage or probe.
	writeRepoFile(t, etc, "20-extra.conf", "repository=https://custom.example/void\n")

	// The /etc 00-repository-main.conf masks the /usr/share one, while the
	// unmasked multilib and custom files are still read.
	writeRepoFile(t, usr, "xbps-arch.conf", "architecture=aarch64\n")
	writeRepoFile(t, usr, "00-repository-main.conf", "repository=https://masked.example/current\n")
	writeRepoFile(t, usr, "10-repository-multilib.conf", "repository=https://repo-default.voidlinux.org/current/multilib\n")
	writeRepoFile(t, usr, "10-repository-custom.conf", "repository=https://custom.example/void\n")

	cfg, err := fetch.DiscoverRepositories([]string{etc, usr})
	if err != nil {
		t.Fatalf("DiscoverRepositories: %v", err)
	}
	if cfg.Arch != "x86_64-musl" {
		t.Errorf("Arch = %q, want the masked /etc value x86_64-musl", cfg.Arch)
	}
	if len(cfg.Lines) != 3 {
		t.Fatalf("lines = %+v, want main, multilib and custom (masked main and non-glob file ignored)", cfg.Lines)
	}
	for _, line := range cfg.Lines {
		if strings.Contains(line.Value, "masked.example") {
			t.Errorf("masked /usr/share file contributed a line: %+v", line)
		}
		if strings.Contains(line.Value, "20-extra") {
			t.Errorf("non-repository file contributed a line: %+v", line)
		}
	}

	set, err := fetch.ClassifyRepositories(cfg, []string{"repo-fastly.voidlinux.org"}, nil)
	if err != nil {
		t.Fatalf("ClassifyRepositories: %v", err)
	}
	if set.Primary != "/current" {
		t.Errorf("Primary = %q, want /current", set.Primary)
	}
	wantSuffixes := "/current,/current/multilib"
	if strings.Join(set.Suffixes, ",") != wantSuffixes {
		t.Errorf("Suffixes = %v, want %s", set.Suffixes, wantSuffixes)
	}
	if len(set.URLs) != 2 {
		t.Errorf("URLs = %v, want the two managed repositories", set.URLs)
	}
	for _, line := range set.Lines {
		custom := strings.Contains(line.Value, "custom.example")
		if custom && line.Managed {
			t.Errorf("custom repository was classified managed: %+v", line)
		}
		if !custom && !line.Managed {
			t.Errorf("default repository was not classified managed: %+v", line)
		}
	}
}

func TestClassifyPreviousChosenHostAndLocalPaths(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "10-repository-custom.conf", strings.Join([]string{
		"repository=https://repo-de.voidlinux.org/current",
		"repository=https://custom.example/void",
		"repository=file:///var/cache/local",
		"repository=/srv/xbps",
		"",
	}, "\n"))
	cfg, err := fetch.DiscoverRepositories([]string{root})
	if err != nil {
		t.Fatalf("DiscoverRepositories: %v", err)
	}
	// repo-de is not in the live list here; it is the previous chosen host.
	set, err := fetch.ClassifyRepositories(cfg, nil, []string{"https://repo-de.voidlinux.org"})
	if err != nil {
		t.Fatalf("ClassifyRepositories: %v", err)
	}
	if set.Primary != "/current" || len(set.Suffixes) != 1 {
		t.Errorf("set = %+v, want only the previous chosen repository", set)
	}
	if len(set.Lines) != 4 {
		t.Errorf("lines = %d, want all preserved lines", len(set.Lines))
	}
}

func TestDiscoverRepositoriesNoRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	_, err := fetch.DiscoverRepositories([]string{root, filepath.Join(root, "missing")})
	if err == nil {
		t.Fatal("DiscoverRepositories accepted a scan with no repository= line")
	}
	if err.Error() != "no xbps repository configuration found under /usr/share/xbps.d" {
		t.Errorf("error = %q, want the exact dec:#48 §3.1 message", err)
	}
}

func TestClassifyRepositoriesNoManagedRepository(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "10-repository-custom.conf", strings.Join([]string{
		"repository=https://custom.example/void",
		"repository=file:///var/cache/local",
		"",
	}, "\n"))
	cfg, err := fetch.DiscoverRepositories([]string{root})
	if err != nil {
		t.Fatalf("DiscoverRepositories: %v", err)
	}
	_, err = fetch.ClassifyRepositories(cfg, []string{"repo-fi.voidlinux.org"}, nil)
	if err == nil {
		t.Fatal("ClassifyRepositories accepted a custom-only repository set")
	}
	if err.Error() != "no Void mirror repository found to rewrite" {
		t.Errorf("error = %q, want the exact dec:#48 §3.1 message", err)
	}
}

func TestDiscoveredArchitectureFallsBackOnlyWhenAbsent(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "00-repository-main.conf", "repository=https://repo-default.voidlinux.org/current\n")
	cfg, err := fetch.DiscoverRepositories([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Arch != "" {
		t.Errorf("Arch = %q, want empty so the CLI applies the GOARCH fallback", cfg.Arch)
	}
}

func TestNonFreeSuffixes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		suffixes []string
		want     []string
	}{
		{"primary only", []string{"/current"}, []string{"/current/nonfree"}},
		{"multilib configured", []string{"/current", "/current/multilib"}, []string{"/current/nonfree", "/current/multilib/nonfree"}},
		{"musl primary", []string{"/current/musl"}, []string{"/current/musl/nonfree"}},
		{"multilib not primary", []string{"/current/musl", "/current/multilib"}, []string{"/current/musl/nonfree", "/current/multilib/nonfree"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := fetch.NonFreeSuffixes(tt.suffixes)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("NonFreeSuffixes(%v) = %v, want %v", tt.suffixes, got, tt.want)
			}
		})
	}
}

func TestClassifyRepositoriesUsesTheMirrorBasePath(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "10-repository-main.conf", strings.Join([]string{
		"repository=https://mirror.example/one/current",
		"repository=https://mirror.example/one/current/multilib",
		"",
	}, "\n"))
	cfg, err := fetch.DiscoverRepositories([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	set, err := fetch.ClassifyRepositories(cfg, []string{"https://mirror.example/one"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(set.Suffixes, ",") != "/current,/current/multilib" {
		t.Errorf("Suffixes = %v, want suffixes relative to the mirror's /one base", set.Suffixes)
	}
	if set.Primary != "/current" {
		t.Errorf("Primary = %q, want /current", set.Primary)
	}

	// The previous manifest's full URL is enough when the live list moved on.
	set, err = fetch.ClassifyRepositories(cfg, []string{"https://other.example"}, []string{"https://mirror.example/one"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(set.Suffixes, ",") != "/current,/current/multilib" {
		t.Errorf("previous-mirror Suffixes = %v", set.Suffixes)
	}
}

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

func TestDiscoverRepositoriesReadsEtcFirstAndSortsFiles(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	usr := filepath.Join(root, "usr")
	writeRepoFile(t, etc, "20-extra.conf", strings.Join([]string{
		"# comment",
		"repository=https://mirror.example/current/multilib",
		"ignorepkg=foo",
		"",
	}, "\n"))
	writeRepoFile(t, etc, "00-main.conf", strings.Join([]string{
		"repository=https://mirror.example/current/",
		"repository=https://mirror.example/current/musl/",
		"repository=file:///var/cache/local",
		"",
	}, "\n"))
	writeRepoFile(t, usr, "00-main.conf", "repository=https://repo-default.voidlinux.org/current\n")

	set, err := fetch.DiscoverRepositories([]string{etc, usr})
	if err != nil {
		t.Fatalf("DiscoverRepositories: %v", err)
	}
	if set.Primary != "/current" {
		t.Errorf("Primary = %q, want /current", set.Primary)
	}
	wantSuffixes := []string{"/current", "/current/musl", "/current/multilib"}
	if strings.Join(set.Suffixes, ",") != strings.Join(wantSuffixes, ",") {
		t.Errorf("Suffixes = %v, want %v", set.Suffixes, wantSuffixes)
	}
	if len(set.URLs) != 3 {
		t.Errorf("URLs = %v, want the three remote repositories", set.URLs)
	}
}

func TestDiscoverRepositoriesEmptyEtcFallsBackToUsrShare(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	usr := filepath.Join(root, "usr")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, usr, "00-repository-main.conf", "repository=https://repo-default.voidlinux.org/current\n")

	set, err := fetch.DiscoverRepositories([]string{etc, usr})
	if err != nil {
		t.Fatalf("DiscoverRepositories: %v", err)
	}
	if set.Primary != "/current" {
		t.Errorf("Primary = %q, want /current", set.Primary)
	}
}

func TestDiscoverRepositoriesNoRemoteRepository(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "00-main.conf", "repository=file:///var/cache/local\n")
	_, err := fetch.DiscoverRepositories([]string{root})
	if err == nil {
		t.Fatal("DiscoverRepositories accepted a set with no remote repository")
	}
	if !strings.Contains(err.Error(), "no repositories") {
		t.Errorf("error = %q, want a no-repositories error", err)
	}
}

func TestDiscoverRepositoriesTolerantOfMissingDirsAndMalformedLines(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "00-main.conf", strings.Join([]string{
		"not a repository line",
		"repository",
		"repository = https://mirror.example/void",
		"",
	}, "\n"))
	// The second directory does not exist.
	set, err := fetch.DiscoverRepositories([]string{root, filepath.Join(root, "missing")})
	if err != nil {
		t.Fatalf("DiscoverRepositories: %v", err)
	}
	if set.Primary != "/void" {
		t.Errorf("Primary = %q, want /void", set.Primary)
	}
}

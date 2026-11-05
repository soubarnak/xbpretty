package fetch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

func TestManifestMirrorHosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fetch.json")
	if err := os.WriteFile(path, []byte(`{"schema":1,"mirror":"https://repo-de.voidlinux.org/current"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	hosts := fetch.ManifestMirrorHosts(path)
	if len(hosts) != 1 || hosts[0] != "repo-de.voidlinux.org" {
		t.Errorf("ManifestMirrorHosts = %v, want [repo-de.voidlinux.org]", hosts)
	}
}

func TestManifestMirrorHostsTolerant(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{"not json", `{"schema":1}`, `{"mirror":"not a url"}`} {
		path := filepath.Join(dir, "fetch.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if hosts := fetch.ManifestMirrorHosts(path); hosts != nil {
			t.Errorf("ManifestMirrorHosts(%q) = %v, want nil", body, hosts)
		}
	}
	if hosts := fetch.ManifestMirrorHosts(filepath.Join(dir, "missing.json")); hosts != nil {
		t.Errorf("missing manifest returned %v", hosts)
	}
}

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// TestBF11CleanFetchAfterTwoFetchesRemovesFile covers a configuration that
// lives only under /usr/share: the first fetch creates the masking file under
// /etc, and a second fetch must not turn that file into a backup. clean --fetch
// then deletes the file instead of restoring fetch-written content.
func TestBF11CleanFetchAfterTwoFetchesRemovesFile(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, _ := newFetchEnv(t, fixture)
	e.root = true
	usrShare := filepath.Join(e.dir, "usr-share")
	etcDir := filepath.Join(e.dir, "etc-xbps.d")
	for _, dir := range []string{usrShare, etcDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(usrShare, "00-repository-main.conf"),
		[]byte("repository=https://repo-default.voidlinux.org/current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fetchXBPSDDirs = []string{usrShare}
	fetchWriteDir = etcDir
	target := filepath.Join(etcDir, "00-repository-main.conf")

	for i := 1; i <= 2; i++ {
		if code, _, errs := e.run("xbpretty", "fetch", "--auto", "1"); code != 0 {
			t.Fatalf("fetch %d exit = %d\n%s", i, code, errs)
		}
	}
	manifest, err := fetch.ReadManifestFor(fetchManifestPath, etcDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || !manifest.Files[0].Created || manifest.Files[0].Backup != "" {
		t.Fatalf("manifest after two fetches = %+v, want one created entry with no backup", manifest.Files)
	}

	if code, _, errs := e.run("xbpretty", "clean", "--fetch"); code != 0 {
		t.Fatalf("clean --fetch exit = %d\n%s", code, errs)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		content, _ := os.ReadFile(target)
		t.Errorf("clean --fetch left %s behind (%v):\n%s", target, err, content)
	}
	if _, err := os.Stat(target + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a backup of fetch's own output exists: %v", err)
	}
	if _, err := os.Stat(fetchManifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest survived clean --fetch: %v", err)
	}
}

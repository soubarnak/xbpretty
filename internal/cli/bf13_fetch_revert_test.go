package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// TestBF13FetchSyncRevertRemovesNewBackup: a fetch whose sync fails restores
// the configuration and leaves no .xbpretty-backup behind, because the backup
// this run wrote is no longer referenced by any manifest.
func TestBF13FetchSyncRevertRemovesNewBackup(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	e.set("XB_STUB_RUN_STDERR", fmt.Sprintf(
		"ERROR: [reposync] failed to fetch file '%s/one/current/x86_64-repodata': Not Found\n", fixture.srv.URL))

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	restored, err := os.ReadFile(target)
	if err != nil || string(restored) != string(original) {
		t.Errorf("target was not restored: %q %v", restored, err)
	}
	if _, err := os.Stat(target + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a reverted fetch left its backup behind: %v", err)
	}
	entries, err := os.ReadDir(xbpsd)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), fetch.BackupSuffix) {
			t.Errorf("stale backup %s survived the revert", entry.Name())
		}
	}
}

// TestBF13FetchIgnoresOrphanBackup: a backup left by an earlier run is stale
// once the admin has edited the file, so the next fetch snapshots the bytes
// the file holds now and clean --fetch restores those.
func TestBF13FetchIgnoresOrphanBackup(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	edited := []byte("repository=https://repo-default.voidlinux.org/current\nignorepkg=hand-edited\n")
	if err := os.WriteFile(target, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+fetch.BackupSuffix, []byte("stale pre-fetch bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, _, errs := e.run("xbpretty", "fetch"); code != 0 {
		t.Fatalf("fetch exit = %d\n%s", code, errs)
	}
	backup, err := os.ReadFile(target + fetch.BackupSuffix)
	if err != nil || string(backup) != string(edited) {
		t.Fatalf("backup = %q, %v; want the edited bytes the file held before this run", backup, err)
	}
	if code, _, errs := e.run("xbpretty", "clean", "--fetch"); code != 0 {
		t.Fatalf("clean --fetch exit = %d\n%s", code, errs)
	}
	restored, err := os.ReadFile(target)
	if err != nil || string(restored) != string(edited) {
		t.Errorf("clean --fetch restored %q, %v; want the admin's edited bytes", restored, err)
	}
}

// TestBF13RevertKeepsABackupThePreviousManifestNames: when the backup an
// earlier manifest names has gone missing, a later fetch snapshots the target
// again at that same path. If that fetch's sync fails, the restored manifest
// still names the path, so the revert must leave the file for clean --fetch.
func TestBF13RevertKeepsABackupThePreviousManifestNames(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	backup := target + fetch.BackupSuffix

	if code, _, errs := e.run("xbpretty", "fetch"); code != 0 {
		t.Fatalf("first fetch exit = %d\n%s", code, errs)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	e.set("XB_STUB_RUN_STDERR", fmt.Sprintf(
		"ERROR: [reposync] failed to fetch file '%s/one/current/x86_64-repodata': Not Found\n", fixture.srv.URL))

	if code, _, errs := e.run("xbpretty", "fetch"); code != 1 {
		t.Fatalf("second fetch exit = %d, want 1\n%s", code, errs)
	}
	manifest, err := fetch.ReadManifestFor(fetchManifestPath, xbpsd)
	if err != nil {
		t.Fatalf("the first fetch's manifest was not restored: %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Backup != backup {
		t.Fatalf("restored manifest = %+v, want one entry naming %s", manifest.Files, backup)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("the revert removed a backup the restored manifest names: %v", err)
	}
}

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// newCleanFetchEnv is one clean --fetch world: a fixture xbps.d directory and a
// test-root manifest path, both outside the real system.
func newCleanFetchEnv(t *testing.T) (*env, string, string) {
	t.Helper()
	e := newEnv(t)
	e.root = true
	xbpsd := filepath.Join(e.dir, "xbps.d")
	if err := os.MkdirAll(xbpsd, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(e.dir, "var-lib-xbpretty", "fetch.json")
	orig := fetchManifestPath
	fetchManifestPath = manifestPath
	t.Cleanup(func() { fetchManifestPath = orig })
	return e, xbpsd, manifestPath
}

func writeCleanFetchManifest(t *testing.T, path string, manifest fetch.Manifest) {
	t.Helper()
	if err := fetch.WriteManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
}

func contentSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestCleanFetchRestoresTheBackupAndDeletesTheManifest(t *testing.T) {
	e, xbpsd, manifestPath := newCleanFetchEnv(t)
	target := filepath.Join(xbpsd, "00-repository-main.conf")
	backup := target + fetch.BackupSuffix
	const original = "repository=https://repo-default.voidlinux.org/current\n"
	const written = "# Written by xbpretty fetch on 2026-11-05T20:55:12+05:30; revert with 'xbp clean --fetch'.\nrepository=https://repo-fi.voidlinux.org/current\n"
	for path, content := range map[string]string{target: written, backup: original} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeCleanFetchManifest(t, manifestPath, fetch.Manifest{
		SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
		Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
		Files: []fetch.ManifestFile{{
			Path: target, Backup: backup, Created: false, SHA256: contentSHA(written),
		}},
	})

	code, out, errs := e.run("xbpretty", "clean", "--fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if out != "" || errs != "" {
		t.Errorf("clean --fetch printed: stdout %q stderr %q", out, errs)
	}
	restored, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != original {
		t.Errorf("target = %q, want the backup's bytes", restored)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest was not deleted: %v", err)
	}
	if calls := e.commands(); len(calls) != 0 {
		t.Errorf("clean --fetch ran a child: %q", calls)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("restoring consumed the backup: %v", err)
	}
}

func TestCleanFetchDeletesAFileFetchCreated(t *testing.T) {
	e, xbpsd, manifestPath := newCleanFetchEnv(t)
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	const written = "repository=https://repo-fi.voidlinux.org/current\n"
	if err := os.WriteFile(target, []byte(written), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCleanFetchManifest(t, manifestPath, fetch.Manifest{
		SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
		Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
		Files: []fetch.ManifestFile{{Path: target, Created: true, SHA256: contentSHA(written)}},
	})
	code, _, errs := e.run("xbpretty", "clean", "--fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("a fetch-created file was not deleted")
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest was not deleted")
	}
}

func TestCleanFetchWithoutAManifestErrors(t *testing.T) {
	e, _, _ := newCleanFetchEnv(t)
	code, out, errs := e.run("xbpretty", "clean", "--fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
	if !strings.Contains(errs, "xbpretty: no repository configuration written by xbp fetch") {
		t.Errorf("stderr = %q", errs)
	}
}

func TestCleanFetchNamesACorruptManifestAndTouchesNothing(t *testing.T) {
	e, xbpsd, manifestPath := newCleanFetchEnv(t)
	target := filepath.Join(xbpsd, "00-repository-main.conf")
	const content = "repository=https://repo-fi.voidlinux.org/current\n"
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := e.run("xbpretty", "clean", "--fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errs, "xbpretty: reading "+manifestPath+": ") {
		t.Errorf("stderr does not name the corrupt manifest:\n%s", errs)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != content {
		t.Errorf("a corrupt manifest touched the target: %q %v", got, err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("a corrupt manifest was removed: %v", err)
	}
}

func TestCleanFetchWarnsBeforeRevertingAHandEditedFile(t *testing.T) {
	tests := []struct {
		name     string
		created  bool
		backup   bool
		warning  string
	}{
		{name: "restoring", backup: true, warning: "was modified after fetch; restoring "},
		{name: "removing", created: true, warning: "was modified after fetch; removing it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, xbpsd, manifestPath := newCleanFetchEnv(t)
			target := filepath.Join(xbpsd, "00-repository-main.conf")
			const fetched = "repository=https://repo-fi.voidlinux.org/current\n"
			const handEdited = "repository=https://hand-edited.example/current\n"
			if err := os.WriteFile(target, []byte(handEdited), 0o644); err != nil {
				t.Fatal(err)
			}
			entry := fetch.ManifestFile{Path: target, Created: tt.created, SHA256: contentSHA(fetched)}
			if tt.backup {
				backup := target + fetch.BackupSuffix
				if err := os.WriteFile(backup, []byte(fetched), 0o644); err != nil {
					t.Fatal(err)
				}
				entry.Backup = backup
			}
			writeCleanFetchManifest(t, manifestPath, fetch.Manifest{
				SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
				Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
				Files: []fetch.ManifestFile{entry},
			})

			code, _, errs := e.run("xbpretty", "clean", "--fetch")
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			want := "xbpretty: " + target + " " + tt.warning
			if tt.backup {
				want += target + fetch.BackupSuffix
			}
			if !strings.Contains(errs, want) {
				t.Errorf("stderr lacks %q:\n%s", want, errs)
			}
			if tt.created {
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Errorf("hand-edited fetch-created file was not removed")
				}
			} else {
				restored, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if string(restored) != fetched {
					t.Errorf("target = %q, want the backup bytes", restored)
				}
			}
		})
	}
}

func TestCleanFetchSkipsAnAbsentFileAndStillSucceeds(t *testing.T) {
	e, xbpsd, manifestPath := newCleanFetchEnv(t)
	missing := filepath.Join(xbpsd, "20-repository-gone.conf")
	writeCleanFetchManifest(t, manifestPath, fetch.Manifest{
		SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
		Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
		Files: []fetch.ManifestFile{{Path: missing, Created: true, SHA256: "abc"}},
	})
	code, _, errs := e.run("xbpretty", "clean", "--fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if errs != "" {
		t.Errorf("absent file produced stderr: %q", errs)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest was not deleted")
	}
}

func TestCleanFetchIsRootOnly(t *testing.T) {
	e, _, manifestPath := newCleanFetchEnv(t)
	e.root = false
	writeCleanFetchManifest(t, manifestPath, fetch.Manifest{
		SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
		Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
		Files: []fetch.ManifestFile{},
	})
	code, _, errs := e.run("xbpretty", "clean", "--fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errs, "hint: run with sudo to restore the repository configuration") {
		t.Errorf("stderr lacks the FR-048 root hint:\n%s", errs)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("a non-root run touched the manifest: %v", err)
	}
}

func TestCleanFetchVerboseNamesEachRevert(t *testing.T) {
	e, xbpsd, manifestPath := newCleanFetchEnv(t)
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	const written = "repository=https://repo-fi.voidlinux.org/current\n"
	if err := os.WriteFile(target, []byte(written), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCleanFetchManifest(t, manifestPath, fetch.Manifest{
		SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
		Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
		Files: []fetch.ManifestFile{{Path: target, Created: true, SHA256: contentSHA(written)}},
	})
	code, _, errs := e.run("xbpretty", "clean", "--fetch", "-v")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(errs, "verbose: removed "+target) {
		t.Errorf("verbose run does not name the removed file:\n%s", errs)
	}
}

// TestCleanFetchManifestSchemaIsTheFetchSchema pins the wire contract clean
// reads: a schema-1 object with written_at, mirror, arch and files.
func TestCleanFetchManifestSchemaIsTheFetchSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fetch.json")
	manifest := fetch.Manifest{
		SchemaVersion: 1,
		WrittenAt:     "2026-11-05T20:55:12+05:30",
		Mirror:        "https://repo-fi.voidlinux.org",
		Arch:          "x86_64",
		Files: []fetch.ManifestFile{{
			Path: "/etc/xbps.d/00-repository-main.conf",
			Backup: "/etc/xbps.d/00-repository-main.conf.xbpretty-backup",
			Created: false,
			SHA256: "abc",
		}},
	}
	if err := fetch.WriteManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "written_at", "mirror", "arch", "files"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("manifest lacks %q: %s", key, raw)
		}
	}
}

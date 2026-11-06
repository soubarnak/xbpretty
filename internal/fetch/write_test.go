package fetch_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

const originalRepositoryConf = `# original comment
architecture=x86_64
repository=https://repo-default.voidlinux.org/current
ignorepkg=foo bar
bestmatching=true
repository=https://mirror.example/custom
repository=/var/cache/local
repository=https://repo-default.voidlinux.org/current/multilib
`

func managedSet(t *testing.T, dir, name string) (fetch.RepositorySet, string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(originalRepositoryConf), 0o644); err != nil {
		t.Fatal(err)
	}
	managed := []fetch.RepositoryLine{
		{
			File: path, Basename: name,
			Value: "https://repo-default.voidlinux.org/current",
			Host:  "repo-default.voidlinux.org", Suffix: "/current", Managed: true,
		},
		{
			File: path, Basename: name,
			Value: "https://repo-default.voidlinux.org/current/multilib",
			Host:  "repo-default.voidlinux.org", Suffix: "/current/multilib", Managed: true,
		},
		{
			File: path, Basename: name,
			Value: "https://mirror.example/custom",
			Host:  "mirror.example", Suffix: "/custom", Managed: false,
		},
		{
			File: path, Basename: name,
			Value: "/var/cache/local", Managed: false,
		},
	}
	return fetch.RepositorySet{Lines: managed, Suffixes: []string{"/current", "/current/multilib"}}, path
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestPlanWritesRewritesOnlyManagedLines(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	writtenAt := time.Date(2026, time.November, 5, 20, 55, 12, 0, time.FixedZone("IST", 5*3600+30*60))

	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", writtenAt, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(writes))
	}
	write := writes[0]
	wantContent := `# Written by xbpretty fetch on 2026-11-05T20:55:12+05:30; revert with 'xbp clean --fetch'.
# original comment
architecture=x86_64
repository=https://repo-fi.voidlinux.org/current
ignorepkg=foo bar
bestmatching=true
repository=https://mirror.example/custom
repository=/var/cache/local
repository=https://repo-fi.voidlinux.org/current/multilib
`
	if string(write.Content) != wantContent {
		t.Errorf("content:\n%s\nwant:\n%s", write.Content, wantContent)
	}
	if write.Path != filepath.Join(dir, "10-repository-main.conf") {
		t.Errorf("path = %q, want the same-basename target", write.Path)
	}
	if write.Source != source {
		t.Errorf("source = %q, want %q", write.Source, source)
	}
	if write.SHA256 != sha256Hex([]byte(wantContent)) {
		t.Errorf("sha256 = %q, want the hash of the written content", write.SHA256)
	}
}

func TestPlanWritesSkipsFilesWithNoManagedLine(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "20-repository-custom.conf")
	if err := os.WriteFile(custom, []byte("repository=https://example.com/current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	set := fetch.RepositorySet{Lines: []fetch.RepositoryLine{{
		File: custom, Basename: "20-repository-custom.conf",
		Value: "https://example.com/current", Host: "example.com",
		Suffix: "/current", Managed: false,
	}}}
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 0 {
		t.Errorf("writes = %+v, want none: a file with no rewritten line is not written", writes)
	}
}

func TestApplyWritesSavesBackupAndManifest(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	writtenAt := time.Date(2026, time.November, 5, 20, 55, 12, 0, time.FixedZone("IST", 5*3600+30*60))
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", writtenAt, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "fetch.json")

	manifest, _, _, err := fetch.ApplyWrites(writes, fetch.Manifest{}, manifestPath, writtenAt, "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.WrittenAt != "2026-11-05T20:55:12+05:30" || manifest.Mirror != "https://repo-fi.voidlinux.org" || manifest.Arch != "x86_64" {
		t.Errorf("manifest header = %+v", manifest)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("manifest files = %+v, want one", manifest.Files)
	}
	entry := manifest.Files[0]
	backup := source + fetch.BackupSuffix
	if entry.Path != source || entry.Backup != backup || entry.Created {
		t.Errorf("entry = %+v, want path %s, backup %s, created false", entry, source, backup)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SHA256 != sha256Hex(content) {
		t.Errorf("sha256 = %q, want the written content's hash", entry.SHA256)
	}
	backupContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if string(backupContent) != originalRepositoryConf {
		t.Errorf("backup = %q, want the pre-fetch bytes", backupContent)
	}
	for _, path := range []string{source, backup, manifestPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %o, want 0644", path, info.Mode().Perm())
		}
		if os.Geteuid() == 0 {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 || stat.Gid != 0 {
				t.Errorf("%s owner = %v, want root:root", path, stat)
			}
		}
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, raw)
	}
	if decoded["schema_version"] != float64(1) {
		t.Errorf("manifest schema_version = %v, want 1", decoded["schema_version"])
	}
	files, ok := decoded["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("manifest files = %v", decoded["files"])
	}
	field := files[0].(map[string]any)
	for _, key := range []string{"path", "backup", "created", "sha256"} {
		if _, ok := field[key]; !ok {
			t.Errorf("manifest file entry is missing %q: %v", key, field)
		}
	}
}

func TestApplyWritesCreatedFileHasNoBackup(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// The source lives in /usr/share and the /etc target does not exist yet.
	targetDir := filepath.Join(t.TempDir(), "etc-xbps.d")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writes[0].Path = filepath.Join(targetDir, filepath.Base(source))

	manifest, _, _, err := fetch.ApplyWrites(writes, fetch.Manifest{}, filepath.Join(t.TempDir(), "fetch.json"), time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Files[0]
	if !entry.Created || entry.Backup != "" {
		t.Errorf("entry = %+v, want created true and no backup", entry)
	}
	if _, err := os.Stat(entry.Path + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a backup was created for a new file")
	}
}

func TestApplyWritesKeepsTheFirstBackup(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "fetch.json")
	first, _, _, err := fetch.ApplyWrites(writes, fetch.Manifest{}, manifestPath, time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}

	// A second fetch rewrites the target; the backup must stay the original.
	writes, err = fetch.PlanWrites(set, "https://repo-de.voidlinux.org", time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := fetch.ApplyWrites(writes, first, manifestPath, time.Now(), "https://repo-de.voidlinux.org", "x86_64"); err != nil {
		t.Fatal(err)
	}
	backupContent, err := os.ReadFile(source + fetch.BackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(backupContent) != originalRepositoryConf {
		t.Errorf("backup = %q, want the first write's original bytes", backupContent)
	}
}

func TestApplyWritesCarriesUntouchedEntries(t *testing.T) {
	dir := t.TempDir()
	set, _ := managedSet(t, dir, "10-repository-main.conf")
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	previous := fetch.Manifest{
		SchemaVersion: 1,
		WrittenAt:     "2026-11-04T10:00:00Z",
		Mirror:        "https://old.example",
		Arch:          "x86_64",
		Files: []fetch.ManifestFile{{
			Path:    filepath.Join(dir, "20-repository-multilib.conf"),
			Backup:  filepath.Join(dir, "20-repository-multilib.conf.xbpretty-backup"),
			Created: false,
			SHA256:  "deadbeef",
		}},
	}
	manifest, _, _, err := fetch.ApplyWrites(writes, previous, filepath.Join(t.TempDir(), "fetch.json"), time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("manifest files = %+v, want the written file and the carried-over entry", manifest.Files)
	}
	var carried bool
	for _, entry := range manifest.Files {
		if entry.Path == previous.Files[0].Path {
			carried = entry.Backup == previous.Files[0].Backup && entry.SHA256 == previous.Files[0].SHA256
		}
	}
	if !carried {
		t.Errorf("untouched entry was not carried over: %+v", manifest.Files)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fetch.json")
	want := fetch.Manifest{
		SchemaVersion: 1,
		WrittenAt:     "2026-11-05T20:55:12+05:30",
		Mirror:        "https://repo-fi.voidlinux.org",
		Arch:          "x86_64",
		Files: []fetch.ManifestFile{{
			Path:    "/etc/xbps.d/00-repository-main.conf",
			Backup:  "/etc/xbps.d/00-repository-main.conf.xbpretty-backup",
			Created: false,
			SHA256:  strings.Repeat("a", 64),
		}},
	}
	if err := fetch.WriteManifest(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := fetch.ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != want.SchemaVersion || got.WrittenAt != want.WrittenAt ||
		got.Mirror != want.Mirror || got.Arch != want.Arch || len(got.Files) != 1 ||
		got.Files[0] != want.Files[0] {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	if _, err := fetch.ReadManifest(filepath.Join(t.TempDir(), "missing.json")); !os.IsNotExist(err) {
		t.Errorf("missing manifest error = %v, want os.ErrNotExist", err)
	}
}

func TestScanSyncErrors(t *testing.T) {
	written := []string{"https://repo-fi.voidlinux.org/current"}
	tests := []struct {
		name        string
		stdout      string
		stderr      string
		wantFailed  bool
		wantForeign bool
	}{
		{
			name:       "written repository",
			stderr:     "ERROR: [reposync] failed to fetch file 'https://repo-fi.voidlinux.org/current/x86_64-repodata': Not Found\n",
			wantFailed: true,
		},
		{
			name:        "foreign repository",
			stderr:      "ERROR: [reposync] failed to fetch file 'https://example.com/current/x86_64-repodata': Not Found\n",
			wantForeign: true,
		},
		{
			name:       "written repository on stdout",
			stdout:     "ERROR: [reposync] failed https://repo-fi.voidlinux.org/current\n",
			wantFailed: true,
		},
		{
			name:   "not a line start",
			stderr: "  ERROR: [reposync] failed https://repo-fi.voidlinux.org/current\n",
		},
		{
			name:        "a longer path is not the written path",
			stderr:      "ERROR: [reposync] failed https://repo-fi.voidlinux.org/current-extra\n",
			wantForeign: true,
		},
		{
			name:   "clean",
			stderr: "repo-fi: 1234 packages\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed, foreign := fetch.ScanSyncErrors(tt.stdout, tt.stderr, written)
			if failed != tt.wantFailed || foreign != tt.wantForeign {
				t.Errorf("ScanSyncErrors = (%v, %v), want (%v, %v)", failed, foreign, tt.wantFailed, tt.wantForeign)
			}
		})
	}
}

func TestWrittenURLsIncludesDerivedFiles(t *testing.T) {
	writes := []fetch.PlannedWrite{
		{URLs: []string{"https://repo-fi.voidlinux.org/current"}},
		{URLs: []string{"https://repo-fi.voidlinux.org/current/nonfree"}},
		// A preserved custom line is not written by this run and must not
		// widen the sync-error scope.
		{Content: []byte("repository=https://example.com/custom\n")},
	}
	got := fetch.WrittenURLs(writes)
	want := "https://repo-fi.voidlinux.org/current\nhttps://repo-fi.voidlinux.org/current/nonfree"
	if strings.Join(got, "\n") != want {
		t.Errorf("WrittenURLs = %v, want %s", got, want)
	}
}

func TestPlanWritesNonFreeAddsDerivedFiles(t *testing.T) {
	dir := t.TempDir()
	set, _ := managedSet(t, dir, "10-repository-main.conf")
	writtenAt := time.Date(2026, time.November, 5, 20, 55, 12, 0, time.FixedZone("IST", 5*3600+30*60))

	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", writtenAt, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, write := range writes {
		byName[write.Basename] = string(write.Content)
	}
	for name, suffix := range map[string]string{
		"10-repository-nonfree.conf":          "/current/nonfree",
		"10-repository-multilib-nonfree.conf": "/current/multilib/nonfree",
	} {
		content, ok := byName[name]
		if !ok {
			t.Errorf("--non-free did not write %s: %v", name, byName)
			continue
		}
		want := "repository=https://repo-fi.voidlinux.org" + suffix + "\n"
		if !strings.HasSuffix(content, want) {
			t.Errorf("%s content = %q, want the derived line %q", name, content, want)
		}
	}
}

func TestPlanWritesNonFreeDoesNotDuplicateConfiguredPaths(t *testing.T) {
	dir := t.TempDir()
	set, _ := managedSet(t, dir, "10-repository-main.conf")
	// Stock Void already configures /current/nonfree; the flag is a no-op for it.
	set.Lines = append(set.Lines, fetch.RepositoryLine{
		File: filepath.Join(dir, "10-repository-main.conf"), Basename: "10-repository-main.conf",
		Value: "https://repo-default.voidlinux.org/current/nonfree",
		Host:  "repo-default.voidlinux.org", Suffix: "/current/nonfree", Managed: true,
	})
	set.Suffixes = append(set.Suffixes, "/current/nonfree")
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, write := range writes {
		if write.Basename == "10-repository-nonfree.conf" {
			t.Errorf("--non-free wrote a duplicate non-free file: %+v", writes)
		}
	}
}

func TestPlanWritesMasksByBasename(t *testing.T) {
	systemDir := t.TempDir()
	etcDir := t.TempDir()
	set, source := managedSet(t, systemDir, "10-repository-main.conf")
	if filepath.Dir(source) != systemDir {
		t.Fatalf("source = %q, want the /usr/share fixture", source)
	}
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), etcDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 {
		t.Fatalf("writes = %+v, want one", writes)
	}
	if writes[0].Source != source {
		t.Errorf("source = %q, want %q", writes[0].Source, source)
	}
	if writes[0].Path != filepath.Join(etcDir, "10-repository-main.conf") {
		t.Errorf("target = %q, want the same basename under the write directory", writes[0].Path)
	}
}

func TestApplyWritesRollsBackWhenTheManifestCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := fetch.ApplyWrites(writes, fetch.Manifest{}, filepath.Join(blocker, "fetch.json"), time.Now(), "https://repo-fi.voidlinux.org", "x86_64"); err == nil {
		t.Fatal("ApplyWrites succeeded with an uncreatable manifest")
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != originalRepositoryConf {
		t.Errorf("target was not rolled back:\n%s", content)
	}
	if _, err := os.Stat(source + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a failed commit left its backup behind: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".xbpretty.tmp-") {
			t.Errorf("a failed commit left a temp file: %s", entry.Name())
		}
	}
}

func TestReadManifestForRejectsInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "fetch.json")
	valid := fetch.Manifest{
		SchemaVersion: 1, WrittenAt: "2026-11-05T20:55:12+05:30",
		Mirror: "https://repo-fi.voidlinux.org", Arch: "x86_64",
		Files: []fetch.ManifestFile{{
			Path: filepath.Join(dir, "00-repository-main.conf"), Created: true,
			SHA256: strings.Repeat("a", 64),
		}},
	}
	tests := []struct {
		name   string
		mutate func(*fetch.Manifest)
	}{
		{"relative path", func(m *fetch.Manifest) { m.Files[0].Path = "00-repository-main.conf" }},
		{"outside config dir", func(m *fetch.Manifest) { m.Files[0].Path = filepath.Join(t.TempDir(), "x.conf") }},
		{"wrong backup", func(m *fetch.Manifest) { m.Files[0].Backup = "/tmp/elsewhere" }},
		{"short sha", func(m *fetch.Manifest) { m.Files[0].SHA256 = "abc" }},
		{"non-hex sha", func(m *fetch.Manifest) { m.Files[0].SHA256 = strings.Repeat("z", 64) }},
		{"future schema", func(m *fetch.Manifest) { m.SchemaVersion = 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := valid
			manifest.Files = append([]fetch.ManifestFile(nil), valid.Files...)
			tt.mutate(&manifest)
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := fetch.ReadManifestFor(path, dir); err == nil {
				t.Errorf("ReadManifestFor accepted %s", tt.name)
			}
		})
	}
}

func TestReadRegularFileNoFollowRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := fetch.ReadRegularFileNoFollow(link); err == nil {
		t.Error("ReadRegularFileNoFollow followed a symlink")
	}
	content, err := fetch.ReadRegularFileNoFollow(target)
	if err != nil || string(content) != "secret" {
		t.Errorf("regular read = %q, %v", content, err)
	}
}

func TestPlanWritesNonFreeSkipsABasenameAlreadyWritten(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "10-repository-main.conf")
	nonfreeNamed := filepath.Join(dir, "10-repository-nonfree.conf")
	for path, body := range map[string]string{
		main:         "repository=https://repo-default.voidlinux.org/current\n",
		nonfreeNamed: "repository=https://repo-default.voidlinux.org/current\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set := fetch.RepositorySet{
		Suffixes: []string{"/current"},
		Lines: []fetch.RepositoryLine{
			{File: main, Basename: "10-repository-main.conf", Value: "https://repo-default.voidlinux.org/current", Suffix: "/current", Managed: true},
			{File: nonfreeNamed, Basename: "10-repository-nonfree.conf", Value: "https://repo-default.voidlinux.org/current", Suffix: "/current", Managed: true},
		},
	}
	writes, err := fetch.PlanWrites(set, "https://repo-fi.voidlinux.org", time.Now(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]fetch.PlannedWrite{}
	for _, write := range writes {
		if _, exists := byPath[write.Path]; exists {
			t.Errorf("duplicate planned path %s", write.Path)
		}
		byPath[write.Path] = write
	}
	entry, ok := byPath[filepath.Join(dir, "10-repository-nonfree.conf")]
	if !ok {
		t.Fatal("the discovered 10-repository-nonfree.conf was not written")
	}
	if entry.Derived {
		t.Error("the discovered file was replaced by a derived non-free write")
	}
	if !strings.Contains(string(entry.Content), "repository=https://repo-fi.voidlinux.org/current\n") {
		t.Errorf("discovered file content = %q", entry.Content)
	}
}

func TestApplyWritesDedupesByPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "10-repository-main.conf")
	write := fetch.PlannedWrite{
		Path: target, Content: []byte("repository=https://repo-fi.voidlinux.org/current\n"),
		SHA256: strings.Repeat("a", 64),
	}
	manifest, _, _, err := fetch.ApplyWrites([]fetch.PlannedWrite{write, write}, fetch.Manifest{}, filepath.Join(t.TempDir(), "fetch.json"), time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 {
		t.Errorf("manifest files = %+v, want one deduped entry", manifest.Files)
	}
}

func TestApplyWritesRefetchKeepsCreated(t *testing.T) {
	dir := t.TempDir()
	set, _ := managedSet(t, dir, "10-repository-main.conf")
	fixtureSource := filepath.Join(dir, "10-repository-main.conf")
	// Move the source out of the target directory so fetch creates it.
	systemDir := t.TempDir()
	systemSource := filepath.Join(systemDir, filepath.Base(fixtureSource))
	original, err := os.ReadFile(fixtureSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemSource, original, 0o644); err != nil {
		t.Fatal(err)
	}
	for i := range set.Lines {
		set.Lines[i].File = systemSource
	}
	if err := os.Remove(fixtureSource); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "fetch.json")
	first, _, _, err := fetch.ApplyWrites(
		mustPlan(t, set, "https://repo-fi.voidlinux.org", dir),
		fetch.Manifest{}, manifestPath, time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != 1 || !first.Files[0].Created || first.Files[0].Backup != "" {
		t.Fatalf("first manifest = %+v, want created with no backup", first.Files)
	}
	// The pre-fetch state was absence, and a re-fetch must not change that:
	// the entry stays created, with no backup of the first fetch's output.
	second, _, backups, err := fetch.ApplyWrites(
		mustPlan(t, set, "https://repo-de.voidlinux.org", dir),
		first, manifestPath, time.Now(), "https://repo-de.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	entry := second.Files[0]
	if !entry.Created || entry.Backup != "" {
		t.Errorf("second entry = %+v, want created with no backup", entry)
	}
	if _, err := os.Stat(first.Files[0].Path + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a re-fetch snapshotted the first fetch's output: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("backups written = %v, want none", backups)
	}
}

func TestApplyWritesRewritesAnOrphanBackup(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	// An earlier run left a backup behind; the admin has since edited the file,
	// so that backup is stale and must not be trusted.
	edited := []byte("repository=https://repo-default.voidlinux.org/current\nignorepkg=hand-edited\n")
	if err := os.WriteFile(source, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	stale := source + fetch.BackupSuffix
	if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, _, backups, err := fetch.ApplyWrites(
		mustPlan(t, set, "https://repo-fi.voidlinux.org", dir),
		fetch.Manifest{}, filepath.Join(t.TempDir(), "fetch.json"), time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(stale)
	if err != nil || string(backup) != string(edited) {
		t.Errorf("backup = %q, %v; want the bytes the target held before this run", backup, err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Backup != stale {
		t.Errorf("manifest = %+v, want the rewritten backup", manifest.Files)
	}
	if len(backups) != 1 || backups[0] != stale {
		t.Errorf("created backups = %v, want [%s]", backups, stale)
	}
}

func TestApplyWritesReturnsOnlyTheBackupsItWrote(t *testing.T) {
	dir := t.TempDir()
	set, source := managedSet(t, dir, "10-repository-main.conf")
	manifestPath := filepath.Join(t.TempDir(), "fetch.json")
	first, _, backups, err := fetch.ApplyWrites(
		mustPlan(t, set, "https://repo-fi.voidlinux.org", dir),
		fetch.Manifest{}, manifestPath, time.Now(), "https://repo-fi.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0] != source+fetch.BackupSuffix {
		t.Errorf("first run backups = %v, want [%s]", backups, source+fetch.BackupSuffix)
	}

	// The second run reuses the manifest's backup and writes none.
	_, _, backups, err = fetch.ApplyWrites(
		mustPlan(t, set, "https://repo-de.voidlinux.org", dir),
		first, manifestPath, time.Now(), "https://repo-de.voidlinux.org", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Errorf("second run backups = %v, want none: the manifest backup is reused", backups)
	}
}

func mustPlan(t *testing.T, set fetch.RepositorySet, mirror, dir string) []fetch.PlannedWrite {
	t.Helper()
	writes, err := fetch.PlanWrites(set, mirror, time.Now(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return writes
}

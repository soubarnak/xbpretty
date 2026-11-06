package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func testEntry() Entry {
	version := "2.2.5_6"
	auto := false
	return Entry{
		SchemaVersion:    SchemaVersion,
		StartedAt:        "2026-11-06T00:00:00Z",
		FinishedAt:       "2026-11-06T00:00:05Z",
		Status:           StatusApplied,
		RequestedBy:      "root (0)",
		Command:          []string{"install", "figlet"},
		RequestedTargets: []string{"figlet"},
		Packages: []Transition{{
			Name: "figlet", Arch: "x86_64",
			Repository: "https://repo-default.voidlinux.org/current",
			Size:       94372, InstalledSize: 672593,
			Operation: OpInstall,
			After:     &PackageState{Version: &version, AutoInstalled: &auto},
		}},
	}
}

// TestStoreWritePublishesSchemaOneEntry pins FR-029's publication rules: one
// pretty JSON file, mode 0644, a mode-0755 directory, no temp file left, and
// an id that matches the filename.
func TestStoreWritePublishesSchemaOneEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	s := New(dir)
	entry, err := s.Write(testEntry())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if entry.ID != 1 {
		t.Errorf("id = %d, want 1", entry.ID)
	}
	path := filepath.Join(dir, "1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading entry: %v", err)
	}
	if !strings.HasSuffix(string(raw), "}\n") {
		t.Errorf("entry does not end with one newline:\n%q", raw)
	}
	if !strings.Contains(string(raw), "\n  \"schema_version\": 1,") {
		t.Errorf("entry is not two-space indented:\n%s", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("entry is not valid JSON: %v", err)
	}
	if got := decoded["id"]; got != float64(1) {
		t.Errorf("id field = %v, want 1", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %04o, want 0644", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o755 {
		t.Errorf("directory mode = %04o, want 0755", dirInfo.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "1.json.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file survived publication: %v", err)
	}
}

// TestStoreIDsAreMaxPlusOneAndListAscending pins FR-029's allocation and
// FR-032's ascending list order.
func TestStoreIDsAreMaxPlusOneAndListAscending(t *testing.T) {
	s := New(t.TempDir())
	first, err := s.Write(testEntry())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Write(testEntry())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("ids = %d, %d; want 1, 2", first.ID, second.ID)
	}
	// A gap is allowed and the next id is max+1, not count+1.
	if err := os.Remove(filepath.Join(s.Dir(), "1.json")); err != nil {
		t.Fatal(err)
	}
	third, err := s.Write(testEntry())
	if err != nil {
		t.Fatal(err)
	}
	if third.ID != 3 {
		t.Errorf("id after a gap = %d, want 3", third.ID)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != 2 || entries[1].ID != 3 {
		t.Errorf("list = %+v, want ids 2 then 3", entries)
	}
	last, ok, err := s.Last()
	if err != nil || !ok {
		t.Fatalf("Last = %v, %v, %v", last, ok, err)
	}
	if last.ID != 3 {
		t.Errorf("last id = %d, want 3", last.ID)
	}
}

// TestStoreMissingAndEmpty pin the empty-store read: no directory means no
// entries and no error, and a read creates nothing.
func TestStoreMissingAndEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "absent"))
	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
	if _, ok, err := s.Last(); err != nil || ok {
		t.Errorf("Last = ok %v, err %v; want empty, nil", ok, err)
	}
	if _, err := os.Stat(s.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a read created the store: %v", err)
	}
}

// TestStoreLoadErrors pins the read contract: an unknown id is ErrNotFound, a
// corrupt entry fails with the file named, a future schema is a hard error and
// an id that disagrees with the filename is corrupt.
func TestStoreLoadErrors(t *testing.T) {
	t.Run("unknown id", func(t *testing.T) {
		s := New(t.TempDir())
		if _, err := s.Load(7); !errors.Is(err, ErrNotFound) {
			t.Errorf("Load = %v, want ErrNotFound", err)
		}
	})
	t.Run("corrupt entry names the file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "1.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := New(dir)
		_, err := s.Load(1)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Load error = %v, want the file named", err)
		}
		if _, err := s.List(); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("List error = %v, want the file named", err)
		}
	})
	t.Run("future schema", func(t *testing.T) {
		dir := t.TempDir()
		body := `{"schema_version":2,"id":1,"packages":[]}`
		path := filepath.Join(dir, "1.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s := New(dir)
		_, err := s.Load(1)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("Load error = %v, want the file named", err)
		}
		if !strings.Contains(err.Error(), "schema_version") {
			t.Errorf("error does not name the schema: %v", err)
		}
	})
	t.Run("id must match the filename", func(t *testing.T) {
		dir := t.TempDir()
		body := `{"schema_version":1,"id":2,"packages":[]}`
		path := filepath.Join(dir, "1.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s := New(dir)
		if _, err := s.Load(1); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Load error = %v, want the file named", err)
		}
	})
	t.Run("only numeric json is an entry", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "1.json.tmp"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := New(dir)
		entries, err := s.List()
		if err != nil || len(entries) != 0 {
			t.Errorf("List = %+v, %v; want no entries and no error", entries, err)
		}
	})
}

// TestStoreUnknownKeysAreIgnoredAndTempsSwept pins the read leniency and the
// stale-temp rule: unknown extra keys are ignored, and a mutating write clears
// a leftover temp file.
func TestStoreUnknownKeysAreIgnoredAndTempsSwept(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema_version":1,"id":1,"status":"Applied","requested_by":"root (0)","command":[],"packages":[],"future_key":true}`
	if err := os.WriteFile(filepath.Join(dir, "1.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "9.json.tmp")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleTempAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != 1 {
		t.Errorf("entries = %+v, want the one entry", entries)
	}
	// A read must not remove the stale temp file.
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("a read removed the stale temp: %v", err)
	}
	if _, err := s.Write(testEntry()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a write did not sweep the stale temp: %v", err)
	}
}

// TestStoreDeleteAndClear pins the mutating API #70's history clear consumes.
func TestStoreDeleteAndClear(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	first, err := s.Write(testEntry())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Write(testEntry())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load(first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load deleted = %v, want ErrNotFound", err)
	}
	if err := s.Delete(99); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing = %v, want ErrNotFound", err)
	}
	stale := filepath.Join(dir, "7.json.tmp")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleTempAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	n, err := s.Clear()
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if n != 1 {
		t.Errorf("cleared %d entries, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "2.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("entry survived Clear: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale temp survived Clear: %v", err)
	}
	if second.ID != 2 {
		t.Errorf("ids shifted: %d", second.ID)
	}
}

// writeEntryFile writes a raw JSON body at <dir>/1.json and returns the store.
func writeEntryFile(t *testing.T, body string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "1.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(dir), path
}

// TestStoreValidatesRequiredFields pins the read-side schema contract beyond
// schema_version/id: status enum, operation enum, non-empty names and the
// required top-level fields.
func TestStoreValidatesRequiredFields(t *testing.T) {
	const valid = `{"schema_version":1,"id":1,"status":"Applied","requested_by":"root (0)","command":["install","x"],"packages":[{"name":"x","operation":"Install"}]}`
	cases := []struct {
		name string
		body string
		want string
	}{
		{"bogus status", strings.Replace(valid, `"Applied"`, `"Bogus"`, 1), `invalid status "Bogus"`},
		{"unknown operation", strings.Replace(valid, `"Install"`, `"Reinstall?"`, 1), `invalid operation "Reinstall?"`},
		{"empty name", strings.Replace(valid, `"name":"x"`, `"name":""`, 1), `packages[0] has no name`},
		{"missing requested_by", strings.Replace(valid, `,"requested_by":"root (0)"`, "", 1), `missing requested_by`},
		{"missing command", strings.Replace(valid, `,"command":["install","x"]`, "", 1), `missing command`},
		{"missing packages", strings.Replace(valid, `,"packages":[{"name":"x","operation":"Install"}]`, "", 1), `missing packages`},
		{"missing schema", strings.Replace(valid, `"schema_version":1,`, "", 1), `unsupported schema_version 0`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store, path := writeEntryFile(t, tt.body)
			_, err := store.Load(1)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load error = %v, want %q naming %s", err, tt.want, path)
			}
			if _, err := store.List(); err == nil || !strings.Contains(err.Error(), path) {
				t.Errorf("List error = %v, want the file named", err)
			}
		})
	}
}

// TestStoreNonRegularAndOversizedEntries pins the read guards: a FIFO is not
// an entry and never blocks the read, and an oversized file is corrupt.
func TestStoreNonRegularAndOversizedEntries(t *testing.T) {
	t.Run("fifo", func(t *testing.T) {
		dir := t.TempDir()
		if err := syscall.Mkfifo(filepath.Join(dir, "1.json"), 0o644); err != nil {
			t.Skipf("mkfifo: %v", err)
		}
		store := New(dir)
		entries, err := store.List()
		if err != nil || len(entries) != 0 {
			t.Errorf("List = %+v, %v; want a FIFO ignored without blocking", entries, err)
		}
	})
	t.Run("oversized", func(t *testing.T) {
		store, path := writeEntryFile(t, strings.Repeat("x", maxEntryBytes+1))
		if _, err := store.Load(1); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Load error = %v, want the oversized file named", err)
		}
	})
}

// TestStoreCreatesPermissiveAncestorsUnderUmask pins FR-029's 0755 store mode
// even under a restrictive umask: non-root readers must be able to traverse
// to the leaf.
func TestStoreCreatesPermissiveAncestorsUnderUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	root := filepath.Join(t.TempDir(), "a", "b", "history")
	if _, err := New(root).Write(testEntry()); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(filepath.Dir(root), ".."), filepath.Dir(root), root} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %04o, want 0755 under umask 077", dir, info.Mode().Perm())
		}
	}
	info, err := os.Stat(filepath.Join(root, "1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("entry mode = %04o, want 0644 under umask 077", info.Mode().Perm())
	}
}

// TestStoreConcurrentWriters pins max+1 allocation under concurrency: no lost
// entries, no duplicate ids, no sweep of a live temp file.
func TestStoreConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	const writers, each = 4, 10
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if _, err := s.Write(testEntry()); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Write: %v", err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != writers*each {
		t.Errorf("entries = %d, want %d", len(entries), writers*each)
	}
	seen := map[uint32]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			t.Errorf("duplicate id %d", entry.ID)
		}
		seen[entry.ID] = true
	}
	if _, err := os.Stat(filepath.Join(dir, "1.json.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("live temp survived: %v", err)
	}
}

// TestStoreRejectsUnsafePackageMetadata pins the read-surface guard: an entry
// whose name/arch/version could escape a cache path or be read as a flag is
// corrupt, so list/detail fail closed instead of offering a replay.
func TestStoreRejectsUnsafePackageMetadata(t *testing.T) {
	cases := []struct {
		name    string
		nameV   string
		arch    string
		version string
	}{
		{"traversal", "../evil", "x86_64", "1.0_1"},
		{"flag", "-f", "x86_64", "1.0_1"},
		{"hidden", ".hidden", "x86_64", "1.0_1"},
		{"arch separator", "figlet", "x86/64", "1.0_1"},
		{"version traversal", "figlet", "x86_64", "../../1"},
		{"NUL", "fig\x00let", "x86_64", "1.0_1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			version := tc.version
			entry := map[string]any{
				"schema_version":    1,
				"id":                1,
				"started_at":        "2026-11-06T00:00:00Z",
				"finished_at":       "2026-11-06T00:00:05Z",
				"status":            "Applied",
				"requested_by":      "root (0)",
				"command":           []string{"install", "figlet"},
				"requested_targets": []string{"figlet"},
				"implicit":          false,
				"replay_of":         nil,
				"replay_kind":       nil,
				"packages": []any{map[string]any{
					"name": tc.nameV, "arch": tc.arch,
					"repository": "file:///repo", "size": 1, "installed_size": 1,
					"operation": "Install", "before": nil,
					"after": map[string]any{
						"version": version, "auto_installed": false,
						"hold": false, "repolock": false,
					},
				}},
			}
			raw, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "1.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := New(dir).Load(1); err == nil {
				t.Errorf("Load accepted unsafe metadata %+v", tc)
			}
		})
	}
}

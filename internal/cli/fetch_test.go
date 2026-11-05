package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// fetchMirror describes one mirror the fixture serves.
type fetchMirror struct {
	name       string
	region     string
	location   string
	tier       int
	delay      time.Duration
	probeFail  bool
	suffixFail bool
}

type fetchFixture struct {
	t       *testing.T
	srv     *httptest.Server
	mirrors []fetchMirror
	rawList string // when set, served verbatim instead of the mirrors above

	mu           sync.Mutex
	listRequests int
	probeRanges  []string
	headPaths    []string
	probeURLs    []string
}

func newFetchFixture(t *testing.T, mirrors []fetchMirror) *fetchFixture {
	t.Helper()
	f := &fetchFixture{t: t, mirrors: mirrors}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

// handle serves the mirror list and every mirror's repodata paths.
func (f *fetchFixture) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/mirrors.lst" {
		f.mu.Lock()
		f.listRequests++
		f.mu.Unlock()
		if f.rawList != "" {
			_, _ = w.Write([]byte(f.rawList))
			return
		}
		var b strings.Builder
		b.WriteString("# fixture mirror list\n")
		for _, m := range f.mirrors {
			fmt.Fprintf(&b, "%s\t%s/%s/\t%s\t%d\n", m.region, f.srv.URL, m.name, m.location, m.tier)
		}
		_, _ = w.Write([]byte(b.String()))
		return
	}
	name, suffix, ok := splitFixturePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	spec, found := f.find(name)
	if !found {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodHead {
		f.mu.Lock()
		f.headPaths = append(f.headPaths, r.URL.Path)
		f.mu.Unlock()
		if spec.suffixFail {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	// The probe is the ranged GET of <primary>/<arch>-repodata.
	if !strings.HasSuffix(suffix, "-repodata") || r.Header.Get("Range") == "bytes=0-0" {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	f.probeRanges = append(f.probeRanges, r.Header.Get("Range"))
	f.probeURLs = append(f.probeURLs, r.URL.Path)
	f.mu.Unlock()
	if spec.probeFail {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	if spec.delay > 0 {
		time.Sleep(spec.delay)
	}
	body := make([]byte, 64)
	copy(body, []byte{0x28, 0xB5, 0x2F, 0xFD})
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

// splitFixturePath returns the mirror name and the repository suffix in paths
// such as /m1/current/nonfree/x86_64-repodata.
func splitFixturePath(path string) (name, suffix string, ok bool) {
	trimmed := strings.TrimPrefix(path, "/")
	name, rest, found := strings.Cut(trimmed, "/")
	if !found || name == "" {
		return "", "", false
	}
	return name, "/" + rest, true
}

func (f *fetchFixture) find(name string) (fetchMirror, bool) {
	for _, m := range f.mirrors {
		if m.name == name {
			return m, true
		}
	}
	return fetchMirror{}, false
}

func (f *fetchFixture) probePaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.probeURLs...)
}

func (f *fetchFixture) counts() (list, probes, heads int, ranges, headPaths []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listRequests, len(f.probeRanges), len(f.headPaths), append([]string(nil), f.probeRanges...), append([]string(nil), f.headPaths...)
}

// newFetchEnv wires the fixture and a repository configuration into a test
// environment. It returns the env and the xbps.d directory.
func newFetchEnv(t *testing.T, fixture *fetchFixture) (*env, string) {
	t.Helper()
	e := newEnv(t)
	e.set("XB_STUB_RUN_STDOUT", "")
	e.set("XB_STUB_RUN_STDERR", "")
	xbpsd := filepath.Join(e.dir, "xbps.d")
	if err := os.MkdirAll(xbpsd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xbpsd, "00-repository-main.conf"),
		[]byte("repository=https://repo-default.voidlinux.org/current\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origURL, origDirs, origArch, origClient, origPick, origEvents, origPrevious, origWriteDir, origManifest :=
		fetchMirrorListURL, fetchXBPSDDirs, fetchArch, fetchHTTPClient, pickMirror, mirrorScoreEvents, fetchPreviousHosts, fetchWriteDir, fetchManifestPath
	fetchMirrorListURL = fixture.srv.URL + "/mirrors.lst"
	fetchXBPSDDirs = []string{xbpsd}
	fetchWriteDir = xbpsd
	fetchManifestPath = filepath.Join(e.dir, "fetch.json")
	fetchArch = "x86_64"
	fetchHTTPClient = fixture.srv.Client
	pickMirror = nil
	mirrorScoreEvents = func(fetch.ProgressEvent) {}
	fetchPreviousHosts = func() []string { return nil }
	t.Cleanup(func() {
		fetchMirrorListURL, fetchXBPSDDirs, fetchArch, fetchHTTPClient, pickMirror, mirrorScoreEvents, fetchPreviousHosts, fetchWriteDir, fetchManifestPath =
			origURL, origDirs, origArch, origClient, origPick, origEvents, origPrevious, origWriteDir, origManifest
	})
	return e, xbpsd
}

// assertNoFiles proves the read-only paths touched nothing in the xbps.d
// fixture directory.
func assertNoFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "00-repository-main.conf" {
		t.Errorf("xbps.d directory changed: %v", names)
	}
}

func chosenLine(table string) string {
	for _, line := range strings.Split(table, "\n") {
		if strings.HasSuffix(line, "*") {
			return line
		}
	}
	return ""
}

func TestFetchRanksProbesAndPrintsTable(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "slow", region: "EU", location: "Helsinki, Finland", tier: 1, delay: 250 * time.Millisecond},
		{name: "fast", region: "NA", location: "Chicago, USA", tier: 2},
		{name: "medium", region: "AS", location: "Singapore", tier: 3, delay: 100 * time.Millisecond},
	})
	e, xbpsd := newFetchEnv(t, fixture)
	events := 0
	mirrorScoreEvents = func(event fetch.ProgressEvent) {
		events++
		if event.Total != 3 || event.Done < 1 || event.Done > 3 {
			t.Errorf("progress event = %+v", event)
		}
	}

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("table lines = %d, want header + 3 rows + dry-run line:\n%s", len(lines), out)
	}
	// Rank: fast, medium, slow; the chosen marker is on the fast row.
	if !strings.Contains(lines[1], "/fast") || !strings.HasSuffix(lines[1], "*") {
		t.Errorf("first ranked row is not the chosen fast mirror: %q", lines[1])
	}
	if !strings.Contains(lines[2], "/medium") {
		t.Errorf("second ranked row is not medium: %q", lines[2])
	}
	if !strings.Contains(lines[3], "/slow") {
		t.Errorf("third ranked row is not slow: %q", lines[3])
	}
	if !strings.HasSuffix(out, "Dry run: no repository configuration was written.\n") {
		t.Errorf("missing the dry-run line:\n%s", out)
	}
	if errs != "" {
		t.Errorf("unexpected stderr:\n%s", errs)
	}

	list, probes, heads, ranges, headPaths := fixture.counts()
	if list != 1 {
		t.Errorf("mirror list requests = %d, want 1", list)
	}
	if probes != 3 {
		t.Errorf("probes = %d, want 3", probes)
	}
	for _, r := range ranges {
		if r != "bytes=0-262143" {
			t.Errorf("Range = %q, want bytes=0-262143", r)
		}
	}
	// Only the chosen candidate needs validation, and only its primary suffix.
	if heads != 1 || len(headPaths) != 1 || !strings.HasSuffix(headPaths[0], "/fast/current/x86_64-repodata") {
		t.Errorf("HEADs = %v, want only the fast mirror", headPaths)
	}
	if events != 3 {
		t.Errorf("MirrorScore events = %d, want 3", events)
	}
	if cmds := e.commands(); len(cmds) != 0 {
		t.Errorf("fetch started a child: %v", cmds)
	}
	assertNoFiles(t, xbpsd)
}

func TestFetchAutoTwoValidatesTwoFastest(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "fastbroken", region: "EU", location: "A", tier: 1, suffixFail: true},
		{name: "second", region: "EU", location: "B", tier: 2, delay: 120 * time.Millisecond},
		{name: "third", region: "EU", location: "C", tier: 3, delay: 300 * time.Millisecond},
	})
	e, _ := newFetchEnv(t, fixture)

	code, out, errs := e.run("xbpretty", "fetch", "--auto", "2", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	selected := chosenLine(out)
	if !strings.Contains(selected, "/second") {
		t.Errorf("chosen = %q, want the second-fastest mirror", selected)
	}
	_, _, _, _, headPaths := fixture.counts()
	joined := strings.Join(headPaths, "\n")
	if !strings.Contains(joined, "/fastbroken/current/x86_64-repodata") {
		t.Errorf("the fastest mirror was not validated first: %v", headPaths)
	}
	if !strings.Contains(joined, "/second/current/x86_64-repodata") {
		t.Errorf("the second mirror was not validated: %v", headPaths)
	}
	if strings.Contains(joined, "/third/") {
		t.Errorf("--auto 2 validated the third mirror: %v", headPaths)
	}
}

func TestFetchRootlessPrintsTableHintAndWritesNothing(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "Helsinki, Finland", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = false

	code, out, errs := e.run("xbpretty", "fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s\n%s", code, out, errs)
	}
	if chosenLine(out) == "" {
		t.Errorf("table has no Chosen marker:\n%s", out)
	}
	if !strings.Contains(errs, "hint: run with sudo to write the repository configuration") {
		t.Errorf("stderr lacks the exact sudo hint:\n%s", errs)
	}
	if strings.Contains(out, "Dry run:") {
		t.Errorf("rootless run printed the dry-run line:\n%s", out)
	}
	assertNoFiles(t, xbpsd)
	if cmds := e.commands(); len(cmds) != 0 {
		t.Errorf("rootless fetch started a child: %v", cmds)
	}
}

func TestFetchDryRunAsRootWritesNothing(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "Dry run: no repository configuration was written.") {
		t.Errorf("missing dry-run line:\n%s", out)
	}
	if strings.Contains(errs, "sudo") {
		t.Errorf("dry run printed the sudo hint:\n%s", errs)
	}
	assertNoFiles(t, xbpsd)
	if cmds := e.commands(); len(cmds) != 0 {
		t.Errorf("dry run started a child: %v", cmds)
	}
}

// writeFetchConf sets the one managed configuration file the write path
// masks. It returns the original bytes.
func writeFetchConf(t *testing.T, dir string) []byte {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, "00-repository-main.conf")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	original := []byte("# system comment\n" +
		"architecture=x86_64\n" +
		"repository=https://repo-default.voidlinux.org/current\n" +
		"ignorepkg=foo bar\n" +
		"bestmatching=true\n" +
		"repository=https://example.com/custom\n" +
		"repository=/var/cache/local\n")
	if err := os.WriteFile(filepath.Join(dir, "10-repository-main.conf"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	return original
}

func fetchSHA256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestFetchWritesMaskingFileBackupManifestAndSyncs(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "Helsinki, Finland", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")

	code, out, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if errs != "" {
		t.Errorf("successful fetch wrote stderr:\n%s", errs)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	header, body, ok := strings.Cut(string(written), "\n")
	if !ok {
		t.Fatalf("written file has no header line:\n%s", written)
	}
	const prefix = "# Written by xbpretty fetch on "
	const suffix = "; revert with 'xbp clean --fetch'."
	if !strings.HasPrefix(header, prefix) || !strings.HasSuffix(header, suffix) {
		t.Errorf("header = %q, want %q...%q", header, prefix, suffix)
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(header, prefix), suffix)
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Errorf("header timestamp %q is not RFC3339: %v", stamp, err)
	}
	wantBody := "# system comment\n" +
		"architecture=x86_64\n" +
		"repository=" + fixture.srv.URL + "/one/current\n" +
		"ignorepkg=foo bar\n" +
		"bestmatching=true\n" +
		"repository=https://example.com/custom\n" +
		"repository=/var/cache/local\n"
	if body != wantBody {
		t.Errorf("written body:\n%s\nwant:\n%s", body, wantBody)
	}

	backup := target + fetch.BackupSuffix
	backupContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if string(backupContent) != string(original) {
		t.Errorf("backup = %q, want the original bytes", backupContent)
	}
	for _, path := range []string{target, backup} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %o, want 0644", path, info.Mode().Perm())
		}
	}

	manifest, err := fetch.ReadManifestFor(fetchManifestPath, xbpsd)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Mirror != fixture.srv.URL+"/one" ||
		manifest.Arch != "x86_64" || manifest.WrittenAt == "" {
		t.Errorf("manifest header = %+v", manifest)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("manifest files = %+v, want one", manifest.Files)
	}
	entry := manifest.Files[0]
	if entry.Path != target || entry.Backup != backup || entry.Created || entry.SHA256 != fetchSHA256Of(written) {
		t.Errorf("manifest entry = %+v", entry)
	}

	cmds := e.commands()
	if len(cmds) != 1 || cmds[0] != "-S" {
		t.Errorf("children = %q, want exactly xbps-install -S", cmds)
	}
	if !strings.Contains(out, "Sources have been written to "+target) {
		t.Errorf("stdout does not name the written file:\n%s", out)
	}
}

func TestFetchSyncErrorOnWrittenRepositoryRestoresPreviousConfiguration(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Errorf("target was not restored:\n%s", restored)
	}
	if _, err := os.Stat(fetchManifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest survived a full revert: %v", err)
	}
	for _, want := range []string{
		"ERROR: [reposync] failed to fetch file",
		"xbpretty: repository sync failed; previous configuration restored",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}
}

func TestFetchSyncForeignErrorIsForwardedWithoutRevert(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)
	e.set("XB_STUB_RUN_STDERR", "ERROR: [reposync] failed to fetch file 'https://example.com/custom/x86_64-repodata': Not Found\n")

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for a foreign repository error\n%s", code, errs)
	}
	written, err := os.ReadFile(filepath.Join(xbpsd, "10-repository-main.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), fixture.srv.URL+"/one/current") {
		t.Errorf("the new configuration was reverted:\n%s", written)
	}
	if _, err := os.Stat(fetchManifestPath); err != nil {
		t.Errorf("manifest missing after a successful sync: %v", err)
	}
	for _, want := range []string{
		"ERROR: [reposync] failed to fetch file 'https://example.com/custom/x86_64-repodata'",
		"xbpretty: repository sync reported errors; review the output above",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}
	if strings.Contains(errs, "previous configuration restored") {
		t.Errorf("a foreign error reverted the configuration:\n%s", errs)
	}
}

func TestFetchNonZeroSyncExitRevertsAndPassesTheChildCode(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	e.set("XB_STUB_EXIT", "42")

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 42 {
		t.Fatalf("exit = %d, want 42 (xbps-install's code)\n%s", code, errs)
	}
	restored, err := os.ReadFile(filepath.Join(xbpsd, "10-repository-main.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Errorf("target was not restored on a non-zero sync exit:\n%s", restored)
	}
	if !strings.Contains(errs, "xbpretty: repository sync failed; previous configuration restored") {
		t.Errorf("stderr lacks the restore line:\n%s", errs)
	}
}

func TestFetchSyncKeepsStdinAvailableForTheKeyPrompt(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)

	// The sync child is the only reader. If xbpretty consumed stdin first, the
	// stub would see EOF instead of the answer.
	script := filepath.Join(t.TempDir(), "xbps-install-sync")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nread line\nprintf 'READ:%s\\n' \"$line\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(script)
	e.setAnswers("key-import-answer\n")

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(errs, "READ:key-import-answer") {
		t.Errorf("the sync child did not receive stdin; xbpretty consumed it:\n%s", errs)
	}
}

func TestFetchCountryFiltersBeforeProbe(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "east", region: "EU", location: "Helsinki, Finland", tier: 1},
		{name: "west", region: "NA", location: "Chicago, USA", tier: 2},
	})
	e, _ := newFetchEnv(t, fixture)

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run", "-c", "USA")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "/west") || strings.Contains(out, "/east") {
		t.Errorf("country filter did not select only the USA mirror:\n%s", out)
	}
	if _, probes, _, _, _ := fixture.counts(); probes != 1 {
		t.Errorf("probes = %d, want 1 after filtering", probes)
	}

	// The user's value is one name, not a word list: a full location is not a
	// match for any single location word.
	code, out, errs = e.run("xbpretty", "fetch", "--dry-run", "-c", "Chicago, USA")
	if code != 1 {
		t.Fatalf("full-location exit = %d, want 1\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "xbpretty: no mirrors match the requested country or region") {
		t.Errorf("full-location stderr = %q", errs)
	}

	code, out, errs = e.run("xbpretty", "fetch", "--dry-run", "-c", "atlantis")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "xbpretty: no mirrors match the requested country or region") {
		t.Errorf("stderr = %q", errs)
	}
}

func TestFetchMirrorListFailureFallsBackToSnapshot(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	fixture.srv.Close() // every request now fails
	e, _ := newFetchEnv(t, fixture)

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run", "-c", "no-such-country")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: mirror list unavailable; using the built-in snapshot from 2026-11-05") {
		t.Errorf("missing snapshot warning:\n%s", errs)
	}
	if !strings.Contains(errs, "xbpretty: no mirrors match the requested country or region") {
		t.Errorf("snapshot fallback did not reach the country filter:\n%s", errs)
	}
}

func TestFetchNonFreeAddsValidationSuffixes(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run", "--non-free")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	_, _, _, _, headPaths := fixture.counts()
	joined := strings.Join(headPaths, "\n")
	for _, want := range []string{
		"/one/current/x86_64-repodata",
		"/one/current/nonfree/x86_64-repodata",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("HEADs %v do not include %s", headPaths, want)
		}
	}
	if strings.Contains(joined, "/multilib/nonfree/") {
		t.Errorf("--non-free synthesised a multilib path that was not configured: %v", headPaths)
	}

	// When /current/multilib is a managed suffix, its non-free sibling joins
	// the validation set.
	if err := os.WriteFile(filepath.Join(xbpsd, "10-repository-multilib.conf"),
		[]byte("repository=https://repo-default.voidlinux.org/current/multilib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs = e.run("xbpretty", "fetch", "--dry-run", "--non-free")
	if code != 0 {
		t.Fatalf("multilib exit = %d, want 0\n%s", code, errs)
	}
	_, _, _, _, headPaths = fixture.counts()
	joined = strings.Join(headPaths, "\n")
	if !strings.Contains(joined, "/one/current/multilib/nonfree/x86_64-repodata") {
		t.Errorf("HEADs %v do not include the configured multilib non-free suffix", headPaths)
	}
}

func TestFetchSuffixFailureSelectsNextMirror(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "broken", region: "EU", location: "A", tier: 1, suffixFail: true},
		{name: "good", region: "EU", location: "B", tier: 2, delay: 80 * time.Millisecond},
	})
	e, _ := newFetchEnv(t, fixture)

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if selected := chosenLine(out); !strings.Contains(selected, "/good") {
		t.Errorf("chosen = %q, want the next mirror", selected)
	}
}

func TestFetchNoCandidateServingAllSuffixes(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "broken", region: "EU", location: "", tier: 1, suffixFail: true}})
	e, _ := newFetchEnv(t, fixture)

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "xbpretty: no candidate mirror serves all configured repositories") {
		t.Errorf("stderr = %q", errs)
	}
}

func TestFetchProbeFailuresWarnAndRankSurvivors(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "good", region: "EU", location: "", tier: 2, delay: 20 * time.Millisecond},
		{name: "bad", region: "EU", location: "", tier: 1, probeFail: true},
	})
	e, _ := newFetchEnv(t, fixture)

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "xbpretty: 1 of 2 mirrors did not respond") {
		t.Errorf("missing failed-mirror warning:\n%s", errs)
	}
	if !strings.Contains(out, "/good") || strings.Contains(out, "/bad") {
		t.Errorf("table should rank only the survivor:\n%s", out)
	}

	code, _, errs = e.run("xbpretty", "fetch", "--dry-run", "-v")
	if code != 0 {
		t.Fatalf("verbose exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "verbose: ") || !strings.Contains(errs, "/bad") {
		t.Errorf("verbose run does not name the failed mirror:\n%s", errs)
	}
}

func TestFetchRejectsArgumentsAndPassthrough(t *testing.T) {
	fixture := newFetchFixture(t, nil)
	e, _ := newFetchEnv(t, fixture)
	for _, args := range [][]string{
		{"fetch", "extra"},
		{"fetch", "--", "extra"},
	} {
		code, _, errs := e.run(append([]string{"xbpretty"}, args...)...)
		if code != 2 {
			t.Errorf("%v exit = %d, want 2\n%s", args, code, errs)
		}
	}
	if list, _, _, _, _ := fixture.counts(); list != 0 {
		t.Errorf("argument validation made %d network requests", list)
	}
}

func TestFetchNARefusalsMakeNoNetworkOrFileAccess(t *testing.T) {
	fixture := newFetchFixture(t, nil)
	e, _ := newFetchEnv(t, fixture)
	refusals := map[string]string{
		"--sources": "--sources is not available: Void repositories contain no source packages",
		"--debian":  "--debian is not available: xbpretty targets Void Linux only",
		"--ubuntu":  "--ubuntu is not available: xbpretty targets Void Linux only",
		"--devuan":  "--devuan is not available: xbpretty targets Void Linux only",
	}
	for flag, line := range refusals {
		code, _, errs := e.run("xbpretty", "fetch", flag)
		if code != 2 {
			t.Errorf("%s exit = %d, want 2", flag, code)
		}
		if !strings.Contains(errs, "xbpretty: "+line) {
			t.Errorf("%s stderr = %q, want %q", flag, errs, line)
		}
	}
	if list, probes, heads, _, _ := fixture.counts(); list+probes+heads != 0 {
		t.Errorf("N/A refusals touched the network: list=%d probes=%d heads=%d", list, probes, heads)
	}
}

// rewriteScheme lets a test make an https request reach the fixture's plain
// HTTP listener, so --https-only's scheme rewrite can be observed end to end.
type rewriteScheme struct {
	base http.RoundTripper
}

func (r rewriteScheme) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" {
		req = req.Clone(req.Context())
		req.URL.Scheme = "http"
	}
	return r.base.RoundTrip(req)
}

func TestFetchHTTPSOnlyRewritesTheProbedAndShownURL(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, _ := newFetchEnv(t, fixture)
	base := fixture.srv.Client()
	fetchHTTPClient = func() *http.Client {
		return &http.Client{Transport: rewriteScheme{base: base.Transport}}
	}

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run", "--https-only")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "https://") || strings.Contains(out, "http://") {
		t.Errorf("--https-only table does not show the rewritten URL:\n%s", out)
	}
}

func TestFetchBareAutoUsesTheTopThreeShortlist(t *testing.T) {
	var mirrors []fetchMirror
	for i := 0; i < 4; i++ {
		mirrors = append(mirrors, fetchMirror{
			name:       fmt.Sprintf("m%d", i),
			region:     "EU",
			location:   fmt.Sprintf("Place %d", i),
			tier:       i + 1,
			suffixFail: true,
		})
	}
	fixture := newFetchFixture(t, mirrors)
	e, _ := newFetchEnv(t, fixture)

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run", "--auto")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	_, _, _, _, headPaths := fixture.counts()
	if len(headPaths) != 3 {
		t.Errorf("bare --auto validated %d mirrors, want 3: %v", len(headPaths), headPaths)
	}
}

func TestFetchAutoRangeErrors(t *testing.T) {
	fixture := newFetchFixture(t, nil)
	e, _ := newFetchEnv(t, fixture)
	for _, args := range [][]string{
		{"fetch", "--auto", "0"},
		{"fetch", "--auto", "256"},
		{"fetch", "--auto=abc"},
	} {
		code, _, errs := e.run(append([]string{"xbpretty"}, args...)...)
		if code != 2 {
			t.Errorf("%v exit = %d, want 2", args, code)
		}
		if !strings.Contains(errs, "xbpretty: --auto must be between 1 and 255") {
			t.Errorf("%v stderr = %q, want the exact --auto error", args, errs)
		}
	}
	if list, _, _, _, _ := fixture.counts(); list != 0 {
		t.Errorf("--auto validation touched the network")
	}
}

func TestFetchRootlessDryRunExitsZero(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = false

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(out, "Dry run: no repository configuration was written.") {
		t.Errorf("missing dry-run line:\n%s", out)
	}
	if strings.Contains(errs, "hint:") {
		t.Errorf("rootless dry run printed a hint:\n%s", errs)
	}
	assertNoFiles(t, xbpsd)
}

func TestFetchPickerSeamGatesAndValidatesOnce(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{
		{name: "first", region: "EU", location: "A", tier: 1},
		{name: "second", region: "EU", location: "B", tier: 2, delay: 60 * time.Millisecond},
	})
	e, _ := newFetchEnv(t, fixture)
	e.tty = true
	e.root = true
	t.Setenv("TERM", "xterm")
	t.Setenv("LC_ALL", "C.UTF-8")

	calls := 0
	pickMirror = func(ranked []fetch.Result) (int, bool) {
		calls++
		return 0, true
	}

	// Root + TTY + no --auto + no --dry-run runs the picker seam exactly once
	// and writes its single choice.
	code, out, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	if calls != 1 {
		t.Errorf("picker calls = %d, want 1", calls)
	}
	if selected := chosenLine(out); !strings.Contains(selected, "/first") {
		t.Errorf("chosen = %q, want the picker's first row", selected)
	}
	if _, _, heads, _, _ := fixture.counts(); heads != 1 {
		t.Errorf("HEADs = %d, want the picker's single choice validated once", heads)
	}

	// A picker choice failing validation errors without trying a second row.
	calls = 0
	fixture.mirrors[0].suffixFail = true
	code, _, errs = e.run("xbpretty", "fetch")
	if code != 1 || !strings.Contains(errs, "xbpretty: no candidate mirror serves all configured repositories") {
		t.Errorf("failed picker validation = %d, %q", code, errs)
	}
	if calls != 1 {
		t.Errorf("picker calls = %d, want the picker consulted once", calls)
	}
	if _, _, heads, _, _ := fixture.counts(); heads != 2 {
		t.Errorf("HEADs = %d, want exactly one more validation", heads)
	}
}

func TestFetchPickerNotOpenedWhenRootlessDryRunOrNonTTY(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, _ := newFetchEnv(t, fixture)
	called := 0
	pickMirror = func(ranked []fetch.Result) (int, bool) {
		called++
		return 0, true
	}

	cases := []struct {
		name   string
		root   bool
		tty    bool
		term   string
		locale string
		argv   []string
	}{
		{name: "non-TTY", root: true, tty: false, term: "xterm", locale: "C.UTF-8", argv: []string{"fetch"}},
		{name: "dry-run", root: true, tty: true, term: "xterm", locale: "C.UTF-8", argv: []string{"fetch", "--dry-run"}},
		{name: "auto", root: true, tty: true, term: "xterm", locale: "C.UTF-8", argv: []string{"fetch", "--auto", "2"}},
		{name: "rootless", root: false, tty: true, term: "xterm", locale: "C.UTF-8", argv: []string{"fetch"}},
		{name: "non-UTF-8 locale", root: true, tty: true, term: "xterm", locale: "C", argv: []string{"fetch"}},
		{name: "TERM=dumb case-insensitive", root: true, tty: true, term: "DuMb", locale: "C.UTF-8", argv: []string{"fetch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = 0
			e.root = tc.root
			e.tty = tc.tty
			t.Setenv("TERM", tc.term)
			t.Setenv("LC_ALL", tc.locale)
			e.run(append([]string{"xbpretty"}, tc.argv...)...)
			if called != 0 {
				t.Errorf("picker opened for %s", tc.name)
			}
		})
	}
}

func TestFetchUsesXbpsArchitectureForMusl(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	if err := os.WriteFile(filepath.Join(xbpsd, "00-repository-main.conf"), []byte(
		"architecture=x86_64-musl\nrepository=https://repo-default.voidlinux.org/current/musl\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != "/one/current/musl/x86_64-musl-repodata" {
		t.Errorf("probe paths = %v, want /one/current/musl/x86_64-musl-repodata", probes)
	}
	_, _, _, _, headPaths := fixture.counts()
	if len(headPaths) != 1 || headPaths[0] != "/one/current/musl/x86_64-musl-repodata" {
		t.Errorf("HEAD paths = %v, want the musl arch path", headPaths)
	}
}

func TestFetchAarch64PathsAndNonFreeDerivation(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	if err := os.WriteFile(filepath.Join(xbpsd, "00-repository-main.conf"), []byte(strings.Join([]string{
		"architecture=aarch64",
		"repository=https://repo-default.voidlinux.org/current",
		"repository=https://repo-default.voidlinux.org/current/multilib",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run", "--non-free")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != "/one/current/aarch64-repodata" {
		t.Errorf("probe paths = %v, want /one/current/aarch64-repodata", probes)
	}
	_, _, _, _, headPaths := fixture.counts()
	joined := strings.Join(headPaths, "\n")
	for _, want := range []string{
		"/one/current/aarch64-repodata",
		"/one/current/multilib/aarch64-repodata",
		"/one/current/nonfree/aarch64-repodata",
		"/one/current/multilib/nonfree/aarch64-repodata",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("HEADs %v do not include %s", headPaths, want)
		}
	}
	if strings.Contains(joined, "/current/aarch64/nonfree") {
		t.Errorf("non-free path was derived from the architecture instead of the configured suffix: %v", headPaths)
	}
}

func TestFetchCustomRepositoryIsNeverProbedOrValidated(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	if err := os.WriteFile(filepath.Join(xbpsd, "20-repository-custom.conf"),
		[]byte("repository=https://custom.invalid/void\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	for _, path := range append(fixture.probePaths(), fixture.headPathsForTest()...) {
		if strings.Contains(path, "custom") {
			t.Errorf("custom repository was probed or validated: %q", path)
		}
	}
}

func (f *fetchFixture) headPathsForTest() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.headPaths...)
}

func TestFetchCustomOnlyRepositorySet(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	if err := os.WriteFile(filepath.Join(xbpsd, "00-repository-main.conf"),
		[]byte("repository=https://custom.invalid/void\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: no Void mirror repository found to rewrite") {
		t.Errorf("stderr = %q, want the exact no-managed-repository line", errs)
	}
	if probes := len(fixture.probePaths()); probes != 0 {
		t.Errorf("custom-only run probed %d mirrors", probes)
	}
	if list, _, _, _, _ := fixture.counts(); list != 1 {
		t.Errorf("mirror list requests = %d, want 1", list)
	}
}

func TestFetchMasksByBasenameAcrossXBPSDDirectories(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, _ := newFetchEnv(t, fixture)

	etc := filepath.Join(e.dir, "etc-xbps.d")
	usr := filepath.Join(e.dir, "usr-xbps.d")
	writeCLIRepoFile(t, etc, "xbps-arch.conf", "architecture=x86_64\n")
	writeCLIRepoFile(t, etc, "00-repository-main.conf", "repository=https://repo-default.voidlinux.org/current\n")
	writeCLIRepoFile(t, usr, "00-repository-main.conf", "repository=https://repo-default.voidlinux.org/current/musl\n")
	writeCLIRepoFile(t, usr, "10-repository-multilib.conf", "repository=https://repo-default.voidlinux.org/current/multilib\n")
	fetchXBPSDDirs = []string{etc, usr}

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != "/one/current/x86_64-repodata" {
		t.Errorf("probe paths = %v, want the /etc main primary path", probes)
	}
	_, _, _, _, headPaths := fixture.counts()
	joined := strings.Join(headPaths, "\n")
	if !strings.Contains(joined, "/one/current/multilib/x86_64-repodata") {
		t.Errorf("unmasked /usr/share multilib was not validated: %v", headPaths)
	}
	if strings.Contains(joined, "/current/musl") {
		t.Errorf("masked /usr/share main contributed a suffix: %v", headPaths)
	}
}

func writeCLIRepoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFetchMalformedMirrorRowsOnlyUnderVerbose(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, _ := newFetchEnv(t, fixture)
	fixture.rawList = "EU\t" + fixture.srv.URL + "/one/\tHelsinki, Finland\t1\nthis row is malformed\n"

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(errs, "verbose: mirror list") {
		t.Errorf("malformed rows were reported without -v:\n%s", errs)
	}

	code, _, errs = e.run("xbpretty", "fetch", "--dry-run", "-v")
	if code != 0 {
		t.Fatalf("verbose exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(errs, "verbose: mirror list: line 2") {
		t.Errorf("malformed row was not reported under -v:\n%s", errs)
	}
}

func TestFetchNonFreeWritesTheDerivedRepositoryFile(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)

	code, out, errs := e.run("xbpretty", "fetch", "--non-free")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	derived := filepath.Join(xbpsd, "10-repository-nonfree.conf")
	content, err := os.ReadFile(derived)
	if err != nil {
		t.Fatalf("derived non-free file: %v", err)
	}
	header, body, ok := strings.Cut(string(content), "\n")
	if !ok || !strings.HasPrefix(header, "# Written by xbpretty fetch on ") ||
		!strings.HasSuffix(header, "; revert with 'xbp clean --fetch'.") {
		t.Errorf("derived header = %q", header)
	}
	wantBody := "repository=" + fixture.srv.URL + "/one/current/nonfree\n"
	if body != wantBody {
		t.Errorf("derived body = %q, want %q", body, wantBody)
	}
	info, err := os.Stat(derived)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("derived mode = %o, want 0644", info.Mode().Perm())
	}
	manifest, err := fetch.ReadManifestFor(fetchManifestPath, xbpsd)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("manifest files = %d, want the managed file plus the derived one", len(manifest.Files))
	}
	var derivedEntry *fetch.ManifestFile
	for i := range manifest.Files {
		if manifest.Files[i].Path == derived {
			derivedEntry = &manifest.Files[i]
		}
	}
	if derivedEntry == nil {
		t.Fatalf("manifest lacks the derived file: %+v", manifest.Files)
	}
	if !derivedEntry.Created || derivedEntry.Backup != "" || derivedEntry.SHA256 != fetchSHA256Of(content) {
		t.Errorf("derived manifest entry = %+v", *derivedEntry)
	}
	if strings.Count(out, "Sources have been written to ") != 2 {
		t.Errorf("stdout should name two written files:\n%s", out)
	}
}

func TestFetchTwiceDoesNotDoubleTheMirrorBaseOrStackHeaders(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("first fetch exit = %d\n%s", code, errs)
	}
	first, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	_, firstBody, _ := strings.Cut(string(first), "\n")

	code, _, errs = e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("second fetch exit = %d\n%s", code, errs)
	}
	second, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(second), "# Written by xbpretty fetch on ") != 1 {
		t.Errorf("second fetch stacked provenance headers:\n%s", second)
	}
	_, secondBody, _ := strings.Cut(string(second), "\n")
	if secondBody != firstBody {
		t.Errorf("second fetch changed the body:\nfirst:\n%s\nsecond:\n%s", firstBody, secondBody)
	}
	if strings.Contains(secondBody, "/one/one") {
		t.Errorf("second fetch doubled the mirror's base path:\n%s", secondBody)
	}
	if !strings.Contains(secondBody, "repository="+fixture.srv.URL+"/one/current\n") {
		t.Errorf("second fetch wrote an unexpected repository:\n%s", secondBody)
	}
}

func TestFetchMasksAStockSystemFileUnderEtc(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, _ := newFetchEnv(t, fixture)
	e.root = true
	etcDir := filepath.Join(e.dir, "etc-xbps.d")
	shareDir := filepath.Join(e.dir, "usr-share-xbps.d")
	for _, dir := range []string{etcDir, shareDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const name = "10-repository-main.conf"
	original := []byte("repository=https://repo-default.voidlinux.org/current\n")
	if err := os.WriteFile(filepath.Join(shareDir, name), original, 0o644); err != nil {
		t.Fatal(err)
	}
	fetchXBPSDDirs = []string{etcDir, shareDir}
	fetchWriteDir = etcDir

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	target := filepath.Join(etcDir, name)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the /etc mask was not written: %v", err)
	}
	if !strings.Contains(string(written), "repository="+fixture.srv.URL+"/one/current") {
		t.Errorf("written mask = %q", written)
	}
	if _, err := os.Stat(target + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("masking a /usr/share file created a backup: %v", err)
	}
	manifest, err := fetch.ReadManifestFor(fetchManifestPath, etcDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || !manifest.Files[0].Created || manifest.Files[0].Backup != "" {
		t.Errorf("manifest entry = %+v, want created with no backup", manifest.Files)
	}
	system, err := os.ReadFile(filepath.Join(shareDir, name))
	if err != nil || string(system) != string(original) {
		t.Errorf("/usr/share file was modified: %q %v", system, err)
	}
}

func TestFetchNonFreeSyncErrorOnDerivedURLReverts(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	e.set("XB_STUB_RUN_STDERR", fmt.Sprintf(
		"ERROR: [reposync] failed to fetch file '%s/one/current/nonfree/x86_64-repodata': Not Found\n", fixture.srv.URL))

	code, _, errs := e.run("xbpretty", "fetch", "--non-free")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	restored, err := os.ReadFile(filepath.Join(xbpsd, "10-repository-main.conf"))
	if err != nil || string(restored) != string(original) {
		t.Errorf("derived-URL sync error did not restore the main file: %q %v", restored, err)
	}
	if _, err := os.Stat(filepath.Join(xbpsd, "10-repository-nonfree.conf")); !os.IsNotExist(err) {
		t.Errorf("derived file survived the revert: %v", err)
	}
	if _, err := os.Stat(fetchManifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest survived the revert: %v", err)
	}
}

func TestFetchIgnoresACorruptPreviousManifest(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	writeFetchConf(t, xbpsd)
	if err := os.MkdirAll(filepath.Dir(fetchManifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fetchManifestPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := e.run("xbpretty", "fetch")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 despite a corrupt previous manifest\n%s", code, errs)
	}
	if _, err := fetch.ReadManifestFor(fetchManifestPath, xbpsd); err != nil {
		t.Errorf("fetch did not replace the corrupt manifest: %v", err)
	}
}

func TestFetchCleanEditFetchCleanSnapshotsAfresh(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")

	if code, _, errs := e.run("xbpretty", "fetch"); code != 0 {
		t.Fatalf("first fetch exit = %d\n%s", code, errs)
	}
	if code, _, errs := e.run("xbpretty", "clean", "--fetch"); code != 0 {
		t.Fatalf("first clean exit = %d\n%s", code, errs)
	}
	restored, _ := os.ReadFile(target)
	if string(restored) != string(original) {
		t.Fatalf("first clean did not restore the original:\n%s", restored)
	}
	if _, err := os.Stat(target + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Fatalf("clean left the consumed backup behind: %v", err)
	}

	edited := []byte("repository=https://repo-default.voidlinux.org/current\nignorepkg=hand-edited\n")
	if err := os.WriteFile(target, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := e.run("xbpretty", "fetch"); code != 0 {
		t.Fatalf("second fetch exit = %d\n%s", code, errs)
	}
	if backup, err := os.ReadFile(target + fetch.BackupSuffix); err != nil || string(backup) != string(edited) {
		t.Fatalf("second fetch did not snapshot the edited file: %q %v", backup, err)
	}
	if code, _, errs := e.run("xbpretty", "clean", "--fetch"); code != 0 {
		t.Fatalf("second clean exit = %d\n%s", code, errs)
	}
	restored, _ = os.ReadFile(target)
	if string(restored) != string(edited) {
		t.Errorf("second clean restored %q, want the edited bytes", restored)
	}
}

func TestFetchRevertFailureKeepsTheManifestAndReportsTheStep(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write through a read-only directory")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "10-repository-main.conf")
	if err := os.WriteFile(target, []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var buf bytes.Buffer
	st := &state{stderr: &buf}
	st.revertFetchWrites(
		[]fetch.PreImage{{Path: target, Existed: true, Content: []byte("previous\n")}},
		fetch.Manifest{Files: []fetch.ManifestFile{{Path: target}}},
	)
	if !strings.Contains(buf.String(), "xbpretty: reverting "+target+": ") {
		t.Errorf("revert failure was not reported:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "previous configuration restored") {
		t.Errorf("a failed revert claimed the configuration was restored:\n%s", buf.String())
	}
}

func TestFetchMissingSyncBinaryFailsBeforeWriting(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	missing := filepath.Join(e.dir, "missing-xbps-install")
	e.writeConfig(missing)

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: xbps binary not found: "+missing) {
		t.Errorf("stderr lacks the exact missing-binary error:\n%s", errs)
	}
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	content, err := os.ReadFile(target)
	if err != nil || string(content) != string(original) {
		t.Errorf("a missing binary changed the target: %q %v", content, err)
	}
	if _, err := os.Stat(target + fetch.BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a missing binary created a backup: %v", err)
	}
	if _, err := os.Stat(fetchManifestPath); !os.IsNotExist(err) {
		t.Errorf("a missing binary wrote a manifest: %v", err)
	}
}

func TestFetchNonFreeRefusesToClobberAnExistingDerivedBasename(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	custom := filepath.Join(xbpsd, "10-repository-nonfree.conf")
	customBody := []byte("repository=https://custom.example/nonfree\n")
	if err := os.WriteFile(custom, customBody, 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errs := e.run("xbpretty", "fetch", "--non-free")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	if !strings.Contains(errs, "refusing to overwrite "+custom) {
		t.Errorf("stderr does not name the refused file:\n%s", errs)
	}
	content, err := os.ReadFile(custom)
	if err != nil || string(content) != string(customBody) {
		t.Errorf("custom non-free file was clobbered: %q %v", content, err)
	}
	main, err := os.ReadFile(filepath.Join(xbpsd, "10-repository-main.conf"))
	if err != nil || string(main) != string(original) {
		t.Errorf("the main file changed despite the refusal: %q %v", main, err)
	}
	if _, err := os.Stat(fetchManifestPath); !os.IsNotExist(err) {
		t.Errorf("a refused fetch wrote a manifest: %v", err)
	}
}

func TestFetchFailedRefetchRestoresThePreRunConfiguration(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true
	original := writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")

	if code, _, errs := e.run("xbpretty", "fetch"); code != 0 {
		t.Fatalf("first fetch exit = %d\n%s", code, errs)
	}
	first, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	e.set("XB_STUB_RUN_STDERR", fmt.Sprintf(
		"ERROR: [reposync] failed to fetch file '%s/one/current/x86_64-repodata': Not Found\n", fixture.srv.URL))

	code, _, errs := e.run("xbpretty", "fetch")
	if code != 1 {
		t.Fatalf("second fetch exit = %d, want 1\n%s", code, errs)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(first) {
		t.Errorf("a failed refetch restored the pre-first-fetch file instead of the pre-run configuration:\n%s", after)
	}
	manifest, err := fetch.ReadManifestFor(fetchManifestPath, xbpsd)
	if err != nil {
		t.Fatalf("the first fetch's manifest was not kept: %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Created || !strings.HasSuffix(manifest.Files[0].Backup, fetch.BackupSuffix) {
		t.Errorf("kept manifest = %+v, want the first fetch's entry", manifest.Files)
	}
	backup, err := os.ReadFile(target + fetch.BackupSuffix)
	if err != nil || string(backup) != string(original) {
		t.Errorf("the manifest backup was not kept for clean --fetch: %q %v", backup, err)
	}
	if code, _, errs := e.run("xbpretty", "clean", "--fetch"); code != 0 {
		t.Fatalf("clean after the failed refetch exit = %d\n%s", code, errs)
	}
	restored, _ := os.ReadFile(target)
	if string(restored) != string(original) {
		t.Errorf("clean after the failed refetch = %q, want the original", restored)
	}
}

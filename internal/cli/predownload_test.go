package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/download"
)

// repoFixture is a local HTTP repository: it serves the exact
// `<pkgver>.<arch>.xbps` and `.sig2` paths the plan derives, records every
// request, and can refuse a path or hold a transfer open.
type repoFixture struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	files    map[string][]byte
	statuses map[string]int
	requests []string
	block    chan struct{}
	started  chan string
}

func newRepoFixture(t *testing.T) *repoFixture {
	t.Helper()
	f := &repoFixture{
		t:        t,
		files:    map[string][]byte{},
		statuses: map[string]int{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *repoFixture) URL() string { return f.server.URL }

func (f *repoFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	path := r.URL.Path
	data, ok := f.files[path]
	status := f.statuses[path]
	block, started := f.block, f.started
	f.requests = append(f.requests, path)
	f.mu.Unlock()

	if block != nil {
		if started != nil {
			select {
			case started <- path:
			default:
			}
		}
		<-block
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}

// add registers an archive and its signature and returns the archive's
// SHA-256, the value the repository metadata fixture must carry.
func (f *repoFixture) add(pkgver, arch string, content []byte) string {
	name := pkgver + "." + arch + ".xbps"
	f.mu.Lock()
	f.files["/"+name] = content
	f.files["/"+name+".sig2"] = []byte("signature:" + name)
	f.mu.Unlock()
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// refuse makes one derived path answer with status.
func (f *repoFixture) refuse(path string, status int) {
	f.mu.Lock()
	f.statuses[path] = status
	f.mu.Unlock()
}

func (f *repoFixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *repoFixture) requestedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// planLine is one captured plan row with an exact download size.
func planLine(pkgver, repo string, downloadSize int) string {
	return fmt.Sprintf("%s install x86_64 %s 1000 %d\n", pkgver, repo, downloadSize)
}

// hashLine is one bulk filename-sha256 answer line.
func hashLine(pkgver, sha, repo string) string {
	return fmt.Sprintf("%s: %s (%s)\n", pkgver, sha, repo)
}

// useCacheDir points the predownload pass at dir instead of /var/cache/xbps.
func (e *env) useCacheDir(dir string) {
	e.t.Helper()
	orig := cacheDirFn
	cacheDirFn = func() string { return dir }
	e.t.Cleanup(func() { cacheDirFn = orig })
}

// cacheProbeInstall writes an xbps-install stand-in that snapshots the cache
// directory the moment the real child starts, so a test can prove the archives
// were published before it ran.
func (e *env) cacheProbeInstall(cacheDir, listingPath string) {
	e.t.Helper()
	path := filepath.Join(e.dir, "probe-xbps-install")
	script := "#!/bin/sh\n" +
		"printf '%s %s\\n' \"${0##*/}\" \"$*\" >> \"$XB_STUB_LOG\"\n" +
		"case \"${1:-}\" in\n" +
		"  -*n*) printf '%s' \"${XB_STUB_STDOUT:-}\"; exit 0 ;;\n" +
		"esac\n" +
		"ls -1 \"$XB_CACHE_PROBE_DIR\" > \"$XB_CACHE_PROBE_OUT\"\n" +
		"printf '%s' \"${XB_STUB_RUN_STDOUT-${XB_STUB_STDOUT:-}}\"\n" +
		"exit \"${XB_STUB_EXIT:-0}\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
	e.set("XB_CACHE_PROBE_DIR", cacheDir)
	e.set("XB_CACHE_PROBE_OUT", listingPath)
	e.pointInstallAt(path)
}

// TestAC030PredownloadFillsTheCacheBeforeTheChild pins FR-036 and FR-038: a
// confirmed real transaction fetches every remote archive pair into the xbps
// cache after the checks and before xbps-install starts, with matching hashes.
func TestAC030PredownloadFillsTheCacheBeforeTheChild(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.setAnswers("\n")
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgs := []string{"alpha-1.0_1", "beta-2.0_1", "gamma-3.0_1"}
	var plan, hashes strings.Builder
	contents := map[string][]byte{}
	for i, pkgver := range pkgs {
		content := []byte(fmt.Sprintf("archive-bytes-%d", i))
		contents[pkgver] = content
		sha := f.add(pkgver, "x86_64", content)
		plan.WriteString(planLine(pkgver, f.URL(), len(content)+64))
		hashes.WriteString(hashLine(pkgver, sha, f.URL()))
	}
	e.set("XB_STUB_STDOUT", plan.String())
	e.set("XB_STUB_HASHES", hashes.String())
	listing := filepath.Join(e.dir, "cache-at-child.txt")
	e.cacheProbeInstall(cache, listing)

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(errs, "predownload") {
		t.Errorf("the happy path printed a predownload warning:\n%s", errs)
	}
	for pkgver, content := range contents {
		archive := filepath.Join(cache, pkgver+".x86_64.xbps")
		got, err := os.ReadFile(archive)
		if err != nil {
			t.Errorf("%s was not predownloaded: %v", archive, err)
			continue
		}
		if string(got) != string(content) {
			t.Errorf("%s holds %q, want %q", archive, got, content)
		}
		if _, err := os.Stat(archive + ".sig2"); err != nil {
			t.Errorf("%s.sig2 was not predownloaded: %v", archive, err)
		}
	}
	raw, err := os.ReadFile(listing)
	if err != nil {
		t.Fatalf("the child never recorded the cache: %v", err)
	}
	for _, pkgver := range pkgs {
		pair := pkgver + ".x86_64.xbps"
		if !strings.Contains(string(raw), pair) {
			t.Errorf("the child started before %s was in the cache:\n%s", pair, raw)
		}
	}
}

// TestAC031PredownloadFailureFallsBackToXBPS pins FR-038's accelerator rule:
// one failed archive prints exactly one warning, xbps takes over, and the
// run's exit code is xbps's own.
func TestAC031PredownloadFailureFallsBackToXBPS(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgs := []string{"alpha-1.0_1", "beta-2.0_1", "gamma-3.0_1"}
	var plan, hashes strings.Builder
	for i, pkgver := range pkgs {
		content := []byte(fmt.Sprintf("archive-%d", i))
		sha := f.add(pkgver, "x86_64", content)
		plan.WriteString(planLine(pkgver, f.URL(), len(content)+16))
		hashes.WriteString(hashLine(pkgver, sha, f.URL()))
	}
	f.refuse("/beta-2.0_1.x86_64.xbps.sig2", http.StatusNotFound)
	e.set("XB_STUB_STDOUT", plan.String())
	e.set("XB_STUB_HASHES", hashes.String())
	e.set("XB_STUB_EXIT", "17")

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 17 {
		t.Fatalf("exit = %d, want 17 (xbps's code)\n%s", code, errs)
	}
	const want = "xbpretty: predownload: 1 of 3 archives could not be fetched; xbps will download them\n"
	if got := strings.Count(errs, want); got != 1 {
		t.Errorf("the warning printed %d times, want exactly once:\n%s", got, errs)
	}
	if !ranRealCommand(e) {
		t.Errorf("xbps never took over: %q", e.commands())
	}
}

// TestAC031HashMismatchDeletesPartAndFallsBack pins FR-038's mismatch rule: a
// bad body never publishes, `.part` is gone, and a real transaction falls back
// with the one warning.
func TestAC031HashMismatchDeletesPartAndFallsBack(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("this body does not match the metadata hash")
	f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, strings.Repeat("0", 64), f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (xbps takes over)\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: predownload: 1 of 1 archives could not be fetched; xbps will download them") {
		t.Errorf("missing the fallback warning:\n%s", errs)
	}
	if parts := partFiles(t, cache); len(parts) != 0 {
		t.Errorf("hash mismatch left .part files behind: %q", parts)
	}
	if _, err := os.Stat(filepath.Join(cache, pkgver+".x86_64.xbps")); err == nil {
		t.Error("the mismatching archive was published")
	}
}

// TestAC031DownloadOnlyFailsClosed keeps FR-038's other half: with no child to
// take over, a hash mismatch is the run's outcome, verified files stay.
func TestAC031DownloadOnlyFailsClosed(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	good, bad, later := "alpha-1.0_1", "beta-2.0_1", "gamma-3.0_1"
	goodBytes := []byte("good archive")
	badBytes := []byte("bad archive")
	laterBytes := []byte("later archive")
	goodSHA := f.add(good, "x86_64", goodBytes)
	f.add(bad, "x86_64", badBytes)
	laterSHA := f.add(later, "x86_64", laterBytes)
	plan := planLine(good, f.URL(), len(goodBytes)+16) +
		planLine(bad, f.URL(), len(badBytes)+16) +
		planLine(later, f.URL(), len(laterBytes)+16)
	hashes := hashLine(good, goodSHA, f.URL()) + hashLine(bad, strings.Repeat("0", 64), f.URL()) +
		hashLine(later, laterSHA, f.URL())
	e.set("XB_STUB_STDOUT", plan)
	e.set("XB_STUB_HASHES", hashes)

	code, _, errs := e.run("xbpretty", "install", "figlet", "-d", "-y")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (fail closed)\n%s", code, errs)
	}
	if !strings.Contains(errs, "SHA-256 mismatch") {
		t.Errorf("missing the per-file error:\n%s", errs)
	}
	if got, err := os.ReadFile(filepath.Join(cache, good+".x86_64.xbps")); err != nil || string(got) != string(goodBytes) {
		t.Errorf("the verified archive was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, bad+".x86_64.xbps")); err == nil {
		t.Error("the mismatching archive was published")
	}
	// Fail-closed abort is the first failure: the archive after it is never
	// attempted, so no child would have covered it.
	for _, path := range f.requestedPaths() {
		if strings.Contains(path, later) {
			t.Errorf("the run continued past the first failure: %q", f.requestedPaths())
		}
	}
	if _, err := os.Stat(filepath.Join(cache, later+".x86_64.xbps")); err == nil {
		t.Error("an archive after the first failure was published")
	}
}

// partFiles lists the leftover `.part` files under dir.
func partFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var out []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			out = append(out, entry.Name())
		}
	}
	return out
}

// TestAC030FullyCachedPlanPerformsNoNetwork pins the cache scan: a plan whose
// archive and signature are already valid asks the repository for nothing.
func TestAC030FullyCachedPlanPerformsNoNetwork(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgs := []string{"alpha-1.0_1", "beta-2.0_1"}
	var plan, hashes strings.Builder
	for i, pkgver := range pkgs {
		content := []byte(fmt.Sprintf("cached-%d", i))
		sha := f.add(pkgver, "x86_64", content)
		plan.WriteString(planLine(pkgver, f.URL(), len(content)+16))
		hashes.WriteString(hashLine(pkgver, sha, f.URL()))
		if err := os.WriteFile(filepath.Join(cache, pkgver+".x86_64.xbps"), content, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cache, pkgver+".x86_64.xbps.sig2"), []byte("sig"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.set("XB_STUB_STDOUT", plan.String())
	e.set("XB_STUB_HASHES", hashes.String())

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got := f.requestCount(); got != 0 {
		t.Errorf("a fully cached plan made %d network requests: %q", got, f.requestedPaths())
	}
}

// TestPredownloadRunsAfterThePrompt pins the placement: a refused prompt never
// touches the network (FR-036), so the fill really happens after the checks.
func TestPredownloadRunsAfterThePrompt(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.setAnswers("n\n")
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (refused)\n%s", code, errs)
	}
	if !strings.Contains(errs, "user refused confirmation") {
		t.Errorf("the prompt was not refused:\n%s", errs)
	}
	if got := f.requestCount(); got != 0 {
		t.Errorf("a refused prompt downloaded %d files: %q", got, f.requestedPaths())
	}
}

// TestPredownloadSkippedOnDiskPreflight pins FR-038's disk preflight: no
// transfer starts when the cache filesystem cannot hold the plan, and xbps
// takes over after the skip warning.
func TestPredownloadSkippedOnDiskPreflight(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), 1<<40))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	orig := preflightFn
	preflightFn = func(string, uint64) error { return syscall.ENOSPC }
	t.Cleanup(func() { preflightFn = orig })

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (xbps takes over)\n%s", code, errs)
	}
	if !strings.Contains(errs, "xbpretty: predownload skipped:") ||
		!strings.Contains(errs, "xbps will download the archives") {
		t.Errorf("missing the skip warning:\n%s", errs)
	}
	if got := f.requestCount(); got != 0 {
		t.Errorf("a transfer started despite the failed preflight: %q", f.requestedPaths())
	}
	if !ranRealCommand(e) {
		t.Errorf("xbps never took over: %q", e.commands())
	}
}

// TestDownloadOnlyStopsAfterTheCacheFill pins FR-040/AC-033: the cache is
// filled, then the run stops with no pkgdb write, no log, no history, no
// completion line and no real child.
func TestDownloadOnlyStopsAfterTheCacheFill(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	e.setAnswers("\n")
	cache := t.TempDir()
	e.useCacheDir(cache)
	history := t.TempDir()
	e.set("XBPRETTY_HISTORY_DIR", history)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, out, errs := e.run("xbpretty", "install", "figlet", "--download-only")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got, err := os.ReadFile(filepath.Join(cache, pkgver+".x86_64.xbps")); err != nil || string(got) != string(content) {
		t.Errorf("the cache was not filled: %v", err)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("the normal summary was not printed:\n%s", out)
	}
	for _, line := range []string{"Transaction complete", "Downloads Complete"} {
		if strings.Contains(out, line) || strings.Contains(errs, line) {
			t.Errorf("download-only printed a completion line %q:\n%s", line, out+errs)
		}
	}
	if !strings.Contains(errs, "Do you want to continue? [Y/n] ") {
		t.Errorf("download-only did not prompt like a real transaction:\n%s", errs)
	}
	for _, c := range e.commands() {
		if strings.HasPrefix(c, "-y") {
			t.Errorf("download-only ran a real xbps transaction: %q", c)
		}
	}
	if path, err := config.TransactionLog(); err == nil {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("download-only wrote the transaction log %s", path)
		}
	}
	entries, err := os.ReadDir(history)
	if err != nil {
		t.Fatalf("reading the history dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("download-only wrote history: %v", entries)
	}
}

// TestDownloadOnlyEmptySetIsANoop pins FR-040's verified no-op: a plan with no
// download set stops cleanly after its checks, with no metadata pass and no
// network.
func TestDownloadOnlyEmptySetIsANoop(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	e.set("XB_STUB_STDOUT", removePlanFixture)

	code, _, errs := e.run("xbpretty", "remove", "wget", "--download-only", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(errs, "predownload") {
		t.Errorf("an empty set printed a predownload line:\n%s", errs)
	}
	for _, c := range e.commands() {
		if strings.HasPrefix(c, "-y") {
			t.Errorf("download-only ran a real xbps transaction: %q", c)
		}
		if strings.Contains(c, "-R -s") {
			t.Errorf("an empty set ran the metadata pass: %q", c)
		}
	}
}

// TestDownloadOnlyRespectsDryRun pins FR-040: --dry-run wins and downloads
// nothing, not even into a writable cache.
func TestDownloadOnlyRespectsDryRun(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet", "--dry-run", "--download-only")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got := f.requestCount(); got != 0 {
		t.Errorf("--dry-run downloaded %d files: %q", got, f.requestedPaths())
	}
	if entries, err := os.ReadDir(cache); err != nil || len(entries) != 0 {
		t.Errorf("--dry-run wrote the cache: %v, %v", entries, err)
	}
}

// TestPredownloadDisabledByPassthru pins FR-038's -c/-r rule: a cache or root
// xbpretty did not model disables the fill silently, with only a verbose line.
func TestPredownloadDisabledByPassthru(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y", "-v", "--", "-c", "/tmp/other-cache")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got := f.requestCount(); got != 0 {
		t.Errorf("the -c passthrough still downloaded %d files: %q", got, f.requestedPaths())
	}
	if !strings.Contains(errs, "verbose: predownload disabled by the -c/-r passthrough") {
		t.Errorf("missing the verbose line:\n%s", errs)
	}
	for _, c := range e.commands() {
		if strings.Contains(c, "-R -s") {
			t.Errorf("the metadata pass ran despite the passthrough: %q", c)
		}
	}
}

// TestPredownloadMissingHashWarns pins FR-037: an absent metadata hash still
// fetches, with one warning per package; xbps verifies later.
func TestPredownloadMissingHashWarns(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("unverified archive")
	f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", "")

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	want := pkgver + ": no SHA-256 in repository metadata; xbps will verify it at install\n"
	if !strings.Contains(errs, want) {
		t.Errorf("missing the per-package warning %q:\n%s", want, errs)
	}
	if got, err := os.ReadFile(filepath.Join(cache, pkgver+".x86_64.xbps")); err != nil || string(got) != string(content) {
		t.Errorf("the unverified archive was not fetched: %v", err)
	}
}

// TestPredownloadSignalCancellation pins FR-041: Ctrl-C during the fill
// removes `.part`, starts no child, and exits 130.
func TestPredownloadSignalCancellation(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_EXIT", "0")
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	f.mu.Lock()
	f.block = make(chan struct{})
	f.started = make(chan string, 4)
	started := f.started
	block := f.block
	f.mu.Unlock()

	done := make(chan struct {
		code int
		errs string
	}, 1)
	go func() {
		code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
		done <- struct {
			code int
			errs string
		}{code, errs}
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		close(block)
		t.Fatal("the archive transfer never started")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		close(block)
		t.Fatalf("signalling the test process: %v", err)
	}

	result := <-done
	close(block)
	if result.code != exitInterrupted {
		t.Fatalf("exit = %d, want 130\n%s", result.code, result.errs)
	}
	if parts := partFiles(t, cache); len(parts) != 0 {
		t.Errorf("cancellation left .part files behind: %q", parts)
	}
	if ranRealCommand(e) {
		t.Errorf("a cancelled predownload still ran xbps: %q", e.commands())
	}
}

// TestDownloadOnlyYesSkipsThePrompt pins AC-033's -y path.
func TestDownloadOnlyYesSkipsThePrompt(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet", "-d", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("-y still prompted:\n%s", errs)
	}
	if _, err := os.Stat(filepath.Join(cache, pkgver+".x86_64.xbps")); err != nil {
		t.Errorf("the cache was not filled: %v", err)
	}
}

// TestCacheSeedSinkSeedsTheParser pins FR-041's parser seeding: predownload
// completions continue the child's own counters, and `.sig2` never counts.
func TestCacheSeedSinkSeedsTheParser(t *testing.T) {
	sink := &cacheSeedSink{completed: map[string]int64{}}
	sink.Event(downloadEventFileDone("alpha-1.0_1.x86_64.xbps", 300))
	sink.Event(downloadEventFileDone("alpha-1.0_1.x86_64.xbps.sig2", 20))
	sink.Event(downloadEventCacheHit("beta-2.0_1.x86_64.xbps", 700))

	parser := NewStreamParser(2, 1000)
	sink.Seed(parser)
	state := parser.State()
	if state.DownloadedBytes != 1000 {
		t.Errorf("DownloadedBytes = %d, want 1000 (the two archives, no signatures)", state.DownloadedBytes)
	}
}

func downloadEventFileDone(file string, size int64) download.Event {
	return download.Event{Kind: download.EventFileDone, File: file, Bytes: size, Total: size}
}

func downloadEventCacheHit(file string, size int64) download.Event {
	return download.Event{Kind: download.EventCacheHit, File: file, Bytes: size, Total: size}
}

// TestPredownloadPassthruSpellings pins FR-038's -c/-r rule for both the
// short clusters and xbps's long --cachedir/--rootdir forms: the fill is off
// and only the verbose line says so.
func TestPredownloadPassthruSpellings(t *testing.T) {
	cases := []struct {
		name     string
		passthru []string
	}{
		{"short cachedir", []string{"-c", "/tmp/other-cache"}},
		{"short rootdir", []string{"-r", "/tmp/other-root"}},
		{"short cluster", []string{"-cr", "/tmp/other"}},
		{"long cachedir space", []string{"--cachedir", "/tmp/other-cache"}},
		{"long cachedir equals", []string{"--cachedir=/tmp/other-cache"}},
		{"long rootdir space", []string{"--rootdir", "/tmp/other-root"}},
		{"long rootdir equals", []string{"--rootdir=/tmp/other-root"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			cache := t.TempDir()
			e.useCacheDir(cache)
			f := newRepoFixture(t)

			pkgver := "alpha-1.0_1"
			content := []byte("archive")
			sha := f.add(pkgver, "x86_64", content)
			e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
			e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

			argv := append([]string{"xbpretty", "install", "figlet", "-y", "-v", "--"}, tt.passthru...)
			code, _, errs := e.run(argv...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if got := f.requestCount(); got != 0 {
				t.Errorf("%v still downloaded %d files: %q", tt.passthru, got, f.requestedPaths())
			}
			if !strings.Contains(errs, "verbose: predownload disabled by the -c/-r passthrough") {
				t.Errorf("missing the verbose line:\n%s", errs)
			}
		})
	}
}

// TestPredownloadInvalidFilename pins FR-037's rejection for both surfaces: a
// real transaction falls back with FR-038's skip warning, and a fail-closed
// surface prints the bare FR-037 line and exits 1.
func TestPredownloadInvalidFilename(t *testing.T) {
	pkgver := "evil..pkg-1.0_1"
	setup := func(t *testing.T) (*env, *repoFixture) {
		e := newEnv(t)
		e.root = true
		e.useCacheDir(t.TempDir())
		f := newRepoFixture(t)
		e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), 4096))
		e.set("XB_STUB_HASHES", "")
		return e, f
	}

	t.Run("real transaction warns and falls back", func(t *testing.T) {
		e, f := setup(t)
		code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
		if code != 0 {
			t.Fatalf("exit = %d, want 0 (xbps takes over)\n%s", code, errs)
		}
		want := "xbpretty: predownload skipped: invalid package filename from repository metadata: " + pkgver +
			"; xbps will download the archives\n"
		if !strings.Contains(errs, want) {
			t.Errorf("missing the skip warning %q:\n%s", want, errs)
		}
		if got := f.requestCount(); got != 0 {
			t.Errorf("the invalid target was fetched: %q", f.requestedPaths())
		}
		if !ranRealCommand(e) {
			t.Errorf("xbps never took over: %q", e.commands())
		}
	})

	t.Run("download-only fails closed", func(t *testing.T) {
		e, _ := setup(t)
		code, _, errs := e.run("xbpretty", "install", "figlet", "-d", "-y")
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s", code, errs)
		}
		want := "xbpretty: invalid package filename from repository metadata: " + pkgver + "\n"
		if !strings.Contains(errs, want) {
			t.Errorf("missing the FR-037 line %q:\n%s", want, errs)
		}
	})
}

// TestPredownloadMixedRowExclusions pins FR-036's row rules together: one
// remote row is fetched, ftp gets exactly one warning, and local, hold,
// configure and remove rows are never targets.
func TestPredownloadMixedRowExclusions(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	remote := "alpha-1.0_1"
	content := []byte("remote archive")
	sha := f.add(remote, "x86_64", content)
	plan := planLine(remote, f.URL(), len(content)+16) +
		planLine("beta-1.0_1", "ftp://ftp.example/current", 100) +
		planLine("gamma-1.0_1", "/var/cache/xbps", 100) +
		"delta-1.0_1 hold x86_64 " + f.URL() + " 1000 100\n" +
		"epsilon-1.0_1 configure x86_64 " + f.URL() + " 1000 100\n" +
		"zeta-1.0_1 remove x86_64 " + f.URL() + " 1000 100\n"
	e.set("XB_STUB_STDOUT", plan)
	e.set("XB_STUB_HASHES", hashLine(remote, sha, f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	const ftpWarning = "1 archive(s) from ftp:// repositories were not predownloaded; xbps will fetch them\n"
	if !strings.Contains(errs, ftpWarning) {
		t.Errorf("missing the ftp warning %q:\n%s", ftpWarning, errs)
	}
	if strings.Contains(errs, "gamma-1.0_1") {
		t.Errorf("a local row produced a warning:\n%s", errs)
	}
	wantPaths := []string{
		"/alpha-1.0_1.x86_64.xbps.sig2",
		"/alpha-1.0_1.x86_64.xbps",
	}
	if got := f.requestedPaths(); !equalStrings(got, wantPaths) {
		t.Errorf("requested %q, want exactly the remote pair %q", got, wantPaths)
	}
}

// TestDownloadOnlyAllCached pins FR-040's all-cached case: no network, no
// output, exit 0.
func TestDownloadOnlyAllCached(t *testing.T) {
	e := newEnv(t)
	e.root = true
	cache := t.TempDir()
	e.useCacheDir(cache)
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("cached archive")
	sha := f.add(pkgver, "x86_64", content)
	if err := os.WriteFile(filepath.Join(cache, pkgver+".x86_64.xbps"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, pkgver+".x86_64.xbps.sig2"), []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, out, errs := e.run("xbpretty", "install", "figlet", "-d", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got := f.requestCount(); got != 0 {
		t.Errorf("an all-cached download-only run made %d requests: %q", got, f.requestedPaths())
	}
	if strings.Contains(errs, "predownload") {
		t.Errorf("an all-cached run printed a predownload line:\n%s", errs)
	}
	if !strings.Contains(out, "Install") {
		t.Errorf("the summary is missing:\n%s", out)
	}
}

// TestDownloadOnlyMissingSignatureFailsClosed pins FR-040: a missing .sig2 on
// a remote repository is a failure, not a fallback, when no child takes over.
func TestDownloadOnlyMissingSignatureFailsClosed(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.useCacheDir(t.TempDir())
	f := newRepoFixture(t)

	pkgver := "alpha-1.0_1"
	content := []byte("archive")
	sha := f.add(pkgver, "x86_64", content)
	f.refuse("/"+pkgver+".x86_64.xbps.sig2", http.StatusNotFound)
	e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
	e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

	code, _, errs := e.run("xbpretty", "install", "figlet", "-d", "-y")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, errs)
	}
	if !strings.Contains(errs, "the repository no longer serves the signature") {
		t.Errorf("missing the per-file signature error:\n%s", errs)
	}
}

// TestPredownloadMetadataFailure pins FR-038's metadata-failure shape: the
// bulk hash pass failing skips the fill with the skip warning for a real
// transaction and fails closed for --download-only.
func TestPredownloadMetadataFailure(t *testing.T) {
	setup := func(t *testing.T) (*env, *repoFixture) {
		e := newEnv(t)
		e.root = true
		e.useCacheDir(t.TempDir())
		f := newRepoFixture(t)
		pkgver := "alpha-1.0_1"
		content := []byte("archive")
		sha := f.add(pkgver, "x86_64", content)
		e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
		e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))
		e.set("XB_STUB_HASH_EXIT", "1")
		return e, f
	}

	t.Run("real transaction warns and falls back", func(t *testing.T) {
		e, f := setup(t)
		code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
		if code != 0 {
			t.Fatalf("exit = %d, want 0 (xbps takes over)\n%s", code, errs)
		}
		want := "xbpretty: predownload skipped: repository metadata unavailable: xbps-query exited 1; xbps will download the archives\n"
		if !strings.Contains(errs, want) {
			t.Errorf("missing the skip warning %q:\n%s", want, errs)
		}
		if got := f.requestCount(); got != 0 {
			t.Errorf("a transfer ran without metadata: %q", f.requestedPaths())
		}
	})

	t.Run("download-only fails closed", func(t *testing.T) {
		e, _ := setup(t)
		code, _, errs := e.run("xbpretty", "install", "figlet", "-d", "-y")
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s", code, errs)
		}
		if !strings.Contains(errs, "xbpretty: predownload: repository metadata unavailable: xbps-query exited 1") {
			t.Errorf("missing the fail-closed error:\n%s", errs)
		}
	})
}

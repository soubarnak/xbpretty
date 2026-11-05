package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sha256Hex is the independent expected value the fixtures assert against.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fixture is a local archive repository the engine can talk to. It records
// every request and can fail, delay or truncate per path.
type fixture struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	requests  []string
	active    int
	maxActive int

	// handler decides the answer for a path. Nil serves okBody.
	handler func(w http.ResponseWriter, r *http.Request) bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		f.active++
		if f.active > f.maxActive {
			f.maxActive = f.active
		}
		handler := f.handler
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			f.active--
			f.mu.Unlock()
		}()
		if handler != nil && handler(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("archive:" + filepath.Base(r.URL.Path)))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// serveBody replaces the handler with a static body per path suffix.
func (f *fixture) serveBody(bodies map[string][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = func(w http.ResponseWriter, r *http.Request) bool {
		body, ok := bodies[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return true
	}
}

// targetFor builds a target against the fixture's /current repository.
func targetFor(t *testing.T, repository string, data []byte, hash string) Target {
	t.Helper()
	target, err := NewTarget("figlet", "figlet-2.2.5_6", repository, "x86_64", hash, uint64(len(data)))
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	return target
}

func newTestEngine(dir string, mode Mode) *Engine {
	return New(Options{
		DestDir:       dir,
		Mode:          mode,
		UserAgent:     "xbpretty/test",
		Concurrency:   4,
		Attempts:      4,
		DialTimeout:   2 * time.Second,
		HeaderTimeout: 2 * time.Second,
		IdleTimeout:   2 * time.Second,
	})
}

// TestFetchSignatureThenArchiveAndPublish pins the happy path: signature URL
// first, archive second, SHA-256 verified, `.part` published by rename with
// cache permissions and no residue.
func TestFetchSignatureThenArchiveAndPublish(t *testing.T) {
	f := newFixture(t)
	archive := []byte("the archive bytes")
	signature := []byte("the signature bytes")
	f.serveBody(map[string][]byte{
		"figlet-2.2.5_6.x86_64.xbps":      archive,
		"figlet-2.2.5_6.x86_64.xbps.sig2": signature,
	})
	dir := t.TempDir()
	engine := newTestEngine(dir, ModeDownload)
	target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))

	results := engine.Fetch(context.Background(), []Target{target})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("Fetch: %+v", results)
	}
	if results[0].ReusedArchive || results[0].ReusedSignature {
		t.Errorf("a fresh fetch reported a cache hit: %+v", results[0])
	}
	got, err := os.ReadFile(filepath.Join(dir, target.Filename))
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if string(got) != string(archive) {
		t.Errorf("archive = %q, want %q", got, archive)
	}
	sig, err := os.ReadFile(filepath.Join(dir, target.Filename+".sig2"))
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	if string(sig) != string(signature) {
		t.Errorf("signature = %q", sig)
	}
	info, err := os.Stat(filepath.Join(dir, target.Filename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			t.Errorf("residue left behind: %s", entry.Name())
		}
	}
	f.mu.Lock()
	requests := append([]string(nil), f.requests...)
	f.mu.Unlock()
	if len(requests) != 2 || requests[0] != "/current/"+target.Filename+".sig2" || requests[1] != "/current/"+target.Filename {
		t.Errorf("requests = %v, want signature then archive", requests)
	}
}

// TestFetchVerifiesSHA256AndRemovesPart pins the mismatch path: the archive is
// never published, `.part` is deleted, and the message names both hashes.
func TestFetchVerifiesSHA256AndRemovesPart(t *testing.T) {
	f := newFixture(t)
	served := []byte("tampered bytes")
	want := sha256Hex([]byte("expected bytes"))
	f.serveBody(map[string][]byte{
		"figlet-2.2.5_6.x86_64.xbps":      served,
		"figlet-2.2.5_6.x86_64.xbps.sig2": []byte("sig"),
	})
	dir := t.TempDir()
	engine := newTestEngine(dir, ModeDownload)
	target := targetFor(t, f.srv.URL+"/current", []byte("expected bytes"), want)

	results := engine.Fetch(context.Background(), []Target{target})
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("Fetch accepted a hash mismatch: %+v", results)
	}
	msg := results[0].Err.Error()
	if !strings.Contains(msg, "SHA-256 mismatch: expected "+want) || !strings.Contains(msg, "got "+sha256Hex(served)) {
		t.Errorf("error = %q, want both hashes", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, target.Filename)); !os.IsNotExist(err) {
		t.Error("the mismatching archive was published")
	}
	if _, err := os.Stat(filepath.Join(dir, target.Filename+".part")); !os.IsNotExist(err) {
		t.Error(".part survived the hash mismatch")
	}
}

// TestFetchRetriesServerErrorsButNotNotFound pins FR-038's retry classes: 5xx
// gets four attempts at most and can recover; 404 is one attempt.
func TestFetchRetriesServerErrorsButNotNotFound(t *testing.T) {
	t.Run("5xx then success", func(t *testing.T) {
		f := newFixture(t)
		archive := []byte("archive")
		var archiveHits atomic.Int32
		f.mu.Lock()
		f.handler = func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, ".sig2") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("sig"))
				return true
			}
			if archiveHits.Add(1) <= 2 {
				http.Error(w, "temporary", http.StatusServiceUnavailable)
				return true
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(archive)
			return true
		}
		f.mu.Unlock()
		engine := newTestEngine(t.TempDir(), ModeDownload)
		target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
		results := engine.Fetch(context.Background(), []Target{target})
		if results[0].Err != nil {
			t.Fatalf("Fetch: %v", results[0].Err)
		}
		if got := archiveHits.Load(); got != 3 {
			t.Errorf("archive attempts = %d, want 3", got)
		}
	})

	t.Run("404 is not retried", func(t *testing.T) {
		f := newFixture(t)
		f.mu.Lock()
		f.handler = func(w http.ResponseWriter, r *http.Request) bool {
			http.NotFound(w, r)
			return true
		}
		f.mu.Unlock()
		engine := newTestEngine(t.TempDir(), ModeDownload)
		target := targetFor(t, f.srv.URL+"/current", []byte("x"), sha256Hex([]byte("x")))
		results := engine.Fetch(context.Background(), []Target{target})
		if results[0].Err == nil {
			t.Fatal("a 404 was accepted")
		}
		// Signature first, one attempt; the archive is never requested.
		if got := f.requestCount(); got != 1 {
			t.Errorf("requests = %d, want 1 (signature 404, no retry)", got)
		}
	})
}

// TestFetchGivesUpAfterFourAttempts pins the attempt ceiling.
func TestFetchGivesUpAfterFourAttempts(t *testing.T) {
	f := newFixture(t)
	var hits atomic.Int32
	f.mu.Lock()
	f.handler = func(w http.ResponseWriter, r *http.Request) bool {
		hits.Add(1)
		http.Error(w, "down", http.StatusInternalServerError)
		return true
	}
	f.mu.Unlock()
	engine := newTestEngine(t.TempDir(), ModeDownload)
	target := targetFor(t, f.srv.URL+"/current", []byte("x"), sha256Hex([]byte("x")))
	results := engine.Fetch(context.Background(), []Target{target})
	if results[0].Err == nil {
		t.Fatal("an always-failing host was accepted")
	}
	if got := hits.Load(); got != 4 {
		t.Errorf("attempts = %d, want 4", got)
	}
}

// TestFetchCapsConcurrentTransfersPerHost pins three transfers per host:port,
// with no global cap.
func TestFetchCapsConcurrentTransfersPerHost(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.handler = func(w http.ResponseWriter, r *http.Request) bool {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body"))
		return true
	}
	f.mu.Unlock()

	var targets []Target
	for i := 0; i < 6; i++ {
		target, err := NewTarget(fmt.Sprintf("pkg%d", i), fmt.Sprintf("pkg%d-1.0_1", i),
			f.srv.URL+"/current", "x86_64", "", 4)
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
	}
	engine := newTestEngine(t.TempDir(), ModeDownload)
	results := engine.Fetch(context.Background(), targets)
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("Fetch: %v", result.Err)
		}
	}
	f.mu.Lock()
	maxActive := f.maxActive
	f.mu.Unlock()
	if maxActive != 3 {
		t.Errorf("max concurrent transfers = %d, want 3", maxActive)
	}
}

// TestFetchCacheScan pins FR-038's cache rules: a matching pair is a hit, a
// missing signature is fetched alone, and a hash mismatch refetches both.
func TestFetchCacheScan(t *testing.T) {
	archive := []byte("archive bytes")
	sig := []byte("signature")

	t.Run("both present is a hit", func(t *testing.T) {
		f := newFixture(t)
		f.serveBody(map[string][]byte{"figlet-2.2.5_6.x86_64.xbps": archive, "figlet-2.2.5_6.x86_64.xbps.sig2": sig})
		dir := t.TempDir()
		target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
		writeFile(t, dir, target.Filename, archive)
		writeFile(t, dir, target.Filename+".sig2", sig)
		results := newTestEngine(dir, ModeCache).Fetch(context.Background(), []Target{target})
		if results[0].Err != nil {
			t.Fatalf("Fetch: %v", results[0].Err)
		}
		if !results[0].ReusedArchive || !results[0].ReusedSignature {
			t.Errorf("cache hit not reported: %+v", results[0])
		}
		if got := f.requestCount(); got != 0 {
			t.Errorf("a fully cached package made %d request(s)", got)
		}
	})

	t.Run("missing signature is fetched alone", func(t *testing.T) {
		f := newFixture(t)
		f.serveBody(map[string][]byte{"figlet-2.2.5_6.x86_64.xbps": archive, "figlet-2.2.5_6.x86_64.xbps.sig2": sig})
		dir := t.TempDir()
		target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
		writeFile(t, dir, target.Filename, archive)
		results := newTestEngine(dir, ModeCache).Fetch(context.Background(), []Target{target})
		if results[0].Err != nil {
			t.Fatalf("Fetch: %v", results[0].Err)
		}
		if !results[0].ReusedArchive || results[0].ReusedSignature {
			t.Errorf("cache scan = %+v", results[0])
		}
		f.mu.Lock()
		requests := append([]string(nil), f.requests...)
		f.mu.Unlock()
		if len(requests) != 1 || !strings.HasSuffix(requests[0], ".sig2") {
			t.Errorf("requests = %v, want only the signature", requests)
		}
	})

	t.Run("hash mismatch refetches both", func(t *testing.T) {
		f := newFixture(t)
		f.serveBody(map[string][]byte{"figlet-2.2.5_6.x86_64.xbps": archive, "figlet-2.2.5_6.x86_64.xbps.sig2": sig})
		dir := t.TempDir()
		target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
		writeFile(t, dir, target.Filename, []byte("stale archive"))
		writeFile(t, dir, target.Filename+".sig2", []byte("stale sig"))
		results := newTestEngine(dir, ModeCache).Fetch(context.Background(), []Target{target})
		if results[0].Err != nil {
			t.Fatalf("Fetch: %v", results[0].Err)
		}
		if got := f.requestCount(); got != 2 {
			t.Errorf("requests = %d, want the pair", got)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, target.Filename)); string(got) != string(archive) {
			t.Errorf("archive not replaced: %q", got)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, target.Filename+".sig2")); string(got) != string(sig) {
			t.Errorf("signature not replaced: %q", got)
		}
	})
}

// TestFetchSignature404Message pins FR-037's exact missing-signature line.
func TestFetchSignature404Message(t *testing.T) {
	f := newFixture(t)
	archive := []byte("archive")
	f.mu.Lock()
	f.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, ".sig2") {
			http.NotFound(w, r)
			return true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(archive)
		return true
	}
	f.mu.Unlock()
	engine := newTestEngine(t.TempDir(), ModeDownload)
	target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
	results := engine.Fetch(context.Background(), []Target{target})
	if results[0].Err == nil {
		t.Fatal("a missing signature was accepted")
	}
	want := target.Filename + ".sig2: 404 Not Found; the repository no longer serves the signature"
	if results[0].Err.Error() != want {
		t.Errorf("error = %q, want %q", results[0].Err, want)
	}
}

// TestFetchLocalRepositoryCopies pins FR-037's local/file:// behaviour:
// `download` copies the archive and a signature when present, verifies the
// SHA-256, and never goes near the network.
func TestFetchLocalRepositoryCopies(t *testing.T) {
	archive := []byte("local archive")
	dir := t.TempDir()
	src := filepath.Join(dir, "repo")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	target, err := NewTarget("figlet", "figlet-2.2.5_6", src, "x86_64", sha256Hex(archive), uint64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, target.Filename, archive)
	writeFile(t, src, target.Filename+".sig2", []byte("sig"))
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	results := newTestEngine(dest, ModeDownload).Fetch(context.Background(), []Target{target})
	if results[0].Err != nil {
		t.Fatalf("Fetch: %v", results[0].Err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, target.Filename)); string(got) != string(archive) {
		t.Errorf("archive = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, target.Filename+".sig2")); string(got) != "sig" {
		t.Errorf("signature = %q", got)
	}

	// file:// works the same way, and a missing signature is allowed.
	srcURL := "file://" + src
	targetURL, err := NewTarget("figlet", "figlet-2.2.5_6", srcURL, "x86_64", sha256Hex(archive), uint64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	dest2 := t.TempDir()
	results = newTestEngine(dest2, ModeDownload).Fetch(context.Background(), []Target{targetURL})
	if results[0].Err != nil {
		t.Fatalf("file:// Fetch: %v", results[0].Err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest2, target.Filename)); string(got) != string(archive) {
		t.Errorf("file:// archive = %q", got)
	}

	// A local hash mismatch is refused and publishes nothing.
	bad, err := NewTarget("figlet", "figlet-2.2.5_6", src, "x86_64", sha256Hex([]byte("other")), uint64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	dest3 := t.TempDir()
	results = newTestEngine(dest3, ModeDownload).Fetch(context.Background(), []Target{bad})
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "SHA-256 mismatch") {
		t.Fatalf("local mismatch = %v", results[0].Err)
	}
	if _, err := os.Stat(filepath.Join(dest3, target.Filename)); !os.IsNotExist(err) {
		t.Error("the mismatching local archive was published")
	}
}

// TestFetchStalePartIsTruncated pins the `.part` rule: an interrupted earlier
// run's residue never corrupts the next download.
func TestFetchStalePartIsTruncated(t *testing.T) {
	f := newFixture(t)
	archive := []byte("fresh")
	f.serveBody(map[string][]byte{
		"figlet-2.2.5_6.x86_64.xbps":      archive,
		"figlet-2.2.5_6.x86_64.xbps.sig2": []byte("sig"),
	})
	dir := t.TempDir()
	target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
	writeFile(t, dir, target.Filename+".part", []byte("a much longer stale prefix that would corrupt an append"))
	results := newTestEngine(dir, ModeDownload).Fetch(context.Background(), []Target{target})
	if results[0].Err != nil {
		t.Fatalf("Fetch: %v", results[0].Err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, target.Filename)); string(got) != string(archive) {
		t.Errorf("archive = %q, want %q", got, archive)
	}
	if _, err := os.Stat(filepath.Join(dir, target.Filename+".part")); !os.IsNotExist(err) {
		t.Error(".part survived")
	}
}

// TestFetchRejectsFTPRepositories pins the ftp exclusion at the engine seam
// #72 and the download command both rely on.
func TestFetchRejectsFTPRepositories(t *testing.T) {
	target, err := NewTarget("figlet", "figlet-2.2.5_6", "ftp://ftp.example/current", "x86_64", "abc", 1)
	if err != nil {
		t.Fatal(err)
	}
	results := newTestEngine(t.TempDir(), ModeDownload).Fetch(context.Background(), []Target{target})
	if results[0].Err == nil {
		t.Fatal("an ftp repository was fetched")
	}
	want := "figlet-2.2.5_6: ftp:// repositories are not supported by xbp download"
	if results[0].Err.Error() != want {
		t.Errorf("error = %q, want %q", results[0].Err, want)
	}
}

func writeFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFetchRetryStatusClasses pins FR-038's status codes: 408, 429 and 5xx are
// retried; other 4xx answers are final.
func TestFetchRetryStatusClasses(t *testing.T) {
	for _, tc := range []struct {
		status  int
		retried bool
	}{
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusBadRequest, false},
		{http.StatusForbidden, false},
	} {
		t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
			f := newFixture(t)
			archive := []byte("archive")
			var hits atomic.Int32
			f.mu.Lock()
			f.handler = func(w http.ResponseWriter, r *http.Request) bool {
				if strings.HasSuffix(r.URL.Path, ".sig2") {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("sig"))
					return true
				}
				if hits.Add(1) <= 2 {
					http.Error(w, "noluck", tc.status)
					return true
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(archive)
				return true
			}
			f.mu.Unlock()
			engine := newTestEngine(t.TempDir(), ModeDownload)
			target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
			results := engine.Fetch(context.Background(), []Target{target})
			if tc.retried {
				if results[0].Err != nil {
					t.Fatalf("Fetch after a retryable %d: %v", tc.status, results[0].Err)
				}
				if got := hits.Load(); got != 3 {
					t.Errorf("%d attempts = %d, want 3", tc.status, got)
				}
			} else {
				if results[0].Err == nil {
					t.Fatalf("Fetch accepted a final %d", tc.status)
				}
				if got := hits.Load(); got != 1 {
					t.Errorf("%d attempts = %d, want 1", tc.status, got)
				}
			}
		})
	}
}

// TestFetchRefusesHTTPSDowngradeRedirect pins FR-038's redirect rule and the
// ten-redirect budget share one policy, including for an injected client.
func TestFetchRefusesHTTPSDowngradeRedirect(t *testing.T) {
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHits.Add(1)
		_, _ = w.Write([]byte("should not be reached"))
	}))
	defer plain.Close()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer tlsSrv.Close()

	archive := []byte("archive")
	target := targetFor(t, tlsSrv.URL+"/current", archive, sha256Hex(archive))
	var attempts atomic.Int32
	engine := New(Options{
		DestDir: t.TempDir(),
		Mode:    ModeDownload,
		Client:  tlsSrv.Client(),
		Log: func(format string, args ...any) {
			attempts.Add(1)
		},
	})
	results := engine.Fetch(context.Background(), []Target{target})
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "refusing to follow an https to http redirect") {
		t.Fatalf("redirect error = %v", results[0].Err)
	}
	if plainHits.Load() != 0 {
		t.Error("the https→http redirect was followed")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (a downgrade is not retried)", attempts.Load())
	}
}

// TestFetchDoesNotRetryTLSCertificateFailures pins that system-root
// verification failures are final, not a transient error.
func TestFetchDoesNotRetryTLSCertificateFailures(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("archive"))
	}))
	defer tlsSrv.Close()
	target := targetFor(t, tlsSrv.URL+"/current", []byte("archive"), sha256Hex([]byte("archive")))
	var attempts atomic.Int32
	engine := New(Options{
		DestDir: t.TempDir(),
		Mode:    ModeDownload,
		Log: func(format string, args ...any) {
			attempts.Add(1)
		},
	})
	results := engine.Fetch(context.Background(), []Target{target})
	if results[0].Err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (a TLS failure is not retried)", attempts.Load())
	}
}

// TestFetchPublishesByAtomicRename pins that the destination only ever
// appears complete: while the transfer runs, the bytes live in `.part`.
func TestFetchPublishesByAtomicRename(t *testing.T) {
	dir := t.TempDir()
	archive := []byte("archive")
	var target Target
	var sawDest, sawPart bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig2") {
			_, _ = w.Write([]byte("sig"))
			return
		}
		_, destErr := os.Stat(filepath.Join(dir, target.Filename))
		_, partErr := os.Stat(filepath.Join(dir, target.Filename+".part"))
		sawDest = destErr == nil
		sawPart = partErr == nil
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	var err error
	target, err = NewTarget("figlet", "figlet-2.2.5_6", srv.URL+"/current", "x86_64", sha256Hex(archive), uint64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	results := newTestEngine(dir, ModeDownload).Fetch(context.Background(), []Target{target})
	if results[0].Err != nil {
		t.Fatalf("Fetch: %v", results[0].Err)
	}
	if sawDest {
		t.Error("the destination existed before the rename")
	}
	if !sawPart {
		t.Error("the in-flight bytes were not in .part")
	}
}

// TestFetchReusedArchiveFetchesMissingSignature pins FR-039's rule for the
// download surface: a reused archive still gets a missing signature.
func TestFetchReusedArchiveFetchesMissingSignature(t *testing.T) {
	f := newFixture(t)
	archive := []byte("archive")
	sig := []byte("sig")
	f.serveBody(map[string][]byte{"figlet-2.2.5_6.x86_64.xbps": archive, "figlet-2.2.5_6.x86_64.xbps.sig2": sig})
	dir := t.TempDir()
	target := targetFor(t, f.srv.URL+"/current", archive, sha256Hex(archive))
	writeFile(t, dir, target.Filename, archive)
	results := newTestEngine(dir, ModeDownload).Fetch(context.Background(), []Target{target})
	if results[0].Err != nil {
		t.Fatalf("Fetch: %v", results[0].Err)
	}
	if !results[0].ReusedArchive || results[0].ReusedSignature {
		t.Errorf("result = %+v", results[0])
	}
	f.mu.Lock()
	requests := append([]string(nil), f.requests...)
	f.mu.Unlock()
	if len(requests) != 1 || !strings.HasSuffix(requests[0], ".sig2") {
		t.Errorf("requests = %v, want only the signature", requests)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, target.Filename+".sig2")); string(got) != string(sig) {
		t.Errorf("signature = %q", got)
	}
}

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// archiveServer is a local repository serving one archive and its signature.
type archiveServer struct {
	t    *testing.T
	srv  *httptest.Server
	body []byte
	sig  []byte

	mu       sync.Mutex
	requests []string
}

func newArchiveServer(t *testing.T, body, sig []byte) *archiveServer {
	t.Helper()
	a := &archiveServer{t: t, body: body, sig: sig}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.requests = append(a.requests, r.URL.Path)
		a.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, ".sig2") {
			if a.sig == nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(a.sig)
			return
		}
		_, _ = w.Write(a.body)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *archiveServer) requestCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.requests)
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// downloadFixture points the stub query at one resolvable name.
func downloadFixture(t *testing.T, e *env, repository, pkgver, arch, hash string) {
	t.Helper()
	e.set("XB_STUB_REPOS", repository+"\n")
	e.set("XB_STUB_RESOLVE", strings.Join([]string{pkgver, repository, arch, hash}, "\n"))
}

// noTransactionRan proves `download` used no root, sync, prompt or real
// xbps transaction.
func noTransactionRan(t *testing.T, e *env) {
	t.Helper()
	for _, line := range e.commandsWithBinary() {
		binary, args := splitStubLine(line)
		if binary != "xbps-query" {
			t.Errorf("download ran %s: %q", binary, args)
		}
		if args == "-S" || strings.HasPrefix(args, "-S ") || strings.HasPrefix(args, "-y") {
			t.Errorf("download ran a sync or transaction: %q", args)
		}
	}
}

// TestDownloadWritesArchiveAndSignatureToCwd pins FR-039 and AC-032: no root,
// no prompt, the archive and .sig2 in the current directory, the exact success
// block, and no cache/log/history.
func TestDownloadWritesArchiveAndSignatureToCwd(t *testing.T) {
	body := []byte("figlet archive")
	sig := []byte("figlet signature")
	server := newArchiveServer(t, body, sig)
	e := newEnv(t)
	e.root = false
	e.tty = false
	t.Chdir(t.TempDir())
	downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))

	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, errs)
	}
	want := "Downloads Complete:\n  figlet-2.2.5_6.x86_64.xbps was written to ./figlet-2.2.5_6.x86_64.xbps\n"
	if out != want {
		t.Errorf("stdout = %q, want exactly %q", out, want)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want nothing", errs)
	}
	for name, want := range map[string][]byte{
		"figlet-2.2.5_6.x86_64.xbps":      body,
		"figlet-2.2.5_6.x86_64.xbps.sig2": sig,
	} {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
		info, _ := os.Stat(name)
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %o, want 644", name, info.Mode().Perm())
		}
	}
	noTransactionRan(t, e)
	logPath := filepath.Join(os.Getenv("XDG_STATE_HOME"), "xbpretty", "transactions.log")
	if _, err := os.Stat(logPath); err == nil {
		t.Error("download wrote a transaction log line")
	}
}

// TestDownloadReportsMissingNamesAndDownloadsTheRest pins AC-032's partial
// run: the resolvable package is written, both stderr lines print, no success
// block appears and the exit code is 1.
func TestDownloadReportsMissingNamesAndDownloadsTheRest(t *testing.T) {
	body := []byte("figlet archive")
	server := newArchiveServer(t, body, []byte("sig"))
	e := newEnv(t)
	e.root = false
	t.Chdir(t.TempDir())
	downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
	e.set("XB_STUB_MISSING", "nonsense")

	code, out, errs := e.run("xbpretty", "download", "figlet", "nonsense")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want no block on a partial run", out)
	}
	for _, want := range []string{
		"xbpretty: nonsense: not found in any configured repository\n",
		"xbpretty: Some packages were not found.\n",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr missing %q:\n%s", want, errs)
		}
	}
	if _, err := os.Stat("figlet-2.2.5_6.x86_64.xbps"); err != nil {
		t.Errorf("the resolvable package was not downloaded: %v", err)
	}
}

// TestDownloadNeedsRepositories pins FR-039's empty-repository refusal.
func TestDownloadNeedsRepositories(t *testing.T) {
	e := newEnv(t)
	t.Chdir(t.TempDir())
	e.set("XB_STUB_REPOS", "")
	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	want := "xbpretty: no repositories are configured; add one in /etc/xbps.d\n"
	if errs != want {
		t.Errorf("stderr = %q, want %q", errs, want)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

// TestDownloadRefusesUnverifiableMetadata pins FR-037's hash-less refusal: a
// cwd archive without a metadata hash is not written.
func TestDownloadRefusesUnverifiableMetadata(t *testing.T) {
	body := []byte("archive")
	server := newArchiveServer(t, body, []byte("sig"))
	e := newEnv(t)
	t.Chdir(t.TempDir())
	e.set("XB_STUB_REPOS", server.srv.URL+"/current\n")
	e.set("XB_STUB_RESOLVE", "figlet-2.2.5_6\n"+server.srv.URL+"/current\nx86_64\n")
	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	want := "xbpretty: figlet-2.2.5_6: repository metadata has no SHA-256; refusing to write an unverifiable package\n"
	if errs != want {
		t.Errorf("stderr = %q, want %q", errs, want)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if _, err := os.Stat("figlet-2.2.5_6.x86_64.xbps"); !os.IsNotExist(err) {
		t.Error("an unverifiable archive was written")
	}
	if server.requestCount() != 0 {
		t.Error("a request was made before the refusal")
	}
}

// TestDownloadRefusesFTPRepositories pins FR-037's ftp exclusion.
func TestDownloadRefusesFTPRepositories(t *testing.T) {
	e := newEnv(t)
	t.Chdir(t.TempDir())
	e.set("XB_STUB_REPOS", "ftp://ftp.example/current\n")
	e.set("XB_STUB_RESOLVE", "figlet-2.2.5_6\nftp://ftp.example/current\nx86_64\n"+sha256Of([]byte("x")))
	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	want := "xbpretty: figlet-2.2.5_6: ftp:// repositories are not supported by xbp download\n"
	if errs != want {
		t.Errorf("stderr = %q, want %q", errs, want)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
}

// TestDownloadReusesMatchingArchivesNoNetwork pins FR-039's cache reuse.
func TestDownloadReusesMatchingArchivesNoNetwork(t *testing.T) {
	body := []byte("cached archive")
	sig := []byte("cached signature")
	server := newArchiveServer(t, body, sig)
	e := newEnv(t)
	t.Chdir(t.TempDir())
	downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
	if err := os.WriteFile("figlet-2.2.5_6.x86_64.xbps", body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("figlet-2.2.5_6.x86_64.xbps.sig2", sig, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "figlet-2.2.5_6.x86_64.xbps was written") {
		t.Errorf("stdout = %q", out)
	}
	if got := server.requestCount(); got != 0 {
		t.Errorf("a fully cached download made %d request(s)", got)
	}
}

// TestDownloadOverwritesMismatchedArchive pins FR-039's overwrite rule.
func TestDownloadOverwritesMismatchedArchive(t *testing.T) {
	body := []byte("fresh archive")
	sig := []byte("fresh signature")
	server := newArchiveServer(t, body, sig)
	e := newEnv(t)
	t.Chdir(t.TempDir())
	downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
	if err := os.WriteFile("figlet-2.2.5_6.x86_64.xbps", []byte("stale archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("figlet-2.2.5_6.x86_64.xbps.sig2", []byte("stale sig"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := e.run("xbpretty", "download", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps"); string(got) != string(body) {
		t.Errorf("archive = %q, want the freshly downloaded bytes", got)
	}
	if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps.sig2"); string(got) != string(sig) {
		t.Errorf("signature = %q, want the fresh signature", got)
	}
	if got := server.requestCount(); got != 2 {
		t.Errorf("requests = %d, want the pair", got)
	}
}

// TestDownloadCopiesLocalRepositories pins FR-037's local/file:// copy.
func TestDownloadCopiesLocalRepositories(t *testing.T) {
	body := []byte("local archive")
	dir := t.TempDir()
	src := filepath.Join(dir, "repo")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "figlet-2.2.5_6.x86_64.xbps"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "figlet-2.2.5_6.x86_64.xbps.sig2"), []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, repository := range []string{src, "file://" + src} {
		t.Run(repository, func(t *testing.T) {
			e := newEnv(t)
			t.Chdir(t.TempDir())
			downloadFixture(t, e, repository, "figlet-2.2.5_6", "x86_64", sha256Of(body))
			code, out, errs := e.run("xbpretty", "download", "figlet")
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n%s", code, errs)
			}
			if !strings.Contains(out, "Downloads Complete:") {
				t.Errorf("stdout = %q", out)
			}
			if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps"); string(got) != string(body) {
				t.Errorf("archive = %q", got)
			}
			if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps.sig2"); string(got) != "sig" {
				t.Errorf("signature = %q", got)
			}
		})
	}
}

// TestDownloadDeduplicatesNamesPreservingOrder pins FR-039's name handling:
// the same name twice is one package.
func TestDownloadDeduplicatesNames(t *testing.T) {
	body := []byte("archive")
	server := newArchiveServer(t, body, []byte("sig"))
	e := newEnv(t)
	t.Chdir(t.TempDir())
	downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
	code, out, errs := e.run("xbpretty", "download", "figlet", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if got := strings.Count(out, "was written to"); got != 1 {
		t.Errorf("success block has %d package lines, want 1:\n%s", got, out)
	}
	resolutions := 0
	for _, args := range e.commands() {
		if strings.HasPrefix(args, "-R -S figlet") {
			resolutions++
		}
	}
	if resolutions != 1 {
		t.Errorf("resolved figlet %d times, want 1", resolutions)
	}
}

// TestDownloadZeroNames pins FR-039's zero-name message; validateInvocation
// already owns it, and download must keep it.
func TestDownloadZeroNames(t *testing.T) {
	e := newEnv(t)
	t.Chdir(t.TempDir())
	code, out, errs := e.run("xbpretty", "download")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errs != "xbpretty: download needs at least one package\n" {
		t.Errorf("stderr = %q", errs)
	}
}

// TestDownloadFetchesMissingSignatureForReusedArchive pins FR-039's rule that
// a reused matching archive still gets its missing signature.
func TestDownloadFetchesMissingSignatureForReusedArchive(t *testing.T) {
	body := []byte("cached archive")
	sig := []byte("fresh signature")
	server := newArchiveServer(t, body, sig)
	e := newEnv(t)
	t.Chdir(t.TempDir())
	downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
	if err := os.WriteFile("figlet-2.2.5_6.x86_64.xbps", body, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "Downloads Complete:") {
		t.Errorf("stdout = %q", out)
	}
	if got := server.requestCount(); got != 1 {
		t.Errorf("requests = %d, want only the signature", got)
	}
	if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps.sig2"); string(got) != string(sig) {
		t.Errorf("signature = %q, want %q", got, sig)
	}
}

// TestDownloadResolutionFailureForwardsStderrAndCode pins the FR-073 rule for
// the per-name query: a non-2 failure is the child's, stderr and code intact.
func TestDownloadResolutionFailureForwardsStderrAndCode(t *testing.T) {
	e := newEnv(t)
	t.Chdir(t.TempDir())
	e.set("XB_STUB_REPOS", "https://repo.example/current\n")
	e.set("XB_STUB_RESOLVE_EXIT", "1")
	e.set("XB_STUB_RESOLVE_STDERR", "ERROR: repository metadata is corrupt\n")
	code, out, errs := e.run("xbpretty", "download", "figlet")
	if code != 1 {
		t.Errorf("exit = %d, want the child's 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errs != "ERROR: repository metadata is corrupt\n" {
		t.Errorf("stderr = %q, want the child's stderr verbatim", errs)
	}
}

// TestDownloadFTPNameDoesNotBlockTheRest pins multi-name behaviour: an ftp
// refusal fails that name, the others still download, and the run exits 1
// without the success block.
func TestDownloadFTPNameDoesNotBlockTheRest(t *testing.T) {
	body := []byte("figlet archive")
	server := newArchiveServer(t, body, []byte("sig"))
	e := newEnv(t)
	t.Chdir(t.TempDir())
	e.set("XB_STUB_REPOS", server.srv.URL+"/current\n")
	e.set("XB_STUB_RESOLVE_FIGLET", strings.Join([]string{
		"figlet-2.2.5_6", server.srv.URL + "/current", "x86_64", sha256Of(body),
	}, "\n"))
	e.set("XB_STUB_RESOLVE_WGET", strings.Join([]string{
		"wget-1.25.0_1", "ftp://ftp.example/current", "x86_64", sha256Of([]byte("wget")),
	}, "\n"))

	code, out, errs := e.run("xbpretty", "download", "figlet", "wget")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want no block with a failed name", out)
	}
	if !strings.Contains(errs, "xbpretty: wget-1.25.0_1: ftp:// repositories are not supported by xbp download\n") {
		t.Errorf("stderr missing the ftp failure:\n%s", errs)
	}
	if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps"); string(got) != string(body) {
		t.Errorf("the other package was not downloaded: %q", got)
	}
}

// TestDownloadHashlessNameDoesNotBlockTheRest pins the same continuation for
// an unverifiable package.
func TestDownloadHashlessNameDoesNotBlockTheRest(t *testing.T) {
	body := []byte("figlet archive")
	server := newArchiveServer(t, body, []byte("sig"))
	e := newEnv(t)
	t.Chdir(t.TempDir())
	e.set("XB_STUB_REPOS", server.srv.URL+"/current\n")
	e.set("XB_STUB_RESOLVE_FIGLET", strings.Join([]string{
		"figlet-2.2.5_6", server.srv.URL + "/current", "x86_64", sha256Of(body),
	}, "\n"))
	e.set("XB_STUB_RESOLVE_WGET", strings.Join([]string{
		"wget-1.25.0_1", "https://repo.example/current", "x86_64",
	}, "\n"))

	code, out, errs := e.run("xbpretty", "download", "figlet", "wget")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want no block with a failed name", out)
	}
	if !strings.Contains(errs, "xbpretty: wget-1.25.0_1: repository metadata has no SHA-256; refusing to write an unverifiable package\n") {
		t.Errorf("stderr missing the hash-less failure:\n%s", errs)
	}
	if got, _ := os.ReadFile("figlet-2.2.5_6.x86_64.xbps"); string(got) != string(body) {
		t.Errorf("the other package was not downloaded: %q", got)
	}
}

// TestDownloadFailureMessages pins the CLI's per-file messages and exit codes.
func TestDownloadFailureMessages(t *testing.T) {
	body := []byte("archive bytes")

	t.Run("missing signature", func(t *testing.T) {
		server := newArchiveServer(t, body, nil)
		e := newEnv(t)
		t.Chdir(t.TempDir())
		downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
		code, out, errs := e.run("xbpretty", "download", "figlet")
		if code != 1 || out != "" {
			t.Errorf("exit = %d, stdout = %q", code, out)
		}
		want := "xbpretty: figlet-2.2.5_6.x86_64.xbps.sig2: 404 Not Found; the repository no longer serves the signature\n"
		if errs != want {
			t.Errorf("stderr = %q, want %q", errs, want)
		}
	})

	t.Run("hash mismatch", func(t *testing.T) {
		tampered := []byte("tampered bytes")
		server := newArchiveServer(t, tampered, []byte("sig"))
		e := newEnv(t)
		t.Chdir(t.TempDir())
		downloadFixture(t, e, server.srv.URL+"/current", "figlet-2.2.5_6", "x86_64", sha256Of(body))
		code, out, errs := e.run("xbpretty", "download", "figlet")
		if code != 1 || out != "" {
			t.Errorf("exit = %d, stdout = %q", code, out)
		}
		want := "xbpretty: figlet-2.2.5_6.x86_64.xbps: SHA-256 mismatch: expected " + sha256Of(body) +
			", got " + sha256Of(tampered) + "\n"
		if errs != want {
			t.Errorf("stderr = %q, want %q", errs, want)
		}
		if _, err := os.Stat("figlet-2.2.5_6.x86_64.xbps"); !os.IsNotExist(err) {
			t.Error("the mismatching archive was written")
		}
		if _, err := os.Stat("figlet-2.2.5_6.x86_64.xbps.part"); !os.IsNotExist(err) {
			t.Error(".part survived")
		}
	})

	t.Run("archive 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".sig2") {
				_, _ = w.Write([]byte("sig"))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()
		e := newEnv(t)
		t.Chdir(t.TempDir())
		e.set("XB_STUB_REPOS", srv.URL+"/current\n")
		e.set("XB_STUB_RESOLVE", strings.Join([]string{"figlet-2.2.5_6", srv.URL + "/current", "x86_64", sha256Of(body)}, "\n"))
		code, _, errs := e.run("xbpretty", "download", "figlet")
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		want := "xbpretty: figlet-2.2.5_6.x86_64.xbps: 404 Not Found\n"
		if errs != want {
			t.Errorf("stderr = %q, want %q", errs, want)
		}
	})
}

// TestDownloadSignalCancelsAndExits130 drives the built binary with a stalled
// archive transfer, sends Ctrl-C and asserts the FR-041 cleanup contract.
func TestDownloadSignalCancelsAndExits130(t *testing.T) {
	body := []byte("archive")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig2") {
			_, _ = w.Write([]byte("sig"))
			return
		}
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	bin := buildBinary(t)
	cfgDir, _ := integrationConfig(t, "Never")
	work := t.TempDir()
	cmd := exec.Command(bin, "download", "figlet")
	cmd.Dir = work
	cmd.Env = envWith(map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(t.TempDir(), "state"),
		"XB_STUB_LOG":     filepath.Join(t.TempDir(), "commands.log"),
		"XB_STUB_REPOS":   srv.URL + "/current\n",
		"XB_STUB_RESOLVE": strings.Join([]string{"figlet-2.2.5_6", srv.URL + "/current", "x86_64", sha256Of(body)}, "\n"),
		"NO_COLOR":        "1",
	})
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(work, "figlet-2.2.5_6.x86_64.xbps.part")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(part); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("the transfer never created .part\nstderr:\n%s", errs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if code != 130 {
			t.Errorf("exit = %d, want 130", code)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the process did not exit after SIGINT")
	}
	if !strings.Contains(errs.String(), "xbpretty: interrupted") {
		t.Errorf("stderr missing the interruption line:\n%s", errs.String())
	}
	if out.String() != "" {
		t.Errorf("stdout = %q", out.String())
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Error(".part survived SIGINT")
	}
}

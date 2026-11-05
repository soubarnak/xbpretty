package cli

import (
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
	if !strings.HasSuffix(suffix, "/current/x86_64-repodata") {
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
	xbpsd := filepath.Join(e.dir, "xbps.d")
	if err := os.MkdirAll(xbpsd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xbpsd, "00-main.conf"),
		[]byte("repository=https://mirror.example/current\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origURL, origDirs, origArch, origClient, origPick, origEvents :=
		fetchMirrorListURL, fetchXBPSDDirs, fetchArch, fetchHTTPClient, pickMirror, mirrorScoreEvents
	fetchMirrorListURL = fixture.srv.URL + "/mirrors.lst"
	fetchXBPSDDirs = []string{xbpsd}
	fetchArch = "x86_64"
	fetchHTTPClient = fixture.srv.Client
	pickMirror = nil
	mirrorScoreEvents = func(fetch.ProgressEvent) {}
	t.Cleanup(func() {
		fetchMirrorListURL, fetchXBPSDDirs, fetchArch, fetchHTTPClient, pickMirror, mirrorScoreEvents =
			origURL, origDirs, origArch, origClient, origPick, origEvents
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
	if strings.Join(names, ",") != "00-main.conf" {
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

func TestFetchRootReachesWriteSeamOnly(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	e.root = true

	code, out, errs := e.run("xbpretty", "fetch")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(errs, "writing the repository configuration is not implemented in this build") {
		t.Errorf("stderr does not name the #74 write seam:\n%s", errs)
	}
	if !strings.Contains(out, "Mirror") {
		t.Errorf("table was not printed before the write seam:\n%s", out)
	}
	assertNoFiles(t, xbpsd)
	if cmds := e.commands(); len(cmds) != 0 {
		t.Errorf("#73 started a child: %v", cmds)
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
	e, _ := newFetchEnv(t, fixture)

	code, _, errs := e.run("xbpretty", "fetch", "--dry-run", "--non-free")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	_, _, _, _, headPaths := fixture.counts()
	joined := strings.Join(headPaths, "\n")
	for _, want := range []string{
		"/one/current/x86_64-repodata",
		"/one/current/nonfree/x86_64-repodata",
		"/one/current/multilib/nonfree/x86_64-repodata",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("HEADs %v do not include %s", headPaths, want)
		}
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

	calls := 0
	pickMirror = func(ranked []fetch.Result) (int, bool) {
		calls++
		return 0, true
	}

	// Root + TTY + no --auto + no --dry-run runs the picker seam exactly once.
	code, out, errs := e.run("xbpretty", "fetch")
	if code != 1 { // #74's write seam still refuses
		t.Fatalf("exit = %d, want 1\n%s\n%s", code, out, errs)
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
		name string
		root bool
		tty  bool
		argv []string
	}{
		{"non-TTY", true, false, []string{"fetch"}},
		{"dry-run", true, true, []string{"fetch", "--dry-run"}},
		{"auto", true, true, []string{"fetch", "--auto", "2"}},
		{"rootless", false, true, []string{"fetch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = 0
			e.root = tc.root
			e.tty = tc.tty
			e.run(append([]string{"xbpretty"}, tc.argv...)...)
			if called != 0 {
				t.Errorf("picker opened for %s", tc.name)
			}
		})
	}
}

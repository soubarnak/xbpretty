package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parityRepoRoot is the module root from internal/cli.
func parityRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// parityNAFlagRefusal runs one transaction N/A flag against the built binary
// and pins the exact one-line refusal. Each row's wrapper passes its own id,
// so a row only pays for its own case.
func parityNAFlagRefusal(t *testing.T, id string) {
	t.Helper()
	cases := map[string]struct {
		args []string
		tail string
	}{
		"P-OPT-007": {[]string{"install", "figlet", "--option", "Acquire::x=y"}, "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf."},
		"P-OPT-014": {[]string{"install", "figlet", "--allow-unauthenticated"}, "--allow-unauthenticated is not available on xbps: xbps verifies repository signatures and has no unauthenticated mode."},
		"P-OPT-017": {[]string{"remove", "figlet", "--remove-essential"}, "--remove-essential is not available on xbps: xbps has no essential-package protection to override."},
		"P-OPT-019": {[]string{"install", "figlet", "--fix-broken"}, "-f/--fix-broken is not available on xbps: xbps has no broken-dependency repair mode. Check the pkgdb with xbps-pkgdb --checks dependencies, or force a reinstall with xbp install -- -f PKG."},
		"P-OPT-021": {[]string{"install", "figlet", "--install-recommends"}, "--install-recommends is not available on xbps: xbps has no recommends metadata; every declared run_depends dependency is installed."},
		"P-OPT-023": {[]string{"install", "figlet", "--install-suggests"}, "--install-suggests is not available on xbps: xbps has no suggests metadata."},
		"P-OPT-047": {[]string{"remove", "figlet", "--remove-config"}, "--remove-config is not available on xbps: xbps has no config-files state; xbps-remove -f removes files, it does not create a config-only removal."},
		"P-OPT-049": {[]string{"upgrade", "--target-release", "stable"}, "-t/--target-release is not available on xbps: xbps has no default-release targeting; pin a repository with a repository= line in /etc/xbps.d."},
		"P-OPT-051": {[]string{"upgrade", "--exclude", "linux*"}, "--exclude is not available on xbps: xbps has no upgrade exclusion; hold the package with xbps-pkgdb -m hold PKG (release with -m unhold)."},
		"P-OPT-041": {[]string{"fetch", "--sources"}, "--sources is not available: Void repositories contain no source packages"},
		"P-OPT-044": {[]string{"fetch", "--debian"}, "--debian is not available: xbpretty targets Void Linux only"},
		"P-OPT-045": {[]string{"fetch", "--ubuntu"}, "--ubuntu is not available: xbpretty targets Void Linux only"},
		"P-OPT-046": {[]string{"fetch", "--devuan"}, "--devuan is not available: xbpretty targets Void Linux only"},
		"P-BEH-025": {[]string{"install", "figlet", "--option", "Acquire::x=y"}, "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf."},
		"P-BEH-032": {[]string{"install", "figlet", "--option", "Acquire::x=y"}, "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf."},
	}
	tc, ok := cases[id]
	if !ok {
		t.Fatalf("no refusal case for %s", id)
	}
	bin := buildBinary(t)
	stubEnv, probe := xbpsStubPath(t)
	env := mergeEnv(invalidConfigDir(t), stubEnv)
	code, out, errs := surfaceRun(t, bin, env, tc.args...)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if want := "xbpretty: " + tc.tail + "\n"; errs != want {
		t.Errorf("stderr = %q, want exactly %q", errs, want)
	}
	if out != "" {
		t.Errorf("refusal wrote stdout:\n%s", out)
	}
	if data, err := os.ReadFile(probe); err == nil && len(data) > 0 {
		t.Errorf("an xbps child ran before the refusal:\n%s", data)
	}
}

// parityFetchRefusalExact pins the four fetch N/A lines to byte equality and
// proves the refusal touched no network or file.
func parityFetchRefusalExact(t *testing.T, id string) {
	t.Helper()
	cases := map[string]struct {
		flag string
		line string
	}{
		"P-OPT-041": {"--sources", "--sources is not available: Void repositories contain no source packages"},
		"P-OPT-044": {"--debian", "--debian is not available: xbpretty targets Void Linux only"},
		"P-OPT-045": {"--ubuntu", "--ubuntu is not available: xbpretty targets Void Linux only"},
		"P-OPT-046": {"--devuan", "--devuan is not available: xbpretty targets Void Linux only"},
	}
	tc, ok := cases[id]
	if !ok {
		t.Fatalf("no fetch refusal case for %s", id)
	}
	fixture := newFetchFixture(t, nil)
	e, _ := newFetchEnv(t, fixture)
	code, out, errs := e.run("xbpretty", "fetch", tc.flag)
	if code != 2 {
		t.Fatalf("%s exit = %d, want 2", tc.flag, code)
	}
	if out != "" {
		t.Errorf("%s wrote stdout:\n%s", tc.flag, out)
	}
	if want := "xbpretty: " + tc.line + "\n"; errs != want {
		t.Errorf("%s stderr = %q, want exactly %q", tc.flag, errs, want)
	}
	if list, probes, heads, _, _ := fixture.counts(); list+probes+heads != 0 {
		t.Errorf("N/A refusal touched the network: list=%d probes=%d heads=%d", list, probes, heads)
	}
}

// parityNoOpNegative pins AC-066 for one negative compatibility flag: accepted
// silently, absent from every argv, and the command's exit code is unchanged.
func parityNoOpNegative(t *testing.T, id string) {
	t.Helper()
	flags := map[string]string{
		"P-OPT-020": "--no-fix-broken",
		"P-OPT-022": "--no-install-recommends",
		"P-OPT-024": "--no-install-suggests",
	}
	flag, ok := flags[id]
	if !ok {
		t.Fatalf("no no-op case for %s", id)
	}
	banned := strings.TrimPrefix(flag, "--no-")

	e := newEnv(t)
	e.root = true
	code, out, errs := e.run("xbpretty", "install", "figlet", "--dry-run", flag)
	if code != 0 {
		t.Fatalf("%s exit = %d, want 0\nstderr:\n%s", flag, code, errs)
	}
	if errs != "" {
		t.Errorf("%s printed to stderr: %q", flag, errs)
	}
	if out == "" {
		t.Errorf("%s suppressed the dry-run summary", flag)
	}
	for _, argv := range e.transactionCalls() {
		if strings.Contains(argv, banned) {
			t.Errorf("%s reached an xbps argv: %q", banned, argv)
		}
	}

	// A failing command keeps its own code and never reports the flag.
	bad := newEnv(t)
	bad.root = true
	bad.set("XB_STUB_EXIT", "1")
	bad.set("XB_STUB_RUN_STDERR", "ERROR: transaction failed\n")
	code, _, errs = bad.run("xbpretty", "install", "figlet", flag)
	if code != 1 {
		t.Errorf("%s failing command exit = %d, want 1\n%s", flag, code, errs)
	}
	if strings.Contains(errs, "not available") || strings.Contains(errs, "unknown flag") {
		t.Errorf("%s was refused instead of accepted:\n%s", flag, errs)
	}
	for _, argv := range bad.transactionCalls() {
		if strings.Contains(argv, banned) {
			t.Errorf("%s reached a failing xbps argv: %q", banned, argv)
		}
	}
}

// parityUpgradeFlagNoop pins one accepted no-op upgrade flag: same argv as
// upgrade.
func parityUpgradeFlagNoop(t *testing.T, id string) {
	t.Helper()
	flags := map[string]string{
		"P-OPT-052": "--full",
		"P-OPT-053": "--no-full",
		"P-OPT-054": "--safe",
	}
	flag, ok := flags[id]
	if !ok {
		t.Fatalf("no upgrade no-op case for %s", id)
	}
	e := newEnv(t)
	e.root = true
	code, _, errs := e.run("xbpretty", "upgrade", "--no-sync", "--dry-run", flag)
	if code != 0 {
		t.Fatalf("%s exit = %d, want 0\n%s", flag, code, errs)
	}
	if got := e.transactionCalls(); len(got) != 1 || got[0] != "-n -u" {
		t.Errorf("%s commands = %q, want one -n -u transaction", flag, got)
	}
}

// parityFullUpgradeCommand pins P-CMD-011: the full-upgrade command and the
// [Nala] full_upgrade config both leave the argv at -u.
func parityFullUpgradeCommand(t *testing.T) {
	TestAC008UpgradeFamilyArgv(t)
	TestFullUpgradeConfigChangesNothing(t)
}

// parityDownloadNoMirrorTable drives the real download telemetry frame through
// the built binary and proves the per-mirror table never renders, while the
// Download frame keeps its fixed seven rows.
func parityDownloadNoMirrorTable(t *testing.T) {
	errs, _, code := ptyWidgetRun(t, []string{"xbpretty", "install", "-y", "figlet"}, 80)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	frames := progressFrames(errs)
	if len(frames) == 0 {
		t.Fatalf("no progress frame captured:\n%q", errs)
	}
	if got := len(frameLines(frames[0])); got != heightDownload {
		t.Errorf("Download frame height = %d, want %d:\n%s", got, heightDownload, frames[0])
	}
	for _, literal := range []string{"Connections", "Average", "State"} {
		if strings.Contains(errs, literal) {
			t.Errorf("%q rendered in the real download view:\n%s", literal, errs)
		}
	}
}

// parityUpdateCompletionLines drives a real update through the telemetry stub
// and proves the completion line nala prints for the sync (BF-03): `Fetched`
// when a repodata transfer completed, `Nothing to fetch` when none did.
func parityUpdateCompletionLines(t *testing.T, syncBody, want string) {
	e := newEnv(t)
	e.root = true
	e.tty = true
	stub := filepath.Join(t.TempDir(), "update-stub")
	script := `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
  -S)
` + syncBody + `
    ;;
esac
exit 0
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(stub)
	e.set("XB_STUB_STDOUT", "")
	code, _, errs := e.run("xbpretty", "update")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "Updating Package Lists") || !strings.Contains(errs, "Downloading: https://repo-default.voidlinux.org/current") {
		t.Errorf("the real Update view did not render:\n%s", errs)
	}
	if !strings.Contains(errs, want) {
		t.Errorf("%q missing from the real update output:\n%s", want, errs)
	}
}

// parityUpdateFetchedBody fetches one repodata transfer: its tick on stderr and
// its completion line.
const parityUpdateFetchedBody = `
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...\n"
    sleep 0.05
    printf "[ 50%%] x86_64-repodata: [1854KB 50%%] 2391KB/s ETA: 00m01s\033[K\r" >&2
    sleep 0.05
    printf "x86_64-repodata: 1854KB [avg rate: 2391KB/s]\033[K\n" >&2
    exit 0`

// parityUpdateUnchangedBody syncs two repositories, neither of which changed.
const parityUpdateUnchangedBody = `
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...\n"
    sleep 0.05
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata' ...\n"
    exit 0`

// parityNoAuthFileRead proves P-BEH-038 as an absence: xbpretty never reads
// /etc/apt/auth.conf or a netrc file; Void repositories are public.
func parityNoAuthFileRead(t *testing.T) {
	for _, literal := range []string{"/etc/apt", "auth.conf", ".netrc"} {
		parityNoProductionLiteral(t, literal)
	}
}

// parityNoLegacyHistoryJSON proves P-BEH-011 as an absence: no production code
// constructs or reads a legacy history.json.
func parityNoLegacyHistoryJSON(t *testing.T) {
	parityNoProductionLiteral(t, "history.json")
	parityNoProductionLine(t, "history", ".json")
}

// parityNoDynamicCompletionProtocol proves P-BEH-028 as an absence: no
// production code implements the COMPLETE=<shell> handshake.
func parityNoDynamicCompletionProtocol(t *testing.T) {
	parityNoProductionLiteral(t, "COMPLETE=")
}

// parityNoI18nCatalog proves P-BEH-029 as an absence: there is no translation
// catalog and no locale selection in the shipped code.
func parityNoI18nCatalog(t *testing.T) {
	for _, literal := range []string{"gettext", "ngettext", "libintl", "i18n"} {
		parityNoProductionLiteral(t, literal)
	}
}

// parityNoMirrorRotation proves P-BEH-035 as an absence: no production code
// rotates candidates or enforces per-package fairness; xbps resolves one URL.
func parityNoMirrorRotation(t *testing.T) {
	for _, literal := range []string{"rotation", "fairness", "round-robin", "deque"} {
		parityNoProductionLiteral(t, literal)
	}
}

// parityNoDpkgProtocol proves P-VIS-033 as an absence: no dpkg conffile prompt
// or status-fd parsing exists; maintainer output is ordinary child output.
func parityNoDpkgProtocol(t *testing.T) {
	for _, literal := range []string{"status-fd", "conffile", "dpkg"} {
		parityNoProductionLiteral(t, literal)
	}
}

// parityNoQKeyInInline proves P-BEH-039 as an absence: q is a full-screen key
// only; the inline viewport, download and transaction paths never read it.
func parityNoQKeyInInline(t *testing.T) {
	t.Helper()
	root := parityRepoRoot(t)
	skip := map[string]bool{
		filepath.Join(root, "internal", "cli", "tui.go"):          true,
		filepath.Join(root, "internal", "cli", "fetch_picker.go"): true,
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || skip[path] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "'q'") {
			t.Errorf("%s consumes the q key outside a full-screen surface", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// parityNoProductionLiteral scans the shipped Go sources for a literal that
// must not exist. It is the explicit-absence seam for surfaces with no runtime
// state to observe.
func parityNoProductionLiteral(t *testing.T, literal string) {
	t.Helper()
	root := parityRepoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), literal) {
			t.Errorf("%s names %q", path, literal)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// parityNoProductionLine rejects a name assembled on one source line from
// separate fragments, which a single literal scan would miss.
func parityNoProductionLine(t *testing.T, fragments ...string) {
	t.Helper()
	root := parityRepoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			all := true
			for _, fragment := range fragments {
				if !strings.Contains(line, fragment) {
					all = false
					break
				}
			}
			if all {
				t.Errorf("%s:%d assembles %s", path, i+1, strings.Join(fragments, " + "))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// parityDebNeverInstalls proves P-BEH-040: a .deb path is never installed
// directly; it reaches xbps as an ordinary package spec.
func parityDebNeverInstalls(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	code, out, errs := surfaceRun(t, bin, map[string]string{
		"XDG_CONFIG_HOME": cfgDir,
		"XDG_STATE_HOME":  filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG":     logPath,
		"XB_STUB_STDOUT":  planFixture,
		"NO_COLOR":        "1",
	}, "install", "--dry-run", "./evil.deb")
	if code != 0 {
		t.Fatalf("install --dry-run ./evil.deb = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading stub log: %v", err)
	}
	if !strings.Contains(string(raw), "./evil.deb") {
		t.Errorf("./evil.deb did not reach xbps as a package spec:\n%s", raw)
	}
}

// parityNoDebianPackaging proves P-PKG-001: no debian/ packaging tree ships at
// any depth and the equivalent xbps-src artifact exists.
func parityNoDebianPackaging(t *testing.T) {
	root := parityRepoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			if d.Name() == "debian" {
				t.Errorf("a debian/ packaging tree exists: %s", path)
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".deb", ".dsc":
			t.Errorf("a Debian packaging artifact exists: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "srcpkgs", "xbpretty", "template")); err != nil {
		t.Errorf("the xbps-src template is missing: %v", err)
	}
}

// parityNoPpaArtifacts proves P-PKG-004: Debian/Ubuntu/Devuan suites and PPAs
// never exist. Only whole path segments are matched, so apparmor-like names
// cannot false-positive.
func parityNoPpaArtifacts(t *testing.T) {
	root := parityRepoRoot(t)
	segments := map[string]bool{"debian": true, "ubuntu": true, "devuan": true}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			if segments[strings.ToLower(d.Name())] {
				t.Errorf("a Debian-family suite directory exists: %s", path)
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ppa", ".dsc", ".deb":
			t.Errorf("a Debian-family artifact exists: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// parityExitCodeMatrix pins P-BEH-002 across the normal, runtime and signalled
// paths without re-running the whole AC suite.
func parityExitCodeMatrix(t *testing.T) {
	TestExitCodePassThrough(t)
	TestAC014SignalledChildDecidesTheExitCode(t)
}

// parityS1GateCase runs one full-screen gate cell in a real PTY: --tui opens
// from Auto, --no-tui demotes Tui to the text summary.
func parityS1GateCase(t *testing.T, id string) {
	t.Helper()
	cases := map[string]struct {
		mode       string
		args       []string
		wantScreen bool
		send       string
	}{
		"P-OPT-005": {mode: "Auto", args: []string{"upgrade", "--tui"}, wantScreen: true, send: "q"},
		"P-OPT-006": {mode: "Tui", args: []string{"upgrade", "--no-tui"}, wantScreen: false, send: "n\n"},
	}
	tc, ok := cases[id]
	if !ok {
		t.Fatalf("no S1 gate case for %s", id)
	}
	bin := testrootBinary(t)
	dir := t.TempDir()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnvMode(t, dir, query, tc.mode, "Always")
	r := startPty(t, bin, env, tc.args, 80, 24)
	r.send(t, tc.send)
	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit = %d, want 1\nstderr:\n%s", code, r.stderr.String())
	}
	out := r.cap.String()
	if tc.wantScreen {
		if !strings.Contains(out, "\x1b[?1049h") {
			t.Errorf("--tui did not open S1: %q", out)
		}
		return
	}
	if strings.Contains(out, "\x1b[?1049h") {
		t.Errorf("--no-tui still opened S1: %q", out)
	}
	if !strings.Contains(out, "Total download size:") {
		t.Errorf("--no-tui did not render the text summary:\n%q", out)
	}
}

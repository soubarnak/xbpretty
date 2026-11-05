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

// parityDownloadNoMirrorTable proves P-VIS-030: the Download view has no
// per-mirror table. nala's Connections/Average/State columns cannot exist
// because xbps resolves one sequential source per package.
func parityDownloadNoMirrorTable(t *testing.T) {
	view := progressView{
		kind:      viewDownload,
		msgHeader: "Downloading",
		msgText:   "figlet-2.2.5_6",
		ratio:     0.5,
		left: []progressInfo{
			{label: "Packages", value: "1/2"},
			{label: "Data", value: "92 KiB"},
		},
		right: []progressInfo{
			{label: "Remaining", value: "46 KiB"},
			{label: "Speed", value: "1.0 MiB/s"},
		},
	}
	out := strings.Join(renderProgressView(view, 80, false), "\n")
	for _, literal := range []string{"Connections", "Average", "State", "Mirror"} {
		if strings.Contains(out, literal) {
			t.Errorf("%q rendered in the Download view:\n%s", literal, out)
		}
	}
	if !strings.Contains(out, "Downloading Packages") {
		t.Errorf("the Download view title is missing:\n%s", out)
	}
}

// parityUpdateNoFetched proves P-VIS-031: `Fetched ` never prints after an
// update; the widget clears with no completion line. xbps reports no fetched
// byte total.
func parityUpdateNoFetched(t *testing.T) {
	view := progressView{
		kind:      viewUpdate,
		msgHeader: "Updating",
		msgText:   "https://repo-default.voidlinux.org/current",
		ratio:     1,
		left: []progressInfo{
			{label: "Repositories", value: "2/2"},
			{label: "Elapsed", value: "1s"},
		},
	}
	out := strings.Join(renderProgressView(view, 80, false), "\n")
	if strings.Contains(out, "Fetched ") {
		t.Errorf("a Fetched line rendered in the Update view:\n%s", out)
	}
	if !strings.Contains(out, "Updating:") {
		t.Errorf("the Update view message is missing:\n%s", out)
	}
}

// parityUpdateNoNothingToFetch proves P-VIS-032: `Nothing to fetch` never
// prints; xbps always attempts a sync and reports per-repository lines.
func parityUpdateNoNothingToFetch(t *testing.T) {
	view := progressView{
		kind:      viewUpdate,
		msgHeader: "Updating",
		msgText:   "https://repo-default.voidlinux.org/current",
		ratio:     1,
	}
	out := strings.Join(renderProgressView(view, 80, false), "\n")
	if strings.Contains(out, "Nothing to fetch") {
		t.Errorf("Nothing to fetch rendered in the Update view:\n%s", out)
	}
}

// parityNoAuthFileRead proves P-BEH-038 as an absence: xbpretty never reads
// /etc/apt/auth.conf or a netrc file; Void repositories are public. The
// checked seam is the built source, because there is no runtime surface that
// could prove a negative without a network fixture.
func parityNoAuthFileRead(t *testing.T) {
	for _, literal := range []string{"/etc/apt/auth.conf", ".netrc"} {
		parityNoProductionLiteral(t, literal)
	}
}

// parityNoLegacyHistoryJSON proves P-BEH-011 as an absence: no production
// code names a legacy history.json, so it is never read or imported.
func parityNoLegacyHistoryJSON(t *testing.T) {
	parityNoProductionLiteral(t, "history.json")
}

// parityNoDynamicCompletionProtocol proves P-BEH-028 as an absence: no
// production code implements the COMPLETE=<shell> handshake; completions are
// static generated files plus runtime lookups.
func parityNoDynamicCompletionProtocol(t *testing.T) {
	parityNoProductionLiteral(t, "COMPLETE=")
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

// parityDebNeverInstalls proves P-BEH-040: a .deb path or https:// package
// argument is never installed directly. It reaches xbps as an ordinary
// package spec; the equivalent surface is `xbp download`.
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

// parityNoDebianPackaging proves P-PKG-001: no debian/ packaging tree ships;
// the equivalent artifact is the xbps-src template.
func parityNoDebianPackaging(t *testing.T) {
	root := parityRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "debian")); !os.IsNotExist(err) {
		t.Errorf("a debian/ packaging tree exists at the repository root (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "srcpkgs", "xbpretty", "template")); err != nil {
		t.Errorf("the xbps-src template is missing: %v", err)
	}
}

// parityNoPpaArtifacts proves P-PKG-004: Debian/Ubuntu/Devuan suites and PPAs
// never exist; xbpretty is Void-only.
func parityNoPpaArtifacts(t *testing.T) {
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
		if strings.Contains(strings.ToLower(path), "ppa") {
			t.Errorf("a PPA artifact exists: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// parityTemplateHasNoEtcConfig proves P-PKG-005's negative half: the package
// installs no /etc/xbpretty/config.toml and declares no conf_files. The docs
// half is owned by the packaging slice and does not change this absence.
func parityTemplateHasNoEtcConfig(t *testing.T) {
	root := parityRepoRoot(t)
	template, err := os.ReadFile(filepath.Join(root, "srcpkgs", "xbpretty", "template"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(template)
	for _, literal := range []string{"/etc/xbpretty", "conf_files", "/etc/"} {
		if strings.Contains(text, literal) {
			t.Errorf("the template installs %q", literal)
		}
	}
}

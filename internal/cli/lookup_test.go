package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lookupRead returns a captured `xbps-query` answer: the shape FR-011 reads
// the old version and installed size out of.
func lookupRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/lookups", path))
	if err != nil {
		t.Fatalf("reading capture %s: %v", path, err)
	}
	return string(raw)
}

// TestParseOldMatchesCapturedLookups pins the shape against the corpus rather
// than against an assumption: `xbps-query -p pkgver,installed_size` prints the
// pkgver, then the size on its own line. The summary shows the version bare,
// so the name has to come back off the pkgver (FR-011).
func TestParseOldMatchesCapturedLookups(t *testing.T) {
	for _, capture := range []string{
		"0591/xbps.txt",
		"0591/base-files.txt",
		"0607/xbps.txt",
		"0607/base-files.txt",
	} {
		t.Run(capture, func(t *testing.T) {
			raw := lookupRead(t, capture)
			pkg := strings.TrimSuffix(filepath.Base(capture), ".txt")
			lines := strings.Split(strings.TrimSpace(raw), "\n")
			if len(lines) != 2 {
				t.Fatalf("capture has %d lines, want a pkgver and a size: %q", len(lines), raw)
			}
			wantVersion, ok := strings.CutPrefix(strings.TrimSpace(lines[0]), pkg+"-")
			if !ok {
				t.Fatalf("capture's first line %q does not name %s", lines[0], pkg)
			}
			old := parseOld(raw)
			if old.Version != wantVersion {
				t.Errorf("version = %q, want %q", old.Version, wantVersion)
			}
			if strings.Contains(old.Version, pkg) {
				t.Errorf("version %q still carries the package name", old.Version)
			}
			wantSize, err := parseSize(lines[1])
			if err != nil {
				t.Fatalf("capture's size line %q: %v", lines[1], err)
			}
			if !old.HasSize || old.InstalledSize != wantSize {
				t.Errorf("size = %d (has=%v), want %d", old.InstalledSize, old.HasSize, wantSize)
			}
		})
	}
}

// TestParseOldMissingPackage covers the `missing` capture: xbps-query exits 2
// and prints nothing, so the summary falls back to the new version alone.
func TestParseOldMissingPackage(t *testing.T) {
	raw := lookupRead(t, "0607/missing.txt")
	if strings.TrimSpace(raw) != "" {
		t.Fatalf("capture is not empty: %q", raw)
	}
	if old := parseOld(raw); old.Version != "" || old.HasSize {
		t.Errorf("old = %+v, want an empty lookup", old)
	}
}

// TestAC010UpgradeShowsOldVersion drives a captured lookup answer through the
// summary the way a real run does: an upgrade row renders `old → new`, both
// bare versions.
func TestAC010UpgradeShowsOldVersion(t *testing.T) {
	e := newEnv(t)
	e.root = true
	e.set("XB_STUB_STDOUT", "xbps-0.60.8_1 update x86_64 https://repo-default.voidlinux.org/current 533481 121137\n")
	e.set("XB_STUB_OLD", lookupRead(t, "0607/xbps.txt"))
	code, out, errs := e.run("xbpretty", "install", "xbps", "-n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, "0.60.7_1 → 0.60.8_1") {
		t.Errorf("summary is missing the old version:\n%s", out)
	}
}

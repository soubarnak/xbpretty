package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// TestAC035FetchWritesSyncsAndCleanFetchReverts drives FR-047–FR-049 through
// the built binary with the build-tagged test root: masking file, backup,
// schema-1 manifest, the post-write sync, revert on a written-repository
// ERROR:, and clean --fetch restoring through the manifest.
func TestAC035FetchWritesSyncsAndCleanFetchReverts(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "Helsinki, Finland", tier: 1}})
	bin := testrootBinary(t)
	_, env := implicitEnv(t)

	dir := t.TempDir()
	xbpsd := filepath.Join(dir, "xbps.d")
	if err := os.MkdirAll(xbpsd, 0o755); err != nil {
		t.Fatal(err)
	}
	original := writeFetchConf(t, xbpsd)
	target := filepath.Join(xbpsd, "10-repository-main.conf")
	manifestPath := filepath.Join(dir, "var-lib-xbpretty", "fetch.json")
	env["XBPRETTY_TEST_FETCH_XBPSD"] = xbpsd
	env["XBPRETTY_TEST_FETCH_WRITE_DIR"] = xbpsd
	env["XBPRETTY_TEST_FETCH_MANIFEST"] = manifestPath
	env["XBPRETTY_TEST_FETCH_MIRROR_LIST"] = fixture.srv.URL + "/mirrors.lst"
	env["XBPRETTY_TEST_FETCH_ARCH"] = "x86_64"
	env["XB_STUB_RUN_STDOUT"] = ""
	env["XB_STUB_RUN_STDERR"] = ""
	clearLog(t, implicitLog(env))

	code, out, errs := surfaceRun(t, bin, env, "fetch")
	if code != 0 {
		t.Fatalf("fetch exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
	if !strings.Contains(out, "Sources have been written to "+target) {
		t.Errorf("stdout does not name the written file:\n%s", out)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "repository="+fixture.srv.URL+"/one/current") {
		t.Errorf("the chosen mirror was not written:\n%s", written)
	}
	backup, err := os.ReadFile(target + fetch.BackupSuffix)
	if err != nil || string(backup) != string(original) {
		t.Errorf("backup = %q, %v; want the original bytes", backup, err)
	}
	manifest, err := fetch.ReadManifestFor(manifestPath, xbpsd)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Files) != 1 || manifest.Files[0].Path != target {
		t.Errorf("manifest = %+v", manifest)
	}
	if calls := strings.Join(stubCalls(t, implicitLog(env)), "\n"); !strings.Contains(calls, "xbps-install -S") {
		t.Errorf("xbps-install -S did not run:\n%s", calls)
	}

	// clean --fetch restores the pre-fetch file and removes the manifest.
	clearLog(t, implicitLog(env))
	code, _, errs = surfaceRun(t, bin, env, "clean", "--fetch")
	if code != 0 {
		t.Fatalf("clean --fetch exit = %d, want 0\n%s", code, errs)
	}
	restored, err := os.ReadFile(target)
	if err != nil || string(restored) != string(original) {
		t.Errorf("clean --fetch did not restore the original: %q %v", restored, err)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("clean --fetch did not remove the manifest: %v", err)
	}
	if calls := stubCalls(t, implicitLog(env)); len(calls) != 0 {
		t.Errorf("clean --fetch ran a child: %q", calls)
	}

	// A sync ERROR naming the written repository restores the previous
	// configuration, removes the manifest and exits non-zero.
	env["XB_STUB_RUN_STDERR"] = fmt.Sprintf(
		"ERROR: [reposync] failed to fetch file '%s/one/current/x86_64-repodata': Not Found\n", fixture.srv.URL)
	code, _, errs = surfaceRun(t, bin, env, "fetch")
	if code != 1 {
		t.Fatalf("failed-sync fetch exit = %d, want 1\n%s", code, errs)
	}
	restored, err = os.ReadFile(target)
	if err != nil || string(restored) != string(original) {
		t.Errorf("a failed sync did not restore the previous configuration: %q %v", restored, err)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("a reverted write left the manifest behind: %v", err)
	}
	if !strings.Contains(errs, "xbpretty: repository sync failed; previous configuration restored") {
		t.Errorf("stderr lacks the restore line:\n%s", errs)
	}
}

// TestFetchTestHooksAreAbsentFromOrdinaryBuilds keeps the write-dir and
// manifest overrides out of the release binary.
func TestFetchTestHooksAreAbsentFromOrdinaryBuilds(t *testing.T) {
	ordinary, err := os.ReadFile(buildBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := os.ReadFile(testrootBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"XBPRETTY_TEST_FETCH_XBPSD",
		"XBPRETTY_TEST_FETCH_MIRROR_LIST",
		"XBPRETTY_TEST_FETCH_ARCH",
		"XBPRETTY_TEST_FETCH_WRITE_DIR",
		"XBPRETTY_TEST_FETCH_MANIFEST",
		"XBPRETTY_TEST_FETCH_RANKED",
	} {
		if bytes.Contains(ordinary, []byte(name)) {
			t.Errorf("the ordinary build contains %s; the fetch hook leaked out of xbpretty_testroot", name)
		}
		if !bytes.Contains(tagged, []byte(name)) {
			t.Errorf("the xbpretty_testroot build is missing %s", name)
		}
	}
}

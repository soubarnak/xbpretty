package download

import (
	"reflect"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/xbps"
)

// TestArchiveURLIsPlanMetadataDerived pins FR-037's URL shape: the repository
// with only a trailing slash removed, then <pkgver>.<arch>.xbps. Nothing is
// invented: no path suffix, no mirror, no alternate filename.
func TestArchiveURLIsPlanMetadataDerived(t *testing.T) {
	url, err := ArchiveURL("https://repo-default.voidlinux.org/current/", "figlet-2.2.5_6", "x86_64")
	if err != nil {
		t.Fatalf("ArchiveURL: %v", err)
	}
	want := "https://repo-default.voidlinux.org/current/figlet-2.2.5_6.x86_64.xbps"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	if got := SignatureURL(url); got != want+".sig2" {
		t.Errorf("signature = %q, want %q", got, want+".sig2")
	}
	noarch, err := ArchiveURL("/srv/repo", "data-1.0_1", "noarch")
	if err != nil {
		t.Fatalf("ArchiveURL local: %v", err)
	}
	if noarch != "/srv/repo/data-1.0_1.noarch.xbps" {
		t.Errorf("local url = %q", noarch)
	}
}

// TestFilenameRejectsPathMetacharacters pins FR-037's refusal: a repository
// must never be able to walk out of the destination directory.
func TestFilenameRejectsPathMetacharacters(t *testing.T) {
	for _, tc := range []struct{ pkgver, arch string }{
		{"../figlet-2.2.5_6", "x86_64"},
		{"a/figlet-2.2.5_6", "x86_64"},
		{`a\figlet-2.2.5_6`, "x86_64"},
		{"figlet-2.2.5_6", "../x86_64"},
		{"figlet\x00-2.2.5_6", "x86_64"},
	} {
		_, err := ArchiveURL("https://example.invalid/current", tc.pkgver, tc.arch)
		if err == nil {
			t.Errorf("ArchiveURL(%q, %q) accepted an unsafe filename", tc.pkgver, tc.arch)
			continue
		}
		if !strings.Contains(err.Error(), "invalid package filename from repository metadata: "+tc.pkgver) {
			t.Errorf("error = %q, want the FR-037 message naming %q", err, tc.pkgver)
		}
	}
}

// TestParseHashesKeysByRepositoryAndPkgver pins the bulk filename-sha256
// answer: `<pkgver>: <sha256> (<repository>)`, keyed by (repository, pkgver).
func TestParseHashesKeysByRepositoryAndPkgver(t *testing.T) {
	stdout := strings.Join([]string{
		"figlet-2.2.5_6: 94372aabbccddeeff00112233445566778899aabbccddeeff001122334455667 (https://repo-default.voidlinux.org/current)",
		"wget-1.25.0_1: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef (https://mirror.example/current/)",
		"[*] nano-8.2_1: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa (https://mirror.example/current)",
		"",
		"not a hash row",
	}, "\n")
	hashes := ParseHashes(stdout)
	want := map[HashKey]string{
		{Repository: "https://repo-default.voidlinux.org/current", Pkgver: "figlet-2.2.5_6"}: "94372aabbccddeeff00112233445566778899aabbccddeeff001122334455667",
		{Repository: "https://mirror.example/current", Pkgver: "wget-1.25.0_1"}:              "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		{Repository: "https://mirror.example/current", Pkgver: "nano-8.2_1"}:                 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if !reflect.DeepEqual(hashes, want) {
		t.Errorf("hashes = %#v\nwant %#v", hashes, want)
	}
	// Looking a repository up with its configured trailing slash matches.
	if got := hashes[HashKeyFor("https://mirror.example/current/", "wget-1.25.0_1")]; got == "" {
		t.Error("a trailing slash on the repository broke the key lookup")
	}
}

// TestParseResolvedReadsThreeOrFourLines pins FR-037's per-name resolution:
// four lines mean the metadata hash is present, three mean it is absent.
func TestParseResolvedReadsThreeOrFourLines(t *testing.T) {
	withHash, err := ParseResolved(strings.Join([]string{
		"figlet-2.2.5_6",
		"https://repo-default.voidlinux.org/current",
		"x86_64",
		"94372aabbccddeeff00112233445566778899aabbccddeeff001122334455667",
	}, "\n"))
	if err != nil {
		t.Fatalf("ParseResolved with hash: %v", err)
	}
	if withHash.Pkgver != "figlet-2.2.5_6" || withHash.Repository != "https://repo-default.voidlinux.org/current" ||
		withHash.Arch != "x86_64" || !withHash.HasHash || withHash.SHA256 == "" {
		t.Errorf("resolved = %+v", withHash)
	}

	withoutHash, err := ParseResolved("figlet-2.2.5_6\nhttps://repo-default.voidlinux.org/current\nx86_64\n")
	if err != nil {
		t.Fatalf("ParseResolved without hash: %v", err)
	}
	if withoutHash.HasHash || withoutHash.SHA256 != "" {
		t.Errorf("hash-less resolution reported a hash: %+v", withoutHash)
	}

	if _, err := ParseResolved("figlet-2.2.5_6\n"); err == nil {
		t.Error("a two-line answer was accepted as a resolution")
	}
}

// TestDownloadRowsSelectsTheDownloadSet pins FR-036's download set as
// --print-uris and the predownload engine share it: downloading actions on an
// http(s) repository with a reported size, or a download action with no size.
func TestDownloadRowsSelectsTheDownloadSet(t *testing.T) {
	rows := []xbps.Row{
		{Name: "alpha", Version: "1.0_1", Action: xbps.ActionUpdate, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 100, HasDownload: true},
		{Name: "beta", Version: "1.0_1", Action: xbps.ActionInstall, Arch: "noarch", Repo: "https://repo/current/", DownloadSize: 50, HasDownload: true},
		{Name: "gamma", Version: "1.0_1", Action: xbps.ActionReinstall, Arch: "x86_64", Repo: "http://repo/current", DownloadSize: 25, HasDownload: true},
		{Name: "delta", Version: "1.0_1", Action: xbps.ActionHold, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 1000, HasDownload: true},
		{Name: "epsilon", Version: "1.0_1", Action: xbps.ActionRemove, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 1000, HasDownload: true},
		{Name: "zeta", Version: "1.0_1", Action: xbps.ActionUpdate, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 0, HasDownload: true},
		{Name: "eta", Version: "1.0_1", Action: xbps.ActionInstall, Arch: "x86_64", Repo: "/var/cache/xbps", DownloadSize: 100, HasDownload: true},
		{Name: "theta", Version: "1.0_1", Action: "download", Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 0, HasDownload: false},
	}
	got := DownloadRows(rows)
	var names []string
	for _, row := range got {
		names = append(names, row.Name)
	}
	want := []string{"alpha", "beta", "gamma", "theta"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("selected %v, want %v", names, want)
	}
}

// TestTargetFromRowCarriesPlanMetadata pins the fields #72 will consume.
func TestTargetFromRowCarriesPlanMetadata(t *testing.T) {
	row := xbps.Row{Name: "figlet", Version: "2.2.5_6", Action: xbps.ActionUpdate, Arch: "x86_64",
		Repo: "https://repo/current", DownloadSize: 94372, HasDownload: true}
	target, err := TargetFromRow(row, "deadbeef")
	if err != nil {
		t.Fatalf("TargetFromRow: %v", err)
	}
	wantURL := "https://repo/current/figlet-2.2.5_6.x86_64.xbps"
	if target.URL != wantURL || target.SignatureURL != wantURL+".sig2" {
		t.Errorf("urls = %q / %q", target.URL, target.SignatureURL)
	}
	if target.Filename != "figlet-2.2.5_6.x86_64.xbps" || target.Pkgver != "figlet-2.2.5_6" ||
		target.Arch != "x86_64" || target.Repository != "https://repo/current" ||
		target.SHA256 != "deadbeef" || target.Size != 94372 {
		t.Errorf("target = %+v", target)
	}
}

// TestPlannedDownloadRowsIsWiderThanPredownload pins FR-028's decision: every
// downloading action yields a --print-uris object, including local and
// zero-size rows; hold/remove/configure do not.
func TestPlannedDownloadRowsIsWiderThanPredownload(t *testing.T) {
	rows := []xbps.Row{
		{Name: "remote", Version: "1.0_1", Action: xbps.ActionUpdate, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 100, HasDownload: true},
		{Name: "local", Version: "1.0_1", Action: xbps.ActionInstall, Arch: "x86_64", Repo: "/srv/repo", DownloadSize: 0, HasDownload: true},
		{Name: "zero", Version: "1.0_1", Action: xbps.ActionReinstall, Arch: "noarch", Repo: "https://repo/current", DownloadSize: 0, HasDownload: true},
		{Name: "held", Version: "1.0_1", Action: xbps.ActionHold, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 100, HasDownload: true},
		{Name: "removed", Version: "1.0_1", Action: xbps.ActionRemove, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 100, HasDownload: true},
	}
	var names []string
	for _, row := range PlannedDownloadRows(rows) {
		names = append(names, row.Name)
	}
	want := []string{"remote", "local", "zero"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("PlannedDownloadRows = %v, want %v", names, want)
	}
	localURL, err := ArchiveURL("/srv/repo", "local-1.0_1", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if localURL != "/srv/repo/local-1.0_1.x86_64.xbps" {
		t.Errorf("local url = %q", localURL)
	}
}

// TestTargetsFromRowsFiltersFTPAndLocal pins #72's pre-target filter: ftp and
// local rows never reach the cache-mode engine, and their counts are returned
// for the FR-037 warnings.
func TestTargetsFromRowsFiltersFTPAndLocal(t *testing.T) {
	rows := []xbps.Row{
		{Name: "good", Version: "1.0_1", Action: xbps.ActionUpdate, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 100, HasDownload: true},
		{Name: "ftp", Version: "1.0_1", Action: xbps.ActionInstall, Arch: "x86_64", Repo: "ftp://ftp.example/current", DownloadSize: 100, HasDownload: true},
		{Name: "local", Version: "1.0_1", Action: xbps.ActionInstall, Arch: "x86_64", Repo: "/srv/repo", DownloadSize: 100, HasDownload: true},
		{Name: "held", Version: "1.0_1", Action: xbps.ActionHold, Arch: "x86_64", Repo: "https://repo/current", DownloadSize: 100, HasDownload: true},
	}
	hashes := map[HashKey]string{HashKeyFor("https://repo/current", "good-1.0_1"): "abc"}
	targets, ftp, local, err := TargetsFromRows(rows, hashes)
	if err != nil {
		t.Fatalf("TargetsFromRows: %v", err)
	}
	if len(targets) != 1 || targets[0].Pkgver != "good-1.0_1" || targets[0].SHA256 != "abc" {
		t.Errorf("targets = %+v", targets)
	}
	if ftp != 1 || local != 1 {
		t.Errorf("exclusions = ftp %d, local %d, want 1/1", ftp, local)
	}
}

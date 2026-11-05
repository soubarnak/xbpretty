// Package download is xbpretty's own archive fetch engine: it derives one URL
// pair per package from plan metadata, transfers the signature and the archive
// (three concurrent transfers per host, four attempts), verifies SHA-256 from
// repository metadata, and publishes the result with `.part` plus an atomic
// rename. RSA verification is deliberately absent: xbps owns it (ADR-0014).
package download

import (
	"fmt"
	"strings"

	"github.com/soubarnak/xbpretty/internal/xbps"
)

// ActionDownload is FR-036's plan action for an archive whose size xbps did
// not report. xbps itself does not print it today; the predicate accepts it
// because the spec names it.
const ActionDownload = "download"

// MaxRedirects is FR-038's redirect budget.
const MaxRedirects = 10

// Filename returns the archive's file name and rejects metadata that could
// escape the destination directory (FR-037).
func Filename(pkgver, arch string) (string, error) {
	name := pkgver + "." + arch + ".xbps"
	if strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid package filename from repository metadata: %s", pkgver)
	}
	return name, nil
}

// ArchiveURL derives `<repository>/<pkgver>.<arch>.xbps` from plan metadata.
// Only a trailing slash is trimmed: no mirror, path suffix or alternate file
// name is ever invented (FR-037).
func ArchiveURL(repository, pkgver, arch string) (string, error) {
	name, err := Filename(pkgver, arch)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(repository, "/") + "/" + name, nil
}

// SignatureURL is always the archive URL with `.sig2` appended (FR-037).
func SignatureURL(archiveURL string) string {
	return archiveURL + ".sig2"
}

// Target is one archive the engine transfers. #72 builds these from plan rows
// and the bulk hash pass; `download` builds them from per-name resolution.
type Target struct {
	Package      string
	Pkgver       string
	Repository   string
	Arch         string
	Filename     string
	URL          string
	SignatureURL string
	SHA256       string // empty when repository metadata has no hash
	Size         uint64 // the plan's exact download size, when known
}

// TargetFromRow derives one target from a plan row and its metadata hash.
func TargetFromRow(row xbps.Row, sha256 string) (Target, error) {
	return NewTarget(row.Name, row.Pkgver(), row.Repo, row.Arch, sha256, row.DownloadSize)
}

// NewTarget builds and validates a target from resolved package metadata.
func NewTarget(pkgname, pkgver, repository, arch, sha256 string, size uint64) (Target, error) {
	name, err := Filename(pkgver, arch)
	if err != nil {
		return Target{}, err
	}
	url, err := ArchiveURL(repository, pkgver, arch)
	if err != nil {
		return Target{}, err
	}
	return Target{
		Package:      pkgname,
		Pkgver:       pkgver,
		Repository:   repository,
		Arch:         arch,
		Filename:     name,
		URL:          url,
		SignatureURL: SignatureURL(url),
		SHA256:       sha256,
		Size:         size,
	}, nil
}

// HashKey is the bulk filename-sha256 pass's key: one repository and one
// pkgver (FR-037).
type HashKey struct {
	Repository string
	Pkgver     string
}

// HashKeyFor normalises only a trailing slash on the repository, the same
// trim the URL derivation does.
func HashKeyFor(repository, pkgver string) HashKey {
	return HashKey{Repository: strings.TrimSuffix(repository, "/"), Pkgver: pkgver}
}

// IsRemoteRepository reports whether predownload may fill the cache from this
// repository: only http(s) rows are downloaded (FR-037).
func IsRemoteRepository(repository string) bool {
	return strings.HasPrefix(repository, "http://") || strings.HasPrefix(repository, "https://")
}

// IsFTPRepository reports an ftp:// repository, which predownload excludes
// and `download` refuses (FR-037).
func IsFTPRepository(repository string) bool {
	return strings.HasPrefix(repository, "ftp://")
}

// IsDownloadRow reports whether a plan row belongs to FR-036's download set:
// a downloading action on an http(s) repository with a reported download size,
// or a download action whose size xbps did not report. hold, remove and
// configure are excluded even when their size fields are non-zero.
func IsDownloadRow(row xbps.Row) bool {
	switch row.Action {
	case xbps.ActionInstall, xbps.ActionUpdate, xbps.ActionReinstall, ActionDownload:
	default:
		return false
	}
	if !IsRemoteRepository(row.Repo) {
		return false
	}
	if row.DownloadSize > 0 {
		return true
	}
	return row.Action == ActionDownload && !row.HasDownload
}

// DownloadRows filters a plan to FR-036's download set, preserving plan order.
func DownloadRows(rows []xbps.Row) []xbps.Row {
	out := make([]xbps.Row, 0, len(rows))
	for _, row := range rows {
		if IsDownloadRow(row) {
			out = append(out, row)
		}
	}
	return out
}

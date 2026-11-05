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

// isDownloadingAction reports whether a plan action is one that fetches an
// archive. hold, remove and configure are excluded even when their size
// fields are non-zero.
func isDownloadingAction(action string) bool {
	switch action {
	case xbps.ActionInstall, xbps.ActionUpdate, xbps.ActionReinstall, ActionDownload:
		return true
	}
	return false
}

// IsDownloadRow reports whether a plan row belongs to FR-036's predownload
// set: a downloading action on an http(s) repository with a reported download
// size, or a download action whose size xbps did not report.
func IsDownloadRow(row xbps.Row) bool {
	if !isDownloadingAction(row.Action) || !IsRemoteRepository(row.Repo) {
		return false
	}
	if row.DownloadSize > 0 {
		return true
	}
	return row.Action == ActionDownload && !row.HasDownload
}

// DownloadRows filters a plan to FR-036's predownload set, preserving plan
// order. Local, file:// and ftp:// rows are excluded; #72 gets their counts
// from TargetsFromRows so it can print the FR-037 exceptions.
func DownloadRows(rows []xbps.Row) []xbps.Row {
	out := make([]xbps.Row, 0, len(rows))
	for _, row := range rows {
		if IsDownloadRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// PlannedDownloadRows is the --print-uris row set (FR-028): every plan row
// with a downloading action, hold excluded, with no repository-scheme or size
// filter. FR-028 calls these "the plan's downloading actions" and defines the
// URL purely as <repository>/<pkgver>.<architecture>.xbps, so a local file:
// repository or an unreported size still yields one object. This is
// deliberately wider than FR-036's predownload set above; do not fold the two
// predicates together.
func PlannedDownloadRows(rows []xbps.Row) []xbps.Row {
	out := make([]xbps.Row, 0, len(rows))
	for _, row := range rows {
		if isDownloadingAction(row.Action) {
			out = append(out, row)
		}
	}
	return out
}

// PartitionDownloadRows splits FR-036's downloading rows into the http(s)
// rows that become targets and the excluded repository kinds. One predicate
// decides both the download set and the FR-037 warning counts, so the warning
// and the targets can never disagree.
func PartitionDownloadRows(rows []xbps.Row) (remote []xbps.Row, excludedFTP, excludedLocal int) {
	for _, row := range rows {
		if !isDownloadingAction(row.Action) {
			continue
		}
		switch {
		case IsFTPRepository(row.Repo):
			excludedFTP++
		case !IsRemoteRepository(row.Repo):
			excludedLocal++
		default:
			if row.DownloadSize == 0 && !(row.Action == ActionDownload && !row.HasDownload) {
				continue
			}
			remote = append(remote, row)
		}
	}
	return remote, excludedFTP, excludedLocal
}

// TargetsFromRows builds FR-036 predownload targets from plan rows and the
// bulk filename-sha256 map. It returns how many ftp:// and local/other rows
// were excluded so #72 can print the FR-037 warnings. ftp:// rows never reach
// the engine, so its `xbp download`-specific error text cannot leak into the
// predownload path.
func TargetsFromRows(rows []xbps.Row, hashes map[HashKey]string) (targets []Target, excludedFTP, excludedLocal int, err error) {
	remote, excludedFTP, excludedLocal := PartitionDownloadRows(rows)
	for _, row := range remote {
		target, err := TargetFromRow(row, hashes[HashKeyFor(row.Repo, row.Pkgver())])
		if err != nil {
			return nil, excludedFTP, excludedLocal, err
		}
		targets = append(targets, target)
	}
	return targets, excludedFTP, excludedLocal, nil
}

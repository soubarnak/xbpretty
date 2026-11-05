package cli

import (
	"fmt"

	"github.com/soubarnak/xbpretty/internal/discovery"
	"github.com/soubarnak/xbpretty/internal/download"
)

// printURIObject is FR-028's NDJSON object. Field order is the spec's.
type printURIObject struct {
	Schema       int     `json:"schema"`
	Package      string  `json:"package"`
	Pkgver       string  `json:"pkgver"`
	Repository   string  `json:"repository"`
	URL          string  `json:"url"`
	SignatureURL string  `json:"signature_url"`
	Size         uint64  `json:"size"`
	SHA256       *string `json:"sha256"`
}

// runPrintURIs implements FR-028: one object per planned download on stdout,
// then stop. It runs no sync, no summary, no prompt, no download and no
// transaction, and needs no root.
func (st *state) runPrintURIs(cmd string, opts *options) int {
	rows, code := st.planTransaction(cmd, opts, nil)
	if code >= 0 {
		return code
	}
	hashes := st.bulkFilenameSHA256()
	for _, row := range download.DownloadRows(rows) {
		url, err := download.ArchiveURL(row.Repo, row.Pkgver(), row.Arch)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		obj := printURIObject{
			Schema:       discovery.SchemaVersion,
			Package:      row.Name,
			Pkgver:       row.Pkgver(),
			Repository:   row.Repo,
			URL:          url,
			SignatureURL: download.SignatureURL(url),
			Size:         row.DownloadSize,
		}
		if sha, ok := hashes[download.HashKeyFor(row.Repo, row.Pkgver())]; ok {
			obj.SHA256 = &sha
		}
		if err := discovery.WriteNDJSON(st.stdout, obj); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
	}
	return exitOK
}

// bulkFilenameSHA256 runs FR-037's one offline bulk hash pass. A failure is
// not fatal for --print-uris: the hash is null when unavailable.
func (st *state) bulkFilenameSHA256() map[download.HashKey]string {
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-R", "-s", "", "-p", "filename-sha256")
	if err != nil || res.exitCode != 0 {
		if st.r.verbose {
			fmt.Fprintln(st.stderr, "verbose: the repository metadata hash pass failed; sha256 will be null")
		}
		return map[download.HashKey]string{}
	}
	return download.ParseHashes(res.stdout)
}

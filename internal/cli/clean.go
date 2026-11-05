package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/soubarnak/xbpretty/internal/fetch"
	"github.com/soubarnak/xbpretty/internal/hints"
)

// xbpsDBDir is the system package database root FR-020's `clean --lists`
// walks for cached repodata. The xbpretty_testroot hook may point it at a
// fixture; the release binary always uses /var/db/xbps.
var xbpsDBDir = "/var/db/xbps"

// runClean implements FR-020. `clean` runs `xbps-remove -O` as root with no
// prompt and streams its output; `--lists` only deletes cached repodata and
// prints nothing. `clean` no longer aliases autoremove and never runs it.
func (st *state) runClean(opts *options) int {
	// --lists wins if both are given; the next install/upgrade needs
	// `xbp update`.
	if opts.lists {
		return st.cleanLists(opts)
	}
	// FR-020: every clean form is root-only, including the #74 seam.
	if euidFn() != 0 {
		if opts.cleanFetch {
			// FR-048 names the revert, not the generic clean hint.
			fmt.Fprintln(st.stderr, "hint: run with sudo to restore the repository configuration")
		} else {
			st.privHint("clean", opts)
		}
		return exitRuntime
	}
	if opts.cleanFetch {
		return st.cleanFetchRestore(opts)
	}
	if err := st.r.cfg.ResolveFor("remove"); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	// FR-066: xbps-remove -O's output is forwarded child output, so both
	// streams go to stderr; streamStderr also keeps them for the hint table.
	code, output, err := st.r.streamStderr(st.r.cfg.Xbps.RemovePath, "-O")
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if code != 0 {
		if h := hints.Lookup(code, output); h != nil {
			fmt.Fprintf(st.stderr, "hint: %s\n", h.Text)
		}
	}
	return code
}

// cleanLists deletes every `*-repodata` file one directory below the xbps
// database root. Repository keys live in the keys/ subdirectory and never
// match `*-repodata`, so they stay, and the next install fails until
// `xbp update` refreshes the index.
func (st *state) cleanLists(opts *options) int {
	if euidFn() != 0 {
		st.privHint("clean", opts)
		return exitRuntime
	}
	matches, err := filepath.Glob(filepath.Join(xbpsDBDir, "*", "*-repodata"))
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: cannot list cached repodata: %v\n", err)
		return exitRuntime
	}
	for _, path := range matches {
		info, err := os.Lstat(path)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: cannot remove %s: %v\n", path, err)
			return exitRuntime
		}
		if info.IsDir() {
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: cannot remove %s: %v\n", path, err)
			return exitRuntime
		}
	}
	return exitOK
}

// cleanFetchRestore implements FR-048: restore a listed backup over its path,
// or delete a file fetch created, then delete the manifest. It is manifest-
// driven, so it never guesses a fixed filename. A hand-edited file is warned
// about and still reverted: leaving a broken fetch-written config behind is
// worse than the warning.
func (st *state) cleanFetchRestore(_ *options) int {
	manifest, err := fetch.ReadManifestFor(fetchManifestPath, fetchWriteDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(st.stderr, "xbpretty: no repository configuration written by xbp fetch")
			return exitRuntime
		}
		fmt.Fprintf(st.stderr, "xbpretty: reading %s: %v\n", fetchManifestPath, err)
		return exitRuntime
	}

	for _, entry := range manifest.Files {
		info, statErr := os.Lstat(entry.Path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			fmt.Fprintf(st.stderr, "xbpretty: restoring %s: %v\n", entry.Path, statErr)
			return exitRuntime
		}
		if !info.Mode().IsRegular() {
			fmt.Fprintf(st.stderr, "xbpretty: restoring %s: is not a regular file\n", entry.Path)
			return exitRuntime
		}
		current, readErr := fetch.ReadRegularFileNoFollow(entry.Path)
		if readErr != nil {
			fmt.Fprintf(st.stderr, "xbpretty: restoring %s: %v\n", entry.Path, readErr)
			return exitRuntime
		}
		if entry.SHA256 != "" && contentSHA256(current) != entry.SHA256 {
			if entry.Backup != "" {
				fmt.Fprintf(st.stderr, "xbpretty: %s was modified after fetch; restoring %s\n", entry.Path, entry.Backup)
			} else {
				fmt.Fprintf(st.stderr, "xbpretty: %s was modified after fetch; removing it\n", entry.Path)
			}
		}
		if entry.Backup != "" {
			content, backupErr := fetch.ReadRegularFileNoFollow(entry.Backup)
			if backupErr != nil {
				fmt.Fprintf(st.stderr, "xbpretty: restoring %s: %v\n", entry.Path, backupErr)
				return exitRuntime
			}
			if err := fetch.WriteAtomic(entry.Path, content); err != nil {
				fmt.Fprintf(st.stderr, "xbpretty: restoring %s: %v\n", entry.Path, err)
				return exitRuntime
			}
			// The backup has been consumed. A later fetch must snapshot
			// afresh rather than reuse this pre-fetch file.
			if err := os.Remove(entry.Backup); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(st.stderr, "xbpretty: restoring %s: removing backup %s: %v\n", entry.Path, entry.Backup, err)
				return exitRuntime
			}
			if st.r != nil && st.r.verbose {
				fmt.Fprintf(st.stderr, "verbose: restored %s\n", entry.Path)
			}
			continue
		}
		if err := os.Remove(entry.Path); err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: removing %s: %v\n", entry.Path, err)
			return exitRuntime
		}
		if st.r != nil && st.r.verbose {
			fmt.Fprintf(st.stderr, "verbose: removed %s\n", entry.Path)
		}
	}

	if err := os.Remove(fetchManifestPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(st.stderr, "xbpretty: removing %s: %v\n", fetchManifestPath, err)
		return exitRuntime
	}
	return exitOK
}

func contentSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"

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
	if opts.cleanFetch {
		// TODO(#74): restore the repository configuration written by fetch.
		return st.cleanFetchRestore(opts)
	}

	if err := st.r.cfg.ResolveFor("remove"); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if euidFn() != 0 {
		st.privHint("clean", opts)
		return exitRuntime
	}
	code, err := st.r.stream(st.r.cfg.Xbps.RemovePath, "-O")
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if code != 0 {
		if h := hints.Lookup(code, hints.Output{Stderr: st.r.lastStderr}); h != nil {
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

// cleanFetchRestore is the #74 seam for `clean --fetch`. The flag must never
// fall through to `xbps-remove -O`; #74 owns restoring fetch's repository
// configuration.
func (st *state) cleanFetchRestore(_ *options) int {
	fmt.Fprintln(st.stderr, "xbpretty: clean --fetch is not implemented in this build")
	return exitRuntime
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/fetch"
)

// Fetch seams. They are package variables only so the tests can inject a local
// fixture; nothing in the release binary can change them. A built-binary test
// hook that needs them is compiled only under the xbpretty_testroot tag.
var (
	fetchMirrorListURL = fetch.MirrorListURL
	fetchXBPSDDirs     = []string{"/etc/xbps.d", "/usr/share/xbps.d"}
	fetchHTTPClient    = fetch.NewHTTPClient
	fetchArch          = runtimeXBPSArch()
	fetchPreviousHosts = func() []string { return fetch.ManifestMirrorHosts(fetch.DefaultManifestPath) }
	// pickMirror is #78's picker seam. Nil means the non-TTY top-3 shortlist,
	// which is also the documented fallback while the picker is unbuilt.
	pickMirror func(ranked []fetch.Result) (int, bool)
	// mirrorScoreEvents receives the MirrorScore progress events. #66 renders
	// them; this ticket owns only the emission.
	mirrorScoreEvents = func(fetch.ProgressEvent) {}
)

const fetchMirrorListTimeout = 10 * time.Second

// fetchWritePlan is everything #74's write step needs from the read-only
// pipeline: the one chosen mirror, the repository suffixes it was validated
// against, and whether --non-free asked for derived files.
type fetchWritePlan struct {
	Mirror       fetch.Result
	Mirrors      []fetch.Mirror // the live list (or snapshot) after filtering
	Repositories fetch.RepositorySet
	Suffixes     []string
	Arch         string
	NonFree      bool
	WrittenAt    time.Time
}

// runFetch is FR-042's read-only pipeline plus the #74 write seam.
func (st *state) runFetch(opts *options) int {
	cfg, err := fetch.DiscoverRepositories(fetchXBPSDDirs)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}

	client := fetchHTTPClient()
	userAgent := "xbpretty/" + Version
	ctx, cancel := context.WithTimeout(context.Background(), fetchMirrorListTimeout)
	mirrors, warnings, err := fetch.FetchMirrorsDetailed(ctx, client, fetchMirrorListURL, userAgent)
	cancel()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: mirror list unavailable; using the built-in snapshot from %s\n", fetch.SnapshotDate)
		mirrors, err = fetch.Snapshot()
		if err != nil {
			fmt.Fprintln(st.stderr, "xbpretty: no mirror list available")
			return exitRuntime
		}
		warnings = nil
	}
	if st.r != nil && st.r.verbose {
		for _, warning := range warnings {
			fmt.Fprintf(st.stderr, "verbose: mirror list: %s\n", warning)
		}
	}

	repos, err := fetch.ClassifyRepositories(cfg, fetch.MirrorHosts(mirrors), fetchPreviousHosts())
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	arch := repos.Arch
	if arch == "" {
		arch = fetchArch
	}

	mirrors = fetch.FilterMirrors(mirrors, opts.country)
	if len(mirrors) == 0 {
		fmt.Fprintln(st.stderr, "xbpretty: no mirrors match the requested country or region")
		return exitRuntime
	}
	if opts.httpsOnly {
		for i := range mirrors {
			mirrors[i] = fetch.RewriteToHTTPS(mirrors[i])
		}
	}

	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Client:      client,
		Mirrors:     mirrors,
		PrimaryPath: repos.Primary,
		Arch:        arch,
		UserAgent:   userAgent,
		Progress:    mirrorScoreEvents,
	})
	if len(report.Failures) > 0 {
		if st.r != nil && st.r.verbose {
			for _, failure := range report.Failures {
				fmt.Fprintf(st.stderr, "verbose: %s: %v\n", failure.Mirror.URL, failure.Err)
			}
		}
		fmt.Fprintf(st.stderr, "xbpretty: %d of %d mirrors did not respond\n", len(report.Failures), len(mirrors))
	}

	ranked := fetch.Rank(report.Results)
	if len(ranked) == 0 {
		fmt.Fprintln(st.stderr, "xbpretty: no mirrors responded")
		return exitRuntime
	}

	suffixes := append([]string(nil), repos.Suffixes...)
	if opts.nonFree {
		suffixes = appendUnique(suffixes, fetch.NonFreeSuffixes(repos.Suffixes)...)
	}

	allowPicker := !opts.dryRun && !opts.explicit["auto"] && euidFn() == 0 &&
		isTTY(st.stdin) && isTTY(st.stdout) && st.mode != config.ModePlain &&
		!strings.EqualFold(os.Getenv("TERM"), "dumb") && utf8Locale()
	chosen, ok := st.chooseMirror(client, ranked, suffixes, arch, userAgent, opts, allowPicker)
	if !ok {
		return exitRuntime
	}

	if err := fetch.RenderTable(st.stdout, ranked, chosen.URL); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}

	if opts.dryRun {
		fmt.Fprintln(st.stdout, "Dry run: no repository configuration was written.")
		return exitOK
	}
	if euidFn() != 0 {
		fmt.Fprintln(st.stderr, "hint: run with sudo to write the repository configuration")
		return exitRuntime
	}

	// TODO(#74): rewrite the managed repository= lines, save
	// .xbpretty-backup, write /var/lib/xbpretty/fetch.json, run
	// `xbps-install -S` and revert on failure. #73 owns only this seam.
	paths, code, err := st.writeRepositoryConfiguration(fetchWritePlan{
		Mirror:       chosen,
		Mirrors:      mirrors,
		Repositories: repos,
		Suffixes:     suffixes,
		Arch:         arch,
		NonFree:      opts.nonFree,
		WrittenAt:    time.Now(),
	})
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return code
	}
	for _, path := range paths {
		fmt.Fprintf(st.stdout, "Sources have been written to %s\n", path)
	}
	return code
}

// chooseMirror implements FR-045's shortlist rule. --auto N validates the N
// fastest in rank order; without it the top 3 are tried. The picker seam, when
// #78 installs one, returns a single index whose choice is validated once.
func (st *state) chooseMirror(client *http.Client, ranked []fetch.Result, suffixes []string, arch, userAgent string, opts *options, allowPicker bool) (fetch.Result, bool) {
	validate := func(candidate fetch.Result) error {
		return fetch.Validate(context.Background(), fetch.ValidationOptions{
			Client:    client,
			BaseURL:   candidate.URL,
			Suffixes:  suffixes,
			Arch:      arch,
			UserAgent: userAgent,
		})
	}

	if pickMirror != nil && allowPicker {
		index, ok := pickMirror(ranked)
		if !ok {
			return fetch.Result{}, false
		}
		if index < 0 || index >= len(ranked) {
			fmt.Fprintln(st.stderr, "xbpretty: no candidate mirror serves all configured repositories")
			return fetch.Result{}, false
		}
		if err := validate(ranked[index]); err != nil {
			if st.r != nil && st.r.verbose {
				fmt.Fprintf(st.stderr, "verbose: %s: %v\n", ranked[index].URL, err)
			}
			fmt.Fprintln(st.stderr, "xbpretty: no candidate mirror serves all configured repositories")
			return fetch.Result{}, false
		}
		return ranked[index], true
	}

	depth := 3
	if opts.explicit["auto"] {
		depth = opts.auto
	}
	if depth > len(ranked) {
		depth = len(ranked)
	}
	for _, candidate := range ranked[:depth] {
		if err := validate(candidate); err != nil {
			if st.r != nil && st.r.verbose {
				fmt.Fprintf(st.stderr, "verbose: %s: %v\n", candidate.URL, err)
			}
			continue
		}
		return candidate, true
	}
	fmt.Fprintln(st.stderr, "xbpretty: no candidate mirror serves all configured repositories")
	return fetch.Result{}, false
}

// writeRepositoryConfiguration is #74's single write seam. This ticket does
// not touch /etc/xbps.d, backups, manifests, the sync validation or revert.
func (st *state) writeRepositoryConfiguration(plan fetchWritePlan) ([]string, int, error) {
	_ = plan
	return nil, exitRuntime, errors.New("writing the repository configuration is not implemented in this build")
}

// appendUnique appends values that are not already present, preserving order.
func appendUnique(dst []string, values ...string) []string {
	seen := map[string]bool{}
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			dst = append(dst, v)
		}
	}
	return dst
}

// utf8Locale reports whether the effective locale is UTF-8. LC_ALL wins over
// LC_CTYPE, which wins over LANG; an unset environment is the C locale.
func utf8Locale() bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		return strings.Contains(lower, "utf-8") || strings.Contains(lower, "utf8")
	}
	return false
}

// runtimeXBPSArch maps Go's architecture name to xbps's.
func runtimeXBPSArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "i686"
	case "arm64":
		return "aarch64"
	case "arm":
		// Void packages 32-bit ARM as armv6l or armv7l. GOARM is a
		// compile-time setting, so read it back from the build info.
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "GOARM" && setting.Value == "6" {
					return "armv6l"
				}
			}
		}
		return "armv7l"
	default:
		return runtime.GOARCH
	}
}

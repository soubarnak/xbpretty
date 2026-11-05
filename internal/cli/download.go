package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/download"
)

// runDownload implements FR-039: named packages only, archives and their
// signatures written to the current directory with SHA-256 verification. No
// root, summary, prompt, sync, log or history.
func (st *state) runDownload(opts *options) int {
	if err := st.r.cfg.ResolveFor("query"); err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	names := dedupeNames(opts.args)
	if len(names) == 0 {
		fmt.Fprintln(st.stderr, "xbpretty: download needs at least one package")
		return exitRuntime
	}
	// FR-039: no repositories at all is its own refusal.
	repos, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-L")
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if repos.exitCode != 0 {
		fmt.Fprint(st.stderr, repos.stderr)
		return repos.exitCode
	}
	if strings.TrimSpace(repos.stdout) == "" {
		fmt.Fprintln(st.stderr, "xbpretty: no repositories are configured; add one in /etc/xbps.d")
		return exitRuntime
	}

	var targets []download.Target
	var missing []string
	failedNames := false
	for _, name := range names {
		res, err := st.r.run(st.r.cfg.Xbps.QueryPath, "-R", "-S", name,
			"-p", "pkgver,repository,architecture,filename-sha256")
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		if res.exitCode != 0 {
			// FR-039's not-found case is xbps's silent exit 2. Any other
			// failure is the child's, and its stderr and code pass through.
			if res.exitCode == 2 && strings.TrimSpace(res.stderr) == "" {
				missing = append(missing, name)
				fmt.Fprintf(st.stderr, "xbpretty: %s: not found in any configured repository\n", name)
				continue
			}
			fmt.Fprint(st.stderr, res.stderr)
			return res.exitCode
		}
		resolved, err := download.ParseResolved(res.stdout)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %s: cannot resolve the package: %v\n", name, err)
			return exitRuntime
		}
		// FR-037: no metadata hash means the cwd copy would be unverifiable.
		if !resolved.HasHash {
			fmt.Fprintf(st.stderr, "xbpretty: %s: repository metadata has no SHA-256; refusing to write an unverifiable package\n", resolved.Pkgver)
			failedNames = true
			continue
		}
		if download.IsFTPRepository(resolved.Repository) {
			fmt.Fprintf(st.stderr, "xbpretty: %s: ftp:// repositories are not supported by xbp download\n", resolved.Pkgver)
			failedNames = true
			continue
		}
		target, err := download.NewTarget(name, resolved.Pkgver, resolved.Repository, resolved.Arch, resolved.SHA256, 0)
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return st.finishDownload(missing, failedNames, nil)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	engineOpts := download.Options{
		DestDir:     cwd,
		Mode:        download.ModeDownload,
		Concurrency: 1,
	}
	if st.r.verbose {
		engineOpts.Log = func(format string, args ...any) {
			fmt.Fprintf(st.stderr, "verbose: "+format+"\n", args...)
		}
	}
	engine := download.New(engineOpts)

	// FR-074/FR-041: Ctrl-C cancels the engine, which removes `.part`, then
	// the run reports the interruption and exits 130.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	written := make([]download.Result, 0, len(targets))
	for _, target := range targets {
		if ctx.Err() != nil {
			fmt.Fprintln(st.stderr, "xbpretty: interrupted")
			return exitInterrupted
		}
		result := engine.FetchOne(ctx, target)
		if result.Err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(st.stderr, "xbpretty: interrupted")
				return exitInterrupted
			}
			// FR-038: an explicit download aborts on the first exhausted or
			// mismatching file and keeps everything already verified.
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", result.Err)
			return exitRuntime
		}
		written = append(written, result)
	}
	return st.finishDownload(missing, failedNames, written)
}

// finishDownload prints the success block only for a complete run. Missing
// names and refused names both suppress it; missing names add FR-039's
// summary line.
func (st *state) finishDownload(missing []string, failedNames bool, written []download.Result) int {
	if len(missing) > 0 {
		fmt.Fprintln(st.stderr, "xbpretty: Some packages were not found.")
	}
	if failedNames || len(missing) > 0 {
		return exitRuntime
	}
	fmt.Fprintln(st.stdout, "Downloads Complete:")
	for _, result := range written {
		name := result.Target.Filename
		fmt.Fprintf(st.stdout, "  %s was written to ./%s\n", name, name)
	}
	return exitOK
}

// dedupeNames keeps the first occurrence of every package name (FR-039).
func dedupeNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

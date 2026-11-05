# ADR-0019: Packaging and release pipeline for rev 6

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-001, FR-004–FR-006, FR-076, AC-001, AC-003–AC-004, AC-058–AC-060

## Context

Rev 6 adds nine commands, a second config layer, a TUI engine and an HTTP/TLS
stack. nala's packaging is Debian-specific: `clap_complete` dynamic completion,
`clap_mangen` per-subcommand pages, a shipped `/etc/nala/nala.conf`. xbps-src's
`go` build style runs only `go build`/`go test`, and a cross build cannot run
the binary it just built, so generated artifacts must be committed. The old
size budget assumed no TLS.

## Decision

1. **Version** — rev 6 is `0.4.0`, rev 7 is `0.5.0`. `srcpkgs/xbpretty/template`
   is the single source; the tag, `--version`, `go_ldflags` and the release
   title all derive from it. No `CHANGELOG.md`.
2. **Aliases** — the set stays closed at six (`xbp`, `xbp-install`,
   `xbp-remove`, `xbp-upgrade`, `xbp-autoremove`, `xbp-update`). Every new
   command is `xbp <command>`; no new `xbp-*` name.
3. **Completions and manpages** — static generated files from one command
   table, committed, installed via `vcompletion`/`vman`, freshness-tested and
   syntax/lint-checked. One `xbpretty.8` plus `xbpretty.conf.5`; six `man8`
   symlinks. No dynamic `COMPLETE=` protocol, no per-command pages, no build-time
   generator invocation in xbps-src.
4. **No `/etc`** — no `/etc/xbpretty/config.toml`, no `conf_files`; the example
   and migration guide install under `/usr/share/doc/xbpretty/`.
5. **Mirror snapshot** — embedded `mirrors.lst` plus date; `make mirrors`
   refreshes; the release manager runs it when older than 90 days. No bot, no
   runtime cache.
6. **Template** — `version`, `revision=1`, `build_style=go`, version-only
   `go_ldflags` (never `-s`/`-w`), `depends="xbps>=0.60.0"`, `pre_build` with
   `CGO_ENABLED=0` and `GOFLAGS="-trimpath -buildvcs=false"`, the exact install
   list, default `do_check`. 9 architectures, glibc+musl sharing a binary.
7. **Release** — one job loops the 9 architectures; gates on tests/vet,
   completion/manpage freshness, shell syntax, static link, 8,000,000-byte
   binary and 3,600,000-byte x86_64 package budgets, package contents (no
   `/etc`), clean-root install and signed-repository completeness; a missing
   `XBPS_PRIVKEY` fails the release. The template job bumps version/revision/
   checksum.
8. **Size** — #50's ≤100 KB budget applies to the TUI engine's own delta; the
   `net/http`/TLS stack (~4.67 MB) is the reason for the re-based envelope.

## Consequences

- The release is reproducible from the tag: no generator or network step
  mutates the tree.
- A missing key can never publish an unsigned repository.
- The alias set, no-`/etc` rule and version boundary are hard to reverse; each
  needs a new spec revision.

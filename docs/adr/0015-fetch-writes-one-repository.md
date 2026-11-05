# ADR-0015: Fetch writes one repository and reverts through a manifest

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-042–FR-050, AC-034–AC-036, AC-065

## Context

nala benchmarks Debian/Ubuntu mirrors and writes a multi-URI sources file. xbps
is not a load balancer: the first repository offering a package wins, there is
no per-package download failover, and every configured repository is synced.
Writing N mirrors would give no balancing, N× sync cost and a silently mixed
package set. Void publishes one mirror list, and the mirror must be written into
the system `repository=` configuration, then proved.

## Decision

1. **One mirror** — `fetch` selects exactly one mirror and writes it into every
   managed `repository=` line. `--auto [N]` keeps N as a validation shortlist
   depth (bare 3), not a write count. Non-TTY uses the same top-3 shortlist.
2. **Source** — `https://xmirror.voidlinux.org/raw/mirrors.lst`, TSV, with an
   embedded snapshot fallback and no runtime disk cache.
3. **Probe** — one 256 KiB ranged GET of `<mirror><primary>/<arch>-repodata`,
   zstd-magic validated, score = wall-clock ms, latency = TTFB, 10 concurrent,
   2/2/5 s timeouts, deterministic score/latency/tier/URL order, no tier
   weighting.
4. **Write** — one file per managed basename in `/etc/xbps.d`, same basename as
   the system file it masks, preserving every non-managed line, atomic
   temp+rename, `0644 root:root`, `.xbpretty-backup` on first write, manifest
   `/var/lib/xbpretty/fetch.json` (schema 1) as the sole revert source.
5. **Prove and revert** — after writing, run `xbps-install -S`; xbps always
   exits 0 from `-S`, so scan for per-repository `ERROR:` lines scoped to the
   repositories this run wrote, revert on failure and exit non-zero/1.
   `clean --fetch` restores or deletes through the manifest.
6. **Rootless** — probe and print the ranking read-only, mark the would-be
   choice, print the sudo hint and exit 1; `--dry-run` does the same but exits
   0. The root check is after probing and before the picker.
7. **Output** — the ranking table and the `Sources have been written to ...`
   lines are stdout (durable); progress, warnings, hints and the sync output are
   stderr. The picker is single-select; a cancelled pick exits 1.

## Consequences

- Re-running `fetch` is idempotent and can keep the fastest mirror.
- A bad write is recoverable without remembering the previous file.
- A custom or local repository is never silently rewritten.
- The embedded snapshot makes offline runs useful; `make mirrors` refreshes it
  at release time.

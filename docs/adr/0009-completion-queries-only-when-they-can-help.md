# ADR-0009: Completion queries only when they can help

- Status: Accepted
- Date: 2026-10-18
- Relates to: FR-024, AC-025
- Answers: which completion queries run, and what the remaining ones cost (v1.1,
  issue #14, ticket #18)

## Context

The generated completion scripts called `xbps-query` on every TAB with no
guard:

- `install` completion ran `xbps-query -Rs <word>` on an empty word, which
  searches the whole repository.
- Zsh and fish ran the same search when the word was a flag (`xbp install -<TAB>`).
  Bash already skipped flag words.
- `autoremove` offered installed package names, though it takes none. FR-024
  names only `remove` for installed names.

Go also carried three helpers (`List`, `Installed`, `Available`) that exec
`xbps-query`. Nothing called them: the shell scripts keep their own pipelines.

## Decision

1. **`install` searches only for a word the user has started.** In bash, zsh
   and fish, completion after `xbp install` runs `xbps-query -Rs <word>` only
   when the word is non-empty and does not start with `-`. On an empty word it
   offers nothing.
2. **No shell queries on a flag word.** Zsh and fish now follow bash.
3. **`autoremove` completes no package names**, in any shell.
4. **`remove` still completes installed package names** from `xbps-query -l`
   (AC-025 holds).
5. **The unused Go helpers are deleted** (`List`, `Installed`, `Available` and
   their parsing helpers). The completion package holds only code that runs.
6. **No caching.** The measurement below is the basis for a later decision.

The rules are tested by running the generated scripts against a stub
`xbps-query` that logs its calls. Bash always runs. Zsh and fish run when the
shell is on `PATH` and are skipped otherwise.

A latent bug surfaced while running the fish script for the first time: fish
`string replace -r '-[^-]*$' ''` treats the pattern as an option and fails, so
fish offered no package names at all. The scripts now pass `--` before the
pattern.

## Latency of the remaining queries

Measured on 2026-10-18 on the maintainer's host, not in a container, against
the host's real, full index. Read-only, no root, nothing synced or changed.

- Host: Void Linux, kernel 7.2.11_1, AMD Ryzen 5 7535HS (12 threads), ext4 on
  NVMe. xbps 0.60.7 (API 20250629).
- Index: `https://repo-default.voidlinux.org/current` x86_64 repodata
  2.2 MB (synced 2026-10-18), 14823 packages; plus multilib (0.7 MB, 5740
  packages), nonfree (67) and multilib/nonfree (21), and the maintainer's
  extra repositories. `xbps-query -L` totals 20666 packages over all
  repositories; `xbps-query -l` lists 1409 installed packages.
- Method: a shell loop timed each call with `date +%s%N` before and after,
  stdout and stderr to `/dev/null`, ten runs per command in a row, in
  milliseconds. No warm-up run was discarded. Command line:
  `xbps-query -Rs <prefix>` for each prefix, and `xbps-query -l`.
- Caveat: the host was busy while measuring (load average 17.9 on 12 threads,
  one Claude session and builds running). Absolute times are therefore an
  upper bound and the outliers below are noise from that load.

| Command | Matches | Runs (ms) | Typical |
|---|---|---|---|
| `xbps-query -Rs fig` | 281 lines | 334 322 373 354 338 354 410 371 417 344 | about 350 ms |
| `xbps-query -Rs lib` | 7238 lines | 366 377 415 391 387 392 364 383 385 363 | about 385 ms |
| `xbps-query -Rs python` | 1546 lines | 394 411 377 3246 1099 854 647 377 314 300 | about 380 ms (outliers 3.2 s, 1.1 s) |
| `xbps-query -Rs a` | 19564 lines | 318 369 422 371 541 317 314 310 312 316 | about 320 ms |
| `xbps-query -l` | 1409 lines | 38 38 37 40 40 40 40 36 40 37 | about 38 ms |

Reading: a repository search costs roughly 0.3 to 0.4 s whatever the prefix,
so the cost is the scan of the index, not the number of matches. A prefix of
one letter is as slow as `lib`. The installed list is about ten times cheaper,
so `remove` completion is instant. That makes the empty-word search, which was
the same cost with the largest possible output, worth removing, and leaves a
fixed 0.3 to 0.4 s per TAB on a started word in `install`. Whether that
justifies a cache is for a later effort, which should repeat this measurement
on an idle host.

## Note: `log` completion (ticket #22)

`log` joins the command list in bash, zsh and fish. `xbp log` completes
installed package names for its filter, from the same local `xbps-query -l`
that `remove` uses. Packages removed since are not offered; completing from
the log file itself is not done. On a word that starts with `-` it runs no
`xbps-query` and offers `--limit`, and bash offers nothing after `--limit`
since the value is a number. The query rules above are otherwise unchanged,
and the runtime tests cover `log` in every shell.

## Consequences

- `xbp install <TAB>` on an empty word offers nothing.
- Package-name completion for `autoremove` is gone; the shell offers flags only.
- Fish completion of package names works.
- Rev 4 stays frozen: FR-024 reads as the rules above from here on.

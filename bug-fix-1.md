# bug-fix-1: code review findings and fix plan

- **Date:** 2026-11-06
- **Baseline:** `main` at `d92d66a` (xbpretty 0.4.0, rev 6)
- **Status:** ready to implement

## How to read this plan

Each fix has an ID (`BF-nn`), a severity, the files it touches, the defect, how
to reproduce it, the change, and the tests that prove it. Work through the
phases in order: Phase 0 makes the suite trustworthy, Phase 1 is the visible
nala-parity gap the user reported, and the later phases follow severity.

Severity:

- **critical:** a hang, data loss, or an unintended root action
- **high:** wrong behaviour in a common path, or a broken guarantee from the spec
- **medium:** wrong output or state in a less common path
- **low:** cosmetic, rare, or parity polish

Every finding was checked against the code. Most were reproduced with a
test-root build:

```sh
go build -tags xbpretty_testroot -o "$S/xbp" ./cmd/xbpretty
env XBPRETTY_TEST_EUID=0 XBPRETTY_HISTORY_DIR="$S/hist" PATH="$S/bin:$PATH" \
  script -qec 'stty cols 90 rows 30; "$S/xbp" upgrade' /dev/null
```

Here `$S` is a scratch directory and `$S/bin` holds fake `xbps-install`,
`xbps-query` and `xbps-remove` shell scripts. A plan line from the fake
`xbps-install -n -u` looks like
`curl-8.22.0_1 update x86_64 https://repo-default.voidlinux.org/current 287000 274000`.

Findings marked *(trace)* were confirmed by reading the code path but not
reproduced end to end.

## Review scope

| Area | Covered |
|---|---|
| Transaction pipeline, live progress, PTY engine, `internal/term` | yes |
| History, replay, log, fetch, download, discovery, policy, args | yes |
| Inline summary, full-screen TUI, key decoding, widths | yes |
| Spec vs real nala v0.18.2 source (cloned at tag `v0.18.2`) | yes |
| Config parser, layers, legacy keys; completion and manpage drift | yes (Phase 6) |
| **Security review** of the `[Xbps]` root-trust check, completion-script input handling, and release signing | **not done**; see "Out of scope" |

`go vet ./...` is clean. `go test ./...` **fails** on `main`; see BF-01.

---

## Summary

| ID | Sev | Area | Title |
|---|---|---|---|
| BF-01 | high | tests | Integration tests read the host's real `/var/lib/xbpretty/history` |
| BF-02 | high | parity | `xbp upgrade` sync step shows raw xbps output, not nala's update UI |
| BF-03 | high | parity | Update UI lacks nala's per-repository lines, `Fetched` line and count line |
| BF-04 | medium | progress | Update view repository total counts only `*-repository-*.conf` files |
| BF-05 | medium | progress | In-place renderer silently discards inserted lines |
| BF-06 | critical | engine | A child line over 64 KiB deadlocks the transaction in PTY mode |
| BF-07 | critical | engine | A closed stderr pipe kills xbps mid-transaction with SIGPIPE |
| BF-08 | critical | tui | A paste split by a pause leaks keys; a pasted `y` confirms a root transaction |
| BF-09 | high | log | Root follows invoker-controlled symlinks when writing and chowning the log |
| BF-10 | medium | log | Missing `~/.local` / `~/.local/state` are created root-owned under sudo |
| BF-11 | high | fetch | A second `fetch` breaks `clean --fetch` restoration |
| BF-12 | high | fetch | On musl, `fetch` probes the wrong repodata name and fails |
| BF-13 | medium | fetch | A reverted fetch leaves a stale backup that a later fetch trusts |
| BF-14 | high | args | `--assume-yes=false`, `--dry-run=no`, etc. are accepted and treated as set |
| BF-15 | high | discovery | A held or locally newer installed package loses its installed state in `list` |
| BF-16 | high | progress | Successful live runs drop xbps package messages and child warnings |
| BF-17 | medium | progress | Install view double-counts updates and sits at 100% through configure |
| BF-18 | medium | progress | Predownload completions never reach the commit view (dead FR-041 seed) |
| BF-19 | medium | progress | Mixed install/remove plans show "Removing Packages"; xbps-install removal lines unparsed |
| BF-20 | low | progress | Download view over-counts retries; failed files stay in position |
| BF-21 | low | signals | SIGTERM during predownload exits 130, not 143 |
| BF-22 | medium | summary | Reinstall counts the full installed size as new disk use |
| BF-23 | medium | tui | Scrollbar thumb follows the selection, not the scroll offset |
| BF-24 | medium | tui | At 60×18, columns touch and version spans overwrite the Old Version column |
| BF-25 | low | tui | A custom `Regular` background leaves holes behind styled cells |
| BF-26 | low | tui | At 60 columns the help block drops `(y) start upgrade` |
| BF-27 | low | tui | The "Terminal too small" notice overwrites the borders below ~47 columns |
| BF-28 | low | tui | Horizontal wheel events scroll vertically |
| BF-29 | low | tui | Scroll offset is derived from the selection; `k` after `End` scrolls instead of moving |
| BF-30 | low | tui | The fetch picker's info band is one truncated row, not two |
| BF-31 | low | summary | Turning colour on changes how narrow headers wrap |
| BF-32 | low | history | History shows the raw value `ManualHold` instead of `Manual hold` |
| BF-33 | low | width | Several emoji blocks measure as width 1 |
| BF-34 | low | summary | `VersionDiff` is dead code with a stale defect note in the spec |
| BF-35 | medium | replay | `redo` after `undo` warns about every package |
| BF-36 | medium | replay | Undo uses the after-state repository and size for the restored version |
| BF-37 | low | hints | FR-073's `ignorepkg` hint is missing |
| BF-38 | low | download | `xbp download` leaves an orphan `.sig2` after an archive failure |
| BF-39 | low | history | History list header is `Date`; nala's is `Date and Time` |
| BF-40 | medium | discovery | `--description` includes the repository path in the text |
| BF-41 | medium | policy | `policy <virtual>` reports `Installed: none` for an installed provider |
| BF-42 | medium | discovery | `show` reports pkgver, pattern and virtual tokens as "not found" |
| BF-43 | medium | discovery | Every multi-variant name gets a `-R -S` call before token filtering (slow) |
| BF-44 | low | discovery | `:arch` split breaks `pc:` / `font:` names; `[!x]` is not negation |
| BF-45 | low | engine | Cursor not hidden inside the inline box; a double blank line between surfaces |
| BF-46 | low | engine | PTY open failure leaves the widget at 0% for the whole run |

| BF-47 | low | config | `./path` values are reported as "floats are not supported" |
| BF-48 | low | config | Raw control characters are accepted inside TOML strings |
| BF-49 | low | completion | Accepted no-op flags (`-A`, `--no-fix-broken`, …) are never completed |

---

## Phase 0: make the test suite trustworthy

### BF-01 (high): integration tests read the host's real history store

- **Files:**
  - `internal/cli/surface_test.go` (`surfaceRun`, around L150)
  - `internal/cli/transaction_implicit_test.go` (`implicitEnv`, around L486)
  - the shared env builders `newEnv`, `newFetchEnv`, `implicitEnv`, `discoveryEnv` and `emptyConfigDir`
- **Defect:** the child process inherits no `XBPRETTY_HISTORY_DIR`, so it uses
  `/var/lib/xbpretty/history`. On a machine where `xbp` has been run (this one
  has `1.json` from 2026-11-06 07:09), these fail:
  - `TestSurfaceClosedAliasDispatch`: `history undo` and `history redo` expect "history is empty"
  - `TestAC013ImplicitAutoremove/replay_verbs_never_run_the_orphan_transaction`, and the four `TestParity_P_*` tests that wrap it: `undo` queries the real installed packages
- **Repro:** `go test ./internal/cli/ -run 'TestSurfaceClosedAliasDispatch|TestAC013ImplicitAutoremove'`
  on any machine with a non-empty `/var/lib/xbpretty/history`.
- **Fix:**
  1. In every helper that builds a child environment for the built binary, set
     `XBPRETTY_HISTORY_DIR` to `t.TempDir()` unless the test sets it
     explicitly. Do the same for `XDG_STATE_HOME`, `XDG_CONFIG_HOME` and
     `HOME`; most helpers set some but not all of them.
  2. Add one guard helper, `hermeticEnv(t, env map[string]string) map[string]string`,
     in `internal/cli/file_helpers_test.go`. It fills the four variables above
     when they are missing, and every helper calls it last.
  3. The comment `#69's unimplemented seam` in
     `transaction_implicit_test.go:489` is stale (undo is implemented). The
     subtest's real intent is "undo never plans an orphan transaction".
     Rewrite it to seed one history entry in the temp store. Then assert that
     no recorded call contains `-o` and that the exit code is whatever undo
     returns for that entry.
- **Tests:** add `TestIntegrationEnvIsHermetic` in
  `internal/cli/file_helpers_test.go`. For each env helper, assert:
  - the returned map has `XBPRETTY_HISTORY_DIR`, `XDG_STATE_HOME`,
    `XDG_CONFIG_HOME` and `HOME`, all under the test's temp root;
  - running the built binary with `history` under that env prints
    `No history entries found.`.
- **Done when:** `go test ./...` passes on this machine without moving
  `/var/lib/xbpretty/history`.

---

## Phase 1: nala parity for the sync step (the reported problem)

### Background

nala v0.18.2 is the Rust rewrite (tag `v0.18.2`, commit `2b7ff05`). Your
screenshot in `screenshots-nala/` is the legacy **Python** nala 0.15.x. That is
why its footer reads `Upgrade 80 Packages`, while v0.18.2's (and xbpretty's) is
` Upgrade 80`. The footer is correct; do not change it.

In v0.18.2, `nala upgrade`, `full-upgrade` and `safe-upgrade` call the **whole**
`update()` routine before planning (`src/main.rs:92-94`, gated by
`auto_update` / `--update` / `--no-update` in
`src/config/configuration.rs:276-299`). So `upgrade` shows exactly what
`nala update` shows:

1. **The live 7-row `Updating Package Lists` widget** (`src/cmd/update.rs:81`,
   `src/tui/progress.rs:212-234`):
   - message ` Downloading: <url>`
   - info rows `Data: pos/len`, `Remaining:` (left) and `Speed:`, `Elapsed:` (right)
2. **Persistent per-item lines above the widget**
   (`update-item = { $state }: { $description }`, `locales/en-US/main.ftl:503`):

   | Line | Label role |
   |---|---|
   | `No Change: <desc>` | Primary |
   | `Updated: <desc> [<size>]` | Secondary |
   | `Ignored: <desc>` | Notice |
   | `Error: <desc>`, then the error text on its own line | Error |

   Sources: `update.rs:215-290`. `[<size>]` is appended only when the size is
   > 0, in Binary units with precision 1.
3. **A completion line in Highlight:** `Fetched <size> in <time> (<rate>/s)`,
   or `Nothing to fetch` (`update.rs:129`, `progress.rs:259-271`).
4. **The count line:** `N packages can be upgraded. Run '<cmd>' to see them.`,
   plural-aware, with the command in Primary (`update.rs:131-157`,
   `main.ftl:492-496`). It is printed during `upgrade` too.

xbpretty does none of this for `upgrade`. For `update` it draws a different
6-row widget and prints no per-repository lines. The spec encoded the
difference on false premises:

- **P-VIS-031** says xbps reports no fetched bytes. It does: each downloaded
  repodata prints `<arch>-repodata: 1854KB [avg rate: …]`, and the file size
  is on disk.
- **P-VIS-032** says xbps "always syncs". So does apt; the honest mapping is
  "nothing downloaded" → `Nothing to fetch`.

What xbps 0.60.x emits during `xbps-install -S`:

| Event | Output | Stream |
|---|---|---|
| Repository start | `[*] Updating repository \`<url>/<arch>-repodata' ...` | stdout |
| Transfer tick (only when downloading) | `[ 40%] x86_64-repodata: [1854KB 40%] 2391KB/s ETA: 00m01s` | stderr when stdout is a TTY, else stdout |
| Transfer done (only when downloading) | `x86_64-repodata: 1854KB [avg rate: 2391KB/s]` | as above |
| Unchanged repodata | nothing after the start line | |
| Failure | `ERROR: [reposync] failed to fetch file \`<url>': <reason>` | stderr |
| New key | `<url>' repository has been RSA signed by "<signer>"`, `Fingerprint: …`, `Do you want to import this public key? [Y/n]` | stdout, and reads stdin |

Sources: xbps `lib/fetch_cb.c` / `bin/xbps-install/fetch_cb.c:153-193`,
`lib/repo_sync.c:140-146`, `bin/xbps-install/state_cb.c:68-70,154-158`.

### BF-02 (high): the upgrade-family sync step never uses the update UI

- **Files:** `internal/cli/transaction.go` (`syncStep` L118-142, `runTransaction` L207-225, `runUpdate` L486-544)
- **Defect:** `runUpdate` wraps `syncStep()` in `beginUpdateLive()`.
  `runTransaction` (upgrade, full-upgrade, safe-upgrade, and any verb with
  `--update`) calls `syncStep()` bare, so it falls back to `streamStderr()`,
  which forwards xbps's raw text.
- **Repro:** `xbp upgrade --dry-run` as root on a TTY prints
  `[*] Updating repository …` lines; `xbp update` draws the widget.
- **Fix:** add one sync routine, used by every caller.
  1. Create `internal/cli/sync.go` with:
     ```go
     // runSync is the one repository-sync step (FR-013) for update, the
     // upgrade family and --update. It draws nala's update UI when a live
     // surface is allowed and forwards xbps output otherwise.
     func (st *state) runSync(caller string) (code int, out hints.Output, err error)
     ```
     It returns `-1` for "continue", matching `syncStep`'s contract.
  2. Move the body of `syncStep` into `runSync`. Gate the live view on
     `st.liveMode() != liveOff` only, never on the command name.
     `liveMode()` (`execute.go:20`) already excludes `-v`, `TERM=dumb`,
     non-TTY stderr and `Plain` mode, and it does **not** exclude
     `--dry-run`, which is correct here: nala shows the update UI on
     `upgrade --dry-run` too.
  3. Move `beginUpdateLive` / `endLive` and the failure-path dump
     (`fmt.Fprint(st.stderr, st.r.lastStdout)` / `lastStderr`, then the hint)
     from `runUpdate` into `runSync`, so both callers get identical failure
     behaviour.
  4. Replace the call in `runUpdate` (L497-522) with `runSync("update")`.
     Replace the one in `runTransaction` (L213) with `runSync(cmd)`. Delete
     `syncStep`.
  5. **Key-import prompt:** xbps may ask `Do you want to import this public key? [Y/n]`
     during `-S`, which needs a visible prompt and stdin. In `runSync`, when the
     parser sees `^Fingerprint: ` or the `has been RSA signed by` line:
     - call `live.Suspend()`, a new method that clears the box and stops drawing;
     - forward the following child lines raw until the next `[*] Updating repository` line or EOF;
     - then call `live.Resume()`.

     This is the analogue of nala's release-info suspend. Test it with a fake
     `xbps-install` that prints the prompt and reads a line.
- **Tests** (`internal/cli/transaction_sync_integration_test.go`):
  - `TestUpgradeSyncUsesUpdateWidget`: PTY run of `upgrade --dry-run` with a
    fake two-repository sync. Assert the transcript contains
    `Updating Package Lists` and does **not** contain `[*] Updating repository`.
  - `TestUpgradeSyncPlainWhenPiped`: stderr not a TTY. Assert the raw xbps
    lines are forwarded (the existing FR-013 behaviour stays for non-TTY).
  - `TestSyncFailureDumpsOutputAndHint`: the fake sync exits 1 with a stderr
    error. Assert the widget is cleared, then the captured output and the hint
    are printed, with the same exit code from `update` and `upgrade`.
  - `TestSyncKeyImportPromptIsVisible`: the fake prints the key-import block
    and reads stdin. Assert the prompt text reaches the terminal unboxed and
    `y\n` from the test reaches the child.

### BF-03 (high): the update UI lacks nala's lines, completion line and count line

- **Files:** `internal/cli/telemetry.go`, `internal/cli/progress.go`,
  `internal/cli/progress_view.go`, `internal/cli/sync.go` (new),
  `internal/cli/transaction.go` (`runUpdate` count line L533-539,
  `printUpgradable` L569-582)
- **Fix**, in order:
  1. **Parser events** (`telemetry.go`). Add callbacks on `StreamParser`:
     ```go
     OnRepoFetched func(file string, size int64) // "<arch>-repodata: <size> [avg rate: …]"
     OnRepoError   func(url, reason string)      // "ERROR: [reposync] failed to fetch file `<url>': <reason>"
     ```
     - `OnRepoFetched`: reuse `downloadCompleteRegex`, but call it when the
       file name ends in `-repodata` and `state.Phase == "RepoSync"`. Feed it
       from **both** `FeedStdout` and `FeedStderr`, because xbps writes it to
       stdout when stdout is not a TTY.
     - `OnRepoError`: add
       ``repoErrorRegex = regexp.MustCompile(`^ERROR: \[reposync\] failed to fetch file ['"\x60]([^'"\x60]+)['"\x60]: (.+)$`)``,
       matched in `FeedStderr`.
     - Parse the size with the existing `sizeRegex` and a helper
       `parseHumanSize(string) int64`, base 1024, units `B KB MB GB`. Check
       whether `telemetry.go` already has one before writing it.
     - Route repodata ticks (the TTY and piped tick regexes, file ending in
       `-repodata`) to a new `OnRepoTick(file string, pos, rate int64)`.
  2. **A sync tracker** (`sync.go`). This is a small state machine, unit-testable without a PTY:
     ```go
     type repoStatus int // repoNoChange, repoUpdated, repoError
     type syncTracker struct {
         current       string // URL from the last [*] Updating line
         currentStatus repoStatus
         currentSize   int64
         errText       string
         fetchedBytes  int64
         fetched       int
         started       time.Time
         emit          func(line string) // receives finished, rendered lines
     }
     func (t *syncTracker) start(url string)           // finalises the previous repository, then sets current
     func (t *syncTracker) fetched(size int64)         // marks current as Updated
     func (t *syncTracker) failed(url, reason string)  // marks current (or url) as Error
     func (t *syncTracker) finish()                    // finalises the last repository at EOF
     ```
     `finalise` renders one line, with label roles from `st.theme` and
     `st.colorErr`:
     - `No Change: <desc>` (Primary)
     - `Updated: <desc> [<size>]` (Secondary), with size in Binary precision 1
       (`summary.FormatSize` with precision 1, or the progress unit helper
       `progress_units.go`)
     - `Error: <desc>` (Error), followed by the reason on its own line

     `<desc>` is the URL from the start line with the trailing
     `/<arch>-repodata` replaced by ` <arch>-repodata`. This mirrors nala's
     `<base> <suite> <file>` shape, e.g.
     `https://repo-default.voidlinux.org/current x86_64-repodata`.
     `Ignored:` is N/A on xbps: there are no optional index files.
  3. **Wire it up in `runSync`:**
     - parser `OnRepoUpdate` calls `tracker.start(url)`, then `live.Apply(… m.repository(url) …)`;
     - `OnRepoFetched` calls `tracker.fetched(size)`;
     - `OnRepoError` calls `tracker.failed`;
     - after the child exits, call `tracker.finish()`.

     `tracker.emit` is `live.InsertAbove(line)` when live (see BF-05), else
     `fmt.Fprintln(st.stderr, line)`. In the non-live path, stop forwarding the
     raw `[*] Updating repository` lines and print the tracker lines instead,
     but only when stderr is a TTY. When stderr is not a TTY, keep raw
     forwarding so scripts that grep xbps output keep working (record this in
     the spec as an adaptation).
  4. **Completion line**, after `endLive` and only on success:
     - `fetched > 0`: `Fetched <size> in <time_str(elapsed)> (<rate>/s)`, in Highlight
     - otherwise: `Nothing to fetch`

     Prefer an exact byte total. Before the sync, `os.Stat` every
     `/var/db/xbps/*/<arch>-repodata` (glob, no URL escaping needed) and record
     size and mtime. After the sync, sum the sizes of files whose size or
     mtime changed. Fall back to `tracker.fetchedBytes` when the glob finds
     nothing (for example under `-r` / a rootdir passthrough). Use
     `repos.Arch` / `xbps-uhelper arch` for `<arch>` (see BF-12, and share
     that helper).
  5. **Update view to 7 rows** (`progress.go` `view()` `viewUpdate` case L401-410,
     `progress_view.go` `height()`):
     - `msgHeader: "Downloading"`, `msgText: <repodata URL>`
     - `left`: `Data: <pos>/<len>` and `Remaining: <time>`
     - `right`: `Speed: <rate>/s` and `Elapsed: <time>`
     - `pos`/`len`: sum of tick positions and of the sizes of transfers
       started so far (nala's total also grows as items start)
     - ratio: `finished / total` repositories when the total is known (BF-04), else bytes
     - heights: make `height()` return `heightDownload` for `viewUpdate` too
     - keep `Repositories n/N` in the **plain** 40-cell line only
       (`progress.go` L476-481), as a documented extension
     - change `repository()` (`progress.go:258`) to count a repository as done
       when the *next* one starts or at EOF, not when it starts. Today the
       first repository shows `1/N` immediately.
  6. **Count line**, for `update` and the upgrade family, after the completion line:
     - change `runUpdate`'s line (L537) to nala's plural-aware text:
       - 1 package: `1 package can be upgraded. Run 'xbp list --upgradable' to see it.`
       - N packages: `N packages can be upgraded. Run 'xbp list --upgradable' to see them.`

       Paint `xbp list --upgradable` Primary when `st.color`.
     - with `update_show_packages`, `printUpgradable` prints
       `<name> (<old>) -> (<new>)`: name Primary, parentheses Highlight,
       versions Secondary
     - for the upgrade family, print the count line on **stderr** after the
       sync, using the dry-run rows already planned in `runTransaction`
       (no second `xbps-install -n -u`). It is a status line there, and stdout
       stays the durable summary (decision D-2).
- **Spec and docs changes** (same PR):
  - `testdata/parity/matrix.tsv` and `testdata/parity/nala-v0.18.2.tsv`:
    - P-VIS-031: `absence` → `adaptation`, "Fetched line from stat'd repodata bytes"
    - P-VIS-032: → `equivalent`
    - P-VIS-017: header `Downloading`
    - P-VIS-016: Update info rows
    - P-BEH-024: sync uses the update UI
    - new row P-VIS-0xx "per-repository status lines" `adaptation` (no `Ignored:`)

    Then run `make parity` to regenerate spec Appendix A.2-A.4 and
    `docs/parity/matrix.md`.
  - `xbpretty-spec.md`: amend FR-012, FR-013, FR-064 (heights: Update 7),
    FR-066, FR-071 (update view text), AC-008, AC-009, AC-046, AC-068.
  - New ADR `docs/adr/0021-sync-step-follows-nala-update.md`. It supersedes
    #45 D10/D12 for the sync step and records decisions D-1 and D-2 below.
  - `docs/research/xbps-progress-telemetry.md`: add the repodata line formats above.
- **Tests:**
  - `internal/cli/sync_test.go`:
    - `TestSyncTrackerStates`: start A, start B, fetched(1854 KiB), start C, failed(C), finish. Lines are, in order, `No Change: A`, `Updated: B [1.8 MiB]`, `Error: C` plus the reason.
    - `TestSyncTrackerNothingFetched`: completion text is `Nothing to fetch`.
  - `internal/cli/telemetry_test.go`:
    - `TestRepoFetchedOnBothStreams`
    - `TestRepoErrorRegex`, using the 0.60.7 string from `testdata/hints/0607/stale-index.txt` if it matches, else a new fixture
  - `internal/cli/progress_view_test.go`: `TestUpdateViewSevenRows`, a golden for the new layout at 80 and 60 columns
  - `internal/cli/transaction_test.go`: `TestUpdateCountLinePlural` covering 1 and 3 packages
  - PTY: `TestUpgradeSyncPrintsRepositoryLinesAboveWidget`, which asserts the transcript order: status lines, widget, `Fetched …`, count line, then the summary

### BF-04 (medium): update view repository total is wrong

- **Files:** `internal/cli/progress.go:819-825` (`repositoryCount`)
- **Defect:** it calls `fetch.DiscoverRepositories`, which only collects lines
  from files matching `*-repository-*.conf`. On this machine it reports 4, but
  `xbps-install -S` syncs every remote repository in `/etc/xbps.d`
  (`danklinux.conf`, `dms.conf`, `20-*.conf`, …: about 12 remote).
  - With more repositories than counted, `repository()` grows the total, so
    the bar reaches 100% after the 4th repository and sits there.
  - With fewer, it clears early (observed: `Repositories: 2/4` at 50%).
- **Fix:**
  1. Replace the body with a query of `xbps-query -L`, which lists the
     repositories xbps itself will use. That respects `/etc` over
     `/usr/share` by basename, `rootdir`, and `-R` / `--repository`
     passthrough. Each line looks like ` <n> <url> (<sig>)`. Count lines whose
     URL contains `://` and is not `file://`, de-duplicated.
  2. Run it through `st.r.run(st.r.cfg.Xbps.QueryPath, "-L")` before the sync
     starts, and add `"query"` to the binaries `runSync` resolves.
  3. If the call fails, fall back to a corrected config scan. Add
     `fetch.AllRepositoryLines(dirs)`, which reads **every** `*.conf`, lets an
     `/etc/xbps.d` basename override the same name in `/usr/share/xbps.d`,
     drops local paths, and de-duplicates.
  4. Pass the `--` passthrough through to `-L` when it contains `-R`, `-r` or `-C`.
- **Tests:** `TestRepositoryCountFromQueryL`: a fake `xbps-query -L` with 5
  remote and 2 local entries gives 5. `TestRepositoryCountFallbackReadsAllConf`:
  temp dirs with `20-foo.conf` and `00-repository-main.conf` give 2.

### BF-05 (medium): the in-place renderer discards inserted lines

- **Files:** `internal/cli/progress.go:763-776` (`flushInsertsLocked`), `:696-736` (`drawLocked`)
- **Defect:** `update` and fetch's MirrorScore view use the in-place renderer
  (no `viewport`). `flushInsertsLocked` consumes `model.inserts` and returns
  without printing them when `l.viewport == nil`. Any line BF-03 inserts above
  the box would vanish.
- **Fix:** add a non-viewport path. In `flushInsertsLocked`, when
  `l.viewport == nil && l.mode == liveWidgetMode && l.drawn > 0`, build one
  write:
  1. `\r`
  2. `\x1b[<drawn-1>A` (when `drawn > 1`)
  3. `\x1b[J`
  4. each line plus `\r\n`

  Then set `l.drawn = 0` and return `true`, so the caller redraws the box below
  the inserted lines.
  - When `drawn == 0`, write the lines directly.
  - In `livePlainMode`, write `\r\x1b[2K` + line + `\n`, then redraw the plain line.

  Add a public `(*liveProgress).InsertAbove(line string)`: lock, append to
  `model.inserts`, then `flushInsertsLocked` and `requestLocked(true)`. BF-16
  reuses it.
- **Tests:** `TestInPlaceInsertAboveRedrawsBox`: a `bytes.Buffer` sink. Draw,
  insert two lines, and assert the byte sequence: cursor-up, clear-to-end, the
  two lines, then the box again with the same height.

### Decisions to confirm before Phase 1 (defaults chosen)

- **D-1: stream.** nala prints the enhanced widget and its lines on
  **stdout**; xbpretty's ADR-0005 keeps everything but durable output on
  stderr. **Default: keep stderr** for the widget, status lines and `Fetched`.
  This preserves `xbp upgrade --dry-run | grep` and is the existing contract.
  Record it in A.4 as a deliberate deviation.
- **D-2: count line during upgrade.** nala prints it on stdout during
  `upgrade`. **Default: print it on stderr during the upgrade family**, so
  stdout stays the summary only. `update` keeps it on stdout (its durable
  output).

---

## Phase 2: hangs, data loss and unintended root actions

### BF-06 (critical): a child line over 64 KiB deadlocks the transaction (PTY mode)

- **Files:** `internal/cli/run.go:207-225` (stdout scanner on the PTY master), `:238-250` (stderr parser scanner over `io.Pipe`)
- **Defect:** both use `bufio.Scanner` with the default 64 KiB token limit. On
  `ErrTooLong`, `Scan()` returns false and the goroutine stops reading.
  - **stdout:** the PTY buffer fills and the child blocks on `write(1)`.
  - **stderr:** the parser goroutine exits, `io.Copy(mw, stderrPipe)` blocks
    on `pw.Write` with no reader, and the child blocks on `write(2)`.

  Either way xbpretty hangs forever while xbps holds the pkgdb lock mid-unpack.
- **Repro:** a fake real-run script prints `head -c 70000 /dev/zero | tr '\0' x; echo; …`.
  `timeout 15 script -qec "xbp install -y --no-auto-remove curl"` exits 124;
  piped (direct path) it exits 0. The stderr variant also exits 124.
- **Fix:**
  1. In `run.go`, add:
     ```go
     // boundedSplit never lets a token exceed max: an over-long line is
     // returned in max-sized pieces, so the scanner cannot fail with
     // ErrTooLong and stop draining the child.
     func boundedSplit(split bufio.SplitFunc, max int) bufio.SplitFunc {
         return func(data []byte, atEOF bool) (int, []byte, error) {
             adv, tok, err := split(data, atEOF)
             if adv == 0 && tok == nil && err == nil && len(data) >= max {
                 return max, data[:max], nil
             }
             return adv, tok, err
         }
     }
     ```
  2. stdout: `scanner.Buffer(make([]byte, 0, 64<<10), 64<<10)` and `scanner.Split(boundedSplit(bufio.ScanLines, 32<<10))`.
  3. stderr: `s.Split(boundedSplit(scanLinesOrCR, 32<<10))`, with the same `Buffer`.
  4. Belt and braces: after each scan loop, if `scanner.Err() != nil`, drain
     the source to EOF: `io.Copy(&rawStdout, master)` for stdout,
     `io.Copy(io.Discard, pr)` in the parser goroutine.
- **Tests:** `internal/cli/run_test.go` `TestStreamHybridLongLinesDoNotDeadlock`.
  Table over stdout and stderr; `runner{live: true}`; the child is
  `sh -c 'head -c 70000 /dev/zero | tr "\0" x; echo; echo done'` (stderr
  variant: `>&2`). Fail after 10 s. Assert exit 0 and that the captured stream
  ends with `done\n`.

### BF-07 (critical): a closed stderr pipe kills xbps mid-transaction

- **Files:**
  - `internal/cli/main.go:133` (`stderr: err`; only stdout is wrapped in `newResilientWriter`) and `:208-215` (`runner.out` / `runner.stderr`)
  - `internal/cli/run.go:201, 287-291, 324` (the display writers)
- **Defect:** child output is forwarded to raw stderr. When stderr is a closed
  pipe, the `os/exec` copy goroutine errors and closes its read end, and
  xbps's next write raises SIGPIPE. That leaves a half-applied transaction and
  exit 141. FR-066/FR-074 say an EPIPE stops writing but never aborts the
  transaction.
- **Repro:** `sh -c 'xbp install -y --no-auto-remove curl; echo rc=$?' 2>&1 | head -2`
  prints `rc=141`, and the fake child never reaches its last line.
- **Fix:**
  1. In `Run` (`main.go`), create `errW := newResilientWriter(err)` once and
     use it for `st.stderr`, `runner.out` and `runner.stderr`. `asFile`
     (`transaction.go:1075-1080`) already unwraps `*resilientWriter`, so
     `isTTY`, `terminalWidth` and `startInline` keep working; check every
     `.(*os.File)` type assertion on stderr in `internal/cli` (grep `stderrWriter().(*os.File)`, `run.go:179`) and route it through `asFile`.
  2. Check `resilientWriter` (`epipe_unix.go` / `main.go`) is safe for
     concurrent use. If not, keep the `serializedWriter` wrapping, as all three
     stream functions already do.
- **Tests:** `internal/cli/integration_test.go` `TestBrokenStderrPipeDoesNotKillChild`.
  Spawn the test-root binary with stderr an `os.Pipe` writer whose reader is
  closed. The stub real run prints 200 lines and then touches a marker file.
  Assert exit 0 and that the marker exists.

### BF-08 (critical): a split paste leaks keys into the full-screen summary

- **Files:** `internal/term/input.go:368-370, 379-398` (`discardPaste`)
- **Defect:** `discardPaste` stops at the first 100 ms gap or after 2 s. Every
  remaining pasted byte, including `ESC[201~`, is then decoded as keys. A
  paste delivered in two chunks over ssh or tmux can therefore deliver `y` and
  start a root transaction from S1. The function's own comment says this must
  not happen.
- **Repro:** decoder over `os.Pipe`: write `\x1b[200~line one\n`, sleep 150 ms,
  write `y\x1b[201~`. The keys are `paste-discarded, rune:y, unknown`.
- **Fix:** make paste mode stateful instead of time-bounded.
  1. Add `inPaste bool` to `Decoder`.
  2. On `ESC[200~`, set `inPaste = true` and return `KeyPaste`.
  3. At the top of the decode loop, while `inPaste`, read bytes with
     `d.nextByte(-1)`, which blocks but honours `d.cancel`. Slide the 6-byte
     window, and clear `inPaste` on `\x1b[201~`.
  4. Delete the 2 s deadline.
  5. Cancel/EOF still returns, so a terminal that never sends `201~` swallows
     input until close or a signal. That is safe: `Ctrl-C` is delivered as
     SIGINT because ISIG stays on (FR-072).
- **Tests:** `internal/term/input_linux_test.go`
  `TestPasteWithGapStaysDiscarded`: write `\x1b[200~abc`, sleep 250 ms, write
  `y\x1b[201~q`. The keys are exactly `[KeyPaste, rune:q]`.
  `TestPasteCancelReturns`: an open paste, then `Close()` returns within 100 ms.

### BF-09 (high): the log writer follows invoker-controlled symlinks as root

- **Files:** `internal/cli/execute.go:220-242` (`writeLog`), `internal/config/config.go` (`StateDir`, `TransactionLog`)
- **Defect:** under `sudo` / `doas`, root opens
  `~invoker/.local/state/xbpretty/transactions.log` with
  `O_APPEND|O_CREATE` and then calls `os.Chown` on the directory and the file.
  Both follow symlinks, and the invoking user controls both paths. A user
  allowed only `doas xbp` can therefore make root append a line to, and chown,
  any file or directory. Reproduced against a scratch "victim" file.
- **Fix:**
  1. Open with `os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW`.
  2. Chown through the descriptor: `f.Chown(uid, gid)`.
  3. Before opening, `os.Lstat` the state directory and each component from
     the invoker's home down. Refuse, with the existing
     `transaction log unavailable: …` warning, if any component is a symlink
     or not owned by root or the invoker. Chown the directory with
     `os.Lchown` only after that check.
  4. Put the component walk in a helper, `config.SafeInvokerDir(path string) error`,
     shared with BF-10.
- **Tests:** `internal/cli/log_test.go` `TestWriteLogRefusesSymlinkUnderInvoker`
  (test-root EUID, `SUDO_USER`, the sudo-home hook, `transactions.log` as a
  symlink to a temp file). Assert the target is unchanged and stderr contains
  `transaction log unavailable`. Add the same with the `xbpretty` directory
  as a symlink.

### BF-10 (medium): missing state ancestors are created root-owned under sudo

- **Files:** `internal/config/config.go` `StateDir` (`os.MkdirAll(dir, 0o755)` as root)
- **Defect:** on a fresh account, the first `sudo xbp install` creates
  `~/.local` and `~/.local/state` owned by root. `writeLog` then chowns only
  the last directory, so other programs cannot create their own state there.
  *(trace)*
- **Fix:** when `invokingUser()` is set, walk from the invoker's home and
  create each missing component with `os.Mkdir(p, 0o755)` followed by
  `os.Lchown(p, uid, gid)`, refusing symlinks (via `SafeInvokerDir` from
  BF-09). Do the same for the history-store parent if any per-user path
  exists there.
- **Tests:** `internal/config/config_test.go` `TestStateDirCreatesInvokerOwnedAncestors`.
  Inject the chown through a package-level `lchown` variable and assert it is
  called for every created component with the invoker's uid and gid.

### BF-11 (high): a second `fetch` breaks `clean --fetch`

- **Files:** `internal/fetch/write.go:394-412` (`ApplyWrites`)
- **Defect:** on a re-fetch, `case prev.Created && exists` snapshots the file
  the *first* fetch created into `<file>.xbpretty-backup` and records
  `created:false`. `clean --fetch` then restores fetch-written content instead
  of deleting the file (FR-047/FR-048).
- **Repro:** two runs of `fetch --auto 1` with an `/usr/share`-only config,
  then `clean --fetch`. `etc/00-repository-main.conf` still contains
  `# Written by xbpretty fetch`.
- **Fix:** collapse the two `prev.Created` cases into
  `case prev.Created: t.created = true`. The pre-fetch state was "absent"
  whatever the file holds now; a later hand edit is already covered by
  clean's "modified after fetch; removing it" warning.
- **Tests:** `internal/fetch/write_test.go` `TestApplyWritesRefetchKeepsCreated`:
  apply twice, feeding manifest 1 into run 2. The second entry has
  `Created==true`, `Backup==""`, and no `.xbpretty-backup` file exists.
  `internal/cli/clean_fetch_test.go` `TestCleanFetchAfterTwoFetchesRemovesFile`.

### BF-12 (high): `fetch` uses the wrong architecture on musl

- **Files:** `internal/cli/fetch.go:29, 92-95, 467-490` (`runtimeXBPSArch`)
- **Defect:** with no `architecture=` line, the arch falls back to
  `runtime.GOARCH` mapped to `x86_64` / `aarch64` / …, never `*-musl`. A stock
  musl system's repository `…/current/musl` serves `x86_64-musl-repodata`, so
  every probe 404s and `fetch` exits 1 with `no mirrors responded`.
  `XBPS_ARCH` is ignored too. Five of the nine release builds are musl.
  *(trace)*
- **Fix:** add `func (st *state) xbpsArch(cfg fetch.RepositoryConfig) string`. It resolves in this order:
  1. `cfg.Arch`
  2. `$XBPS_ARCH`
  3. `xbps-uhelper arch`, via `st.r.run(st.r.cfg.Xbps.UhelperPath, "arch")` with `ResolveFor("uhelper")`, trimmed
  4. `runtimeXBPSArch()`

  Keep the `fetchArch` test hook as the top-priority override. Reuse the
  helper in BF-03 (repodata glob).
- **Tests:** `internal/cli/fetch_test.go` `TestFetchArchFromUhelper`: a fake
  `xbps-uhelper` prints `x86_64-musl`. Assert the probe server receives
  `GET /current/musl/x86_64-musl-repodata`. `TestFetchArchFromEnv`:
  `XBPS_ARCH=aarch64-musl`.

### BF-13 (medium): a reverted fetch leaves a backup that a later fetch trusts

- **Files:** `internal/cli/fetch.go:390-419` (`revertFetchWrites`), `internal/fetch/write.go:410-412, 422`
- **Defect:** when the post-write sync fails, `revertFetchWrites` restores the
  files and the manifest but leaves the `.xbpretty-backup` files this run
  created. The next fetch (no manifest, so the `exists` branch) reuses that
  stale backup because `fileExists(t.backup)` skips refreshing it. A later
  `clean --fetch` then restores outdated content and discards the
  administrator's edits.
- **Fix:**
  1. Have `ApplyWrites` return the `createdBackups` slice it already builds
     (L419), next to the pre-images. `revertFetchWrites` removes those paths
     after a successful restore.
  2. In `ApplyWrites`, when there is no `previousByPath` entry and the target
     exists, always rewrite the backup from the current content. Do not trust
     an existing one.
- **Tests:** `internal/cli/fetch_test.go` `TestFetchSyncRevertRemovesNewBackup`
  (no `*.xbpretty-backup` remains). `TestFetchIgnoresOrphanBackup`: a
  pre-seeded stale backup is overwritten.

### BF-14 (high): boolean long flags accept and ignore `=value`

- **Files:** `internal/cli/args.go:168-196` (`parseLongFlag`)
- **Defect:** `--assume-yes=false` sets assume-yes; `--dry-run=no` turns on a
  dry run; `history clear --all=0` clears everything. Reproduced:
  `undo 1 --assume-yes=false </dev/null` ran `xbps-install -y …` with no
  prompt.
- **Fix:** after `LookupFlag`, add:
  ```go
  if hasValue && f.Kind == spec.BoolFlag {
      return occurrence{}, i, fmt.Errorf("--%s does not take a value", name)
  }
  ```
  The caller already maps parse errors to exit 2 through `printUsageError`.
- **Tests:** `internal/cli/cli_test.go` `TestBoolLongFlagRejectsValue`, a table
  over `--yes=false`, `--dry-run=1`, `--all=0` and `--no-color=` (empty).
  Assert exit 2, the message, and no child spawned. Update the completion
  generators only if they advertise `--flag=` for booleans.

### BF-15 (high): discovery drops installed state for held or locally newer packages

- **Files:** `internal/cli/discovery.go:594-597` (`defaultRow`)
- **Defect:** installed state attaches only when the chosen repository
  variant's pkgver equals the installed one. A package held at 1.0_1 while the
  repository has 2.0_1 shows `list -i` → `heldpkg (2.0_1) […] []`, and `-m`
  gives `"installed":false,"held":false`.
- **Fix:** when the name is installed and the pkgvers differ, return
  `s.installedRow(name, info)`, the `[local]` row with the installed and held
  attributes, and carry the repository candidate as the upgradable target.
- **Tests:** `TestAC019HeldOlderThanRepositoryKeepsInstalledState`, for text
  and `-m`.

---

## Phase 3: live progress accuracy and missing output

### BF-16 (high): successful live runs drop package messages and child warnings

- **Files:** `internal/cli/run.go:217-236`, `execute.go:113-115`, `progress.go:246-251`, `telemetry.go` (`FeedStdout` / `FeedStderr`)
- **Defect:** in live mode, child lines are only captured. Only
  `… installed/updated/configured/removed successfully.` lines are inserted
  above the box, and on success the captures are discarded. So these never
  reach the user:
  - xbps `[*] <pkgver> install message:` blocks (for example a kernel's "run update-grub")
  - configure-script output
  - stderr `WARNING:` lines

  nala shows every dpkg/maintainer-script line above its bar.
- **Fix:**
  1. Change `FeedStdout` / `FeedStderr` to return `consumed bool`: true for
     ticks, completions, phase headers, package events, the summary line and
     repository-sync lines.
  2. In `streamHybridWithSignals`, when `r.live` and a line was not consumed
     and is not blank after `cleanLine`, call
     `r.liveProgress.InsertAbove(cleanLine(line))` (from BF-05).
  3. Keep the stderr `WARNING:` lines in the Warning role (prefix only) when
     `colorErr` is set; forward everything else verbatim (FR-061 forbids
     repainting child output).
  4. In `streamDirectWithSignals` with `r.live` (the plain live line), do the
     same through `InsertAbove`.
- **Tests:** `internal/cli/inline_pty_linux_test.go`
  `TestLiveRunInsertsUnparsedChildLines`: the stub run prints
  `[*] curl-8.22.0_1 install message:`, two message lines and one stderr
  `WARNING: x`, then exits 0. Assert all of them appear in the transcript
  above the final position and the box is torn down. `TestTelemetryConsumed`,
  a table of line → consumed.

### BF-17 (medium): Install view double-counts updates

- **Files:** `internal/cli/progress.go:226-240` (`packageEvent`)
- **Defect:** xbps prints `<old-pkgver>: updating to <new-ver> ...` and then
  `<new-pkgver>: unpacking ...`. Both add a distinct key to `unpacked`, so with
  the `2·A` two-unit denominator the bar reaches 100% when unpacking ends and
  sits there through configure (dracut, dkms). The status also shows the
  **old** pkgver.
- **Fix:**
  1. Key `unpacked`, `configured` and `removed` by name:
     `name, _ := xbps.SplitPkgver(event.Package)`.
  2. For `updating`, set `currentPkg = name + "-" + event.Details` (the new
     version; confirm `ParsePackageEvent` puts the target version in
     `Details`, otherwise add it from `updateRunRegex` group 2).
- **Tests:** `internal/cli/progress_test.go` `TestInstallRatioUpdateCountsOnce`:
  one update row. After `updating`+`unpacking` the ratio is 0.5 and the status
  is `Unpacking curl-8.22.0_1`; after `configuring` the ratio is 1.0.

### BF-18 (medium): predownload completions never reach the commit view

- **Files:** `internal/cli/execute.go:47-66`, `telemetry.go:481-493` (`SeedDownloaded`), `predownload.go` (`cacheSeedSink`)
- **Defect:** `downloadSink.Seed(st.r.parser)` writes into parser state that
  nothing reads, then `execute` builds a fresh `progressModel`. After a
  successful predownload, the commit view restarts at `Packages 0/N`,
  `Data 0.0 B/total` until `[*] Verifying`. After a partial predownload,
  xbps's fallback transfers count from 0 (breaks FR-041).
- **Fix:**
  1. Add `Completed() map[string]int64` to `downloadSink`; `cacheSeedSink`
     returns a copy under its mutex.
  2. Add `(*progressModel).markCached(filename string)`. It resolves via
     `archiveBytes`, sets `completed[key]`, adds to `completedBytes` /
     `completedCount`, sets `lastCompleted`, and does **not** append an insert.
  3. In `execute`'s live branch, call `markCached` for each completed file
     right after `newProgressModel`. If
     `completedCount == totalArchives`, start in `viewInstall` (`viewRemove`
     for remove-only).
  4. Delete the now-unused `Seed` / `SeedDownloaded` / `State()` plumbing, and
     set `st.r.parser = nil` at the end of `execute` and in `endLive` so no
     phase inherits stale parser totals.
- **Tests:** `internal/cli/progress_test.go` `TestModelSeededFromPredownload`:
  two archives with one marked cached gives `Packages 1/2` and the matching
  position; all cached gives `kind == viewInstall`.

### BF-19 (medium): mixed plans show "Removing Packages"; xbps-install removals unparsed

- **Files:** `internal/cli/progress.go:216-221, 338-340`, `telemetry.go:37-38`
- **Defect:** an `xbps-install -u` plan with a replaced package (a `remove`
  row plus updates) switches to the Remove view at 0% from Verifying through
  Collecting. xbps-install's own `<pkgver>: removing ...` /
  `<pkgver>: removed successfully.` lines match no regex, so the removal is
  neither counted nor shown.
- **Fix:**
  1. Add
     ``installRemoveRunRegex = `^([\w.+~\-]+):\s+removing \.\.\.$` `` and
     ``installRemoveOkRegex = `^([\w.+~\-]+):\s+removed successfully\.$` ``,
     mapped to `removing` / `removed` in `ParsePackageEvent`.
  2. In `phase()` and the verifying branch of `view()`, choose `viewRemove`
     only when `installTotal+configureTotal == 0`.
  3. In `packageEvent`, do not switch to `viewRemove` when `installTotal > 0`.
     Count the removal in an Install denominator of `2A + C + R`.
- **Tests:** `TestMixedPlanStaysInstallView`; `telemetry_test.go` cases for the
  two new line forms.

### BF-20 (low): Download view over-counts retries; failed files stay in position

- **Files:** `internal/cli/predownload.go:281-306` *(trace)*
- **Fix:**
  - On `EventFileStart`, `EventRetry` and `EventFileFailed`, set `v.written[ev.File] = 0`.
  - On `EventFileFailed`, also apply `m.clearInFlight(ev.File)` (delete the
    `archiveBytes` key) before `Request(true)`.
- **Tests:** `TestDownloadEventViewRetryResets`: tick 80%, retry, tick 10% gives
  in-flight 10%; after `FileFailed`, position 0.

### BF-21 (low): SIGTERM during predownload exits 130

- **Files:** `internal/cli/predownload.go:92-93, 197` *(trace)*
- **Fix:** replace `signal.NotifyContext` with an explicit
  `chan os.Signal` plus `context.WithCancel`, record the signal in
  `predownloadOutcome.signal`, and return `128+int(sig.(syscall.Signal))`
  (FR-074).
- **Tests:** `TestPredownloadSIGTERMExits143`, using the engine seam to block,
  then `syscall.Kill(os.Getpid(), syscall.SIGTERM)`.

---

## Phase 4: rendering

### BF-22 (medium): reinstall counts the full installed size as new disk use

- **Files:** `internal/summary/summary.go:100-102`
- **Defect:** `xbp install --reinstall figlet` (10 MB) prints
  `Disk space required: 10 MiB`; FR-062's formula nets to about 0. The same
  value reaches S1 through `NetDiskBytes`.
- **Fix:** in `case actionReinstall`, add
  `if old, ok := s.Old[row.Name]; ok && old.HasSize { s.OldBytes += old.InstalledSize }`.
  Update the `OldBytes` field comment.
- **Tests:** `internal/summary/summary_test.go` `TestReInstallDiskDeltaNetsOldSize`:
  `NetDiskBytes()==0` and ` Disk space required: 0 B`.

### BF-23 (medium): the scrollbar thumb follows the selection

- **Files:** `internal/tui/summary.go:461`
- **Defect:** the thumb is positioned from `m.Sel*maxPos/(len-visible)`.
  `Sel` runs to `len-1`, so the thumb moves before the view scrolls and gets
  clipped from 6 cells down to 1 near the end. The golden
  `testdata/tui/scrollable-80x24.txt` encodes the bug (3 cells at Sel 24).
- **Fix:** use the computed `scroll` (L412-415):
  `thumbY = tableY + 1 + scroll*maxPos/(len(tab.Rows)-visible)`. Regenerate
  `scrollable-80x24.{txt,sgr}` with the repo's golden update path.
- **Tests:** `TestScrollbarThumbTracksScrollOffset`: for 30 rows at 80×24 and
  every Sel, the thumb is 6 cells; for Sel ≤ 12 it starts at the first
  interior row; at Sel 29 it ends on the last interior row.

### BF-24 (medium): at 60×18, columns touch and spans overwrite the neighbouring column

- **Files:** `internal/tui/summary.go:538-557` (`columnLayout` squeeze), `598-610` (`drawSpansAligned`)
- **Defect:**
  - Squeezed columns are placed edge to edge, which shows in the goldens as
    `Old Version:New Version:Size:` and `ManualHold2 MiB`.
  - `drawSpansAligned` centres with `x+(w-total)/2`, which goes negative when
    the spans are wider than the column, so the new version overwrites the old
    one on non-selected rows.
- **Fix:**
  1. In `drawSpansAligned`, when `total > w`, set `start = x` and clip each
     span at `x+w`.
  2. In the squeeze path, reserve `n-1` one-cell gaps. Shrink column 0
     (Package) first, down to 8 cells, then fall back to right-edge clipping:
     `offsets[i] = pos; pos += widths[i] + 1`.
  3. Regenerate the 60×18 goldens (`summary-60x18`, `history-allgroups-60x18`, `fetch-60x18` if affected).
- **Tests:** `TestSqueezedSpansStayInColumn` and `TestSqueezedColumnsKeepGap` (`columnLayout([]int{25,17,17,7},49)`).

### BF-25 (low): a custom Regular background has holes

- **Files:** `internal/tui/theme.go:52-68` and direct `t.Primary` / `t.Regular` uses in `summary.go`, `fetch.go`, `package_info.go`
- **Fix:** in `Theme.style`, after picking the role,
  `if s.BG == term.Default { s.BG = t.Regular.BG }`. Route `border.fill`,
  `table.fill` and `gutter.fill` through `t.style(...)`.
- **Tests:** `TestRegularBackgroundCoversFrame`: with `Regular.BG = ANSI(4)`,
  every interior cell of `RenderSummary` and `RenderFetch` at 80×24 has that
  background.

### BF-26 (low): help block drops `(y) start upgrade` at 60 columns

- **Files:** `internal/tui/summary.go:503-505`
- **Fix:**
  1. Compute the wrapped help lines before `tableH`.
  2. Set `footerRows = max(footerRows, len(helpLines))`, capped so `tableH ≥ 4`.
  3. Clip help only when that cap applies.

  Regenerate `summary-60x18`.
- **Tests:** `TestHelpKeepsConfirmHintAtFloor`.

### BF-27 (low): the too-small notice overwrites the borders

- **Files:** `internal/tui/summary.go:668-670` (`centerText`), used by `RenderSummary`, `RenderPackageInfo` and `RenderFetch`
- **Fix:** `wrapText(notice, W-2)`, draw each line with
  `DrawTextCenter(1, y, W-2, …)`, centred vertically.
- **Tests:** `TestTooSmallKeepsBorder` for w ∈ {30, 40, 46}, h = 10.

### BF-28 (low): horizontal wheel scrolls vertically

- **Files:** `internal/term/input.go:347-352`
- **Fix:** `switch btn &^ (4|8|16|32) { case 64: wheel-up; case 65: wheel-down; default: KeyUnknown }`.
- **Tests:** `TestSGRWheelButtons`: 66 and 67 are unknown; 68 is wheel-up.

### BF-29 (low): scroll offset is derived from the selection (S1/S2/S3)

- **Files:** `internal/tui/summary.go:412-415`, `internal/tui/fetch.go:195-199`, `internal/tui` summary and fetch models
- **Defect:** after `End`, `k` scrolls the list rather than moving the
  cursor, and `PgUp` jumps the view. nala's ratatui `TableState` keeps an
  offset and only scrolls when the selection leaves the view.
- **Fix:**
  1. Add `Offset int` to `SummaryModel`, `FetchModel` and the tab state.
  2. In key handling: `if Sel < Offset { Offset = Sel }` and
     `if Sel >= Offset+visible { Offset = Sel-visible+1 }`.
  3. Render from `Offset`, and use `Offset` in BF-23's thumb formula.
- **Tests:** `TestScrollKeepsOffsetWhenMovingUp`: 30 rows, `End`, then `k`, gives offset unchanged and Sel 28.

### BF-30 (low): fetch picker info band is one truncated row

- **Files:** `internal/tui/fetch.go:231-235`
- **Fix:** `wrapText(info, w-2)` into the 2-row band (FR-070); truncate the second row with `…` only if needed.
- **Tests:** a golden update for `fetch-60x18`, plus an assertion that the full first sentence is present.

### BF-31 (low): colour changes how narrow headers wrap

- **Files:** `internal/summary/table.go:44-47`, `wrapANSICell` L241-288
- **Fix:** wrap the plain header with `wrapCell`, then paint each line with `r.paint(RoleHighlight, line)`.
- **Tests:** `TestNarrowWrapIndependentOfColour` at widths 50 and 60: `stripANSI(colour) == plain`.

### BF-32 (low): history shows `ManualHold`

- **Files:** `internal/cli/history_tui.go:67`, `internal/cli/history.go:475`
- **Fix:** add `heldReasonLabel(r string) string`, mapping `ManualHold` → `Manual hold` and passing anything else through. Use it in both places, then regenerate `history-allgroups-{80x24,60x18}`.
- **Tests:** `TestHistoryHeldReasonDisplay`.

### BF-33 (low): emoji width table gaps

- **Files:** `internal/term/width.go:106-108`
- **Fix:** insert these ranges in sorted order: `{0x1f680,0x1f6c5},{0x1f6cc,0x1f6cc},{0x1f6d0,0x1f6d2},{0x1f6d5,0x1f6d7},{0x1f6dc,0x1f6df},{0x1f6eb,0x1f6ec},{0x1f6f4,0x1f6fc},{0x1f7e0,0x1f7eb},{0x1f7f0,0x1f7f0}`.
- **Tests:** `RuneWidth` is 2 for 🚀, 🛑 and 🟢.

### BF-34 (low): `VersionDiff` is dead code

- **Files:** `internal/summary/format.go:199-212`, `xbpretty-spec.md` (FR-063 and the reconciliation note at ~L428)
- **Fix:** delete `VersionDiff` and its tests; both renderers use
  `VersionParts`. Remove the "implementation defect" sentence from the spec.

---

## Phase 5: commands

### BF-35 (medium): `redo` after `undo` warns about every package

- **Files:** `internal/cli/replay.go:436-445`
- **Fix:** `expected := transition.After; if cmd == "redo" { expected = transition.Before }`.
  Use `stateVersion(expected)` in the comparison and in the message.
- **Tests:** `TestRedoAfterUndoPrintsNoDriftWarning`.

### BF-36 (medium): undo uses the after-state repository and size

- **Files:** `internal/cli/replay.go:90-141, 473-482` (`repositoryServes`) *(trace)*
- **Defect:** schema-1 entries record one `repository` / `size`, both for the
  after version. Undo shows the new archive's size in the summary and caps the
  predownload body (`bodyLimit`) at it, so a larger old archive fails
  predownload and falls back to xbps.
- **Fix:**
  - In `repositoryServes`, query `-p pkgver,repository,filename-size` and
    store the resolved repository and size on `replayTarget`.
  - For cache hits, use the archive's `Stat().Size()`.
  - When the size is unknown, use `0`, so `bodyLimit` is not enforced.
- **Tests:** `TestUndoRestoreUsesResolvedRepositoryAndSize`.

### BF-37 (low): `ignorepkg` hint missing (FR-073)

- **Files:** `internal/hints/hints.go:52-92`, `testdata/hints/0607/`
- **Fix:**
  1. Capture a 0.60.7 fixture for installing a package listed in
     `ignorepkg=` with `scripts/capture-fixtures` (exit code, stream and
     text), plus a `.meta`.
  2. Add a `Table` entry `ignorepkg` with text:
     `the package is listed in an ignorepkg= line in /etc/xbps.d; remove that line to install it.`
- **Tests:** the fixture-driven `TestLookupIgnorepkg`.

### BF-38 (low): orphan `.sig2` after an archive failure in `xbp download`

- **Files:** `internal/download/engine.go:332-345` (`fetchOne`)
- **Fix:** in `ModeDownload`, keep the signature as `.sig2.part` and publish
  it only after the archive verifies. Remove it on failure. This splits
  `attempt` into download-to-part and `publish`. Keep the signature-first
  order (FR-038).
- **Tests:** `TestDownloadModeNoSignatureWhenArchiveFails`.

### BF-39 (low): history list header

- **Files:** `internal/cli/history.go:366`, `history_integration_test.go:191`, `history_tui_pty_linux_test.go:511`
- **Fix:** `Date` → `Date and Time` (nala's header).

### BF-40 (medium): `--description` includes the repository path

- **Files:** `internal/cli/discovery.go:151-155, 370-376`
- **Fix:** add a `repositoryMode bool` parameter to
  `parseDescriptionProperty` and pass `true` for the repository pass. The
  existing `TestAC019SummaryDescriptionCompositionAndOrGlobs` asserts the
  wrong output; correct it.

### BF-41 (medium): `policy <virtual>` shows `Installed: none`

- **Files:** `internal/cli/policy.go:301-336`
- **Fix:** after canonicalising, re-run `st.record("-S", name)` when the name differs from the token.
- **Tests:** `TestAC021PolicyVirtualTokenUsesCanonicalInstalledRecord` (`awk` → `gawk`).

### BF-42 (medium): `show` rejects pkgver, pattern and virtual tokens

- **Files:** `internal/cli/discovery.go:953-957`
- **Fix:** always try `-R -S` before marking a token missing, then canonicalise with `policyRecordName`.
- **Tests:** `TestAC020ShowResolvesPkgverAndVirtualTokens`.

### BF-43 (medium): `-R -S` for every multi-variant name before filtering

- **Files:** `internal/cli/discovery.go:462-470, 494-513`
- **Fix:** apply the token filter before `namesNeedingResolution`. Each call
  costs about 0.22 s, so `list <name>` with a local repository next to the
  official one currently takes tens of seconds (NFR-002).
- **Tests:** `TestAC018ResolutionLimitedToMatchedNames`: count `-R -S` calls in the stub log.

### BF-44 (low): glob token edge cases

- **Files:** `internal/cli/discovery.go:779, 798, 836`
- **Fix:**
  - Split `name:arch` at the last `:` only when the suffix is a known arch
    (`noarch`, `x86_64`, `i686`, `aarch64`, `armv6l`, `armv7l`, `ppc*`, each
    optionally `-musl`).
  - Rewrite `[!` to `[^` before `path.Match`.
  - Make an unbalanced `{` a usage error (exit 2), not literal text.
- **Tests:** `TestAC019GlobArchSuffixAndBracketNegation` (`pc:zlib-ng`, `font:*`, `cmdpack:x86_64`, `[!a]*`).

### BF-45 (low): cursor visible in the inline box; double blank line

- **Files:** `internal/tui` `Viewport` (anchor / teardown), `internal/cli/inline.go`
- **Fix:**
  - Emit `\x1b[?25l` when a viewport anchors and `\x1b[?25h` in `Teardown`
    and in the guard's restore list (`term.Guard`), so a panic or signal also
    restores the cursor.
  - Skip the no-anchor `\r\n` when the previous surface in the same process
    ended on a fresh line (track it in `state`).
- **Tests:** a PTY transcript contains `?25l` before the first frame and
  `?25h` after teardown; no `\n\n\n` between the predownload and commit
  surfaces.

### BF-46 (low): PTY open failure leaves the widget at 0%

- **Files:** `internal/cli/run.go:141-144`
- **Defect:** on `openPty` failure (for example a chroot without devpts) the
  run falls back to `streamDirect`, which in live mode never feeds the parser.
- **Fix:** in `streamDirectWithSignals`, when `r.live`, tee stdout and stderr
  through line scanners (using `boundedSplit` from BF-06) into
  `r.parser.FeedStdout` / `FeedStderr`, exactly as the hybrid path does.
- **Tests:** `TestDirectLiveFeedsParser`: force `openPty` to fail through a
  package variable and assert the model reaches 100%.

---

## Phase 6: config, completion, manpages

### What was checked and is correct

This review found no high- or medium-severity defects in this area. It
confirmed:

- **TOML subset:** CRLF, literal and basic strings with escapes, inline
  tables (trailing comma rejected), comments, duplicate keys and tables
  (including alias pairs `[nala]` + `[Nala]`), dotted keys, multiline
  strings, BOM, invalid UTF-8, and `--config -` / `/dev/stdin`. Two
  differential runs against Python `tomllib` (800 generated documents) agreed
  on 779. All 21 differences are constructs the spec deliberately rejects:
  floats, dates, hex integers, underscores, quoted keys and dotted keys. The
  parser accepted nothing `tomllib` rejects.
- **Layers:** the user layer overrides the system layer per key;
  `--config` replaces both; the last `--config` wins; `[Color.theme]` merges
  role by role.
- **Legacy keys:** all ten rev-5 keys map, deprecate or retire exactly as
  `docs/migration-rev6.md` says, with one aggregate warning across both
  layers.
- **`[Xbps]` path trust:** functional behaviour only (see "Out of scope"). An
  untrusted file loses its `[Xbps]` paths with one warning per key and keeps
  its other keys. A symlinked config is judged on its target.
- **Completions and manpages:** committed `completions/*` are byte-identical
  to `xbp completion <shell>`; manpage freshness tests pass; bash behaviour was
  checked live. zsh and fish were checked by reading only, because neither
  shell is installed here.

### BF-47 (low): `./path` values are reported as "floats are not supported"

- **Files:** `internal/config/toml.go:701-710` (`startsNumeric`), reached from `parseScalar` (L615-625)
- **Defect:** `startsNumeric` treats a leading `.` as numeric, so a bare
  `./xbps-query` reaches `looksLikeFloat` and the error names floats. The
  value is still rejected with exit 2; only the message is wrong.
- **Repro:** `printf 'xbps_query_path = ./xbps-query\n' > t.toml; xbp --config t.toml list -i`
  prints `config t.toml:1: floats are not supported`. The expected message,
  which `relative/xbps-query` already gets, is
  `[Xbps] query_path must be a bare program name or an absolute path, got "./xbps-query"`.
- **Fix:** remove `c == '.'` from the `case` in `startsNumeric`. TOML 1.0
  forbids a leading `.` in floats anyway, so nothing valid is lost.
- **Tests:** `internal/config/config_test.go` `TestLegacyDotPathMessage`: the
  error contains `must be a bare program name or an absolute path` and not
  `floats`. Add one more case each to the float tests in `toml_test.go`:
  `.5` is rejected and `0.5` keeps the float message.

### BF-48 (low): raw control characters are accepted inside strings

- **Files:** `internal/config/toml.go:388-411` (both string loops in `parseString`)
- **Defect:** TOML 1.0 forbids U+0000–U+001F (except tab) and U+007F inside
  strings. The parser copies them. Schema checks catch most uses later, but
  with a misleading message. For example, `fg = "Red\x01"` gives
  `unknown colour "Red\x01"`, and a control byte inside an `[Xbps]` path would
  only be caught by the path check.
- **Fix:** in both the literal and the basic string loops, before appending
  byte `c`:
  ```go
  if (c < 0x20 && c != '\t') || c == 0x7f {
      return p.errorf("control characters are not allowed in strings")
  }
  ```
  Leave escaped forms (`\n`, `\t`, `\u0001`) alone: they are valid in basic
  strings.
- **Tests:** `toml_test.go` `TestParseRejectedConstructs`: add cases with a
  raw `0x01` in a basic string and a raw `0x7f` in a literal string, and
  assert the error text and line number. Add a positive case: `"a\tb"` with a
  raw tab is still accepted.

### BF-49 (low): accepted no-op flags are never completed

- **Files:**
  - `internal/completion/script.go:81` (`wordsFor`), `:458` and `:465` (fish writers)
  - `internal/completion/completion_test.go:60-64` (the helper mirrors the filter, and its comment calls NoOp flags "N/A")
- **Defect:** the generators skip `f.NoOp` as if it were N/A. FR-019
  separates *refused* N/A flags (never offered, FR-005) from *accepted*
  no-ops:
  - `-A/--all-arches` on `list`, `search` and `show`
  - `--no-fix-broken`, `--no-install-recommends`, `--no-install-suggests`
  - `--full`, `--no-full` and `--safe` on transaction verbs

  So `xbp list -<TAB>` never offers `-A`, although `--help` lists it.
- **Decision D-3:** offer accepted no-ops in completion. Default **yes**,
  matching `--help` and nala, where these flags complete. If you choose "no",
  instead change the test comment and FR-005 to say no-ops are deliberately
  hidden, and close this item.
- **Fix (if yes):**
  1. Change the filter to `f.Hidden || f.Refusal != ""` at `script.go:81`,
     `:458` and `:465`, and in the test helper. Fix the helper's comment.
  2. Run `make completions` and commit the regenerated `completions/*`. The
     freshness test fails until you do.
- **Tests:** extend `TestEveryCommandOffersItsOwnFlags`:
  - bash `flags_list` contains `-A` and `--all-arches`
  - `flags_install` contains `--no-fix-broken`
  - refused flags (for example `--allow-unauthenticated`) are still absent

### Note: theme role merge replaces the whole role

A user `[Color.theme] Notice = { fg = "Blue" }` replaces the system layer's
whole `Notice` style, so a system `modifier = "Bold"` is dropped. That matches
"theme roles merge role by role" in the README and FR-054. It is recorded here
only so the behaviour is a conscious choice; no change is planned.

---

## Spec, ADR and doc changes (collected)

| Change | Driven by |
|---|---|
| FR-012, FR-013, FR-064, FR-066, FR-071; AC-008, AC-009, AC-046, AC-068 | BF-02, BF-03 |
| Parity TSV rows P-VIS-016/017/031/032, P-BEH-024, new per-repository row; `make parity` | BF-03 |
| New ADR-0021 (sync step follows nala update; D-1, D-2) | BF-02, BF-03 |
| FR-063 / reconciliation note: remove the `VersionDiff` defect text | BF-34 |
| FR-072: paste mode is stateful, with no timeout | BF-08 |
| FR-064: Install-view keys are package names; removals within install count | BF-17, BF-19 |
| FR-074 note: stderr EPIPE never reaches the child | BF-07 |
| `docs/research/xbps-progress-telemetry.md`: repodata formats, `removing` lines | BF-03, BF-19 |
| Optional, not part of this plan: add a v0.18.2 screenshot to `screenshots-nala/`, and label the existing ones "legacy Python nala 0.15.x" | review note |

## Suggested PR breakdown

Each PR must pass `make check`, which runs tests, vet, parity freshness,
completions and manpages, shellcheck and mandoc.

1. **PR 1:** BF-01 (tests hermetic). This must land first so every later PR
   has a reliable baseline.
2. **PR 2:** BF-06, BF-07, BF-08 (hang, SIGPIPE, paste). Small, independent, critical.
3. **PR 3:** BF-09, BF-10 (log path safety under sudo/doas).
4. **PR 4:** BF-11, BF-12, BF-13 (fetch).
5. **PR 5:** BF-14, BF-15 (args, discovery state).
6. **PR 6:** BF-05, BF-04, BF-02, BF-03, plus the spec, ADR and parity
   updates. This is the visible nala-parity fix; do the parts in that order
   inside the PR.
7. **PR 7:** BF-16 to BF-21 (progress accuracy and passthrough).
8. **PR 8:** BF-22 to BF-34 (rendering; regenerate goldens once at the end).
9. **PR 9:** BF-35 to BF-46.
10. **PR 10:** BF-47 to BF-49 (Phase 6). Confirm D-3 first, then run `make completions`.

## Verification after all PRs

1. `go test ./...` passes on a machine with a populated `/var/lib/xbpretty/history`.
2. `make check` passes.
3. Manual check on a real Void install:
   - `sudo xbp upgrade` on a TTY shows `No Change:` / `Updated:` lines, the
     7-row `Updating Package Lists` widget, then `Fetched … in …` or
     `Nothing to fetch`, then the count line, then the summary.
   - `sudo xbp upgrade --dry-run 2>/dev/null | cat` prints only the summary on stdout.
   - `sudo xbp update` looks the same as the sync part of `upgrade`.
   - `sudo xbp fetch --auto 1` twice, then `sudo xbp clean --fetch`, restores the original `/etc/xbps.d`.
4. The same on a musl VM: `sudo xbp fetch --dry-run` ranks mirrors.

## Out of scope / not reviewed

- **Security review** of the `[Xbps]` binary-path root-trust check (ADR-0016),
  shell-completion handling of untrusted package names, and the release
  signing pipeline (`.github/workflows/release.yml`). The reviewer for this
  area was stopped by Anthropic's usage safeguards before it reported. Phase 6
  checked only that the trust check behaves as documented, not whether it can
  be bypassed. A
  dedicated security review, with the project's own tooling or a human
  reviewer, is recommended before 0.4.1. BF-09 was found by a different
  reviewer and is included.
- The spec's broader parity decisions not listed above (for example the
  Download view's per-mirror table, P-VIS-030, which nala v0.18.2 does show
  and xbpretty could now feed from its 3-per-host predownload limiter). These
  are candidates for a later `bug-fix-2`.

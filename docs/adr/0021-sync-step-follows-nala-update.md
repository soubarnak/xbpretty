# ADR-0021: The sync step follows nala's update view

- Status: Accepted
- Date: 2026-11-06
- Relates to: FR-012, FR-013, FR-064, FR-066, FR-065, AC-008, AC-009, AC-046,
  AC-068; ADR-0005, ADR-0012, ADR-0020. Supersedes #45 D10 and D12 for the
  sync step only.

## Context

In nala v0.18.2, `nala upgrade`, `full-upgrade` and `safe-upgrade` run the whole
`update()` routine before planning, gated by `auto_update`, `--update` and
`--no-update`. So `upgrade` shows what `nala update` shows: the 7-row
`Updating Package Lists` widget, persistent per-repository lines above it, a
completion line (`Fetched <size> in <time> (<rate>/s)` or `Nothing to fetch`)
and the count line.

xbpretty drew a different sync. `xbp update` drew a 6-row widget with no
per-repository lines, and the upgrade family forwarded xbps's raw
`[*] Updating repository` text through `streamStderr`. Two rev-6 spec rows
encoded false premises to justify that: P-VIS-031 said xbps reports no fetched
byte total (it prints `<arch>-repodata: <size> [avg rate: …]` for each
transfer), and P-VIS-032 said xbps always syncs (the honest mapping is
"nothing downloaded" to `Nothing to fetch`). Bug-fix-1 (BF-02, BF-03, BF-04,
BF-05, BF-16) corrected the code and the spec.

Two questions needed a recorded answer: which stream the new lines go to, and
where the count line belongs during an upgrade.

## Decision

1. **One sync routine.** `runSync` in `internal/cli/sync.go` serves `update`,
   the upgrade family and `--update`. The live view is gated on `liveMode()`
   alone, never on the command name. A `--dry-run` upgrade shows the update view,
   as nala does.
2. **Per-repository lines.** `No Change: <desc>`, `Updated: <desc> [<size>]` and
   `Error: <desc>` with xbps's reason on the next line, printed above the box as
   each repository finishes. `<desc>` is the base URL and `<arch>-repodata`
   separated by a space. There is no `Ignored:` line, because xbps has no
   optional index files.
3. **Completion line.** After a successful sync, `Fetched <size> in <time>
   (<rate>/s)` when a repodata transfer completed, else `Nothing to fetch`. The
   size sums the sizes on xbps's completion lines. That gives KiB precision from
   xbps output. A stat diff of `/var/db/xbps` was considered and not used: the
   completion line already carries the size, and the stat diff would miss a
   rootdir passthrough.
4. **Key import.** A new-key block suspends the box and is forwarded raw until
   the next repository line, so xbps's `Do you want to import this public key?`
   prompt is visible and can be answered on stdin. This is nala's release-info
   suspend in xbps's terms.
5. **D-1, stream: stderr.** The widget, the status lines and the completion line
   stay on stderr, as ADR-0005 requires. nala prints them on stdout. Keeping
   stderr preserves `xbp upgrade --dry-run | grep` and the durable-output rule
   (FR-066). This is a recorded deviation from nala.
6. **D-2, count line.** `update` prints its count line on stdout, because the
   count is its durable output. The upgrade family prints the count line on
   stderr after the sync, from the rows already planned, so stdout holds only
   the summary. nala prints it on stdout during `upgrade`. This is a recorded
   deviation.
7. **D-3, accepted no-ops are completed.** `-A/--all-arches`, `--no-fix-broken`,
   `--no-install-recommends`, `--no-install-suggests`, `--full`, `--no-full` and
   `--safe` are offered by completion, because `--help` lists them. Refused N/A
   flags are never offered (FR-005).
8. **Non-live deviation.** A live surface needs stderr to be a TTY, `TERM` not
   `dumb`, not `-v`, and `[Ui] mode` not `Plain`. Off a live surface the sync
   forwards xbps's raw lines on stderr and prints none of the per-repository or
   completion lines. This keeps scripts that grep xbps output working (FR-013).
   A TTY run can also take this path: `-v` prints every command and its raw
   sync output, and `TERM=dumb` or `Plain` ask for no live UI. When only stdout
   is redirected, the lines are printed through the plain 40-cell line (FR-065).
9. **`update_show_packages` is update-only.** `update` lists packages when it is
   set. The upgrade family prints only the count line, so it never lists
   packages.
10. **Spec revision.** The change is recorded as an in-place amendment of
    rev 6, and the revision number stays 6. ADR-0020 decision 5 makes a
    disposition change a new spec revision. This ADR is the recorded exception:
    0.4.1 is a bug-fix release on the rev-6 contract, and the two dispositions
    changed because the rev-6 premises were false. A new revision would rename
    the contract for a patch release. The lead decides whether a rev 7 is
    opened instead.
11. **No inventory row for the per-repository lines.** The nala inventory is
    frozen to its tag, with 189 rows and fixed kind counts (ADR-0020). Adding a
    row for `No Change:`/`Updated:`/`Error:` would be a newly discovered nala
    surface, which is also a spec revision. The lines are documented under
    P-VIS-017 instead.

## Alternatives rejected

- **Print the lines and the widget on stdout, as nala does.** It would make
  `xbp upgrade --dry-run` output a mix of progress and summary, and break the
  durable-output rule for every script that pipes stdout (D-1).
- **Keep the raw forwarding for the upgrade family.** It is what the bug report
  complained about; `update` and `upgrade` must look the same (BF-02).
- **Stat `/var/db/xbps/*/<arch>-repodata` before and after the sync for the
  Fetched size.** The completion line already carries the size. A stat diff
  would need a second glob over the xbps cache and would not follow a `-r`
  rootdir passthrough. Bug-fix-1 preferred the stat diff, with the completion
  sum as the fallback. The shipped code takes the completion sum only, which is
  a deviation from the plan, recorded here.
- **Print the count line on stdout during the upgrade family, as nala does.**
  The summary is the transaction's durable output, and a second stdout block
  would break `xbp upgrade --dry-run | cat` (D-2).

## Consequences

- P-VIS-031 becomes `adaptation` and P-VIS-032 becomes `equivalent`. The N/A
  total falls from 37 to 35. `tools/parity/parity_test.go` still asserts 37, so
  the test constant must change with this ADR. That edit is a Go file and is
  outside the documentation stream.
- `Fetched` shows KiB precision, because xbps prints `1854KB`, not bytes.
- A zero-exit sync whose repositories all report `Error:` prints no completion
  line, because xbps exits 0 on fetch failure (FR-049) and `Nothing to fetch`
  would be false. A sync with at least one fetched repository still prints
  `Fetched`.
- The Update view is 7 rows, like Download. The plain 40-cell line keeps
  `Repositories n/N`, a documented extension.
- A future nala surface for the per-repository lines would need its own row,
  and so a new spec revision.

# ADR-004: The summary's figures follow the transaction, not the plan

- Status: Accepted
- Date: 2026-10-15
- Relates to: FR-011, FR-012, NFR-004
- Answers: the open consequence in ADR-003 about held rows and the download total

## Context

FR-012 fixes what the footer's figures mean — new installed size minus old
installed size minus removed size, against a download total — but not which
plan rows feed them. Every row of a plan carries two sizes, including rows for
work the transaction does not do, and the captures show this plainly:

- `0607/remove.txt`: `wget-1.25.0_1 remove ... 3673038 824658`. Nothing is
  fetched for a removal; 824658 bytes of download belong to no fetch.
- `0607/hold.txt`: `acl-2.4.0_1 hold ... 43080 20653`. A held package is not
  fetched and not unpacked either.
- `0607/configure.txt`: `libpcre2-10.49_1 configure ... 2032768 597015`. The
  package's files are already unpacked, so the bytes are already on disk.
- `0607/reinstall.txt`: `curl-8.22.0_1 reinstall ...` — read from the same
  facts and counted the same way.

The three questionable rows are therefore exactly the three actions that do no
work, and the same principle settles all of them: a plan row contributes to a
figure only where it does the thing the figure measures.

## Decision

1. **Download counts fetches.** Only `install`, `update` and `reinstall` rows
   add to the download total. `remove`, `hold` and `configure` rows add
   nothing, whatever sizes they carry. This answers the question ADR-003 left
   open: held rows are not counted, because a held package is not downloaded.

2. **Disk is FR-012's formula, with the replaced size subtracted in full.** An
   upgrade subtracts the installed size the lookup answered for, whether the
   package grows or shrinks; `configure` and `hold` rows add nothing. A
   shrinking upgrade is therefore not a special case, and a `remove` row frees
   its installed size.

3. **A held package is reported under Hold alone.** It is not listed in
   Install or Upgrade as well. The dry run names it `hold` (0.60.7), and the
   `xbps-query -H` map is the second source for a plan that does not; either
   way, the package is not part of the transaction, and listing it under
   Upgrade would say that it is. The two sources are joined on the package
   name alone, taken off the pkgver, so `base-files-0.146_1` is held as
   `base-files`.

4. **`configure` rows are counted, not listed** (FR-011). They count in the
   Install total; no line, no group of their own. A plan of nothing but
   configure rows prints its count and no group.

5. **A row's size column shows what the transaction does with it.** Rows the
   transaction fetches are shown by their download size; a removal and a held
   package, neither of which is fetched, by their installed size.

## Consequences

- The footer's counts and its download and disk figures describe the same set
  of rows, so a held or configure row can raise a count without raising a
  figure. That is intended: FR-011 asks for the count.
- A package whose old size the lookups did not answer for contributes its new
  bytes and nothing on the old side, because FR-012's formula has no old
  figure to subtract.
- Sizes are rendered with the space FR-012's example shows (`48.2 MiB`), the
  counts keep the nouns that example spells (`3 install`, not `3 installs`),
  and columns are padded by display width, so the `→` an upgrade carries —
  three bytes, one column — does not shift the rows under it.
- The `held:` note a group carries under Install and Upgrade pre-dates this
  ADR and now repeats what the Hold group already says.

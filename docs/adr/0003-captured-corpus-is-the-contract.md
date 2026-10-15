# ADR-003: The captured corpus is the contract

- Status: Accepted
- Date: 2026-10-15
- Relates to: FR-011, FR-017, NFR-004

## Context

NFR-004 asks for parser and hint fixtures from at least two xbps versions. The
corpus had none for the `hold` or `configure` actions, nothing that pinned what
`xbps-query -p pkgver,installed_size` actually prints, and
`scripts/capture-fixtures.sh` wrote `.out`/`.err` triples while the tests read
`.txt`/`.meta` — so regenerating the corpus could not feed the tests that are
supposed to define it.

The script now writes exactly what the tests read, into a directory per version
(`0.60.7_1` -> `0607`) and a directory per kind: `parser/` for the dry-run
stdout `internal/xbps` reads, `hints/` for the stderr `internal/hints` matches,
`lookups/` for the `xbps-query` answers `internal/cli` decides on. Capturing
the missing cases settled five behaviours the code had only assumed.

## What the captures settled

1. **A lookup answer is a pkgver, not a version.** `xbps-query -p
   pkgver,installed_size <pkg>` prints the pkgver on one line and the installed
   size on the next (`testdata/lookups`), the same on 0.59.1 and 0.60.7. The
   code read the first line as the version, so an upgrade whose old version was
   looked up rendered as `xbps-0.60.7_1 → 0.60.8_1`. The name is now split off
   the pkgver: `parseOld`, pinned by `TestParseOldMatchesCapturedLookups`.

2. **Held packages have an action of their own, and only 0.60.7 prints it.**
   With a package on hold, 0.60.7's `xbps-install -Sun` names it
   `acl-2.4.0_1 hold x86_64 ...`; 0.59.1 drops it from the plan altogether.
   `xbps-query -H` prints the *installed* pkgver on both versions, while the
   plan row names the version the repository offers, so the two disagree
   whenever an upgrade is pending. FR-011's Hold group is therefore built from
   the `hold` rows in the plan (`summary.Build`), with the `-H` map as the
   second source.

3. **A `configure` row is a state, not a plan step.** It appears for a package
   interrupted between unpacking and configuring, and only while no upgrade is
   pending for it — with one pending, the row prints `update`. 0.59.1 cannot
   produce one at all in a container, because it refuses every transaction
   until xbps itself is updated (the case its hint fixture captures).

4. **The "not installed" message is on stdout.** `xbps-remove -yn <pkg>` exits
   2 with `Package 'pkg' is not currently installed.` on **stdout** and nothing
   on stderr, on 0.60.7 both as a user and as root. FR-017 matches hints on
   stderr alone, so its `not-installed` entry cannot fire for this case. The
   fixture was recaptured from the stream the message really comes from
   (`testdata/hints/0607/not-installed.out`); what to do about the entry is a
   behaviour decision, not a fixture one.

5. **A truncated index reads as a missing package on 0.59.1.** With its
   repodata emptied, 0.59.1 fails `xbps-install -Sn figlet` with exit 2 and
   `Package 'figlet' not found in repository pool.` — no `failed to open
   repodata` line at all — and leaves `xbps-install -Sun` succeeding with an
   empty plan and empty stderr. The `stale-index` pattern is therefore a
   0.60.7 reading of the breakage (`testdata/hints/0591/not-found.txt`).

## Decision

The corpus in `testdata/` is the contract the parser, the hint table and the
lookup reading are written against, and `scripts/capture-fixtures.sh` produces
it in the shape the tests read back. A capture that stops matching is a failing
test, not a stale file. State a case needs is induced deliberately and recorded
in its `.meta`, because two of the actions only occur in state that a normal
run does not reach.

## Consequences

- A regenerated capture is a new capture: the expectations pinned to it (row
  counts, sizes, versions in `plan_test.go`) move with it.
- Held rows carry a download size from the plan, though a held package is not
  downloaded; whether the footer's download total should count them is
  unanswered here.
- The `not-installed` hint entry stays in the table but is unreachable until
  the stream question in (4) is settled.
- `lookupOld` issues the same one-package-per-call lookups as before; only the
  reading of the answer changed (ADR-001 stands).

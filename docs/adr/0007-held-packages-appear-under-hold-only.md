# ADR-0007: Held packages appear under Hold only, and no summary line ends in whitespace

- Status: Accepted
- Date: 2026-10-18
- Relates to: FR-011, FR-012, ADR-0004
- Supersedes: the last consequence of ADR-0004 (the `held:` note repeats the Hold group)

## Context

ADR-0004 decided that a held package is reported under Hold alone, and noted
as a consequence that the dim `held:` note under Install, Upgrade and Remove
"pre-dates this ADR and now repeats what the Hold group already says". The
note stayed in the code, so every held package was printed twice.

Rows also ended in trailing spaces. Every column, the last included, was
padded to the width of its longest cell. The last column has nothing after it,
so its padding is invisible in a terminal and noise in `xbp upgrade -n | grep`,
in diffs and when copying.

## Decision

1. **The `held:` note is removed.** A held package is listed once, in the Hold
   group, with colour on or off. Install, Upgrade and Remove list only the
   packages the transaction changes.
2. **The last column of a row is not padded.** Every other column keeps its
   width rule, so column starts, and what a user sees in a terminal, do not
   move. No group header, row or footer ends in whitespace.
3. **The padding helper `pad` is deleted.** It had no caller; `padRight` stays
   for the columns that still pad.

## Consequences

- ADR-0004 decision 3 now holds without an exception, and its consequence about
  the repeated note no longer applies.
- The Hold group is the only place a held package is named, so the footer's
  `N hold` count and the Hold group agree.
- A summary line can be matched with `$` anchors.

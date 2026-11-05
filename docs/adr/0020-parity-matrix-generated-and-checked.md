# ADR-0020: The parity matrix is generated and machine-checked

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-077, NFR-004, AC-061–AC-063

## Context

"Full parity or explicit, documented, tested N/A" is the bar the project holds
itself to, and a hand-maintained table cannot hold it: it rots the first time a
flag is renamed, and emptiness is invisible. A pure test suite cannot hold it
either — it cannot run before the implementation exists, and it cannot express
an absence such as "the Download view has no mirror table".

## Decision

1. Two TSVs are the only source: `testdata/parity/nala-v0.18.2.tsv` (frozen
   inventory, 189 rows) and `testdata/parity/matrix.tsv` (one disposition per
   surface, 189 rows, 37 N/A, `# spec_rev: 6`).
2. `xbpretty-spec.md` Appendix A and `docs/parity/matrix.md` are generated from
   them and freshness-tested; a rendered table is never hand-edited.
3. Every `n/a` row names the exact refusal (stderr line + exit 2) or the exact
   absence (a literal that never appears plus what happens instead), cites a
   spec AC, and — once the implementation exists — a resolving Go test.
   `release:` markers are forbidden on N/A rows.
4. The checker is stdlib Go in `tools/parity` (the map reserved
   `internal/parity`; either location is the same contract). It runs at spec
   approval without product code and at release with test-resolution and
   `internal/spec` cross-checks. `go test ./...` is the gate.
5. A disposition/xbps/behaviour change or a newly discovered nala surface is a
   new spec revision; test names, evidence lines, notes and typos are in-place
   maintenance. A new xbpretty-only feature is an FR, never a matrix row.

## Alternatives rejected

- A reviewed document alone: no mechanical completeness, rots silently.
- An executable suite alone: cannot run pre-implementation, cannot express
  prose or visual absences, has no inventory.
- Markdown as the normative source: escaping traps, fragile parsing, reflowed
  diffs. JSON: hostile to 189 hand-edited rows.

## Consequences

- Adding a nala surface is a spec revision; adding an xbpretty feature is not a
  matrix change at all.
- The nala side is source-evidenced, never execution-evidenced: nala cannot be
  built or run on the development host.
- The matrix is a fixture family, not a document about fixtures; its
  implementation backlog falls out of the `test: —` cells.

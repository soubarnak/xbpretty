# ADR-0013: History is a per-entry store with best-effort replay

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-029–FR-035, AC-024–AC-029, AC-043
- Amends: ADR-0008 (command vocabulary; line shape unchanged)

## Context

Rev 5 scoped undo/redo out and kept only an append-only line log. nala v0.18.2
writes one JSON file per successful transaction and replays through its normal
transaction path. xbps has no history, no downgrade action and no apt resolver,
so parity requires a new store and an explicit replay algorithm that fails
closed when an archive is gone.

## Decision

1. **Store** — `/var/lib/xbpretty/history/<id>.json`, schema 1, before/after
   package snapshots, root-owned `0644` and never chowned. IDs are max+1;
   writes use a temp file, fsync, an exclusive link and a directory fsync.
   Corrupt entries fail the whole read; `history clear --all` is the escape
   hatch.
2. **When** — one entry after a real xbps child exits 0, before the line log
   and any implicit autoremove; a failed transaction, dry run, download-only,
   `update`, `clean`, `download`, `fetch`, declined prompt or hold-only plan
   writes nothing. A write failure is a warning and never changes the exit
   code.
3. **What** — xbps-native transitions: operation derived from the before/after
   snapshots (`xbps-uhelper cmpver` distinguishes upgrade from downgrade),
   exact plan sizes, arch and repository, `automatic-install`, hold and
   repolock. `config_files_only` does not exist and is never recorded.
4. **Replay** — pinned versions only. Preflight each restore against
   `/var/cache/xbps` and then `xbps-query -R -p pkgver`; if neither serves the
   exact version, refuse **before any mutation**. Restore first, remove second,
   one summary and one prompt, a replay repo built with `xbps-rindex` for
   cache-only archives, one `-f` (never `-f -f`), no sync and no implicit
   autoremove.
5. **Redo and clear** — redo replays the after state; bare undo targets the
   newest entry, bare redo targets the source of the newest undo, explicit
   `redo last` is literal. `history clear <id|last>` and `--all` are root-only.
6. **Line log** — dual-write, distinct roles, never imported. Legacy lines lack
   the data a replay needs, so synthesising entries would offer undo that
   cannot be honoured. The command vocabulary gains `purge`, `autopurge`,
   `undo`, `redo`; the line shape is unchanged. `--no-log` does not disable
   history.
7. **Empty store** — `No history entries found.` on stdout, exit 0, plus one
   stderr note when the line log is non-empty. No synthetic rows.

## Consequences

- Undo can restore versions and package presence, not the whole filesystem:
  modified conffiles survive one `-f`, purged conffiles cannot return, and
  dependencies outside the entry stay installed.
- A missing archive, a moved repository or a partial commit is a documented
  limit with an explicit message, never a silent partial undo.
- The history store is system state; the line log remains the invoking user's
  audit file. ADR-0008 records the vocabulary amendment.

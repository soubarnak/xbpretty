# ADR-0018: Rev 6 migration is warnings and documentation

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-035, FR-056, FR-058, FR-060, AC-041–AC-043
- Amends: ADR-0008 (line log is permanently audit-only)

## Context

Rev 6 changes the config schema and retires three keys. Existing users have a
flat `config.toml` and a line-based transaction log. A converter or an
automatic rewrite would be a destructive root write to a hand-maintained file
at upgrade time; importing the line log into replayable history would fabricate
undo entries that cannot be honoured, because the lines lack old versions,
operations, arch/repository, sizes and snapshots.

## Decision

1. **No write** — rev 6 never creates or modifies a config file, backup, marker
   or temp file. No `config migrate` command.
2. **Read and warn** — the ten rev-5 flat keys are accepted for rev 6 with one
   warning per key, plus one aggregate warning pointing at the migration guide.
   The seven mappable keys alias to their rev-6 targets; `verbose`, `box_style`
   and `progress_bar` are retired and ignored. Within a file the canonical key
   wins; across layers the later layer wins.
3. **Value shim** — the ten legacy keys also accept rev-5 bare strings and
   quoted booleans, because rev 5 itself did. Everything else stays strict
   TOML. Rev 7 removes both the aliases and the shim.
4. **Docs** — `docs/migration-rev6.md`, `docs/config.toml.example` (byte-for-byte
   the canonical example, parses to defaults), a README section, and a
   release-note block. Installed as `/usr/share/doc/xbpretty/migration-rev6.md`
   and `config.toml.example`. The package creates no `/etc` file.
5. **Line log** — unchanged, append-only, audit-only, never imported, never
   rewritten. `xbp log` reads old lines verbatim. `--no-log` does not disable
   history.
6. **Empty history** — `No history entries found.` on stdout, exit 0, plus one
   stderr note when the line log is non-empty. History ids start at 1 with the
   first successful post-rev-6 transaction.
7. **Boundary** — rev 6 accepts and warns; the first release carrying spec
   rev 7 rejects with exit 2. The window is one release train; extending it
   needs a new spec revision. Rev 6 is 0.4.0, rev 7 is 0.5.0.
8. **System layer** — optional, absent by default, admin-created root-owned and
   not group/world-writable. A legacy path in a user file under sudo warns
   twice (deprecation, then trust), is ignored, and the built-in binary is used.

## Consequences

- Rollback is free: the file was never modified, so a 0.3.x binary still reads
  it.
- Users see exactly what to change and where to read more; there is no hidden
  conversion.
- The rev-7 removal is a planned, documented break, not a surprise.

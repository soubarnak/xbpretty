# ADR-0016: Config is nala's TOML in layers, and only root-trusted files choose binaries

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-003, FR-051–FR-059, AC-037–AC-040, AC-067

## Context

Rev 5 had one user file with flat keys, including `xbps_install_path`. Under
`sudo`, xbpretty reads the invoking user's file, so a user-owned file could
make root execute a user-chosen binary. nala's schema is nala's TOML, strictly
validated; copying it gives parity, but xbps needs its own binary paths and log
toggle. A full TOML library would break NFR-001's zero-require rule for a small,
closed schema.

## Decision

1. **Schema** — exactly `[Nala]` (six keys, nala's defaults), `[Ui]` (`mode`,
   `unit`), `[Color]` (`mode`), `[Color.theme]` (nine PascalCase roles) and
   `[Xbps]` (six binary paths plus `transaction_log`). No other table or key.
   Unknown keys and values are hard errors, exit 2.
2. **Parser** — a stdlib-only TOML subset (~700 LOC), exact `path:line` errors,
   the canonical nala file parses unchanged, and every unsupported construct
   fails loudly with a named message.
3. **Layers** — `/etc/xbpretty/config.toml` (optional), the user XDG/passwd-home
   file, and long-only `--config FILE` replacing both. Under `sudo`/`doas`,
   `XDG_CONFIG_HOME`/`XDG_STATE_HOME` are ignored and the invoking user's
   passwd home is used. Later layers win per key; theme roles merge.
4. **Trust** — when euid is 0, a `[Xbps]` path is honoured only from a regular
   file owned by uid 0 with no group/world write bit, judged on the opened
   descriptor. Untrusted keys are ignored with one warning; non-privileged keys
   still apply. A legacy `xbps_*_path` is gated identically.
5. **Legacy** — ten rev-5 flat keys are accepted for rev 6 with per-key
   warnings and a rev-5 value shim (bare strings/quoted booleans) scoped to
   those ten keys; rev 7 rejects them. Nothing is rewritten.
6. **Flags** — `--config` is long-only; `-c` is fetch's `--country`. `--debug`,
   `--color`/`--no-color`, `--tui`/`--no-tui`, hidden `--history-dir` and
   `-v`/`-V` are global; every other flag is command-scoped.
7. **No invented keys** — no config key for history dir, predownload, cache,
   proxy or fetch. Those have their own mechanisms and are named in the
   tailored errors.

## Consequences

- The privileged-path hole is closed without losing FR-018a or fixture testing.
- A rev-5 file keeps working and warns; a typo is never silently accepted.
- Vendoring a TOML library is rejected permanently by NFR-001; the subset is
  the contract.

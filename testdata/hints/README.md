# Hint fixtures

The streams xbps wrote for each failure `internal/hints` explains, captured
byte for byte. A case keeps both streams under fixed names, because xbps
writes some failures to stdout and some to stderr (ADR-0006):

| File | Holds |
|---|---|
| `<case>.txt` | stderr |
| `<case>.out` | stdout |
| `<case>.meta` | the command, its exit code, and any state the capture induced |

| Directory | xbps version | Source |
|---|---|---|
| `0607/` | 0.60.7_1 | Host Void Linux box |
| `0591/` | 0.59.1_5 | `voidlinux/voidlinux:latest` container |

## Missing `.out` files

Three `0607/` cases have no `.out`: `stale-index`, `stale-index-upgrade` and
`lock-wait`. They were captured on the host before the script kept stdout. The
hint test reads a missing `.out` as empty, so nothing fails, but that is a
default, not an observation: their stdout was never recorded.

They cannot be filled in without the original circumstances. `stale-index` and
`stale-index-upgrade` need the live repository index truncated, and
`lock-wait` needs two real transactions racing, so only the host capture
(`sudo XB_CAPTURE_MODE=host scripts/capture-fixtures.sh`) reproduces them, and
it performs real installs on the host (host mode refuses to run without root
or on any xbps other than 0.60.7, unless `XB_CAPTURE_ALLOW_XBPS_VERSION=1`; its
lock race never upgrades the host, see ADR-0010). It also replaces the
plans in `testdata/parser/0607/`, so it waits for a reason of its own.

`0607/not-found.out` is the exception: its command is a dry run that needs no
privilege, so it was re-run as a user and added. Its stderr matched `.txt`
byte for byte, and `not-found.meta` says so.

## Cases added on their own

`0591/xbps-unsigned-update-install` (`xbps-install -y figlet`) came from
running its `capture_pre` line alone in the container, after the same
repository setup, rather than from a full run of the phase. A full run would
also rewrite `testdata/parser/0591/upgrade` and the lookups, which follow the
live repository. Its stderr is byte for byte the refusal in
`0591/xbps-unsigned-update.txt`.

Regenerate with `scripts/capture-fixtures.sh`.

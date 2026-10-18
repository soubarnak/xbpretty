# ADR-0008: The transaction log line format is a contract

- Status: Accepted
- Date: 2026-10-18
- Relates to: FR-013, FR-019, AC-012
- Changes: what FR-013 records (held packages are dropped)

## Context

FR-013 says every successful real execution is appended to the transaction
log, one line each. The line format was a detail of `writeLog`: nothing said
what the fields were, and the line listed every row of the plan. A plan
carries `hold` rows for packages the user pinned, so a transaction that never
touched `linux6.12` still logged it. A reader of the log could not tell what
a transaction changed. A reader that parses the log, such as the `xbp log`
command (ticket #21) or a user script, needs a format it can rely on.

## Decision

**The line format is a contract.** Each line is

```
<timestamp> <command>[ <name-version>...]
```

- `<timestamp>` is an RFC 3339 timestamp in UTC, with a trailing `Z`, for
  example `2026-10-15T09:41:01Z`.
- `<command>` is the xbpretty command name the user ran: `install`, `remove`,
  `upgrade`, `autoremove` or `update`. It is not xbps's own argv.
- Each package entry is the package name, a hyphen and the version, as xbps
  prints it (`figlet-2.2.5_6`). Splitting at the last hyphen gives name and
  version, the pkgver rule the summary uses.
- Fields are separated by a single space. A line never ends in a space, and
  never has leading, doubled or tab separators. A line ends with one `\n`.
- `update` lines carry no packages, so an `update` line is the timestamp, one
  space and `update`.

**Only changed packages are written.** The packages on a line are the
transaction's `install`, `update`, `reinstall`, `remove` and `configure`
rows, in plan order. `hold` rows are not written: a held package is not
touched by the transaction.

This is the one change to what FR-013 records. FR-013 listed every package in
the plan, which included held packages. From this ADR on, held packages are
dropped. A transaction whose plan holds only `hold` rows logs a line with the
command and no packages, like `update`. Rev 4 stays frozen; this ADR carries
the delta, and FR-013's "packages" reads as "changed packages" from here on.

**The log stays append-only and cannot replay a transaction.** Lines are only
ever appended, never edited, reordered or removed by xbpretty. A line records
what changed, not the arguments, flags, repository or plan sizes needed to run
the transaction again. The glossary's "Transaction log" entry already says
this and is unchanged.

Lines written before this ADR may name held packages. They are valid lines of
the same shape and are read as they are; nothing rewrites them.

## Consequences

- A reader can parse a line with a split on single spaces: field 1 is the
  time, field 2 the command, the rest the changed packages.
- `xbp log` (ticket #21) can answer "what changed" and match a package by exact
  name, since every package entry is a changed one.
- The format is documented in the README. A change to it needs a new ADR.
- Old lines in an existing log may still list held packages. A filter by
  package name can therefore show a line where an old transaction only held
  that package.

## The `xbp log` command

`xbp log [--limit N] [package...]` is the reader this contract exists for.

- It prints matching lines verbatim to stdout, oldest first, and writes
  nothing else there. It reads the path FR-013 and FR-019 resolve, so under
  sudo or doas it reads the invoking user's log. It resolves that path without
  creating directories, needs no root, never writes the log, never runs xbps
  and never prompts.
- A package argument is a name without a version. A line matches when any
  package entry on it, split at the last hyphen (`xbps.SplitPkgver`, the rule
  the summary uses), has exactly that name. `curl` matches `curl-8.10.1_1` and
  not `libcurl-8.10.1_1`. Several names match any of them. `update` lines carry
  no entries, so they never match a filter. Filtering never matches the
  timestamp or the command word.
- `--limit N` takes a positive integer and keeps the last N matching lines;
  the filter applies first. A zero, negative or non-numeric N is a usage error
  (exit 2), as is `--limit` on any other command.
- A missing or empty log prints one line on stderr saying nothing has been
  logged yet, and exits 0. A log that exists but cannot be read prints an
  error on stderr and exits 1. A non-empty log with no match prints nothing
  and exits 0.
- `--dry-run`, `--yes`, `--no-sync` and arguments after `--` are usage errors
  (exit 2): no xbps command runs, so they mean nothing. `--color` and
  `--verbose` are accepted and have no effect.
- `log` is reached as `xbp log` or `xbpretty log` only. There is no `xbp-log`
  link, so the binary keeps its six names and AC-026 stands.

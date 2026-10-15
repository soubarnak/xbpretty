# ADR-0005: Output rules the code only stated

- Status: Accepted
- Date: 2026-10-15
- Relates to: FR-013, FR-014, FR-017, FR-020, AC-012, AC-014
- Answers: what the rev 4 spec leaves open about a successful exit, the log's
  ownership and a signalled child (the fix pass's Q1 and Q2)

## Context

Four rules were written down in the tree — in comments, in the hint table, in
the spec — but never applied when a run reached them.

- The `stale-index` hint carries exit codes 0 and 2 because ADR-002 measured
  that a truncated index fails an install with 2 and leaves an upgrade with an
  empty plan and 0. The plan step read stderr only on a non-zero exit, so the
  0 branch was unreachable and the breakage reached the user as a bare
  `Nothing to do.`
- `--color=auto` returned `opts.color == "always"` for any explicit `--color`,
  so `auto` skipped both the TTY check and `NO_COLOR`.
- FR-013 logs every real execution that exits 0, and the glossary defines the
  transaction log the same way, but the log was written only when the run
  produced plan rows — so `update`, which produces none, never appeared.
- A signalled child has no exit code, and `exitCode` reported Go's `-1` as
  255. AC-014 promises the code the shell would see.

FR-013 and FR-014 also leave two things open that a run cannot avoid deciding:
who owns state written as root, and what code a signal-killed child reports.

## Decision

1. **A successful plan still reads stderr.** A dry run whose exit code is 0 is
   checked against the hint table too, and a match prints the raw xbps stderr
   followed by the hint, exactly as a failure does. The exit code stays xbps's
   own, so a stale index still exits 0 — the two lines are the only signal
   that the index, not the system, was empty (FR-017, ADR-002).

2. **`auto` means auto, wherever it comes from.** The `--color` flag overrides
   the config key, and `auto` is the same rule in both places: colour on a
   terminal with `NO_COLOR` unset, nothing otherwise. `always` and `never`
   keep overriding both checks (FR-020).

3. **Every real execution that exits 0 is logged, `update` included.** A line
   for a run with no packages names the timestamp and the command and stops
   there, so no field is ever empty and the line carries no trailing space.
   A failed or interrupted run writes nothing (FR-013).

4. **The log belongs to the invoking user.** When the effective user is root
   and `SUDO_USER` or `DOAS_USER` resolves, the state directory and
   `transactions.log` are chowned to that account's uid and gid, whether they
   were created by this run or by an earlier root-owned one. A failed chown is
   a warning, never an error, like every other log failure (FR-013, AC-012).
   Without an invoking user — a root shell, or a name that does not resolve —
   nothing is chowned and the files stay root's.

5. **A signalled child reports 128 + the signal.** SIGINT gives 130, SIGTERM
   143: the code a shell reports, which is what makes FR-014's "the child's
   exit code" true from a shell. A real exit code still passes through
   unchanged (FR-014, AC-014).

## Consequences

- A stale index prints the raw xbps line and the hint before `Nothing to do.`,
  on stderr; stdout still holds only the summary (NFR-005).
- The ownership of every line in the log follows the account that asked for
  the transaction, so `sudo xbp install foo` no longer leaves state that the
  invoking user cannot rewrite.
- A signal that xbpretty inherits as ignored is not forwarded: Go keeps an
  ignored disposition ignored, and a background job of a non-interactive shell
  gets SIGINT that way. Under a terminal — the Ctrl-C AC-014 describes — the
  forwarding is live, and the interrupted run writes no log line.
- The spec stays frozen at rev 4; this ADR and ADR-002 carry the deltas.

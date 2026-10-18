# ADR-0006: Hints match where xbps writes them

- Status: Accepted
- Date: 2026-10-18
- Relates to: FR-017, AC-015, AC-016, NFR-004
- Answers: which stream a hint is matched on (the open question in ADR-0003 #4)

## Context

FR-017 matches a hint on an exit code plus a stderr substring, and `plan` and
`execute` passed `hints.Lookup` only stderr. Two captures showed that xbps does
not keep to stderr.

- `xbps-remove -yn <pkg>` for a package that is not installed exits 2 and
  prints ``Package `pkg' is not currently installed.`` on **stdout**, with
  stderr empty: as a user and as root at 0.60.7, as root in the container at
  0.59.1. The `not-installed` entry could never fire.
- Asked to refuse because xbps itself is outdated, 0.59.1 writes `The 'xbps'
  package must be updated, please run ...` and exits 16 on **stderr** for
  `xbps-install -Sn htop curl wget` and `xbps-install -y figlet`, but on
  **stdout** for `xbps-install -Syu` and `xbps-install -y -u`. An `upgrade`,
  the command most likely to hit it, never got its hint.

The same captures settled the third observation in the ticket: with a
truncated index, 0.59.1 writes only `Package 'figlet' not found in repository
pool.` (ADR-0003 #5). That is the `not-found` hint, not a different one, and
needs no new entry; the `stale-index` pattern stays a 0.60.7 reading.

## Decision

**Each hint names the streams xbps has been measured writing its failure to,
and is matched on those streams only.** `Hint.Streams` lists them and
`Lookup(exitCode, Output{Stdout, Stderr})` reads each hint's substring from
its own streams.

| Hint | Stream |
|---|---|
| `stale-index` | stderr |
| `xbps-unsigned-update` | stderr and stdout |
| `not-found` | stderr |
| `not-installed` | stdout |
| `lock-wait` | stderr |

Matching both streams for every entry was the shorter change and was rejected.
A successful dry run carries the whole plan on stdout, and an exit-0 entry
(`stale-index`, `lock-wait`) would then scan package names for its substring.
Naming the stream keeps AC-016's rule that a hint is never guessed.

The exit-code-plus-substring rule is unchanged: both must match, and an
unmatched failure gets no hint.

### The dry run reads both streams; the real run reads stderr

`plan` already captures both streams, so it passes both. `execute` streams
xbps straight to the terminal (FR-014) and tees only stderr. Teeing stdout
would turn it from a terminal into a pipe, which xbps can tell apart, and the
live output is what FR-014 promises.

That leaves a gap, stated here rather than covered. `not-installed` is fixed
end to end, because the dry run `xbps-remove -yn` carries the message. The
outdated-xbps refusal on an upgrade is not: at 0.59.1 the dry run
`xbps-install -Sun` exits 0 with a real plan (`testdata/parser/0591/upgrade`),
and only the real run (`-Syu`, `-y -u`) is refused, on stdout, which `execute`
does not read. `xbp upgrade` against an outdated xbps therefore prints xbps's
own line and no `hint:` line. The `xbps-unsigned-update` entry lists stdout so
that a caller holding that stream gets the hint, and so the table records
what was measured; the real-run path does not hold it yet. An install gets its
hint, since xbps writes that refusal to stderr. Whether `execute` should read
stdout, and how without costing the terminal, was left as a separate decision;
the next section settles it.

#### Decision: the gap stays

The real run keeps reading stderr only. A measurement at 0.59.1 and 0.60.7
(rootless podman, `script -q` for the pty, fifos for the pipes) showed what a
stdout tee would cost: with stdout a pipe, xbps moves its progress and download
lines from stderr (`\r` redraws, `ESC[K`) to stdout as plain lines without the
`[ N%]` prefix. That is a visible change to the live output FR-014 promises.
(That measurement also reported the closing "downloaded, installed" line
missing from the piped stdout; the next subsection corrects it.)
A pty keeps the output but merges the two streams, which FR-017 and the
per-stream matching above rule out, and it would be the module's first
dependency (`go.mod` has none). So `xbp upgrade` against an outdated xbps keeps
xbps's own line and gets no `hint:` line. The line is clear without one.

The measurement also showed the refusal on stdout in every wiring for
`xbps-install -Syu`. An install does not follow it. At 0.59.1 every
install-shaped command exits 16 with the refusal on stderr and nothing on
stdout: `-Sn` and `-n` (the plan, with and without sync) and `-y` and `-Sy`
(the real run). Each was run for `figlet`, `-Sn` also for `htop curl wget`, and
`-y` and `-Sy` with stdout a terminal as well as a file. Only `-u` moves the
refusal to stdout. For
`xbp install` the plan is refused first, `plan` reads both streams, and the
real run is never reached; that run is `xbps-install -y` without `-S`, since
the plan synced, and its refusal would be caught on stderr anyway. So an
install gets its hint on either path, with or without `--no-sync`.

#### Piped stdout keeps the closing line

The measurement above left one question open: why the closing
"downloaded, installed" line did not appear on a piped stdout. It is answered
now, and the earlier report was wrong. **xbps 0.60.7 writes the closing line to
stdout when stdout is a pipe, and the line arrives intact once the pipe is
drained to EOF; it is neither withheld nor lost at exit.** The earlier miss
came from the reader, not from xbps: the line is the tail of the last
`write(1, ...)`, one 773-byte write issued just before xbps exits, so a reader
that stops at or before that write sees everything but the line. That reading
is an inference. The old fifo run is not preserved, so it was not reproduced.

Method, in `scripts/measure-piped-stdout.sh` (rootless podman, container root,
`ghcr.io/void-linux/void-glibc:latest`; `xbps-install -Syu xbps` leaves
`xbps-install --version` at 0.60.7; `strace` and `util-linux` installed for the
trace and for `script`). Each run removes `figlet`, empties the package cache
and installs it, so the closing line is
`1 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.`
`script` gives xbps a terminal.

| stdout is | closing line |
|---|---|
| the terminal | on the terminal |
| a pipe (`xbps-install ... \| cat > file`), drained to EOF | in the file |
| a file | in the file |
| a pipe, under `strace -f -e trace=write,writev` | in the file, and in a `write(1, ...)` in the trace |
| a file, under the same `strace` | in the file, and in a `write(1, ...)` in the trace |

With stdout a pipe or a file, the terminal on stderr received nothing: the
progress and download lines, the transaction lines and the closing line were
all written to fd 1, as the paragraph above says. The decision stands: the real
run keeps reading stderr only, because a stdout tee changes the live output,
not because a line is lost. No runtime behaviour changes.

### Fixtures

A hint case keeps both streams under fixed names: `.txt` is stderr, `.out` is
stdout, `.meta` the command and exit code. `not-installed` is back as a
fixture the hint test reads, `0607/not-installed.txt` (empty, the stderr) with
its `.out`, and `capture-fixtures.sh` now writes it in that shape. Two cases
were added from 0.59.1. The container phase of the script reproduced the two
0591 hint cases the corpus already held, `not-found` and
`xbps-unsigned-update`, byte for byte, six files:

- `0591/not-installed`: the second version for the `not-installed` entry.
- `0591/xbps-unsigned-update-upgrade`: `xbps-install -Syu`, exit 16, message on
  stdout.

A third came later, from the measurement under "Decision: the gap stays":

- `0591/xbps-unsigned-update-install`: `xbps-install -y figlet`, the real run
  `xbp install` issues, exit 16, message on stderr. With
  `0591/xbps-unsigned-update` (`-Sn`, the plan) it covers both runs of an
  install.

`TestCapturedCasesMatch` lists every captured case, and
`TestHintsMatchWhereXbpsWritesThem` moves each message to the other stream and
requires the hint to stay silent, except for `xbps-unsigned-update`.

## Consequences

- The `not-installed` entry is reachable: ADR-0003 #4's consequence is
  settled by this ADR.
- `stale-index` and `stale-index-upgrade` gained their `.out` from the host
  recapture of 2026-10-18; both are empty, so xbps writes those errors to
  stderr only. `lock-wait` still has no `.out`: the recapture found no
  upgrade pending, which its lock race needs, so that step stopped and its
  output was not committed. The test reads a missing `.out` as empty and does
  not claim stdout was observed to be empty. A host recapture while an upgrade
  is pending (`sudo XB_CAPTURE_MODE=host scripts/capture-fixtures.sh`)
  supplies it. `not-found` was the fourth; its `.out` came from an
  unprivileged dry run (see `testdata/hints/README.md`).
- 0.60.7 has no capture of `xbps-unsigned-update` on either stream: the
  refusal needs an outdated xbps, which only the 0.59.1 container has. The
  ticket's remark that both versions behave alike for `-Sn` rests on that
  container alone.
- Rev 4 stays frozen: FR-017's "stderr substring" reads as "a substring of the
  stream xbps writes it to" from here on.

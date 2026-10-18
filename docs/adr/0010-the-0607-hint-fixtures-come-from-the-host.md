# ADR-0010: The 0.60.7 hint fixtures come from the host

- Status: Accepted
- Date: 2026-10-18
- Relates to: NFR-004, ADR-0003, ADR-0006

## Context

NFR-004 asks for hint fixtures "reproduced in a `voidlinux/voidlinux`
container". The `hints/0607` corpus was not: `stale-index`,
`stale-index-upgrade` and `lock-wait` were captured on the maintainer's host
(Void with xbps 0.60.7), and the host recapture (`XB_CAPTURE_MODE=host`) is how
they are meant to be replaced, so the 0.60.7 hint corpus comes from the host.
The container only supplies the 0.59.1 hint cases and the 0.60.7 plan cases
that need an induced hold or unpacked state.

The deviation is a decision, not an accident, and the reasons are plain:

- The 0.60.7 captures predate the container script. They were taken on the
  host, where xbps was already 0.60.7, before the script kept stdout, and the
  `hints/0607` fixtures are what the `hints.go` patterns were first written
  from (ADR-0002).
- The host's xbps is the one users run, so a capture there is the closest
  observation of what xbpretty meets. The container needs an xbps upgrade
  before it matches, and its image state (cache, repository definition) is
  set up by the script, not by a real system.
- A container run reproduces the same stderr for these cases. Host mode was
  exercised in one for this ADR and `stale-index` and `lock-wait` came out
  with the wording the fixtures hold. That makes the container a valid check
  of the script, not the source of record.

## Decision

**The 0.60.7 hint fixtures come from a host capture, and NFR-004's wording is
read as "reproduced against a real xbps, in a container where the version
allows".** The 0.59.1 hint fixtures stay container captures. Rev 4 stays
frozen; this ADR carries the delta, as ADR-0003 and ADR-0006 do.

A host capture changes the machine it runs on, so the script's host mode is
bounded (`scripts/capture-fixtures.sh`):

- It refuses to run without root, and prints what it will change before it
  changes anything.
- It exits non-zero unless the host's xbps is 0.60.7, the version whose
  directory (`testdata/*/0607`) it would write.
  `XB_CAPTURE_ALLOW_XBPS_VERSION=1` overrides this, so a check inside a
  container whose xbps differs can get past it.
- It captures the install plan before it installs anything, for a package the
  host does not have, and stops when the plan has no `install` row.
- Its lock race reads no stdin and passes no `-y`. The foreground run waits
  for the lock and aborts at xbps's prompt. The holder is an upgrade whose
  stdin is a pipe that closes after a few seconds, so it also aborts and the
  host is never upgraded. The recorded command is `xbps-install -Su`, not the
  `-Syu` the first capture used.
- The index backup is a private `mktemp` file, restored by the existing trap.

## Consequences

- `hints/0607/lock-wait.meta` will record `xbps-install -Su` after the next
  recapture. The stderr line (`package database locked, waiting...`) and exit
  0 are the same as the `-Syu` capture; the test reads only those.
- A reader who needs a 0.60.7 hint case without a host can use a container
  whose xbps is upgraded to 0.60.7, writing to an output root outside
  `testdata`. That is how host mode was checked for this ADR; the override
  is only needed when the container's version differs.
- The corpus stays two-version (0.59.1 from the container, 0.60.7 from the
  host), which is what NFR-004 asks for.

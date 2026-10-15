# ADR-002: Measured xbps behaviour the spec assumed otherwise

- Status: Accepted
- Date: 2026-10-15
- Relates to: FR-007, FR-011, FR-017, NFR-004

Captures from xbps 0.60.7 (host Void box) and 0.59.1 (`voidlinux/voidlinux`
container) contradicted four assumptions in the rev 4 spec. The code follows
the measured behaviour; the spec text stands, and this ADR records why.

## 1. A stale index exits 0 on an upgrade, 2 on an install

Truncating the repodata produces, for `xbps-install -Sn figlet`:

```
exit 2
ERROR: failed to open repodata: /var/db/xbps/.../x86_64-repodata: Invalid or incomplete multibyte or wide character
ERROR: Package 'figlet' not found in repository pool.
```

but for `xbps-install -Sun`:

```
exit 0
ERROR: failed to open repodata: ... (same line)
```

The upgrade gets an empty plan and success, indistinguishable from "Nothing
to do." by exit code alone. The hint table therefore matches on both exit
codes 0 and 2 for `stale-index`, so the warning is never lost — this is the
one place where a hint fires on a successful exit, because the error text is
real either way.

## 2. Unprivileged `-Sn` works; only `-S` needs root

FR-007 assumed the whole sync flag had to be dropped for an unprivileged
dry run. Measured: `xbps-install -Sn htop` as a non-root user exits 0 and
prints the full plan from the cached index, while `xbps-install -S` fails
with `Permission denied`. The failure is the *fetch*, not the flag.

The spec's rule was kept as written (unprivileged plans omit `-S` and print
the stale-index warning), per the decision recorded on the map: it is the
conservative reading, and it matches AC-007 exactly. The warning now fires
because the index cannot be *refreshed*, not because it cannot be read.

## 3. `ignorepkg` leaves no error line

FR-017 lists "package ignored via `ignorepkg` in xbps config" as a hint case,
but xbps prints nothing for it: the package is silently dropped from the plan,
exiting 0. Measured with `ignorepkg=sl` in `/etc/xbps.d/`: the plan for `sl`
comes back empty with no stderr at all, and `xbps-install -Sy sl` reports
`Package 'sl' already installed.`

Because FR-017 fires only on an exit code plus a stderr substring, and there
is no stderr, there is nothing to match. No hint entry exists for this case,
and none is guessed. A silently skipped package surfaces instead as
`Nothing to do.` (FR-015).

## 4. A locked database blocks rather than fails

FR-017 lists a lock-held failure. Racing two live transactions captures:

```
WARNING: package database locked, waiting...
```

and the second process *waits* for the first instead of exiting. There is no
failure exit code to pass through. The hint table records exit 0 with that
substring, so if xbpretty ever surfaces the warning it explains it; today the
wait itself is invisible because xbps blocks inside the streamed child, and
Ctrl-C reaches it through FR-014's signal forwarding.

## Fixture provenance

Every string above is checked in verbatim under `testdata/hints/`, with the
command and exit code in a sibling `.meta`. The test
`TestCapturedCasesMatch` fails if xbps's wording ever stops matching a
pattern, which is NFR-004's promise.

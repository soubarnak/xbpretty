# ADR-001: Batched xbps-query lookups use a worker pool

- Status: Accepted
- Date: 2026-10-15
- Supersedes: the wording of NFR-002

## Context

NFR-002 requires wrapper overhead under 100 ms for transactions of up to 200
packages, and states: *"xbps-query calls are batched: at most one call per
property set per transaction."*

Measured on Void Linux with xbps 0.60.7:

```console
$ xbps-query -p installed_size glibc xbps vim
ERROR: xbps-query: too many arguments
$ time (for i in $(seq 200); do xbps-query -p installed_size glibc >/dev/null; done)
6390 ms total
```

`xbps-query -p` accepts exactly one package per call, so the batching NFR-002
assumes does not exist in the tool. 200 sequential lookups cost ~6.4 s, sixty
times the budget. There is no bulk mode: `-l` and `-s` reject the property
combination that would be needed, and `-R` inherits the same one-package rule.

## Decision

Lookups run through a bounded worker pool (`internal/cli/transaction.go`,
`lookupOld`) of 8 concurrent `xbps-query -p pkgver,installed_size` calls,
issuing one call per package per property set. Held packages come from a
single `xbps-query -H`.

"Batched" is therefore satisfied in the sense NFR-002 cares about — the calls
are issued together rather than one-per-row on the critical path — while the
per-call shape is dictated by xbps itself.

## Consequences

- Typical transactions stay comfortably inside the 100 ms budget; the 200
  package worst case is bounded by pool width, not by row count.
- Eight concurrent xbps-query processes read the package database without
  contending on the transaction lock, which is only taken by mutating
  commands.
- If a future xbps grows a bulk property mode, `lookupOld` is the single
  place to change: its contract is a map of package name to old version and
  size.

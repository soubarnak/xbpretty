# Lookup fixtures

Every `xbps-query` answer the CLI reads a decision out of, captured byte for
byte, with the command and exit code in the `.meta` beside it.

| Directory | xbps version | Source |
|---|---|---|
| `0607/` | 0.60.7_1 | Host Void Linux box, except `hold.txt`, which needs a package on hold and comes from a container |
| `0591/` | 0.59.1_5 | `voidlinux/voidlinux:latest` container |

| File | Command | What it pins |
|---|---|---|
| `<pkg>.txt` | `xbps-query -p pkgver,installed_size <pkg>` | the old version and installed size, on two lines: the pkgver first, the size second |
| `hold.txt` | `xbps-query -H` | one pkgver per held package, and nothing at all when none are |
| `missing.txt` | `xbps-query -p pkgver,installed_size this-package-does-not-exist-xyz` | an empty answer with exit 2 for a package that is not installed |

The first line of a `<pkg>.txt` is a **pkgver**, not a bare version:
`xbps-0.60.7_1`, not `0.60.7_1`. The summary shows an upgrade as
`old → new` with both versions bare (FR-011), so the name comes back off the
pkgver — `parseOld` in `internal/cli/transaction.go`, pinned by
`TestParseOldMatchesCapturedLookups`.

`hold.txt` names the **installed** pkgver, while a plan row for the same held
package names the version the repository offers, so the two only agree while
no upgrade is pending.

Regenerate with `scripts/capture-fixtures.sh`; the container writes the 0.59.1
set and the hold list, the host run writes the rest.

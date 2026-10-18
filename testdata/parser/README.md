# Parser fixtures

These are real dry-run transaction plans captured from xbps, byte for byte.
They are the contract `internal/xbps.ParsePlan` is written against: the
fixtures define the format, and a captured case that stops parsing is a
failure, not a silent skip.

| Directory | xbps version | Source |
|---|---|---|
| `0607/` | 0.60.7_1 | Host Void Linux box, plus the state-dependent cases (`hold`, `configure`, `nothing`) from a `voidlinux/voidlinux` container running 0.60.7 |
| `0591/` | 0.59.1_5 | `voidlinux/voidlinux:latest` container, captured before its xbps was upgraded |

Every capture was taken with `LC_ALL=C`, against the one repository
(`repo-default.voidlinux.org/current`) the fixtures are read against.

| File | Command that produced it |
|---|---|
| `0607/install.txt` | `xbps-install -Sn figlet` |
| `0607/upgrade.txt` | `xbps-install -Sun` |
| `0607/remove.txt` | `xbps-remove -n wget` |
| `0607/autoremove.txt` | `xbps-remove -on` |
| `0607/nothing.txt` | `xbps-install -Sun`, on a system with nothing to upgrade |
| `0607/reinstall.txt` | `xbps-install -Sfn curl` |
| `0607/hold.txt` | `xbps-install -Sun`, with a package on hold |
| `0607/configure.txt` | `xbps-install -Sun`, with a package left configurable |
| `0591/upgrade.txt` | `xbps-install -Sun` |

The `.meta` beside each plan records the command, its exit code, and whatever
state the capture had to induce first. Four of them are reconstructed, not
captured: `0607/install`, `reinstall`, `remove` and `upgrade` were taken on the
host before the script wrote a `.meta` or kept the other stream. Their `.meta`
was written afterwards from the command table above, says so in a `NOTE:` line,
and has no `.err` beside it, because stderr was never observed for them. The
exit code is inferred from the plan being complete. Nothing reads a `.meta`
here, and the plans themselves are untouched. A host recapture would replace
them with new plans and move the figures `plan_test.go` pins, so it waits for
a reason of its own. Two of the actions a plan prints only appear in induced
state:

- **`hold`**: 0.60.7 prints a package on hold as `<pkgver> hold ...`. 0.59.1
  drops it from the plan instead of naming it, so `0591/` has no hold plan —
  `testdata/lookups/0591/hold.txt` carries what its `xbps-query -H` says.
- **`configure`**: a package killed between unpacking and configuring prints
  as `<pkgver> configure ...`. A package with an upgrade pending prints
  `update` in its place, so the capture interrupts a transaction over packages
  the plan does not otherwise touch. Only 0.60.7 can produce one: 0.59.1
  refuses every transaction until xbps itself is updated, which is the case
  `testdata/hints/0591/xbps-unsigned-update.txt` captures.

`0607/nothing.txt` is empty for the same reason: an empty plan needs a system
with nothing pending. The container supplied it by bringing its own packages
up to date first, so no host had to be upgraded for the capture.

Two columns of the format are optional: `base-container-0.3_3` (in
`0591/upgrade.txt`) carries an installed size but no download size, and
`removed-packages` (in `0607/upgrade.txt`) reports an installed size of `0`.
Both are exercised by the tests.

Regenerate with `scripts/capture-fixtures.sh` for the container captures and
`sudo XB_CAPTURE_MODE=host scripts/capture-fixtures.sh` for the host's own. A
regenerated capture is a new capture: the expectations pinned to it in
`plan_test.go` have to move with it.

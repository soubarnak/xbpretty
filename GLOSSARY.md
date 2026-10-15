# xbpretty

A friendlier front end for the xbps package manager on Void Linux: it previews a
transaction, prints a summary, asks once, and then runs xbps.

## Language

**Invoking user**:
The person who launched xbpretty. Under sudo or doas this is the `SUDO_USER` or
`DOAS_USER` account, not root: their home directory supplies the config file and
receives the transaction log. Distinct from the effective user, which is root for
every real execution.
_Avoid_: real user, caller, owner.

**Plan**:
The dry-run output of xbps for one transaction, parsed into rows of package
version, action, architecture, repository and sizes. A plan changes nothing on
disk.
_Avoid_: preview, simulation, diff.

**Summary**:
What xbpretty prints from a plan: the Install, Upgrade, Remove and Hold groups,
followed by the counts-and-sizes footer. It goes to stdout; every other line goes
to stderr.
_Avoid_: report, preview, output.

**Held package**:
A package the user has pinned on hold in xbps. The summary gives held packages
their own group rather than hiding them.
_Avoid_: pinned, frozen, locked.

**Transaction log**:
The append-only file listing one line per successful real execution, in the
invoking user's state directory. It records transactions; it cannot replay them.

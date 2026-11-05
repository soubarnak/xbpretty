# xbpretty

A friendlier front end for the xbps package manager on Void Linux: it previews a
transaction, prints a summary, asks once, and then runs xbps. Rev 6 adds
replayable history, parallel predownload, Void mirror fetch, discovery with
machine output, and a nala-faithful full-screen TUI.

## Language

**Invoking user**:
The person who launched xbpretty. Under sudo or doas this is the `SUDO_USER` or
`DOAS_USER` account, not root: their passwd home supplies the config file and
receives the transaction log. `XDG_CONFIG_HOME`/`XDG_STATE_HOME` are ignored
when an invoking user is present. Distinct from the effective user, which is
root for every real execution.
_Avoid_: real user, caller, owner.

**Plan**:
The dry-run output of xbps for one transaction, parsed into rows of package
version, action, architecture, repository and sizes. A plan changes nothing on
disk.
_Avoid_: preview, simulation, diff.

**Summary**:
What xbpretty prints from a plan: the operation sections in fixed order,
followed by the counts-and-sizes footer. It goes to stdout; every live or
diagnostic line goes to stderr.
_Avoid_: report, preview, output.

**Section**:
One operation's label, `=` rule and table inside the inline summary. The order
is Remove, AutoRemove, Purge, AutoPurge, Install, ReInstall, Upgrade,
Downgrade, Configure, Held; Downgrade never appears on xbps.
_Avoid_: group, block, card.

**Section rule**:
The ASCII `=` line that opens and closes every summary section and the
`Summary` footer, exactly as wide as the terminal.
_Avoid_: border, separator, divider.

**Theme role**:
One of the nine named styles (`Primary`, `Secondary`, `Highlight`, `Regular`,
`ProgressFilled`, `ProgressUnfilled`, `Notice`, `Warning`, `Error`) that colour
every surface.
_Avoid_: colour, style name, palette slot.

**Theme style**:
A `{ fg, bg?, modifier? }` value for one theme role. `fg` is required; an
omitted `modifier` means BOLD; `modifier = ""` means none.
_Avoid_: colour spec, role value.

**Held package**:
A package the user has pinned on hold in xbps. The summary gives held packages
their own group rather than hiding them; the only reason is `Manual hold`.
_Avoid_: pinned, frozen, locked.

**Progress view**:
One of the progress widget's fixed-height layouts — Download, Install,
Remove, Update, MirrorScore.
_Avoid_: progress screen, phase.

**Progress widget**:
The rounded, width-capped live box drawn on stderr during a real execution.
_Avoid_: progress bar, spinner, box.

**Plain progress**:
The single 40-cell stderr line used when the widget is unavailable but stderr
is a terminal.
_Avoid_: fallback bar, ASCII progress.

**Durable output**:
The stdout bytes a pipeline may consume: the summary, `Nothing to do.`,
`update`'s count/list line, `download`'s block, discovery rows/JSON, the fetch
table and its written-file lines, `--version`, help and completions. Everything
else is stderr. In `Tui` mode the transaction summary screen is the one stated
exception, and stdout is empty for that transaction.
_Avoid_: stdout output, printable output.

**Transaction log**:
The append-only audit file listing one line per successful real execution,
naming only the packages the transaction changed (ADR-0008), in the invoking
user's state directory. Its command vocabulary now includes `purge`,
`autopurge`, `undo` and `redo`. It records transactions; it cannot replay them
and is never imported into history.
_Avoid_: history, journal, audit trail.

**History store**:
The system-wide, root-owned record of successful transactions, one JSON entry
per transaction, at `/var/lib/xbpretty/history`.
_Avoid_: database, journal, log.

**History entry**:
The before/after package snapshot of one successful transaction, with who ran
it and what they asked for. Schema 1.
_Avoid_: record, snapshot file.

**Replay**:
Re-running a history entry's `before` state (undo) or `after` state (redo) as a
new transaction, pinned to the recorded versions.
_Avoid_: rollback, revert, restore.

**Undo / Redo**:
The two replay directions. Each writes a new history entry; undo restores the
before state, redo restores the after state.
_Avoid_: rollback, forward, revert.

**Predownload**:
Filling the xbps cache with a transaction plan's archives before xbps runs, in
parallel, without changing the plan.
_Avoid_: preload, prefetch.

**Download set**:
The plan rows whose archives will actually be fetched: actions install, update,
reinstall or download, a remote repository and a positive download size.
Removals, holds, configures and local-repository rows are excluded.
_Avoid_: download plan, fetch list.

**Cache hit**:
A cached archive whose SHA-256 matches the plan's repository metadata, with its
signature present.
_Avoid_: cached, warm cache.

**Metadata hash**:
The `filename-sha256` value in a repository's index, xbps's only per-archive
content hash.
_Avoid_: checksum, digest.

**Download destination**:
The current directory for `xbp download`, the xbps cache for predownload and
`--download-only`.
_Avoid_: output dir, target.

**Partial file**:
The `.part` file written before an atomic rename; never a usable archive.
_Avoid_: temp file, incomplete download.

**Download-only**:
The `-d/--download-only` stop: predownload the plan into the xbps cache, then
stop without a pkgdb write.
_Avoid_: dry download, prefetch mode.

**Mirror**:
A Void package-server base URL published in `mirrors.lst`, carrying region,
location and tier; xbps uses it only as a `repository=` base.
_Avoid_: server, host, source.

**Mirror score**:
The wall-clock milliseconds to fetch a 256 KiB sample of a mirror's
`<arch>-repodata`; lower is faster.
_Avoid_: rank, speed, latency.

**Chosen mirror**:
The single mirror written into every managed `repository=` line.
_Avoid_: selected mirror, best mirror.

**Repository configuration**:
The `*-repository-*.conf` files under `/etc/xbps.d` that mask
`/usr/share/xbps.d` and define xbps's `repository=` list.
_Avoid_: sources list, repo config.

**Fetch manifest**:
The record at `/var/lib/xbpretty/fetch.json` of the files `xbp fetch` wrote and
their backups, used by `xbp clean --fetch`.
_Avoid_: backup file, state file.

**Embedded mirror snapshot**:
The copy of Void's mirror list compiled into xbpretty and used only when the
live list cannot be fetched; it carries the date it was taken.
_Avoid_: fallback list, cached list.

**Variant**:
One package version as offered by one repository. A name can have several.
_Avoid_: version, release, build.

**Repository precedence**:
The order repositories appear in `xbps-query -L`. The first repository offering
a name wins unless `bestmatching=true`.
_Avoid_: priority, pin, weight.

**Virtual name**:
A `cmd:…`, `shlib:…` or explicit `provides` entry. It is not a package.
_Avoid_: virtual package, metapackage.

**Candidate**:
The version xbps resolves for a name.
_Avoid_: latest, chosen version.

**Orphan**:
An automatic-install package with no remaining reverse dependencies; printed as
`Auto-Removable`.
_Avoid_: unused package, leftover.

**Notice**:
A non-fatal stderr line for a token that matched nothing.
_Avoid_: warning, info line.

**N/A**:
A nala surface xbps cannot express. A flag or config N/A is accepted by the
parser, refused with one stderr line and exit 2, and never silently ignored; a
visual/TUI/behaviour N/A never renders and names what happens instead.
_Avoid_: unsupported, missing, gap.

**Sync step**:
A standalone `xbps-install -S` index refresh before a dry run. It is never part
of transaction argv.
_Avoid_: early update, refresh.

**Config layer**:
One file in the read order: system, user, or `--config`. Later layers win per
key; theme roles merge role-by-role.
_Avoid_: config file, profile.

**Privileged config key**:
A `[Xbps]` binary path that can make root execute a file.
_Avoid_: dangerous key, path key.

**Trusted config file**:
A regular file owned by root with no group/world write bit, checked on the
opened descriptor.
_Avoid_: safe config, root config.

**Legacy key**:
A rev-5 flat config key accepted with a warning for rev 6 only; rev 7 rejects
it.
_Avoid_: old key, deprecated key.

**Migration window**:
The single spec revision during which legacy keys are accepted; the next
revision rejects them.
_Avoid_: deprecation period, grace period.

**System config layer**:
The optional `/etc/xbpretty/config.toml`, read before the user layer and
trusted only when root-owned and not group/world-writable. Absent by default;
the package does not create it.
_Avoid_: global config, etc config.

**Pre-rev-6 transaction**:
A transaction recorded only in the line log, which the history store does not
contain and cannot replay.
_Avoid_: legacy history, old entry.

**Full-screen surface**:
A view that takes the alternate screen and the keyboard. There are exactly
three: transaction summary, history detail, fetch picker.
_Avoid_: TUI screen, modal, app.

**Transaction summary screen**:
The operation-tabbed full-screen view of a plan, shown only in `Tui` mode with
the terminal, stdin, UTF-8, root and flag gates met.
_Avoid_: TUI summary, confirm screen.

**History detail screen**:
The same view, read-only, over one history entry.
_Avoid_: history TUI, replay screen.

**Fetch picker**:
The full-screen single-choice mirror list shown by `fetch` unless selection is
automatic.
_Avoid_: mirror menu, selector.

**Inline progress viewport**:
The bottom-anchored, width-capped progress box that persists across every mode
except `Plain` and never enters the alternate screen.
_Avoid_: progress pane, live box.

**Alias name**:
One of the six extra names the package installs (`xbp`, `xbp-install`,
`xbp-remove`, `xbp-upgrade`, `xbp-autoremove`, `xbp-update`) that dispatch to
the same binary; the set is closed.
_Avoid_: symlink, shortcut, command alias.

**Parity matrix**:
The indexed record of every user-visible nala surface and how xbpretty treats
it on xbps. Two TSVs are normative; Appendix A and `docs/parity/matrix.md` are
generated.
_Avoid_: parity table, feature matrix, gap list.

**Disposition**:
The single class a parity row carries — equivalent, adaptation, extension,
N/A, or defect-not-copied.
_Avoid_: status, verdict, classification.

**Tested N/A**:
An N/A parity row that names the exact user-visible refusal or absence, cites
the spec criterion requiring it, and (once implemented) the test asserting it.
_Avoid_: documented gap, accepted limitation, known missing.

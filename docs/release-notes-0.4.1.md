# xbpretty 0.4.1

xbpretty 0.4.1 is the first bug-fix release on rev 6. It fixes the sync step so
`update` and the upgrade family look the same as nala's `Updating Package
Lists`, removes two hangs and a SIGPIPE that could leave a transaction half
applied, closes the sudo log-path writes, and corrects a set of discovery,
history and full-screen details. The rev-6 contract and the config format are
unchanged. It builds for all nine Void Linux architecture variants from one
static binary.

The rev 6 feature set is unchanged: one binary with seven names (`xbpretty` and
the six aliases `xbp`, `xbp-install`, `xbp-remove`, `xbp-upgrade`,
`xbp-autoremove` and `xbp-update`), history with undo and redo, predownload and
fetch, discovery with machine output, and the optional full-screen TUI.

## Install

Add the signed repository once, then install and update like any other package:

```sh
echo 'repository=https://github.com/soubarnak/xbpretty/releases/latest/download' | sudo tee /etc/xbps.d/20-xbpretty.conf
sudo xbps-install -S xbpretty
```

The first repository sync asks you to accept the repository key. Check that the
fingerprint is exactly:

```
fa:d1:00:32:ce:b1:b9:fd:73:05:05:76:75:54:37:25
```

Do not accept a different fingerprint. Later updates arrive through
`sudo xbps-install -Su` or `sudo xbp upgrade`.

## Behaviour changes to know about

- **The sync step looks the same everywhere.** `xbp update`, `xbp upgrade` and
  the other upgrade-family verbs draw the `Updating Package Lists` view on a
  live terminal. It shows one line per repository (`No Change:`, `Updated:`,
  `Error:`), then `Fetched <size> in <time> (<rate>/s)` or `Nothing to fetch`.
  Scripts and non-terminal runs still get xbps's own sync output unchanged.
- **The upgrade count line goes to stderr.** `xbp upgrade` prints `N packages
  can be upgraded` on stderr after the sync, so `xbp upgrade --dry-run | cat`
  prints only the summary. `xbp update` keeps its count on stdout.
- **Boolean long flags reject a value.** `--assume-yes=false`, `--dry-run=no`
  and `--all=0` exit 2 with `does not take a value` and run no child. Before,
  they were accepted and treated as set, so `--assume-yes=false` still
  confirmed a transaction.
- **Discovery arch suffixes are checked.** `name:arch` splits only when the
  suffix is a known architecture, so `pc:zlib-ng` is one name. `[!x]` negates a
  glob, and an unbalanced `{` is a usage error.
- **Completion offers accepted no-op flags.** `-A/--all-arches`,
  `--no-fix-broken`, `--no-install-recommends`, `--no-install-suggests`,
  `--full`, `--no-full` and `--safe` now complete, as `--help` lists them.

## Fixes

### Sync and update

- The upgrade family's sync uses the update view instead of raw xbps text.
- The Update view is seven rows, and its repository total comes from
  `xbps-query -L`, so it counts every remote repository.
- A new-key prompt from xbps is visible and can be answered.
- A failed sync clears the view, prints the captured output and the hint, and
  exits with the sync's code.

### Progress accuracy

- A child line longer than 64 KiB no longer hangs the transaction in terminal
  mode (critical).
- A closed or failing stderr no longer kills xbps with SIGPIPE in the middle of
  a transaction (critical).
- Package messages and child warnings appear above the live box instead of
  being dropped on success.
- The Install view no longer double-counts an update, and a mixed install and
  remove plan stays in the Install view.
- Predownload completions reach the commit view, so it does not restart at 0.
- A download retry no longer over-counts, and a failed file leaves the position.
- SIGTERM during predownload exits 143, not 130.
- If the pseudo-terminal cannot open, the widget still advances.
- Reinstall no longer counts the full installed size as new disk use.
- The cursor is hidden while the inline box is drawn and is restored on every
  exit path.

### Full-screen interface

- A bracketed paste split by a pause no longer leaks keys. A pasted `y` cannot
  confirm a root transaction (critical).
- The scrollbar thumb follows the scroll offset, and moving the selection up
  after `End` moves the selection, not the view.
- Squeezed 60-column layouts keep column gaps, and the confirm hint stays in the
  help block. The too-small notice stays inside the border.
- The custom `Regular` background covers the whole frame.
- Horizontal wheel events no longer scroll vertically.
- Emoji such as 🚀, 🛑 and 🟢 measure as two cells.
- Column headers wrap the same with and without colour.

### History and replay

- `redo` after `undo` no longer warns about every package.
- `undo` takes the repository and size of the version it restores, not the
  after-state values, so a larger old archive is no longer rejected by
  predownload.
- A held reason reads `Manual hold`, not `ManualHold`.
- The history list header reads `Date and Time`.

### Discovery

- An installed package that is held, or newer than the repository, keeps its
  installed state in `list`, in text and in `-m` output.
- `--description` prints the description alone, without the repository path.
- `policy <virtual>` reports the installed provider, not `Installed: none`.
- `show` accepts pkgver, pattern and virtual tokens.
- Multi-variant names are resolved only after the token filter, so `list <name>`
  no longer makes one slow query per name.

### Fetch and downloads

- A second `fetch` keeps the `created` state, so `clean --fetch` removes the
  file it created instead of restoring fetch-written content.
- On musl, `fetch` probes the musl repodata name, using `xbps-uhelper arch` and
  `XBPS_ARCH`.
- A reverted fetch removes the backups it wrote, and a later fetch never trusts
  a stale backup.
- `xbp download` stages the signature and publishes it only after the archive
  verifies, so a failed archive leaves no `.sig2`.

### Security hardening

- Under `sudo` or `doas`, the transaction log refuses a symlink or hard link as
  the log file, and a symlinked component of the state directory. It opens the
  log without following links and chowns it through that descriptor.
- Directories that root creates for the invoking user are owned by that user,
  not by root.
- Boolean long flags reject a value (see above).

### Config and completion

- A `./path` value is reported as an invalid path, not as a float.
- Raw control characters inside TOML strings are rejected with a clear message.
- Completion offers the accepted no-op flags (see above).

### Tests

- The integration suite no longer reads the host's `/var/lib/xbpretty/history`.
  Every child environment sets the history, state, config and home directories
  to the test's temporary directory.

## Known limitations

- **No `ignorepkg` hint.** A package listed in an `ignorepkg=` line gives no
  message from xbps 0.60.7, so there is nothing to match. The hint from FR-073
  is not implemented.
- **History at 60 columns.** The history table can still touch neighbouring
  columns at exactly 60×18. The 80-column layout is unaffected.
- **Double blank line between full-screen surfaces.** The cursor fix is in, but
  the extra blank line between the predownload and commit surfaces is not
  changed.
- **Sudo log-path check.** The component walk that refuses symlinks under
  `sudo`/`doas` checks the path before it opens it. A user who can rename the
  path in that window could still redirect the write. The no-follow open on the
  final component stops a plain symlink.
- **Fetched size precision.** The `Fetched` total sums xbps's completion sizes,
  which xbps prints with no decimals (for example `1854KB`). The total is
  KiB-precise.
- **A sync where every repository fails.** xbps exits 0 in that case. xbpretty
  prints the `Error:` lines and no completion line, and the run continues to
  the plan against the cached index.
- **`update_show_packages` is plain text.** The package list is
  `name old -> new` with no colour roles.
- **Security review pending.** The `[Xbps]` path trust check and the release
  signing pipeline have not had a dedicated security review. Run one before
  relying on this release for root-owned systems.

## Migrating from 0.4.0

No config change is needed. Rev 6 still accepts the rev-5 flat config keys with
a warning and never rewrites your config file. The complete key map, retired
keys, behaviour changes and rollback steps are in the migration guide:

- [Migrating from rev 5](https://github.com/soubarnak/xbpretty/blob/main/docs/migration-rev6.md)
- [Canonical rev 6 config example](https://github.com/soubarnak/xbpretty/blob/main/docs/config.toml.example)

Both files are installed under `/usr/share/doc/xbpretty/`. Rev 7 (0.5.0) is the
first release that rejects the rev-5 keys.

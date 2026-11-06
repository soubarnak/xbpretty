<p align="center">
  <img src="https://bongbetic.com/branding/bongbetic-logo-lockup.png" alt="Bongbetic" width="320">
</p>

# xbpretty

> **Rev 6 ships as xbpretty 0.4.0.** The specification targets full parity
> with nala v0.18.2 and adds history/undo/redo, predownload, fetch, discovery
> with machine output and the full-screen TUI. This README is the 0.4.0 user
> guide; the contract is [`xbpretty-spec.md`](xbpretty-spec.md), with the
> matrix in [`docs/parity/matrix.md`](docs/parity/matrix.md), the migration
> guide in [`docs/migration-rev6.md`](docs/migration-rev6.md) and the decisions
> in [`docs/adr/`](docs/adr/). The six alias names and the superset extensions
> are unchanged.

A modern, friendly front end for [`xbps`](https://github.com/void-linux/xbps), the Void Linux package manager, inspired by [Nala](https://gitlab.com/volian/nala).

`xbpretty` previews transactions as nala's borderless `=`-ruled tables, highlights package version diffs, asks for confirmation before acting, tracks downloads and installations in a live progress widget, records replayable transaction history, and explains common xbps errors with actionable hints. It wraps `xbps-install`, `xbps-remove`, and `xbps-query`; it does not replace them.

Built in pure Go with zero external dependencies, `xbpretty` compiles to a single static binary (`CGO_ENABLED=0`), starts in under 2ms, and remains 100% compatible with offline `xbps-src` packaging.

```
 Upgrade
================================================================================
 Package                          Old Version  New Version        Size
 curl                             8.21.0_1     8.22.0_1        274 KiB
 libcurl                          8.21.0_1     8.22.0_1        312 KiB

 Summary
================================================================================
 2 upgrade
 Total download size: 587 KiB
 Disk space required: 18 KiB
```

---

## Features

- **Nala-Style Inline Summary**: Borderless `=`-ruled tables in the fixed operation order (*Remove*, *AutoRemove*, *Purge*, *AutoPurge*, *Install*, *ReInstall*, *Upgrade*, *Configure*, *Held*), with the counts-and-sizes footer.
- **Version Delta Highlighting**: Displays version transitions (`old → new`) with only the changed components highlighted (`Notice`).
- **Live Progress Widget**: Real-time Download (7-row), Install, Remove, Update and MirrorScore (6-row) views fed by a hybrid PTY/pipe engine. `[Ui] mode = "Plain"` is the supported off switch.
- **Full-Screen TUI**: The S1 transaction summary, S2 history detail, S3 mirror picker and package-info view, all gated on `[Ui] mode = "Tui"` and the 60×18 entry floor.
- **Zero Dependencies & Blazing Fast**: Pure Go standard library VT100/ANSI rendering engine. No third-party TUI frameworks, sub-2ms startup, and offline packaging friendly.
- **Script & Pipeline Friendly**: stdout carries durable output only; the prompt, widget, hints and forwarded child output go to stderr. `Auto` colour is per stream, and `NO_COLOR` is honoured.
- **Replayable History**: Every successful change is recorded under `/var/lib/xbpretty/history` and can be inspected, undone or redone with `xbp history`, `xbp undo` and `xbp redo`.
- **Discovery with Machine Output**: `xbp list`, `search`, `show` and `policy` are read-only, root-free and offline, with NDJSON schema 1 via `-m`.
- **Parallel Predownload**: Real transactions fill the xbps cache in parallel before xbps runs, falling back cleanly on failure; `-d` stops after the cache fill, `xbp download` writes archives and signatures to the current directory, and `upgrade --print-uris` prints the plan's URLs as JSON.
- **Mirror Fetch**: `xbp fetch` benchmarks Void mirrors with a ranged `repodata` probe, writes exactly one mirror with backups and a manifest, validates it with `xbps-install -S` and reverts on failure; `clean --fetch` restores the previous configuration.
- **Humanized Error Hints**: Translates cryptic xbps errors (missing packages, stale indexes, locked databases, and reverse dependencies via `xbps-query -X`) into clear, actionable advice.
- **Transaction Log**: Appends every successful change to the append-only transaction log, queryable at any time with `xbp log`.
- **Kernel Reboot Reminders**: Automatically prompts a reboot reminder whenever an upgrade touches the Linux kernel or glibc.
- **Sudo / Doas Aware**: Resolves `SUDO_USER` or `DOAS_USER` when run with root privileges so your user configuration and transaction logs remain in your home directory with correct file ownership.

---

## Visual Showcase

### Live Execution Progress

When running interactively, child `xbps` telemetry is captured and rendered into an in-place rounded widget. Download is seven rows; the other views are six.

```
╭── Downloading... ────────────────────────────────────────────────────────────╮
│  Total Packages: 2/5                                                         │
│  Downloading:    firefox-130.0_1                                             │
│  ETA: 0:00:03  [━━━━━━━━━━━━────────] 61.2% • 42.1 MiB/68.8 MiB • 8.4 MiB/s   │
╰──────────────────────────────────────────────────────────────────────────────╯
```

The summary is nala's inline surface, not a box: `=` rules at the terminal width, borderless tables and a colour-independent footer.

---

## Install

### Binary Package (Signed XBPS Repository)

`xbpretty` is packaged and distributed via a signed XBPS repository for the Void architectures `x86_64` and `i686` (glibc), plus `x86_64`, `aarch64`, `armv7l` and `armv6l` in both glibc and musl variants.

Add the repository configuration:

```sh
echo 'repository=https://github.com/soubarnak/xbpretty/releases/latest/download' | sudo tee /etc/xbps.d/20-xbpretty.conf
sudo xbps-install -S xbpretty
```

On the first repository sync, verify that the key fingerprint matches:

```
fa:d1:00:32:ce:b1:b9:fd:73:05:05:76:75:54:37:25
```

Do not accept a different fingerprint. Subsequent updates arrive automatically through `sudo xbps-install -Su` or `sudo xbp upgrade`.

### Migrating from rev 5

Rev 6 (xbpretty 0.4.0) accepts the rev-5 flat config keys with a warning during
the rev-6 window and never rewrites your config file. The key map, retired
keys, behaviour changes, verification and rollback steps are in
[`docs/migration-rev6.md`](docs/migration-rev6.md), with the canonical example
in [`docs/config.toml.example`](docs/config.toml.example). Both install under
`/usr/share/doc/xbpretty/`. Rev 7 (0.5.0) removes the rev-5 keys.

### From Source

Requires Go 1.25. Release builds pin `GOTOOLCHAIN=go1.25.5`: that toolchain
produces a binary under the 8,000,000-byte budget, while newer toolchains can
cross it. Do not raise the budget to match a newer compiler.

```sh
make build        # Compiles static binary ./xbpretty
make test         # Runs test suite
```

To create convenient symlinks for your user (`xbp`, `xbp-install`, etc.):

```sh
./xbpretty link ~/.local/bin
```

---

## Usage

One binary, seven names: the `xbpretty` binary plus six aliases. `xbp` is the
primary shorthand; the five `xbp-<verb>` commands are symlinks that dispatch on
`argv[0]` and fix their verb. No other `xbp-*` name exists.

### Transactions

Every transaction verb (except `update`) prints the plan, then the summary, then asks once. The
dry run always uses xbps's `-n`; the real run always appends `-y`; the index
sync is a separate `xbps-install -S` step and never appears inside a
transaction's argv.

| Command | Alias | Dry run | Real run |
|---|---|---|---|
| `xbp install PKG...` | `xbp-install` | `xbps-install -n PKG...` | `xbps-install -y PKG...` |
| `xbp remove PKG...` | `xbp-remove` | `xbps-remove -n PKG...` | `xbps-remove -y PKG...` |
| `xbp purge PKG...` | | `xbps-remove -n -f PKG...` | `xbps-remove -y -f PKG...` |
| `xbp autoremove` | `xbp-autoremove` | `xbps-remove -n -o` | `xbps-remove -y -o` |
| `xbp autopurge` | | `xbps-remove -n -o -f` | `xbps-remove -y -o -f` |
| `xbp upgrade` | `xbp-upgrade` | `xbps-install -n -u` | `xbps-install -y -u` |
| `xbp full-upgrade` / `xbp safe-upgrade` | | same as `upgrade` | same as `upgrade` |
| `xbp update` | `xbp-update` | `xbps-install -S`, then `xbps-install -n -u` to count | not applicable |
| `xbp download PKG...` | | xbpretty-owned | xbpretty-owned |
| `xbp clean` | | `xbps-remove -O` | `xbps-remove -O` |

Notes:

- `uninstall` is an alias of `remove`; `dist-upgrade` and `full-upgrade` are
  aliases of each other and behave exactly like `upgrade`. `xbps -u` never
  removes packages, so `--full`, `--no-full` and `--safe` change no argv.
- `install` does not refresh the index by default; `--update` adds the sync
  step. The upgrade family syncs by default.
- `remove` reports and filters packages that are not installed; if nothing
  remains it prints `Nothing to do.` and exits 0.
- Implicit autoremove runs after a successful install/remove/purge/upgrade when
  `[Nala] auto_remove = true`; `--no-auto-remove` disables it and
  `--auto-remove` forces it. It never runs for `-y` unless forced explicitly.
- `clean` runs `xbps-remove -O`. `clean --lists` deletes cached `*-repodata`
  files under `/var/db/xbps/*/` and keeps the repository keys; `clean --fetch`
  restores the repository configuration written by `fetch`.

```sh
sudo xbp install figlet vim
xbp upgrade --dry-run                 # preview, no prompt
sudo xbp upgrade                      # sync, preview, confirm, upgrade
sudo xbp remove --purge nano          # purge configuration too
xbp autoremove --dry-run              # show orphans without changing anything
```

### Discovery

`list`, `search`, `show`/`info` and `policy` are read-only, root-free and
offline: they use a fixed `xbps-query` pass set and never sync or touch the
network.

| Command | Purpose |
|---|---|
| `xbp list [GLOB...]` | List packages matching the given globs |
| `xbp search PATTERN...` | Search names and descriptions (`--names` for names only) |
| `xbp show PKG...` / `xbp info PKG...` | Show the full details block |
| `xbp policy [PKG...]` | Show repository precedence, signatures, pins and holds; with names, the installed/candidate/version table |

Discovery flags: `-i/--installed`, `-u/--upgradable`, `-V/--virtual`,
`-a/--all-versions`, `-A/--all-arches` (accepted no-op), `--description`,
`--summary`, `-m/--machine`.

`-m` prints NDJSON: one compact JSON object per line with `"schema":1`, no
ANSI even with `--color=always`.

```sh
xbp list --upgradable
xbp list -u -m | jq -r 'select(.download_size) | "\(.name) \(.download_size)"'
xbp search editor
xbp show bash
xbp policy
xbp policy zstd
```

### Downloads

`xbp download PKG...` writes each package's archive and `.sig2` signature into
the current directory, verifying the repository SHA-256 first. It needs no
root, no sync, no prompt and writes no log or history entry. The archives can
be carried to an offline machine.

```sh
xbp download figlet
xbp download figlet vim
```

- `-d`/`--download-only` on a transaction prefetches the plan into the xbps
  cache, then stops without changing the package database.
- `xbp upgrade --print-uris` prints one JSON object per planned download and
  stops (non-root; no sync, summary or prompt). It cannot be combined with
  `--dry-run` or `--download-only`.

```sh
sudo xbp install -d figlet             # fill the cache, do not install
xbp upgrade --print-uris | jq -r .url
```

### Fetch

`xbp fetch` benchmarks Void mirrors with a 256 KiB ranged `repodata` probe,
ranks them deterministically and configures exactly one mirror (xbps is
first-match, so writing several would just multiply index syncs).

```sh
sudo xbp fetch                          # rank, confirm, write /etc/xbps.d files
sudo xbp fetch --auto 2                 # try the two fastest without a picker
sudo xbp fetch --dry-run                # show the ranking, write nothing
sudo xbp fetch -c IN --https-only       # restrict by country and scheme
sudo xbp clean --fetch                  # restore the previous configuration
```

A non-root `fetch` prints the ranking plus a sudo hint and writes nothing.
`fetch` keeps a backup of the previous files, records a manifest, runs
`xbps-install -S` after writing and reverts if the sync reports one of the
mirrors it wrote.

### History and replay

Every successful transaction is recorded under `/var/lib/xbpretty/history`
(override with `XBPRETTY_HISTORY_DIR` or the hidden `--history-dir DIR`) as one
schema-1 JSON entry, independent of the line-based transaction log.

```sh
xbp history                            # list entries, oldest first
xbp history 12                         # detail for entry 12
xbp history last                       # detail for the newest entry
xbp history info 12                    # same as history 12
```

- `undo <id|last>` restores the recorded before-state: absent packages are
  removed, differing versions are restored from the cache or the exact
  repository version. Every archive is checked before anything is changed; if
  any is missing the command fails closed with exit 1.
- `redo <id|last>` reapplies the after-state. Bare `redo` targets the newest undo
  and replays it; `redo last` targets the newest entry literally.
- Each replay is recorded as a new history entry, so the store stays
  append-only.
- `history clear <id|last>` and `history clear --all` prune the store. Replay
  and clear are root-only.

```sh
sudo xbp undo 12
sudo xbp redo
sudo xbp history clear --all
```

### Full-screen interface

The TUI is opt-in: set `[Ui] mode = "Tui"` or pass `--tui`. It opens only on a
terminal that is at least 60×18, with stdout and stdin TTYs, a UTF-8 locale,
and root for the transaction summary. `Auto` (the default) and `Plain` keep the
text summary.

- Transaction summary: `j`/`k` and arrows move, `h`/`l` switch tabs,
  `Home`/`End`/`PgUp`/`PgDn` and the mouse wheel scroll, `y` confirms,
  `q`/`Q`/`n`/`N`/`Esc`/`Ctrl-C` refuse, `s` shows package info.
- History detail: navigation and `s` work; `q`/`Esc`/`Ctrl-C` quit with exit 0;
  `y` is ignored.
- Mirror picker: `j`/`k` move (wrapping), `g`/`G`/`Home`/`End` jump, `Enter` or
  `q` confirms, `Esc`/`Ctrl-C` cancels.

`--no-tui` disables the full-screen summary. The fetch picker is part of the
enhanced surface: it still appears under `--no-tui`, but never with `--auto`,
`--dry-run`, `Plain`, `TERM=dumb`, a non-TTY stream, a non-UTF-8 locale or
without root.

### Flags

Global flags (accepted before or after the subcommand, up to `--`):

| Flag | Description |
|---|---|
| `-h`, `--help` | Print the generated command and flag table, exit 0 |
| `-V`, `--version` | Print `xbpretty <version>` and the detected xbps version. After `list`/`search`, `-V` means `--virtual`; use `--version` for the global form |
| `-v`, `--verbose` | Print every xbps command before running it |
| `--debug` | Dump the resolved configuration (implies `-v`) |
| `--config FILE` | Read `FILE` instead of the system and user config files |
| `--color auto\|always\|never` | Colour mode; `Auto` is per stream |
| `--no-color` | Never emit ANSI colour (mutually exclusive with `--color`) |
| `--tui` / `--no-tui` | Force or disable the full-screen interface |
| `--history-dir DIR` | Hidden: override the history store directory |

Transaction flags:

| Flag | Commands | Description |
|---|---|---|
| `--dry-run` | transaction verbs | Show the plan and summary, then stop |
| `-y`, `--yes`, `--assume-yes` | transaction verbs, `undo`, `redo` | Skip the confirmation prompt |
| `-n`, `--assume-no` | transaction verbs, `undo`, `redo` | Refuse without prompting, exit 1 |
| `-d`, `--download-only` | transaction verbs | Prefetch the plan into the cache and stop |
| `--simple` | transaction verbs | Print the simple summary |
| `--update` / `--no-update`, `--no-sync` | transaction verbs | Force or suppress the separate sync step |
| `--purge` | transaction verbs | Remove configuration files too (`-f`) |
| `--reinstall` | `install` | Reinstall with `-f` after an installed guard |
| `--auto-remove`, `--autoremove` / `--no-auto-remove`, `--no-autoremove` | transaction verbs | Force or suppress implicit autoremove |
| `--full`, `--no-full`, `--safe` | transaction verbs | Accepted no-ops; `--no-full` wins |
| `--print-uris` | upgrade family | Print the plan's download URLs as JSON and stop |
| `--no-log` | transaction verbs, `update`, `undo`, `redo` | Do not append to the transaction log |
| `-- ...` | transaction verbs | Pass the remaining arguments to xbps in both runs |

Command-specific flags: `-c/--country` (fetch, repeatable), `--auto [N]`,
`--https-only`, `--non-free` (fetch); `--lists`, `--fetch` (clean);
`--limit N` (log); `--all` (history clear); discovery flags above.

`--help`, `--version`, `completion` and `link` do not load the config.
Unknown flags, a flag on the wrong command, and conflicting pairs (`-y`/`-n`,
`--color`/`--no-color`, `--update`/`--no-update`, `--auto-remove`/`--no-auto-remove`,
`--tui`/`--no-tui`) exit 2. Flags that apt has but xbps cannot express are
refused with a clear message and exit 2; they are never silent no-ops.

### Scripting, streams and exit codes

- stdout carries durable output only: the summary, `Nothing to do.`, update
  counts, download results, discovery, history lists/details, help/version and
  machine output. The prompt, progress widget, hints, warnings and forwarded
  xbps output go to stderr. The one exception is the full-screen summary,
  which owns the screen for that transaction.
- With `Auto` colour, ANSI is emitted only for a stream that is a TTY, and
  never when `NO_COLOR` is set. Redirected stdout therefore stays clean:
  `xbp upgrade --dry-run | grep pkg`.
- Exit codes: `0` success/nothing to do/dry run/`--download-only`/clean/update/
  empty history/fetch success; `1` refusal/non-TTY/non-root/runtime/missing
  history/missing archive/partial download; `2` usage/conflict/refused flag/
  config error. xbps failures pass their own code through.
- A broken stdout pipe stops writing but does not abort the transaction, and
  the run keeps its exit code. SIGINT/SIGTERM are forwarded to the child.

### Shell completion and links

```sh
sudo xbp completion bash > /usr/share/bash-completion/completions/xbpretty
sudo xbp completion zsh  > /usr/share/zsh/site-functions/_xbpretty
sudo xbp completion fish > /usr/share/fish/vendor_completions.d/xbpretty.fish
./xbpretty link ~/.local/bin          # DIR must already exist
```

`link` creates exactly the six alias names and is silent on success. It never
creates the directory, and it refuses to overwrite a conflicting file or link.

---

## Transaction Log

Successful operations are appended to `$XDG_STATE_HOME/xbpretty/transactions.log` (falls back to `~/.local/state/xbpretty/transactions.log`):

```
<RFC 3339 UTC timestamp> <command> [<name-version> ...]
```

For example:
```
2026-10-21T21:49:22Z install firefox-158.0.1_1
2026-10-22T03:59:12Z remove firefox-158.0.1_1
```

Query the log using `xbp log`:

```sh
xbp log                  # View full history (oldest first)
xbp log --limit 10       # View last 10 entries
xbp log curl             # Show transactions affecting exactly package 'curl'
```

See [ADR-0008](docs/adr/0008-transaction-log-line-format.md) for details on log structure.

---

## Configuration

Configuration is nala's TOML schema: `[Nala]`, `[Ui]`, `[Color]`,
`[Color.theme]`, plus xbps-only `[Xbps]`. There are two layers,
`/etc/xbpretty/config.toml` (optional, administrator-created) and
`$XDG_CONFIG_HOME/xbpretty/config.toml` (or `~/.config/xbpretty/config.toml`).
The user file wins per key; theme roles merge role by role.

When executed with `sudo` or `doas`, xbpretty resolves the calling user's
passwd home so your personal settings always apply. Command-line flags take
precedence over configuration values. Under root, the `[Xbps]` binary paths are
honoured only from a root-owned file that is not group- or world-writable; a
user file's paths are ignored with a warning, while its other keys still apply.

The canonical example is [`docs/config.toml.example`](docs/config.toml.example),
which parses to the built-in defaults:

```toml
[Nala]
auto_remove = true
auto_update = true
update_show_packages = false
full_upgrade = false
simple = false
assume_yes = false

[Ui]
mode = "Auto"          # Auto, Tui or Plain
unit = "Binary"        # Binary or Decimal

[Color]
mode = "Auto"          # Auto, Always or Never

[Color.theme]
Primary   = { fg = "BrightGreen" }
Secondary = { fg = "BrightBlue" }
Highlight = { fg = "Reset" }
Regular   = { fg = "Reset", modifier = "" }
ProgressFilled   = { fg = "BrightGreen" }
ProgressUnfilled = { fg = "BrightRed" }
Notice    = { fg = "BrightYellow" }
Warning   = { fg = "BrightYellow" }
Error     = { fg = "BrightRed" }

[Xbps]
install_path = "xbps-install"
remove_path = "xbps-remove"
query_path = "xbps-query"
rindex_path = "xbps-rindex"
uhelper_path = "xbps-uhelper"
pkgdb_path = "xbps-pkgdb"
transaction_log = true
```

`[Ui] mode = "Plain"` is the supported way to turn off the progress widget and
the full-screen surfaces. The rev-5 flat keys (`assume_yes`, `box_style`,
`progress_bar`, `color`, `sync`, `log`, the three `xbps_*_path` names and
`verbose`) are accepted with one aggregate warning during the rev-6 window and
are removed in rev 7; see
[`docs/migration-rev6.md`](docs/migration-rev6.md).

---

## Architecture & Specification

For in-depth design decisions and specifications, consult the project documentation:

- [xbpretty-spec.md](xbpretty-spec.md) — Complete functional specification and acceptance criteria.
- [GLOSSARY.md](GLOSSARY.md) — Ubiquitous language and domain terminology.
- [ADR-0005](docs/adr/0005-output-rules-the-code-only-stated.md) — Output formatting and pipeline rules.
- [ADR-0008](docs/adr/0008-transaction-log-line-format.md) — Append-only transaction log format.
- [ADR-0012](docs/adr/0012-nala-visual-system.md) — Nala v0.18.2 visual system: roles, borderless tables and the widget.
- [ADR-0013](docs/adr/0013-history-store-and-replay.md) — History store and replay guarantees.
- [ADR-0014](docs/adr/0014-predownload-is-an-accelerator.md) — Predownload as an accelerator.
- [ADR-0015](docs/adr/0015-fetch-writes-one-repository.md) — One-mirror fetch policy.
- [ADR-0016](docs/adr/0016-config-schema-layers-and-trust.md) — Config schema, layers and root trust.
- [ADR-0017](docs/adr/0017-full-screen-surfaces-and-engine.md) — Full-screen surfaces, inline viewport and engine.
- [ADR-0020](docs/adr/0020-parity-matrix-generated-and-checked.md) — The parity matrix, generated and checked.
- [docs/migration-rev6.md](docs/migration-rev6.md) — Rev-5 to rev-6 config and behaviour migration.

---

## Releasing

Releases are tag driven. `srcpkgs/xbpretty/template` is the single version
source; rev 6 is `0.4.0` and the tag is `xbpretty-v0.4.0`.

1. Update `version=` in `srcpkgs/xbpretty/template`.
2. Refresh the mirror snapshot when it is older than 90 days: `make mirrors`.
3. Commit and push to `main`.
4. Push a Git tag matching the version: `git tag xbpretty-v0.4.0 && git push origin xbpretty-v0.4.0`.
5. The GitHub Actions release workflow gates tests, vet, generated completions
   and manpages, shell syntax, `mandoc`, `shellcheck` and actionlint, then builds
   all nine architectures with `tools/build-xbps.sh`, enforces the static-link,
   8 MB binary and 3.6 MB package budgets, checks the package contents and both
   the unsigned and signed clean-root installs, signs each architecture, and
   creates the GitHub release from `docs/release-notes-0.4.0.md`.
6. The workflow's `template` job then commits the release tarball `checksum=`
   back to `main`. Build acceptance with
   `xbps-src pkg xbpretty` runs from `main` after that checksum bump; the tagged
   tree itself still carries the previous release's checksum, as xbps-src
   requires a published tarball hash.

---

## License

[MIT](LICENSE).

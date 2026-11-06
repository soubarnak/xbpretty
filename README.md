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

`xbpretty` is packaged and distributed via a signed XBPS repository for all official Void Linux architectures (`x86_64`, `i686`, `aarch64`, `armv7l`, `armv6l`, across both glibc and musl).

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

One binary, seven names: the `xbpretty` binary plus six aliases. `xbp` is the primary shorthand; the `xbp-*` commands are symlinks that dispatch on `argv[0]`.

| Command | Wraps | Alias | Description |
|---|---|---|---|
| `xbp install <pkgs...>` | `xbps-install <pkgs...>` (no `-S`) | `xbp-install` | Install one or more packages |
| `xbp remove <pkgs...>` | `xbps-remove <pkgs...>` | `xbp-remove` | Remove installed packages |
| `xbp upgrade` | `xbps-install -Su` (separate `-S` step, then `-u`) | `xbp-upgrade` | Upgrade all installed packages |
| `xbp autoremove` | `xbps-remove -o` | `xbp-autoremove` | Clean up orphaned packages |
| `xbp update` | `xbps-install -S` | `xbp-update` | Refresh repository sync index |
| `xbp log [--limit N] [pkgs...]` | *None* (reads transaction log) | *None* | View past transaction log |
| `xbp link [dir]` | *None* | *None* | Generate command symlinks in target directory |
| `xbp completion bash\|zsh\|fish` | *None* | *None* | Output shell completion script |

### Flags

| Flag | Description |
|---|---|
| `--dry-run` | Preview the transaction plan and summary without making any changes |
| `-n`, `--assume-no` | Refuse at the confirmation prompt (exit 1) without changing anything |
| `-y`, `--yes` | Proceed automatically without confirmation prompt |
| `-v`, `--verbose` | Print raw command lines and stream unbuffered xbps output directly |
| `--update` | Run the separate repository sync step (`xbps-install -S`) first |
| `--no-update`, `--no-sync` | Do not refresh repository indexes before calculating the transaction |
| `--no-log` | Do not append this execution to the transaction log |
| `--simple` | Print the simple summary with no rules, table or version diff |
| `--color=WHEN` | Colour output: `auto` (default), `always`, or `never`; `Auto` is per stream |
| `--no-color` | Emit no ANSI colour |
| `--tui` / `--no-tui` | Force or disable the full-screen interface |
| `--limit N` | With `xbp log`: output only the last `N` matching transaction entries |
| `--version` | Display version information for xbpretty and underlying xbps |
| `-- [xbps-flags]` | Forward arguments after `--` directly to xbps (e.g. `xbp install -- -f pkg`) |

### Workflow & Privileges

- **Interactive Root**: Runs `xbps` in dry-run mode (`--dry-run`), renders the summary table, and prompts `[Y/n]` on stderr. Answering `y` proceeds with execution using the live progress widget.
- **Unprivileged User**: Running install/upgrade commands without root shows the transaction plan and exits with a reminder to execute under `sudo` or `doas`.
- **Non-Interactive / Scripting**: If stdin is not a TTY and neither `-y` nor `--dry-run` is provided, `xbpretty` prints the summary and exits 1 with a reminder to provide `-y`.
- **Piping & Grep**: stdout carries durable output only. With `Auto` colour, a redirected stdout gets no ANSI and the summary keeps natural table widths, so `xbp upgrade --dry-run | grep pkg` stays clean.

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
precedence over configuration values.

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
the full-screen surfaces. The rev-5 flat keys (`box_style`, `progress_bar`,
`color`, `sync`, `log`, the `xbps_*_path` names and `verbose`) are accepted with
one aggregate warning during the rev-6 window and are removed in rev 7; see
[`docs/migration-rev6.md`](docs/migration-rev6.md).

---

## Architecture & Specification

For in-depth design decisions and specifications, consult the project documentation:

- [xbpretty-spec.md](xbpretty-spec.md) — Complete functional specification and acceptance criteria.
- [GLOSSARY.md](GLOSSARY.md) — Ubiquitous language and domain terminology.
- [ADR-0005](docs/adr/0005-output-rules-the-code-only-stated.md) — Output formatting and pipeline rules.
- [ADR-0008](docs/adr/0008-transaction-log-line-format.md) — Append-only transaction log format.
- [ADR-0012](docs/adr/0012-nala-visual-system.md) — Nala v0.18.2 visual system: roles, borderless tables and the widget.
- [ADR-0017](docs/adr/0017-full-screen-surfaces-and-engine.md) — Full-screen surfaces, inline viewport and engine.
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
   (and the version/revision) back to `main`. Build acceptance with
   `xbps-src pkg xbpretty` runs from `main` after that checksum bump; the tagged
   tree itself still carries the previous release's checksum, as xbps-src
   requires a published tarball hash.

---

## License

[MIT](LICENSE).

<p align="center">
  <img src="https://bongbetic.com/branding/bongbetic-logo-lockup.png" alt="Bongbetic" width="320">
</p>

# xbpretty

A modern, friendly front end for [`xbps`](https://github.com/void-linux/xbps), the Void Linux package manager, inspired by [Nala](https://gitlab.com/volian/nala).

`xbpretty` previews transactions in rich Unicode container tables, highlights package version diffs, asks for confirmation before acting, tracks downloads and installations in a live framed progress widget, records transaction history, and explains common xbps errors with actionable hints. It wraps `xbps-install`, `xbps-remove`, and `xbps-query`; it does not replace them.

Built in pure Go with zero external dependencies, `xbpretty` compiles to a single static binary (`CGO_ENABLED=0`), starts in under 2ms, and remains 100% compatible with offline `xbps-src` packaging.

```
╭── Packages To Upgrade (2) ───────────────────────────────────────────────────╮
│  Package                           Version Delta                        Size │
├──────────────────────────────────────────────────────────────────────────────┤
│  curl                              8.21.0_1 → 8.22.0_1              274.5 KiB │
│  libcurl                           8.21.0_1 → 8.22.0_1              312.8 KiB │
╰──────────────────────────────────────────────────────────────────────────────╯

╭── Summary ───────────────────────────────────────────────────────────────────╮
│  Changes:  2 upgrade                                                         │
│  Transfer: 587.3 KiB download  •  Disk: +18.4 KiB                            │
╰──────────────────────────────────────────────────────────────────────────────╯
```

---

## Features

- **Nala-Style Boxed Tables**: Encloses package actions into organized container cards (*Packages To Install*, *Packages To Upgrade*, *Packages To Remove*, *Held Packages*) with human-readable sizes and aligned columns.
- **Version Delta Highlighting**: Displays version transitions (`old → new`) with upgraded components or package revisions highlighted.
- **Live Framed Progress Widget**: Real-time 5-line terminal widget tracking download speeds, ETA, cyan-over-red progress bars, and unpack/configure spinners powered by a hybrid PTY/pipe execution engine.
- **Dynamic Resize Support**: Automatically adapts progress box width when the terminal window is resized (`SIGWINCH`).
- **Zero Dependencies & Blazing Fast**: Pure Go standard library VT100/ANSI rendering engine. No heavy third-party TUI frameworks, sub-2ms startup overhead, and offline packaging friendly.
- **Script & Pipeline Friendly**: Automatically drops container borders, ANSI escape codes, and interactive widgets when piped (`xbp upgrade -n | grep pkg`) or when `NO_COLOR` is set.
- **Humanized Error Hints**: Translates cryptic xbps errors (missing packages, stale indexes, locked databases, and reverse dependencies via `xbps-query -X`) into clear, actionable advice.
- **Transaction History**: Appends every successful change to `$XDG_STATE_HOME/xbpretty/transactions.log`, queryable at any time with `xbp log`.
- **Kernel Reboot Reminders**: Automatically prompts a reboot reminder whenever an upgrade touches the Linux kernel or glibc.
- **Sudo / Doas Aware**: Resolves `SUDO_USER` or `DOAS_USER` when run with root privileges so your user configuration and transaction logs remain in your home directory with correct file ownership.

---

## Visual Showcase

### Live Execution Progress Widget

When running interactively, child `xbps` progress is captured and rendered into a smooth, in-place 5-line widget:

**Download Phase:**
```
╭── Downloading... ────────────────────────────────────────────────────────────╮
│  Total Packages: 2/5                                                         │
│  Downloading:    firefox-130.0_1                                             │
│  ETA: 0:00:03  [━━━━━━━━━━━━────────] 61.2% • 42.1 MiB/68.8 MiB • 8.4 MiB/s   │
╰──────────────────────────────────────────────────────────────────────────────╯
```

**Unpack & Configure Phase:**
```
╭── Running xbps... ───────────────────────────────────────────────────────────╮
│  Phase:           ⠋ Unpacking packages                                       │
│  Current Target:  firefox-130.0_1                                            │
│  Status:          [━━━━━━━━━━━━━━━━━━━━] 100.0% • 5/5 actions                 │
╰──────────────────────────────────────────────────────────────────────────────╯
```

**Completion Teardown:**
```
✔ Transaction complete: 2 installed, 3 updated, 0 removed in 4.2s
```

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

### From Source

Requires Go 1.25 or newer:

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

One binary, six names. `xbp` is the primary shorthand; the `xbp-*` commands are symlinks that dispatch on `argv[0]`.

| Command | Wraps | Alias | Description |
|---|---|---|---|
| `xbp install <pkgs...>` | `xbps-install -S <pkgs...>` | `xbp-install` | Install one or more packages |
| `xbp remove <pkgs...>` | `xbps-remove <pkgs...>` | `xbp-remove` | Remove installed packages |
| `xbp upgrade` | `xbps-install -Su` | `xbp-upgrade` | Full system upgrade |
| `xbp autoremove` | `xbps-remove -o` | `xbp-autoremove` | Clean up orphaned packages |
| `xbp update` | `xbps-install -S` | `xbp-update` | Refresh repository sync index |
| `xbp log [--limit N] [pkgs...]` | *None* (reads transaction log) | *None* | View past transaction log |
| `xbp link [dir]` | *None* | *None* | Generate command symlinks in target directory |
| `xbp completion bash\|zsh\|fish` | *None* | *None* | Output shell completion script |

### Flags

| Flag | Description |
|---|---|
| `-n`, `--dry-run` | Preview the transaction plan and summary without making any changes |
| `-y`, `--yes` | Proceed automatically without confirmation prompt |
| `-v`, `--verbose` | Print raw command lines and stream unbuffered xbps output directly |
| `--box-style=STYLE` | Select container border style: `rounded` (default), `classic`, or `ascii` |
| `--progress-bar` | Force enable in-place execution progress widget |
| `--no-progress-bar` | Disable in-place progress widget and stream xbps logs line-by-line |
| `--no-sync` | Do not refresh repository indexes before calculating transaction |
| `--no-log` | Do not append this execution to the transaction log |
| `--limit N` | With `xbp log`: output only the last `N` matching transaction entries |
| `--color=WHEN` | Colorize summary: `auto` (default), `always`, or `never` |
| `--version` | Display version information for xbpretty and underlying xbps |
| `-- [xbps-flags]` | Forward any arguments following `--` directly to xbps (e.g. `xbp install -- -f pkg`) |

### Workflow & Privileges

- **Interactive Root**: Runs `xbps` in dry-run mode (`-n`), renders the summary table, and prompts `[Y/n]` on stderr. Answering `y` proceeds with execution using the live progress widget.
- **Unprivileged User**: Running install/upgrade commands without root shows the transaction plan and exits with a reminder to execute under `sudo` or `doas`.
- **Non-Interactive / Scripting**: If stdin is not a TTY and neither `-y` nor `--dry-run` is provided, `xbpretty` prints the summary and exits 1 with a reminder to provide `-y`.
- **Piping & Grep**: When output is redirected or piped (`xbp upgrade -n | grep pkg`), box characters and ANSI codes are automatically stripped to guarantee clean plaintext parsing.

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

Configuration is loaded from `$XDG_CONFIG_HOME/xbpretty/config.toml` (or `~/.config/xbpretty/config.toml`).

When executed with `sudo` or `doas`, xbpretty resolves the calling user's home directory so your personal settings always apply. Command-line flags take precedence over configuration values.

Example `config.toml`:

```toml
# General preferences
assume_yes    = false      # Equivalent to -y/--yes
color         = "auto"     # "auto", "always", or "never"
verbose       = false      # Stream raw command lines and output
sync          = true       # Refresh repository indexes by default
log           = true       # Record successful runs to transaction log

# Visual & TUI preferences
box_style     = "rounded"  # "rounded", "classic", or "ascii"
progress_bar  = true       # Enable 5-line framed execution widget

# Custom binary paths (optional)
# xbps_install_path = "/usr/bin/xbps-install"
# xbps_remove_path  = "/usr/bin/xbps-remove"
# xbps_query_path   = "/usr/bin/xbps-query"
```

---

## Architecture & Specification

For in-depth design decisions and specifications, consult the project documentation:

- [xbpretty-spec.md](xbpretty-spec.md) — Complete functional specification and acceptance criteria.
- [GLOSSARY.md](GLOSSARY.md) — Ubiquitous language and domain terminology.
- [ADR-0005](docs/adr/0005-output-rules-the-code-only-stated.md) — Output formatting and pipeline rules.
- [ADR-0008](docs/adr/0008-transaction-log-line-format.md) — Append-only transaction log format.
- [ADR-0011](docs/adr/0011-nala-tui-layout-and-execution-progress.md) — Nala-style TUI layout and hybrid PTY/pipe progress engine.

---

## Releasing

Releases are tag driven:

1. Update `version=` in `srcpkgs/xbpretty/template`.
2. Commit and push to `main`.
3. Push a Git tag matching the version: `git tag xbpretty-vX.Y.Z && git push origin xbpretty-vX.Y.Z`.
4. The GitHub Actions release workflow runs the test suite, builds multi-architecture packages using `tools/build-xbps.sh`, signs the repository, and creates the GitHub release with updated checksums.

---

## License

[MIT](LICENSE).

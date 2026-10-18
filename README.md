<p align="center">
  <img src="https://bongbetic.com/branding/bongbetic-logo-lockup.png" alt="Bongbetic" width="320">
</p>

# xbpretty

A friendlier front end for [`xbps`](https://github.com/void-linux/xbps), the Void Linux package
manager. It previews every transaction, shows what changes and how much space it uses, asks before
it acts, and explains the common xbps failures. It wraps `xbps-install`, `xbps-remove` and
`xbps-query`; it does not replace them.

Every summary ends with a footer such as:

```
3 install, 12 upgrade, 1 remove, 2 hold · 48.2 MiB download · +120.4 MiB disk
```

## Install

xbpretty is released as a signed XBPS repository, one package per architecture (x86_64, i686,
aarch64, armv7l, armv6l, each in glibc and musl where Void offers it). Add it once:

```sh
echo 'repository=https://github.com/soubarnak/xbpretty/releases/latest/download' | sudo tee /etc/xbps.d/20-xbpretty.conf
sudo xbps-install -S xbpretty
```

xbps asks you to accept the repository key on the first sync. The fingerprint must be:

```
fa:d1:00:32:ce:b1:b9:fd:73:05:05:76:75:54:37:25
```

Do not accept a different one. Updates arrive with `sudo xbps-install -Su`.

### From source

Needs Go 1.25 or newer.

```sh
make build        # builds ./xbpretty
make test
```

## Usage

One binary, six names. `xbp` is the short form; the `xbp-*` names are symlinks that dispatch on
`argv[0]`.

| Command | Wraps | Alias |
|---|---|---|
| `xbp install <pkgs...>` | `xbps-install -S <pkgs...>` | `xbp-install` |
| `xbp remove <pkgs...>` | `xbps-remove <pkgs...>` | `xbp-remove` |
| `xbp upgrade` | `xbps-install -Su` | `xbp-upgrade` |
| `xbp autoremove` | `xbps-remove -o` | `xbp-autoremove` |
| `xbp update` | `xbps-install -S` | `xbp-update` |
| `xbp log [--limit N] [package...]` | nothing (reads the transaction log) | none |

Other commands: `xbp completion bash|zsh|fish`, and `xbp link [dir]` to create the symlinks when
you install by hand.

Flags: `-n/--dry-run`, `-y/--yes`, `-v/--verbose`, `--no-sync`, `--no-log`,
`--color=auto|always|never`, `--version`. Arguments after `--` go to xbps unchanged, for example
`xbp install -- -f pkg`.

By default xbpretty runs xbps with `-n`, prints the summary, and prompts `[Y/n]` when you are root.
Unprivileged, it prints the summary and tells you to rerun under `doas` or `sudo`. Without a
terminal, pass `-y` or `--dry-run`. Only the summary goes to stdout, so `xbp upgrade -n | grep`
works.

`xbp log` prints the transaction log, oldest first, one stored line per line of stdout. Package
names (no versions) keep only lines that changed a package with exactly that name, so `xbp log curl`
does not show `libcurl`; several names match any of them. `--limit N` keeps the last N matching
lines. It needs no root, writes nothing and runs no xbps. A missing or empty log prints a note on
stderr and exits 0; an unreadable one exits 1. `--dry-run`, `--yes`, `--no-sync` and `--` are usage
errors. There is no `xbp-log` link.

Transactions are logged to `$XDG_STATE_HOME/xbpretty/transactions.log`, one line per successful
real run, appended and never rewritten:

```
<RFC 3339 UTC timestamp> <command> [<name-version> ...]
```

For example `2026-10-15T09:41:01Z install figlet-2.2.5_6`. Fields are separated by single spaces
and a line never ends in a space. Only packages the transaction changed (install, update,
reinstall, remove, configure) are listed; held packages are not. `update` lines carry no packages.
The log cannot replay a transaction. See [ADR-0008](docs/adr/0008-transaction-log-line-format.md).

Configuration lives in
`$XDG_CONFIG_HOME/xbpretty/config.toml` (`assume_yes`, `color`, `verbose`, `sync`, `log`,
`xbps_install_path`, `xbps_remove_path`, `xbps_query_path`). Command-line flags win over the file.

See [xbpretty-spec.md](xbpretty-spec.md) for the full specification.

## Releasing

Releases are tag driven. Bump `version=` in `srcpkgs/xbpretty/template`, commit, push, then push a
tag `xbpretty-vX.Y.Z` that matches it. The workflow runs the tests, builds the packages with
`tools/build-xbps.sh`, signs the repository and attaches it to the GitHub release. It then commits
the new `checksum` to the template.

## License

[MIT](LICENSE).

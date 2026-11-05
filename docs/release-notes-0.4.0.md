# xbpretty 0.4.0

xbpretty 0.4.0 is the rev 6 release: nala v0.18.2 parity, a full-screen TUI,
replayable history, predownload and fetch, discovery with machine output, and
the new two-layer configuration. It builds for all nine Void Linux architecture
variants from one static binary.

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

## What is new in 0.4.0

- **Nala v0.18.2 parity** across the command surface, rendered tables, summary
  figures and error hints, with the full matrix in `docs/parity/matrix.md`.
- **History, undo and redo** — `xbp history`, `xbp undo`, `xbp redo` replay
  recorded transactions from the append-only transaction log.
- **Predownload and fetch** — `xbp fetch` writes one repository transaction file
  and `--predownload` fills the cache before the transaction starts.
- **Discovery and machine output** — `list`, `search`, `show`/`info`, `policy`
  and their NDJSON (`-m`, schema 1) forms.
- **Full-screen TUI** — the optional `Tui` mode, with the documented text
  fallback below 60x18.
- **Two-layer configuration** — the optional administrator-created
  `/etc/xbpretty/config.toml` is read before the user layer; the package itself
  installs no `/etc` file.
- **One binary, six names** — `xbpretty`, `xbp`, `xbp-install`, `xbp-remove`,
  `xbp-upgrade`, `xbp-autoremove` and `xbp-update`.
- **Signed packages for nine architectures** — `x86_64`, `x86_64-musl`, `i686`,
  `aarch64`, `aarch64-musl`, `armv7l`, `armv7l-musl`, `armv6l` and
  `armv6l-musl`.

## Migrating from 0.3.x (spec rev 5)

Rev 6 accepts the rev-5 flat config keys with a warning and never rewrites your
config file. The complete key map, retired keys, behaviour changes and rollback
steps are in the migration guide:

- [Migrating from rev 5](https://github.com/soubarnak/xbpretty/blob/main/docs/migration-rev6.md)
- [Canonical rev 6 config example](https://github.com/soubarnak/xbpretty/blob/main/docs/config.toml.example)

Both files are installed under `/usr/share/doc/xbpretty/`. Rev 7 (0.5.0) is the
first release that rejects the rev-5 keys, so this upgrade does not require a
config rewrite.

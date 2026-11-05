# Migrating from xbpretty 0.3.x (spec rev 5) to rev 6

Rev 6 changes the config schema, the meaning of `-n`, the summary and progress rendering, and adds history, fetch and the TUI. **It does not rewrite your config file and it does not import your old transaction log.** This page is the whole migration.

## 1. Who this is for

Anyone with a `~/.config/xbpretty/config.toml` written for 0.3.x (spec rev 5), or a `transactions.log` from before rev 6.

## 2. What changed in one screen

- The config is nala's TOML: `[Nala]`, `[Ui]`, `[Color]`, `[Color.theme]`, plus the xbps-only `[Xbps]` table.
- There are two layers: `/etc/xbpretty/config.toml` (optional, administrator-created) and your user file. Under `sudo`/`doas`, xbpretty reads the invoking user's passwd home, not `/root`.
- `-n` now means `--assume-no` (nala's meaning). The dry run is long-only `--dry-run`.
- `--box-style`, `--progress-bar` and `--no-progress-bar` are gone. The summary is borderless tables; the progress widget is controlled by `[Ui] mode = "Plain"`.
- `clean` no longer aliases `autoremove`; it cleans the xbps cache (`xbps-remove -O`).
- `install` no longer syncs the index by default. `--update` adds a sync step; the upgrade family still syncs by default.
- New commands: `download`, `history`, `undo`, `redo`, `fetch`, `list`, `search`, `show`/`info`, `policy`.
- The transaction log is unchanged and audit-only. `xbp history` starts empty.

## 3. Do I have to do anything now?

No. Rev 6 reads your old flat keys, warns once per key, and runs. The seven renames below only silence warnings and prepare for rev 7. The three retired keys can be deleted whenever.

## 4. The key map

| Rev 5 key | Rev 6 target | Action |
|---|---|---|
| `assume_yes` | `[Nala] assume_yes` | Rename |
| `sync` | `[Nala] auto_update` | Rename; the scope narrows (only the upgrade family's implicit sync) |
| `log` | `[Xbps] transaction_log` | Rename; it only gates the line log |
| `color` | `[Color] mode` | Rename and map `auto`→`Auto`, `always`→`Always`, `never`→`Never` |
| `xbps_install_path` | `[Xbps] install_path` | Rename; privileged under sudo |
| `xbps_remove_path` | `[Xbps] remove_path` | Rename; privileged under sudo |
| `xbps_query_path` | `[Xbps] query_path` | Rename; privileged under sudo |
| `verbose` | — | Delete; use `-v` |
| `box_style` | — | Delete; the summary has no box |
| `progress_bar` | — | Delete; use `[Ui] mode = "Plain"` |

Before and after:

```toml
# 0.3.x
assume_yes = false
color = "auto"
sync = true
log = true
box_style = "rounded"
progress_bar = true
```

```toml
# rev 6
[Nala]
assume_yes = false
auto_update = true

[Color]
mode = "Auto"

[Xbps]
transaction_log = true
```

## 5. Copy-paste example

Copy [`docs/config.toml.example`](config.toml.example) to `~/.config/xbpretty/config.toml`. It is byte-for-byte the canonical example and parses to the built-in defaults, so it changes nothing until you edit it. The same file is installed at `/usr/share/doc/xbpretty/config.toml.example`.

Minimal working file (all defaults):

```toml
[Nala]
auto_remove = true
auto_update = true
```

## 6. Retired keys

- `verbose` → pass `-v` on the command line.
- `progress_bar` → set `[Ui] mode = "Plain"` (no progress widget or plain line; the summary is unchanged).
- `box_style` → nothing. The summary is borderless tables; the progress widget is always rounded.

They are accepted and ignored for rev 6 with a warning; rev 7 rejects them.

## 7. Behaviour changes a rename does not capture

- **`sync` scope.** `auto_update` gates only the upgrade family's implicit sync. `install`/`remove` never add `-S`; `--update` adds a standalone sync step.
- **`log = false`.** It stops only the append-only line log. The history store is always written for successful transactions and has no off switch.
- **Colour.** `[Color] mode` takes PascalCase (`Auto`, `Always`, `Never`). The old `color = "auto"` value maps automatically. `NO_COLOR` still works; `TERM=dumb` disables the live UI, not colour.
- **Strict TOML.** Rev 6 is a TOML subset. Floats, dates, multiline strings, dotted keys, arrays of tables, quoted keys, `theme = { ... }` and `[Color.theme.Primary]` are hard errors with a path and line. Duplicate keys are an error. The ten legacy keys are the only positions that still accept rev-5 bare/quoted values.
- **XDG under sudo.** Under `sudo`/`doas`, `XDG_CONFIG_HOME` and `XDG_STATE_HOME` are ignored; the invoking user's passwd home supplies config and state.

## 8. The system layer

`/etc/xbpretty/config.toml` is **not installed** and does not exist by default. Create it only for host-wide policy:

```sh
sudo install -Dm644 /usr/share/doc/xbpretty/config.toml.example /etc/xbpretty/config.toml
sudo chown root:root /etc/xbpretty/config.toml
sudo chmod 0644 /etc/xbpretty/config.toml
```

It is read before your user file; your user file wins per key. Theme roles merge role-by-role. A malformed system file is a hard error even if your user file would override it.

## 9. Sudo

Under `sudo xbp install foo`, xbpretty reads your passwd home's config and writes your state, not root's. To see which files are read:

```sh
sudo xbp log --debug
```

A `[Xbps]` binary path in a user-owned file is **ignored** under sudo (a root-owned binary path from a user file would let a user choose what root executes). You will see two warnings for a legacy `xbps_*_path`: the deprecation warning and the trust warning. Fix it by deleting the key (the built-in `xbps-*` names are correct on every normal Void system) or by moving it to the root-owned system layer.

## 10. The transaction log and history

- `transactions.log` is untouched: same path, same line format, append-only. `xbp log` reads old and new lines identically.
- Rev 6 adds replayable history at `/var/lib/xbpretty/history`. It starts **empty**. Old log lines are never imported, because they lack the old version of upgraded packages, the operation, arch/repository, sizes and the automatic-install bit; a synthesised entry would offer undo that cannot be honoured.
- `xbp history` on an empty store prints `No history entries found.` and, when the log has entries, one note telling you to use `xbp log`.
- History ids start at 1 with the first successful post-rev-6 transaction.
- `--no-log`/`transaction_log = false` stops the line log only; history is still recorded.

## 11. Verifying

```sh
xbp log --debug      # no 'legacy key' lines after you migrate
xbp log              # your old transactions, unchanged
xbp history          # entries from this release onward
```

## 12. Rolling back

Rev 6 never modifies your config, so a 0.3.x binary still reads the same file. Keep a copy before editing:

```sh
cp ~/.config/xbpretty/config.toml ~/.config/xbpretty/config.toml.bak
```

A rollback does not migrate history back; 0.3.x simply ignores `/var/lib/xbpretty/history`.

## 13. When the old keys stop working

The first release whose spec is **rev 7** rejects the ten rev-5 names as unknown keys, exit 2. A patch release never removes or extends the window; extending it needs a new spec revision. There is no auto-conversion and no `config migrate` command — this page and the example are the migration.

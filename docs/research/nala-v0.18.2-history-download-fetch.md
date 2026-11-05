# Research: nala v0.18.2 — History Store/Replay, Parallel Downloader, Mirror Fetch, and `--machine` Output

- **Ticket**: [#40](https://github.com/soubarnak/xbpretty/issues/40) (Wayfinder map [#37](https://github.com/soubarnak/xbpretty/issues/37), full nala-parity effort)
- **Author**: Soubarna
- **Date**: 2026-11-05
- **Status**: Complete
- **Parity reference**: nala tag **v0.18.2** = commit `2b7ff0554ca88e761fa6eaf8604e19c5e3033815` ("Prepare Nala 0.18.2 Debian release", 2026-10-22), <https://github.com/volitank/nala/tree/v0.18.2>. Every `file:line` below is from that tagged tree, **not** `main`.
- **Primary sources read**:
  - `src/cmd/history/{mod,model,store,replay,view,legacy,tests}.rs`
  - `src/libnala/{mod,transaction}.rs` (the `Operation`/`PackageState`/`PackageTransition` types stored in history)
  - `src/summary.rs` (the only writer of history entries), `src/util/prompt.rs`, `src/util/privilege.rs`
  - `src/download/{mod,downloader,uri,auth,proxy}.rs`, `src/hashsum.rs`, `src/fs.rs`, `src/progress.rs`, `src/terminal.rs`
  - `src/cmd/fetch/{mod,score,sources,mirrors,release,output}.rs`, `src/tui/fetch.rs`
  - `src/cmd/{clean,list,show,policy,mod}.rs`, `src/cli/{commands,flags,parser}.rs`, `src/config/{paths,configuration,file,keys,logger,mod}.rs`, `src/main.rs`
  - `tests/deb/history/{run,verify.py,legacy.json,rust-v1.json}`
  - Machine-output schema: `rust-apt` 0.11.3 (pinned in `Cargo.lock:1969-1972`), crate source `src/iterators/serde.rs`
- **Scope correction**: there is **no `src/cmd/download.rs`** in v0.18.2. The `download` command lives in `src/download/downloader.rs:23-74`; `src/cmd/mod.rs:10` only wires `show, policy, update, upgrade, history, fetch, clean` plus `install`/`list`.

---

## 0. Executive Summary

1. **History is one pretty-printed JSON file per transaction**, `<history-dir>/<id>.json`, schema `schema_version: 1`, written atomically (temp + rename) *after* a successful dpkg run. The directory defaults to `/var/lib/nala/history` and is overridable by `--history-dir` / `NALA_HISTORY_DIR` (`src/config/paths.rs:1`, `src/config/configuration.rs:177-186`, `src/config/mod.rs:112-118`).
2. **Entries are complete before/after package-state snapshots**, not apt/dpkg logs: `{name, size, operation, before{version,auto_installed,config_files_only}, after{...}}`. Undo/redo is a *cache-marking replay* of those snapshots through the normal solver + summary + dpkg commit path, which means **an undo/redo is itself recorded as a new history entry**.
3. **IDs are `max(existing IDs) + 1`**, allocated before dpkg and only persisted on success; there is no monotonic counter, so IDs can be reused after a failed transaction or after `history clear` of the newest entry (`src/cmd/history/store.rs:153-160`, `src/summary.rs:359-389`).
4. **The downloader gives every package a Tokio task but caps *per-domain* concurrency at 3 connections**, with FIFO waiters and an "exclusive" reservation that prevents multi-mirror packages from starving single-mirror packages; the first-choice mirror rotates round-robin (`src/download/mod.rs:14-160`; test `src/download/uri.rs:458-492`).
5. **Verification is hash-only, not signature-per-file**: SHA512 preferred, SHA256 fallback, MD5/SHA1 never used; trust in the hash derives from APT's index-file trust, and untrusted packages (or URLs with no hash) require `--allow-unauthenticated` (`src/hashsum.rs:17-35`, `src/download/uri.rs:292-364`, `src/download/downloader.rs:81-100`).
6. **Retries are 4 attempts per URL** (`retries` 0..=3) then the next candidate mirror; a **hash mismatch aborts the whole download run** (`src/download/uri.rs:95-115, 178-228`).
7. **`fetch` scores mirrors by milliseconds to GET `<mirror>/dists/<release>/Release`**, 10 concurrent probes, 1 s timeout, no retries; results sorted ascending and either the top N (`--auto`, bare `--auto` = 3), the top 3 in non-TUI mode, or a TUI multi-select (`src/cmd/fetch/score.rs:16-110`, `src/cmd/fetch/mod.rs:42-101`).
8. **`--machine` output for `list`/`show` is the `rust-apt` `Serialize` impl for `Version`** — one pretty JSON object *per version*, concatenated on stdout (not a JSON array). `policy -m` is a JSON array when package names are given and a global object when they are not (`src/cmd/mod.rs:225-244, 275`, `src/cmd/policy.rs:255-316`, `rust-apt-0.11.3/src/iterators/serde.rs:23-61`).

---

## 1. History

### 1.1 On-disk layout

| Path | Purpose | Source |
|---|---|---|
| `/var/lib/nala/history/` | Default history directory (`HISTORY_DEFAULT`) | `src/config/paths.rs:1, 37, 62` |
| `<dir>/<id>.json` | One `HistoryEntry`, pretty JSON + trailing `\n` | `src/cmd/history/store.rs:227-241` |
| `<dir>/<id>.json.tmp` | Transient write target, renamed over `<id>.json` | `src/cmd/history/store.rs:227-241` |
| `<dir>/.legacy-history-handled` | Marker: legacy `history.json` already imported | `src/cmd/history/store.rs:13, 269-271` |
| `<dir>.json` (sibling) | Legacy single-file store written by older nala releases (`history.json`) | `src/cmd/history/legacy.rs:37-41` |
| `<parent>/.<dir-name>.importing-<pid>/` | Staging directory used while migrating legacy history | `src/cmd/history/store.rs:256-263` |
| any other filename | Ignored: reader accepts only stem-parseable-`u32` `.json` files | `src/cmd/history/store.rs:66-72, 96-118`, `src/cmd/history/tests.rs:547-563` |

Resolution order for the directory: `NALA_HISTORY_DIR` env var → hidden global `--history-dir` CLI flag → compile-time default (`src/config/mod.rs:112-118`, `src/cli/parser.rs:24-26`, `src/config/configuration.rs:153-156, 177-186`).

Writes are **atomic-ish**: serialize to `<id>.json.tmp`, then `std::fs::rename` over the final name; there is no `fsync` of file or directory (`src/cmd/history/store.rs:230-241`). Every mutating history operation first sweeps stale state: removes `<u32>.json.tmp` files and leftover `.<name>.importing-*` directories, leaving unrelated files alone (`src/cmd/history/store.rs:16-64`, `src/cmd/history/tests.rs:599-621`).

### 1.2 JSON schema, version 1

`schema_version` is a hard constant `HISTORY_SCHEMA_VERSION: u32 = 1` (`src/cmd/history/model.rs:9`). The canonical fixture `tests/deb/history/rust-v1.json` is:

```json
{
  "schema_version": 1,
  "id": 7,
  "started_at": "2026-04-22T19:30:47.432161415+00:00",
  "finished_at": "2026-04-22T19:30:55.983496201+00:00",
  "status": "Applied",
  "requested_by": "beta-user (1000)",
  "command": "beta full-upgrade",
  "requested_targets": [],
  "altered": 1,
  "packages": [
    {
      "name": "beta-package",
      "size": 10,
      "operation": "Upgrade",
      "before": { "version": "1.0", "auto_installed": false, "config_files_only": false },
      "after":  { "version": "2.0", "auto_installed": false, "config_files_only": false }
    }
  ]
}
```

The **writer never emits `altered`** — that field comes from the previous (Python/beta) nala schema and is silently ignored by serde because `HistoryEntry` has no `deny_unknown_fields` (`src/cmd/history/model.rs:29-40`; test explicitly proves this tolerance at `src/cmd/history/tests.rs:331-337`; the integration verifier also asserts a freshly written entry has no `altered`, `tests/deb/history/verify.py:44`).

Field-by-field:

| Field | Type | Semantics / source |
|---|---|---|
| `schema_version` | `u32` | Constant `1`; required on read. Missing/invalid → hard error (`src/cmd/history/store.rs:125-137`). |
| `id` | `u32` | Transaction ID; must equal the numeric filename (`src/cmd/history/store.rs:141-147`). |
| `started_at` | RFC 3339 string | `Utc::now().to_rfc3339()` captured **after downloads, immediately before dpkg** (`src/summary.rs:346`); nanosecond precision, `+00:00` suffix. |
| `finished_at` | RFC 3339 string | `Utc::now().to_rfc3339()` after `dpkg::run_install` returns `Ok` (`src/summary.rs:382`). |
| `status` | `"Applied"` \| `"Unknown"` | Unit enum, serialized as its variant name; `Unknown` is the `Default` and is never written by this code (`src/cmd/history/model.rs:12-17, 57`). |
| `requested_by` | string | `"{username} ({uid})"`. UID from `SUDO_UID` else effective UID; username from `SUDO_USER`, then `LOGNAME`, `USER`, `LNAME`, `USERNAME`, else localized "unknown" (`src/cmd/history/model.rs:51-58`, `src/util/privilege.rs:30-43`). |
| `command` | string | `std::env::args().skip(1).join(" ")` — the **entire argv after the program name**, including flags and `-o` options (`src/cmd/history/model.rs:59`). |
| `requested_targets` | `string[]` | Raw CLI package specs from config key `PKG_NAMES` — not resolved package names, not deduped (e.g. `["nala-history-test=1.0"]`; `src/cmd/history/model.rs:60-63`, verifier at `tests/deb/history/verify.py:48-58`). |
| `packages` | `PackageTransition[]` | The changed rows; see below. |

`PackageTransition` (`src/libnala/transaction.rs:156-165`):

| Field | Type | Notes |
|---|---|---|
| `name` | string | Package name only (no arch). |
| `size` | `u64` | Download size in bytes (`version.size() as usize` → `u64`; `src/download/uri.rs:63`). |
| `operation` | enum string | `Remove`, `AutoRemove`, `Purge`, `AutoPurge`, `Install`, `Reinstall`, `Upgrade`, `Downgrade`, `Configure`, `Held` (`src/libnala/transaction.rs:9-21`). Serde writes the **variant name** (`Reinstall`), even though `AsRef<str>`/`Display` use `ReInstall` (`src/libnala/transaction.rs:63-78`). |
| `before`, `after` | `PackageState` | `{version: Option<String>, auto_installed: Option<bool>, config_files_only: bool}` (`src/libnala/transaction.rs:113-119`). |
| `held_reason` | optional enum | `Excluded`, `ManualHold`, `PhasedUpdate{percentage}`, `KeptBack`; **omitted from JSON when `None`** (`src/libnala/transaction.rs:90-97, 163-164`). |

Serialization details:

- Two-space pretty JSON with exactly one trailing newline (`src/cmd/history/store.rs:230-232`).
- `held_reason` is the only conditional field inside a transition; the `before`/`after` objects always contain all three keys because none are `skip_serializing`.
- All keys for `HistoryEntry` are always present (no `skip_serializing_if` on the entry).
- Omitted `Option` fields inside `PackageState` serialize as `null`, not absent.

Versioning policy on read (`src/cmd/history/store.rs:120-150`): parse to `serde_json::Value` first, require `schema_version` (else `"has no valid schema_version"`), require it equals `1` (else `"uses unsupported schema version N; supported version is 1. Use a compatible Nala version or move this file aside"`), then deserialize and require `entry.id == filename_id` (else `"contains ID X; expected Y from its filename"`). There is no forward migration.

### 1.3 ID allocation and selectors

```
next_id(store) = max(entry.id for entry in get_history(store)) + 1     # src/cmd/history/store.rs:153-160
```

- `get_history` itself includes the merged legacy view, so IDs continue after imported entries (`src/cmd/history/store.rs:75-94`).
- IDs are allocated **before** dpkg but persisted only when the install succeeds (`src/summary.rs:359`, `373-389`). A failed/interrupted/refused transaction writes no file, so the next transaction reuses that ID; there is **no gap-tracking counter**.
- Clearing the newest entry lowers the next ID: `history clear last` then a new transaction reuses the cleared number (clear deletes the file, `src/cmd/history/store.rs:209-212`; allocation is pure `max+1`).
- Selector grammar: `last` (case-insensitive) or a decimal `u32`; anything else is a clap parse error (`src/cli/commands.rs:195-213`). `Last` resolves to the **maximum ID**, not the most recent filename mtime (`src/cmd/history/model.rs:84-98`).
- On a wholly empty store, `last` errors with `history-empty`; an unknown numeric ID errors with `history-not-found` (`src/cmd/history/model.rs:89-97`; messages `locales/en-US/main.ftl:426-430`).

### 1.4 Write path: what creates an entry, and when

`HistoryEntry::applied` + `write_to_file` are called **only** from `summary::commit_with_display_rows` (`src/summary.rs:373-389`). There is no "started" record; a transaction that crashes leaves nothing.

Ordering inside the commit (`src/summary.rs:263-400`):

1. Compute auto-removals and sorted change sets (`287-294`).
2. Print summary and prompt (`316-318`).
3. Build downloader for versions whose first URI is not `file:` (`320-336`).
4. If `--print-uris`: print JSON and **return before any history work** (`338-344`).
5. `started_at = Utc::now().to_rfc3339()` (`346`) — i.e. **download time is excluded** from the recorded interval.
6. Download (`351-353`).
7. If `--download-only`: **return; no history entry** (`355-357`).
8. `history_id = prepare_history_store(config)` (migrates legacy, validates, computes `max+1`) (`359`).
9. Run dpkg pre-hooks, then `dpkg::run_install` (`367-372`).
10. On `Ok`, if the change set is non-empty, write `HistoryEntry` with `finished_at` now (`373-386`).
11. Run post-hooks; propagate `install_result`, then `history_result`, then `post_invoke_result` (`390-395`).

Consequences worth encoding in xbps equivalents:

- `update` creates no history (it does not go through `commit`; dispatch at `src/main.rs:124`, `src/cmd/update.rs:74-160`).
- `--download-only` and `--print-uris` create no history.
- **A dpkg post-hook failure (or any failure after dpkg succeeded) still leaves a history entry**, because the entry is written before `DPKG::Post-Invoke` runs and the error is raised after (`373-394`; integration case "post-hook failure preserves state and history", `tests/deb/history/run:232-239`).
- **A failing dpkg, a failing pre-hook, an interrupted postinst, or `--assume-no` produce zero entries** (`tests/deb/history/run:210-239`, `run:281-305`).
- All package rows are recorded, including `Configure` and `Held`, but only replayable rows matter to replay (§1.6). Held rows are excluded from the display "Altered" count (`src/cmd/history/model.rs:69-73`).
- `HistoryEntry` is also written by replay, since undo/redo ends in `crate::summary::commit` (`src/cmd/history/replay.rs:88`).

### 1.5 Listing, detail, and TUI

`history` dispatch (`src/cmd/history/mod.rs:20-88`):

```
if clear --all: sudo_check; clear_history(all); print count; return        # 21-28
entries = get_history()                                                    # 30
if undo/redo: find_selector; entry.undo/redo(config).await                  # 32-40
if clear: clear_history(selector|all); print per-id confirmation; return   # 42-51
selector = (info ? info.id : args.history_id)                              # 53-56
if no selector: empty ? "No history entries found." : list_table           # 58-66
entry = find_selector(selector)                                            # 68
pkg_set = entry.grouped_packages()                                         # 69
if pkg_set empty or !use_fullscreen_ui: entry.print_detail(config)         # 71-79
else: fullscreen read-only SummaryTab::for_history(...)                    # 81-85
```

- **List table** columns: ID, Command, Date and Time, Requested-By, Altered (`src/cmd/history/view.rs:26-51`). Timestamps are parsed as UTC and rendered in local time as `%Y-%m-%d %H:%M:%S %Z`; unparseable strings are shown verbatim (`src/cmd/history/view.rs:14-23`; test `src/cmd/history/tests.rs:47-53`). The table is `comfy_table` with the `NOTHING` preset, last column right-aligned (`src/table.rs:6-18`).
- **Detail view** (used by both `history <id>` and `history info <id>`; `info` has alias `show`, `src/cli/commands.rs:154-157`): ID, Status, Command, Requested-By, Started, Finished, Requested Targets (`None` when empty), Altered, then one table per operation in the fixed `Operation::to_vec()` order (Remove, AutoRemove, Purge, AutoPurge, Install, Reinstall, Upgrade, Downgrade, Configure, Held), skipping empty groups (`src/cmd/history/view.rs:65-118`, `src/libnala/transaction.rs:24-37`).
- **TUI detail** is only reachable when the config UI mode is `Tui` (global `--tui` or `Ui.mode = "tui"`) *and* stdout is a TTY *and* `TERM != dumb` (`src/cmd/history/mod.rs:76-79`, `src/terminal.rs:36-54`, `src/config/configuration.rs:190-202`, `src/config/file.rs:13-21`). It is read-only: `confirm_enabled: false` and title `tui-history-title` (`src/tui/summary/mod.rs:213-232`).
- `history` listing itself is read-only: no sudo, no APT lock (`src/main.rs:166-175`).

### 1.6 Replay algorithm (`undo` / `redo`)

Entry points: `HistoryEntry::undo` / `redo` (`src/cmd/history/replay.rs:28-47`), both:

```
prepare_replay:
  if status != Applied: error "not replayable ... was not recorded as applied"   # 50-55
  if replayable().next() is None: error "has no package changes to replay"       # 57-59
  sudo_check(config)                                                             # 61
  config.set_bool(NO_AUTO_REMOVE, true)                                          # 62
  Cache::new()                                                                   # 64
for package in replayable():
  undo: package.mark_undo(&cache)   # -> undo_action().apply()
  redo: package.mark_redo(&cache)   # -> redo_action().apply()
commit_replay:
  cache.resolve(fix_broken=false); on failure dump broken pkgs and bail         # 68-76
  if any (installed, target) with target < installed:
      apt.set("APT::Get::allow-downgrades", "true")                             # 79-86
  summary::commit(cache, config)   # prompts, downloads, dpkg, writes history   # 88
```

`replayable()` excludes only `Configure` and `Held` (`src/cmd/history/model.rs:76-80`, `src/libnala/transaction.rs:54`). Rows that are replayable but unsupported throw during action computation, aborting the replay before any commit (nothing has been marked into a *committed* cache at that point).

**Undo mapping** (`src/cmd/history/replay.rs:163-236`):

| Recorded operation | Undo action | Failure condition |
|---|---|---|
| `Install`, `before.config_files_only == true` | `Remove { purge: false }` (leaves config files) | — |
| `Install`, `before` was entirely missing (`version == None && !config_files_only`) | `Remove { purge: true }` | — |
| `Install`, `before.version == Some(v)` | `Install { version: v, auto_installed: before.auto_installed }` | before version missing |
| `Remove`/`AutoRemove`/`Purge`/`AutoPurge`/`Upgrade`/`Downgrade` | `Install { version: before.version, auto_installed: before.auto_installed }` | `before.config_files_only` (restoring config-only state unimplemented) or missing version |
| `Reinstall` | — | always errors ("reinstall has no recorded inverse") |
| `Configure` | — | always errors |
| `Held` | — | always errors |

Note the asymmetry: undo of a `Purge` re-installs the package (it cannot restore "config files only" as an intermediate), and undo of an `Install` that replaced an older installed version is an **install of the older version**, i.e. a downgrade.

**Redo mapping** (`src/cmd/history/replay.rs:239-288`):

| Recorded operation | Redo action |
|---|---|
| `Install`, `Upgrade`, `Downgrade` | `Install { version: after.version, auto_installed: after.auto_installed }` |
| `Remove`, `AutoRemove` | `Remove { purge: false }` |
| `Purge`, `AutoPurge` | `Remove { purge: true }` |
| `Reinstall` | `Reinstall { version: after.version, auto_installed }` (requires the package to be installed) |
| `Configure` | error |
| `Held` | error |

**Action application** (`src/cmd/history/replay.rs:92-160`):

```
pkg = cache.get(name) or error
cache.resolver().clear(&pkg); pkg.protect()          # don't drag dependencies along
Install:   ver = pkg.get_version(recorded) or error
           ver.set_candidate()
           mark_install(deps=true, user=true) || mark_install(false, true) or error
           if recorded auto_installed: pkg.mark_auto(...)
Reinstall: require pkg.installed().is_some(); ver.set_candidate(); pkg.mark_reinstall(true)
Remove:    pkg.mark_delete(purge)
```

Safety / behaviour rails:

- `sudo_check` is required (`src/cmd/history/replay.rs:61`), and because `history undo|redo|clear` count as mutating (`src/main.rs:155-169`), the parent also takes the APT lock before dispatch (`src/main.rs:81-87`).
- Implicit autoremove is disabled for replay via `NO_AUTO_REMOVE` (`src/cmd/history/replay.rs:62`), so an undo does not sweep unrelated orphans.
- Downgrades are permitted automatically for replay by setting `APT::Get::allow-downgrades` when the solver produced a lower target (`src/cmd/history/replay.rs:79-86`); this also satisfies the unattended-safety check (`src/summary.rs:235-248`).
- `--assume-no` and `--assume-yes` are **not available as flags on `history`** (the `History` args are only an optional selector and a subcommand, `src/cli/commands.rs:139-149`). Replay confirmation goes through the ordinary prompt, so `assume_yes` only takes effect from the config file (`Nala.assume_yes`) — `src/util/prompt.rs:18-25`, `src/summary.rs:27-40`. A non-interactive stdin returns "no" on EOF and replay aborts with `prompt-refused` (`src/util/prompt.rs:32-35`).
- Because replay runs the full commit, **it writes a new history entry** whose operations are the *actual* solver operations (e.g. undo of an `Upgrade` records a `Downgrade`; redo records an `Upgrade`). The integration test asserts the exact sequence `Upgrade Install Remove Purge Upgrade Remove Install Install Remove Install Purge Downgrade Upgrade` after 8 replays over 5 imported entries (`tests/deb/history/run:122-147`; verifier compares operation lists at `tests/deb/history/verify.py:59-63`).
- Replay does not consult `finished_at`/`started_at` and does not verify that the package is still in the recorded `after` state; the only consistency checks are that the package (and, for versioned actions, the recorded version) still exists in the current APT cache, and that a redo-reinstall target is currently installed.

### 1.7 `clear` and `info` semantics

- `history clear --all`: short-circuits **before** loading the store (`src/cmd/history/mod.rs:21-28`), requires root, then deletes every numeric `<u32>.json` file under the history dir and returns the count (`src/cmd/history/store.rs:178-202`). Non-numeric files (e.g. `1.json.bak`, the legacy marker) survive; a corrupt `3.json` is still deleted and counted (test `src/cmd/history/tests.rs:522-544`). Output: `history-cleared` with count (`src/cmd/history/mod.rs:25`).
- `history clear <id|last>`: loads entries, resolves selector, deletes `<id>.json`, prints `Cleared history entry <id>.` (`src/cmd/history/mod.rs:42-50`, `src/cmd/history/store.rs:204-213`). The `target` arg group makes one of `history_id`/`--all` mandatory and they conflict (`src/cli/commands.rs:174-193`).
- Both forms first run legacy migration, so clearing a *legacy-derived* ID materialises the imported store, deletes the migrated `<id>.json`, sets `.legacy-history-handled`, and leaves the legacy `history.json` byte-identical (`src/cmd/history/store.rs:176`, `src/cmd/history/store.rs:247-314`; test `tests/deb/history/run:151-165`; `tests/deb/history/verify.py:31-35`).
- `history info <id>` (alias `history show <id>`) cannot list: it always requires the target (`src/cli/commands.rs:154-157`, `src/cmd/history/mod.rs:53-56`). `history` with no selector lists all; `history <id>` is equivalent to `info <id>` (`src/cli/commands.rs:139-149` with `args_conflicts_with_subcommands = true`, so `history 2 undo` is invalid; use `history undo 2`).

### 1.8 Legacy migration

- Legacy path = `<history-dir>.json` (string-concatenated sibling, `src/cmd/history/legacy.rs:37-41`), shape: JSON object keyed by numeric string ID plus a `"Nala"` metadata key which is skipped (`src/cmd/history/legacy.rs:43-67`).
- Legacy fields are PascalCase (`Date`, `Requested-By`, `Command[]`, `Purged`, `Explicit[]`, `Removed[][]`, `Auto-Removed[][]`, `Installed[][]`, `Reinstalled[][]`, `Upgraded[][]`, `Downgraded[][]`), with `#[serde(default)]` so missing arrays are fine (`src/cmd/history/legacy.rs:12-28`).
- Purging is inferred from `Purged: true` **or** a first command word of `purge`/`autopurge` (`src/cmd/history/legacy.rs:70-75`).
- Rows are `[name, version, size]` for install/remove/reinstall, `[name, new_version, size, old_version]` or `[name, old_version, new_version, size]` for upgrade/downgrade; the parser prefers the "modern" interpretation when the second element parses as a size, mirroring the old Python reader (`src/cmd/history/legacy.rs:149-175`). Versions `null`, `""` and `"None"` become `None` (`src/cmd/history/legacy.rs:185-192`). Sizes are decimal strings; a non-numeric size is a hard error.
- Conversion builds synthetic before/after states (`src/cmd/history/legacy.rs:109-147`): Install: config-only(None) → installed(version); Reinstall: installed → same; Upgrade/Downgrade: installed(old) → installed(new); Remove/AutoRemove: installed(v) → config-only(v); Purge/AutoPurge: installed(v) → missing. Legacy rows have **no `auto_installed` information** (always `None`), and `started_at == finished_at == Date`, `status = Applied` (`src/cmd/history/legacy.rs:91-105`).
- Migration triggers only on mutating history paths: `write_to_file`, `clear_history`, `prepare_history_store` (before package mutation) (`src/cmd/history/store.rs:163-166, 176, 219, 247-314`). Plain `history` listing only **merges** legacy records into the in-memory view, renumbering them `1..N` and appending current entries after (`src/cmd/history/store.rs:75-94`) — it does not write anything.
- Migration mechanics: read merged entries, write them (renumbered) into `.<name>.importing-<pid>/`, add the marker file inside staging, then atomically exchange the directories with `renameat2(RENAME_EXCHANGE)` (fallback `rename` if the target did not exist), then delete the old directory (`src/cmd/history/store.rs:260-294`). On any error the staging directory is removed and the legacy file/store are untouched (test `src/cmd/history/tests.rs:390-404`).
- A conversion failure therefore **blocks package mutation**, because `prepare_history_store` runs before dpkg (`src/summary.rs:359`; integration case "failed conversion blocks package mutation", `tests/deb/history/run:263-279`).

### 1.9 Failure modes

| Failure | Behaviour | Source |
|---|---|---|
| `<id>.json` unparseable / missing `schema_version` / wrong version | `get_history` returns error; every history read/write fails until the file is moved aside. `clear --all` still works **if no legacy file is present**, because it bypasses `get_history`. | `src/cmd/history/store.rs:120-137`, `src/cmd/history/mod.rs:21-28`, `src/cmd/history/tests.rs:565-597` |
| `id` field ≠ filename | hard error | `src/cmd/history/store.rs:141-147` |
| Stale `.tmp` or importing dirs | silently removed on the next mutating history op | `src/cmd/history/store.rs:16-64` |
| Legacy conversion failure | error; no mutation; legacy + current files unchanged; marker absent | `src/cmd/history/store.rs:296-306`, `tests/deb/history/run:263-279` |
| Replay of `status != Applied` | error before cache creation | `src/cmd/history/replay.rs:50-55` |
| Replay of a `Configure`/`Held`-only entry | `history-no-changes` | `src/cmd/history/replay.rs:57-59` |
| Undo hits a `Reinstall` row (Configure/Held rows are filtered out and never marked) | error, no commit | `src/cmd/history/replay.rs:222-228, 265-280` |
| Recorded version no longer in cache | `pkg-version-cache-missing` | `src/cmd/history/replay.rs:105-114, 129-138` |
| Broken dependency resolution | dump `show_broken_pkg` lines and bail | `src/cmd/history/replay.rs:68-76` |
| Not root | `root-required` error before anything | `src/cmd/history/replay.rs:61`, `src/util/privilege.rs:22-27`, `src/main.rs:81-87` |
| Prompt refused / EOF / `--assume-no` | `prompt-refused`, exit non-zero; no entry | `src/util/prompt.rs:9-15, 32-35`, `src/summary.rs:316-318` |
| Concurrent transaction | serialised by APT lock taken in `main` for history mutations, installs, removes, upgrades, fetch, clean, update | `src/main.rs:81-87`, `153-176` |

### 1.10 Facts downstream ticket #46 needs

1. Store granularity is **per successful transaction**, one file, no journal, no "started" record: you cannot reconstruct a failed/interrupted transaction from history.
2. The replayable unit is a **package row with before/after state**, not an argv string; `command`/`requested_targets` are display metadata only.
3. Replay is **forward re-execution through the normal solver**: it re-resolves dependencies, re-downloads, re-prompts, and writes a *new* entry. It is not a state rollback and has no idempotence check.
4. **Configure and Held rows are recorded but never replayable**; Reinstall is redo-only.
5. Undo/redo **cannot express config-files-only restoration** and errors for purge-undo of config-only rows and for undo of reinstall.
6. There is **no `--machine` output for history**; the only machine-readable artefact is the on-disk v1 JSON.
7. `history undo` and `history redo` accept **no** `-y/--assume-yes`; batch replay requires `Nala.assume_yes = true` in the config file (or a TTY prompt).
8. ID semantics: `max+1`, gap-free only while nothing fails; `last` = max ID. A replayed transaction receives the next ID, so replay chains grow the store.

---

## 2. Parallel downloader

### 2.1 Object model and call sites

- `Downloader` owns a `reqwest::Client`, `AuthConf`, `UriFilter`, archive/partial dirs, a `DomainMap`, a `tokio::task::JoinSet<Result<Uri>>`, and an unbounded progress channel (`src/download/downloader.rs:194-207`).
- One `Uri` represents one package file: ordered candidate URLs (`VecDeque`), expected size, archive/partial paths, optional `HashSum`, filename, retry counter (`src/download/uri.rs:21-38`).
- Call sites:
  - install/remove/upgrade/autoremove: `Downloader::new` → `add_version` per version (skipping versions whose first URI is `file:`) → `run(config, rm_partial = false)` (`src/summary.rs:325-353`).
  - `nala download`: `Downloader::new` → `add_version` for the first downloadable version of each package → `run(config, rm_partial = true)` (`src/download/downloader.rs:23-74`).
  - direct `http(s)://…` package arguments to install: `add_from_cmdline` → `run(config, true)` → downloaded file treated as a local `.deb` (`src/cmd/install.rs:12-52`).

### 2.2 Concurrency and per-domain scheduling

Constants and state (`src/download/mod.rs:14-34`):

- `DOMAIN_CONNECTION_LIMIT = 3` — **maximum concurrent connections per domain**, not a global limit.
- `Domain { active: IndexSet<String>, exclusive: IndexSet<String> }` keyed by domain string in an `IndexMap`; FIFO `VecDeque<Waiter>`; `next: usize` rotation cursor.

Registration (`src/download/mod.rs:97-107`): every candidate domain of every URI is registered; a package with **exactly one candidate** is additionally recorded as `exclusive` in that domain.

Reservation (`src/download/mod.rs:37-63`, pseudocode):

```
reserve(package, candidates):
  start = next % len(candidates); next += 1
  for offset in 0..len:
    name = candidates[(start + offset) % len]
    domain = domains[name]
    exclusive = package in domain.exclusive
    if domain.active.len() >= 3:              continue
    if !exclusive && !domain.exclusive.is_empty(): continue   # keep scarce domains for single-mirror pkgs
    domain.active.insert(package); domain.exclusive.remove(package)
    return name
  return None
```

- The cursor rotation means the *first-choice* mirror varies across packages, so all configured mirrors get used even though each is capped at 3 (test asserts each of four mirrors receives exactly 3 of 12 packages, `src/download/uri.rs:458-492`).
- If no slot is free, `acquire` enqueues a waiter and awaits a oneshot; `remove` (called when a transfer finishes/fails) frees the slot and runs `dispatch`, which hands freed slots to waiters **in FIFO order** (`src/download/mod.rs:109-160`; tests `src/download/mod.rs:192-247`).
- `acquire_uri` selects the actual URL corresponding to the granted domain, removes it from the candidate deque, and returns `(url, domain)` (`src/download/uri.rs:132-147`).
- Overall task count is unbounded: `download()` spawns one task per `Uri` into the `JoinSet` (`src/download/downloader.rs:273-282`); backpressure comes only from domain slots. Memory is one task per file plus an open HTTP connection only when a slot is held.

### 2.3 Per-file pipeline

`Uri::download` (`src/download/uri.rs:149-229`):

```
if archive_file exists:
    if hash known and file hashes equal:
        if size == 0: size = metadata len (and AddTotal)
        Update{bytes: size, domain: None}         # counted as "cached"
        domains.cancel(filename)                  # drop exclusive reservation/waiters
        Finished(filename); return Ok
    remove archive_file unconditionally
while let Some((url, domain)) = acquire_uri():
    retries = 0
    while retries <= 3:
        bytes_downloaded = 0
        match download_file(url):
            Ok(hash):
                domains.remove(domain)
                check_hash(hash)?                 # may delete partial + Message::Exit + bail
                partial.rename(archive)
                Finished(filename); return Ok
            Err(e):
                retries += 1
                NonFatal{error, bytes: bytes_downloaded, domain}
    domains.remove(domain)                        # this mirror exhausted; try next candidate
Message::Exit
bail "No URIs could be downloaded for <file>"
```

- Retry count: **4 attempts per URL** (`retries` starts at 0, loop condition `<= 3`), with no delay/backoff; after that, the next candidate URL is used. All candidates exhausted → the whole run aborts (`Message::Exit` breaks the progress loop).
- Partial files are re-opened with `File::create` on every attempt, i.e. **truncated, no resume** (`src/fs.rs:69-85`). There is no Range request logic anywhere in `download_file` (`src/download/uri.rs:232-289`).
- `download_file` streams chunks, feeds the hasher, updates progress per chunk (domain-attributed), and writes through a `BufWriter`; it flushes at the end but does not fsync (`src/download/uri.rs:257-288`).
- Content-Length: if the expected size was 0, it is learned from the response and added to the progress total (`src/download/uri.rs:248-255`).
- On a hash mismatch the partial file is deleted, `Message::Exit` is sent (global abort), and the error is `download-checksum` (`src/download/uri.rs:95-115`).

### 2.4 Verification and trust

- Hash selection: `sha512`, else `sha256`, else hard error `hash-unavailable`; the code comment cites Debian policy that MD5/SHA1 must not be used for security (`src/hashsum.rs:15-35`). `HashSum` is a two-variant enum (`Sha512`, `Sha256`) that serializes as `{"Sha512": "hex"}` (`src/hashsum.rs:87-133`).
- `HashSum::from_path` reads 4096-byte chunks; `get_hasher` rejects any other type (`src/hashsum.rs:45-133`).
- No per-package signature or InRelease verification exists in nala's download path. Trust is inherited from APT: `UriFilter::uris` checks `pf.index_file().is_trusted()` and records the **package name** as untrusted when false (`src/download/uri.rs:322-337`). Separately, every URI whose `hash.is_none()` is added to the same untrusted set at run start (`src/download/downloader.rs:303-315`).
- Untrusted non-empty → warning + list, then hard error unless `--allow-unauthenticated` or `APT::Get::AllowUnauthenticated` is set (`src/download/downloader.rs:81-100`, `src/config/configuration.rs:253-256`).
- If a hash is absent, `check_hash` **no-ops** (comment warns it will not error), and the hasher defaults to SHA512 only for computing a value that is never compared (`src/download/uri.rs:95-100, 260-262`).

### 2.5 Transport: proxy, auth, timeouts, redirects

- Client builder sets only `connect_timeout(30s)`, `read_timeout(120s)`, and the proxy (`src/download/downloader.rs:218-223`). There is **no overall request timeout** and **no redirect policy configured**, so reqwest's defaults apply (redirects followed; the exact hop limit is a reqwest-version property, not visible in nala source).
- Proxy (`src/download/proxy.rs:75-171`): reads `Acquire::{http,https}::Proxy` (global value + per-domain children) and `Acquire::{http,https}::Proxy-Auto-Detect` (modern spelling) with legacy `ProxyAutoDetect` fallback. Values: `direct`/`false` → no proxy; otherwise `http`, `https`, or `socks5h` URL. Auto-detect runs the command with the URL argument, takes the first stdout line, `DIRECT` or a proxy URL; results are cached per `scheme://host` in a `Mutex<HashMap>`. Precedence: explicit per-domain → auto-detect → per-scheme default (`src/download/proxy.rs:129-170`).
- Auth (`src/download/auth.rs:21-82`): reads `Dir::Etc::netrc` (default `/etc/apt/auth.conf`) then every `*.conf` in `Dir::Etc::netrcparts` (default `/etc/apt/auth.conf.d/`), sorted. Netrc-style `machine … login … password …` tokens; files are consulted in order and the **first matching entry overall** wins; missing files are fine. Matching supports `machine host`, `machine host:port`, `machine scheme://host`, and `machine scheme://host/pathprefix`; a scheme-less machine only matches `https`/`tor+https` (`src/download/auth.rs:85-112`). Credentials are sent as HTTP Basic auth, and are skipped if the URL already carries userinfo (`src/download/auth.rs:55-82`).

### 2.6 URL sources, mirrors, local files

`UriFilter::uris` (`src/download/uri.rs:292-364`) builds the candidate deque from the APT version's version files:

1. Skip non-downloadable package files (`325-327`).
2. Untrusted index file → record package name (`329-334`).
3. `file:` URI → **copy the file into the archive dir** using the source basename (`341-346`) and still push the URI (see edge case below).
4. Mirror-base handling: if the index's archive URI is `mirror://host/list` (rewritten to `http://host/list`) or `mirror+http(s)/file:` (`mirror_location`, `403-418`), fetch that list once per downloader (`add_to_mirrors`, `366-387`; cached at `351-353`) and expand each non-comment line's first whitespace token into `{mirror}/{package_path}` (`390-401`; test `428-456`). The package path is percent-encoded, e.g. `pool/main/z/zstd/zstd_1.5.7%2bdfsg-1_arm64.deb`.
5. Otherwise push the archive URI unchanged.

Direct CLI URLs: `parse_cli_uri` accepts only `http`/`https`, extracts the last path segment as filename, and accepts an optional trailing `:<64 or 128 hex>` digest as SHA256/SHA512 (`src/download/downloader.rs:434-465`; tests `471-505`). Without a digest it warns `download-hash-missing` and the file lands in the untrusted set, requiring `--allow-unauthenticated` (`src/download/downloader.rs:248-259`, `303-319`).

Edge case: `file:` URIs are copied into the archive but never removed from the candidate deque; `summary::commit` avoids this by skipping downloader registration for versions whose first URI starts with `file:` (`src/summary.rs:328-335`), but `nala download` does not, so a cache-only/`file:` version can be registered and then fail the HTTP fetch after the copy (code-path inference; no test covers it).

### 2.7 Progress integration

- `Progress::download(config, domain_count)` builds the download viewport; total length is the sum of expected sizes, with a per-file item counter (`src/download/downloader.rs:346-355`, `src/progress.rs:405-407`).
- The run loop ticks every **150 ms**, drains progress messages, re-renders, and refreshes the mirror table (active count/limit/rate) (`src/download/downloader.rs:360-424`). Mirror rows are `MirrorProgress { name, active, limit: 3, rate, state }`, where the rate is a byte/sec average that pauses while idle (`src/download/downloader.rs:120-192`, `src/progress.rs:95-137`).
- Messages: `AddTotal(size)`, `Update{bytes, domain}` (domain `Some` = network, `None` = cached/hash-matched file), `Finished(filename)` ("Last completed"), `NonFatal` (printed error + progress decrement), `Debug`, `Verbose` (only with `--verbose`), `Exit` (global abort) (`src/download/downloader.rs:102-118`, `src/download/downloader.rs:369-409`).
- `q` or Ctrl-C during the loop aborts: shuts down the `JoinSet` and errors `download-exit` (`src/download/downloader.rs:411-415`, `src/terminal.rs:56-69`). The listener only exists when stdin is a TTY.
- Rendering is hidden when stderr is not a TTY or `TERM=dumb` (`src/progress.rs:286`, `src/progress.rs:356-368`).
- The partial directory is created before spawning and removed at the end **only if `rm_partial`** (`src/download/downloader.rs:273-288`); installs leave it in place, `download` and direct-URL installs remove it.

### 2.8 Pre-flight checks

- Disk space: `required = Σ required_download_size()` (0 for an already-valid archive file), compared against `statvfs(archive).blocks_available * fragment_size`; insufficient space aborts before any transfer with a localized message (`src/download/downloader.rs:321-338`, `src/download/uri.rs:41-52`).
- `archive_dir.mkdir()` and `partial_dir.mkdir()` are create-all; failures are contextual errors (`src/download/downloader.rs:275, 321`, `src/fs.rs:77-85`).
- Empty URI list: `run` bails `download-failed` if `finish` returns no files (`src/download/downloader.rs:426-430`); callers guard by checking `uris().is_empty()` (`src/summary.rs:351`).

### 2.9 Failure modes

| Failure | Effect |
|---|---|
| 4 failed attempts on a URL | Next candidate mirror; if none, whole run aborts (`download-no-uris`) |
| Hash mismatch | Partial deleted; whole run aborts (`download-checksum`); already-downloaded good files remain in archive |
| Untrusted package / hash-less CLI URI without `--allow-unauthenticated` | Abort before any transfer (`download-auth-required`) |
| Insufficient disk space | Abort before transfer |
| User `q`/Ctrl-C | Tasks shut down; `download-exit` error |
| HTTP error status | Treated as non-fatal attempt; retried |
| Redirects | Followed by reqwest defaults; final URL not re-checked for domain accounting (domain comes from the original URL) |

### 2.10 Facts downstream ticket #47 needs

1. The parallelism control is **per-domain 3**, not a global N; a global limit would be a behavioural change.
2. The scheduler distinguishes `exclusive` packages (single candidate) and withholds domains from multi-mirror packages while exclusives are pending — a fairness rule, not just a mutex.
3. First-choice mirrors rotate; test evidence is a deterministic 3-per-mirror distribution.
4. Retry policy is **4 attempts per URL, no backoff, then failover**, with global abort on checksum mismatch or exhausted candidates.
5. There is **no resume**: partial files are truncated per attempt and only hash-verified after a complete transfer.
6. Verification is sha512→sha256 from repo index metadata; there is no per-file signature; untrusted or hash-less files are gated by an explicit allow flag.
7. `file:` inputs are copied into the archive, not downloaded (and the summary path deliberately never registers them).
8. Progress is fed by an unbounded mpsc channel with 150 ms rendering; cached hits are counted separately from network bytes, and mirror rates pause when idle.

---

## 3. `fetch` (mirror discovery)

### 3.1 Pipeline

`fetch(config)` (`src/cmd/fetch/mod.rs:51-101`), in order:

1. `sudo_check` (`src/cmd/fetch/mod.rs:52`); `fetch` is a mutating command for lock purposes (`src/main.rs:155-169`).
2. Detect `(distro, release, keyring)` (`src/cmd/fetch/mod.rs:54`, `src/cmd/fetch/release.rs:17-46`).
3. Detect component string (`src/cmd/fetch/mod.rs:57`, `src/cmd/fetch/release.rs:48-62`).
4. Parse countries from config (`src/cmd/fetch/mod.rs:60`, `src/cmd/fetch/mod.rs:17-26`).
5. Read existing source domains from disk (`src/cmd/fetch/mod.rs:63`, `src/cmd/fetch/sources.rs:67-113`).
6. Fetch mirror candidates for the distro (`src/cmd/fetch/mod.rs:67`, `src/cmd/fetch/mirrors.rs:15-39`).
7. Remove candidates whose URL contains any already-configured domain string (`src/cmd/fetch/mod.rs:71`, `src/cmd/fetch/mod.rs:28-40`).
8. Score the remainder (`src/cmd/fetch/mod.rs:75`, `src/cmd/fetch/score.rs:16-63`).
9. Empty → `fetch-no-mirrors` (`src/cmd/fetch/mod.rs:78-80`).
10. Choose: `--auto N` → top N; non-TUI → top 3; else TUI (`src/cmd/fetch/mod.rs:83-94`).
11. Empty selection → `fetch-none-selected` (`src/cmd/fetch/mod.rs:96-98`).
12. Write `nala.sources` (`src/cmd/fetch/mod.rs:100`, `src/cmd/fetch/output.rs:51-91`).

No `apt update` is run and no history entry is written (`src/config/configuration.rs:276-305` excludes `Fetch` from `update_early`).

### 3.2 Mirror sources

| Distro | Source | Filtering |
|---|---|---|
| Debian | `https://mirror-master.debian.org/status/Mirrors.masterlist` (tagfile) | `Archive-architecture` must contain **all** local architectures; URL = `http://{Site}{Archive-http}` (`src/cmd/fetch/mirrors.rs:10, 103-113`) |
| Ubuntu | Launchpad `archive_mirrors?ws.size=300`, following `next_collection_link` | `enabled == true`, `status == "Official"`, ports/non-ports matched against local arch (only amd64/i386 ⇒ non-ports), URL preference `http_base_url` → `https_base_url` → `base_url` (`src/cmd/fetch/mirrors.rs:11-12, 41-73, 133-170`) |
| Devuan | `https://pkgmaster.devuan.org/mirror_list.txt` (tagfile) | URL = `http://{BaseURL}/devuan` (`src/cmd/fetch/mirrors.rs:13, 172-174`) |
| anything else | — | empty set ⇒ later `fetch-no-mirrors` (`src/cmd/fetch/mirrors.rs:37`) |

Country filtering (`src/cmd/fetch/mirrors.rs:115-131`): user country values are upper-cased into a set (`src/cmd/fetch/mod.rs:17-26`); Debian matches `CountryCode` split on `|` (trimmed) exactly; fallback matches the **first whitespace token** of `Country` exactly; Ubuntu matches the last path segment of `country_link`. Because matching is exact and case-sensitive after user input is upper-cased, Ubuntu's lowercase country slugs and any name-form `Country` values will not match — an apparent bug, not covered by tests (code-path inference).

Existing-source dedupe is a plain substring test `mirror.contains(source_domain)` (`src/cmd/fetch/mod.rs:28-40`), where source domains come from `Http(s)://` regex captures in `.list` lines and deb822 `URIs:` fields (`src/cmd/fetch/sources.rs:13-65`). `nala.sources` itself is skipped when parsing existing sources (`src/cmd/fetch/sources.rs:90-93`), so a re-run does not dedupe against the file it is about to overwrite.

### 3.3 Scoring formula

```
score_mirrors(mirrors):
  client = Client{ timeout 1s, retry never, pool_max_idle_per_host 0 }   # src/cmd/fetch/score.rs:24-28
  semaphore = 10 concurrent probes                                      # src/cmd/fetch/score.rs:30
  for each mirror (strip one trailing '/'):
      t0 = Instant::now()
      body = GET {url}/dists/{release}/Release  (1s total timeout)      # src/cmd/fetch/score.rs:76-82
      validate_release(body)                                            # src/cmd/fetch/score.rs:84, 88-103
      score = elapsed milliseconds (u128, includes body download+validation)
  sort ascending by score                                               # src/cmd/fetch/score.rs:61
```

`validate_release` requires a single RFC-822 section with `Architectures`, `Components`, and at least one of `SHA256`/`SHA512` (weak `MD5Sum`-only or HTML error pages are rejected) (`src/cmd/fetch/score.rs:88-103`; unit test `src/cmd/fetch/score.rs:116-127`). Fastest mirror = smallest integer millisecond count of a full successful fetch; there is no geographic weighting, no HTTP HEAD, and no re-measurement.

### 3.4 `--auto`, `--https-only`, countries, components

- `--auto [N]`: bare flag = `3`; value range `1..=255` (clap `u8` range 1..) (`src/cli/commands.rs:230-237`; test `src/cli/parser.rs:318-327`). With `--auto`, the top N scored mirrors are taken without confirmation (`src/cmd/fetch/mod.rs:42-48, 83-85`).
- Without `--auto`: if the enhanced UI is unavailable (not a TTY, `TERM=dumb`, or `Ui.mode="plain"`), it silently takes the **top 3** (`src/cmd/fetch/mod.rs:86-88`); in a TTY it opens the multi-select TUI (`src/cmd/fetch/mod.rs:89-94`).
- `--https-only`: does **not** filter; it rewrites the scoring URL's scheme with `replacen("http://", "https://", 1)` before probing (`src/cmd/fetch/score.rs:105-110`), so the rewritten HTTPS URL is what gets scored, selected, and written. Mirrors without HTTPS support simply fail probing.
- `--non-free`: adds `contrib non-free` for Debian/Devuan and appends `non-free-firmware` to Components only if **every** chosen mirror answers `GET /dists/{release}/non-free-firmware/` with 2xx within 1 s (`src/cmd/fetch/release.rs:48-62`, `src/cmd/fetch/output.rs:12-49`). Ubuntu's component string is always `main restricted universe multiverse` (`src/cmd/fetch/release.rs:57-60`).
- `--sources`: writes `Types: deb deb-src` instead of `Types: deb` (`src/cmd/fetch/output.rs:65-69`).
- Release detection: explicit `--debian`/`--ubuntu`/`--devuan` wins (release lower-cased, keyring `/usr/share/keyrings/{distro}-archive-keyring.gpg`); otherwise it probes candidate packages `devuan-keyring`, `debian-archive-keyring`, `ubuntu-keyring`, `apt` and takes the first candidate package file's `Origin`/`Codename`, lower-cased (`src/cmd/fetch/release.rs:8-46`).

### 3.5 TUI flow

`App::run` (`src/tui/fetch.rs:136-171`): full-screen ratatui list of scored mirrors with checkboxes and `"<ms> ms"` scores; keys `j`/Down, `k`/Up, Space toggles, `g`/Home, `G`/End, `q` or Enter returns the selected URLs, Ctrl-C returns an empty vec (clean exit, later `fetch-none-selected`) (`src/tui/fetch.rs:148-166`). TUI is only used when `use_enhanced_ui` is true (stdout TTY, non-dumb term, UI mode not plain) and `--auto` is absent (`src/cmd/fetch/mod.rs:82-94`, `src/terminal.rs:46-49`).

### 3.6 Output file

Fixed path `/etc/apt/sources.list.d/nala.sources` (`src/config/paths.rs:2, 59`; `src/cmd/clean.rs:34` hard-codes the same constant, not an APT-resolved path). Content is written with `fs::write` (not atomic, no temp+rename) (`src/cmd/fetch/output.rs:88`):

```text
# Sources file built for nala

Types: deb
URIs: <mirror1>
      <mirror2>
Suites: <release>
Components: <component string>
Signed-By: /usr/share/keyrings/<distro>-archive-keyring.gpg
```

- `URIs:` continuation lines are indented six spaces (`src/cmd/fetch/output.rs:71-77`).
- No `Enabled:`, `Architectures:` or `Languages:` fields are emitted.
- After writing, prints `Sources have been written to <file>` (`src/cmd/fetch/output.rs:89`, `locales/en-US/main.ftl:480`).

### 3.7 Cleanup / revert

There is no automatic backup, rollback, or disable step. The only cleanup is `nala clean --fetch`, which removes the fixed `nala.sources` path (`src/cmd/clean.rs:33-37`); it errors if the file is absent (plain `remove_file` with context, no NotFound tolerance). Re-running `fetch` simply overwrites the file. Other sources files are never touched.

### 3.8 Facts downstream ticket #48 needs

1. Mirror candidate lists are **tagfile/JSON from Debian/Ubuntu/Devuan**, and Debian candidates are filtered by local architecture, not by country by default.
2. Score = full successful `GET /dists/<release>/Release` body time in ms, 1 s cap, 10 probes at a time, sorted ascending; ties are not defined (stable sort of `JoinSet` completion order).
3. `--auto` (default 3, min 1, max 255) selects the top N **without** interaction; TTY default is a multi-select TUI; non-TTY default is top 3 **silently**.
4. `--https-only` rewrites schemes rather than filtering; `--non-free` is a per-distro component string plus an all-mirrors probe for `non-free-firmware`.
5. Output is a fixed-path deb822 `.sources` file with a `# Sources file built for nala` header, `Types`, indented multi-`URIs`, `Suites`, `Components`, `Signed-By`; written non-atomically.
6. Cleanup is only `clean --fetch` (deletes the file); there is no "revert to previous sources" capability.

---

## 4. `--machine` output

### 4.1 Flag plumbing

`-m/--machine` exists on `list`, `search` (via flattened `List` args), `show`, and `policy` (`src/cli/commands.rs:73-75, 101-103, 113-115`). It is stored as config override `machine` (`src/config/keys.rs:17`, `src/config/configuration.rs:56-95` — every `Some(ValueSource::CommandLine)` clap bool becomes an override). Consumers read `config.get_bool(keys::MACHINE, false)`. `download`, `clean`, `fetch`, and `history` have no machine mode.

### 4.2 `list -m` and `show -m`

Both print **`serde_json::to_string_pretty(&Version)` for one version per call**:

- `ShowVersion::show` (`src/cmd/mod.rs:225-237`): if machine → `println!("{}", self.to_json()?)` and return; else pretty text.
- `ShowVersion::list` (`src/cmd/mod.rs:240-274`): same branch.
- `to_json` is literally `serde_json::to_string_pretty(&self.ver)` (`src/cmd/mod.rs:275`), so the schema is defined by `rust-apt`'s `Serialize for Version`, not by nala (`rust-apt 0.11.3`, `rust-apt-0.11.3/src/iterators/serde.rs:23-61`).
- Because each object is `println!`-ed separately, **multiple packages produce concatenated pretty objects, not a JSON array**; e.g. `nala list -m` with no names emits thousands of objects. There is no separator and no wrapper.

Schema (serializer assembled field-by-field, so **absent record fields are omitted**, not null):

```jsonc
{
  // In RECORDS order, only when the apt record has the field:
  "Package": "nala",
  "Version": "0.18.2",
  "Architecture": "all",
  "Priority": "optional",
  "Essential": "yes",              // only if set
  "Section": "admin",
  "Source": "nala",                // may include "(version)"
  "Installed-Size": "1234",        // KiB (APT convention), string
  "Size": "567890",                // download size, bytes, string
  "Maintainer": "…",
  "Original-Maintainer": "…",      // only if set
  "Homepage": "…",
  "SHA256": "…",                   // only if set
  "package_files": [               // one per APT package file backing this version
    {
      "priority": 500,             // int
      "filename": "/var/lib/apt/lists/…_Packages",
      "archive": "stable",         // or "now" for the dpkg status file
      "origin": "Debian",
      "codename": "bookworm",
      "label": "Debian",
      "component": "main",
      "arch": "amd64",
      "site": "deb.debian.org",
      "index_type": "Debian Package Index"
    }
  ],
  // One key per dependency type that actually exists on the version.
  // Key order is HashMap iteration order and is NOT stable.
  "Depends": [
    // Outer array: one entry per dependency relation (ANDed together).
    // Inner array: alternatives that satisfy that relation (ORed).
    [ { "name": "libc6", "comp": ">=", "version": "2.34" } ],    // one alternative
    [ { "name": "foo", "comp": null, "version": null },           // "foo | bar"
      { "name": "bar", "comp": null, "version": null } ]
  ],
  "PreDepends": [ … ], "Recommends": [ … ], "Suggests": [ … ],
  "Conflicts": [ … ], "Replaces": [ … ], "Obsoletes": [ … ],
  "Breaks": [ … ], "Enhances": [ … ]
}
```

Precise details from `rust-apt-0.11.3/src/iterators/serde.rs`:

- The 13 record keys are exactly `Package, Version, Architecture, Priority, Essential, Section, Source, Installed-Size, Size, Maintainer, Original-Maintainer, Homepage, SHA256` (`rust-apt-0.11.3/src/iterators/serde.rs:7-21`; constants `rust-apt-0.11.3/src/records.rs:30-132`). Records are searched across version files; the first non-`None` wins per field (`rust-apt-0.11.3/src/iterators/serde.rs:29-42`).
- `package_files` is always present (possibly empty), serialized after the record fields (`rust-apt-0.11.3/src/iterators/serde.rs:51-52`). Each file object declares 11 fields but writes 10: `priority, filename, archive, origin, codename, label, component, arch, site, index_type` (`rust-apt-0.11.3/src/iterators/serde.rs:74-90`). Optional values serialize as `null`.
- Dependency keys come from `depends_map()`, so only dependency types present on the version appear; each value is an array of or-groups, each or-group an array of `{name, comp, version}` objects (`rust-apt-0.11.3/src/iterators/serde.rs:54-57, 63-71, 92-100`; `rust-apt-0.11.3/src/iterators/dependency.rs:59-73`).
- `list -m -a` (all versions) emits one object per version; plain `list -m` emits the candidate, or the first version if no candidate (`src/cmd/list.rs:11-43`).
- `show -m` (no `-a`) emits the **first** version returned by APT, and if more versions exist it prints a *human-readable* notice to stderr (`show-more-records`, `src/cmd/show.rs:35-70`) — machine consumers should pass `-a` if they need all.
- Values are raw record strings with no ANSI colouring on the machine path; colour only affects the human paths and logger output.

### 4.3 `policy -m`

Two different top-level shapes (`src/cmd/policy.rs:285-316`):

- With package names: a JSON **array** of `PackagePolicy`, one per selected package, printed pretty (`policy_machine`, `src/cmd/policy.rs:275-283`).
- With no package names: a single JSON **object** `GlobalPolicy` (`policy_global_machine`, `src/cmd/policy.rs:255-259`).

Per-package schema (`src/cmd/policy.rs:13-56, 130-160`):

```jsonc
[
  {
    "package": "apt",                 // APT FullName(pretty=true)
    "installed": "2.6.1",             // null if not installed
    "candidate": "2.6.1",             // null if none
    "versions": [
      {
        "version": "2.6.1",
        "priority": 500,              // priority_with_files(true)
        "installed": true,
        "candidate": true,
        "sources": [
          {
            "priority": 500,
            "source": "stable/main amd64 Packages"   // index describe(true).trim()
          }
        ]
      }
    ]
  }
]
```

- Versions are sorted by `priority_with_files(true)` descending, then by APT version comparison descending (`src/cmd/policy.rs:136-141`). Sources are deduplicated by APT package-file index number, then sorted priority descending, source string ascending (`src/cmd/policy.rs:109-128`).
- `source` is `file.index_file().describe(true).trim()`; for the dpkg status database it is the file's filename or the literal `Debian dpkg status file` (`src/cmd/policy.rs:63-75`).
- The `package` field uses `pkg.fullname(true)` (`src/cmd/policy.rs:155`) — APT's `FullName(pretty)`; rust-apt only forwards to C++ (`rust-apt-0.11.3/apt-pkg-c/package.h:142`), so nala does not define this string.
- Missing packages: machine mode calls `log_missing_notices` which emits `pkg-not-found` via the `info!` logger to **stderr**, then still prints the array of the packages it found (`src/cmd/policy.rs:297-301`, `src/glob.rs:189-199`, `src/config/logger.rs:81-82, 139-149`).

Global schema (`src/cmd/policy.rs:36-56, 162-203`):

```jsonc
{
  "package_files": [
    {
      "priority": 500,
      "source": "stable/main amd64 Packages",
      "release": "release o=Debian,a=stable,n=bookworm,l=Debian,c=main,b=amd64", // or "release a=now"
      "site": "deb.debian.org",        // null if empty
      "downloadable": true
    }
  ],
  "pinned_packages": [
    { "name": "pkg", "version": "1.2", "priority": 1001 }
  ]
}
```

- `package_files` includes only downloadable files plus the dpkg status file (`src/cmd/policy.rs:77-79, 162-177`).
- `release` is `None` when every `o/a/n/l/c/b` component is empty; otherwise `"<localized 'release'> key=value,…"` (`src/cmd/policy.rs:81-107`) — note it embeds a **translated label**, so it is not machine-stable across locales.
- `pinned_packages` sorted by `(name, version)` (`src/cmd/policy.rs:193-197`).

### 4.4 Streams, exit codes, consumer rules

- Machine payloads go to **stdout**; all notices/warnings/errors go to stderr through the logger (`src/config/logger.rs:81-82, 139-149`), so `list -m > file` is clean apart from possibly coloured stderr.
- Any error aborts with exit code 1 (`src/main.rs:57-73`).
- Consumers must (a) stream-parse concatenated pretty objects for `list`/`show`, (b) branch on the top-level JSON type for `policy` (array vs object), and (c) tolerate omitted record keys, `null` optionals, unstable dependency-key order, and a localized `release` string.
- There is no machine schema version, no envelope, and no guarantee of field order stability (record order is fixed, dependency order is not).

---

## 5. `download` (to cwd) and `clean`

### 5.1 `download`

`download(config)` (`src/download/downloader.rs:23-74`):

1. Sets APT config `Dir::Cache::Archives` to `"./"` — this is the **APT key**, not a filesystem path (`Paths::Archive.path()` returns the key; `src/config/paths.rs:66-73`; `src/download/downloader.rs:25`). Consequently `archive_dir = "./"`, `partial_dir = "./partial"`.
2. Opens a cache and iterates the deduped CLI package names (`src/download/downloader.rs:30-33`).
3. For each name present in the cache, chooses the **first `is_downloadable()` version**; each non-downloadable version before it logs `download-source-missing` (`src/download/downloader.rs:34-49`).
4. Any name not in the cache sets `not_found`; after the loop it bails `Some packages were not found.` — i.e. **all-or-nothing before downloading** (`src/download/downloader.rs:50-57`).
5. Runs the downloader with `rm_partial = true` (`src/download/downloader.rs:59`), then prints `Downloads Complete:` and one `  <pkg> was written to <path>` line per file (`src/download/downloader.rs:61-71`; strings `locales/en-US/main.ftl:168, 187`).

No sudo check and no APT lock: `download` is classified read-only for lock purposes (`src/main.rs:166-175`), so it fails naturally if `/var/lib/apt/lists` or the cache is unreadable, and it writes to the invoking user's cwd. It does **not** install anything and records no history. Files are moved from `./partial` to `./` by rename; `./partial` is removed at the end (`finish`, `src/download/downloader.rs:284-295`).

### 5.2 `clean`

`clean(config)` (`src/cmd/clean.rs:25-42`) with precedence:

```
if config.get_bool("lists"):        # --lists
    remove files in Dir::State::Lists, then in Lists/partial
elif config.get_bool("fetch"):      # --fetch
    remove /etc/apt/sources.list.d/nala.sources   (errors if absent)
else:
    remove files in Dir::Cache::Archives, then in Archives/partial
```

- `remove_files` deletes only **regular files directly inside** the directory; it ignores subdirectories other than the explicit `partial` pass and flattens read errors (`src/cmd/clean.rs:9-23`).
- `--lists` and `--fetch` are independent booleans but checked in that order; passing both cleans lists only (`src/cmd/clean.rs:25-37`).
- `--lists` uses the APT-configured lists dir; `--fetch` uses the compile-time fixed path (`src/config/paths.rs:2`). Neither touches `history`, `nala.conf`, or the transaction log.
- `clean` requires root and takes the APT lock (`src/main.rs:155-169`).
- There is no dry-run, no age filter ("autoclean"-style), and no per-package selection.

---

## 6. Parity checklist / mapping to xbps (analysis, not nala fact)

Requirements implied by the above for the xbps side; downstream tickets should decide these explicitly.

| Area | nala v0.18.2 fact | xbps equivalent must decide |
|---|---|---|
| History unit | post-success transaction snapshot with before/after package state | xbps exposes installed pkgver and repo pkgver; a transaction log line (per ADR-0008) lacks before/after states and versions of removed packages, so replay needs a richer record than the current log |
| Undo install | remove, purge if package was previously absent | the ADR-0008 transaction log cannot distinguish "was never installed" from "config files remained"; xbps removal semantics and the right flag (e.g. `-F`) need their own sourced research |
| Undo upgrade | install recorded `before.version` as an explicit downgrade | xbps version pinning / downgrade availability must be confirmed |
| Purge undo | reinstall package (config-only state not restorable) | accept the same limitation or record more state |
| Replay safety | no autoremove, allow downgrades, prompt unless config assume_yes | map the `assume_yes` config; decide whether replay prompts itself or passes `-y` to xbps |
| IDs | `max+1`, reused after clear/failure | same choice must be documented if implementing |
| Per-domain concurrency | 3 per domain, unbounded files, exclusive reservation, rotating first choice | xbps drives its own transfers through libfetch (`docs/research/xbps-progress-telemetry.md:16`); a parallel predownload layer must avoid duplicate downloads with the real xbps run |
| Verification | sha512/sha256 from index, no signatures per file | decide the equivalent trust gate (no per-file signatures); whether repo-index hashes are reachable for predownloaded files needs confirmation |
| Retries | 4 per URL then next mirror; any checksum mismatch aborts run | define whether one bad file aborts the whole predownload |
| Fetch score | ms for full `Release` body, 10 concurrent, 1 s timeout | xbps has no `fetch`; Debian's `Release`/`dists` layout has no direct Void analogue — xbps indexes are per-arch repodata files (`docs/adr/0002-measured-xbps-behaviour.md:13-17`, `docs/research/xbps-progress-telemetry.md:240`) |
| Fetch output | deb822 `nala.sources` at fixed path | Void repositories are configured as `repository=` lines in `/etc/xbps.d/*.conf` (`README.md:84`) |
| Machine output | list/show = per-version rust-apt JSON, policy = array or global object | JSON shape must be designed, not copied; xbps row data differs (no APT priorities/sections) |

---

## 7. Gaps / not verified

1. **`FullName(pretty=true)` format** is implemented in APT C++, not in either Rust repo; the exact `name:arch` rendering was not verified against libapt-pkg source. `policy.package` therefore has a dependency-defined string.
2. **reqwest redirect hop limit** is a dependency property; nala only leaves the policy unset (`src/download/downloader.rs:219-223`). The exact cap (commonly 10) was not read from a pinned reqwest source.
3. **`index_type` values** in `package_files` come from APT (`PackageFile::index_type`); only the dpkg-status comparison is used by nala (`src/cmd/policy.rs:63-66`).
4. **dpkg residual state** after undo-of-install (`purge:false` → config files retained vs the integration test's `! dpkg-query -W` assertion at `tests/deb/history/run:122-124`) could not be reproduced here (host is Void, no dpkg). The action mapping is from source; the fixture's exact state assertion was not independently verified.
5. **Country filtering case sensitivity**: the mismatch between upper-cased input and Ubuntu/Devuan field casing is inferred from code, not exercised by tests.
6. **`file:` URI registration in `nala download`** is an untested code-path inference (§2.6).
7. **No test coverage** exists in the tagged tree for downloader retry counts, hash-mismatch abort, proxy, or auth beyond the unit tests cited; the only integration harness is `tests/deb/history/run` (history), not downloads/fetch.
8. **Fetch dedupe semantics** are substring-based and intentionally skip `nala.sources`; no tests cover `remove_existing_sources`, country filters, or `--https-only` rewriting.

---

## 8. Reproduction

```bash
git clone --depth 1 --branch v0.18.2 https://github.com/volitank/nala.git /tmp/nala-v0.18.2
git -C /tmp/nala-v0.18.2 log -1 --format='%H %s'   # 2b7ff055… Prepare Nala 0.18.2 Debian release
# Machine schema dependency:
curl -sSL https://crates.io/api/v1/crates/rust-apt/0.11.3/download | tar xz -C /tmp
sed -n '23,100p' /tmp/rust-apt-0.11.3/src/iterators/serde.rs
# History fixtures:
cat tests/deb/history/rust-v1.json
sed -n '1,120p' tests/deb/history/verify.py
```

# Research: nala v0.18.2 user-facing CLI surface (commands, flags, config, prompts, exit codes, UI modes)

- **Status:** Complete
- **Date:** 2026-11-05
- **Author:** Soubarna (Agentic Researcher)
- **Tracking Issue:** [#38 (Research: nala v0.18.2 command surface, flags, config and behaviours)](https://github.com/soubarnak/xbpretty/issues/38)
- **Parent Issue:** [#37 (Map: Nala v0.18.2 parity for xbpretty)](https://github.com/soubarnak/xbpretty/issues/37)
- **Downstream Blocked Issues:**
  - [#43 (Grilling: transaction commands and shared flags on xbps)](https://github.com/soubarnak/xbpretty/issues/43)
  - [#44 (Grilling: discovery commands and machine-readable output on xbps)](https://github.com/soubarnak/xbpretty/issues/44)
  - [#49 (Grilling: config schema — nala's TOML plus xbps extensions)](https://github.com/soubarnak/xbpretty/issues/49)

---

## 0. Source pin and method

**Source of truth:** `volitank/nala` tag **`v0.18.2`**, commit **`2b7ff0554ca88e761fa6eaf8604e19c5e3033815`** ("Prepare Nala 0.18.2 Debian release"). The tag was cloned directly (`git clone --depth 1 --branch v0.18.2`) and every citation below is a `path:line` relative to that tag's tree.

Nothing here comes from `main`, from `legacy/python`, or from `screenshots-nala/`. Where a fact depends on a dependency's behaviour rather than nala's own code (clap parsing, clap_mangen filenames), the dependency version is named and the method is stated.

**Citation short forms.** Tables abbreviate `src/` paths: `commands.rs`=`src/cli/commands.rs`, `flags.rs`=`src/cli/flags.rs`, `parser.rs`=`src/cli/parser.rs`; `configuration.rs`/`file.rs`/`keys.rs`/`color.rs`/`paths.rs`/`logger.rs`=`src/config/…`; `prompt.rs`/`privilege.rs`/`units.rs`=`src/util/…`; `install.rs`/`clean.rs`/`policy.rs`/`show.rs`/`list.rs`/`update.rs`=`src/cmd/…`; `release.rs`/`output.rs`/`mirrors.rs`/`score.rs`=`src/cmd/fetch/…`; `downloader.rs`/`uri.rs`=`src/download/…`; `row.rs`=`src/tui/summary/row.rs`; `model.rs`=`src/cmd/history/model.rs`; `main.ftl`=`locales/en-US/main.ftl`; `main.rs`=`src/main.rs`; `build.rs`=`build.rs`. Anything else is written in full.

**Verification method.** This host has no `libapt-pkg-dev`, so nala cannot be built or executed here; all nala facts are source-derived. Two dependency behaviours were verified locally against the nearest available crates.io sources (`clap_builder 4.6.7`, `clap_derive 4.6.7`; nala pins `clap 4.6.5` / `clap_complete 4.6.8` — `Cargo.toml:35-36`):

1. clap exits **2** on usage errors and **0** for `--help`/`--version` (`clap_builder-4.6.7/src/util/mod.rs:32`, `clap_builder-4.6.7/src/error/mod.rs:233-249`), compiled and exercised in a throwaway binary.
2. A `Vec<String>` positional in clap 4.6.7 derive is **not** required (`clap_derive-4.6.7/src/derives/args.rs:307-322` sets only `num_args(1..)`), confirmed by a compiled probe: a subcommand with `Vec<String>` parses with zero values. Consequences for "required" arguments are called out per command.

**Reading conventions.** "Exit 0" means the process returns `ExitCode::SUCCESS`; every `anyhow` error propagating out of `main_nala` returns `ExitCode::FAILURE` (1) (`src/main.rs:57-73`). "Root" means `sudo_check` runs (`src/main.rs:81-86`). Prompts are stdout writes; the logger (`info!`, `warn!`, `error!`, `debug!`) writes to **stderr** (`src/config/logger.rs:81-83`).

---

## 1. Executive summary

1. **18 subcommand names, 17 visible + 1 hidden.** Visible: `list`, `search`, `show`, `policy`, `clean`, `download`, `history`, `fetch`, `update`, `upgrade`, `full-upgrade`, `safe-upgrade`, `install`, `remove`, `purge`, `autoremove`, `autopurge`. Hidden: `moo` (`src/cli/commands.rs:12-40`).
2. **Three compatibility aliases:** `info` → `show` (`commands.rs:92`), `uninstall` → `remove` (`commands.rs:30`), `dist-upgrade` → `full-upgrade` (`commands.rs:24`); plus a visible alias `--names-only` for `search --names` (`commands.rs:82`) and hidden alias `--config` for `autoremove --remove-config` (`commands.rs:343`).
3. **Nine global flags** (`-v/--verbose`, `--debug`, `--config`, `--history-dir` (hidden), `--tui`, `--no-tui`, `-o/--option`, `--color`, `--no-color`) — `src/cli/parser.rs:13-46`. Only `--config` is **not** `global = true`, so it must appear before the subcommand.
4. **Five shared flag groups** are flattened into commands: `TransactionFlags`, `FixBrokenFlags`, `InstallFlags`, `InfoFlags`, `AutoRemoveFlags` (`src/cli/flags.rs:1-108`). 55 distinct clap arg ids are defined in total (plus clap's auto-generated `help`/`version`).
5. **`-n` means `--assume-no`, not dry-run**, and `-y` means `--assume-yes` (`flags.rs:28-34`). This is the sharpest collision with xbps conventions and with xbpretty's current spec (`xbpretty-spec.md:104` uses `-n` for dry-run).
6. **Root is enforced per command, not globally:** `clean`, `fetch`, `update`, `upgrade`/`full-upgrade`/`safe-upgrade`, `install`, `remove`/`purge`, `autoremove`/`autopurge`, and `history undo|redo|clear` require root; `list`, `search`, `show`, `policy`, `download`, `history` list/detail and `moo` do not (`src/main.rs:153-177`). The error text is `Nala needs root to <command>` (exit 1).
7. **Exactly one interactive confirmation exists for transactions:** `Do you want to continue? [Y/n] ` (default yes) — skipped by `-y`, refused by `-n`, and never reached by `--print-uris`. `update` additionally has a Release-info confirmation whose default is **no** (`[y/N]`).
8. **Nothing-to-do prints `Nothing to do.` and exits 0** (`src/summary.rs:299-313`; `locales/en-US/main.ftl:79`), including the case where only a read-only summary (e.g. held-back packages on `upgrade`) can be shown.
9. **Two exit codes only, plus clap's:** 0 success, 1 any error (including user refusal and missing root), 2 clap usage error. There is no distinct "nothing to do" or "declined" code.
10. **Config is TOML, strictly validated:** `[Nala]` (6 keys), `[Ui]` (2 keys), `[Color]` (mode + `[Color.theme]` with 9 `Style` entries); every table sets `deny_unknown_fields`, so retired/unknown keys are hard errors, not warnings (`src/config/file.rs:23-121`; `src/config/color.rs:492-642`).
11. **Precedence: explicit CLI flag > config file > built-in default.** Only args whose clap `ValueSource` is `CommandLine` become overrides (`src/config/configuration.rs:56-95`), so `--color`'s default of `auto` does not mask `[Color] mode`.
12. **`Ui.mode` has three states with distinct consequences:** `Auto` (default) = inline enhanced progress + required prompts + interactive fetch picker; `Tui` = `Auto` plus the optional full-screen summary/history screens; `Plain` = line-oriented output only (`src/config/file.rs:12-21`, `src/terminal.rs:26-54`). All enhanced/full-screen paths additionally require stdout to be a TTY and `TERM != dumb`.
13. **`-o KEY=VALUE` passthrough mutates APT config and can be silently overridden** for `APT::Install-Recommends`, `APT::Install-Suggests` and `APT::Default-Release`, because nala writes those three *after* applying `-o` (`configuration.rs:99-124`).
14. **Shell completion is dynamic, not static files:** `clap_complete::CompleteEnv` is the first statement in `main`, sourced through `COMPLETE=<shell> nala`; shipped wrappers exist for bash, zsh and fish (`src/main.rs:38-39`; `debian/_nala`, `debian/nala.bash-completion`, `debian/nala.fish`).
15. **Manpages are generated at build time** from the same clap parser, section 8, for every visible command and subcommand recursively, and the Debian package installs every generated `*.8` (`build.rs:27-66`; `debian/manpages`, `debian/rules:15-19`).
16. **Machine output exists for `list`, `search`, `show` and `policy` (`-m/--machine`) and for `upgrade --print-uris`.** The `policy` schema is defined by nala; the `list`/`show` schema is whatever `rust-apt` 0.11.3's `Version: Serialize` emits, which is **not** defined in nala's repo (gap, see §13/§15).

---

## 2. Command inventory

Clap naming comes from `#[clap(rename_all = "lower")]` on the enum plus explicit `name = ...` attributes (`commands.rs:10-40`); `lower` means "lowercase with separators removed" (`clap_derive-4.6.7/src/item.rs:1477`), hence the explicit `full-upgrade`, `safe-upgrade`, `autopurge` names.

| Command | Aliases | Positionals | Root? (APT lock?) | What happens on success (exit 0) |
|---|---|---|---|---|
| `list` | — | `[PKG_NAMES]...` (0..n, case-insensitive glob name match, `name=version`, `:arch`) | no | Prints one line per package/version, or one line per virtual package; optional `--description`/`--summary` block. Lists all cache packages when no names given. `src/main.rs:97-117`, `src/cmd/list.rs:11-45` |
| `search` | — | `[PKG_NAMES]...` (0..n at clap level, but runtime requires ≥1) | no | Regex search over package names and (unless `--names`) descriptions; prints same line format as `list`; missing tokens produce `info!` notices. `src/main.rs:99-115`, `src/glob.rs:408-449` |
| `show` | `info` | `[PKG_NAMES]...` (0..n at clap level, runtime requires ≥1) | no | For each package, prints `Label: value` lines (`Package`, `Version`, … `Attributes`, dependency fields, `APT-Sources`); with `-a`, every version; excess records produce a notice. `src/cmd/show.rs:35-70`, `src/cmd/mod.rs:190-237` |
| `policy` | — | `[PKG_NAMES]...` (0..n) | no | With names: apt-style `pkg:` block (`Installed:`, `Candidate:`, `Version table:` with `***` markers and per-source priorities). Without names: global `Package files:` + `Pinned packages:` blocks. `src/cmd/policy.rs:285-316,205-273` |
| `clean` | — | none | **yes** | Deletes files in the apt archive dir (+ `partial/`); `--lists` deletes the lists dir (+ `partial/`); `--fetch` deletes `/etc/apt/sources.list.d/nala.sources`. No output on success. `src/cmd/clean.rs:25-43` |
| `download` | — | `<PKG_NAMES>...` (runtime requires ≥1) | no | Sets `Dir::Cache::Archives=./`, downloads the first downloadable version of each package into cwd, prints `Downloads Complete:` then `  <pkg> was written to <path>` per file. `src/download/downloader.rs:23-74` |
| `history` | — | `[ID\|last]` or one of `info\|undo\|redo\|clear` | list/detail: no; `undo\|redo\|clear`: **yes** | No args: table of entries (`ID`, `Command`, `Date and Time`, `Requested-By`, `Altered`) or `No history entries found.`; with selector: detailed record (and full-screen summary in `Tui` mode); `clear` prints count/entry cleared; `undo`/`redo` replay. `src/cmd/history/mod.rs:20-88` |
| `fetch` | — | none | **yes** | Detects distro/release, fetches+times mirrors, removes already-configured ones, picks mirrors (interactive in enhanced UI, else 3 fastest, or `--auto N`), writes `/etc/apt/sources.list.d/nala.sources`, prints `Sources have been written to <file>`. `src/cmd/fetch/mod.rs:51-101` |
| `update` | — | none | **yes** | Runs apt update with progress; prints per-item states; prints finished line; then either lists upgradable packages (config `update_show_packages=true`) or prints `N packages can be upgraded. Run 'nala list --upgradable' to see them.` `src/cmd/update.rs:74-160` |
| `upgrade` | — | none | **yes** | Applies normal apt upgrade (or full/safe per flags/config), prints summary + confirmation, downloads, runs dpkg, records history, prints reboot notice if flagged. `src/main.rs:125-127,189-198`; `src/cmd/upgrade/mod.rs:19-40` |
| `full-upgrade` | `dist-upgrade` | none | **yes** | Identical to `upgrade` but forces `full = true` before dispatch. `src/main.rs:179-187` |
| `safe-upgrade` | — | none | **yes** | Identical to `upgrade` but forces `safe = true`; safe takes precedence over full. `src/main.rs:189-198` |
| `install` | — | `[PKG_NAMES]...` (0..n) | **yes** | With names: installs (or reinstalls/local `.deb`/https URL); with none and `-f/--fix-broken` or config `fix_broken`: fixes broken deps; summary + prompt + download + dpkg + history. `src/main.rs:128-136`, `src/cmd/install.rs:55-116` |
| `remove` | `uninstall` | `<PKG_NAMES>...` (runtime requires ≥1) | **yes** | Marks delete (purge when `--purge`), summary + prompt + dpkg + history. `src/main.rs:137-139`, `src/glob.rs:170-180` |
| `purge` | — | `<PKG_NAMES>...` | **yes** | Same as `remove` but `purge = true` forced. `src/main.rs:179-187` |
| `autoremove` | — | none | **yes** | `summary::commit` with auto-remove forced on; `--remove-config` (alias `--config`) also drops config-state packages. Prints `Nothing to do.` if none. `src/main.rs:140-143`, `src/summary.rs:264-307` |
| `autopurge` | — | none | **yes** | Same as `autoremove` with `purge = true` forced. `src/main.rs:179-187` |
| `moo` | — | none (hidden flags only) | no | Prints ASCII cat + `"...I can't moo for I'm a cat..."`; hidden `--help` prints `I beg, pls moo`; hidden `--update`/`--no-update` add one joke line. `src/cmd/mod.rs:278-299` |

**No subcommand at all** (`nala`, or `nala --verbose`): prints the root help to stdout, then bails with `Subcommand not supplied`; `main` suppresses that message before logging and returns exit **1** (`src/main.rs:146-149,68`; `main.ftl:18`). A `help` subcommand is provided by clap and exits 0.

---

## 3. Global flags and bootstrap order

### 3.1 Global flags

| Flag | Type / value | Default | Behaviour | Source |
|---|---|---|---|---|
| `-v`, `--verbose` | bool, `global` | false (but true when `--debug`) | Sets logger level to `Verbose`; "disable scrolling text and print extra information" | `parser.rs:13-15`; `configuration.rs:128-139,303` |
| `--debug` | bool, `global` | false | Sets logger level to `Debug`; dumps `{config:?}` before dispatch and JSON URIs in downloader | `parser.rs:16-18`; `main.rs:88-90`; `configuration.rs:303` |
| `--config FILE` | path, **not global** | `/etc/nala/nala.conf` when absent | Loads that file; a missing `--config` file is a hard error (no defaults fallback) | `parser.rs:20-22`; `config/mod.rs:91-108` |
| `--history-dir DIR` | path, `global`, `hide = true` | `/var/lib/nala/history` | Overrides only `Paths::History`; also settable via env `NALA_HISTORY_DIR` (env first, flag wins) | `parser.rs:24-26`; `config/mod.rs:112-118`; `config/paths.rs:1,62` |
| `--tui` | bool, `global`, conflicts `--no-tui` | false | Forces `UiMode::Tui` for this invocation | `parser.rs:28-30`; `configuration.rs:198-200` |
| `--no-tui` | bool, `global`, conflicts `--tui` | false | Configured `Tui` becomes `Auto`; configured `Auto`/`Plain` unchanged | `parser.rs:32-34`; `configuration.rs:190-196` |
| `-o`, `--option` | `Vec<String>`, `global`, repeatable | empty | Apt config passthrough, see §11 | `parser.rs:36-38` |
| `--color <auto\|always\|never>` | clap `ColorChoice`, `global` | `auto` | Maps to `Switch`; ignored (not an override) unless explicitly passed | `parser.rs:40-42`; `configuration.rs:56-71,205-214` |
| `--no-color` | bool, `global`, conflicts `--color` | false | Forces `Switch::Never` regardless of file/`--color` | `parser.rs:44-46`; `configuration.rs:205-208` |
| `-h`, `--help` / `-V`, `--version` | clap built-ins | — | help/version exit 0; usage errors exit 2 | `parser.rs:8-11`; clap 4.6.7 `util/mod.rs:32` |

Note: `--color` accepts only lowercase `auto|always|never` (clap `ValueEnum` possible values, `clap_builder-4.6.7/src/util/color.rs:91-102`); a mismatched case is a usage error (exit 2).

### 3.2 Bootstrap order (what runs before any command logic)

1. `CompleteEnv::with_factory(NalaParser::command).complete()` — completion interception, before config or root (`main.rs:39`).
2. clap parse into `ArgMatches` + typed `NalaParser` (`config/mod.rs:92-93`).
3. Config path resolution and load: default file if no `--config`; built-in defaults **only** when the default file does not exist (`config/mod.rs:95-108`).
4. `NALA_HISTORY_DIR`, then `--history-dir` overrides (`config/mod.rs:112-118`).
5. `config.load_command(subcmd_name, subcmd_matches)` → records `config.command`, builds overrides from explicit CLI values, applies `-o`, then the three APT defaults, then sets color mode, then logger level (`configuration.rs:49-139`).
6. `apply_command_defaults` for `full-upgrade`, `safe-upgrade`, `purge`, `autopurge` (`main.rs:179-187`).
7. Root check + APT lock for mutating commands (`main.rs:81-86`), `debug!` config dump, optional early `update` (`main.rs:92-94`), then dispatch.

---

## 4. Per-command surfaces

Each table lists the command's **own** flags; the shared groups are defined once in §5 and are flattened in as noted. Every command also accepts every `global` flag from §3.1.

### 4.1 `list` — `src/cli/commands.rs:42-76`, dispatch `src/main.rs:97-117`

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `[PKG_NAMES]...` | `Vec<String>`, 0..n | Glob-style name match; `name=version` pins; trailing `+`/`-` sets op; `:arch` suffix filters arch; unknown tokens are reported as notices, not fatal | `commands.rs:47-48`; `glob.rs:338-406,189-199` |
| `--description` | bool, false | Append full description under each entry | `commands.rs:50-52`; `cmd/mod.rs:253-269` |
| `--summary` | bool, false | Append the short summary instead | `commands.rs:54-56`; `cmd/mod.rs:253-269` |
| `-a`, `--all-versions` | bool, false | List every version of each package | `flags.rs:79-82`; `cmd/list.rs:13-20` |
| `-A`, `--all-arches` | bool, false | Include packages of all configured architectures | `flags.rs:84-87`; `configuration.rs:241-247` |
| `-i`, `--installed` | bool, false | Restrict sort to installed packages | `commands.rs:61-63`; `glob.rs:327-329` |
| `-u`, `--upgradable` | bool, false | Restrict sort to upgradable packages | `commands.rs:65-67`; `glob.rs:331-333` |
| `-V`, `--virtual` | bool, false | Restrict sort to *only* virtual packages | `commands.rs:69-71`; `glob.rs:320-325` |
| `-m`, `--machine` | bool, false | Print JSON per version instead of the line format | `commands.rs:73-75`; `cmd/mod.rs:240-244` |

Filters compose: `--installed` and `--upgradable` are applied in order, both narrowing the sort (`glob.rs:327-333`); `--virtual` switches the base sort to `only_virtual` (`glob.rs:320-325`). All-filters-empty + no names = whole cache, sorted by `PackageSort` default (`main.rs:111`).

### 4.2 `search` — `src/cli/commands.rs:78-88`

`search` flattens the entire `List` struct (`commands.rs:86-87`), so it has **all** of `list`'s flags with identical semantics, plus:

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `-n`, `--names` (visible alias `--names-only`) | bool, false | Match only package names, never descriptions | `commands.rs:81-83`; `glob.rs:415,434-436` |
| `[PKG_NAMES]...` | `Vec<String>`, 0..n at clap level | Each token compiled as a case-insensitive regex; the first token match wins; descriptions are searched only if no name matched | `glob.rs:408-449` |

With zero patterns, `config.pkg_names()` fails with `You must specify a package` → exit 1 (`configuration.rs:229-239`; `main.ftl:164`). This is a runtime error, not a clap usage error.

### 4.3 `show` — `src/cli/commands.rs:90-104`

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `[PKG_NAMES]...` | `Vec<String>`, 0..n | Runtime requires ≥1 (`pkg_names()` bail) | `commands.rs:94-96` |
| `-a`, `--all-versions` | bool, false | Show a record block for every version; otherwise only the first version per package | `flags.rs:79-82`; `cmd/show.rs:40-56` |
| `-A`, `--all-arches` | bool, false | Include all configured architectures | `flags.rs:84-87` |
| `-m`, `--machine` | bool, false | Print each version record as JSON | `commands.rs:101-103`; `cmd/mod.rs:225-229` |

When more records exist than were printed, nala emits `There are N additional records. Please use the '-a' switch to see them.` via `info!` (stderr) (`cmd/show.rs:58-65`; `main.ftl:351`).

### 4.4 `policy` — `src/cli/commands.rs:106-116`

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `[PKG_NAMES]...` | `Vec<String>`, 0..n | Empty → global policy view | `commands.rs:109-111`; `policy.rs:287-295` |
| `-m`, `--machine` | bool, false | JSON output for package view or global view | `commands.rs:113-115`; `policy.rs:255-283` |

Global view prints `Package files:` then one line per file (` {priority} {source}`, plus `release ...` and `origin <site>` lines), then `Pinned packages:` and `  {name} -> {version} ({priority})` (`policy.rs:239-273`). Package view prints a blank line between packages (`policy.rs:304-311`).

### 4.5 `clean` — `src/cli/commands.rs:118-128`

| Flag | Type · default | Behaviour | Source |
|---|---|---|---|
| `--lists` | bool, false | Deletes every regular file in `Dir::State::Lists` and then in its `partial/` subdir; missing dir is silently ignored | `commands.rs:121-123`; `clean.rs:9-31` |
| `--fetch` | bool, false | Deletes `/etc/apt/sources.list.d/nala.sources`; **errors if it does not exist** (`file-remove` context) | `commands.rs:125-127`; `clean.rs:33-37` |

No flag → archive dir + `partial/` (`clean.rs:39-42`). `--lists` wins over `--fetch` when both are passed (`clean.rs:26-37` early returns). Requires root (`main.rs:155-165`) and the APT lock.

### 4.6 `download` — `src/cli/commands.rs:130-136`

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `<PKG_NAMES>...` | `Vec<String>` required at runtime | For each name, picks the first *downloadable* version; warns for non-downloadable versions; missing packages set a flag and end in a fatal `Some packages were not found.` | `downloader.rs:23-57`; `main.ftl:170` |

No root, no APT lock (`main.rs:170-175`). Downloads to the current working directory by rewriting `Dir::Cache::Archives` to `./` for this process (`downloader.rs:24-25`). Success prints `Downloads Complete:` and one indented line per file (`downloader.rs:59-71`; `main.ftl:168,187`).

### 4.7 `history` — `src/cli/commands.rs:138-213`

Top-level form: `nala history [ID|last]` **or** `nala history <subcommand>`; `args_conflicts_with_subcommands = true` makes mixing them an error (`commands.rs:140`).

| Item | Type · default | Behaviour | Source |
|---|---|---|---|
| `[ID\|last]` | `HistorySelector`, optional | `last` = highest id; integer = exact id; any other token is a parse error | `commands.rs:195-213` |
| `info [ID\|last]` (alias `show`) | subcommand | Detail view for one entry | `commands.rs:156-157` |
| `undo [ID\|last]` | subcommand | Replays the inverse; requires entry state recorded as applied | `commands.rs:158-159`; `history/mod.rs:32-35` |
| `redo [ID\|last]` | subcommand | Re-applies the recorded change | `commands.rs:160-161` |
| `clear <ID\|last>` | subcommand | Deletes one entry; prints `Cleared history entry N.` | `commands.rs:162-163`; `history/mod.rs:42-50` |
| `clear --all` | subcommand flag | Deletes all entries; prints `Cleared N history entries.` (plural-aware) | `commands.rs:190-192`; `history/mod.rs:21-28`; `main.ftl:420-424` |

`clear` has an `ArgGroup` requiring exactly one of `history_id` or `--all`, and `history_id` conflicts with `--all` (`commands.rs:176-193`). With no selector: lists a table, or prints `No history entries found.` (exit 0) when empty (`history/mod.rs:58-66`; `main.ftl:426`). Unknown id → `History entry with ID 'N' does not exist` (exit 1) (`model.rs:84-98`; `main.ftl:430`). `undo`/`redo`/`clear` require root and the APT lock (`main.rs:166-169`).

### 4.8 `fetch` — `src/cli/commands.rs:215-254`, dispatch `src/cmd/fetch/mod.rs:51-101`

| Flag | Type · default | Behaviour | Source |
|---|---|---|---|
| `--non-free` | bool, false | Adds `contrib non-free` for Debian/Devuan; probes `non-free-firmware` and appends it if every chosen mirror serves it | `commands.rs:219-220`; `release.rs:48-62`; `output.rs:12-49` |
| `--https-only` | bool, false | Filters mirrors to HTTPS | `commands.rs:222-224`; `score.rs:36` |
| `--sources` | bool, false | Writes `Types: deb deb-src` instead of `Types: deb` | `commands.rs:226-228`; `output.rs:65-69` |
| `--auto [N]` | `Option<u8>`, `default_missing_value = "3"`, range `1..` | Skips the TUI and takes the N fastest mirrors | `commands.rs:230-237`; `fetch/mod.rs:83-85` |
| `-c`, `--country <CC>` | `Vec<String>`, repeatable | Uppercases country codes and filters the mirror list | `commands.rs:239-241`; `fetch/mod.rs:17-26` |
| `--debian <RELEASE>` | `Option<String>` | Forces distro=debian, lowercases release | `commands.rs:243-245`; `release.rs:16-23` |
| `--ubuntu <RELEASE>` | `Option<String>` | Forces distro=ubuntu | `commands.rs:247-249`; `release.rs:16-23` |
| `--devuan <RELEASE>` | `Option<String>` | Forces distro=devuan | `commands.rs:251-253`; `release.rs:16-23` |

Selection rule: `--auto N` → top N by score; else if **enhanced UI** (Auto or Tui on a TTY) → interactive full-screen picker; else → top 3 (`fetch/mod.rs:82-94`). Empty mirror list → `Nala was unable to find any mirrors.`; empty selection → `No mirrors were selected.` (exit 1) (`fetch/mod.rs:78-98`; `main.ftl:470-471`). Distro detection falls back through keyring packages and then fails with `There was an issue detecting release.` (`release.rs:16-46`). Unsupported distro component derivation fails with `{distro} is unsupported.` (`release.rs:57-62`).

### 4.9 `update` — `src/cli/commands.rs:256-258`

No command-specific flags (`commands.rs:257-258`). Behaviour: root + APT lock; `update_early` does not re-run update for `update` itself; progress view `Updating Package Lists`; per item prints `No Change: ...`, `Updated: ... [size]`, `Ignored: ...`, or `Error: ...` plus error text (`update.rs:215-290`); after success prints the finished string (`update.rs:129`) and then either the upgradable list or the count line (`update.rs:131-157`; `main.ftl:492-497`). Release-file changes get a confirmation (see §8). Pressing `q` during update exits the process with code 1 (`update.rs:119-122`).

### 4.10 `upgrade`, `full-upgrade` (`dist-upgrade`), `safe-upgrade` — `src/cli/commands.rs:260-292`

| Flag | Type · default | Behaviour | Source |
|---|---|---|---|
| `--print-uris` | bool, false | Prints one pretty JSON object per planned download and stops before downloading/installing | `commands.rs:263-265`; `summary.rs:315,338-344` |
| `--exclude <PKG>` | `Vec<String>`, repeatable, glob patterns | Protects matching upgradable/auto-removable packages (`mark_keep` + resolver protect); unmatched pattern is fatal `Exclude pattern '...' did not match any packages`; an upgrade that cannot proceed with protections produces `Selected packages cannot be excluded from upgrade safely.` | `commands.rs:267-269`; `upgrade/mod.rs:42-77`; `glob.rs:289-318`; `main.ftl:507-510` |
| `--full` | bool, false | Select `Upgrade::FullUpgrade` | `commands.rs:271-273`; `main.rs:189-198` |
| `--no-full` | bool, false | Negates `full` via `get_no_bool` | `commands.rs:275-277`; `configuration.rs:220-227`; `main.rs:193` |
| `--safe` | bool, false | Select `Upgrade::SafeUpgrade`; "takes precedence over other Upgrade options" | `commands.rs:279-282`; `main.rs:189-198` |
| shared | — | `TransactionFlags` + `InstallFlags` + `AutoRemoveFlags` | `commands.rs:284-291` |

Mode resolution: `safe` (CLI or `[Nala]`? no file key — `safe` is CLI-only) wins; else `full` (CLI `--full` or `[Nala] full_upgrade`) via `get_no_bool("full")`; else normal (`main.rs:189-198`). The semantic commands set the defaults *before* dispatch: `full-upgrade` sets `full`, `safe-upgrade` sets `safe`, `purge`/`autopurge` set `purge` (`main.rs:179-187`).

Early update: for the upgrade family, `update_early` returns true unless `--no-update`, and honours `[Nala] auto_update` (default true) (`configuration.rs:276-299`).

### 4.11 `install` — `src/cli/commands.rs:294-320`

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `[PKG_NAMES]...` | `Vec<String>`, 0..n | Cache names, `name=version`, glob, `:arch`, local `.deb` paths, `http(s)` URLs (optional `:SHA256`/`:SHA512` suffix) | `commands.rs:297-299`; `install.rs:15-53` |
| `--reinstall` | bool, false | Marks `Operation::Reinstall`; fails if not installed with `{pkg} is not installed, so it cannot be reinstalled` | `commands.rs:301-303`; `glob.rs:156-169`; `main.ftl:114` |
| `-t`, `--target-release <RELEASE>` | `Option<String>` | Sets `APT::Default-Release` | `commands.rs:305-307`; `configuration.rs:121-123` |
| shared | — | `TransactionFlags` + `FixBrokenFlags` + `InstallFlags` + `AutoRemoveFlags` | `commands.rs:309-319` |

With no package names: if `fix_broken` is true (`-f`, or config key `fix_broken` — note the config file has no such field, so CLI-only in practice), runs `cache.fix_broken()` and commits; if it returns false, fails with `Unable to correct broken packages`. Otherwise falls through to `pkg_names()` failing with `You must specify a package` (`main.rs:128-136`; `install.rs:107-116`; `configuration.rs:229-239`).

Install selection semantics worth mapping: virtual packages are resolved via `provides()`; one provider is auto-selected with an info notice, many providers print a numbered list and fail, none prints `{pkg} has no providers and is purely virtual` (`libnala/mod.rs:63-130`; `main.ftl:149-156`). Already-installed-at-candidate prints `{pkg} ({version}) is already installed and at the latest version` and is skipped (`glob.rs:138-148`; `main.ftl:132`). Conflicts between requested ops/versions are fatal (`glob.rs:85-118`; `main.ftl:121,127`).

### 4.12 `remove` / `purge` — `src/cli/commands.rs:322-337`

| Flag / arg | Type · default | Behaviour | Source |
|---|---|---|---|
| `<PKG_NAMES>...` | `Vec<String>`, runtime required | Marks delete; packages not installed are skipped with an info notice | `commands.rs:325-327`; `glob.rs:170-180` |
| shared | — | `TransactionFlags` + `FixBrokenFlags` + `AutoRemoveFlags` | `commands.rs:329-336` |

`--purge` on `remove` is equivalent to the `purge` command; `remove --fix-broken` resolves broken state after marking (`install.rs:94-102`). Protected/essential packages are blocked unless `--remove-essential` (or apt `APT::Get::allow-remove-essential`, `APT::Get::Force-Yes`) (§7).

### 4.13 `autoremove` / `autopurge` — `src/cli/commands.rs:339-348`

| Flag | Type · default | Behaviour | Source |
|---|---|---|---|
| `--remove-config` (alias `--config`) | bool, false | When purging, also remove packages left in config-files state; **requires** purge or autopurge, else fatal `--remove-config must be used with --purge or autopurge.` | `commands.rs:341-344`; `summary.rs:273-277`; `main.ftl:90` |
| shared | — | only `TransactionFlags` | `commands.rs:346-347` |

Explicit `autoremove`/`autopurge` always auto-remove, even with `-y` (`configuration.rs:264-274`).

### 4.14 `moo` — `src/cli/commands.rs:350-359`

Hidden command, `disable_help_flag = true` so `nala moo --help` prints the joke instead of clap help (`commands.rs:38-39,351-359`).

| Flag | Type · default | Behaviour | Source |
|---|---|---|---|
| `--help` | bool, false (hidden) | Prints `I beg, pls moo` and exits 0 | `commands.rs:352-353`; `cmd/mod.rs:283-287` |
| `--update` | bool, false (hidden) | Adds `"...What did you expect to update to do?..."` | `commands.rs:355-356`; `cmd/mod.rs:292-293` |
| `--no-update` | bool, false (hidden) | Adds `"...What did you expect no-update to do?..."` | `commands.rs:358-359`; `cmd/mod.rs:294-296` |

Default output is the multi-line ASCII cat in `src/cmd/mod.rs:278-281` followed by `println!("\"...I can't moo for I'm a cat...\"")` (`cmd/mod.rs:288-290`). No root, no lock (`main.rs:175`).

---

## 5. Shared flag groups

### 5.1 `TransactionFlags` — `src/cli/flags.rs:7-43`

| Flag | Default | Conflicts | Effective behaviour | Source |
|---|---|---|---|---|
| `-d`, `--download-only` | false | — | After a successful download, returns before dpkg/history (`summary.rs:355-357`) | `flags.rs:8-10` |
| `--simple` | false | — | Uses the one-line-per-operation summary and the plain prompt instead of the full tables / TUI (`summary.rs:21-25,104-134`) | `flags.rs:12-14` |
| `--update` | false | `--no-update` | Runs `update` before the transaction; restricted to upgrade/install/remove/purge/autoremove/autopurge commands (`configuration.rs:281-293`) | `flags.rs:16-18` |
| `--no-update` | false | `--update` | Suppresses the early update, including the automatic one on upgrade (`configuration.rs:276-280`) | `flags.rs:20-22` |
| `--allow-unauthenticated` | false | — | Allows unverified downloads; also honoured via apt `APT::Get::AllowUnauthenticated` (`configuration.rs:253-256`; `download/mod.rs:81-100`) | `flags.rs:24-26` |
| `-y`, `--assume-yes` | false | `--assume-no` | All prompts return true; **implicit** autoremove is skipped unless `--autoremove` was explicit (`configuration.rs:264-274`; `prompt.rs:23-25`) | `flags.rs:28-30` |
| `-n`, `--assume-no` | false | `--assume-yes` | All prompts return false; a non-empty transaction therefore fails with `User refused confirmation` (exit 1) (`prompt.rs:19-21`; `summary.rs:316-318`) | `flags.rs:32-34` |
| `--remove-essential` | false | — | Permits removing essential/protected packages (`summary.rs:222-227`) | `flags.rs:36-38` |
| `--purge` | false | — | Remove with config files; equivalent to the `purge` command (`main.rs:179-187`; `glob.rs:179`) | `flags.rs:40-42` |

### 5.2 `FixBrokenFlags` — `src/cli/flags.rs:45-55`

| Flag | Default | Conflicts | Effective behaviour | Source |
|---|---|---|---|---|
| `-f`, `--fix-broken` | false | `--no-fix-broken` | Passed to `cache.resolve(true)`; with no package names on `install` it instead triggers the `fix_broken()` path (`install.rs:94-115`; `main.rs:129`) | `flags.rs:47-50` |
| `--no-fix-broken` | false | `--fix-broken` | `cache.resolve(false)` (`install.rs:94`) | `flags.rs:52-54` |

Default resolve behaviour is *fix broken* (`get_no_bool(FIX_BROKEN, true)`, `install.rs:94`), and the `install`-with-no-args `fix_broken` branch defaults to *off* (`get_bool(FIX_BROKEN, false)`, `main.rs:129`) — the asymmetry is intentional in code.

### 5.3 `InstallFlags` — `src/cli/flags.rs:57-76`

| Flag | Default | Conflicts | Effective behaviour | Source |
|---|---|---|---|---|
| `--install-recommends` | false | `--no-install-recommends` | Sets `APT::Install-Recommends=1` | `flags.rs:60-63`; `configuration.rs:112-115` |
| `--no-install-recommends` | false | `--install-recommends` | Sets `APT::Install-Recommends=0` | `flags.rs:65-67` |
| `--install-suggests` | false | `--no-install-suggests` | Sets `APT::Install-Suggests=1` | `flags.rs:69-72`; `configuration.rs:116-119` |
| `--no-install-suggests` | false | `--install-suggests` | Sets `APT::Install-Suggests=0` | `flags.rs:73-75` |

Built-in apt defaults are recommends **on**, suggests **off** (`configuration.rs:112-119`). There is no config-file key for these; only CLI and `-o` (and `-o` loses, see §11).

### 5.4 `InfoFlags` — `src/cli/flags.rs:78-87`

| Flag | Default | Conflicts | Effective behaviour | Source |
|---|---|---|---|---|
| `-a`, `--all-versions` | false | — | Show/list all versions | `flags.rs:79-82` |
| `-A`, `--all-arches` | false | — | Use every configured apt architecture instead of the first | `flags.rs:84-87`; `configuration.rs:241-247` |

### 5.5 `AutoRemoveFlags` — `src/cli/flags.rs:89-108`

| Flag | Default | Conflicts | Effective behaviour | Source |
|---|---|---|---|---|
| `--auto-remove` (visible alias `--autoremove`) | false | `--no-auto-remove` | Forces auto-remove even under `-y` (`configuration.rs:273`) | `flags.rs:90-99` |
| `--no-auto-remove` (visible alias `--no-autoremove`) | false | `--auto-remove` | Disables auto-remove entirely; prints an info notice when it would otherwise have run (`configuration.rs:269-271`; `summary.rs:279-283`) | `flags.rs:100-107` |

### 5.6 Composition matrix

| Command | Transaction | FixBroken | InstallFlags | InfoFlags | AutoRemove |
|---|---|---|---|---|---|
| `upgrade` / `full-upgrade` / `safe-upgrade` | ✓ | — | ✓ | — | ✓ (`commands.rs:284-291`) |
| `install` | ✓ | ✓ | ✓ | — | ✓ (`commands.rs:309-319`) |
| `remove` / `purge` | ✓ | ✓ | — | — | ✓ (`commands.rs:329-336`) |
| `autoremove` / `autopurge` | ✓ | — | — | — | — (`commands.rs:346-347`) |
| `list` / `search` | — | — | — | ✓ | — (`commands.rs:58-59,86-87`) |
| `show` | — | — | — | ✓ | — (`commands.rs:98-99`) |

### 5.7 All 55 option ids defined by the CLI

From `commands.rs` + `flags.rs` + `parser.rs`: `verbose`, `debug`, `config`, `history_dir`, `tui`, `no_tui`, `option`, `color`, `no_color`, `download_only`, `simple`, `update`, `no_update`, `allow_unauthenticated`, `assume_yes`, `assume_no`, `remove_essential`, `purge`, `fix_broken`, `no_fix_broken`, `install_recommends`, `no_install_recommends`, `install_suggests`, `no_install_suggests`, `all_versions`, `all_arches`, `auto_remove`, `no_auto_remove`, `pkg_names`, `description`, `summary`, `installed`, `upgradable`, `virtual`, `machine`, `names_only`, `lists`, `fetch`, `non_free`, `https_only`, `sources`, `auto`, `country`, `debian`, `ubuntu`, `devuan`, `remove_config`, `reinstall`, `target_release`, `print_uris`, `exclude`, `full`, `no_full`, `safe`, `help`, `moo.help`, `moo.update`, `moo.no_update`. Arg ids are the Rust field names; `try_get_*` keys in `load_args` are exactly these (`configuration.rs:56-95`).

---

## 6. Configuration

### 6.1 Resolution and load

| Rule | Detail | Source |
|---|---|---|
| Default path | `/etc/nala/nala.conf` | `config/mod.rs:95-98` |
| No `--config` and default file missing | Built-in defaults (`Config::default()`), silently | `config/mod.rs:100-105` |
| `--config FILE` given | Always read; missing/unreadable file is a hard error (`Failed to read '<path>'`) | `config/mod.rs:100-108`; `file.rs:93-103`; `main.ftl:278` |
| Default file exists but is invalid | Hard error: `Failed to parse '<path>'. Nala will not ignore an existing configuration file. …compare it with <path>.dpkg-dist`; no fallback | `file.rs:97-103`; `main.ftl:281` |
| Format | TOML, `toml::from_str` | `file.rs:121` |
| Top-level aliases | `[Nala]`/`[nala]`, `[Ui]`/`[ui]`, `[Color]`/`[color]` | `file.rs:26-33` |

### 6.2 `[Nala]` — `src/config/file.rs:36-69`

| Key | Type | Default | Effect | Source |
|---|---|---|---|---|
| `auto_remove` | bool | **true** | Enables implicit autoremove after transactions; skipped under `-y` unless explicit | `file.rs:39-40,61`; `configuration.rs:264-274` |
| `auto_update` | bool | **true** | Run `update` before upgrade unless `--no-update` | `file.rs:42-43,62`; `configuration.rs:295-299` |
| `update_show_packages` | bool | false | After `update`, list each upgradable package instead of the count line | `file.rs:45-46,63`; `update.rs:135-157` |
| `full_upgrade` | bool | false | `nala upgrade` behaves as `full-upgrade` | `file.rs:48-49,64`; `file.rs:110`; `main.rs:193` |
| `simple` | bool | false | Always use the simple transaction summary | `file.rs:51-52,65`; `configuration.rs:258` |
| `assume_yes` | bool | false | Default every prompt to yes | `file.rs:54-55,66`; `prompt.rs:23-25` |

### 6.3 `[Ui]` — `src/config/file.rs:71-88`

| Key | Type | Default | Values | Source |
|---|---|---|---|---|
| `mode` | `UiMode` | `Auto` | `Auto`, `Plain`, `Tui` (exact case; no aliases) | `file.rs:12-21,74-75,84` |
| `unit` | `NumSys` | `Binary` | `Binary` (KiB/MiB/GiB/TiB, base 1024), `Decimal` (KB/MB/GB/TB, base 1000) | `file.rs:77-78,85`; `util/units.rs:4-49` |

`unit` affects every human size string (`config.unit_str`): summary sizes, download sizes, progress totals (`configuration.rs:216-218`). `UnitStr` is constructed with precision 0 (`file.rs:119`; `units.rs:42-46`), and sizes `<=` the divisor fall through one unit (strict `>` comparison, `units.rs:41-46`).

### 6.4 `[Color]` — `src/config/color.rs:637-650`

| Key | Type | Default | Values | Source |
|---|---|---|---|---|
| `mode` | `Switch` | `Auto` | `Auto` (colour only when target is a TTY), `Always`, `Never` | `color.rs:637-642`; `config/mod.rs:20-26`; `color.rs:662-668` |
| `theme` | table | see 6.5 | — | `color.rs:565-585` |

`Auto` checks the *actual output stream*: `color()` always targets stdout; `color_with_target` can target stderr, and both check `is_tty()` independently (`color.rs:670-709`). Logger output therefore stays coloured on stderr only when stderr is a TTY.

### 6.5 `[Color.theme]` — `src/config/color.rs:564-635`

Nine `Style` entries; keys are `PascalCase` (`rename_all = "PascalCase"`).

| Key | Default `fg` | Default `modifier` / bg | Source |
|---|---|---|---|
| `Primary` | `LightGreen` | BOLD | `color.rs:588` |
| `Secondary` | `LightBlue` | BOLD | `color.rs:590` |
| `Highlight` | `Reset` | BOLD | `color.rs:592` |
| `Regular` | `Reset` | none | `color.rs:594` |
| `ProgressFilled` | `LightGreen` | BOLD | `color.rs:596` |
| `ProgressUnfilled` | `LightRed` | BOLD | `color.rs:598` |
| `Notice` | `LightYellow` | BOLD | `color.rs:600` |
| `Warning` | `LightYellow` | BOLD | `color.rs:602` |
| `Error` | `LightRed` | BOLD | `color.rs:604` |

`Style` shape: `{ fg = <color>, bg = <color>?, modifier = <modifiers>? }`; `modifier` defaults to BOLD when omitted; `bg` is optional; unknown keys inside a style are rejected (`color.rs:492-499`).

**Colour grammar** (`color.rs:75-274`):
- Named, case/space/`_`/`-` insensitive: `Reset`, `Black`, `Red`, `Green`, `Yellow`, `Blue`, `Magenta`/`Purple`, `Cyan`, `Gray`/`Grey`, `DarkGray`/`DarkGrey`/`BrightBlack`, `LightRed`/`BrightRed`, `LightGreen`/`BrightGreen`, `LightYellow`/`BrightYellow`, `LightBlue`/`BrightBlue`, `LightMagenta`/`BrightMagenta`/`LightPurple`/`BrightPurple`, `LightCyan`/`BrightCyan`, `White`/`BrightWhite`/`LightWhite`, `LightGray`/`LightGrey` (→ Gray) (`color.rs:105-130`). Note `LightGray` aliases to `Gray`, i.e. ANSI 37 — the only visibly surprising alias.
- `#RRGGBB` (exactly 6 hex digits) → truecolour (`color.rs:132-142`).
- `"0"..="255"` or a bare integer token → 256-colour index; out-of-range integers are rejected (`color.rs:201-240`).
- Table form `{ Rgb = [r, g, b] }` or `{ Indexed = n }`; any other key is rejected (`color.rs:249-274`).

**Modifier grammar** (`color.rs:281-399`): `BOLD`, `DIM`, `ITALIC`, `UNDERLINED`, `SLOW_BLINK`, `RAPID_BLINK`, `REVERSED`, `HIDDEN`, `CROSSED_OUT` (alias `STRIKETHROUGH`/`STRIKE`), given as a `" | "`-separated string or an array of strings; `""` means "no modifiers"; an unknown token is an error (`color.rs:315-346,377-394`).

The shipped `nala.conf` is exactly the default palette, so an untouched file is a no-op (`nala.conf:52-61`; test `file.rs:131-148`).

### 6.6 Validation and unknown/retired fields

| Level | Behaviour | Source |
|---|---|---|
| Unknown top-level table | Error (`deny_unknown_fields` on `ConfigFile`) | `file.rs:23-34`; test `file.rs:158-170` |
| Unknown key inside `[Nala]`/`[Ui]`/`[Color]` | Error | `file.rs:36-37,71-72`; `color.rs:637-638` |
| Unknown key inside `[Color.theme]` / a `Style` | Error | `color.rs:492-499,564-565` |
| Wrong enum casing (`mode = "auto"`) | Error — serde unit variants are exact | `file.rs:12-21`; `config/mod.rs:20-26`; `units.rs:4-11` |
| Missing tables/keys | Silently defaulted (`serde(default)`) | `file.rs:23-34`; test `file.rs:150-156` |
| Retired CLI options | Rejected by clap (exit 2): `--license`, `install --raw-dpkg`, `download --fetch` | `parser.rs:307-316` |

### 6.7 Precedence rules

1. **Explicit CLI flag > config file > built-in default** for the six `[Nala]` keys and for `color` mode (`configuration.rs:141-147,205-214`).
2. **Only `ValueSource::CommandLine` values become overrides** (`configuration.rs:56-60`). Consequently `--color`'s `default_value = "auto"` never masks `[Color] mode`, and an omitted flag never beats the file.
3. **`--no-color` > `--color` > file** — `--no-color` short-circuits to `Never` before looking at either (`configuration.rs:205-208`).
4. **`-t/--target-release` > `-o APT::Default-Release=…`**, because `-t` is applied after `-o` (`configuration.rs:99-123`).
5. **`--install-recommends`/`--install-suggests` (and their `no-` forms) > `-o APT::Install-Recommends|Install-Suggests`**, same reason (`configuration.rs:112-119`).
6. **`--tui`/`--no-tui` > `[Ui] mode`** (`configuration.rs:190-203`).
7. For paired `no_X` keys, negative wins: `get_no_bool` checks `no_<key>` first (`configuration.rs:220-227`). This only matters for `--full`/`--no-full`, the one pair *not* declared as a clap conflict (`commands.rs:271-277`); every other pair is mutually exclusive at parse time.

### 6.8 Complete runtime key map (what `Config::get_*` is actually asked for)

| Key | CLI flag | File field | Default | Read at |
|---|---|---|---|---|
| `assume_yes` | `-y/--assume-yes` | `Nala.assume_yes` | false | `prompt.rs:23`, `summary.rs:236`, `configuration.rs:273` |
| `assume_no` | `-n/--assume-no` | — | false | `prompt.rs:19`, `update.rs:46` |
| `auto_remove` | `--auto-remove`/`--autoremove` | `Nala.auto_remove` | true | `configuration.rs:279` |
| `no_auto_remove` | `--no-auto-remove`/`--no-autoremove` | — | false | `configuration.rs:269` |
| `auto_update` | — | `Nala.auto_update` | true | `configuration.rs:298` |
| `update_show_packages` | — | `Nala.update_show_packages` | false | `update.rs:135` |
| `full` | `--full` | `Nala.full_upgrade` | false | `main.rs:193` |
| `no_full` | `--no-full` | — | false | `main.rs:193` |
| `safe` | `--safe` | — | false | `main.rs:191` |
| `simple` | `--simple` | `Nala.simple` | false | `configuration.rs:258` |
| `purge` | `--purge` | — | false | `summary.rs:273`, `install.rs:92` |
| `remove_config` | `--remove-config` | — | false | `summary.rs:274` |
| `update` | `--update` | — | false | `configuration.rs:281` |
| `no_update` | `--no-update` | — | false | `configuration.rs:277` |
| `download_only` | `-d/--download-only` | — | false | `summary.rs:355` |
| `allow_unauthenticated` | `--allow-unauthenticated` | — | false | `configuration.rs:253` |
| `remove_essential` | `--remove-essential` | — | false | `summary.rs:222` |
| `fix_broken` | `-f/--fix-broken` | — | true (resolve) / false (install-no-args) | `install.rs:94`, `main.rs:129` |
| `install_recommends` / `no_install_recommends` | same names | — | true | `configuration.rs:114` |
| `install_suggests` / `no_install_suggests` | same names | — | false | `configuration.rs:118` |
| `target_release` | `-t/--target-release` | — | none | `configuration.rs:121` |
| `exclude` | `--exclude` | — | none | `upgrade/mod.rs:43` |
| `print_uris` | `--print-uris` | — | false | `summary.rs:315` |
| `machine` | `-m/--machine` | — | false | `cmd/mod.rs:225-229,240-244`; `policy.rs:289,299` |
| `all_versions` | `-a/--all-versions` | — | false | `cmd/show.rs:40`, `cmd/list.rs:13` |
| `all_arches` | `-A/--all-arches` | — | false | `configuration.rs:242` |
| `installed` | `-i/--installed` | — | false | `glob.rs:327` |
| `upgradable` | `-u/--upgradable` | — | false | `glob.rs:331` |
| `virtual` | `-V/--virtual` | — | false | `glob.rs:274,321` |
| `description` / `summary` | same names | — | false | `cmd/mod.rs:253-254` |
| `names_only` | `-n/--names` | — | false | `glob.rs:415` |
| `lists` / `fetch` | same names | — | false | `clean.rs:26,33` |
| `non_free` / `https_only` / `sources` | same names | — | false | `fetch/output.rs:20,65`, `score.rs:36` |
| `auto` | `--auto` | — | none (3 when value omitted) | `configuration.rs:251` |
| `country` | `-c/--country` | — | none | `fetch/mod.rs:18` |
| `debian` / `ubuntu` / `devuan` | same names | — | none | `release.rs:18-20` |
| `reinstall` | `--reinstall` | — | false | `main.rs:133` |
| `tui` / `no_tui` | `--tui` / `--no-tui` | — | false | `configuration.rs:190-203` |
| `color` | `--color` | `Color.mode` | Auto | `configuration.rs:205-214` |
| `no_color` | `--no-color` | — | false | `configuration.rs:206` |
| `verbose` / `debug` | `-v` / `--debug` | — | false | `configuration.rs:301-303` |
| `option` | `-o/--option` | — | none | `configuration.rs:99` |
| `history_dir` | `--history-dir` (env `NALA_HISTORY_DIR`) | — | `/var/lib/nala/history` | `configuration.rs:177-186` |
| `pkg_names` | positional | — | none | `configuration.rs:229-239` |

Only the six `[Nala]` fields, `[Ui] mode`, `[Ui] unit`, `[Color] mode` and `[Color.theme]` are file-backed (`file.rs:105-119`). Everything else is CLI/env-only, even though it flows through the same `get_*` API.

---

## 7. `Ui.mode` semantics and UI selection

### 7.1 Declared meaning — `src/config/file.rs:12-21`

| Mode | Doc comment | Practical meaning |
|---|---|---|
| `Auto` (default) | "Inline progress and required interactive screens." | Inline (non-alternate-screen) progress viewport; text summary + prompt; interactive full-screen **fetch picker** is allowed; no full-screen summary/history |
| `Plain` | "Static, line-oriented output." | No enhanced progress (plain stderr progress lines); text summary + prompt; fetch auto-selects; no full-screen anything |
| `Tui` | "Auto mode plus optional full-screen interfaces." | Everything `Auto` does, plus the summary and history detail screens in the alternate screen |

### 7.2 Resolution — `src/config/configuration.rs:190-203`

```
if no_tui:  file.mode == Tui ? Auto : file.mode
else if tui: Tui
else:        file.mode
```

`--tui` therefore upgrades `Plain` to `Tui`; `--no-tui` only downgrades `Tui` (to `Auto`) and leaves `Auto`/`Plain` alone. The two flags conflict at clap level (`parser.rs:28-34`).

### 7.3 Terminal gating — `src/terminal.rs:26-54`

| Predicate | Requirement |
|---|---|
| `terminal_supports_ui` | `stdout` is a TTY **and** `TERM` is not `dumb` (case-insensitive) |
| `use_enhanced_ui` | `mode != Plain` **and** `terminal_supports_ui` |
| `use_fullscreen_ui` | `mode == Tui` **and** `terminal_supports_ui` |

Note the asymmetry that matters for piping: the enhanced/plain decision inspects **stdout**, while `PlainProgress` renders its live bar on **stderr** when stderr is a TTY (`progress.rs:279-289,355-375`). So `nala upgrade > file` with a TTY on stderr uses the plain renderer, which then draws a progress bar on stderr.

### 7.4 Consumers and behaviour per mode

| Consumer | Gate | `Auto` | `Tui` | `Plain` / non-TTY |
|---|---|---|---|---|
| Progress rendering | `use_enhanced_ui` | inline ratatui viewport (stdout) | inline viewport | stderr lines / bar (`progress.rs:389-401,274-377`) |
| Transaction summary | `use_fullscreen_ui` | full text tables + prompt | full-screen summary tabs, unless `--simple`, `-y` or `-n` (`summary.rs:27-39`) | text + prompt (`summary.rs:38-39`) |
| History detail | `use_fullscreen_ui` | plain text detail | full-screen summary tabs (no confirm; `q`/Esc quits) (`history/mod.rs:76-87`; `tui/summary/mod.rs:228-232`) | plain text detail |
| `fetch` mirror picker | `use_enhanced_ui` | interactive picker | interactive picker | auto-select 3 (`fetch/mod.rs:86-88`) |
| Release-info confirmation | `use_enhanced_ui` | TUI screen | TUI screen | text prompt, default **no** (`update.rs:50-62`) |

Additional details: `--simple` always uses the simple summary and the text prompt, even in `Tui` (`summary.rs:21-25`). `-y`/`-n` suppress the full-screen summary because it would need a keypress (`summary.rs:27-30`), which means with `-y` you get the full text summary on stdout. The TUI summary title is `Nala Upgrade` for every transaction, and the history variant is `Nala History` (`tui/summary/mod.rs:198,229`; `main.ftl:263-264`).

---

## 8. Prompts

| Prompt | Exact text shown | Default | Skipped / overridden by | Source |
|---|---|---|---|---|
| Continue transaction | `Do you want to continue? [Y/n] ` | yes | `-y` → yes, `-n` → no; `--print-uris` skips the whole summary; nothing-to-do never prompts | `summary.rs:23,39`; `prompt.rs:26-31,62-68`; `main.ftl:24` |
| Release info (plain) | `Accept these changes and continue updating? [y/N] ` | **no** | `-y` → yes, `-n` → no; non-TTY → prints `Release information changes must be accepted explicitly. Re-run with '-o Acquire::AllowReleaseInfoChange=true' to accept them.` and rejects | `update.rs:46-62`; `main.ftl:34,38` |
| Release info (TUI) | Full-screen change list with help `(y) accept \| (n/Enter/Esc) reject` | reject | same flags | `tui/release_info.rs:16-33`; `main.ftl:34-37` |
| TUI transaction summary | key help `(q) quit \| (y) start upgrade` | quit | not shown when `-y`/`-n` | `tui/summary/mod.rs:26-33,326-332`; `main.ftl:269` |
| `fetch` picker | list with help `Use ↓↑ to move, Space to select/unselect, Home/End to go top/bottom, q/Enter to exit.` | — | not shown with `--auto` or non-enhanced UI | `tui/fetch.rs`; `main.ftl:485` |

**Answer handling** (`src/util/prompt.rs:70-87`): response is trimmed/lowercased; empty → the default; first char `n`/`y` always works; the translated yes/no keys also work; anything else → error `'<response>' is not a valid response` (exit 1, `main.ftl:22`). EOF on stdin is treated as **no** (`prompt.rs:33-35`). `assume_no` is checked before `assume_yes`, so both set → no (`prompt.rs:19-25`). The prompt hint characters are derived from the translated `prompt-choice` string (`Y/y N/n`) but sanitised: malformed patterns and ambiguous letters fall back to English `y`/`n` (`prompt.rs:41-60`).

**Refusal:** a transaction prompt answered no (including `-n`) bails with `User refused confirmation`, exit 1 (`prompt.rs:14`; `summary.rs:316-318`; `main.ftl:23`).

**No other prompts exist.** `history clear`, `clean`, `download`, `show`, `list`, `policy`, `moo` never prompt. Autoremove under `-y` prints an informational notice instead of prompting (`summary.rs:279-283`; `main.ftl:91`).

---

## 9. Root checks, APT lock and mutation classes

`sudo_check` is `Uid::effective().is_root()`; failure bails with `Nala needs root to <config.command>` (`util/privilege.rs:22-27`; `main.ftl:42`). `<command>` is the clap subcommand name recorded in `config.command` (e.g. `install`, `full-upgrade`, `purge`, `autoremove`).

Mutating commands get root check **and** an exclusive APT lock held for the process lifetime (`main.rs:81-86`; `util/privilege.rs:8-19`).

| Class | Commands | Root + lock |
|---|---|---|
| Mutating | `clean`, `fetch`, `update`, `upgrade`, `full-upgrade`, `safe-upgrade`, `install`, `remove`, `purge`, `autoremove`, `autopurge`, `history undo/redo/clear` | yes (`main.rs:153-169`) |
| Read-only | `list`, `search`, `show`, `policy`, `download`, `history` (list/detail/info), `moo` | no (`main.rs:170-175`) |

Redundant-but-harmless repeats exist: `autoremove`/`autopurge` call `sudo_check` again in dispatch (`main.rs:141`) and `history clear --all` calls it again inside `history()` (`history/mod.rs:21-23`). `download` deliberately does not need root even though it writes files — it writes to the current directory (`downloader.rs:24-25`).

The lock is taken *before* `--update`'s early update and before any package work (`main.rs:81-94`).

---

## 10. Exit codes and error output

| Situation | Exit | Output |
|---|---|---|
| Any successful command, including `Nothing to do.`, `--print-uris`, `--download-only`, `moo`, `history` with empty store | 0 | as described per command |
| `-h/--help`, `-V/--version`, `nala help` | 0 | clap help/version on stdout |
| clap usage error (unknown flag/alias, bad value, conflict, missing value) | **2** | clap error on stderr (`clap_builder-4.6.7/src/util/mod.rs:32`, `clap_builder-4.6.7/src/error/mod.rs:233-249`) |
| Any `anyhow` error: root required, prompt refused, package not found, no candidate, broken packages, config read/parse, fetch failures, history not found, invalid prompt answer | **1** | `error!` → stderr, prefixed with the themed `Error:` label, using `{err:?}` (full context chain) (`main.rs:57-71`) |
| No subcommand | **1** | root help on stdout; the `Subcommand not supplied` error line is suppressed (`main.rs:146-149,68`; `main.ftl:18`) |
| Bootstrap failure (config path/parse) | **1** | hard-coded bold bright-red `Error:` prefix + debug chain, bypassing theme/`--no-color` (`main.rs:41-47`) |
| `q` pressed during `update` | **1** | immediate `std::process::exit(1)` after cleanup → no destructors, no history (`update.rs:117-122`) |
| APT errors (`AptErrors` chain) | 1 | split into `error!` without `E: ` prefix for errors and `warn!` without `W: ` for warnings (`main.rs:60-67`) |

There is no distinct exit code for "declined" (1), "nothing to do" (0), or "partial success". Signalled/aborted dpkg transactions surface as the propagated error → 1 (`tests/deb/safety/run` asserts non-zero).

**Stream split:** summaries, tables, machine JSON, `Nothing to do.`, version lines and help go to **stdout** (`println!`); progress, notices, warnings, errors and debug go to **stderr** (logger default writer, `logger.rs:81-83`). `PlainProgress` writes to stderr; the enhanced inline viewport uses stdout. This matters for `xbp ... | grep` style contracts.

---

## 11. `-o` / `--option` passthrough

| Aspect | Behaviour | Source |
|---|---|---|
| Syntax | `-o KEY=VALUE`, repeatable; also after the subcommand because `global = true` | `parser.rs:36-38` |
| Missing `=` | Fatal `Option '<option>' is not supported` (exit 1) | `configuration.rs:101-103`; `main.ftl:300` |
| Scalar | `apt.set(key, value)` | `configuration.rs:107` |
| Vector | `KEY::=VALUE` strips `::` and calls `apt.set_vector(key, &[value])`, appending across repeats | `configuration.rs:104-106`; test `configuration.rs:461-484` |
| Applied | After CLI overrides and colour setup, before nala's own `APT::Install-Recommends`, `APT::Install-Suggests` and `APT::Default-Release` writes | `configuration.rs:97-124` |
| Consequence | `-o APT::Install-Recommends=…`, `-o APT::Install-Suggests=…` and `-o APT::Default-Release=…` are **overwritten** by nala's derived values unless the corresponding flag/config is also absent/consistent | `configuration.rs:112-123` |
| Used by nala internally | `APT::Get::AllowUnauthenticated`, `APT::Get::Force-Yes`, `APT::Get::allow-downgrades`, `APT::Get::allow-change-held-packages`, `APT::Get::allow-remove-essential`, `Acquire::Progress::Ignore::ShowErrorText` | `configuration.rs:253-256`; `summary.rs:222-258`; `update.rs:263-266` |

Real examples exercised by the repo's own tests: `Dir::State::status`, `Dir::State::lists`, `Dir::Etc::sourcelist`, `Dir::Etc::sourceparts=-`, `Dir::Cache::Archives`, `APT::Install::Pre-Invoke::=exit 23`, `DPkg::Pre-Install-Pkgs::=…`, `DPkg::Post-Invoke`, `APT::Install::Post-Invoke-Success`, `APT::Get::allow-downgrades=true`, `APT::Get::allow-change-held-packages=true`, `Acquire::AllowReleaseInfoChange=true` (`tests/deb/run`, `tests/deb/safety/run`; `main.ftl:38`).

---

## 12. Completion and generated manpages

### 12.1 Shell completion

| Aspect | Behaviour | Source |
|---|---|---|
| Mechanism | `clap_complete::CompleteEnv::with_factory(NalaParser::command).complete()` runs before config load and root checks, so completion works unprivileged and without a valid config | `main.rs:38-39` |
| Feature | `clap_complete` with `unstable-dynamic` | `Cargo.toml:36` |
| Invocation | wrappers source `COMPLETE=<shell> nala`; nala prints/executes a dynamic completer instead of running the command | `debian/nala.bash-completion:1-3`; `debian/_nala:1-5`; `debian/nala.fish:1-3` |
| Shipped shells | bash, zsh, fish (one packaging file each; no elvish/powershell wrapper ships in the Debian package) | `debian/install:1-3`; `debian/manpages` |
| Package-arg completion | `package_completion` — all cache packages including virtual, prefix-filtered, `BTreeSet`-sorted | `cli/completion.rs:22-34`; registered `commands.rs:47,95,110,134,268,298` |
| Installed-only completion | `installed_package_completion` — states Installed/HalfInstalled/UnPacked/HalfConfigured/ConfigFiles | `cli/completion.rs:36-58`; registered `commands.rs:326` |
| History-id completion | numeric `*.json` stems plus `last`, read from `NALA_HISTORY_DIR` or `/var/lib/nala/history` | `cli/completion.rs:60-94`; registered `commands.rs:143,170,186` |
| Failure mode | If the apt cache cannot be opened, completion returns no candidates (no error output) | `cli/completion.rs:15-25` |

Completion is value-completion only where registered; it does not add new flags or subcommand aliases beyond the clap surface.

### 12.2 Manpages

| Aspect | Behaviour | Source |
|---|---|---|
| Generator | `clap_mangen 0.3.0` in `build.rs`, from the same clap `Command` tree | `build.rs:1-3,27-44`; `Cargo.toml:44-48` |
| Scope | Root command plus every visible subcommand, recursively; `filter(\|cmd\| !cmd.is_hide_set())` excludes `moo`; `disable_help_subcommand(true)` suppresses clap's auto `help` page | `build.rs:29-37` |
| Section | `"8"` (system administration) | `build.rs:37` |
| Output | `$OUT_DIR`, one `.8` per command; filenames come from `clap_mangen::Man::get_filename` (derived from the clap command name) | `build.rs:55-66` |
| Packaging | Debian installs every generated `*.8` (`debian/cargo_out_dir/*.8`); the LTO build copies `*/out/*.8` out of the cargo target tree | `debian/manpages:1`; `debian/rules:15-19` |
| Content source | manpage text is clap's `about`/`long_about`/arg help strings; the binary `about` is `Commandline front-end for libapt-pkg` and version `0.18.2` | `parser.rs:8-11`; `Cargo.toml:2-3` |

---

## 13. Machine-readable output

| Command | Flag | Shape | Source |
|---|---|---|---|
| `list`, `search` | `-m/--machine` | One **pretty-printed JSON object per version** (multi-line), produced by `serde_json::to_string_pretty(&rust_apt::Version)` — schema owned by `rust-apt` 0.11.3, not defined in nala | `cmd/mod.rs:240-244,275`; `Cargo.toml:19` |
| `show` | `-m/--machine` | Same per-version JSON, one object per shown version | `cmd/mod.rs:225-229,275` |
| `policy <pkgs>` | `-m/--machine` | Pretty JSON **array** of `{ package, installed, candidate, versions: [ { version, priority, installed, candidate, sources: [ { priority, source } ] } ] }` | `policy.rs:13-34,275-283` |
| `policy` (no pkgs) | `-m/--machine` | Pretty JSON object `{ package_files: [ { priority, source, release, site, downloadable } ], pinned_packages: [ { name, version, priority } ] }` | `policy.rs:36-56,255-259` |
| `upgrade --print-uris` | `--print-uris` | Pretty JSON per planned URI, fields `uris` (array), `size`, `archive`, `partial`, `hash`, `filename`; internal fields skipped | `summary.rs:338-344`; `uri.rs:24-37,93` |

Notes: notices (`'{pkg}' was not found`) still go to stderr through the logger even in machine mode (`policy.rs:299-301`; `glob.rs:189-199`). Nothing else in the CLI has a machine mode — `history`, `fetch`, `clean`, `update`, `download` are human-only.

---

## 14. Mapping hooks for the downstream grilling tickets

Facts below are the nala side only; the xbps decision belongs to #43/#44/#49.

| Nala surface | Why it needs an explicit xbps decision | Source |
|---|---|---|
| `-n` = `--assume-no` | Collides with xbps dry-run `-n` (and xbpretty's current spec uses `-n` for dry-run). Nala has no dry-run flag at all; the closest analogue is `--download-only` or `--print-uris` | `flags.rs:28-34`; `xbpretty-spec.md:104` |
| `-d` = `--download-only` | Short-flag ownership: nala's `-d` is transaction-download-only, not a package selector | `flags.rs:8-10`; `parser.rs:107-117` test |
| `--install-recommends` / `--install-suggests` | apt-only metadata; Void/xbps has no recommends/suggests model → candidate N/A, but the flags also decide `APT::Install-*` which has no xbps equivalent | `flags.rs:57-76`; `configuration.rs:112-119` |
| `-t/--target-release`, `policy`, pin priorities | apt pinning/target-release has no xbps equivalent → `policy` may become N/A or a repository listing | `commands.rs:305-307`; `cmd/policy.rs` |
| `--remove-essential`, protected/Important packages | xbps has no `Essential`/`Important` metadata; replacement rule must be invented or N/A'd | `summary.rs:189-233` |
| `--allow-unauthenticated` | xbps verifies repository signatures, not per-package hashes; the closest behaviour is a repo trust check | `flags.rs:24-26`; `download/mod.rs:81-100` |
| `--print-uris` JSON | Requires a defined xbps URL schema; nala's Uri JSON is apt-shaped | `uri.rs:24-37` |
| `clean --lists`, `clean --fetch` | Nala-specific paths (`/var/lib/apt/lists`, `nala.sources`); xbps has `/var/db/xbps` and `repository=` config | `clean.rs:25-43`; `paths.rs:1-4` |
| `fetch` `--debian/--ubuntu/--devuan`, `--non-free` | Debian-family mirror catalogues and components; Void mirrors and `repository=` file need their own model | `fetch/mirrors.rs:8-35`; `release.rs:16-62`; `output.rs:51-91` |
| `history undo/redo` | Replay depends on recorded before/after states and downloadable archives; limits are explicit in messages | `history/mod.rs:32-39`; `main.ftl:452-465` |
| `upgrade --exclude` globs | Glob semantics are `globset` case-insensitive with `*?[]{}`; xbps `ignorepkg` is a different mechanism | `glob.rs:211-222`; `upgrade/mod.rs:42-77` |
| Summary columns | `Package, Version, Size`; upgrades add `Old Version, New Version`; held rows add `Reason` | `tui/summary/row.rs:93-117`; `main.ftl:98-103` |
| `Ui.mode` TUI scope | Full-screen surfaces are summary and history only; fetch has its own picker gated on enhanced UI, not `Tui` | `terminal.rs:38-54`; `summary.rs:27-36`; `history/mod.rs:76-87` |
| `-o` passthrough | No xbps config-key equivalent exists; needs a documented N/A or a translation table | `parser.rs:36-38`; `configuration.rs:99-110` |

---

## 15. Gaps and limitations

1. **`list` / `search` / `show --machine` JSON schema is not specified in nala.** It is `serde_json` of `rust_apt::version::Version` (`cmd/mod.rs:275`). To pin the exact fields for #44, read `rust-apt` 0.11.3's `Serialize` impl (crates.io, not in the nala tree).
2. **Exact manpage filenames** are produced by `clap_mangen::Man::get_filename`, which was not read from source here. The Debian package does not depend on the names (it globs `*.8`), so this only matters if xbpretty wants to reproduce nala's file naming.
3. **`clap_complete` 4.6.8 source was not available locally.** The dynamic-completion protocol is described from nala's `CompleteEnv` call plus the three shipped wrappers; the full list of shells the engine can emit (beyond bash/zsh/fish) is not asserted.
4. **No runtime execution of nala.** The host lacks `libapt-pkg-dev`, so behaviours are source-derived; the `tests/deb/*` scripts corroborate exit-code and nothing-to-do expectations but were not run.
5. **TUI screen layouts, colours and progress frame rendering** are out of scope here; see `docs/research/tui-rendering-architecture.md` for the visual layer.
6. **`history undo`/`redo` replay semantics** are only summarised via their error messages; a full replay-state model would be a separate study.
7. **i18n is excluded by map decision #37 (preference 10).** The English bundle is the only source cited; 15 other locales exist (`src/i18n/shared.rs:22-36`) and could change prompt hints (the prompt-key sanitiser at `prompt.rs:41-60` shows translations can alter the yes/no letters).
8. **`--color` case sensitivity** is inferred from clap's `PossibleValue` (lowercase only); not exercised against a built nala.

---

## 16. Quick-reference: the one-screen version

```
nala [-v|--debug] [--config FILE] [--history-dir DIR] [--tui|--no-tui]
     [-o KEY=VALUE]... [--color auto|always|never | --no-color] <command> ...

list     [PKGS]... [--description] [--summary] [-a] [-A] [-i] [-u] [-V] [-m]
search   [PKGS]...  (all list flags) + [-n|--names|--names-only]
show     [PKGS]...  [-a] [-A] [-m]                 (alias: info)
policy   [PKGS]...  [-m]
clean    [--lists] [--fetch]
download PKGS...
history  [ID|last] | info|show [ID|last] | undo ID|last | redo ID|last | clear (ID|last|--all)
fetch    [--non-free] [--https-only] [--sources] [--auto [N]] [-c CC]... [--debian R] [--ubuntu R] [--devuan R]
update
upgrade  [--print-uris] [--exclude PKG]... [--full|--no-full] [--safe] + shared
         (full-upgrade alias dist-upgrade forces --full; safe-upgrade forces --safe)
install  [PKGS]... [--reinstall] [-t RELEASE] + shared + fix-broken + recommends + autoremove
remove   PKGS... + shared + fix-broken + autoremove      (alias: uninstall)
purge    PKGS... (forces purge)
autoremove [--remove-config|--config] + shared
autopurge  (forces purge + autoremove)
moo      [--help] [--update] [--no-update]         (hidden)

shared = [-d|--download-only] [--simple] [--update|--no-update]
         [--allow-unauthenticated] [-y|--assume-yes|-n|--assume-no]
         [--remove-essential] [--purge]
fix-broken = [-f|--fix-broken | --no-fix-broken]
recommends = [--install-recommends|--no-install-recommends]
             [--install-suggests|--no-install-suggests]
autoremove = [--auto-remove|--autoremove | --no-auto-remove|--no-autoremove]
```

# Research: XBPS capability map for Nala v0.18.2 parity

- **Status:** Complete
- **Date:** 2026-11-05
- **Author:** Soubarna (background research agent)
- **Ticket:** [#41 (Research: xbps capability map for nala parity)](https://github.com/soubarnak/xbpretty/issues/41)
- **Map:** [#37 (Nala v0.18.2 parity for xbpretty)](https://github.com/soubarnak/xbpretty/issues/37)
- **Downstream consumers:** [#43](https://github.com/soubarnak/xbpretty/issues/43) (transaction commands), [#44](https://github.com/soubarnak/xbpretty/issues/44) (discovery/machine output), [#46](https://github.com/soubarnak/xbpretty/issues/46) (history/undo), [#47](https://github.com/soubarnak/xbpretty/issues/47) (predownload), [#48](https://github.com/soubarnak/xbpretty/issues/48) (mirror fetch)
- **Measured host:** Void Linux x86_64, `xbps` **0.60.7** (`XBPS: 0.60.7 API: 20250629 GIT: UNSET`), 1,487 installed packages, 15 configured repositories, `/var/cache/xbps` 4.1 GB / 242 archives. The Void `current` repository also ships `xbps-0.60.7_1`.
- **xbps sources:** `void-linux/xbps` tag `0.60.7`, commit `34e6a1a1a9917c881246ef538e312a3fc0228ed2` (2026-02-09). The files spot-checked against `master` on 2026-11-05 (`bin/xbps-install/main.c`, `lib/initend.c`) were identical.
- **Nala reference:** `volitank/nala` tag `v0.18.2` (`src/cli/commands.rs`, `src/cli/flags.rs`, `src/cli/parser.rs`).
- **Method:** read-only commands on the host (`xbps-query`, `xbps-install -n`, `xbps-remove -n`, `xbps-remove -O -n`, `xbps-pkgdb` on copies), plus end-to-end transactions in disposable `-r` roots under `/tmp` built with `xbps-create`/`xbps-rindex` and served over a loopback HTTP server. No system package, database, cache, or config file was modified; `find /var/db/xbps -newermt` after the probes showed no writes. Man pages were rendered with `mandoc` from the installed `0.60.7` package.

**Citation conventions.** `src:PATH:LINE` means a file in the xbps 0.60.7 tree at <https://github.com/void-linux/xbps/tree/0.60.7> (append `PATH#LNN` for a permalink); `man NAME(SECTION)` means the locally installed man page (mirrored at <https://man.voidlinux.org/>); `handbook` is a page under <https://docs.voidlinux.org/>; `OBS-x` is an observed command/output reproduced in Appendix A.

---

## Executive summary

1. **xbps covers the core transaction verbs, not the Nala shell.** `xbps-install`, `xbps-remove`, `xbps-query`, `xbps-pkgdb`, and `xbps-reconfigure` cover install/remove/update/downgrade/query/hold/orphan/cache operations, but there is **no history/undo/redo**, no `download` to the current directory, no mirror benchmark/fetch, no `policy`/pin priorities, no JSON/`--machine` output, and no `--print-uris`.
2. **Flags collide, so pass-through is impossible.** Nala's `-n` (assume-no) is xbps's dry-run; Nala's `-d` (download-only) is xbps's debug; `-S` means *sync* in `xbps-install` and *show* in `xbps-query`; `-R` means *repository* in install/query and *recursive* in remove; `-O` means *clean cache* in remove and *list orphans* in query; `-m` means *manual packages* in query and *mode* in pkgdb. A wrapper must translate flags, never forward them.
3. **The dry-run plan is the only stable machine surface, and only since xbps 0.60.** `-n` prints exactly six whitespace-separated fields (`pkgver action arch repository installed_size download_size`), no header (`src:bin/xbps-install/transaction.c:117-131`). Before 0.60 the same line was variable-length because zero sizes were omitted (`src` tag `0.59.2`). A downgrade is labelled `reinstall` in the plan but `downgrade` in the human table (`src:bin/xbps-install/util.c:182-190`; OBS-14/OBS-15).
4. **Repository selection is configuration-order-first, not best-version.** The first registered repository whose index contains a version matching the package expression wins (`man xbps-install(1) §PACKAGE EXPRESSION`; `src:lib/rpool.c:328-335`). `bestmatching=true` switches to greatest version across all repositories (`man xbps.d(5) §bestmatching`; OBS-9). A single repository index holds **one version per package name**; a bare `.xbps` file is not installable (OBS-11).
5. **Remote repositories must be RSA-signed; local repositories are hash-only.** Remote transactions verify each package's `.sig2` against the repository key (`src:lib/transaction_fetch.c:62-77`); local repositories verify only `filename-sha256` from their repodata (`src:lib/transaction_fetch.c:78-95`). There is **no `--allow-unauthenticated`** and a new repository key prompts even with `-y` (`man xbps-install(1) §OPTIONS -y`; OBS-12).
6. **The cache is not a vault and old versions are not directly installable.** `/var/cache/xbps` holds downloaded `pkgver.arch.xbps` + `.sig2`; only 218 of 1,487 installed packages (14.7%) had their exact current archive cached on the host. `xbps-install -f ./foo.xbps` and a bare cache directory both fail (`ERROR: Package '…' not found in repository pool.`). A verified downgrade requires indexing a temporary repository with `xbps-rindex -a` and installing with `-f -R <tmpdir>` (OBS-11).
7. **Cache cleaning is destructive in two different directions.** `xbps-remove -O` keeps only archives whose exact version is present in a *configured repository*; `-O -O` keeps only archives whose exact version is *installed* (`src:bin/xbps-remove/clean-cache.c:101-112`; OBS-16/OBS-17). A cached archive for an installed package is deleted by `-O` when its repository is no longer configured — exactly the archive an undo needs.
8. **Holds only cover install/update.** `xbps-pkgdb -m hold` stores a boolean in the pkgdb; held packages appear as a `hold` plan row whose `pkgver` is the *target* version, non-dry-run all-held updates print nothing and exit 0, and a held dependency aborts with exit 19 and `MISSING:` on stderr. Removal and `xbps-remove -o` ignore holds (OBS-13, OBS-18, OBS-19). `ignorepkg` is a configuration-file dependency override, not an exclude list; explicit installs/updates of an ignored package still happen (OBS-10).
9. **Exit codes are implementation detail.** The five core man pages have no `EXIT STATUS` section; the binaries return `errno`/libfetch values or 1 (`src:bin/xbps-query/main.c:339`, `src:bin/xbps-install/main.c:305`). Observed codes include 0 for sync failure, all-held no-ops, declined prompts, and already-installed; 1 for usage and signature failure; 2 for not-found; 16 for a missing remote `.sig2` (FETCH_UNAVAIL); 19 for unresolved/held dependencies and revdep-guarded removal; 22 for an invalid `--regex` in ownedby; 95 for no repository in show mode (Appendix A, §8).
10. **Version floor matters.** Void shipped xbps 0.59.2 from 2023-09-18 until 0.60 on 2025-06-14 and has shipped 0.60.x since; 0.60.7 landed 2026-02-09. The fixed six-field plan, `--staging`, `-O -O`, `--checks`, `XBPS_SYSLOG`, and exit-19 held-dependency handling are 0.60 features; `.sig2` starts at 0.59.2 (0.59.1 and earlier used `.sig`); zstd repodata requires ≥ 0.54.

---

## 1. What the Nala v0.18.2 surface actually asks for

Nala's clap surface (tag `v0.18.2`) is the parity target: commands `list`, `search`, `show`/`info`, `policy`, `clean`, `download`, `history` (with `info`, `undo`, `redo`, `clear`), `fetch`, `update`, `upgrade`, `full-upgrade`/`dist-upgrade`, `safe-upgrade`, `install`, `remove`/`uninstall`, `purge`, `autoremove`, `autopurge`; global flags `-v/--verbose`, `--debug`, `-c/--config`, `--tui/--no-tui`, `-o/--option`, `--color`, `--no-color`; and the shared transaction/install/fix-broken/autoremove flags (`-d/--download-only`, `--simple`, `--update/--no-update`, `--allow-unauthenticated`, `-y/--assume-yes`, `-n/--assume-no`, `--remove-essential`, `--purge`, `-f/--fix-broken`, recommends/suggests, `--autoremove`). Sources: [`src/cli/commands.rs`](https://github.com/volitank/nala/blob/v0.18.2/src/cli/commands.rs), [`src/cli/flags.rs`](https://github.com/volitank/nala/blob/v0.18.2/src/cli/flags.rs), [`src/cli/parser.rs`](https://github.com/volitank/nala/blob/v0.18.2/src/cli/parser.rs).

Two facts frame the rest of this document:

- Nala is an **APT front end**: its semantics (conffile keep/purge, pin priorities, recommends, release targeting, history replay, mirror sources) are libapt concepts. xbps implements a different, smaller model, so "parity" for some flags can only be an explicit, documented N/A or an xbpretty-owned implementation.
- Nala's own defaults (e.g. `auto_remove = true`, `auto_update = true`, `full_upgrade = false`, see [`nala.conf`](https://github.com/volitank/nala/blob/v0.18.2/nala.conf)) have no xbps equivalent; xbpretty must own those policies.

---

## 2. Capability matrix

### 2.1 Discovery and inspection

| Nala capability | xbps primitive | Observed behaviour (0.60.7) | Limits / N/A | Source |
|---|---|---|---|---|
| `list` installed | `xbps-query -l` | `ii pkgver short_desc`, aligned to the longest pkgver; state codes `ii` installed, `uu` unpacked, `hr` half-removed, `??` unknown. Order is pkgdb dictionary order (stable across runs, not guaranteed sorted). On a TTY truncated to terminal width with `...`; piped, no truncation. | No `--installed` flag needed. No JSON/machine flag. Only `short_desc`; no long description in repodata. | `man xbps-query(1) §MODE -l`; `src:bin/xbps-query/list.c:59-88,165-180`; `src:bin/xbps-install/util.c:42-56` |
| `list --upgradable` | no mode; compute from `xbps-install -n -u` | Plan rows are exactly the upgradable set, but held packages appear as `hold` rows and must be excluded from "upgradable". Missing repositories silently thin the set. | Needs repo metadata; no xbps-query flag; no counts without parsing the plan. | OBS-6, OBS-13; `src:bin/xbps-install/transaction.c:117-131` |
| `list --virtual` | `xbps-query -R -s PATTERN --property provides` / `-p provides` | Prints `pkgver: value (repo)` for each providing package; values are `cmd:`, `shlib:`, explicit `provides` entries. Repo search also matches virtual names through `provides` (`xbps_match_virtual_pkg_in_dict`). `virtualpkg=` can override resolution per config. | No virtual-package list mode; no way to enumerate all virtuals except scanning `provides`. | `man xbps-query(1) §OPTIONS --property,§PROPERTIES provides`; `man xbps.d(5) §virtualpkg`; `src:bin/xbps-query/search.c:102-104`; OBS-20 |
| `list --machine` | N/A | No machine output. The nearest surfaces are `-l` (fixed columns) and `-p` (values only). | xbpretty must synthesize its own schema. | `man xbps-query(1)` (no such option) |
| `search` (installed) | `xbps-query -s PATTERN [--regex]` | Searches installed pkgdb only; markers `[*]` always (installed); prints aligned `pkgver short_desc`. No matches → exit 0, no output. | Not a repo search; no `--names`, `--all`, `--installed`, `--description` switches. | `man xbps-query(1) §MODE -s`; `src:bin/xbps-query/search.c:63-72`; OBS-4 |
| `search` (repos) | `xbps-query -R -s PATTERN [--regex]` | Searches `pkgver` and `short_desc` of every repo package; markers `[*]` installed / `[-]` not installed. Non-regex mode is case-insensitive substring (`strcasestr`) plus xbps package-expression matching; `--regex` is a case-insensitive ERE applied to `pkgver` and `desc` separately. | `^figlet$` matches nothing (it is neither the pkgver nor the description); `^figlet-` matches pkgvers. No names-only mode. | `man xbps-query(1) §MODE -s`; `src:bin/xbps-query/search.c:90-140,154-199`; OBS-4 |
| `search --names` | N/A | — | Could be approximated with `--regex '^name-'`, but descriptions still participate in non-regex mode. | `man xbps-query(1)` (no such option) |
| `show`/`info` | `xbps-query -S PKG` (installed) / `xbps-query -R -S PKG` (repo) | `key: value` lines in plist-dictionary order; arrays are subsequent tab-indented values; bools are `yes`/`no`; numbers are humanised (`521KB`, `92KB`). Installed mode includes `state`, `install-date`, optional `automatic-install` (`yes`, `no` after `-m manual`, or absent when never set) and optional `hold` (present only when held); repo mode substitutes `repository` (the offering repo) and has no `state`. | No all-versions/all-arches; no JSON; numeric fields are lossy; absent properties print nothing. Same `repository` key means "origin repo" installed vs "selected repo" in `-R`. | `man xbps-query(1) §MODE -S,§PROPERTIES`; `src:bin/xbps-query/show-info-files.c:50-160`; OBS-3, OBS-5 |
| `policy` | partial: `xbps-query -R -S PKG -p repository,pkgver`, `xbps-pkgdb -m repolock`, `-H`, `bestmatching` | No priorities. `repolock=yes` restricts updates to the repository the package was installed from; hold pins a version; `bestmatching` changes global selection to highest version. | apt pin priorities, candidate tables, and per-repo versions are N/A. Repolock only works while the origin repo is still registered. | `man xbps-query(1) §PACKAGE MODES`; `man xbps-pkgdb(1) §-m`; `man xbps.d(5) §bestmatching`; OBS-21 |
| file ownership | `xbps-query -o PATTERN [-R] [--regex]` | Matches **full path** with fnmatch (leading wildcard needed), or ERE with `--regex`; output `pkgver: path -> target (regular file\|directory\|symlink\|configuration file)`. `-R` reads `files.plist` from repo packages (network-heavy). | Pattern is not a basename search: `xbps-query -o xbps-install` matches nothing, `/usr/bin/xbps*` does. | `man xbps-query(1) §MODE -o`; `src:bin/xbps-query/ownedby.c:64-95,185-205`; OBS-22 |
| dependencies | `xbps-query -x PKG` (direct), `-X PKG` (reverse), `--fulldeptree -x PKG` | Direct deps are pkg expressions, one per line; full tree is a flat DFS list of `pkgver` lines with no depth metadata; revdeps are pkgvers. | No tree structure/indentation; no machine format. | `man xbps-query(1) §MODE -x,-X,--fulldeptree`; OBS-23 |

### 2.2 Transactions

| Nala capability | xbps primitive | Observed behaviour (0.60.7) | Limits / N/A | Source |
|---|---|---|---|---|
| `update` (refresh lists) | `xbps-install -S` | Syncs every configured remote repo to `/var/db/xbps/<escaped-url>/<arch>-repodata`; prints `[*] Updating repository \`URL' ...` per repo on stdout. Local repos are skipped. Exit code is **0 even when every fetch fails**; failures are `ERROR: [reposync] failed to fetch file …` on stderr. `-S` is ignored when `-n` is present. | No package list output, no "N upgradable" summary, no failure exit code. `-M` is the only way to use fresh metadata without writing (memory sync). | `man xbps-install(1) §OPTIONS -S,-n,-M`; `src:bin/xbps-install/main.c:239-250`; `src:lib/rpool.c:61-81`; OBS-24 |
| `install` | `xbps-install [PKG…]` | Resolves deps, downloads/verifies, unpacks, configures. New deps are marked automatic; explicit args manual. Already-installed arg → stderr `ERROR: Package \`X' already installed.`, exit **0**. | No recommends/suggests; no `--fix-broken`; no `-t/--target-release`; no `--only-upgrade`; `-A` sets automatic for explicit args; `-I` ignores file conflicts; `-U` leaves packages unpacked (`uu`). | `man xbps-install(1) §OPTIONS`; `src:bin/xbps-install/transaction.c:339-383`; OBS-2, OBS-26 |
| `install --reinstall` | `-f` | Reinstall/force downgrade; one `-f` keeps modified conffiles with `Keeping modified configuration file \`…'.`; `-f -f` overwrites all files including conffiles. | No separate reinstall verb; `-f` also changes downgrade semantics. | `man xbps-install(1) §OPTIONS -f`; OBS-25 |
| `upgrade`, `full-upgrade`, `dist-upgrade` | `xbps-install -u` | One full-system transaction; held packages become `hold` rows; if xbps itself must update, the transaction is split and the first run may exit EBUSY (16) with `The 'xbps' package must be updated…` unless `-n`; upstream docs say run `-Su` twice. | No safe/full distinction, no `--exclude`, no `--print-uris`, no partial-upgrade mode. `ignorepkg` does **not** exclude from `-u` (OBS-10). | `man xbps-install(1) §OPTIONS -u`; handbook [XBPS](https://docs.voidlinux.org/xbps/index.html); `src:lib/transaction_ops.c:244-315`; OBS-10 |
| `upgrade PKG`, `--only-upgrade` | `xbps-install -u PKG` | Updates the named package **and its installed reverse dependencies**; if the package is not installed it is **installed** like a plain install (OBS-8). Held named package yields a `hold` plan row. | No "only if installed" switch; wrappers must pre-check with `xbps-query -S`. | `src:lib/transaction_ops.c:368-430`; OBS-8 |
| `remove` | `xbps-remove PKG…` | Removes files whose SHA-256 matches the pkgdb record; **modified files and modified conffiles are silently kept** (debug-only message); unmodified conffiles are deleted. Refuses when revdeps break (`in transaction breaks installed pkg …`, exit 19) unless `-F`. `-R` also removes now-unneeded automatic deps. | No `remove` vs `purge` split: xbps remove already deletes unmodified config, and `-f` is required to delete modified files. No `--remove-essential` concept. | `man xbps-remove(1) §DESCRIPTION,§OPTIONS -f,-F,-R`; `src:lib/transaction_files.c:314-370`; OBS-27, OBS-28 |
| `purge` | `xbps-remove -f` | Deletes modified files too (including conffiles). Observed: no message for the kept/deleted config decision in the normal path; untracked `.new-<version>` files remain. | Not a config-only purge; all modified files are force-deleted. | `man xbps-remove(1) §OPTIONS -f`; OBS-27 |
| `autoremove`, `autopurge` | `xbps-remove -o` (list with `xbps-query -O`) | Removes packages with `automatic-install=true` and no remaining revdeps; iterates until fixed point. Held/unpacked orphans are still removed (OBS-19). Nothing to remove → no output, exit 0. | No `--remove-config` mode; already-removed config state is not represented. | `man xbps-remove(1) §OPTIONS -o`; `src:lib/package_orphans.c:70-145`; OBS-19 |
| `--download-only` | `xbps-install -D` | Downloads `.sig2` then `.xbps` into `<cachedir>/pkgver.arch.xbps[.sig2]`, verifies, and stops before unpacking (no pkgdb lock). **All deps are assumed not installed** (whole download closure, including an xbps self-update closure) and it still prompts unless `-y`. For **local** repos it verifies then exits 0 with a misleading `N installed, N configured` footer and installs nothing. | No current-directory output; no `--print-uris`; works only for remote repos in practice; footer counters are plan counters, not actions. | `man xbps-install(1) §OPTIONS -D`; `src:lib/transaction_pkg_deps.c:218-226`; `src:lib/transaction_commit.c:157-159`; OBS-29, OBS-30 |
| `clean` (archives) | `xbps-remove -O` / `-O -O` | Default keeps only archives whose exact pkgver is in a **registered repository** (and hash matches); `-O -O` keeps only archives whose exact pkgver is **installed**. Both delete `.sig`/`.sig2` companions; print `Removed <file> from cachedir (obsolete)`; no footer; no prompt; no `-y` needed. `.part` files and stray `.sig2` files are ignored. | Two different destructive semantics choose which downgrade candidates survive; see §5. | `man xbps-remove(1) §OPTIONS -O`; `src:bin/xbps-remove/clean-cache.c:97-136,139-185`; OBS-16, OBS-17 |
| `clean --lists` | N/A | Repo indexes live under `/var/db/xbps/<escaped>/`; no command cleans them. Void troubleshooting recommends `rm /var/db/xbps/http*/*-repodata && xbps-install -S`. | xbpretty must implement; deleting indexes forces a full resync. | handbook [Common Issues](https://docs.voidlinux.org/xbps/troubleshooting/common-issues.html); OBS-31 |
| `download` (to cwd) | N/A | `-D` puts archives in the cache only; `xbps-query --cat` prints one file from an archive; `xbps-fetch` is a generic URL fetcher. There is no "download package to cwd" command. | URLs are constructible from repo+pkgver+arch; see §4. | `man xbps-install(1) §OPTIONS -D`; `man xbps-query(1) §MODE --cat` |
| local `.deb`-style install | N/A (`.xbps`) | `xbps-install ./foo.xbps` and `xbps-install /var/cache/xbps` both fail `ERROR: Package '…' not found in repository pool.` A directory only works as `-R <dir>` **if** it contains `<arch>-repodata`. | No bare archive install; build a repo with `xbps-rindex` (see §5). | `man xbps-rindex(1)`; OBS-11 |
| `history`, `undo`, `redo`, `clear` | N/A | pkgdb stores only current state plus per-package `install-date`/`repository`; syslog gets one line per successful install/update/remove if `syslog=true`. No transaction IDs, no inverse operations, no history file. | Must be xbpretty-owned (#46); cache replay is best-effort only. | `src:lib/package_register.c:54-84`; `src:bin/xbps-install/state_cb.c:104-148`; OBS-32 |
| `fetch` (mirrors) | N/A in xbps; external `xmirror` | xbps stores one `repository=` URL per config line; `xmirror` (separate package) rewrites `/etc/xbps.d/*-repository-*.conf` after choosing from `mirrors.lst`. No benchmark, scoring, or auto-selection in xbps. | xbpretty implements fetch (#48); authoritative list is xmirror's `mirrors.yaml` → `mirrors.lst`. | `man xbps.d(5) §repository`; handbook [Mirrors](https://docs.voidlinux.org/xbps/repositories/mirrors/index.html); OBS-33 |

### 2.3 Holds and pin-like controls

| Nala/apt concept | xbps mechanism | Observed behaviour | Limits | Source |
|---|---|---|---|---|
| hold / `apt-mark hold` | `xbps-pkgdb -m hold PKG`; `xbps-query -H`; `-p hold` | Sets `<key>hold</key><true/>` in `pkgdb-0.38.plist`; `-H` prints only `pkgver`; `-p hold` prints `yes`; absence means not held. `xbps-install -u` adds held packages as `hold` plan rows using the *candidate* pkgver; non-dry-run all-held transaction prints nothing and exits 0; `-f` updates a held package. | No config-file hold; requires pkgdb write (root for `/`). Removal/orphans ignore holds. | `man xbps-install(1) §PACKAGE MODES`; `man xbps-pkgdb(1) §-m`; `src:bin/xbps-pkgdb/main.c:58-85`; OBS-13, OBS-18 |
| ignore list / `--exclude` | `ignorepkg=` in xbps.d | Parsed into `ignored_pkgs`; only checked in dependency resolution pass 0: a dependency on an ignored package is treated as satisfied and the package is not installed. **Explicit install/update of an ignored package is unaffected**, and `-u` still updates it. | Not a hold/exclude; no CLI switch; no effect on removals. | `man xbps.d(5) §ignorepkg`; `src:lib/transaction_pkg_deps.c:170-176`; `src:lib/util.c:100-118`; OBS-10 |
| pin to origin repo | `xbps-pkgdb -m repolock PKG`; `--list-repolock-pkgs` | `repolock=yes`; updates are looked up only in the recorded origin repo. If that repo is no longer registered, lookups fail (`Package 'A' not found in repository pool.`). | Not a priority; can strand a package; root needed. | `man xbps-query(1) §PACKAGE MODES`; `src:lib/transaction_pkg_deps.c:360-369`; `src:lib/transaction_ops.c:109-122`; OBS-21 |
| pin a version | hold + no repository carrying the target version | Holds block upgrades but do not force a version; `-f` overrides. | No version pin syntax in config. | `man xbps-install(1) §PACKAGE MODES` |

---

## 3. Command and option reference (0.60.7)

### 3.1 `xbps-install`

Relevant options and measured semantics:

| Option | Meaning | Notes for parity |
|---|---|---|
| `-A, --automatic` | Mark explicit installs automatic | `man`/`src:bin/xbps-install/main.c:46` |
| `-C, --config DIR` | Config directory; relative paths resolved against rootdir | Replaces `/etc/xbps.d` but `/usr/share/xbps.d` is still read | 
| `-c, --cachedir DIR` | Cache dir | Relative to rootdir |
| `-D, --download-only` | Download + verify only | Assumes all deps missing; still prompts; local repo = no-op with misleading footer (OBS-29) |
| `-d, --debug` | Debug to stderr | Nala's `-d` is download-only — collision |
| `-f, --force` | Reinstall/downgrade; once keeps modified conffiles, twice overwrites | `src:bin/xbps-install/main.c:51-52`; OBS-25 |
| `-I, --ignore-file-conflicts` | Ignore conflicts | Added 0.55 (`NEWS:403`) |
| `-i, --ignore-conf-repos` | Ignore config repos | Only `--repository` remains; useful for isolating a temp repo |
| `-M, --memory-sync` | Fetch repodata to memory, ignore on-disk | Works unprivileged; `-Sn` cannot use it (no sync). `-M -n` is the fresh unprivileged plan |
| `-n, --dry-run` | Show plan only | Does not lock pkgdb or sync; six-field output (0.60+); Nala's assume-no collision |
| `-R, --repository URL` | Add repo at top; repeatable | Measured: **first** `-R` argument wins; local path needs `<arch>-repodata` |
| `-r, --rootdir DIR` | Target root | Root requirement = writable rootdir/metadir/cachedir, not euid |
| `--reproducible` | Omit install-date/repository from pkgdb | 0.58+ (`NEWS:285`) |
| `--staging` | Use staged remote packages | 0.60+; Void repodata has empty stage; env `XBPS_STAGING` is inverted (see §7.4) |
| `-S, --sync` | Sync repo indexes | Ignored with `-n`; exit 0 on fetch failures; key import only on sync |
| `-U, --unpack-only` | Unpack, don't configure | Leaves state `uu`; footer still says configured (OBS-34) |
| `-u, --update` | Update named pkgs or all | Named pkg that is missing gets installed; revdeps updated |
| `-v, --verbose` | Verbose | Adds `Found <pkgver> in repository <repo>` lines to **stdout**, polluting plan parsing (`src:bin/xbps-install/state_cb.c:71-74`) |
| `-y, --yes` | Assume yes | Does not suppress the new-key prompt (`man`; OBS-12) |

`--ignore-repo-errors` **does not exist** in 0.60.7 or master: `xbps-install --ignore-repo-errors` exits 1 with `unrecognized option` (OBS-35). `-o` and `-O` are also not install options (`invalid option`).

### 3.2 `xbps-remove`

Options: `-C`, `-c`, `-d`, `-F/--force-revdeps`, `-f/--force`, `-n`, `-O/--clean-cache`, `-o/--remove-orphans`, `-R/--recursive`, `-r`, `-v`, `-y`. (`man xbps-remove(1) §OPTIONS`.)

- `-O` cache cleaning runs **before** the pkgdb lock and exits immediately unless `-o` is also given (`src:bin/xbps-remove/main.c:266-276`).
- A missing package prints `Package \`X' is not currently installed.` on **stdout** and contributes to exit 2; if at least one other package is valid the transaction proceeds and the overall exit is 0 (`src:bin/xbps-remove/main.c:290-315`; OBS-36).
- The transaction summary/footer is shared with `xbps-install` because `xbps-remove` links `../xbps-install/transaction.o` (`bin/xbps-remove/Makefile`), so removal prints `0 downloaded, 0 installed, …, N removed, 0 on hold.` (`src:bin/xbps-install/transaction.c:495`).
- `-R` here is recursive removal, not repository; `-n` is dry-run, not assume-no.

### 3.3 `xbps-query`

| Option | Meaning | Observed notes |
|---|---|---|
| `-l` | List installed | state code + pkgver + short_desc; TTY truncation |
| `-L` | List repositories | `%5zd <uri> (RSA signed/unsigned)`; `-v` adds `Signed-by:` and `<bits> <fingerprint>`; missing repo data prints `-1`; `(Staged)` if stage data exists (`src:bin/xbps-query/list.c:188-205`) |
| `-H` | Holds | pkgver per line |
| `-m` | Manual packages | pkgver per line; **not** machine output |
| `-O` | Orphans | pkgver per line (from `xbps_find_pkg_orphans`) |
| `--list-repolock-pkgs` | Repolocked | pkgver per line |
| `-o PATTERN` | Owned files | full-path fnmatch/ERE; `-R` uses repo `files.plist` |
| `-S PKG` | Show info | **not sync**; installed by default |
| `-s PATTERN` | Search | installed only unless `-R`; `--regex` ERE; `-p PROP` searches a property |
| `-f PKG` | Files | paths; ` -> target` for symlinks |
| `-x` / `-X` | Deps / revdeps | direct only unless `--fulldeptree` |
| `--cat FILE PKG` | Print file from archive | man says repositories only; 0.60 added pkgdb mode (`NEWS:116`), but pkgdb mode needs a resolvable archive path |
| `-R` | Repository mode | not recursive |
| `--regex` | ERE for `-s`/`-o` | case-insensitive (`REG_ICASE`); invalid regex returns 1 for search, 22 silently for ownedby |
| `-p PROP[,PROP…]` | Property values | values only, no keys; missing properties skipped; numbers humanised |
| `-i` | Ignore config repos | `-L` then shows nothing |
| `-M` | Memory sync | fetch repodata to memory; usable unprivileged |
| `--staging` | Use staged remote packages | 0.60+ |

No `--machine`, `--names`, `--all`, `--installed`, `--upgradable`, `--virtual`, or `--json` modes exist.

### 3.4 `xbps-pkgdb` and `xbps-reconfigure`

- `xbps-pkgdb -m auto|manual|hold|unhold|repolock|repounlock PKG…`; silent on success; missing package → stderr `ERROR: xbps-pkgdb: failed to change to <mode> mode to <pkg>: No such file or directory`, exit 1 (`src:bin/xbps-pkgdb/main.c:58-85,212-226`).
- `xbps-pkgdb --checks files,dependencies,alternatives,pkgdb [-a|PKG…]`: silent + exit 0 when clean; **missing package prints `Package X is not installed.` on stdout and still returns 0** (`src:bin/xbps-pkgdb/check.c:93-98`; OBS-37). `--checks` is 0.60+ (`NEWS:167`).
- `xbps-reconfigure -a|-f|-i PKG|-x|--fulldeptree`; no dry-run option (`-n` is rejected, OBS-38). It only processes unconfigured packages unless `-f`; output `PKG: configuring ...` / `PKG: configured successfully.` (`src:bin/xbps-reconfigure/main.c:42-54,70-90`).

### 3.5 Flag collisions (translation table)

| Flag | `xbps-install` | `xbps-remove` | `xbps-query` | `xbps-pkgdb` | `xbps-reconfigure` | Nala |
|---|---|---|---|---|---|---|
| `-n` | dry-run | dry-run | — | — | invalid | assume-no |
| `-d` | debug | debug | debug | debug | debug | download-only |
| `-D` | download-only | — | — | — | — | — |
| `-S` | sync | — | show | — | — | — |
| `-s` | — | — | search | — | — | — |
| `-R` | repository | recursive | repository mode | — | — | — |
| `-r` | rootdir | rootdir | rootdir | rootdir | rootdir | — |
| `-O` | invalid | clean-cache | orphans | — | — | — |
| `-o` | invalid | orphans | ownedby | — | — | apt option |
| `-m` | — | — | manual pkgs | mode | — | machine |
| `-u` | update | — | — | pkgdb format update | — | — |
| `-f` | force | force | files | — | force | fix-broken |
| `-F` | — | force-revdeps | — | — | — | — |
| `-i` | ignore conf repos | — | ignore conf repos | — | ignore PKG | — |
| `-x` | — | — | deps | — | deps | — |
| `-y` | yes | yes | — | — | — | assume-yes |
| `-v` | verbose | verbose | verbose | verbose | verbose | verbose |
| `-C` | config dir | config dir | config dir | config dir | config dir | config file |

(Derived from each tool's usage text: `src:bin/xbps-install/main.c:43-69`, `src:bin/xbps-remove/main.c:46-67`, `src:bin/xbps-query/main.c:44-80`, `src:bin/xbps-pkgdb/main.c:40-56`, `src:bin/xbps-reconfigure/main.c:40-54`.)

---

## 4. Repository metadata, selection, signing, and mirrors

### 4.1 Repodata on disk

A repository is a directory (or URL) containing `<arch>-repodata` and `pkgver.arch.xbps` archives plus `.sig2` files (handbook [Repositories](https://docs.voidlinux.org/xbps/repositories/index.html)). On the host:

```
$ file /var/db/xbps/https___repo-de_voidlinux_org_current/x86_64-repodata
Zstandard compressed data (v0.8+), Dictionary ID: None
$ tar --zstd -tf …/x86_64-repodata
index.plist          # 20,715,593 B
index-meta.plist     # 1,459 B
stage.plist          # 0 B
```

`index.plist` is an XML plist keyed by **package name**; each value is a package dictionary (`pkgver`, `architecture`, `filename-sha256`, `filename-size`, `installed_size`, `run_depends`, `provides`, `replaces`, …). `index-meta.plist` carries `public-key`, `public-key-size`, `signature-by`, `signature-type` (`src:lib/repo.c:186-231`; OBS-39). `stage.plist` is the merged staging data introduced in 0.60 (empty on Void; `NEWS:130-136`).

The local cache path is `/var/db/xbps/<escaped-repo-uri>/<arch>-repodata`; escaping replaces `.`, `/`, `:` with `_` (`src:lib/repo_sync.c:42-75`), e.g. `https___repo-de_voidlinux_org_current`. The whole `x86_64-repodata` on the host is 2,219,849 bytes (OBS-39).

### 4.2 Package URL and hash layout

For a package selected from repository `R` with `pkgver = P` and package `architecture = A`:

```
package:   R/P.A.xbps
signature: R/P.A.xbps.sig2
repodata:  R/<target-arch>-repodata        # target arch, not package arch
```

`src:lib/transaction_fetch.c:116-120` constructs exactly those URLs; `src:lib/util.c:325-339` uses the same layout for local repos/cache. A noarch package therefore has `…noarch.xbps` in a repo whose index file is `x86_64-repodata` (OBS-40). HEAD requests against `repo-de` confirm: `figlet-2.2.5_6.x86_64.xbps` → 200, `content-length: 94372`; `.sig2` → 200, `content-length: 512`; `x86_64-repodata` → 200, `content-length: 2219849` (OBS-41).

**No command prints the URL.** `xbps-query -R -p repository` gives `R`; `-p pkgver` gives `P`; `-p architecture` gives `A` (exact strings). `-p filename-sha256` gives the exact hash; `-p filename-size` gives a **humanised** size (`92KB`), not bytes (`src:bin/xbps-query/show-info-files.c:62-70`). Exact byte sizes are available only for packages in a transaction, via the last two fields of `xbps-install -n`; for arbitrary packages an implementation must read `index.plist` itself or accept the lossy size. This is the central constraint for #47.

### 4.3 Repository selection and precedence

- **First matching repository wins**: "The first repository matching the package expression wins" (`man xbps-install(1) §PACKAGE EXPRESSION`). `src:lib/rpool.c:328-335` selects `REAL_PKG` unless `bestmatching`.
- **Matching includes version constraints**: with repo-old (0.8.0) first and repo-new (1.0.0) second, `taema>=0.9` resolves to 1.0.0 from the second repo, while `taema` resolves to 0.8.0 from the first (OBS-9). Exact old versions fall through to a later repo that still indexes them.
- **One version per package per repository index.** Adding both `taema-0.8.0_1` and `taema-1.0.0_1` to one repo indexes only 1.0.0; `xbps-query -R -p pkgver taema-0.8.0_1` exits 2. Re-running `xbps-rindex -f -a taema-0.8.0_1` overwrites the entry so the repo serves 0.8.0 (OBS-11). This is why undo cannot use a repo that has moved on.
- **`bestmatching=true`** picks the greatest version across all repos, including for dependencies (`man xbps.d(5) §bestmatching`; `src:lib/rpool.c:331-332`; OBS-9).
- **`-R` / `--repository`** adds to the list; measured order is "first argument wins" for both query and install, even though the man page says each is added to the top (`man xbps-install(1) §OPTIONS -R`; OBS-9).
- **Config precedence:** `/etc/xbps.d` (or `-C DIR`) is parsed before `/usr/share/xbps.d`; only `*.conf`, alphabetically; a file with the same basename in the config dir masks the system file; later lines append repositories (`man xbps.d(5) §DESCRIPTION`; `src:lib/conf.c:300-318,472-495`). Measured: a config-dir `00-repository-main.conf` suppressed `/usr/share/xbps.d/00-repository-main.conf` entirely (OBS-42).
- **Duplicates** of the exact same repository string are stored once (`src:lib/repo.c:394-430`).

### 4.4 Signing and trust

- Remote repositories **must** be signed; local repositories do not need signatures (handbook [Repositories](https://docs.voidlinux.org/xbps/repositories/index.html), [Signing Repositories](https://docs.voidlinux.org/xbps/repositories/signing.html)).
- The repository signature lives in `index-meta.plist`; `xbps-rindex --privkey … --sign --signedby …` initializes it and `--sign-pkg` signs each archive (`handbook signing`; OBS-43).
- A remote package is verified by RSA/SHA-256 over the archive against the repository key; failure removes both archive and signature and returns `EPERM` (exit 1): `ERROR: <pkg>: the RSA signature is not valid!` (`src:lib/transaction_fetch.c:62-77`; `src:lib/verifysig.c:92-125`; OBS-30).
- Local repositories verify only `filename-sha256` against their own repodata; the archive's `.sig2` is not checked (`src:lib/transaction_fetch.c:78-95`). A temp repo built from `/var/cache/xbps` therefore installs on the strength of its new hash, not the Void signature (OBS-11).
- Keys are stored in `/var/db/xbps/keys/<fingerprint>.plist`. A new key is imported only during `-S` (`src:bin/xbps-install/main.c:239-250`), prompts even with `-y`: stdout gets `` `URL' repository has been RSA signed by "NAME"`` + `Fingerprint: …`, stderr gets `Do you want to import this public key? [Y/n] ` (OBS-12).
- There is no `--allow-unauthenticated`, no `--ignore-signatures`, and no way to install from an unsigned **remote** repository: missing `.sig2` fails with `ERROR: [trans] failed to download … signature …: Not Found`, exit 16 (FETCH_UNAVAIL) (OBS-30); an imported-key mismatch fails EPERM.

### 4.5 Mirrors and `/etc/xbps.d`

- A mirror is just a base URL used in `repository=` lines; xbps has no mirror ranking, latency probing, or country filtering (`man xbps.d(5) §repository`).
- Defaults are installed in `/usr/share/xbps.d/00-repository-main.conf` (and multilib/nonfree files); local overrides are copies in `/etc/xbps.d` with the **same filename**, as documented in handbook [Changing Mirrors](https://docs.voidlinux.org/xbps/repositories/mirrors/changing.html): copy `*-repository-*.conf`, `sed` the base URL, then `xbps-install -S`.
- The authoritative mirror list is `xmirror`'s [`mirrors.yaml`](https://github.com/void-linux/xmirror/blob/master/mirrors.yaml); xmirror itself fetches `https://xmirror.voidlinux.org/raw/mirrors.lst` (33 enabled entries, TSV `region<TAB>url<TAB>location<TAB>tier`; 4 tier 1, 29 tier 2) and offers a TUI (`xmirror/Makefile:4`; OBS-33).
- xmirror's rewrite algorithm is the de-facto spec for `xbpretty fetch`: find `ROOT/usr/share/xbps.d/*-repository-*.conf`, map each to `ROOT/etc/xbps.d`, copy it, `sed -i "s|https://repo-default.voidlinux.org|${REPOSITORY%/}|g"`, leave `.old` backups, then sync; it refuses to run without root and can reset to default (`xmirror.in:133-166,168-190`; `man xmirror.1`).
- Mirrors are ordinary HTTPS URLs; xbps also accepts `http://` and `ftp://` (`src:lib/util.c:62-72`). The handbook default is `repo-default.voidlinux.org`, a Tier-1 anycast alias.

---

## 5. Cache, downgrade, and undo

### 5.1 What is in `/var/cache/xbps`

- Files: `<pkgver>.<arch>.xbps` plus `<pkgver>.<arch>.xbps.sig2`, written by `-D` or by an ordinary remote install (`src:lib/transaction_fetch.c:150-158`); downloads resume from `<file>.part` via HTTP range (`src:lib/download.c:135-170`).
- Local-repository installs do **not** copy archives into the cache: `download_binpkg()` returns `ENOTSUP` for local repositories and `xbps_transaction_fetch()` only fetches remote packages (`src:lib/transaction_fetch.c:112-114,222-240`; OBS-29). Undo therefore has no archive for packages installed from a local repo/`-R`.
- Measured coverage on the host: 1,487 installed packages; 242 cached `.xbps`; only **218** installed packages have their exact current pkgver cached (14.7%); 24 cached pkgvers are not currently installed (old versions of installed packages or removed packages). Cache is root-owned mode 755, files 644; unprivileged reads work, writes need root.
- A package is considered cached for installation only when **both** `.xbps` and `.sig2` exist (`src:lib/util.c:451-477`). Deleting only `.sig2` makes xbps re-fetch the signature (it reuses the `.xbps` via HTTP `If-Modified-Since`/304) (OBS-44).

### 5.2 Old versions are not directly installable

- A bare archive path or a directory of archives is not a package source: `xbps-install /var/cache/xbps/foo.xbps` → `ERROR: Package '…' not found in repository pool.` (exit 2); `xbps-install -R /var/cache/xbps foo` only works if `/var/cache/xbps/x86_64-repodata` exists (it does not) (`man xbps-install(1) §OPTIONS -R`; OBS-11).
- A repository index holds one version per name (§4.3), so even a repo that still has the old archive on disk will resolve the name to its indexed version.
- Verified undo recipe (executed, OBS-11):
  1. copy the old archive(s) from the cache to a temp dir (preserve `.sig2` if desired);
  2. `xbps-rindex -a <tmpdir>/<pkgver>.<arch>.xbps` (add `-f` if the index already has a newer entry);
  3. `xbps-install -f -R <tmpdir> PKG` (or the exact `pkgver`).
  The plan labels this `reinstall`; the human table labels it `downgrade` (OBS-14, OBS-15); the final version is the old one. Verification is SHA-256 only. Config files follow normal upgrade rules (`Updating configuration file …` when unmodified).
- `-f` is mandatory: without it the transaction reports `Package \`PKG' already installed.` and does nothing (`man xbps-install(1) §DESCRIPTION`; OBS-11).

### 5.3 Cache cleaning destroys undo material

`xbps-remove -O` (default) walks only `*.xbps` files and, for each, looks up the **exact pkgver** in the repository pool. If found and the file hash matches, the archive is kept; otherwise it is removed together with `.sig`/`.sig2`. `-O -O` switches the lookup to the pkgdb (exact pkgver installed) (`src:bin/xbps-remove/clean-cache.c:97-136`).

Measured consequences:

- With the repository that serves the cached versions configured, `-O` kept both archives silently (OBS-16).
- With an empty config (repo no longer configured), `-O` printed `Removed A-1.0_1.noarch.xbps from cachedir (obsolete)` for **installed** packages and deleted both archives (OBS-16).
- On the host, `-O` listed 22 obsolete archives (old kernel/nvidia/vivaldi/etc.); `-O -O` listed those plus 2 current-but-uninstalled archives (`whatsapp-linux`, `xdotool`), 24 total (OBS-17).
- `.part` files and stray `.sig2` files are ignored by the cleaner and persist (`src:bin/xbps-remove/clean-cache.c:165-171`; OBS-45).
- There is no vault: xbps retains only what the cleaner allowed to survive; no command restores an archive once deleted.

---

## 6. Output, progress, locale, and machine-readable surfaces

### 6.1 Dry-run plan (the only stable machine format)

`xbps-install -n` and `xbps-remove -n` print one line per package (`src:bin/xbps-install/transaction.c:117-131`):

```
<pkgver> <action> <arch> <repository> <installed_size> <download_size>
```

- Actions: `install`, `update`, `reinstall`, `remove`, `configure`, `hold`, `download` (`src:bin/xbps-install/util.c:100-126`). **No `downgrade`** — a forced downgrade is `reinstall` (OBS-14). The human table adds `downgrade`/`replaced`, so the two surfaces disagree (`src:bin/xbps-install/util.c:170-190`).
- Fields are always present on 0.60.7, with `-` for missing repo/arch and `0` for missing sizes. On 0.59.2 the same function printed only pkgver+action, then arch/repo if present, then each non-zero size (`git show 0.59.2:bin/xbps-install/transaction.c`): 4–6 fields, ambiguous when only one size exists. A parser that assumes six fields must gate on 0.60.
- Order is transaction order (dependencies before dependents), not sorted; a `hold` row's pkgver is the candidate version, not the installed one (OBS-13).
- `-v` prepends `Found <pkgver> in repository <repo>` lines on stdout (`src:bin/xbps-install/state_cb.c:71-74`); `-d` writes `[DEBUG] …` to stderr.
- Empty transaction → no output, exit 0. No "Nothing to do." string is printed by xbps (that is an xbpretty message).

### 6.2 Human transaction summary

Non-dry-run transactions print a summary before the prompt. Two layouts:

- **Piped / narrow** (`get_maxcols()==0` when stdout is not a TTY, or width ≤ `pkgname_len+61`): `N package(s) will be installed:` followed by `  <pkgver> ` lines (two leading spaces, trailing space), blank line, sizes, then phases (`src:bin/xbps-install/transaction.c:195-290`; `src:bin/xbps-install/util.c:42-56`; OBS-1).
- **TTY wide**: a fixed table:

```
Name Action    Version           New version            Download size
B    install   -                 1.0_1                  -
A    install   -                 1.0_1                  -
```

  Removal and downgrade use the same columns (`A remove 1.0_1 - -`; `A downgrade 2.0_1 1.0_1 -`) (OBS-14, OBS-28; `src:bin/xbps-install/util.c:130-230`).

Phases and per-package lines are on stdout (`src:bin/xbps-install/state_cb.c:75-148`); the interactive prompt `Do you want to continue? [Y/n] ` is on **stderr**, prints no newline, and a `n` answer prints `Aborting!` on stdout and exits **0** (`src:bin/xbps-install/transaction.c:483-490`; OBS-46). Footer:

```
N downloaded, N installed, N updated, N configured, N removed, N on hold.
```

with `configured = cf_pkgcnt + inst_pkgcnt + up_pkgcnt` — i.e. it is a plan counter, not a count of actually configured packages (`src:bin/xbps-install/transaction.c:494-501`). `-U` therefore ends with `N configured` while packages are `uu` (OBS-34), and `-D` ends with `installed/configured` counts while nothing was installed (OBS-29). `xbps-remove` prints the same footer with `removed` set.

### 6.3 Progress telemetry

Detailed formats, regexes, and PTY-vs-pipe behaviour are already documented in [`xbps-progress-telemetry.md`](xbps-progress-telemetry.md) (§2–§4). The parity-relevant facts:

- `isatty(STDOUT_FILENO)` at transfer start decides routing: TTY → progress lines on **stderr** with `\033[K\r`, overall `[%2d%%]`; pipe → progress lines on **stdout** with `\n`, no overall percentage (`src:bin/xbps-install/fetch_cb.c:144,175-180,194-211`).
- Downloads are **serial**: signature first, then archive, one package at a time (`src:lib/transaction_fetch.c:222-259`; OBS-30 shows sig → pkg per package).
- Rate/ETA are throttled to one update per second; ETA `unknown`/`-- stalled --` are possible (`existing research`; `src:bin/xbps-install/fetch_cb.c`).
- No machine-readable progress channel, no JSON, no percentage on stdout when piped.

### 6.4 Locale and colour

- There is no gettext/libintl in xbps (`grep -r gettext bin lib include` finds nothing); all output is English. `LC_ALL` was tested as `C`, `POSIX`, `C.utf8`, `en_US.utf8`: `xbps-query -l`, `xbps-install -n figlet`, and `xbps-query -R -s figlet` produced byte-identical output (md5sums equal). `install-date` is stored at install time with `strftime("%F %R %Z")`, so it carries the install-time timezone, not the reader's locale (`src:lib/package_register.c:66-83`). The only `setlocale` calls are in maintainer-script environment setup and HTTP date parsing (`src:lib/transaction_commit.c:114`, `src:lib/fetch/http.c:514-525`).
- `xbps-query -S` bolds keys (ANSI `\033[1m`) only when stdout is a TTY **and** `NO_COLOR` is unset (`src:bin/xbps-query/show-info-files.c:126-135,169-177`). There is no `--color`/`--no-color`. Dry-run plans and transaction summaries are never colourised. `NO_COLOR` has no effect on progress.

### 6.5 Machine-readable inventory

| Surface | Machine usable? | Caveats |
|---|---|---|
| `xbps-install -n` / `xbps-remove -n` plan | Yes (0.60+) | six fields; `hold`/`reinstall` semantics; no header; verbose pollutes |
| `xbps-query -p PROP[,PROP]` | Values only | no keys; missing props silently omitted; numbers humanised; bools `yes`/`no`; arrays one value per line |
| `xbps-query -R -s PAT -p PROP` | Yes-ish | `pkgver: value (repo)`; substring (`strcasestr`) or regex; bools matched as `true`/`false` here (not `yes`/`no`) |
| `xbps-query -l` | Fixed-width text | TTY truncation; state codes; descriptions; no keys |
| `xbps-query -L [-v]` | Fixed-width text | count, URI, signed; `-1` unavailable; verbose adds key details |
| `xbps-query -S` | Text plist-ish | dictionary order, humanised sizes, tab arrays |
| pkgdb (`/var/db/xbps/pkgdb-0.38.plist`) | Internal XML plist | stable format name, not a public API; root-only writes |
| repodata `index.plist` | XML plist | complete metadata incl. exact sizes/hashes; zstd tar; large |
| syslog (`/var/log/socklog/xbps/current` on Void) | Text | per-package lines, no transaction ids; depends on `syslog=true` and root-readable logs |

No command offers JSON, XML, YAML, or a `--machine` mode.

---

## 7. Exit codes, root, locking, and proxy

### 7.1 Exit codes are not a contract

None of the five core man pages has an `EXIT STATUS` section (verified: `grep -c 'EXIT STATUS'` = 0 for `xbps-install.1`, `xbps-remove.1`, `xbps-query.1`, `xbps-pkgdb.1`, `xbps-reconfigure.1`). The tools return `errno` values, libfetch codes, or `EXIT_FAILURE` (`src:bin/xbps-query/main.c:339`, `src:bin/xbps-install/main.c:305`, `src:bin/xbps-remove/main.c:314`). Observed:

| Code | Situation (observed) |
|---|---|
| 0 | success; empty dry-run; already installed (`xbps-install`); up to date (`-u PKG`); **all-held real update** (silent); declined prompt (`Aborting!`); `-S` with every fetch failing; `xbps-remove -o` with no orphans; `xbps-query` search with no matches; `-p` missing property; `xbps-pkgdb --checks` missing package |
| 1 | usage/invalid option; non-root system transaction at pkgdb lock; `xbps-pkgdb -m` missing pkg; `xbps-reconfigure` missing pkg; `xbps-query -R -s` with no repos; invalid `--regex` in search; RSA signature failure (EPERM); transaction wrapper failure |
| 2 | package not found (`xbps-install`, `xbps-query -S/-x/-X/-f`, `xbps-install -n -u missing`); `xbps-remove` package not installed (message on stdout); `xbps-query -R -S` when the only usable repository cannot be opened (proxy test) |
| 16 | missing remote `.sig2` (libfetch `FETCH_UNAVAIL`); `The 'xbps' package must be updated` (EBUSY) in non-dry-run |
| 19 | unresolved dependency (incl. held dependency); removal blocked by revdeps |
| 22 | invalid `--regex` in `xbps-query -o` (silent, EINVAL) |
| 95 | `xbps-query -R -S` with no usable repositories (ENOTSUP) |

Consequences: a wrapper cannot distinguish success from "nothing happened" or from a skipped/failed sync by exit code alone; it must parse stderr/stdout. This is why ADR-0006 exists for hints.

### 7.2 Root, locks, and writable roots

- There is no euid check. `xbps_pkgdb_lock()` checks `access(rootdir, W_OK)` and creates/locks `<metadir>/lock` (`src:lib/pkgdb.c:67-100`). On `/` as uid 1000: `ERROR: failed to check whether the root directory is writable: /: Permission denied`, exit 1 (OBS-47). In a writable `-r /tmp/...` root, the same uid completed installs/removals/reconfigures/cache cleans (all lab OBS).
- Dry-run (`-n`) and download-only (`-D`) skip the pkgdb lock (`src:bin/xbps-install/main.c:255-260`), so unprivileged planning works; `-D` still needs a writable cachedir (root-owned by default), while `-M -n` needs no writes at all (OBS-24).
- `-S` writes repodata into `<metadir>`; as non-root each inaccessible repo emits `ERROR: [reposync] failed to fetch file …: Permission denied` but the command exits 0 (OBS-24).
- A held database waits: `WARNING: package database locked, waiting...` and blocks rather than failing (ADR-0002 §4; `NEWS:27-29`).
- Holds/repolock/auto/manual require a writable pkgdb; cache cleaning requires write access to the cachedir; `/etc/xbps.d` requires root unless `-C` points elsewhere.

### 7.3 Proxy and TLS

From `man xbps-install(1) §ENVIRONMENT` and libfetch (`src:lib/fetch/common.c:1963-2005`, `src:lib/fetch/http.c:811-813`):

- `HTTP_PROXY` (or lowercase `http_proxy`) applies to **http, https, and ftp**; `HTTPS_PROXY` is ignored. Measured: `HTTP_PROXY=http://127.0.0.1:9` broke an https repo fetch; `HTTPS_PROXY` alone had no effect (OBS-48).
- `NO_PROXY` is a comma/space-separated suffix list. Matching is by **suffix and requires the host to be longer than the suffix**: `NO_PROXY=repo-de.voidlinux.org` did **not** bypass, `NO_PROXY=voidlinux.org` did; `NO_PROXY='*'` bypasses all (OBS-48). Lowercase `no_proxy` is also read.
- Also honored: `HTTP_PROXY_AUTH`, `HTTP_AUTH`, `HTTP_REFERER`, `HTTP_USER_AGENT`, `SOCKS_PROXY`, `FORCE_IPV4`, `FORCE_IPV6`, `FTP_PASSIVE_MODE`, `FTP_LOGIN`, `FTP_PASSWORD`, `FTP_PROXY`, `CONNECTION_TIMEOUT` (ms; default 5 min; `-1` infinite), `SSL_NO_VERIFY_HOSTNAME`, `SSL_NO_VERIFY_PEER`, `SSL_CA_CERT_FILE`, `SSL_CA_CERT_PATH`, `SSL_CLIENT_CERT_FILE`, `SSL_CLIENT_KEY_FILE` (`man xbps-install(1) §ENVIRONMENT`).
- XBPS also honors `XBPS_ARCH`, `XBPS_TARGET_ARCH`, `XBPS_SYSLOG`, `XBPS_STAGING` (see §7.4).

### 7.4 The `XBPS_STAGING` inversion (upstream bug)

`--staging`, `staging=true` and `XBPS_STAGING` were added together in 0.60; `NEWS:134-136` says staging "allows xbps to use staged packages from remote repositories" and that `XBPS_STAGING` overrides the config option. The implementation of the environment override is inverted: `XBPS_STAGING=true` **clears** `XBPS_FLAG_USE_STAGE`, `XBPS_STAGING=false` sets it (`src:lib/initend.c:170-176`). The bug is still present on `master` (checked 2026-11-05). The config keyword itself is correct (`src:lib/conf.c:396-404`), and local repositories merge stage data unconditionally (`src:lib/repo.c:517-524`). Guidance: do not use `XBPS_STAGING`; use `--staging` or `staging=true`. Void's repodata currently has an empty `stage.plist`, so the flag is observationally a no-op on the host (OBS-49).

---

## 8. Version differences across xbps releases shipped on Void

Void's `void-packages` history for `srcpkgs/xbps/template`: 0.59.1 (2020-04-01), 0.59.2 (2023-09-18), 0.60 (2025-06-14), then 0.60.1–0.60.7 through 2026-02-09 (OBS-50). The current repository ships `xbps-0.60.7_1`.

| Feature / behaviour | First version | Evidence | Parity impact |
|---|---|---|---|
| zstd-compressed `<arch>-repodata` | 0.54 | handbook [Common Issues](https://docs.voidlinux.org/xbps/troubleshooting/common-issues.html) ("March 2020 … from gzip to zstd. If XBPS wasn't updated to version 0.54 …") | Any xbpretty repodata reader must handle zstd tar (or shell out); older xbps cannot read current repos. |
| `.sig2` package signatures (ASN.1 fix) | 0.59.2 (2023-08-08 commit, shipped 2023-09-18) | `git log -S'.sig2'`; 0.59.2 sources; `NEWS:149-150` | Predownloader must use `.sig2` on Void ≥ 0.59.2, `.sig` on 0.59.1/older. |
| Fixed six-field dry-run output | 0.60 (2025-06-14) | `NEWS:144`; `src:bin/xbps-install/transaction.c:117-131` vs `0.59.2` variable printer | Plan parsers may support 4–6 fields on 0.59.x but must not assume field positions when a size is zero; 0.60+ is unambiguous. |
| `--staging` flag, `staging=` config, merged stage.plist | 0.60 | `NEWS:130-136`; `man` | Flag/config only on 0.60+; env override buggy. |
| `-O -O` (remove uninstalled from cache) | 0.60 | `NEWS:114`; `src:bin/xbps-remove/clean-cache.c` vs 0.59.2 bool | On 0.59.2 the second `-O` is a no-op; a "deep clean" is impossible. |
| `--checks` selection for `xbps-pkgdb` | 0.60 | `NEWS:167`; commit 2020-07-10 | N/A on 0.59.2. |
| Held-dependency exit 19 instead of silent ignore | 0.60 | `NEWS:78-81`; `src:lib/transaction_pkg_deps.c:281-305` | On 0.59.x a held dependency could yield an inconsistent pkgdb rather than exit 19; hint/error handling differs. |
| `XBPS_SYSLOG` env | 0.60 | `NEWS:124-125` | Needed to test syslog behavior without editing config. |
| Improved checksum/lock error wording | 0.60.6 | `NEWS:15-21` | Hint fixtures captured on 0.60.7 may not match 0.59.2 wording (ADR-0010 already treats 0.59.1 as a separate corpus). |
| `--staging` fix in query, staging index handling | 0.60.4 | `NEWS:40-45` | 0.60.0–0.60.3 staging was unreliable. |
| `xbps-query --cat` works in pkgdb and repo mode | 0.60 | `NEWS:116` | On 0.59.2 `--cat` is repo-mode oriented. |

Practical floor for full parity as researched here: **xbps ≥ 0.60** (Void current since 2025-06-14). If xbpretty intends to support 0.59.2 installations (e.g. an older ISO before first update), the plan parser must accept 4–6 fields, `--staging` must be gated, `-O -O` must be gated, `.sig`/`.sig2` both handled, and held-dependency wording/exit may differ.

---

## 9. Hard limits, and what each downstream ticket must decide

1. **No history model.** pkgdb holds current state only (`state`, `install-date`, `repository`, `automatic-install`, `hold`, `repolock`); syslog holds mostly per-package success lines (`Installed \`x' successfully (rootdir: /).`) plus repo-sync errors, but no transaction IDs, no grouping, and no inverse records, and it may be disabled. #46 must define xbpretty's own store and accept that undo is best-effort via cache.
2. **No direct archive install / no cache replay.** The cache is not a repository; a temp repo must be built with `xbps-rindex` and installed with `-f -R`. One version per repo index means a single repo cannot serve both "current" and "undo" versions. Local-repo installs never enter the cache.
3. **The cache is not a vault and is actively pruned.** Coverage was 14.7% of installed packages; `-O` and `-O -O` delete old versions under different rules; `.part` files persist; no restore command. #46 must either snapshot archives itself or declare undo unavailable when the archive is gone.
4. **Downgrade labels differ between surfaces.** Machine plan: `reinstall`; human table: `downgrade`; footer: `installed`. #43/#46 must not key behaviour off the action word alone; compare versions.
5. **No machine output and no URL/print-uris.** Only the dry-run plan is field-stable; `-p` returns values without keys, humanises sizes, and omits absent properties; `-S` is text. Exact sizes require reading `index.plist`. #44 must define schemas; #47 must either read repodata or accept lossy sizes.
6. **Signing cannot be bypassed.** Remote repos must be signed and packages need `.sig2`; a new key needs an interactive prompt (even `-y`), imported only by `-S`; there is no `--allow-unauthenticated`. Predownloading is safe only if xbps will still verify; don't pre-verify and bypass.
7. **No pin/exclude/recommends/release semantics.** `ignorepkg` is dependency-only and not an upgrade exclude; hold is a per-package pkgdb bit that `-f` overrides and removal ignores; repolock pins to origin repo; no priorities, recommends/suggests, `--only-upgrade`, `--target-release`, `--fix-broken`, `--remove-essential`.
8. **`--download-only` is not "download these packages".** It assumes all deps are missing, includes the xbps self-update closure, prompts unless `-y`, is a verify-only no-op for local repos, and its footer counts planned not actual actions.
9. **Exit codes and streams are unreliable.** Sync failure exits 0; all-held update is silent 0; already-installed is stderr 0; declined prompt is 0; remove-not-installed is stdout 2; query-not-found is silent 2; no-repo show is 95 while no-repo search is 1; signature failure is 1 and missing `.sig2` is 16. There is no `EXIT STATUS` contract.
10. **No root check, but real privilege by file ownership.** Writable `-r` roots make full transactions possible unprivileged; system-wide operations fail at the pkgdb lock (`root directory is not writable`). Holds and config edits inherit this.
11. **No mirror selection in xbps.** `repository=` is static; xmirror is a separate package that rewrites `/etc/xbps.d`; xbps has no latency/throughput scoring. #48 must own list fetching/benchmarking and write config carefully (root, backups, sync, revert).
12. **`XBPS_STAGING` is inverted upstream.** Use the flag/config; don't rely on the env var.

### Ticket-specific hand-off

- **#43 (transaction commands/flags):** use `xbps-install`, `xbps-remove`, `xbps-remove -o`, `-D`, `-f`, `-U`, `-A`, `-y`, `-S`/`-M`; translate Nala's `-n`→dry-run only for preview, provide own assume-no; `purge`→`-f`; no `--exclude`/`--only-upgrade`/`--fix-broken`/`--allow-unauthenticated`; `safe-upgrade`/`full-upgrade` collapse to `-u`; `xbps-install -u` self-update can need a second run; hold rows are not upgrades.
- **#44 (discovery/machine output):** wrap `-l`, `-R -s`, `-S`/`-R -S`, `-p`, `-L`, `-H`, `-m`, `-O`, `-x`/`-X`, `-o`; synthesize upgradable from `-n -u` minus `hold`; no policy/priorities; no all-versions; note TTY truncation and humanised numbers; absence of a property is meaningful (false/unset).
- **#46 (history/undo):** no xbps history; pkgdb fields + syslog only; cache coverage is partial; undo = temp repo + `-f`, labelled `reinstall`; `-O`/`-O -O` destroy candidates; modified conffiles survive remove unless `-f`; `-f -f` overwrites conffiles on install.
- **#47 (predownload):** URL = `repo/pkgver.arch.xbps` (+ `.sig2`); hash exact via `-p filename-sha256`; size humanised via CLI, exact via plan/repodata; cache path `<cachedir>/pkgver.arch.xbps`; both files required; xbps downloads serially; predownload into cache is reused offline and still RSA-verified; local repos are hash-only; remote unsigned/missing sig fails (16/1); proxy is `HTTP_PROXY` (no HTTPS_PROXY; NO_PROXY suffix quirk); resume uses `.part`.
- **#48 (fetch/mirrors):** authoritative list `https://xmirror.voidlinux.org/raw/mirrors.lst` (TSV; 33 enabled, tier 1/2) generated from `void-linux/xmirror` `mirrors.yaml`; a mirror is a base URL plus the repository path (`/current`, `/current/musl`, `/current/aarch64`, subrepo suffixes); write `*-repository-*.conf` copies in `/etc/xbps.d` (same basenames mask `/usr/share/xbps.d`), then `xbps-install -S`; root needed; xmirror uses `.old` backups and a sed of `repo-default`; no xbps benchmark/selection; `-S` failure is exit 0; `-M` can validate without writing.

---

## 10. Gaps and things deliberately not tested

- No privileged system transaction was run; all write-path observations used `-r /tmp/...` roots. xbps honours `-r` for pkgdb/cache/config, so behaviour should match, but maintainer scripts that assume `/` were not exercised.
- No 0.59.x binary was available; 0.59.2 behaviour is from tagged source (`git show 0.59.2:…`), not execution.
- Staged-package behaviour was not exercised with non-empty stage data (Void's `stage.plist` is empty). The `XBPS_STAGING` inversion is source-verified but not observable here.
- File conflicts (`-I`), shared-library failures, out-of-space (`ENOSPC`), and lock contention were not reproduced; source/man citations only.
- `aarch64`/`musl`/`XBPS_TARGET_ARCH` were not tested; claims are from man pages and source.
- `--reproducible` was not runtime-tested.
- ftp:// repositories and TLS client-certificate options were not exercised.
- Cache statistics are host-specific (1,487 installed, 242 archives, 218 exact matches).
- Nala's history schema/replay and downloader design are covered by the separate Nala internals research, not this document.

---

## Appendix A. Observed evidence (commands and outputs)

All host commands were run 2026-11-05 on xbps 0.60.7; `LAB` commands used `/tmp/xbps-lab` with a writable `-r` root and an empty `-C` directory. Paths abbreviated.

**OBS-1 — piped install summary and footer (LAB):**
```
$ xbps-install -r LAB/root -C LAB/conf -R LAB/repo1 -y A
2 packages will be installed:

  B-1.0_1 
  A-1.0_1 

Size required on disk:           41B
Space available on disk:      2237MB

[*] Verifying package integrity
B-1.0_1: verifying SHA256 hash...
A-1.0_1: verifying SHA256 hash...
...
0 downloaded, 2 installed, 0 updated, 2 configured, 0 removed, 0 on hold.
```

**OBS-2 — host dry-run plan:**
```
$ xbps-install -n figlet
figlet-2.2.5_6 install x86_64 https://repo-de.voidlinux.org/current 672593 94372
$ xbps-install -n -D figlet      # 18 lines, e.g.
libxcrypt-compat-4.4.38_1 download x86_64 https://repo-de.voidlinux.org/current 206920 89590
...
xbps-0.60.7_1 install x86_64 https://repo-de.voidlinux.org/current 533481 121137
figlet-2.2.5_6 install x86_64 https://repo-de.voidlinux.org/current 672593 94372
```

**OBS-3 — show/installed vs repo:**
```
$ xbps-query -S xbps | head
architecture: x86_64
automatic-install: yes
changelog: https://raw.githubusercontent.com/void-linux/xbps/master/NEWS
filename-sha256: d1737372…
filename-size: 118KB
install-date: 2026-09-28 22:53 IST
...
repository: https://repo-default.voidlinux.org/current
state: installed
$ xbps-query -R -S xbps | grep -E '^(repository|pkgver)'
pkgver: xbps-0.60.7_1
repository: https://repo-de.voidlinux.org/current
```

**OBS-4 — search markers and regex:**
```
$ xbps-query -R -s figlet
[-] figlet-2.2.5_6           Program for making large letters out of ordinary text
[-] figlet-fonts-20150508_4  Additional fonts for figlet
[-] python3-pyfiglet-0.8.0_8 Implementation of figlet written in Python
$ xbps-query -R -s xbps        # installed matches are [*]
[*] libxbps-0.60.7_1          XBPS package system utilities - runtime library
...
$ xbps-query -R --regex -s '^figlet$'   # no output
$ xbps-query -R --regex -s '^figlet-'   # matches pkgvers
```

**OBS-5 — properties (`-p`) and humanised numbers:**
```
$ xbps-query -p pkgver,repository,hold,automatic-install,state xbps
xbps-0.60.7_1
https://repo-default.voidlinux.org/current
yes
installed
$ xbps-query -p installed_size,filename-size xbps
521KB
118KB
$ xbps-query -p hold xbps          # empty, exit 0
```

**OBS-6 — updates and holds (LAB):**
```
$ xbps-install -r LAB/root -C LAB/conf -n -u -R LAB/repo2
A-2.0_1 hold noarch /tmp/xbps-lab/repo2 24 689
$ xbps-install -r LAB/root -C LAB/conf -y -u -R LAB/repo2
(no output; exit 0; A remains 1.0_1)
$ xbps-install -r LAB/root -C LAB/conf -n -u -f -R LAB/repo2 A
A-2.0_1 update noarch /tmp/xbps-lab/repo2 24 689
```

**OBS-7 — root requirement:**
```
$ xbps-install -y figlet          # uid 1000
ERROR: failed to check whether the root directory is writable: /: Permission denied
exit 1
```

**OBS-8 — `-u` on a missing package installs it:**
```
$ xbps-install -n -u figlet
figlet-2.2.5_6 install x86_64 https://repo-de.voidlinux.org/current 672593 94372
exit 0
```

**OBS-9 — repository priority / bestmatching:**
```
$ xbps-query -R -i --repository=LAB/old --repository=LAB/new -p pkgver taema
taema-0.8.0_1
$ xbps-query -R -i --repository=LAB/new --repository=LAB/old -p pkgver taema
taema-1.0.0_1
$ xbps-query -R -i --repository=LAB/old --repository=LAB/new -p pkgver 'taema>=0.9'
taema-1.0.0_1
$ xbps-query -C LAB/conf-best -R -i --repository=LAB/old --repository=LAB/new -p pkgver taema
taema-1.0.0_1        # bestmatching=true
```

**OBS-10 — ignorepkg is dependency-only:**
```
$ cat LAB/conf-ignoreA/00-ignore.conf
ignorepkg=A
$ xbps-install -r LAB/root -C LAB/conf-ignoreA -n -u -R LAB/repo2
A-2.0_1 update noarch /tmp/xbps-lab/repo2 24 689
$ xbps-install -r LAB/root3 -C LAB/conf-ignoreA -R LAB/repoD -y D
D-1.0_1: installed successfully.
# installed: D only; the A>=0 dependency was treated as satisfied
```

**OBS-11 — cache is not a repo; undo via temp repo:**
```
$ xbps-install -n /var/cache/xbps/taema-0.8.0_1.x86_64.xbps
ERROR: Package '/var/cache/xbps/taema-0.8.0_1.x86_64.xbps' not found in repository pool.
exit 2
$ xbps-install -r LAB/root -C LAB/conf -n -f -R LAB/undo-repo A
A-1.0_1 reinstall noarch /tmp/xbps-lab/undo-repo 24 688
$ xbps-install -r LAB/root -C LAB/conf -y -f -R LAB/undo-repo A
1 package will be installed:
  A-1.0_1
...
[*] Verifying package integrity
A-1.0_1: verifying SHA256 hash...
...
0 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.
# A is now 1.0_1 (downgraded)
$ xbps-query -R -i --repository=LAB/both -p pkgver taema-0.8.0_1
(no output; exit 2)   # one version per index
$ xbps-rindex -f -a LAB/both/taema-0.8.0_1.x86_64.xbps
index: added `taema-0.8.0_1' (x86_64).
# now the repo serves 0.8.0
```

**OBS-12 — new key prompt with `-y`:**
```
$ printf 'y\n' | xbps-install -r LAB/root2 -C LAB/conf -R http://127.0.0.1:8765/LAB/repo-signed -S -y
[*] Updating repository `http://127.0.0.1:8765/xbps-lab/repo-signed/x86_64-repodata' ...
x86_64-repodata: [1139B 100%] …
`http://127.0.0.1:8765/xbps-lab/repo-signed' repository has been RSA signed by "Lab Test"
Fingerprint: 1a:b2:0c:d0:27:8e:fb:a7:1f:6e:7d:cc:8d:4b:32:65
# stderr: Do you want to import this public key? [Y/n]  (no newline)
```

**OBS-13 — hold in `-n` uses candidate version:**
```
$ xbps-pkgdb -r LAB/root -m hold A
$ xbps-query -r LAB/root -H
A-1.0_1
$ xbps-query -r LAB/root -p pkgver A
A-1.0_1
$ xbps-query -r LAB/root -p hold A
yes
$ xbps-install -r LAB/root -C LAB/conf -n -u -R LAB/repo2
A-2.0_1 hold noarch /tmp/xbps-lab/repo2 24 689
```

**OBS-14 / OBS-15 — downgrade labels:**
```
# machine plan:
A-1.0_1 reinstall noarch /tmp/xbps-lab/undo-repo 24 688
# human table (pty, stty cols 120):
Name Action    Version           New version            Download size
A    downgrade 2.0_1             1.0_1                  -
```

**OBS-16 — `-O` depends on configured repos:**
```
$ xbps-remove -r LAB/root2 -C LAB/conf-repo -O        # repo serving A/B configured
(no output; archives kept)
$ xbps-remove -r LAB/root2 -C LAB/conf -O             # empty config
Removed A-1.0_1.noarch.xbps from cachedir (obsolete)
Removed B-1.0_1.noarch.xbps from cachedir (obsolete)
```

**OBS-17 — host cache clean:**
```
$ xbps-remove -n -O | wc -l        # 22 obsolete archives
$ xbps-remove -n -O -O | wc -l     # 24 (adds whatsapp-linux, xdotool)
```

**OBS-18 — held dependency exits 19:**
```
$ xbps-pkgdb -r LAB/root -m hold C
$ xbps-install -r LAB/root -C LAB/conf -n -R LAB/repo3 A
# stderr:
MISSING: C>=2.0_1
ERROR: Transaction aborted due to unresolved dependencies.
# exit 19
```

**OBS-19 — removal ignores holds (actual, LAB):**
```
$ xbps-pkgdb -r LAB/root5 -m hold B
$ xbps-remove -r LAB/root5 -C LAB/conf -y A
$ xbps-remove -r LAB/root5 -C LAB/conf -n -o
B-1.0_1 remove noarch /tmp/xbps-lab/repo1 17 562
$ xbps-remove -r LAB/root5 -C LAB/conf -y -o
1 package will be removed:
  B-1.0_1
...
Removed `B-1.0_1' successfully.
0 downloaded, 0 installed, 0 updated, 0 configured, 1 removed, 0 on hold.
```

**OBS-20 — property search:**
```
$ xbps-query -R -s 'cmd:xbps-install' -p provides
xbps-0.60.7_1: cmd:xbps-install-0.60.7_1 (https://repo-de.voidlinux.org/current)
$ xbps-query -R -s 'noarch' -p architecture | head -2
mcc-0.2.3_1: noarch (https://git.bongbetic.com/…)
optionsplus-linux-0.1.2_1: noarch (https://github.com/…)
```

**OBS-21 — repolock:**
```
$ xbps-pkgdb -r LAB/root2 -m repolock A
$ xbps-query -r LAB/root2 --list-repolock-pkgs
A-1.0_1
$ xbps-query -r LAB/root2 -p repolock A
yes
$ xbps-install -r LAB/root2 -C LAB/conf -n -u -R LAB/repo2
(no output; A does not update)
```

**OBS-22 — ownedby full-path matching:**
```
$ xbps-query -o '/usr/bin/xbps*'
xbps-0.60.7_1: /usr/bin/xbps-install (regular file)
...
$ xbps-query -o xbps-install
(no output)
```

**OBS-23 — full dep tree is flat:**
```
$ xbps-query --fulldeptree -x xbps | head
libxbps-0.60.7_1
libarchive-3.8.9_1
libzstd-1.5.7_1
...
```

**OBS-24 — sync failures still exit 0:**
```
$ xbps-install -S            # uid 1000, several repos unwritable
[*] Updating repository `https://repo-de.voidlinux.org/current/x86_64-repodata' ...
ERROR: [reposync] failed to fetch file `https://github.com/…/x86_64-repodata': Permission denied
...
exit 0
$ HTTP_PROXY=http://127.0.0.1:9 xbps-install -S -n figlet
figlet-2.2.5_6 install x86_64 https://repo-de.voidlinux.org/current 672593 94372   # -S ignored with -n
```

**OBS-25 — `-f` vs `-f -f` on a modified conffile (LAB):**
```
$ xbps-install … -f A
Keeping modified configuration file `/etc/a.conf'.
# a.conf still contains the local change
$ xbps-install … -f -f A
# a.conf overwritten with the package version; no message
```

**OBS-26 — already installed:**
```
$ xbps-install -r LAB/root -C LAB/conf -n B
ERROR: Package `B' already installed.     # stderr, exit 0
$ xbps-install -r LAB/root -C LAB/conf -n -u B
Package 'B' is up to date.                # stderr, exit 0
```

**OBS-27 — remove keeps modified conffile, `-f` deletes it (LAB):**
```
$ echo 'local change' > LAB/root/etc/a.conf
$ xbps-remove … -y A
# /etc/a.conf remains
$ echo 'modified' > LAB/root/etc/a.conf
$ xbps-remove … -y -f A
# /etc/a.conf is gone; LAB/root/etc/a.conf.new-1.0_1 remains
```

**OBS-28 — revdep guard (host, dry-run):**
```
$ xbps-remove -n xbps
xbps-0.60.7_1 in transaction breaks installed pkg `base-system-0.114_2'
xbps-0.60.7_1 in transaction breaks installed pkg `xbpretty-0.3.2_1'
...
ERROR: Transaction aborted due to unresolved dependencies.
exit 19
$ xbps-remove -n -F xbps
xbps-0.60.7_1 remove x86_64 https://repo-default.voidlinux.org/current 533481 121137
exit 0
```

**OBS-29 — `-D` with a local repo is a verify-only no-op:**
```
$ xbps-install -D -r LAB/root -C LAB/conf -R LAB/repo1 -y A
1 package will be installed:
  A-1.0_1
[*] Verifying package integrity
B-1.0_1: verifying SHA256 hash...
A-1.0_1: verifying SHA256 hash...
0 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.
# `xbps-query -l` still shows only C-1.0_1; no cache files
```

**OBS-30 — `-D` from a signed remote repo (success and failure):**
```
# success (key already imported):
$ xbps-install -M -D -r LAB/root2 -C LAB/conf -R http://127.0.0.1:8765/LAB/repo-signed -y A
2 packages will be downloaded:
  B-1.0_1
  A-1.0_1
1 package will be installed:
  A-1.0_1
...
[*] Downloading packages
B-1.0_1.noarch.xbps.sig2: [256B 100%] ...
B-1.0_1.noarch.xbps: [562B 100%] ...
B-1.0_1: verifying RSA signature...
...
2 downloaded, 1 installed, 0 updated, 1 configured, 0 removed, 0 on hold.
# nothing installed; cache has both .xbps + .sig2
# missing .sig2 in an unsigned remote repo:
ERROR: [trans] failed to download `B-1.0_1' signature from `http://…': Not Found
ERROR: Transaction failed! see above for errors.
exit 16
# signed repo but key not imported:
ERROR: B-1.0_1: the RSA signature is not valid!
ERROR: B-1.0_1: removed pkg archive and its signature.
exit 1
```

**OBS-31 — `-O` never touches repository indexes:**
```
$ ls /var/db/xbps | wc -l
27            # 23 escaped repo dirs + keys, triggers, lock, pkgdb-0.38.plist
$ ls /var/db/xbps/https___repo-de_voidlinux_org_current/
x86_64-repodata
# source: clean_cachedir() only chdir()s to xhp->cachedir and collects *.xbps
# (src:bin/xbps-remove/clean-cache.c:139-185), so /var/db/xbps data is untouched.
# Handbook remediation for stale lists: rm /var/db/xbps/http*/*-repodata && xbps-install -S.
```

**OBS-32 — syslog history:**
```
$ tail -3 /var/log/socklog/xbps/current
2026-11-05T04:00:30.51729 user.notice: Nov  5 09:30:30 xbps-install: Updated `gtk4-layer-shell-devel-1.3.0_2' successfully (rootdir: /).
...
$ grep -c 'Installed `' /var/log/socklog/xbps/current
196
$ grep -c 'Updated `' /var/log/socklog/xbps/current
125
# 464 lines total; no transaction id or grouping
```

**OBS-33 — mirror list:**
```
$ curl https://xmirror.voidlinux.org/raw/mirrors.lst | head -2
EU	https://repo-fi.voidlinux.org/	Helsinki, Finland	1
EU	https://repo-de.voidlinux.org/	Frankfurt, Germany	1
# 33 enabled entries: 4 tier 1, 29 tier 2
```

**OBS-34 — `-U` leaves `uu` but footer counts configured:**
```
$ xbps-install … -y -U A
...
0 downloaded, 2 installed, 0 updated, 2 configured, 0 removed, 0 on hold.
$ xbps-query -r LAB/root -l
uu A-1.0_1 lab A
uu B-1.0_1 lab B
```

**OBS-35 — non-existent options:**
```
$ xbps-install --ignore-repo-errors -n figlet
xbps-install: unrecognized option '--ignore-repo-errors'
exit 1
$ xbps-install -O -n figlet
xbps-install: invalid option -- 'O'
exit 1
```

**OBS-36 — remove missing package on stdout, exit 2:**
```
$ xbps-remove -n figlet
Package `figlet' is not currently installed.     # stdout
exit 2
```

**OBS-37 — pkgdb checks missing package returns 0:**
```
$ xbps-pkgdb -r LAB/root --checks dependencies no-such-pkg
Package no-such-pkg is not installed.            # stdout
exit 0
```

**OBS-38 — reconfigure has no dry-run:**
```
$ xbps-reconfigure -r LAB/root -n A
xbps-reconfigure: invalid option -- 'n'
exit 1
```

**OBS-39 — repodata structure:**
```
$ tar --zstd -tf /var/db/xbps/https___repo-de_voidlinux_org_current/x86_64-repodata
index.plist
index-meta.plist
stage.plist
```
Sizes and `index-meta` fields as quoted in §4.1.

**OBS-40 — noarch package in a target-arch repo:**
```
$ xbps-query -R -i --repository=LAB/noarch -p pkgver,architecture,filename-size taema-0.6.0_1
taema-0.6.0_1
noarch
728KB
```

**OBS-41 — HEAD checks for URL layout** (quoted sizes in §4.2; `content-length` 94372 / 512 / 2219849).

**OBS-42 — same-basename config override:**
```
$ xbps-query -C LAB/conf-same -L | head -4
    1 /tmp/xbps-lab/repo-old (RSA unsigned)
      21 https://repo-default.voidlinux.org/current/multilib/nonfree (RSA signed)
...
# /usr/share/xbps.d/00-repository-main.conf was not parsed; the config-dir file masked it
```

**OBS-43 — signing a local repo:**
```
$ xbps-rindex --privkey priv.pem --sign --signedby "Lab Test" repo-signed
Initialized signed repository (2 packages)
$ xbps-rindex --privkey priv.pem --sign-pkg repo-signed/*.xbps
signed successfully repo-signed/A-1.0_1.noarch.xbps
...
```

**OBS-44 — missing `.sig2` with cached `.xbps` re-fetches only the signature:**
```
$ rm LAB/root2/var/cache/xbps/B-1.0_1.noarch.xbps.sig2
$ xbps-install -M -D -r LAB/root2 -C LAB/conf -R http://… -y A
B-1.0_1.noarch.xbps.sig2: [256B 100%] ...
B-1.0_1: verifying RSA signature...
...
```

**OBS-45 — cache cleaner ignores `.part` and stray `.sig2`:**
```
$ touch LAB/root2/var/cache/xbps/foo.xbps.part LAB/root2/var/cache/xbps/bar.xbps.sig2
$ xbps-remove -r LAB/root2 -C LAB/conf -O
$ ls LAB/root2/var/cache/xbps
bar.xbps.sig2  foo.xbps.part
```

**OBS-46 — prompt on stderr, abort exits 0:**
```
$ printf 'n\n' | xbps-install -r LAB/root5 -C LAB/conf -R LAB/repo1 A
# stdout:
2 packages will be installed:
...
Aborting!
# stderr:
Do you want to continue? [Y/n]
# exit 0; nothing installed
```

**OBS-47 — non-root system transaction:**
```
$ xbps-install -y figlet
ERROR: failed to check whether the root directory is writable: /: Permission denied
exit 1
```

**OBS-48 — proxy behaviour:**
```
$ HTTP_PROXY=http://127.0.0.1:9 xbps-query -M -R --repository=https://repo-de.voidlinux.org/current -p pkgver figlet
ERROR: failed to open repository: https://repo-de.voidlinux.org/current: Input/output error
exit 2
$ HTTPS_PROXY=http://127.0.0.1:9 …                         # succeeds
$ HTTP_PROXY=… NO_PROXY=repo-de.voidlinux.org …            # fails
$ HTTP_PROXY=… NO_PROXY=voidlinux.org …                    # succeeds
$ HTTP_PROXY=… NO_PROXY='*' …                              # succeeds
```

**OBS-49 — `--staging` no-op on Void:**
```
$ diff <(xbps-install -n figlet 2>&1) <(xbps-install -n --staging figlet 2>&1)
(no difference)
# stage.plist in repo-de repodata is 0 bytes
```

**OBS-50 — Void xbps version history** (void-packages commits for `srcpkgs/xbps/template`):
```
2026-02-09 xbps: update to 0.60.7
2025-11-16 xbps: update to 0.60.6
2025-08-02 xbps: update to 0.60.5
2025-07-24 xbps: update to 0.60.4
2025-06-17 xbps: update to 0.60.3
2025-06-15 xbps: update to 0.60.2
2025-06-14 xbps: update to 0.60.1
2025-06-14 xbps: update to 0.60
2023-09-18 xbps: update to 0.59.2
2020-04-01 xbps: update to 0.59.1
```

---

## 11. Sources

**Void handbook (primary):**
- [XBPS Package Manager](https://docs.voidlinux.org/xbps/index.html) — command roles, `-Su` twice, `-Rs`, `-f`, `-Ro`.
- [Repositories](https://docs.voidlinux.org/xbps/repositories/index.html) — repodata, local/remote, signing requirement, repo layout.
- [Mirrors](https://docs.voidlinux.org/xbps/repositories/mirrors/index.html) — default mirror, xmirror.voidlinux.org.
- [Changing Mirrors](https://docs.voidlinux.org/xbps/repositories/mirrors/changing.html) — copy `*-repository-*.conf`, sed, `-S`, `-L`.
- [Signing Repositories](https://docs.voidlinux.org/xbps/repositories/signing.html) — RSA PEM key, `xbps-rindex --sign`/`--sign-pkg`.
- [Common Issues](https://docs.voidlinux.org/xbps/troubleshooting/common-issues.html) — key prompt text, zstd repodata history, `rm …/*-repodata`.

**Man pages (installed 0.60.7; rendered with `mandoc -Tascii`):**
- `xbps-install(1)`, `xbps-remove(1)`, `xbps-query(1)`, `xbps-pkgdb(1)`, `xbps-reconfigure(1)`, `xbps-rindex(1)`, `xbps.d(5)`. Mirrored at <https://man.voidlinux.org/>.

**xbps source (tag 0.60.7):**
- <https://github.com/void-linux/xbps/tree/0.60.7> — cited inline as `src:PATH:LINE`.
- `NEWS` lines cited: 15-21, 27-29, 40-45, 78-81, 114-116, 124-125, 130-136, 144, 149-150, 167, 285, 403.
- 0.59.2 comparisons via `git show 0.59.2:…` from the same clone (tag `0.59.2`).

**Void packaging history:**
- `void-linux/void-packages` commits touching `srcpkgs/xbps/template`: <https://github.com/void-linux/void-packages/commits/master/srcpkgs/xbps/template> (dates/versions in OBS-50).

**Mirror tooling:**
- `void-linux/xmirror`: [`mirrors.yaml`](https://github.com/void-linux/xmirror/blob/master/mirrors.yaml), [`Makefile`](https://github.com/void-linux/xmirror/blob/master/Makefile) (`MIRRORURL ?= https://xmirror.voidlinux.org/raw/mirrors.lst`), [`xmirror.in`](https://github.com/void-linux/xmirror/blob/master/xmirror.in), [`xmirror.1`](https://github.com/void-linux/xmirror/blob/master/xmirror.1).

**Nala v0.18.2:**
- [`src/cli/commands.rs`](https://github.com/volitank/nala/blob/v0.18.2/src/cli/commands.rs), [`src/cli/flags.rs`](https://github.com/volitank/nala/blob/v0.18.2/src/cli/flags.rs), [`src/cli/parser.rs`](https://github.com/volitank/nala/blob/v0.18.2/src/cli/parser.rs), [`nala.conf`](https://github.com/volitank/nala/blob/v0.18.2/nala.conf).

**In-repo context:**
- [`docs/research/xbps-progress-telemetry.md`](xbps-progress-telemetry.md) — stream routing, PTY vs pipe, exact progress regexes.
- [`docs/adr/0002-measured-xbps-behaviour.md`](../adr/0002-measured-xbps-behaviour.md) — stale-index exits, unprivileged `-Sn`, `ignorepkg`, lock waiting.
- [`docs/adr/0006-hints-match-where-xbps-writes-them.md`](../adr/0006-hints-match-where-xbps-writes-them.md), [`docs/adr/0010-the-0607-hint-fixtures-come-from-the-host.md`](../adr/0010-the-0607-hint-fixtures-come-from-the-host.md) — stream/exit-code fixture provenance.
- [`docs/adr/0007-held-packages-appear-under-hold-only.md`](../adr/0007-held-packages-appear-under-hold-only.md) — summary treatment of hold rows (consistent with OBS-13).

**Context7 lookups used (as requested):**
- `https://context7.com/api/v1/void-linux/void-docs?type=txt&topic=xbps`
- `https://context7.com/api/v1/websites/voidlinux?type=txt&topic=xbps`

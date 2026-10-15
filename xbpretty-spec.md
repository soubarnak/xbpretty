# xbpretty — Lite Spec (rev 4)

**Status:** Approved, 2026-10-15 (supersedes rev 3)

## 1. Purpose
A Go wrapper around the xbps CLIs for Void Linux that adds a colored transaction summary and a confirmation prompt, in the style of nala. Project: Bongbetic.

## 2. Scope
**In scope**
- Wrapping `install`, `remove`, `upgrade`, `autoremove`, `update` over `xbps-install` / `xbps-remove`
- Dry-run preview, colored summary with sizes and a counts footer, `[Y/n]` prompt
- Live streaming of xbps output during real execution, with signal forwarding
- Transaction log and post-run reboot hint
- User config file for default flags (sudo/doas-aware)
- argv[0] aliases (`xbp`, `xbp-install`, etc.) and a `link` helper
- Flag passthrough to xbps, verbose mode, shell completions, `--version`
- xbps-src template for native packaging

**Out of scope (v1)**
- Parallel downloads
- Undo / redo (the log in FR-013 records transactions but cannot replay them)
- Mirror speed-test and selection
- Formatted `search` / `show`
- libxbps bindings (CLI wrapping only)
- `/etc` config layer
- Auto-retry or auto-elevation

## 3. Functional Requirements

### 3.1 Dispatch and commands

| ID | Pri | Requirement |
|---|---|---|
| FR-001 | M | Ship one binary `xbpretty`. Dispatch on argv[0]: `xbpretty <cmd>`, `xbp <cmd>`, and symlinks `xbp-install`, `xbp-remove`, `xbp-upgrade`, `xbp-autoremove`, `xbp-update` map to the same commands. |
| FR-002 | M | `install <pkgs...>` wraps `xbps-install -S <pkgs...>` (index sync on by default, since xbps refuses new packages against a stale index). `--no-sync` drops `-S`. |
| FR-003 | M | `remove <pkgs...>` wraps `xbps-remove <pkgs...>`. |
| FR-004 | M | `upgrade` wraps `xbps-install -Su`. `--no-sync` drops `-S`. |
| FR-005 | M | `autoremove` wraps `xbps-remove -o`. |
| FR-006 | M | `update` wraps `xbps-install -S` (index sync only, no packages). Requires root. No summary or prompt; streams xbps output per FR-014. |
| FR-007 | M | Index sync rule for dry-runs: as root, the dry-run includes `-S` where the command uses it. When unprivileged, `--dry-run` omits `-S` and prints a one-line warning that the repo index may be stale. |
| FR-008 | M | Second-run sync rule: when the dry-run already synced the index (root), the real run omits `-S`, so the index is not fetched twice and the executed transaction matches the previewed one. |

### 3.2 Transaction flow

| ID | Pri | Requirement |
|---|---|---|
| FR-009 | M | Default mode: run the xbps command with `-n`, print the summary (FR-011/FR-012), then apply FR-016. As root, prompt `[Y/n]` on stderr: empty input, `y` or `Y` proceeds; `n` or `N` aborts with exit 0; any other input re-prompts. On proceed, run the real command with xbps's `-y` appended, so xbps does not prompt a second time. |
| FR-010 | M | Non-interactive rule: if stdin is not a TTY and neither `-y` nor `--dry-run` is given, print the summary, then exit 1 with a hint to pass `-y`. Never block waiting on a pipe. |
| FR-011 | M | Summary groups packages into Install / Upgrade / Remove, plus a Hold group when the dry-run reports held packages. Sources: <br>• Install/upgrade/hold rows: parse the `-n` output columns (pkgver, action, arch, repository, installedsize, downloadsize). <br>• Old version and old installed size for upgrades: `xbps-query -p pkgver,installed_size <pkg>`, batched. <br>• Remove rows, including `autoremove`: `xbps-remove -n` for the package list, plus `xbps-query -p installed_size <pkg>` for sizes. <br>Upgrade rows show `old → new`. `configure` actions are counted but not listed. |
| FR-012 | S | Render sizes human-readable (B/KiB/MiB/GiB) in aligned columns. End the summary with a footer, e.g. `3 install, 12 upgrade, 1 remove, 2 hold · 48.2 MiB download · +120.4 MiB disk`. The disk figure is the net delta (new installed size minus old installed size minus removed size). |
| FR-013 | S | After every real execution that exits 0, append one line to `$XDG_STATE_HOME/xbpretty/transactions.log` (fallback `<home>/.local/state/...`, with `<home>` resolved per FR-019): ISO-8601 timestamp, command, and the package list with versions. Create the directory on first use. Log failures are warnings, not errors. |
| FR-014 | M | During real execution, stream xbps stdout and stderr live, unbuffered, so xbps's own progress output remains visible. Forward SIGINT and SIGTERM to the xbps child process, and exit with the child's exit code. |
| FR-015 | M | If the dry-run yields no packages, print `Nothing to do.`, skip the prompt, and exit 0. |
| FR-015a | C | After a successful `install` or `upgrade` that changed any `linux*` kernel package or `glibc`, print a one-line reboot hint. |

### 3.3 Errors and privileges

| ID | Pri | Requirement |
|---|---|---|
| FR-016 | M | Real execution requires euid 0. When not root and not `--dry-run`, print the summary, then a hint to rerun under `doas` if `/usr/bin/doas` exists, else `sudo`, and exit 1 **without prompting**. |
| FR-017 | M | On xbps failure, print the raw xbps stderr and exit with xbps's exit code. No retry. <br><br>Append a one-line hint for these known cases: <br>• Stale repo index → `xbp update` <br>• Package not found <br>• Lock held by another xbps process <br>• xbps must be updated first → `xbps-install -u xbps` <br>• Package ignored via `ignorepkg` in xbps config <br><br>Hint matching rules: <br>• Every xbps and xbps-query call runs with `LC_ALL=C`. <br>• A hint fires only when **both** the exit code and a stderr substring pattern match. <br>• All patterns live in one table (`hints.go`) and are short substrings, never full lines. <br>• An unmatched failure gets no hint. Never show a guessed one. |
| FR-018 | S | After any failed `remove`, run `xbps-query -X` on each target package. Do not match the error string. If any dependents are returned, list them and hint that `xbp remove -- -F <pkg>` forces removal and will break those dependents. |
| FR-018a | S | Fail with a clear message and exit 1 if the configured xbps binaries are not found. |

### 3.4 Configuration and output

| ID | Pri | Requirement |
|---|---|---|
| FR-019 | M | Read config from `$XDG_CONFIG_HOME/xbpretty/config.toml`, falling back to `<home>/.config/xbpretty/config.toml`. When euid is 0 and `SUDO_USER` or `DOAS_USER` is set, `<home>` is that user's home directory, not `/root`. Keys: `assume_yes`, `color`, `verbose`, `sync` (default true), `log` (default true), `xbps_install_path`, `xbps_remove_path`, `xbps_query_path`. Precedence: CLI flags > config > built-in defaults. A missing file is not an error. A malformed file exits 2 with the parse error and line. |
| FR-020 | M | Disable color when stdout is not a TTY or `NO_COLOR` is set. The config `color` key and a `--color=auto\|always\|never` flag override this. |
| FR-021 | M | Flags: `-n/--dry-run`, `-y/--yes`, `-v/--verbose`, `--no-sync`, `--no-log`, `--color`. `-n` mirrors xbps's own dry-run flag. |
| FR-022 | S | Arguments after `--` pass through verbatim to the underlying xbps command, in both the dry-run and the real run (e.g. `xbp install -- -f pkg`). |
| FR-023 | S | `-v/--verbose` prints each exact xbps or xbps-query command line, on stderr, before running it. |

### 3.5 Tooling

| ID | Pri | Requirement |
|---|---|---|
| FR-024 | S | `xbpretty completion bash\|zsh\|fish` prints a completion script. Completes subcommands and flags, installed package names for `remove` (`xbps-query -l`), and repo package names for `install` (`xbps-query -Rs`). |
| FR-025 | C | `xbpretty link [dir]` creates the `xbp` and `xbp-*` symlinks in `dir` (default: the binary's own directory). Existing correct links are left alone; conflicting files are reported, not overwritten. |
| FR-026 | C | `--version` prints the xbpretty version and the detected xbps version. |
| FR-027 | C | Repository includes an xbps-src template that builds the binary and installs the symlinks and completions. |

## 4. Non-Functional Requirements

| ID | Pri | Requirement |
|---|---|---|
| NFR-001 | M | Single static binary (`CGO_ENABLED=0`). No runtime dependencies other than xbps. |
| NFR-002 | S | Wrapper overhead on top of the xbps dry-run and the xbps-query lookups stays under 100 ms for transactions of up to 200 packages. xbps-query calls are batched: at most one call per property set per transaction. |
| NFR-003 | M | Exit codes: <br>• xbps failures pass through. <br>• Usage or config errors exit 2. <br>• Privilege, non-interactive, or missing-binary errors exit 1. <br>• A user abort or nothing to do exits 0. |
| NFR-004 | M | Fixture tests cover the parser and the hint table, using output captured from at least two xbps versions. <br>• Parser fixtures: captured `-n` output, including hold and configure actions. <br>• Hint fixtures: stderr and exit code for each FR-017 case, reproduced in a `voidlinux/voidlinux` container. <br>The fixtures define the `hints.go` patterns, and a test fails if a captured case stops matching. |
| NFR-005 | S | All interactive text (prompt, hints, warnings, verbose lines) goes to stderr. Only the summary goes to stdout, so `xbp upgrade -n | grep` works. |

## 5. Acceptance Criteria

- **AC-001 (FR-009, FR-016):** Given a non-root user, when they run `xbp install foo` with no flags, then the summary prints, the `doas`/`sudo` hint follows, no prompt appears, and the exit code is 1.
- **AC-002 (FR-009, FR-008):** Given root, when they run `xbp install foo` and press Enter, then the dry-run is `xbps-install -Sn foo`, the real run is `xbps-install -y foo` (no second `-S`), and its exit code is returned.
- **AC-003 (FR-009):** Given root at the prompt, when they enter `n`, then nothing executes and the exit code is 0. When they enter `x`, the prompt repeats.
- **AC-004 (FR-010):** Given root with stdin piped from `/dev/null`, when they run `xbp upgrade`, then the summary prints, no prompt appears, the `-y` hint shows, and the exit code is 1.
- **AC-005 (FR-021):** Given `-n` or `--dry-run`, when the command runs as root or non-root, then only the summary prints and no non-dry xbps command executes.
- **AC-006 (FR-009):** Given `-y` and root, when the command runs, then no prompt appears and the real command executes after the summary.
- **AC-007 (FR-007):** Given a non-root user, when they run `xbp upgrade -n`, then `xbps-install -un` is used and the stale-index warning prints on stderr.
- **AC-008 (FR-006):** Given root, when they run `xbp update`, then `xbps-install -S` runs with output streamed, and no summary or prompt appears.
- **AC-009 (FR-005):** Given orphaned packages exist, when the user runs `xbp autoremove -n`, then they appear in the Remove group with sizes.
- **AC-010 (FR-011, FR-012):** Given a transaction with installs, upgrades, removals and a held package, when the summary prints, then four labeled groups appear, upgrades show `old → new`, sizes are human-readable, and the footer shows the counts, download total and net disk delta.
- **AC-011 (FR-015):** Given a fully up-to-date system, when the user runs `xbp upgrade`, then `Nothing to do.` prints, no prompt appears, and the exit code is 0.
- **AC-012 (FR-013):** Given a successful `xbp install foo` run via `sudo` by user `alice`, when it finishes, then one timestamped line naming `foo` with its version is appended to `/home/alice/.local/state/xbpretty/transactions.log`.
- **AC-013 (FR-015a):** Given an upgrade that updates `linux6.12`, when it completes successfully, then the reboot hint prints.
- **AC-014 (FR-014):** Given a real execution in progress, when the user presses Ctrl-C, then the xbps child receives SIGINT and xbpretty exits with the child's exit code.
- **AC-015 (FR-017):** Given a nonexistent package, when xbps fails, then the raw stderr shows, a hint line follows, and the exit code equals xbps's.
- **AC-016 (FR-017):** Given an xbps failure whose exit code and stderr match no `hints.go` entry, when it fails, then the raw stderr prints with no hint line.
- **AC-017 (FR-018):** Given package `foo` with reverse dependencies, when `xbp remove foo` fails, then `xbps-query -X foo` is run, the dependents are listed, and the `-- -F` hint with its breakage warning is shown.
- **AC-018 (FR-018a):** Given `xbps_install_path` points to a missing file, when any command runs, then a clear error prints and the exit code is 1.
- **AC-019 (FR-019):** Given user `alice` has `assume_yes = true` in her config, when she runs `sudo xbp install foo`, then her config is read and no prompt appears.
- **AC-020 (FR-019):** Given a malformed config file, when any command runs, then the parse error and line number print and the exit code is 2.
- **AC-021 (FR-020, NFR-005):** Given `xbp upgrade -n | cat`, when it runs, then stdout holds only the summary with no ANSI escapes, and any warning appears on stderr.
- **AC-022 (FR-001):** Given a symlink named `xbp-remove`, when it is invoked with `foo`, then the behavior equals `xbpretty remove foo`.
- **AC-023 (FR-022):** Given `xbp install -- -f foo`, when it runs, then both the dry-run and the real xbps invocations include `-f`.
- **AC-024 (FR-023):** Given `-v`, when any command runs, then every xbps and xbps-query command line is printed on stderr before it executes.
- **AC-025 (FR-024):** Given bash completion is loaded, when the user types `xbp remove <TAB>`, then installed package names are offered.
- **AC-026 (FR-025):** Given an empty directory, when the user runs `xbpretty link <dir>`, then all six symlinks are created. On a rerun, nothing changes and nothing errors.

## 6. Constraints / Dependencies
- Void Linux with xbps installed. Uses `xbps-install`, `xbps-remove` and `xbps-query`.
- Go toolchain for building (current stable).
- Config format: TOML.
- Parsing depends on xbps's `-n` output format. Fixtures are required (NFR-004).

## 7. Risks
- The xbps `-n` output format may change between versions. Mitigated by NFR-004 and by failing loudly on unparseable lines rather than guessing.
- FR-017 hints depend on xbps error wording. Mitigated by the exit-code-plus-pattern rule, by suppressing the hint when nothing matches, and by the NFR-004 hint fixtures.
- Dry-run and real run can still diverge if a repo changes between them with `--no-sync`. Accepted; FR-008 removes the common cause.

## 8. Open Questions
None.

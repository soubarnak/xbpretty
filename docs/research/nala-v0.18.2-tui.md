# Research: Nala v0.18.2 Full-Screen TUI — Architecture, Screen Maps and a Zero-Dependency Go Equivalent

- **Ticket**: [#42](https://github.com/soubarnak/xbpretty/issues/42) — "Research: nala v0.18.2 full-screen TUI architecture"
- **Parent map**: [#37 (Map: Nala v0.18.2 parity for xbpretty)](https://github.com/soubarnak/xbpretty/issues/37)
- **Downstream consumers**:
  - [#50 (Grilling: full-screen TUI — tabs, interaction and mode switching)](https://github.com/soubarnak/xbpretty/issues/50)
  - [#52 (Prototype: zero-dependency full-screen TUI shell for xbps)](https://github.com/soubarnak/xbpretty/issues/52)
- **Author**: Soubarna (agentic research)
- **Date**: 2026-11-05
- **Status**: Complete — research only; no xbpretty code or spec changed
- **Parity reference**: nala **v0.18.2**, tag commit `2b7ff0554ca88e761fa6eaf8604e19c5e3033815` ("Prepare Nala 0.18.2 Debian release", 2026-10-22)
  - <https://github.com/volitank/nala/tree/v0.18.2>
  - Raw file pattern: `https://raw.githubusercontent.com/volitank/nala/v0.18.2/<path>`
- **Branch**: `research/nala-tui` (throwaway; never merged)

---

## 0. Method, and how to read the citations

**Primary source only.** Every behavioural claim below comes from the tagged Rust source at v0.18.2, from the repo's own assertion tests, or from its end-to-end pty/tmux tests. The archived Python implementation (`legacy/python`) was *not* used — it is not the parity reference, and at least one rendering fact differs between them (§6.2).

**Citation convention.** All paths are relative to the tag. `src/tui/summary/mod.rs:414-417` means lines 414–417 of that file at v0.18.2, i.e. <https://github.com/volitank/nala/blob/v0.18.2/src/tui/summary/mod.rs#L414-L417>.

**What was and was not executed.** The crate was **not built or run** here: `rust-apt 0.11.3` (Cargo.toml:17) needs `libapt-pkg-dev`, which is absent from this host. Therefore there are no captured screenshots in this document. Instead:

- every screen map is derived from the actual Ratatui constraint trees in the tagged code, and
- the row geometry is cross-checked against the repository's own buffer-assertion tests, which pin exact row indices and column starts (`src/progress.rs:587-656`, `:658-664`, `:696-719`, `:721-732`, `:734-760`), and
- interaction/session facts are cross-checked against the tmux and pty end-to-end tests (`tests/deb/term/run`, `tests/deb/release-info/run`, `tests/deb/release-info/interact.py`).

Where a statement is an inference rather than a direct read, it is marked **(inferred)**.

---

## 1. Executive summary

**The single most important finding for #50/#52: nala v0.18.2 has no unified four-tab application shell.** The word "tabs" applies to exactly one screen — the transaction summary — where the tabs are *operations* (Remove, AutoRemove, Install, …), not features. The four surfaces named in the map (#37 preference 6: "summary / progress / fetch tabs") are four **independent screen modes with four different lifecycles and three different gates**:

| Surface | Terminal mode | Has tabs? | Gate that enables it | Entered from |
|---|---|---|---|---|
| Summary | full-screen **alternate screen**, raw mode, mouse capture | **yes** — one tab per present `Operation` | `use_fullscreen_ui` = `UiMode::Tui` + tty + `TERM≠dumb` | `nala install/remove/upgrade/...`, `nala history <id>` |
| Progress | **inline viewport** pinned to the bottom, raw mode, **no alternate screen** | no | `use_enhanced_ui` = mode ≠ `Plain` + tty + `TERM≠dumb` | every download/update/score/install operation |
| Fetch (mirror select) | full-screen **alternate screen**, raw mode, **no mouse** | no | `use_enhanced_ui` **and** no `--auto` | `nala fetch` |
| Release info | full-screen **alternate screen**, raw mode, **no mouse** | no | `use_enhanced_ui` + stdin *and* stdout tty | `nala update` when APT reports Release-file changes |

The `--tui` flag is documented as "Enable optional full-screen interfaces" (`src/cli/parser.rs:28-30`) and the gate behind it excludes `Auto` (`src/terminal.rs:42-44`), while the default `Ui.mode = "Auto"` deliberately withholds those optional views (`nala.conf:22-25`: `"Auto" (default) uses inline progress and required safety prompts`; `README.md:117-121`). **Out of the box, nala never shows the tabbed summary.** It does show the inline progress box, and it *does* show full-screen `fetch` and full-screen release-info, because those two are treated as *required* interactive screens and use the weaker `use_enhanced_ui` gate (`src/cmd/fetch/mod.rs:86-94`, `src/cmd/update.rs:55-59`) — the `nala.conf:23` comment calls Auto "inline progress and required safety prompts". The *optional* full-screen views that Auto withholds are exactly summary and history.

Other headline findings:

1. **Progress is not a tab and not a log pane.** It is a fixed-height box anchored at the bottom of the *normal* screen (`Viewport::Inline`, `src/terminal.rs:116-123`); completed messages are inserted **above** the box into ordinary scrollback (`src/tui/progress.rs:70-85`). There is no scrollable log pane, no per-package table, and no keybindings.
2. **Progress has four views**, not one: Update (7 rows), Install (6), MirrorScore (6), Download (7 + mirror-table rows, max 12) — `src/progress.rs:62-85`.
3. **Summary requires explicit opt-in.** `use_fullscreen_ui` demands `UiMode::Tui` (`src/terminal.rs:42-44`); `Auto` is the default (`src/config/file.rs:13-16`). `--tui` forces it, `--no-tui` demotes `Tui` back to `Auto` (`src/config/configuration.rs:190-203`). `-y`, `-n` and `--simple` skip it entirely (`src/summary.rs:21-33`).
4. **Only the summary screen uses mouse** (scroll wheel up/down; `src/tui/summary/mod.rs:363-367`). Fetch and release-info do not enable mouse capture.
5. **There is no minimum-size handling and no "terminal too small" state anywhere** (a search of `src/` for minimum/too-small/min-width/min-height logic returns nothing). Layout degrades by clipping; the footer help of the history variant is clipped by construction (§6.7).
6. **Cancellation is an error path.** `q`/`Esc` in the summary returns `Ok(false)`, which becomes `bail!("User refused confirmation")`, exit code 1 (`src/summary.rs:316-318`, `src/util/prompt.rs:9-15`). The term e2e test asserts exactly this: press `q` at "Nala Upgrade", expect status 1 and no package/history change (`tests/deb/term/run:58-70`).
7. **Release info is 100 % APT-specific**: it renders `rust_apt::progress::ReleaseInfoChanges` (`src/tui/release_info.rs:6`, `:71-103`) and exists only because APT refuses to update when a repository's `Release` metadata changes without `Acquire::AllowReleaseInfoChange=true` (`tests/deb/release-info/run:74-84`). xbps has no comparable confirmation step.
8. **A zero-dependency Go equivalent is feasible but is a rendering engine, not a library binding.** Nala's terminal layer is crossterm 0.29 + ratatui 0.30.2 (`Cargo.toml:24-25,33`) used for a small, fixed widget vocabulary (thick/rounded blocks, tabs, table, paragraph, list, scrollbar). What a Go port must build by hand: raw termios, alternate screen, cursor-position queries (DSR), a cell buffer + diff, unicode width, and an event decoder (§13). The *inline* progress viewport is harder than any full-screen screen because it must insert lines into scrollback and re-anchor after child-process output.

---

## 2. Terminal lifecycle

### 2.1 Two guards, two terminal modes

Everything routes through two RAII guards in `src/terminal.rs`.

| Guard | Viewport | Raw mode | Alt screen | Mouse | Used by |
|---|---|---|---|---|---|
| `TerminalGuard` | full screen (`Term::new`, `src/terminal.rs:252`) | yes | yes | opt-in | summary, fetch, release-info |
| `InlineTerminalGuard` | `Viewport::Inline(lines)` (`src/terminal.rs:116-123`) | yes | **no** | no | progress |

`TerminalGuard::new()` (`src/terminal.rs:240-267`) is strictly ordered: `enable_raw_mode()` → `EnterAlternateScreen` → `Terminal::new(CrosstermBackend::new(stdout()))`. Each step has an explicit rollback: if alternate screen fails, raw mode is disabled again (`:243-249`); if `Term` construction fails, the alternate screen is exited and raw mode disabled before returning the error (`:252-259`). `TerminalGuard` owns `RawModeGuard`, `AltScreenGuard`, a `mouse_enabled` flag and the `Term`.

`RawModeGuard` (`src/terminal.rs:71-105`) and `AltScreenGuard` (`:195-229`) are idempotent (`active` flag) and *also* implement `Drop`, so cleanup happens twice-safely:

- `TerminalGuard::suspend()` — show cursor, disable mouse if enabled, leave alternate screen, disable raw mode (`:287-297`).
- `TerminalGuard::resume()` — enable raw mode, re-enter alternate screen, re-enable mouse if it was on, hide cursor, `terminal.clear()` (`:299-310`).
- `TerminalGuard::drop()` — disable mouse, leave alt screen, disable raw, show cursor (`:313-319`).

`InlineTerminalGuard` (`:107-193`) is subtly different and matters for parity:

- `hide()` = `leave(false)` — restore the viewport but **keep raw mode** (used when child output must be written, e.g. dpkg/debconf).
- `suspend()` = `leave(true)` — restore viewport **and** disable raw mode (used before handing the screen to another full-screen UI, e.g. release-info from `update`).
- `resume()` — re-enable raw mode, `hide_cursor()`, mark visible (`:144-149`).
- `leave()` (`:151-169`) restores by absolute repositioning: cursor to the frame origin, `clear_region(ClearType::AfterCursor)`, `swap_buffers()` to invalidate Ratatui's previous frame, `show_cursor()`, and finally a carriage return (`\r`) because "some terminal frontends retain Ratatui's last draw column even after an absolute cursor move" (`:161-164`, `:189-193`).
- `Drop` → `leave(true)` (`:185-187`).

**Panic / error cleanup.** `panic = "abort"` is deliberately *commented out* in the release profile (`Cargo.toml:63-66`: "This causes it to not work on some machines?"), so a Rust panic unwinds and the `Drop` impls above restore the terminal. There is **no** `std::panic::set_hook` and **no** signal handler for `SIGINT`/`SIGTERM`/`SIGHUP` anywhere in `src/` (only `SIGWINCH`, §2.4). Consequence: `SIGTERM` or `SIGHUP` while a full-screen view is up kills the process without running `Drop`, leaving the terminal in the alternate screen / raw mode. A Go port can trivially do better with `os/signal`, and the map's "superset" preference (map #37, standing preference 3) permits it; it is recorded here as a nala defect, not as parity to copy.

### 2.2 Raw mode and Ctrl+C

Raw mode disables canonical input, echo and signal generation, so **Ctrl+C arrives as a key event, not `SIGINT`**. Nala's handling is inconsistent and must be replicated deliberately or fixed:

| Screen | `q` | `Esc` | Ctrl+C | Enter |
|---|---|---|---|---|
| Summary | cancel (`src/tui/summary/mod.rs:327-329`) | cancel | **ignored** (no arm matches `Char('c')` + CONTROL; `:361`) | changelog |
| Fetch | confirm selection (`src/tui/fetch.rs:148-157`) | — | returns empty selection → error (`:159-161`) | confirm selection |
| Release info | reject (`src/tui/release_info.rs:112`) | reject | reject (`:106-109`) | reject |
| Inline progress | cancel if polled (`src/terminal.rs:63-66`) | — | cancel if polled | — |

`poll_exit_event()` (`src/terminal.rs:56-69`) is a zero-timeout `event::poll` + `event::read`, returning true only for `q` or Ctrl+C, and only when stdin is a terminal (`:57-59`). It is polled by `update` (`src/cmd/update.rs:119`), the downloader tick (`src/download/downloader.rs:411`) and mirror scoring (`src/cmd/fetch/score.rs:53`). It is **not** polled during the install/dpkg phase, where stdin is forwarded into the child pty instead (`src/dpkg.rs:404-425`, `:385-401`) — so **`q` cannot cancel an install**; the keystroke goes to dpkg/debconf.

### 2.3 Event loop shape

All three full-screen views use the same shape: **draw, then block on one event**. There is no tick, no timeout, no async select:

```
loop {
    terminal.draw(frame)?;          // Ratatui diffs against the previous buffer
    match event::read()? { ... }    // blocking read from stdin
}
```

- Summary: `src/tui/summary/mod.rs:317-371`.
- Fetch: `src/tui/fetch.rs:136-171`.
- Release info: `src/tui/release_info.rs:14-30`.
- The `show version info` sub-view has its own copy of the loop (`src/tui/summary/row.rs:227-253`).

Only `KeyEventKind::Press` is acted on (`summary:326`, `fetch:144`, `release:24`), which filters Windows-style key repeats/releases.

### 2.4 Resize

There is **no `Event::Resize` arm**. Resizes fall into the catch-all `_ => {}` (`src/tui/summary/mod.rs:368-369`, `fetch:167`, `release:114`) but the loop immediately redraws, and Ratatui re-reads the terminal size on each draw, so a resize is applied one iteration later. The inline renderer calls `terminal.autoresize()` explicitly before inserting a printed message (`src/tui/progress.rs:70-73`).

The term e2e test resizes the tmux pane from 100×30 to 140×30 while a debconf dialog is open and asserts output "resumes at column zero after whiptail and resize" (`tests/deb/term/run:104-128`).

For the **dpkg child**, nala installs a real `SIGWINCH` handler that copies the terminal size from stdin to the child pty (`src/dpkg.rs:69-94`), using `TIOCGWINSZ`/`TIOCSWINSZ` (`:52-55`) and a 24×80 fallback (`:58-67`). This is the only signal handling in the program.

### 2.5 Cursor-position queries (DSR)

The inline viewport needs to know where it is. The pty harness used by the release-info e2e test answers `ESC [ 6 n` with `ESC [ 1 ; 1 R` (`tests/deb/release-info/interact.py:44-45`, and the 100×30 window is set at `:20`). **Any Go reimplementation of the inline progress box must implement this handshake** (or an equivalent anchor strategy); the full-screen views do not need it because the alternate screen is known to be 0,0-based.

### 2.6 stdout vs stdin

TUI eligibility tests **stdout only**: `terminal_supports_ui(stdout_is_tty, TERM)` (`src/terminal.rs:26-36`). stdin is checked only for `poll_exit_event` (`:57`). So `nala --tui update </dev/null` can enter a full-screen view with no readable input. **(inferred)** this either errors or hangs on `event::read`; it is an edge case worth not copying.

---

## 3. Screen inventory and mode gating

### 3.1 `UiMode`

```rust
pub enum UiMode { Auto, Plain, Tui }   // src/config/file.rs:12-21
```

- `Auto` — default; "Inline progress and required interactive screens."
- `Plain` — "Static, line-oriented output."
- `Tui` — "Auto mode plus optional full-screen interfaces."

Config shape: `[Ui] mode = "Auto"` and `unit = "Binary"` (`src/config/file.rs:71-88`, `nala.conf:22-28`).

### 3.2 Resolution

`Config::ui_mode()` (`src/config/configuration.rs:190-203`):

1. `--no-tui` set → `Tui` becomes `Auto`; any other mode is unchanged. (So `--no-tui` cannot downgrade `Plain` further, and it *does* disable the "optional" full-screen views but keeps the required inline ones.)
2. else `--tui` set → `Tui`.
3. else the file value.

Both flags are global and mutually exclusive via clap (`src/cli/parser.rs:28-34`). If both somehow appear in overrides, `--no-tui` wins because it is checked first.

Unit tests pin the matrix: `Auto`→inline only, `Tui`→all, `Plain`→none, non-tty→none, `TERM=dumb`→none, and `--no-tui` turns a configured `Tui` into `Auto` but leaves `Plain` alone (`src/terminal.rs:327-387`, `src/config/configuration.rs:709-740`).

### 3.3 The three gates

| Gate | Formula | Cite |
|---|---|---|
| `use_enhanced_ui` | `mode != Plain` **and** stdout tty **and** `TERM != dumb` (case-insensitive) | `src/terminal.rs:38-40, 46-49` |
| `use_fullscreen_ui` | `mode == Tui` **and** stdout tty **and** `TERM != dumb` | `src/terminal.rs:42-44, 51-54` |

Terminal support: non-tty → false; `TERM` equal to `dumb` (case-insensitive) → false (`src/terminal.rs:26-36`).

### 3.4 Command → screen matrix

| Command (v0.18.2) | Full-screen view | Additional conditions | Cite |
|---|---|---|---|
| `install`, `reinstall` | Summary | not `simple`, not `-y`, not `-n`, not `--print-uris`, non-empty transaction | `src/summary.rs:16-41, 299-318` |
| `remove`, `purge` | Summary | same | `src/summary.rs:16-41` |
| `autoremove`, `autopurge` | Summary | same | `src/main.rs:140-143` |
| `upgrade`, `full-upgrade`, `safe-upgrade`, `dist-upgrade` | Summary | same | `src/cmd/upgrade/mod.rs:19-40`, `src/summary.rs:16-41` |
| `fix-broken` / `install -f` | Summary | same | `src/cmd/install.rs:107-116` |
| `history <id>` / `history info <id>` | Summary (read-only) | history entry has grouped packages | `src/cmd/history/mod.rs:76-88` |
| `history` (list), `clear/undo/redo` | — | never | `src/cmd/history/mod.rs:20-66` |
| `fetch` | Fetch | `use_enhanced_ui` **and** `--auto` absent | `src/cmd/fetch/mod.rs:82-94` |
| `update` | Release info | `use_enhanced_ui`, stdin+stdout tty, pending changes, not `-y`/`-n` | `src/cmd/update.rs:33-72` |
| `update` | Inline Progress (Update) | `use_enhanced_ui` | `src/progress.rs:403`, `src/cmd/update.rs:81` |
| `install`/`upgrade` downloads | Inline Progress (Download) | `use_enhanced_ui` | `src/summary.rs:325-353`, `src/download/downloader.rs:347` |
| commit (dpkg) | Inline Progress (Install) | `use_enhanced_ui` | `src/dpkg.rs:162` |
| `fetch` scoring | Inline Progress (MirrorScore) | `use_enhanced_ui` | `src/cmd/fetch/score.rs:21` |
| `download` | Inline Progress (Download) | `use_enhanced_ui` | `src/download/downloader.rs:23-27` |
| `list`, `search`, `show`, `policy`, `moo`, `clean` | — | never | `src/main.rs:96-145` |

### 3.5 Fallback to inline

Because the gates are pure predicates, fallback is simply "the next branch":

- Summary: `print_full_summary(...)` + `util::confirm(...)` (`src/summary.rs:38-40`) — i.e. the static `═`-ruled tables, then the line prompt. `--simple` short-circuits even earlier to the condensed summary (`:21-25`).
- Fetch: `automatic_mirrors(scored, 3)` — "Plain mode, not starting TUI" — and then `bail!("No mirrors were selected.")` only if empty (`src/cmd/fetch/mod.rs:86-98`).
- Release info: `confirm_with_default(config, "Accept these changes and continue updating?", false)` (`src/cmd/update.rs:57-58`); if stdin or stdout is not a tty it prints `release-info-noninteractive` and rejects (`:50-52`, string at `locales/en-US/main.ftl:38`).
- Progress: `PlainProgress` — a single `\r\x1b[2K[====    ] 43% …` line on **stderr**, interactive only when stderr is a tty and `TERM≠dumb` (`src/progress.rs:279-377`).

Note the `fail-open` direction of the progress fallback: if the enhanced renderer ever errors mid-run, `Progress::switch_to_plain` swaps to the plain renderer and keeps going (`src/progress.rs:497-538`). That is a deliberate resilience property a Go port should copy.

### 3.6 Assume-yes / assume-no interaction

- Summary: `-y` or `-n` **skips the TUI** and prints the static summary; the subsequent `confirm` is a no-op that returns true/false from the flags (`src/summary.rs:27-30`, `src/util/prompt.rs:18-39`).
- Fetch: unaffected by `-y`; it has its own `--auto[=N]` (default 3), which takes the fastest N and never opens the list (`src/cmd/fetch/mod.rs:83-88`, `src/cli/commands.rs:230-237`).
- Release info: `-n` → false, `-y` → true, both before any TUI is considered (`src/cmd/update.rs:46-49`).
- Progress: unaffected; runs regardless.

### 3.7 Privilege integration

There is **no in-TUI privilege prompt and no self-elevation**. Nala requires root up-front: `sudo_check` compares `Uid::effective()` to root and otherwise fails with `root-required = Nala needs root to { $command }` (`src/util/privilege.rs:22-27`, `locales/en-US/main.ftl:42`). For mutating commands the APT frontend lock is acquired before dispatch and held for the whole run — **including while the user sits in the summary TUI** (`src/main.rs:81-86`, `AptLockGuard` at `src/util/privilege.rs:7-19`). Hooks run before the summary is shown (`src/summary.rs:295`), dpkg after confirmation (`:372-395`).

So the TUI is *inside* the privileged, locked transaction — never before it. This matters for xbps, where the lock (if any) and privilege check must likewise precede the UI.

---

## 4. The shell: frame, borders, titles

There is no shared chrome module; the frame is two helper functions in `src/tui/summary/mod.rs:458-469`, reused everywhere:

```rust
pub fn header_block<'a>(config: &'a Config, title: &'a str) -> Block<'a> {
    basic_block(config)
        .title(format!("  {title}  ").set_style(tui_style::style(config, Theme::Highlight)))
        .title_alignment(Alignment::Center)
        .padding(Padding::horizontal(1))
}

pub fn basic_block(config: &Config) -> Block<'_> {
    Block::bordered()
        .border_type(BorderType::Thick)
        .border_style(tui_style::style(config, Theme::Primary))
}
```

Consequences, uniform across summary and release-info (fetch and the progress box use their own but equivalent blocks):

- **Double-line (thick) border**: `┏ ━ ┓ ┃ ┗ ┛` (Ratatui `BorderType::Thick`).
- **Title centred** in the top border, with two spaces of padding baked into the string (`"  Nala Upgrade  "`).
- **One column of horizontal padding** inside the border, so body text starts at column 3 (border + padding + the literal leading space many widgets add).
- Title colour = `Theme::Highlight` (default bold + terminal default fg, `src/config/color.rs:592`); border = `Theme::Primary` (bold bright green, `:588`).

Per-screen deviations:

- **Progress box**: rounded border (`BorderType::Rounded`, `╭╮╰╯`) with `Padding::horizontal(1)` and a *centred* highlighted title, width clamped to 80 (`src/tui/progress.rs:19-20, 140-155`).
- **Fetch**: rounded border, centred Highlight title, **no** horizontal padding on the outer block (`src/tui/fetch.rs:184-188`).
- **Release info**: `header_block(...)` with `.border_style(Notice)` — thick border, but **yellow** instead of green (`src/tui/release_info.rs:35-36`).
- **Summary header/table**: thick border; the scrollbar gutter gets its *own* thick border (§5.1).

Theme defaults that a port must reproduce (`src/config/color.rs:587-604`, `nala.conf:52-61`):

| Theme | Default |
|---|---|
| Primary | bold BrightGreen |
| Secondary | bold BrightBlue |
| Highlight | bold Reset |
| Regular | no-bold Reset |
| ProgressFilled | bold BrightGreen |
| ProgressUnfilled | bold BrightRed |
| Notice | bold BrightYellow |
| Warning | bold BrightYellow |
| Error | bold BrightRed |

`--color never` (or `Color.mode = "Never"`) makes `tui_style::style` return `RatStyle::default()` for every theme (`src/tui/style.rs:53-56`) — borders are still drawn, only SGR is dropped. The term e2e test runs the whole TUI with `--color never` (`tests/deb/term/run:58-64`), which is a useful invariant: **the layout must be legible with zero colour**.

---

## 5. Summary screen — complete geometry

### 5.1 Constraint tree

`SummaryTab::render` (`src/tui/summary/mod.rs:374-455`):

```
outer block (thick, title centred)                       block.inner(area) = body
└─ Layout::vertical([Length(1), Min(0), Length(summary.len())])
   .flex(Flex::Center)                                   // no effect: Min(0) absorbs slack
   ├─ [0] tab strip            (1 row)
   ├─ [1] active table         (all remaining rows)
   └─ [2] footer               (summary.len() rows)
        └─ Layout::horizontal([Max(header_len + size_len), Max(max_help_len)])
           .flex(Flex::SpaceAround)
           ├─ left:  Table of per-operation counts + download/disk sizes
           └─ right: Paragraph of the 4 help strings, centred, wrapped
```

Inside the table area, `App::render` (`src/tui/summary/mod.rs:126-151`) splits again:

```
Layout::horizontal([Min(0), Length(3)])
├─ table + its own thick block + header + rows
└─ 3-column gutter with its own thick block, holding a 1-column scrollbar inset by Margin{1,1}
```

The table itself (`src/tui/summary/mod.rs:82-123`):

- `.header(header)` — one row, styled `Regular`.
- `.row_highlight_style(Primary)` — the selected row is repainted bold green, overriding per-cell colours.
- `.flex(Flex::SpaceAround)` — leftover width is distributed around columns.
- `.block(basic_block(config))` — a second thick border, inside the outer one.
- `.highlight_spacing(HighlightSpacing::Never)` — no selection gutter column, so the layout never shifts when the selection moves.

### 5.2 The "tab bar" is an operation selector

Titles are the **present operations only**, in `Operation::to_vec()` order (`src/tui/summary/mod.rs:292-315`; order at `src/libnala/transaction.rs:24-37`):

`Remove, AutoRemove, Purge, AutoPurge, Install, ReInstall, Upgrade, Downgrade, Configure, Held`

Labels come from `locales/en-US/main.ftl:46-55` (`ReInstall` keeps its odd capitalisation). The strip is `Tabs::new(titles).highlight_style(Primary).select(pos).padding("","").divider(" ")`, rendered into a left-aligned area of `sum(title_widths) + len(titles) + 1` columns — i.e. two columns of slack, no fill to the right edge. There is **no bottom border** under the tabs and no block around them.

`select(pos)` is computed with `self.pkg_set.iter().position(...)`, i.e. a `BTreeMap<Operation, App>` order (`src/tui/summary/mod.rs:303-307`). This is safe only because the enum's `Ord` (`src/libnala/transaction.rs:9`) happens to match `to_vec()`. **A Go port should sort tabs and index them from one single ordered slice**, not from two orderings that must agree.

Initial tab = first present operation in that order (`src/tui/summary/mod.rs:203-208`). `h`/`l` (and Left/Right) move to the **next/previous present** operation, skipping absent ones, and refuse to wrap (`:252-290`).

### 5.3 Column widths, and why the table can clip

Widths are computed per column as

```
width[i] = max( max over rows of len(cell[i]), max over all headers of len(header) )
```

i.e. **every column's minimum is the longest header**, not its own header (`src/tui/summary/mod.rs:86-108`). Consequence: with the 5-column variant the `Reason` column is at least as wide as `"New Version"`, etc. That quirk is visible in real nala renders and should be reproduced if pixel parity matters, or fixed deliberately.

If the sum of widths exceeds the table area, the extra columns are **clipped**: there is no wrapping and no horizontal scrolling in this screen. Long package names or long versions therefore truncate.

### 5.4 Footer composition

`summary` (the footer vector) is built as (`src/tui/summary/mod.rs:380-398`):

1. one row per present operation: `["<Label>:", "<n> Pkgs"]` (`tui-pkgs` = `Pkgs`, `locales/en-US/main.ftl:265`),
2. one empty spacer row,
3. optional `["Total download size:", "<size>"]` when download size > 0 (`:178-183`; `locales/en-US/main.ftl:69`),
4. always `["Disk space required:"|"Disk space to free:", "<size>"]` (`:185-188`; `:70-71`).

Two columns: label left, value **right-aligned**, widths `header_len` and `size_len` measured over those rows only (`src/tui/summary/mod.rs:400-412`, `:435-448`).

**Footer height = the number of those rows.** For a one-operation install that is 4 (op, blank, download, disk); for a two-operation upgrade 5. The right-hand paragraph holds exactly 4 help lines (`summary_help`, `:26-33`), so a 4-row footer is the minimum that shows all of them.

### 5.5 Worked 80×24 example (two operations)

Frame geometry is exact; row contents, cell text and inter-column spacing are illustrative (real column widths depend on the transaction, and `Flex::SpaceAround` distributes leftover width at render time).

```text
 0 ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  Nala Upgrade  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
 1 ┃ Remove Upgrade                                                               ┃
 2 ┃ ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓┏━┓ ┃
 3 ┃ ┃ Package       Old Version   New Version   Size                        ┃┃ ┃ ┃
 4 ┃ ┃ libfoo        1.2-1         1.3-1          120 KiB                    ┃┃ ┃ ┃
 5 ┃ ┃ libbar        0.9-2         0.9-3           44 KiB                    ┃┃ ┃ ┃
 6 ┃ ┃ ...                                                                   ┃┃ ┃ ┃
 7 ┃ ┃                                                                       ┃┃ ┃ ┃
 8 ┃ ┃                          (table rows; scrollbar in the 1-column       ┃┃ ┃ ┃
 9 ┃ ┃                           inner area of the 3-column right gutter)     ┃┃ ┃ ┃
10 ┃ ┃                                                                       ┃┃ ┃ ┃
11 ┃ ┃                                                                       ┃┃ ┃ ┃
12 ┃ ┃                                                                       ┃┃ ┃ ┃
13 ┃ ┃                                                                       ┃┃ ┃ ┃
14 ┃ ┃                                                                       ┃┃ ┃ ┃
15 ┃ ┃                                                                       ┃┃ ┃ ┃
16 ┃ ┃                                                                       ┃┃ ┃ ┃
17 ┃ ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛┗━┛ ┃
18 ┃ Remove:               1 Pkgs          (↑) move up | (↓) move down            ┃
19 ┃ Upgrade:              3 Pkgs        (→) next tab | (←) previous tab          ┃
20 ┃                              (Enter) show changelog | (s) show version info  ┃
21 ┃ Total download size:12.4 MiB          (q) quit | (y) start upgrade           ┃
22 ┃ Disk space required:45.0 MiB                                                 ┃
23 ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

Note the footer's two columns are `Length(header_len)` and `Length(size_len)` with the size right-aligned (`src/tui/summary/mod.rs:435-448`). Because the label column is exactly as wide as the longest label, the widest row has **no gap** between label and value (`Total download size:12.4 MiB`); narrower labels get padding. That is a real rendering quirk, not a sketch error.

The four help lines are exactly `tui-help-move`, `tui-help-tabs`, `tui-help-show`, `tui-help-confirm` (`locales/en-US/main.ftl:266-270`), rendered as one centred, wrapped paragraph in the right-hand footer block (`src/tui/summary/mod.rs:450-454`).

Exact numbers for an 80×24 terminal:

- block interior = rows 1–22, columns 1–78; after the 1-col horizontal padding the content is rows 1–22 × columns 2–77 (76 wide);
- tab strip row 1; table rows 2–17; footer rows 18–22;
- table area 73 columns (x 2–74) and gutter 3 columns (x 75–77), drawn edge-to-edge with no gap;
- table inner 71×14 (header row 3, data rows 4–16); scrollbar drawn in x=76, rows 3–16.

### 5.6 Degenerate sizes

Because the layout is `Length`-fixed for the tab strip and the footer, shrinking the window steals rows from the table first. At `height ≤ 2 + footer` the table area collapses to zero rows and the two thick borders overlap; at `width < 6` the gutter consumes everything. Nothing detects this and nothing prints a notice — a **minimum-size policy is a decision #50 must make**, since nala has none.

---

## 6. Summary tab content and interaction

### 6.1 Row model

`SummaryRow` lazily builds its cells once (`OnceCell`, `src/tui/summary/row.rs:61-74, 121-148`):

1. **package name** — coloured by *operation* theme (`Error` for remove/autoremove/purge/autopurge, `Secondary` for install/upgrade, `Notice` for reinstall/downgrade/configure/held; `src/libnala/transaction.rs:80-88`), left-aligned;
2. if old ≠ new: **old version** (centred) and **new version with diff highlighting** (centred); else a single **version** cell (centred);
3. **reason** cell only if the transition has a `held_reason` (Excluded / Manual hold / Phased n% / Kept back; `src/libnala/transaction.rs:90-110`, `locales/en-US/main.ftl:57-64`);
4. **size**, right-aligned.

Header variants are chosen from the *first row only* (`src/tui/summary/mod.rs:86`, `src/tui/summary/row.rs:93-119`):

| Row 1 has old≠new? | has held reason? | Headers |
|---|---|---|
| yes | yes | Package, Old Version, New Version, Reason, Size |
| yes | no | Package, Old Version, New Version, Size |
| no | yes | Package, Version, Reason, Size |
| no | no | Package, Version, Size |

⚠️ Both the header row and the column count come from `items[0]`. A tab that mixes held and non-held rows will mismatch columns for the later rows. A Go port should compute headers per tab from the union/majority, or at minimum from a homogeneous group. Also note `SummaryTab::new` does **not** filter empty operation sets (`src/tui/summary/mod.rs:173-176`), while the static renderer does (`src/summary.rs:55-67`); an empty set would panic at `items[0]` (`:86`).

### 6.2 Version diff (v0.18.2 behaviour; differs from the legacy Python doc)

`util::version_diff` (`src/util/version.rs:3-42`):

1. revision-only change: if both versions split on the **last `-`** into equal halves, colour only the trailing revision, re-joining with `-`. (xbps uses `_`, not `-` — an adaptation point.)
2. otherwise split on `.`, find the first differing component index `i`, and colour **every component from `i` onward** with `Theme::Notice` (bold bright yellow).

Earlier research in this repo described the *legacy Python* algorithm (orange 256-colour `38;5;202`, only the changed suffix). **v0.18.2 does not do that**; the Rust implementation colours from the first difference to the end and uses the theme colour. The cell text arrives at Ratatui as embedded SGR which `color::ansi_to_text` parses back into styled spans (`src/config/color.rs:401-427`).

### 6.3 Selection and scrolling

- `TableState` starts at row 0 (`src/tui/summary/mod.rs:50`).
- `j`/`Down` next, `k`/`Up` previous — **clamped, no wrap** (`:66-80`).
- `Home`/`End` jump to first/last (`:337-338`), `PageUp`/`PageDown` move 10 rows (`:339-348`).
- Mouse wheel `ScrollUp`/`ScrollDown` move one row (`:363-367`).
- `ScrollbarState` content length = `len-1`, position = selection (`:48`, `:57-60`), rendered in the gutter with `begin_symbol(None).end_symbol(None)`, thumb `Primary`, track `Secondary` (`:136-149`).

The list does not "page"; Ratatui's `Table` keeps the selected row visible automatically.

### 6.4 Confirm / cancel

- `y` → `Ok(true)` → caller proceeds to download/commit (`src/tui/summary/mod.rs:330-332`; `src/summary.rs:316-318`).
- `q` or `Esc` → `Ok(false)` → `bail!("User refused confirmation")` → exit status 1 (`src/tui/summary/mod.rs:327-329`; `src/util/prompt.rs:9-15`).
- **Uppercase `Y` is not accepted** (only `Char('y')`), unlike release-info's `response_answer`.
- There is no "back"; `h`/`l` only move tabs.
- In the history variant, `confirm_enabled = false`, so `y` is dead, the help says `(q) quit`, and the returned bool is ignored (`src/tui/summary/mod.rs:218-232`, `:330`; `src/cmd/history/mod.rs:83-85`).

### 6.5 Changelog pager (Enter)

Enter on a row calls `render_changelog` (`src/tui/summary/row.rs:167-201`):

1. fetch the row's changelog URL with `reqwest` (cached in an `AsyncOnceCell`, `:150-163`); a failure becomes the pager body (`format!("{e:?}")`, `:172-175`);
2. `terminal.suspend()` — leave alternate screen, disable raw, show cursor;
3. spawn **`less --raw-control-chars --clear-screen`** with the changelog on stdin; ignore `BrokenPipe`;
4. `terminal.resume()` — re-enter alternate screen, clear, hide cursor;
5. the redraw happens at the top of the event loop.

This suspend → child → resume dance is the pattern for any external program. In a Go port it is `os/exec` + the same guard transitions.

### 6.6 Version-info sub-view (`s`)

`s` opens a full-screen, **non-scrolling** dump of the package's metadata (`src/tui/summary/row.rs:205-254`):

- `terminal.clear()`, then a `header_block` with the title `Nala Upgrade` (**even in history mode** — the string is hard-coded at `:229`), Highlight border;
- one `Paragraph` per line of `ShowVersion::pretty_map()` formatted as `"Header: first-line"` with the header highlighted (`:216-225`);
- row heights computed as `ceil(line_width / inner_width)` (`:232-235`);
- exit only on `q`/`Esc` (`:247-253`);
- mouse is still captured (the guard keeps it on) but ignored.

Because the loop redraws but there is no offset/scroll, content taller than the screen is clipped and unreachable. **(inferred)** long `apt show` output is therefore cut off in this view.

### 6.7 History variant differences

`SummaryTab::for_history` (`src/tui/summary/mod.rs:213-242`):

- title `Nala History` (`locales/en-US/main.ftl:264`);
- `download_size = None`, `disk_space = None`, so **footer height = number of operations + 1**;
- `confirm_enabled = false`.

With exactly one operation the footer gets **2 rows** but the help paragraph still contains 4 long lines padded to `max_help_len` columns (`:26-33`, `:428-454`) — the wrapped help is clipped. A Go port should either give the help its own fixed 4-row band or drop to a single help line in read-only mode.

---

## 7. Progress screens (inline viewport, not a tab)

### 7.1 What it is

`TuiProgressRenderer` wraps an `InlineTerminalGuard` of a per-view height (`src/tui/progress.rs:46-57`). The box is drawn at the bottom of the **normal** screen; the terminal's scrollback above it is the log. `print(msg)` clears the viewport, `insert_before(n, paragraph)` pushes the message above it (scrolling the terminal if needed), then redraws the box (`src/tui/progress.rs:65-85`).

That gives the visual the map calls "framed execution progress box", plus a running transcript above it.

### 7.2 The four views, and their heights

```rust
fn viewport_lines(self) -> u16 {
    match self {
        Update => 7,
        Install | MirrorScore => 6,
        Download { mirrors } => 7 + (mirrors > 0) + min(mirrors, 3) + (mirrors > 3),
    }
}
```
(`src/progress.rs:70-82`; `MAX_VISIBLE_MIRRORS = 3` at `:19`.)

The repository's own test pins the series `0→7, 1→9, 3→11, 8→12` (`src/progress.rs:658-664`). Height is fixed **at guard creation** from the mirror count the downloader knows up front (`src/download/downloader.rs:347`, `:355`), so it does not grow when additional domains appear mid-run.

Titles (`src/tui/progress.rs:125-132`; `locales/en-US/main.ftl:237-240`):

| View | Title | Progress from |
|---|---|---|
| Update | `Updating Package Lists` | apt acquire bytes |
| Download | `Downloading Packages` | package bytes + mirror table |
| Install | `Installing Packages` | dpkg status-fd percent |
| MirrorScore | `Testing Mirrors` | mirrors scored / total |

### 7.3 Band geometry

Common: width = `min(terminal_width, 80)` (`MAX_VIEW_WIDTH`, `src/tui/progress.rs:20`, `:140`), left-aligned, rounded border, 1-column horizontal padding → inner width `min(W,80) − 4`. Body rows are laid out with fixed `Length` constraints.

**Download** (12 rows at 4+ mirrors). Row indices below are exactly those asserted in `src/progress.rs:610-632` (title 0, mirror header 1, mirrors 2–4, overflow 5, last-completed 6, divider 7, info rows 9–10, bottom border 11):

```text
row 0   ╭───────────────────────────  Downloading Packages  ───────────────────────────╮
row 1   │  Mirrors:                      Connections     Average         State         │
row 2   │  deb.debian.org                3/3             12.0 MiB/s      downloading   │
row 3   │  deb.volian.org                0/3             7.0 MiB/s       idle          │
row 4   │  mirror.example                1/3             —               starting      │
row 5   │  +1 more mirrors · 5 active connections                                      │
row 6   │  Last completed: libssl3t64_3.5.1-1_arm64.deb                                │
row 7   ├────────────────────────────────── Progress ───────────────────────────── 73% ┤
row 8   │ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
row 9   │  Packages:   43/80                     Data:   19.0 MiB/33.0 MiB             │
row 10  │  Remaining:  5s                        Speed:  3.4 MiB/s                     │
row 11  ╰──────────────────────────────────────────────────────────────────────────────╯
```

Geometry notes for that sample (80-column terminal, 4 configured mirrors):

- box width = `min(80, 80) = 80`; content width = 80 − 2 borders − 2 padding = **76**;
- mirror columns are `[Min(16), 16, 16, 14]` → **30 / 16 / 16 / 14** at this width, each cell rendered as `" " + value` (`src/tui/progress.rs:363-453`);
- the divider is drawn on a rect forced to the **full frame width** (`Rect::new(frame.x, divider.y, frame.width, 1)`) with corners `├`/`┤` and only top/left/right borders, so it cuts through the box's side borders; the test pins `starts_with('├')` and `ends_with('┤')` (`src/tui/progress.rs:475-502`, `src/progress.rs:623-624`);
- the bar is one row of 76 cells; **both filled and unfilled use the same glyph `━` (U+2501)** and differ only by colour (§7.8);
- info rows are two 50 % columns (38 each), label padded to the widest label in its own column plus two spaces (`src/tui/progress.rs:525-560`).

**Update** (7 rows) — message, divider, bar, two info rows:

```text
╭──────────────────────────  Updating Package Lists  ──────────────────────────╮
│  Updated: deb.debian.org/debian stable InRelease                             │
├────────────────────────────────── Progress ───────────────────────────── 57% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Data:       19.0 MiB/33.0 MiB         Speed:    3.4 MiB/s                   │
│  Remaining:  5s                        Elapsed:  3s                          │
╰──────────────────────────────────────────────────────────────────────────────╯
```

**Install** (6 rows) — message, divider, bar, one info row:

```text
╭───────────────────────────  Installing Packages  ────────────────────────────╮
│  Status: Configuring libssl3t64:arm64                                        │
├────────────────────────────────── Progress ───────────────────────────── 43% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Package:  libssl3t64:arm64            Elapsed:  7s                          │
╰──────────────────────────────────────────────────────────────────────────────╯
```

**MirrorScore** (6 rows) — message, divider, bar, `Mirrors: n/total`, `Elapsed`:

```text
╭─────────────────────────────  Testing Mirrors  ──────────────────────────────╮
│  Finished: mirror.example.net                                                │
├────────────────────────────────── Progress ───────────────────────────── 58% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Mirrors:  7/12                        Elapsed:  3s                          │
╰──────────────────────────────────────────────────────────────────────────────╯
```

The install row geometry is pinned by `install_view_shows_current_package_and_status` (`src/progress.rs:734-760`).

### 7.4 The "log pane" is terminal scrollback

`print` wraps the message to the viewport width, counts wrapped lines (`message_height`, `src/tui/progress.rs:101-108`), clears the viewport, `insert_before`s the paragraph, then redraws. Messages are therefore immutable history: no scrollback buffer, no scrolling keys, no filtering. The only log-adjacent feature is the single `Last completed:` line inside the box.

During **install**, output from the dpkg pty is streamed into this transcript after filtering (`src/dpkg.rs:229-293`), with a spam filter (`:465+`) and a formatter that re-colours `Removing/Unpacking/Setting up/Processing` (`:428-463`).

### 7.5 Hiding for child full-screen programs

While dpkg/whiptail draws, the box must not fight it. Nala watches the child's byte stream for `ESC [ ? 1049 h` (alt screen), `ESC [ ? 2004 h` (bracketed paste) and `ESC [ 22 ; 0 ; 0 t` (save title/icon) and calls `progress.hide()`; it unhides on `ESC [ ? 1049 l` / `ESC [ 23 ; 0 ; 0 t` (`src/dpkg.rs:34-42`, `:241-267`). It also hides for dpkg's `ConfFile` status so conffile prompts are visible, and unhides afterwards (`:334-340`).

This is the hardest interaction in nala's whole TUI: a full-screen child hijacking the same terminal, with a heuristic ANSI detector. A Go port that shells out to `xbps-install` will face the same problem and should either (a) fully yield the terminal (leave raw mode, drop the box, stream child output verbatim) or (b) reproduce this detector.

### 7.6 Install progress source

Install percent is **not** package-count-based. It comes from dpkg's `--status-fd` lines: `percent` → `set_position`, `pkg_name` → `set_item` (skipping the `dpkg-exec` bookkeeping marker), status text → the message line with `Status: ` prefix and Error/Primary theme (`src/dpkg.rs:322-358`). `length` is fixed at 100 (`:163-164`).

For xbps this maps to the state callbacks already documented in `docs/research/xbps-progress-telemetry.md` (per-package state + percentage), not to dpkg's protocol.

### 7.7 Exit and cleanup

- `Progress::clean_up()` → inline `suspend()` (`src/progress.rs:540-545`), which clears the box and disables raw mode, leaving the transcript on screen.
- Cancellation during update/download/scoring is `poll_exit_event` → `clean_up` → `bail!("Nala was interrupted...")`-style errors (`src/cmd/update.rs:117-121`, `src/download/downloader.rs:411-415`, `src/cmd/fetch/score.rs:53-57`).
- `hide()`/`unhide()` keep raw mode; `suspend()`/`resume()` do not. The distinction matters when handing off to another UI: `Progress::suspend` is used around the release-info TUI (`src/cmd/update.rs:54-60`), and `resume()` re-renders immediately after re-enabling raw (`src/progress.rs:484-495`).

### 7.8 Colour-only affordances (accessibility)

Because `--color never` collapses every theme to `RatStyle::default()` (`src/tui/style.rs:53-56`), any affordance carried *only* by colour disappears. Four affordances are worth checking:

| Affordance | Shape cue | Colour-only? | Source |
|---|---|---|---|
| Progress bar fill | none — filled and unfilled are both `━` | **yes** — with no colour the bar is a solid rule; only the `NN%` in the divider reports progress | `src/tui/progress.rs:504-523` |
| Summary selected row | none — `HighlightSpacing::Never`, no pointer glyph | **yes** — the selection is invisible without colour | `src/tui/summary/mod.rs:117-120` |
| Fetch selected mirror | `✓` vs `☐` | no — survives monochrome | `src/tui/fetch.rs:26-33` |
| Release-info confirm | full sentence + `[y/N]` | no | `src/tui/release_info.rs:41-58` |

Consequence: the **plain** progress renderer (`[====    ] 43%`, `src/progress.rs:291-297`) is *more* legible in monochrome than the TUI. If xbpretty copies the TUI bar, #50 should decide whether to add a shape cue (e.g. `━`/`─` or a block/space split) rather than inheriting a colour-only bar.

---

## 8. Fetch screen (mirror selection)

### 8.1 Flow

`nala fetch` (`src/cmd/fetch/mod.rs:50-101`):

1. `sudo_check` (`:52`);
2. detect distro/release/keyring (`:54`), component (`:57`);
3. read existing sources to avoid duplicates (`:63`, `:71`);
4. fetch the mirror list from distro-specific tagfiles (Debian/Devuan) or the Ubuntu mirror API (`src/cmd/fetch/mirrors.rs:15-39`);
5. score them (below) with an **inline MirrorScore box**;
6. choose: `--auto[=N]` → fastest N; **no enhanced UI** → fastest 3, "Plain mode, not starting TUI"; otherwise **full-screen list** (`src/cmd/fetch/mod.rs:82-94`);
7. empty choice → error `No mirrors were selected.` (`:96-98`, `locales/en-US/main.ftl:471`);
8. write the sources file (`src/cmd/fetch/output.rs:51+`).

### 8.2 Scoring

`score_mirrors` (`src/cmd/fetch/score.rs:16-63`): one HTTP GET per mirror to `<url>/dists/<release>/Release`, 1 s timeout, no retry, max 10 concurrent, value = **milliseconds for the whole request+body**; results pushed as they arrive with a `Finished:` message, sorted ascending at the end. The user-facing definition is literally `fetch-score-help = Score is how many milliseconds it takes to download the Release file.` (`locales/en-US/main.ftl:484`). Scoring is cancellable via `poll_exit_event` (`score.rs:53-57`).

For Void the analogue is fetching `<repo>/<arch>-repodata` and timing it (#37 preference 9).

### 8.3 Geometry

`App::render` (`src/tui/fetch.rs:232-253`):

```
Layout::vertical([Min(0), Length(2), Length(2)])
├─ list area : outer rounded block, title "  Nala Fetch  " centred, Primary border
│              └─ Layout::horizontal([Length(max_url_len + 4), Length(max_score_len)])
│                 .flex(Flex::Center)          // the pair is centred as a group
│                 ├─ List  "Mirrors:"  (Primary text, padding vertical 1)
│                 └─ List  "Score:"    (padding vertical 1)
├─ info area : "\n" + fetch-score-help, centred, italic
└─ footer    : "\n" + fetch-help, centred
```

`fetch_block` uses `Block::default().title(..).style(Regular).padding(Padding::vertical(1))` — **no border** around either list (`src/tui/fetch.rs:255-260`), so the two lists read as borderless columns inside the outer frame.

80×24 example:

```text
 0 ╭────────────────────────────────  Nala Fetch  ────────────────────────────────╮
 1 │                Mirrors:                               Score:                 │
 2 │                ☐ https://deb.debian.org/debian        18 ms                  │
 3 │                ✓ https://mirror.example.net/debian    31 ms                  │
 4 │                ☐ https://debian.osuosl.org/debian     47 ms                  │
 5 │                ☐ https://slow.example.org/debian      112 ms                 │
 6 │ …                                                                             │
19 │    Score is how many milliseconds it takes to download the Release file.     │
20 │                                                                              │
21 │                                                                              │
22 │ Use ↓↑ to move, Space to select/unselect, Home/End to go top/bottom, q/Enter │
23 ╰──────────────────────────────────────────────────────────────────────────────╯
```

Exact geometry at 80×24 with the gallery's four mirrors (`src/progress/preview.rs:115-129`):

- list block 18 rows (outer block interior rows 1–18); the two lists are `[max_url_len + 4, max_score_len]` = **39 + 6 = 45** columns, centred as a pair → first column x = 16, score column x = 55 (0-based, relative to the 78-wide interior);
- each item is `"✓ "|"☐ " + url` (selected = Primary, unselected = Regular, `src/tui/fetch.rs:26-33`); scores are `"<ms> ms"` right of the URLs in a separately rendered list (`:199-228`);
- info line row 19, then a blank row, because each paragraph is `"\n" + text` (`:244-251`);
- **the help line is clipped at 80 columns**: the string is 85 columns, the paragraph is 78 wide and only 2 rows tall, so it wraps to `… q/Enter` on row 22 and `to exit.` is never shown. At ≥90 columns it fits on one line. This is a real 80-column layout defect to fix or reproduce deliberately.

(The help strings are at `locales/en-US/main.ftl:484-485`; the list titles `Mirrors:`/`Score:` at `:8` and `:483`.)

### 8.4 Item rendering and selection

- Each row is `✓ <url>` when selected (Primary style) or `☐ <url>` (Regular) in the left list; the right list shows `"<score> ms"` with the same style (`src/tui/fetch.rs:17-34`). `✓`/`☐` are the Unicode check marks; selection is conveyed twice (glyph + colour).
- Selection highlight is `Secondary.reversed()` applied to **both** lists through one shared `ListState` (`:210-228`), so the mirror and its score highlight together.
- Scores are already sorted ascending, so the best mirror is the top row.
- The first selection is `None`; the first `j`/`k` selects index 0 (the `last_selected` field is never written, so both directions start at 0) (`:36-41`, `:79-106`).
- Movement **wraps** (top↔bottom) — unlike the summary table (`:79-106`).
- `Space` toggles the selected row; with no selection it is a no-op (`:122-130`).

### 8.5 Keys and exit semantics

| Key | Action | Cite |
|---|---|---|
| `j` / `↓` | next (wrap) | `src/tui/fetch.rs:162` |
| `k` / `↑` | previous (wrap) | `:163` |
| `Space` | toggle selected | `:164` |
| `g` / `Home` | first | `:165` |
| `G` / `End` | last | `:166` |
| `q` / `Enter` | **confirm**: return the selected URLs | `:148-157` |
| `Ctrl+C` | return empty vec → `No mirrors were selected.` error | `:159-161` |

There is no mouse capture and no scrolling beyond the list-follows-selection behaviour; the help line omits `g`/`G` and omits that Ctrl+C aborts with an error.

---

## 9. Release-info screen

### 9.1 When it appears

Only from `nala update`, and only when APT reports that a repository's `Release` metadata changed in a way that is not auto-acceptable: `release_info_decision` prints each change message, returns true immediately if **all** changes have `default_action`, and otherwise prompts if interactive (`src/cmd/update.rs:33-72`). Non-interactive runs print `release-info-noninteractive` and fail the update (`:50-52`; `tests/deb/release-info/run:77-84`).

The inline progress box is suspended around the prompt and resumed after (`src/cmd/update.rs:54-60`).

### 9.2 Geometry

`render` (`src/tui/release_info.rs:32-69`):

```
header_block(config, "Repository Information Changed") with border_style = Notice   // thick, yellow
└─ Layout::vertical([Length(3), Min(1)])
   ├─ explanation : centred, wrapped, Regular            // "A repository's identifying information
   │                                                      //  changed in its Release file. Review…"
   └─ details     : Paragraph, Wrap{trim:false}
        Repository: <uri>
        Distribution: <dist>
        (blank)
        Changes
          <field>: <old> → <new>        // or "  <message>" when both values are empty
        (blank)
                 Accept these changes and continue updating? [y/N]   // Highlight, centred
                 (y) accept | (n/Enter/Esc) reject                   // Secondary, centred
```

- The confirm line is `release-info-confirm` + `confirmation_choice(keys, false)` → `[y/N]` in English (`src/tui/release_info.rs:41-51`; `src/util/prompt.rs:62-68`).
- The help line is `release-info-help = ({ $yes }) accept | ({ $no }/Enter/Esc) reject` (`locales/en-US/main.ftl:35`).
- Old value is styled Secondary, the `→` Regular, the new value Highlight (`src/tui/release_info.rs:71-103`).
- **No scrolling**: the details area is the remaining height and long change lists clip.
- 80×24 example (illustrative values from `src/progress/preview.rs:83-113`):

```text
 0 ┏━━━━━━━━━━━━━━━━━━━━━━  Repository Information Changed  ━━━━━━━━━━━━━━━━━━━━━━┓
 1 │     A repository's identifying information changed in its Release file.      │
 2 │                    Review the changes before continuing.                     │
 3 │                                                                              │
 4 │ Repository: https://deb.example.org/debian                                   │
 5 │ Distribution: stable                                                         │
 6 │                                                                              │
 7 │ Changes                                                                      │
 8 │   Origin: Example Linux → Example GNU/Linux                                  │
 9 │   Version: 12 → 13                                                           │
10 │                                                                              │
11 │              Accept these changes and continue updating? [y/N]               │
12 │                      (y) accept | (n/Enter/Esc) reject                       │
23 ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

Layout: explanation occupies exactly 3 interior rows (`Constraint::Length(3)`, `src/tui/release_info.rs:37`), so the en-US two-line string is vertically padded, then the details paragraph takes the rest. Both the confirm prompt and the help line are `Line::centered` within the details width (`:41-58`). There is **no scroll**: more than ~10 change lines simply clip at the bottom.

### 9.3 Keys

| Key | Result | Cite |
|---|---|---|
| `y` / `Y` | accept | `src/tui/release_info.rs:111-113` |
| `n` / `N` | reject | same |
| `q` / `Q` | reject | `:112` |
| `Enter` | reject (default is *No*) | `:112` |
| `Esc` | reject | `:112` |
| `Ctrl+C` | reject | `:106-109` |
| any other key | ignored | `:113-115` |

Keys can be localised: `confirmation_keys` parses the translated `Y/y N/n` template, and the tests pin Turkish `e/h` and Russian `д/н` (`src/util/prompt.rs:41-60`; `src/tui/release_info.rs:149-178`; e2e at `tests/deb/release-info/run:106-122`). Default is always **reject** (`release_info_prompt_defaults_to_rejection`, `src/tui/release_info.rs:124-147`).

### 9.4 How APT-specific it is

Entirely. The screen is a renderer for `rust_apt::progress::ReleaseInfoChanges` (`src/tui/release_info.rs:6`), whose `changes` are `ReleaseInfoChange { field, old_value, new_value, message, default_action }` (`src/progress/preview.rs:83-103`). The fields are `Release`-file keys (`Origin`, `Version`, …), the trigger is libapt's `Acquire::AllowReleaseInfoChange` gate, and the fix-up advice printed in non-interactive mode is an APT `-o` option (`locales/en-US/main.ftl:38`).

**xbps has no equivalent concept**: Void repositories are signed `repodata`, xbps never asks the user to accept metadata drift, and there is no `Release` file. This screen therefore has **no behavioural xbps equivalent** and must become an explicit, tested N/A — or be repurposed into a Void-specific notice (the map already lists this as open fog: "What replaces nala's apt release-upgrade tab/banner on Void").

---

## 10. Keybindings and mouse — consolidated

| Context | Key | Semantics | Cite |
|---|---|---|---|
| Summary | `j` / `↓` | next row (clamp) | `src/tui/summary/mod.rs:335` |
| Summary | `k` / `↑` | previous row (clamp) | `:336` |
| Summary | `h` / `←` | previous present tab (clamp) | `:334` |
| Summary | `l` / `→` | next present tab (clamp) | `:333` |
| Summary | `Home` / `End` | first / last row | `:337-338` |
| Summary | `PageUp`/`PageDown` | ±10 rows | `:339-348` |
| Summary | `Enter` | changelog in `less` | `:349-354` |
| Summary | `s` | version-info sub-view | `:355-360` |
| Summary | `y` | confirm (only if `confirm_enabled`) | `:330-332` |
| Summary | `q` / `Esc` | cancel → exit 1 | `:327-329` |
| Summary | wheel up/down | prev / next row | `:363-367` |
| Version-info | `q` / `Esc` | back | `src/tui/summary/row.rs:247-253` |
| Fetch | `j`/`k`/`↑`/`↓` | move (wrap) | `src/tui/fetch.rs:162-163` |
| Fetch | `Space` | toggle | `:164` |
| Fetch | `g`/`Home`, `G`/`End` | top / bottom | `:165-166` |
| Fetch | `q`/`Enter` | confirm | `:148-157` |
| Fetch | `Ctrl+C` | abort with error | `:159-161` |
| Release info | `y`/`Y`, `n`/`N`, `q`/`Q`, `Enter`, `Esc`, `Ctrl+C` | accept / reject | `src/tui/release_info.rs:105-116` |
| Progress (polled) | `q`, `Ctrl+C` | abort transfer/update/score | `src/terminal.rs:56-69` |

**No keybinding is shared across screens** except `q`; there is no global command palette, no help overlay, no text input, and no paste handling anywhere.

**Mouse** is only: summary wheel up/down (`src/tui/summary/mod.rs:318,363-367`). Capture is enabled when the summary loop starts and disabled in `Drop` (`src/terminal.rs:271-285,313-319`). No clicks, no drag, no hover anywhere.

---

## 11. What #50 and #52 must decide — facts, not opinions

These are the load-bearing facts from the source that change the shape of the downstream tickets.

1. **No four-tab shell exists.** Do not design a tab bar with Summary/Progress/Fetch/Release. In nala: summary is a full-screen app whose tabs are *operations*; progress is an inline bottom box; fetch is its own full-screen list; release-info is a modal full-screen prompt. Only summary/history are classed as "optional" full-screen views; fetch/release-info are "required interactive screens" and appear even in `Auto`. (#50, #52)
2. **The summary TUI is opt-in; the other two full-screen views are not.** `Auto` (default) ⇒ inline progress + full-screen fetch + full-screen release prompt, but static summary. If xbpretty copies nala, most users never see the tabbed screen. (#50: `Ui.mode` semantics)
3. **Progress has no log pane, no per-package rows, no scrolling.** It has four fixed-height views and one `Last completed:` line plus inserted transcript lines above the box. If xbpretty wants a per-package list, that is an extension, not parity. (#50, #52)
4. **`-y` disables the summary TUI but not the progress box**; `--simple` disables the table entirely. (#50: flag interactions)
5. **Cancellation is exit-code 1 with an error string**, not a clean `exit 0`. In fetch, cancelling without a selection is also an error. (#50: UX, #52: test expectations)
6. **No minimum size, no resize UI, no too-small message.** Decide whether to copy that or add a floor (the map permits supersets). (#50)
7. **Install-phase keystrokes go to dpkg, not to nala** — `q` cannot cancel an install. (#50: keybinding semantics must be per-phase)
8. **Release info is APT-only** with no xbps analogue; it must be an explicit, tested N/A or replaced by a Void-specific notice. (#50: the map's open fog)
9. **Rendering invariants that must survive**: thick border for summary/release-info vs rounded for progress/fetch; only the progress box is width-clamped (80); summary table minimum column width = longest header; selected summary row repaints bold green over per-operation colours. Under `--color never` the *layout* survives but the progress bar and the summary selection do not, because both are colour-only (§7.8) — xbpretty must decide whether to add shape cues. (#52)
10. **The zero-dependency cost is in the engine, not the screens** (§13.6): terminal/keyboard/buffer/width ≈ 2–3 k LOC before the first screen is drawn. (#52 feasibility)

---

## 12. xbps adaptation notes per surface

| Nala concept | v0.18.2 implementation | xbps reality | Consequence |
|---|---|---|---|
| Summary operations (10 variants) | `Operation` enum + labels (`src/libnala/transaction.rs:10-55`) | xbps plans have *actions*: install, update, remove, hold, reconfigure (see `GLOSSARY.md`: Install/Upgrade/Remove/Hold groups) | tab set is smaller (≤5); keep `Operation::to_vec` ordering as a single source of truth |
| Old/new version + diff | apt version strings, `-` revision | xbps `<pkg>-<version>_<revision>` | diff must split on `_`, not `-` (`src/util/version.rs:5-11`) |
| Held reason column | apt holds, phased updates, kept back (`src/libnala/transaction.rs:90-110`) | xbps `hold`/`unhold` only; no phased updates | drop `Phased`/`KeptBack` rows or map to a generic "held" reason |
| Sizes | apt depcache download/disk | xbps transaction …download/installed size | same two footer rows |
| Download mirrors ×3 | nala's own async downloader (`src/download/downloader.rs`) | xbps downloads archives itself when running `xbps-install`; predownload is a design decision (#37 pref 8) | the mirror table only exists if xbpretty owns the transfer |
| Install percent | dpkg `--status-fd` percent (`src/dpkg.rs:322-358`) | xbps state callback percent (`docs/research/xbps-progress-telemetry.md`) | same three bands; bar/percent unchanged |
| Release info | `ReleaseInfoChanges` | **none** | explicit N/A |
| Fetch scoring | GET `dists/<rel>/Release` | GET `<repo>/<arch>-repodata` | same list UI; different probe |
| Fetch output | writes `/etc/apt/sources.list.d/nala-sources.list` | writes `/etc/xbps.d/*.conf` `repository=` lines | same selection flow |
| `less` changelog | apt changelog URI | xbps has no changelog URI on packages | likely N/A or a Void package-page URL |

---

## 13. Zero-dependency Go equivalent (NFR-001)

### 13.1 What the Rust stack actually uses

Nala's TUI is **not** a hand-rolled ANSI program; it is crossterm 0.29 + ratatui 0.30.2 with `default-features = false, features = ["crossterm_0_29"]` (`Cargo.toml:24-25,33`). The app-level use of that stack is small and enumerable:

- terminals: raw mode, alt screen, cursor show/hide, size, clear, cursor-position query, scrollback insertion (`src/terminal.rs`);
- widgets: `Block` (thick/rounded borders, titles, padding), `Tabs`, `Table` + `TableState` + `Scrollbar`, `Paragraph` + `Wrap`, `List` + `ListState`, `Line`/`Span`/`Text`, `Cell` (`src/tui/**`);
- styles: fg/bg colour (named, 256-indexed, RGB) + 9 modifiers, converted from nala's own theme config (`src/tui/style.rs`);
- events: key press/release + mouse wheel (`src/tui/**`).

Nothing else. No layout DSL beyond `Layout::horizontal/vertical` with `Length/Min/Max/Percentage` constraints and `Flex::{Center, SpaceAround}`, no custom widgets, no animation, no timers.

### 13.2 What NFR-001 rules out

- Any Ratatui-equivalent widget/TUI library: `bubbletea`, `lipgloss`, `tcell`, `tview`, `termbox`, `pterm`, `gocui`, `termui`.
- Terminal/termios helpers from `golang.org/x/*`: `x/term`, `x/sys/unix`, `x/text`, `x/exp`.
- Width/grapheme libraries: `mattn/go-runewidth`, `rivo/uniseg`.
- `ncurses`/terminfo via cgo.
- Anything that raises the dependency graph above `go.mod = module + go version` (currently zero require lines).

### 13.3 What it still allows

- `syscall` for `ioctl`: `TCGETS`, `TCSETS`, `TIOCGWINSZ` (constants exist per-GOARCH in the standard library; do **not** hard-code `0x5401`/`0x5413`, they differ on mips/ppc). `unsafe.Pointer` to `syscall.Termios`/`Winsize` is permitted and CGO-free.
- `os/signal` for `SIGWINCH` (and, as a superset, `SIGINT`/`SIGTERM` restore-on-exit).
- `os/exec` for the pager and for `xbps-install`; `os.Pipe` for status streams.
- `unicode/utf8`, `strconv`, `strings`, `time`, `bufio`, `sync`, `math`.
- **Embedded tables as data** (a `wcwidth` interval table, border glyph sets, key sequences). Tables are code, not dependencies; a 1000-entry width table is ~8 KB of Go source.
- `embed` for static assets, if any.

### 13.4 VT100 primitives to implement, with the nala call site that forces each

| Primitive | Sequence(s) | Forced by |
|---|---|---|
| alternate screen on/off | `CSI ? 1049 h` / `CSI ? 1049 l` | `EnterAlternateScreen`/`LeaveAlternateScreen`, `src/terminal.rs:202,216,226,255` |
| raw mode on/off | termios: clear `ICANON`/`ECHO`/`ISIG`/`IEXTEN` (and `IXON`/`ICRNL`), `VMIN≥1`; restore saved termios | `enable_raw_mode`/`disable_raw_mode`, `src/terminal.rs:78,84,92,246,295,317` |
| cursor hide/show | `CSI ? 25 l` / `CSI ? 25 h` | `hide_cursor`/`show_cursor`, `src/terminal.rs:146,180,288,307,318` |
| cursor position query | `CSI 6 n` → read `CSI r ; c R` | inline viewport & `insert_before` (`tests/deb/release-info/interact.py:44-45`); summary restore uses absolute moves instead (`src/terminal.rs:171-182`) |
| absolute cursor move | `CSI r ; c H` | `set_cursor_position`, `src/terminal.rs:173` |
| clear from cursor | `CSI 0 J` | `ClearType::AfterCursor`, `src/terminal.rs:176` |
| clear screen | `CSI 2 J` (or `CSI H CSI 2J`) | `terminal.clear()`, `src/terminal.rs:308`, `src/tui/summary/row.rs:213` |
| scroll up 1 line / insert lines | `CSI S` or `LF` at bottom + region ops | `insert_before`, `src/tui/progress.rs:81-83` |
| mouse capture on/off (SGR) | `CSI ? 1000 h 1002 h 1003 h 1006 h` / `l` | `EnableMouseCapture`, `src/terminal.rs:273,281,291,304` |
| SGR colours | `CSI 0/1/2/3/4/5/6/7/8/9 m`, `CSI 38;5;n`, `CSI 38;2;r;g;b` (+ `48` bg) | `src/tui/style.rs:6-51`, emitters at `src/config/color.rs:35-56` |
| SGR reset/parse (incoming text) | parse `CSI [0-9;]* m` into segments | `color::ansi_to_text`, `src/config/color.rs:401-427` |
| carriage return flush | `\r` | `reset_cursor_column`, `src/terminal.rs:189-193` |

Glyph inventory (all single-width, safe): `┏ ━ ┓ ┃ ┗ ┛` (thick), `╭ ╮ ╰ ╯ ─ ┤ ├` (rounded/divider), the scrollbar thumb/track (Ratatui defaults; begin/end symbols explicitly disabled at `src/tui/summary/mod.rs:141-142`), `✓ ☐ ↑ ↓ → ← · —`, plus `━` (U+2501) for the bar. Note `━` vs `─`: the progress bar uses the **heavy** horizontal, the divider the normal one (`src/tui/progress.rs:21-30,504-523`).

### 13.5 Engine components a Go port must build

| Component | Scope | Why it cannot be skipped |
|---|---|---|
| `term` | termios get/set, winsize, alt screen, cursor, raw guard, SIGWINCH | all four screens |
| `screen` | cell grid (rune + fg + bg + modifiers), clip, diff against previous frame, minimal SGR runs | flicker-free redraw; nala's loop redraws on every event |
| `width` | `wcwidth`-style display width for runes + combining | table column widths and wrapping; required even in English for the box glyphs and `✓/☐` |
| `layout` | rect splits with `Length/Min/Max/Percentage` + `Flex` | every nala screen is a constraint tree; hard-coding rects per screen is possible but brittle on resize |
| `widgets` | block/borders/title/padding, paragraph+wrap, table+state, tabs, list, scrollbar | summary, fetch, progress |
| `input` | UTF-8 decode, CSI/SS3 key map (arrows/Home/End/PgUp/PgDn), SGR mouse decode, Ctrl+letter | keybindings §10 |
| `inline` | bottom-anchored viewport: DSR anchor, insert N lines above, re-anchor after child output | progress only — and it is the hardest part |

### 13.6 Rough sizing and difficulty ranking

Order-of-magnitude (Go, including tests), to be refined by #52:

| Piece | LOC | Difficulty |
|---|---|---|
| termios/alt-screen/cursor/winsize/SIGWINCH | 300–500 | low (syscall plumbing, per-arch constants) |
| screen buffer + diff + SGR writer | 500–800 | medium (diff correctness, SGR run minimisation) |
| width table + wrap | 200–400 | medium (correctness of combining/wide chars) |
| layout subset | 300–500 | medium |
| widgets (block, paragraph, table, tabs, list, scrollbar) | 900–1400 | medium-high (table column math, selection-visible scrolling) |
| input decoder | 400–700 | medium (escape-sequence edge cases) |
| **inline progress viewport** | 400–700 | **high** (DSR, scrollback insertion, re-anchor after dpkg/debconf output, resize) |
| summary screen | 500–700 | medium |
| fetch screen | 250–400 | low-medium |
| progress screens | 500–800 | medium |
| **total** | **≈ 4.3k–7k** | |

**Verdict for #52:** faithful reproduction is feasible with the standard library, but the prototype should be scoped to prove the *engine seams* — alt screen + raw guard + diff renderer + one table screen + one inline progress box — rather than all four surfaces. The riskiest unknown is not the tabbed summary (plain 2-D drawing); it is the inline box's coexistence with a full-screen child (`src/dpkg.rs:241-267`) and its DSR-based anchoring.

### 13.7 Parity traps a Go implementation will hit

1. **Width clamping is asymmetric**: only the progress box caps at 80; summary/fetch/release-info are full-width (`src/tui/progress.rs:140` vs `src/tui/summary/mod.rs:378`).
2. **Summary column minimum = longest header**, not own header (`src/tui/summary/mod.rs:86-108`).
3. **Header/column count from row 0** (`:86`) — reproduce or fix deliberately.
4. **Footer height is data-dependent** = op count + spacer + sizes (`:414-417`) and the help can clip (history).
5. **Tab strip does not fill the width** — it is a fixed-width left-aligned area (`:300-314`).
6. **Selected summary row overrides per-operation name colour** (`:117`).
7. **Progress divider spans the frame, ignoring inner padding** (`src/tui/progress.rs:475`).
8. **Bar is heavy `━`, divider is light `─`** (`:21-30`, `:514-520`), and the bar's fill/unfill share the same glyph — colour only (§7.8).
9. **Mirror table switches from 3 to 4 columns at inner width 72**, i.e. terminal width 76 (`:19`, `:363-382`).
10. **`q` in the summary is exit-1**, `y` must be lowercase there but either case in release-info.
11. **`insert_before` messages are permanent**; a redraw must never repaint them (`src/tui/progress.rs:70-85`).
12. **Colour-off mode still draws all borders** (`src/tui/style.rs:53-56`).

---

## 14. Gaps and what could not be verified

1. **No live capture.** The crate was not built (no `libapt-pkg-dev`); screens are derived from constraint trees and the repo's own row-index assertions. Exact glyph runs (e.g. Ratatui's default scrollbar symbols, title truncation on narrow terminals) were not observed. #52's prototype should capture real frames and reconcile.
2. **Ratatui internals** (inline-viewport re-anchoring, `insert_before` scroll strategy, diff algorithm) are treated as a black box; only nala's calls and the DSR evidence are cited. A Go port is free to choose a different strategy as long as the observable behaviour holds.
3. **No upper bound on operation tabs** was exercised: 10 enum variants exist; whether a real transaction can show many at once (and how the fixed-width tab strip overflows) was not tested. **(inferred)** >8 tabs would overflow the strip area and clip.
4. **`s` (version info) layout** computes paragraph heights from `line.width()/inner.width` but does not account for the `"Header: "` prefix; **(inferred)** very long values can overflow the allocated row.
5. **Alternate-screen behaviour of `less`** after `resume()` (cursor position, whether the screen is fully repainted) relies on the Redraw-on-resume invalidation (`swap_buffers`), not observed.
6. **Non-Linux/BSD terminals** were not considered; the map targets Void Linux only.
7. **The exact rendered look of `--color never`** in the fetch highlight (`Secondary.reversed()` → reverse video with no colour) is inferred to be white-on-black inverse; not captured.

---

## Appendix A — source index (v0.18.2)

| File | Lines | What it owns |
|---|---|---|
| `src/terminal.rs` | 388 | UI-mode policy, raw mode, alt screen, mouse, inline viewport, exit polling |
| `src/tui/mod.rs` | 5 | module list |
| `src/tui/style.rs` | 75 | theme → ratatui style, colour-off behaviour |
| `src/tui/summary/mod.rs` | 469 | summary shell, tabs, table, scrollbar, footer, key loop |
| `src/tui/summary/row.rs` | 255 | row cells, version diff, changelog pager, version-info view |
| `src/tui/progress.rs` | 572 | inline progress renderer, four views, bars, mirror table |
| `src/tui/fetch.rs` | 260 | mirror list, scores, selection |
| `src/tui/release_info.rs` | 179 | release-info modal |
| `src/progress.rs` | 761 | progress state model, view selection, plain fallback, layout tests |
| `src/progress/preview.rs` | 178 | ignored `tui_preview` gallery test (Rust-only reproduction harness) |
| `src/summary.rs` | 459 | static summary, TUI entry, commit pipeline |
| `src/cmd/fetch/mod.rs` | 118 | fetch flow and TUI gate |
| `src/cmd/fetch/score.rs` | 128 | mirror scoring |
| `src/cmd/update.rs` | 342 | update loop, release-info decision, exit polling |
| `src/cmd/history/mod.rs` | 91 | history TUI entry |
| `src/dpkg.rs` | 560 | dpkg pty, status-fd percent, SIGWINCH, hide/unhide heuristics |
| `src/config/file.rs` | 171 | `UiMode`, `[Ui]` config |
| `src/config/configuration.rs` | 755 | `ui_mode()` override resolution |
| `src/util/prompt.rs` | 162 | line prompts, localised yes/no keys |
| `src/util/privilege.rs` | 43 | root check, apt lock guard |
| `src/main.rs` | 272 | command dispatch, lock, exit codes |
| `locales/en-US/main.ftl` | 517 | every user-visible string cited here |
| `nala.conf` | 61 | shipped defaults incl. `[Ui] mode = "Auto"` |

## Appendix B — test evidence used

| Test | Asserts | Cite |
|---|---|---|
| `download_view_has_no_unused_rows` | exact download row indices, 80-col clamp, column alignment | `src/progress.rs:587-656` |
| `download_height_follows_known_mirror_count` | 7/9/11/12 rows for 0/1/3/8 mirrors | `:658-664` |
| `mirror_state_comes_from_real_activity` | Starting/Downloading/Idle derivation | `:666-680` |
| `narrow_download_view_keeps_the_mirror_state_legible` | 3-column layout below inner width 72 | `:696-719` |
| `wide_terminal_keeps_the_progress_view_left_aligned_and_eighty_columns_wide` | `╭` at (0,0), `╮` at (79,0), blank at 80 | `:721-732` |
| `install_view_shows_current_package_and_status` | install row layout | `:734-760` |
| `release_info_prompt_defaults_to_rejection` | Enter/Esc/Ctrl+C reject, `y` accepts, others ignored | `src/tui/release_info.rs:124-147` |
| `release_info_prompt_accepts_translated_keys` | localised keys | `:149-178` |
| `ui_mode_respects_runtime_overrides` / `no_tui_disables_only_optional_fullscreen_interfaces` | mode matrix | `src/config/configuration.rs:709-740` |
| `plain_mode_disables_enhanced_ui` … `tui_mode_allows_all_ui` | gate matrix | `src/terminal.rs:327-387` |
| `tests/deb/term/run` | `--tui --color never` shows "Nala Upgrade", `q` → status 1, no dpkg/history change; "Downloading Packages" box cancellable; terminal resumes at column 0 after whiptail + resize | `tests/deb/term/run:58-128` |
| `tests/deb/release-info/run` + `interact.py` | release-info TUI accept/reject, `[y/N]`, non-interactive failure, localised keys, DSR answering, 100×30 pty | `tests/deb/release-info/run:63-122`, `interact.py:14-58` |

## Appendix C — reproducing the gallery

With `libapt-pkg-dev` installed:

```sh
cargo test tui_preview -- --ignored --nocapture
```

prints rendered buffers for one-mirror download, mirror overflow, narrow download, update, install, mirror scoring, release info (80×24 `TestBackend`) and fetch — the exact code paths cited above (`src/progress/preview.rs:131-178`). That is the fastest way for #52 to obtain ground-truth frames.

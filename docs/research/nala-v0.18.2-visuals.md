# Research: nala v0.18.2 inline visual surfaces — byte-faithful reference

- **Ticket:** [#39](https://github.com/soubarnak/xbpretty/issues/39) — *Research: nala v0.18.2 visual system for inline and full-screen output*
- **Parent map:** [#37](https://github.com/soubarnak/xbpretty/issues/37) — *Map: Nala v0.18.2 parity for xbpretty*
- **Downstream consumers:** [#45](https://github.com/soubarnak/xbpretty/issues/45) (visual-system grilling), [#50](https://github.com/soubarnak/xbpretty/issues/50) (TUI), [#51](https://github.com/soubarnak/xbpretty/issues/51) / [#52](https://github.com/soubarnak/xbpretty/issues/52) (prototypes)
- **Branch:** `research/nala-visuals` (throwaway; never merged)
- **Status:** Complete
- **Pinned reference:** tag `v0.18.2`, commit `2b7ff0554ca88e761fa6eaf8604e19c5e3033815`, tagged 2026-10-22 — <https://github.com/volitank/nala/tree/v0.18.2>
- **Date:** 2026-11-05
- **Author:** Soubarna (agentic research)

---

## 0. How this document is built (read first)

**Citation convention.** Every `path:line` citation below is relative to the **v0.18.2 tag**, browsable at
`https://github.com/volitank/nala/blob/v0.18.2/<path>`. External crates are cited by name and version
(`comfy-table 7.2.2`, `ratatui 0.30.2`, `rust-apt 0.11.3`) because their behaviour is part of nala's output.

**Method.** nala itself cannot be built on this host (it links `libapt-pkg` and is Debian-only), so the
render paths were executed rather than guessed:

1. The tagged source was read end-to-end for every surface in scope (`src/summary.rs`, `src/table.rs`,
   `src/tui/summary/{mod,row}.rs`, `src/progress.rs`, `src/tui/progress.rs`, `src/config/{color,file,mod}.rs`,
   `src/terminal.rs`, `src/util/{prompt,version,units}.rs`, `src/libnala/transaction.rs`, `src/dpkg.rs`,
   `src/download/downloader.rs`, `src/cmd/update.rs`, `locales/en-US/main.ftl`, `nala.conf`).
2. Two throwaway Rust harnesses re-executed the same code paths with the **same crate versions nala pins**
   (`Cargo.toml:22,37`):
   - a `comfy-table 7.2.2` harness reproducing `table::get_table` + `SummaryRow` cell strings exactly, driven
     at chosen terminal widths (tty path via `set_width`, piped path via `force_no_tty`);
   - a `ratatui 0.30.2` harness with `render_progress_view` and `ProgressState` ported line-for-line.
3. Fixtures are copied from nala's own gallery/test data in `src/progress/preview.rs:51-81` and
   `src/progress.rs:587-656`; the ported renderer reproduces **every assertion** of the upstream test
   `download_view_has_no_unused_rows` (line count, title line, four mirror rows, overflow row, message row,
   `├…73%…┤` divider row, `Packages:`/`Data:`/`Remaining:`/`Speed:` rows, `╰` bottom row, and the shared
   column x-positions for `Connections`/`3/3`, `Average`/`12 B/s`, `State`/`downloading`).
4. Everything that is a formula (rule width, bar fill, percent, ETA, unit rendering) was checked against the
   function bodies quoted in citations, not inferred from a screenshot.

**What the frames are.** ANSI is stripped from the frames; glyphs, spaces and line breaks are byte-faithful.
Table lines carry a **trailing space** (comfy-table's right padding is not trimmed, see §3.6 and Appendix A).
The prompt line has **no trailing newline**. Live values (percent/rate/ETA) are fixture values computed with
the real formulas; the formulas are given next to each frame.

**What this document does not do.** It does not run a real `nala` binary, does not render the full-screen
TUI byte-for-byte, and does not cover non-English locales (English-only is an approved N/A on the map). Those
gaps are listed again in §12.

---

## 1. Executive summary — the dozen facts that matter

1. **Section rules are ASCII `=` (U+003D), width = terminal columns.** `let sep = "=".repeat(width)` with
   `width = rust_apt::util::terminal_width()` (ioctl → `COLUMNS` → 80). There is no `═` (U+2550) anywhere
   in v0.18.2; the map's “`══════` section rules” wording does not match the tagged source (see §11.1).
   `src/summary.rs:150-151`, `src/summary.rs:154-162`.
2. **Tables are borderless, full-width, ASCII-free.** `comfy_table` with preset `NOTHING`,
   `ContentArrangement::DynamicFullWidth`, header cells painted `Highlight`, **only the last column**
   (Size) right-aligned. `src/table.rs:5-18`.
3. **Section headers are `Highlight` (bold default foreground), not the operation colour.** Every operation
   uses the same header style. `src/summary.rs:155`, operation colouring lives on the package-name cell.
4. **Operation → theme role:** Remove/AutoRemove/Purge/AutoPurge → `Error`; Install/Upgrade → `Secondary`;
   ReInstall/Downgrade/Configure/Held → `Notice`. So in v0.18.2 **installs are bold light-blue, not green**.
   `src/libnala/transaction.rs:80-88`.
5. **Inline table cells are almost entirely uncoloured.** Only the package name (operation role) and the
   changed components of the new version (`Notice`) carry ANSI. Old version, reason and size are emitted raw;
   their “Regular” style exists only in the full-screen TUI. `src/tui/summary/row.rs:121-148`.
6. **Version-diff emphasis:** the new version is compared component-wise (`-` revision first, then `.`
   segments); everything from the first differing dotted component onward is wrapped in `Notice`, the rest is
   plain. Revision-only bumps colour just the revision. `src/util/version.rs:3-41`.
7. **Summary footer counts are uncoloured** (`Upgrade 3`), the `Summary` heading is `Highlight`, then a blank
   line, `Total download size:` (only when > 0), `Disk space required:`/`Disk space to free:`, a blank line,
   then the prompt. `src/summary.rs:160-175`, `src/summary.rs:69-102`.
8. **Prompt:** `Do you want to continue? [Y/n] ` on **stdout**, no colour, no trailing newline, default yes.
   `src/util/prompt.rs:18-39`, `locales/en-US/main.ftl:22-26`.
9. **Progress widget is a rounded box, 80 columns max, left-aligned, height 6–12 lines depending on view and
   mirror count.** Title is centred `Highlight` inside 2-space padding; a `├ ┤` divider carries a centred
   `Progress` label and a right-aligned ` NN% `; the bar is `━` (U+2501) across the full inner width, split
   into `ProgressFilled` and `ProgressUnfilled` **by colour only** — the glyph is identical on both sides.
   `src/tui/progress.rs:19-30,134-165,481-523`.
10. **There is no spinner** anywhere in v0.18.2. `src/tui/progress.rs` draws no animated glyph; plain mode
    draws `[====…]`.
11. **Installation finishes silently:** the widget is cleared (`clean_up` → cursor to origin + clear-down) and
    no completion line is printed. `src/dpkg.rs:169-171`. The only completion lines are
    `Fetched … in … (…/s)` for `update` and `Downloads Complete:` for `download`.
12. **`NO_COLOR` as an environment variable is not implemented.** Only `--no-color`, `--color
    always|never|auto` and `[Color] mode` exist (`src/config/configuration.rs:205-214`); `TERM=dumb`
    disables the *UI*, not colour (`src/terminal.rs:26-54`, `src/progress.rs:280-289`).

---

## 2. Colour system

### 2.1 Theme roles, defaults, exact bytes

Nine roles (`src/config/color.rs:547-558`). Defaults are the `ThemePalette` constructors
(`src/config/color.rs:588-604`) and match `nala.conf:52-61`. A `Style` is `{fg, bg?, modifier}`, where the
default modifier is **BOLD** and `ThemePalette::default_regular()` is explicitly `no_bold`
(`src/config/color.rs:492-541`).

| Role | Default style (nala.conf:52-61) | `Style::ansi_prefix()` bytes | Reset | Visible as |
|---|---|---|---|---|
| `Primary` | `fg=BrightGreen` (bold by default) | `ESC[1;92m` | `ESC[0m` | bold bright green |
| `Secondary` | `fg=BrightBlue` | `ESC[1;94m` | `ESC[0m` | bold bright blue |
| `Highlight` | `fg=Reset` + bold | `ESC[1;39m` | `ESC[0m` | bold, terminal default colour |
| `Regular` | `fg=Reset`, `modifier=""` | `ESC[0;39m` | `ESC[0m` | no attributes, default fg |
| `ProgressFilled` | `fg=BrightGreen` | `ESC[1;92m` | `ESC[0m` | bold bright green |
| `ProgressUnfilled` | `fg=BrightRed` | `ESC[1;91m` | `ESC[0m` | bold bright red |
| `Notice` | `fg=BrightYellow` | `ESC[1;93m` | `ESC[0m` | bold bright yellow |
| `Warning` | `fg=BrightYellow` | `ESC[1;93m` | `ESC[0m` | identical to Notice |
| `Error` | `fg=BrightRed` | `ESC[1;91m` | `ESC[0m` | bold bright red |

Notes that bite implementers:

- `Primary`/`Secondary`/`Progress*`/`Notice`/`Warning`/`Error` are **bold by default** because `Style`'s
  `modifier` defaults to `bold()` (`src/config/color.rs:497-498`); `Regular` is the only role that is not.
- `Highlight`'s foreground is `Reset` (`ColorCode::Reset` → SGR 39), so its “colour” is the terminal default
  and only the bold attribute is visible. Themes that set a colour make section headers coloured.
- `Notice` and `Warning` are byte-identical under the default theme; they are distinct configuration keys, not
  distinct colours (`src/config/color.rs:600-602`).
- The `Regular` prefix is generated but **never emitted by any inline surface** in v0.18.2: outside the TUI,
  `Theme::Regular` is only used as a `ProgressMessage` theme (`src/progress.rs:42`), which is itself rendered by
  the TUI. Treat the row above as the byte pattern nala *would* emit for that role.
- Inline painting is `format!("{prefix}{string}ESC[0m")` (`src/config/color.rs:683-689`), i.e. every painted
  span resets at its end. `Style` display emits `ESC[<mods>;<fg>[;<bg>]m` with mods joined by `;`, or `0` when
  no modifier is set (`src/config/color.rs:510-536`).
- Modifiers: BOLD, DIM, ITALIC, UNDERLINED, SLOW_BLINK, RAPID_BLINK, REVERSED, HIDDEN, CROSSED_OUT
  (`src/config/color.rs:281-304`), configurable as `"BOLD | ITALIC"` strings or arrays
  (`src/config/color.rs:315-346`).
- Colours accept names (with Bright* aliases), 0–255 indexes and `#RRGGBB` (`src/config/color.rs:98-185`).

### 2.2 Where colour is decided (switch, target, mode)

- `Switch { Auto, Always, Never }` (`src/config/mod.rs:21-26`). `Auto` means *the target stream is a TTY*
  (`src/config/color.rs:662-668, 696-709`).
- Resolution: `--no-color` → `Never`; else CLI `--color` override; else `[Color] mode`
  (`src/config/configuration.rs:205-214`). Only CommandLine-sourced clap values are overrides, so the config
  file is respected when `--color` is at its clap default (`src/config/configuration.rs:55-60`).
- **Inline surfaces paint against stdout; log lines paint against stderr.** `color!` uses
  `Target::Stdout` (`src/config/color.rs:670-681`); the logger uses `Target::Stderr`
  (`src/config/logger.rs:139-150`).
- **The TUI does not emit ANSI strings.** It converts the theme `Style` into a `ratatui::style::Style`
  (`src/tui/style.rs:6-71`); with `Switch::Never` every role becomes `RatStyle::default()`, i.e. no
  foreground and no modifiers (`src/tui/style.rs:53-56`).
- **No `NO_COLOR` / `CLICOLOR` / `FORCE_COLOR` env support** — the only env lookups in the colour/terminal
  path are `TERM` (`src/terminal.rs:46-54`) and `TERM` again in plain progress (`src/progress.rs:280-289`).

### 2.3 Role → element map (inline surfaces)

| Surface / element | Role |
|---|---|
| `=` section rule | none (plain `println!`) |
| Section label, e.g. ` Upgrade` | `Highlight` (`src/summary.rs:155`) |
| Table header cells (`Package:` …) | `Highlight` (`src/table.rs:10`) |
| Package-name cell | operation role: Remove-ish → `Error`; Install/Upgrade → `Secondary`; Reinstall/Downgrade/Configure/Held → `Notice` (`src/libnala/transaction.rs:80-88`, `src/tui/summary/row.rs:126`) |
| Old version cell | none (raw string) |
| New version, changed components only | `Notice` (`src/util/version.rs:9,35`) |
| Reason cell (`Kept back`, `Manual hold`, `Excluded`, `Phased …%`) | none |
| Size cell, count lines, size lines | none |
| ` Summary` heading | `Highlight` |
| `Total download size:` / `Disk space required:` / `Disk space to free:` | none |
| Prompt `Do you want to continue? [Y/n] ` | none |
| `Fetched … in … (…/s)` (update completion) | `Highlight` (`src/cmd/update.rs:129`) |
| `Downloads Complete:` | none (`src/download/downloader.rs:61`) |
| `  <pkg> was written to <path>` | `Primary` on both names only (`src/download/downloader.rs:62-72`) |
| `Notice:` / `Warning:` / `Error:` log prefixes | `Notice` / `Warning` / `Error` (`src/config/logger.rs:96-120,139-150`) |
| `nala fetch` mirror-score message | `Finished: ` header in `Primary` (default message theme), URL segment in `Regular` (`src/cmd/fetch/score.rs:44-48`) |

### 2.4 A mutation-derived caveat for custom themes

Because inline cells are mostly plain strings (§1.5), **an inline table does not apply `Regular` to
old-version/reason/size cells**. If a user theme gives `Regular` a background or modifier, the full-screen TUI
shows it but the inline table does not. Likewise:
`version_diff` inserts `Notice` spans that end in `ESC[0m`, so any plain separator character emitted between
two highlighted components (the `.` in `1.<N>3<reset>.<N>0-1<reset>`) is rendered with **terminal default**
rather than an enclosing `Regular` (`src/util/version.rs:22-41`). With the default theme both are invisible
because `Regular.fg == Reset`. §11.4 lists this as a decision for #45.

---

## 3. Inline full transaction summary

### 3.1 When it prints (decision tree)

`display_summary` (`src/summary.rs:16-41`):

1. `[Nala] simple = true` (or `--simple`) → **simple summary** (§3.12) + prompt; TUI is never considered.
2. Otherwise, if `Ui.mode == "Tui"` **and** neither `assume_yes` nor `assume_no` is set → full-screen summary
   TUI (`use_fullscreen_ui`, `src/summary.rs:27-36`). `Ui.mode` defaults to `Auto`, and `Auto` never enables
   fullscreen (`src/terminal.rs:38-54`; `nala.conf:23-25`). `--no-tui` turns `Tui` into `Auto`.
3. Otherwise → **full inline summary** + prompt (`src/summary.rs:38-40`).

Caller-side gates (`commit_with_display_rows`, `src/summary.rs:299-318`):

- No changes and no displayed rows → `Nothing to do.` (no summary at all).
- No changes but held/excluded rows exist → **read-only summary** (same renderer, no prompt).
- `--print-uris` → no summary, prints JSON URIs instead.
- `--download-only` → summary + prompt, then downloads, then exits before dpkg.

**Which operation sections appear:** `sorted_summary_sets` iterates `Operation::to_vec()` and keeps only
non-empty sets (`src/summary.rs:55-67`). The fixed order is
`Remove, AutoRemove, Purge, AutoPurge, Install, ReInstall, Upgrade, Downgrade, Configure, Held`
(`src/libnala/transaction.rs:24-37`). There is no separate “Kept Back” section in v0.18.2: kept-back,
excluded, manual-hold and phased packages all live in **Held** with a `Reason:` column (§3.7).

### 3.2 Anatomy

```
<"=" × terminal_width>          ← plain, no colour
" " + Highlight(op.label())     ← e.g. " Upgrade"
<"=" × terminal_width>
<comfy_table, full width>       ← header row + one row per package
… repeated per non-empty operation …
<"=" × terminal_width>
" " + Highlight("Summary")
<"=" × terminal_width>
" " + "<Operation> <count>"     ← uncoloured, one line per section
…
                            ← blank line (println!)
" Total download size: <size>"   ← only when download size > 0
" Disk space required: <size>"   (or " Disk space to free: <size>")
                            ← blank line (println!)
"Do you want to continue? [Y/n] " ← no newline; omitted under --assume-yes/--assume-no
```

Rule width comes from `rust_apt::util::terminal_width()`: ioctl on stdout, then stderr, then stdin; else
`COLUMNS`; else **80** (`rust-apt 0.11.3 src/util.rs:64-69`, fetched from crates.io; this function lives
outside the nala repo). Table width is independent: comfy-table queries `crossterm::terminal::size()` only if
stdout is a TTY, otherwise it falls back to content-natural widths (`comfy-table 7.2.2 src/table.rs:270-282`,
`src/utils/arrangement/mod.rs:15-38`).

### 3.3 Frame — full summary, 80-column TTY

Fixture: Remove 1, Install 1, Upgrade 3, Held 1 (one upgrade is exactly 1 MiB to show the `1024 KiB` quirk).
Colour roles: see legend after the frame.

```text
================================================================================
 Remove
================================================================================
 Package:                    Version:                                     Size: 
 oldpkg                      1.4.2-1                                      3 MiB 
================================================================================
 Install
================================================================================
 Package:                    Version:                                     Size: 
 hello                       2.10-3                                      54 KiB 
================================================================================
 Upgrade
================================================================================
 Package:             Old Version:         New Version:                   Size: 
 libssl3t64           3.5.1-1              3.5.4-1                        2 MiB 
 nala                 0.18.1-1             0.18.2-1                    1024 KiB 
 vim-runtime          9.1.1234-1           9.1.1300-1                     8 MiB 
================================================================================
 Held
================================================================================
 Package:                     Old Version:   New Version:   Reason:       Size: 
 linux-image-6.1.0-40-amd64   6.1.140-1      6.1.146-1      Kept back    41 MiB 
================================================================================
 Summary
================================================================================
 Remove 1
 Install 1
 Upgrade 3
 Held 1

 Total download size: 12 MiB
 Disk space required: 18 MiB

Do you want to continue? [Y/n] 

```

Colour map (line numbers are 1-based into the frame; “plain” = no escape sequences):

| Lines | Element | Role |
|---|---|---|
| 1,3,6,8,11,13,18,20,23,25 | `=` rules | plain |
| 2,7,12,19,24 | ` Remove` / ` Install` / ` Upgrade` / ` Held` / ` Summary` | `Highlight` (the escape wraps only the label; the leading space is plain, `src/summary.rs:155`) |
| 4,9,14,21 | header row (`Package:`, `Old Version:`, …) | each header cell `Highlight`; inter-column spaces plain; **trailing space plain** |
| 5,10,15,16,17,22 | package names | `Error` for `oldpkg`; `Secondary` for `hello`/`libssl3t64`/`nala`/`vim-runtime`; `Notice` for the held `linux-image…` name |
| 15,16,17 | new-version changed components | `Notice` on `4-1` / `2-1` / `1300-1`; the rest of the cell is plain |
| 22 | held versions + reason | plain (`6.1.140-1`, `6.1.146-1`, `Kept back`) |
| 26–29 | counts (`Remove 1` …) | plain |
| 31–32 | size lines | plain |
| 34 | prompt | plain |

Notes visible in the frame:

- **Trailing spaces:** every table row and the header row end with exactly one space (right padding of the
  last column). The labels/rules do not. This is `println!("{pkgs}")` using `Display`, not `trim_fmt`
  (`comfy-table 7.2.2 src/table.rs:48-50, 88-95`).
- **`1024 KiB`:** `nala` is exactly 1 MiB in the fixture; `UnitStr::str` only switches unit when
  `value > divisor`, so 1 MiB renders as `1024 KiB` (`src/util/units.rs:34-46`). This is upstream behaviour,
  not a fixture artefact.
- **What `Size:` means depends on the operation.** The cell is the transition's stored `size`
  (`src/tui/summary/row.rs:145`): archive/download size for install/reinstall (`after_version.size()`,
  `src/libnala/mod.rs:165`), candidate size for upgrade/downgrade (`src/libnala/mod.rs:230`) and held
  (`src/cmd/upgrade/held.rs:30`), but **installed size** (space freed) for remove/purge/configure
  (`src/libnala/mod.rs:201,259`). The header is `Size:` in every case, and the footer's “Total download size”
  comes from apt, not from summing this column.

### 3.4 Frame — same fixture at 100 columns

Only the table and rules widen; nothing else changes (the progress box caps at 80, the summary does not).

```text
====================================================================================================
 Remove
====================================================================================================
 Package:                           Version:                                                  Size: 
 oldpkg                             1.4.2-1                                                   3 MiB 
====================================================================================================
 Install
====================================================================================================
 Package:                          Version:                                                   Size: 
 hello                             2.10-3                                                    54 KiB 
====================================================================================================
 Upgrade
====================================================================================================
 Package:                  Old Version:              New Version:                             Size: 
 libssl3t64                3.5.1-1                   3.5.4-1                                  2 MiB 
 nala                      0.18.1-1                  0.18.2-1                              1024 KiB 
 vim-runtime               9.1.1234-1                9.1.1300-1                               8 MiB 
====================================================================================================
 Held
====================================================================================================
 Package:                         Old Version:       New Version:       Reason:               Size: 
 linux-image-6.1.0-40-amd64       6.1.140-1          6.1.146-1          Kept back            41 MiB 
====================================================================================================
 Summary
====================================================================================================
 Remove 1
 Install 1
 Upgrade 3
 Held 1

 Total download size: 12 MiB
 Disk space required: 18 MiB

Do you want to continue? [Y/n] 

```

### 3.5 Rules — glyph, width, placement

- Glyph: `=` (U+003D) repeated. `src/summary.rs:151`.
- Width: the full terminal width, including when piped (`COLUMNS`/80 fallback). No cap.
- Placement: before **each** section label, after each label, and twice around ` Summary`
  (`src/summary.rs:153-162`). No blank lines anywhere in the summary block.
- The section label line is `" {label}"` — one leading space, label painted `Highlight`, no trailing text.
  Table lines also start with one space (left padding), so labels and package names align at column 2.

### 3.6 Table mechanics (comfy-table 7.2.2)

Settings, all in `src/table.rs:5-18`:

```rust
Table::new()
  .load_preset(comfy_table::presets::NOTHING)   // no borders, no separators
  .set_content_arrangement(ContentArrangement::DynamicFullWidth)
  .set_header(headers.iter().map(|s| color::highlight!(s.as_ref())))
// then:
table.column_mut(headers.len()-1).unwrap().set_cell_alignment(CellAlignment::Right)
```

Consequences, each verified in the harness:

- **No vertical separators.** Columns are separated by the right padding of the left column + the delimiter
  space + the left padding of the right column = **2 spaces** when content fits, plus alignment padding.
  Line = `" "` + cell1 + gap + cell2 + … + cellN + `" "` (trailing).
  `comfy-table 7.2.2 src/utils/formatting/content_format.rs:263-322`, `presets.rs:154`.
- **Header cells are ordinary cells** for width purposes and participate in `column_max_content_widths`
  (`comfy-table 7.2.2 src/row.rs:60-70`). They wrap like data when squeezed (see §3.11).
- **Alignment:** cell alignment defaults to `None`, then column alignment, then `Left`
  (`comfy-table 7.2.2 src/utils/formatting/content_format.rs:279-303`). nala sets **only** the last column to
  `Right`. The per-item `Item::center`/`Item::right` alignments in `src/tui/summary/row.rs:129-145` are **lost
  on the inline path**: `table.add_rows(rows.iter().map(|row| row.items(config)))` passes `&Vec<Item>`, and
  comfy-table converts each `&Item` through its `Display` impl (`Row: From<T: Into<Cells>>`,
  `Cell: From<T: ToString>`, `comfy-table 7.2.2 src/cell.rs:150-154`, `src/row.rs:97-105`). The resulting cells
  have `alignment: None`. Net effect inline: package, old version, new version, reason are **left-aligned**;
  only size is right-aligned. The full-screen TUI uses `Item::get_cell()` and keeps centring
  (`src/tui/summary/mod.rs:112`, `src/tui/summary/row.rs:48-54`) — the two surfaces genuinely differ.
- **Full width:** when stdout is a TTY and the terminal is wider than the minimum, `DynamicFullWidth` spreads
  all leftover columns across visible columns; the integer remainder goes left-to-right
  (`comfy-table 7.2.2 src/utils/arrangement/dynamic.rs:491-520`). Hence the table line length equals the
  terminal width (80/100 in the frames) and the left-most flexible column absorbs +1 of remainder.
- **Wrapping, no ellipsis:** content wider than its column is split on `' '` and, for a single long token,
  hard-split at the column width. Row height grows; no `…` appears because nala never sets `row.max_height`
  (the truncation indicator only applies with `max_height`:
  `comfy-table 7.2.2 src/utils/formatting/content_format.rs:100-130`).
- **ANSI-aware measurement:** nala enables comfy-table's `custom_styling` feature (`Cargo.toml:37`), so widths
  are measured after stripping ANSI
  (`comfy-table 7.2.2 src/utils/formatting/content_split/custom_styling.rs:9-11`). Colours never shift columns.
- **Piped/non-TTY:** `Table::width()` returns `None` when stdout is not a TTY, so arrangement silently falls
  back to `Disabled` — natural widths, no wrapping, no full-width expansion
  (`comfy-table 7.2.2 src/table.rs:270-282`, `src/utils/arrangement/mod.rs:29-38`). See §6.
- **Shared factory:** `table::get_table` is also used by `nala history` list/detail
  (`src/cmd/history/view.rs:26-44,113-118`), but history frames use a different header line
  (`"{op} ({count})"`, uncoloured) and no `=` rules. Do not generalise the transaction summary framing to
  history.

### 3.7 Column sets per operation

Derived from `SummaryRow::headers()` (`src/tui/summary/row.rs:93-119`) using
`display_old_version()` = “both versions exist **and differ**” (`row.rs:83-91`) and
`held_reason.is_some()`:

| Row shape | Columns (left → right) |
|---|---|
| versions differ, no reason (Upgrade, Downgrade; usually `ReInstall` no) | `Package:`, `Old Version:`, `New Version:`, `Size:` |
| versions differ + reason (Held because a candidate exists) | `Package:`, `Old Version:`, `New Version:`, `Reason:`, `Size:` |
| no version change, reason (e.g. held with before == after) | `Package:`, `Version:`, `Reason:`, `Size:` |
| plain install/remove/configure | `Package:`, `Version:`, `Size:` |

Critical implementation detail: **the header set is taken from the first row of the section only**
(`src/summary.rs:143-144`). If an adapter allows a Held section whose first row has no version change but a
later row does (or vice versa), the columns will be wrong and cells will shift. nala's producers are
homogeneous — a Held row carries `before`/`after` taken from an upgradable snapshot, so the versions normally
differ (`src/cmd/upgrade/held.rs:32-67`, `src/libnala/transaction.rs:193-205`); an xbps adapter must guarantee
the same or normalise the section.

Cell contents:

- Package: the **transition name**. nala passes the package name (`PackageTransition.name`); for library
  consumers it is whatever the producer stored (`src/tui/summary/row.rs:126`).
- Versions: `after.version_str().or(before.version_str())`; literal `Unknown` when neither exists
  (`row.rs:76-81,131-139`, `locales/en-US/main.ftl:5`).
- Reason: `HeldReason::summary()` → `Excluded`, `Manual hold`, `Phased {n}%`, `Phased`, `Kept back`
  (`src/libnala/transaction.rs:99-110`, `locales/en-US/main.ftl:57-64`).
- Size: `config.unit_str(package.size)` (download size, §3.9).

### 3.8 Version-diff emphasis (upgrades)

Algorithm (`src/util/version.rs:3-41`), reproduced exactly:

1. Split old and new at the **last** `-` (Debian-style revision). If both split and the prefixes are equal,
   return `"<prefix>-" + Notice("<new revision>")`. Example: `3.3.14-1 → 3.3.14-2` renders `3.3.14-` plain +
   `2` in `Notice`.
2. Otherwise split both at `.`; find the first index where old/new components differ (or where old runs out).
3. Emit new components joined by `.`; components at or after that index are wrapped in `Notice`, earlier ones
   are plain. Example `1.2.3-1 → 1.3.0-1` → `1.` + Notice(`3`) + `.` + Notice(`0-1`).
4. The **old** version is never emphasised; it is a raw string.
5. If `new` has fewer components than `old`, the loop breaks and `start_color` stays 0 → every new component is
   highlighted (edge case, not exercised by nala's own commands).

Raw bytes for the second example (from the harness, `cat -v`; `^[` = ESC):

```text
 ^[[1;94mcalamares^[[0m          3.3.14-1              3.3.14-^[[1;93m2^[[0m                     1024 KiB 
 ^[[1;94mlibfoo^[[0m             1.2.3-1               1.^[[1;93m3^[[0m.^[[1;93m0-1^[[0m                         2 MiB 
```

Read this closely: `1.` is plain, `3` is `ESC[1;93m…ESC[0m`, then a **plain** `.` (the Notice span closed with
its own reset, so the dot is terminal-default rather than any enclosing style), then `0-1` highlighted. Only the name and the Notice spans carry escapes; old version, size and
padding do not. Stripped frame (`Package:`-style headers `Highlight`):

```text
 Package:           Old Version:          New Version:                    Size: 
 calamares          3.3.14-1              3.3.14-2                     1024 KiB 
 libfoo             1.2.3-1               1.3.0-1                         2 MiB 
```

### 3.9 Sizes, units and counts

- `UnitStr::str` (`src/util/units.rs:24-47`): Binary by default (`nala.conf:28`), precision **0** for the
  summary (`UnitStr::new(0, ui.unit)`, `src/config/file.rs:117`), powers TiB/GiB/MiB/KiB, strict `>`, fallback
  `"{bytes} B"`.
- `Total download size:` prints only when `cache.depcache().download_size() > 0`; the disk line always prints:
  `Disk space required:` for `DiskSpace::Require`, `Disk space to free:` for `DiskSpace::Free`
  (`src/summary.rs:69-102`, `locales/en-US/main.ftl:69-75`).
- Counts: `" {label} {count}"` — **no colon** and no colour in full mode
  (`locales/en-US/main.ftl:76`, `src/summary.rs:164-173`). Simple mode uses
  `"{label}: {count}"` with the label highlighted (`locales/en-US/main.ftl:77`, §3.12).

### 3.10 Footer block + prompt

After the last section's table there is **no blank line**; the rule for ` Summary` follows immediately. Layout
of the footer is in §3.2. The prompt is written with `print!`, not `println!`:

- Text: `Do you want to continue? [Y/n] ` (`locales/en-US/main.ftl:24`, `src/util/prompt.rs:27-30`).
- Default yes; the displayed keys derive from `prompt-choice = Y/y N/n` and are validated so a bad translation
  falls back to `y/n` (`src/util/prompt.rs:41-60`).
- Accepted answers: empty → default; first character `y`/`n` (case-insensitive) wins; also the translated
  keys; anything else is an error `'<response>' is not a valid response`
  (`src/util/prompt.rs:70-87`, `locales/en-US/main.ftl:22`).
- Refusal (`n`) aborts with `User refused confirmation` printed as an `Error:` line on stderr by the top-level
  handler (`src/util/prompt.rs:9-15`, `src/summary.rs:316-318`, `src/main.rs:57-70`).
- `--assume-yes`/`--assume-no` short-circuit the prompt (`src/util/prompt.rs:19-25`); the summary is still
  printed in inline mode, and the full-screen TUI is skipped entirely when either flag is set
  (`src/summary.rs:27-30`).

### 3.11 Squeezed-terminal behaviour (long names)

There is no minimum width and no truncation. At 50 columns headers word-wrap and a long package name hard-wraps
over three lines; row height grows and there is still no separator between logical rows:

```text
==================================================
 Remove
==================================================
 Package:          Version:                 Size: 
 oldpkg            1.4.2-1                  3 MiB 
==================================================
 Install
==================================================
 Package:          Version:                 Size: 
 hello             2.10-3                  54 KiB 
==================================================
 Upgrade
==================================================
 Package:     Old Version:  New             Size: 
                            Version:              
 libssl3t64   3.5.1-1       3.5.4-1         2 MiB 
 nala         0.18.1-1      0.18.2-1     1024 KiB 
 vim-runtime  9.1.1234-1    9.1.1300-1      8 MiB 
==================================================
 Held
==================================================
 Package:   Old        New       Reason:    Size: 
            Version:   Version:                   
 linux-ima  6.1.140-1  6.1.146-  Kept      41 MiB 
 ge-6.1.0-             1         back             
 40-amd64                                         
==================================================
 Summary
==================================================
 Remove 1
 Install 1
 Upgrade 3
 Held 1

 Total download size: 12 MiB
 Disk space required: 18 MiB

Do you want to continue? [Y/n] 

```

At 40 columns headers split mid-word (`Versio` / `n:`). Reproduce with the harness; the exact split points are
comfy-table's `split_line` with `MIN_FREE_CHARS = 2`
(`comfy-table 7.2.2 src/utils/formatting/content_split/mod.rs:73-90,148`).

### 3.12 Simple summary (`simple = true` / `--simple`)

`print_simple_summary` (`src/summary.rs:104-134`):

- One line per non-empty operation: `"{Highlight(label)}: {count}"` — **no leading space**, label highlighted.
- One indented line: two spaces + package names joined by `", "`; held packages append `" ({reason})"`.
- No rules, no `Summary` block, no `Old Version`/`New Version` diffing.
- Then the standard size block and prompt.

```text
Remove: 1
  oldpkg
Install: 1
  hello
Upgrade: 3
  libssl3t64, nala, vim-runtime
Held: 1
  linux-image-6.1.0-40-amd64 (Kept back)

 Total download size: 12 MiB
 Disk space required: 18 MiB

Do you want to continue? [Y/n] 
```

Roles: the four `Xxx:` labels are `Highlight`; package lists, counts and size lines are plain.

### 3.13 `Nothing to do.` and read-only summary

- Both `pkgs` and `pkg_set` empty → `Nothing to do.` alone, no rules, no prompt (`src/summary.rs:299-313`).
- `pkgs` empty but `pkg_set` non-empty (e.g. `nala upgrade` where everything is held) → read-only summary:
  same full/simple renderer, **no prompt** (`src/summary.rs:43-53,300-304`).
- `nala install`, `nala upgrade`, `nala remove` (and the `download` commit path) all funnel through
  `commit_with_display_rows` (`src/summary.rs:267-318`); the summary is not printed when `--print-uris` is set.

---

## 4. Live progress widget (inline Ratatui viewport)

### 4.1 Gating

- A view is “enhanced” unless `Ui.mode == "Plain"` and provided stdout is a TTY and `TERM != dumb`
  (`src/terminal.rs:26-54`). `Ui.mode` defaults to `Auto`, which *does* allow the inline widget; only `Plain`
  forbids it (`nala.conf:23-25`).
- The widget is a Ratatui `Viewport::Inline(lines)` owned by `InlineTerminalGuard`, which also enables raw mode
  (`src/terminal.rs:107-187`). It is anchored to the current cursor row on creation and occupies exactly
  `lines` rows (`ratatui-core 0.1.2 src/terminal/init.rs:122-145`).
- `Progress::new` picks the TUI renderer or `PlainProgress` (`src/progress.rs:390-401`). If a TUI render call
  fails, it permanently downgrades to plain (`switch_to_plain`, `src/progress.rs:497-538`).

### 4.2 Geometry — exact heights

`ProgressView::viewport_lines()` (`src/progress.rs:62-85`):

| View | Command(s) | Height formula | Examples |
|---|---|---|---|
| `Update` | `nala update` | 7 | 7 |
| `Install` | dpkg phase of install/upgrade/remove | 6 | 6 |
| `MirrorScore` | `nala fetch` | 6 | 6 |
| `Download { mirrors: k }` | archive download for install/upgrade/download | `7 + (k>0) + min(k,3) + (k>3)` | k=0→7, k=1→9, k=3→11, k≥4→12 |

The height is set **once** at creation; resize keeps it
(`ratatui-core 0.1.2 src/terminal/resize.rs:24-72`). The upstream tests pin these values
(`src/progress.rs:658-664`).

Internal layout (anchor frame width `W = min(terminal_cols, 80)`):

- Outer box: `W` columns × height above. `MAX_VIEW_WIDTH = 80`; area is clamped left-aligned
  (`src/tui/progress.rs:19-20,140`; test `wide_terminal_keeps_the_progress_view_left_aligned_and_eighty_columns_wide`,
  `src/progress.rs:721-732`).
- Inner width: `W − 2` borders − 2 horizontal padding = **`W − 4`** (`Block::bordered()` +
  `Padding::horizontal(1)`, `src/tui/progress.rs:142-144`).
- Download inner: mirror block (`1 + min(k,3) + (k>3)` rows), then message row (1), then progress body (4):
  divider (1), bar (1), info (2).
- Update inner: message (1) + progress body (4).
- Install / MirrorScore inner: message (1) + progress body (3): divider, bar, **one** info row.

### 4.3 Frame — Download, one mirror (80 columns)

Fixture from `src/progress/preview.rs:51-71`: length 33 MiB, position 19 MiB, 3.5 s elapsed, items 43/80,
mirrors = `deb.debian.org 3/3 @12 MiB/s`.

```text
╭───────────────────────────  Downloading Packages  ───────────────────────────╮
│  Mirrors                       Connections     Average         State         │
│  deb.debian.org                3/3             12.0 MiB/s      downloading   │
│  Last completed: libssl3t64_3.5.1-1_arm64.deb                                │
├────────────────────────────────── Progress ───────────────────────────── 57% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Packages:   43/80                     Data:   19.0 MiB/33.0 MiB             │
│  Remaining:  3s                        Speed:  5.4 MiB/s                     │
╰──────────────────────────────────────────────────────────────────────────────╯
```

Column x-positions (0-based): mirror text starts at 3/33/49/65 for name/connections/average/state; info labels
at 3 (left) and 41 (right), values at 15 and 49. These follow from inner width 76 and
`[Min(16), 16, 16, 14]` when `inner >= 72`.

### 4.4 Frame — Download with mirror overflow (80 columns)

`k = 4` → height 12, three visible mirrors + one overflow line; header still 4 columns.

```text
╭───────────────────────────  Downloading Packages  ───────────────────────────╮
│  Mirrors                       Connections     Average         State         │
│  deb.debian.org                3/3             13.0 MiB/s      downloading   │
│  deb.volian.org                0/3             7.0 MiB/s       idle          │
│  mirror.example.net            1/3             —               starting      │
│  +1 more mirrors · 6 active connections                                      │
│  Last completed: libssl3t64_3.5.1-1_arm64.deb                                │
├────────────────────────────────── Progress ───────────────────────────── 57% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Packages:   43/80                     Data:   19.0 MiB/33.0 MiB             │
│  Remaining:  3s                        Speed:  5.4 MiB/s                     │
╰──────────────────────────────────────────────────────────────────────────────╯
```

Overflow line: `" +{hidden} more mirrors · {active} active connections"` in `Secondary`, where `hidden =
configured − 3` and `active` is the sum of active connections of **all** mirrors (including hidden ones)
(`src/tui/progress.rs:341-360`, `locales/en-US/main.ftl:253`). Note the `·` U+00B7.

### 4.5 Frame — Download, narrow terminal (60 columns)

The mirror table switches from 4 to 3 columns when **inner width < 72**, i.e. `min(cols,80) < 76`
(`src/tui/progress.rs:19,363-382`). At 60 the “Average” column disappears; titles/labels are clipped, not
wrapped.

```text
╭─────────────────  Downloading Packages  ─────────────────╮
│  Mirrors                            Active  State        │
│  deb.debian.org                     3/3     downloading  │
│  deb.volian.org                     0/3     idle         │
│  mirror.example.net                 1/3     starting     │
│  +1 more mirrors · 6 active connections                  │
│  Last completed: libssl3t64_3.5.1-1_arm64.deb            │
├──────────────────────── Progress ─────────────────── 57% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Packages:   43/80           Data:   19.0 MiB/33.0 MiB   │
│  Remaining:  3s              Speed:  5.4 MiB/s           │
╰──────────────────────────────────────────────────────────╯
```

### 4.6 Frame — Install (80 columns)

`src/progress/preview.rs:155-164`: length 100, position 43, 7.5 s elapsed, item `libssl3t64:arm64`,
message `Status: ` + `Configuring libssl3t64:arm64`.

```text
╭───────────────────────────  Installing Packages  ────────────────────────────╮
│  Status: Configuring libssl3t64:arm64                                        │
├────────────────────────────────── Progress ───────────────────────────── 43% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Package:  libssl3t64:arm64            Elapsed:  8s                          │
╰──────────────────────────────────────────────────────────────────────────────╯
```

`Status: ` comes from `format!("{}: ", t!("progress-status"))`; the status text is the verbatim dpkg status
payload (`pmstatus:<pkg>:<pct>:<text>` → `<text>`) (`src/dpkg.rs:322-357,509-538`). Message header theme is
`Primary`, or `Error` when the dpkg record type is `pmerror` (`src/dpkg.rs:344-355`).

### 4.7 Frame — Update (80 columns)

Production message is the first apt-pulse string, styled **`Regular`** via `.regular()`
(`src/cmd/update.rs:99-107`): `<Downloading|Processing|subprocess>: <reconstructed URI>`
(`src/cmd/update.rs:297-341`, `locales/en-US/main.ftl:499-503`).

```text
╭──────────────────────────  Updating Package Lists  ──────────────────────────╮
│  Downloading: https://deb.debian.org/debian/dists/stable/InRelease           │
├────────────────────────────────── Progress ───────────────────────────── 57% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Data:       19.0 MiB/33.0 MiB         Speed:    5.4 MiB/s                   │
│  Remaining:  3s                        Elapsed:  4s                          │
╰──────────────────────────────────────────────────────────────────────────────╯
```

Note the header `Downloading: ` is `Regular`, not `Primary`: the update command calls `.regular()` on the
apt-pulse message (`src/cmd/update.rs:103-105`). Before any pulse with an active worker, the message row shows
the default ` Status: — ` (label `Primary`, em dash `Regular`; `—` = U+2014, `src/tui/progress.rs:302-311`).

### 4.8 Frame — Mirror scoring (80 columns)

Production message: `Finished: ` + URL (`src/cmd/fetch/score.rs:41-49`, `locales/en-US/main.ftl:236`).

```text
╭─────────────────────────────  Testing Mirrors  ──────────────────────────────╮
│  Finished: https://deb.debian.org/debian                                     │
├────────────────────────────────── Progress ───────────────────────────── 58% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Mirrors:  7/12                        Elapsed:  4s                          │
╰──────────────────────────────────────────────────────────────────────────────╯
```

### 4.9 Element-by-element specification

All from `src/tui/progress.rs`; role names are the TUI mappings (`src/tui/style.rs:53-75`), which are the same
role colours as the inline table.

| Element | Glyph / format | Role | Source |
|---|---|---|---|
| Box corners/edges | `╭ ╮ ╰ ╯` + `─` (`BorderType::Rounded`) | `Primary` | `:142-145` |
| Title | `"  {title}  "` centred, one space each side of the title text (total 2 leading / 2 trailing) | `Highlight` | `:146-153` |
| Title texts | `Updating Package Lists`, `Downloading Packages`, `Installing Packages`, `Testing Mirrors` | — | `:125-132`, locale `:237-240` |
| Divider | `├` + `─` fill + centred `" Progress "` + right `" {pct}% "` + `─` fill + `┤`, drawn on the **full frame width** (ignores horizontal padding) | border `Primary`, both titles `Highlight` | `:481-502` |
| Bar | `━` × `filled` + `━` × `unfilled`; `filled = round(inner_width × ratio)`, `unfilled = inner_width − filled` | first span `ProgressFilled`, second `ProgressUnfilled` | `:504-523` |
| Bar row framing | `│` + one blank padding column + bar (`inner_width`) + one blank + `│` | padding blank uncoloured | `:455-479` |
| Info label | leading raw space, `"{label}:"` then spaces so all values in a column align (max label width + 2) | `Primary` | `:538-559` |
| Info value | raw text | `Regular` | `:553-557` |
| Info columns | two 50 % halves of the inner width | — | `:525-536` |
| Mirror header labels | `" {label}"` (leading space styled) | `Primary` | `:384-408` |
| Mirror row name / connections / average | `" {text}"` | `Regular` | `:410-453` |
| Mirror state word | `starting` / `downloading` / `idle` | `Notice` / `Primary` / `Secondary` | `:424-445` |
| Mirror overflow line | `" +n more mirrors · m active connections"` | `Secondary` | `:341-360` |
| Message line (with message) | raw space + header span + segment spans | header = `ProgressMessage.theme` (default `Primary`, `.regular()` where the caller asks, `Error` for dpkg errors); segments `Regular` | `:110-123,291-319`, `src/progress.rs:29-59` |
| Message line (no message) | `" {default_label}: "` + `—`; labels are `Last completed` (download), `Status` (update/install/mirror) | label `Primary`; dash `Regular` | `:302-317` |
| Scrolling messages above the box | plain text (may contain ANSI from the producer), wrapped to frame width, left aligned | `Regular` base style | `:65-85` |

Geometry facts that are easy to get wrong:

- The bar fills the **inner width**, not the outer width: `W − 4` glyphs. At 80 → 76 `━`; at 60 → 56.
- The bar row therefore looks like `│` + space + 76×`━` + space + `│`; the padding columns are never painted.
- The divider is the only row drawn at full frame width (ignores padding).
- `percent = (ratio * 100.0) as u64` — **truncation**, while `filled` uses **round**. At 19/33:
  `percent = 57`, `filled = round(76 × 0.5757) = 44`.
- The bar has **no unfilled glyph distinction**; only colour separates filled and unfilled. A monochrome or
  `Never`-colour run shows a solid line.
- Titles/labels are **clipped, not wrapped**, when the terminal is too narrow (Paragraph without `Wrap`).

### 4.10 Number and time formatting (authoritative)

| Quantity | Formula | Example | Source |
|---|---|---|---|
| Percent (divider, plain line) | `(ratio*100.0) as u64` (truncate) | 57 % | `src/tui/progress.rs:482`, `src/progress.rs:303` |
| Bar fill | `round(width × ratio)`, clamped | 44 / 76 | `src/tui/progress.rs:509-511`, `src/progress.rs:294-296` |
| Ratio | `position / length`, clamped to 1, 0 when length 0 | 0.5757 | `src/progress.rs:252-257` |
| Rate | `ceil(transferred / elapsed_seconds)` | 5.4 MiB/s | `src/progress.rs:231-237` |
| ETA | `ceil((length − position) / rate)`; `None` if position 0 or done or rate 0 → `—` | 3 s | `src/progress.rs:239-250` |
| Elapsed | `ceil(elapsed_seconds)` (so sub-second is `1s`) | 4 s | `src/progress.rs:229` |
| `Data`/`total` (transfer views) | `unit_str(position) + "/" + unit_str(length)` | `19.0 MiB/33.0 MiB` | `src/progress.rs:217-227` |
| `Data`/`total` (install/score) | `"{position}/{length}"` integers | `7/12` | same |
| Units in the widget | always **Binary**, precision **1** (`UnitStr::new(1, NumSys::Binary)`), independent of `[Ui] unit` | `19.0 MiB`; exactly 1 MiB → `1024.0 KiB` | `src/progress.rs:166`, `src/util/units.rs:27-47` |
| Time strings | `>1d`: `Xd Xh Xmin Xs`; `>1h`: `Xh Xmin Xs`; `>60s`: `Xmin Xs`; else `Xs` (strict `>`) | `3s`, `1min 4s` | `rust-apt 0.11.3 src/util.rs:145-167` |

Note the unit inconsistency: the **summary** uses `[Ui] unit` with precision 0, the **widget** hard-codes Binary
with precision 1. Exactly 1 MiB in the widget renders `1024.0 KiB`, not `1.0 MiB`, because the unit switch uses
a strict `>` and the 1-decimal format is applied after the unit is chosen.

### 4.11 Live-update protocol

- Download: `set_items(0,total)` + `set_mirrors(...)` at start; every 150 ms tick re-reads channel messages
  (`inc`, `inc_cached`, `dec`, `Message::Finished` → message `Last completed: <file>`, `Message::AddTotal`),
  updates items/mirrors, then `render()` (`src/download/downloader.rs:340-421`).
- Update: apt pulses push `UpdatePosition` (bytes) and message strings; `render()` per message or pulse
  (`src/cmd/update.rs:81-127`).
- Install: dpkg status-fd lines set position/item/message and call `render()` per line
  (`src/dpkg.rs:322-357`).
- Mirror score: one `inc(1)` + `render()` per completed mirror, message `Finished: <url>`
  (`src/cmd/fetch/score.rs:41-56`).
- There is **no spinner, no elapsed ticker, and no SIGWINCH handler**; updates are event/tick driven, resize is
  picked up on the next draw (§9).
- Messages that must persist are printed above the box via `terminal.insert_before(...)`, styled `Regular` and
  wrapped (`src/tui/progress.rs:65-85`). In plain mode they are `eprintln!`ed (§5).
- During a conffile prompt (`pmconffile`) or when a child takes the terminal, the widget is hidden/suspended and
  restored (`hide`/`suspend`/`resume`, `src/progress.rs:443-495`, `src/dpkg.rs:335-340`).
- `q`/`Ctrl‑C` polled between frames → cleanup + `Exiting at user request`
  (`src/terminal.rs:56-69`, `locales/en-US/main.ftl:176`).

### 4.12 Finish / cleanup — no completion line

`run_install` sets position = length, renders once (100 %), then `clean_up()` (`src/dpkg.rs:169-171`), which for
the TUI renderer calls `InlineTerminalGuard::suspend()` → move cursor to the viewport origin, clear from cursor
down (`ClearType::AfterCursor`), restore cursor, then write `\r` (`src/terminal.rs:151-193`, `src/tui/progress.rs:61`).
**Nothing is printed to mark completion of the install phase.** The last screen content is the dpkg status
lines that were inserted above the box. In plain mode the line is erased with `\r ESC[2K`
(`src/progress.rs:346-353`).

The only transaction-related completion lines are:

| Command | Line | Role | Source |
|---|---|---|---|
| `nala update` | `Fetched {size} in {time} ({rate}/s)` or `Nothing to fetch` (when `length <= 1`) | `Highlight` | `src/progress.rs:259-271`, `src/cmd/update.rs:129`, locale `:259` |
| `nala download` | `Downloads Complete:` then `  {package} was written to {path}` | plain; names `Primary` | `src/download/downloader.rs:61-72`, locale `:168,187` |
| install/upgrade/remove | *(none)* | — | `src/dpkg.rs:169-171` |

Debian-specific `check_reboot_required` notices (`Info:` lines) exist after a commit
(`src/summary.rs:402-418`) but have no Void analogue.

---

## 5. Plain progress (`Ui.mode = "Plain"`, non-TTY, or TUI render failure)

`PlainProgress` (`src/progress.rs:274-377`) writes **one line to stderr**, redrawn in place with
`\r ESC[2K` only when **stderr is a terminal and `TERM != dumb`**
(`src/progress.rs:280-289,346-353,367-376`).

### 5.1 Line grammar

```
"[" + "=" × filled + " " × (40 − filled) + "]" + " " + "%3d" + "% " + message [+ " " + total + " " + rate]
                                                             [+ " | Mirrors: " + total]        (score view)
                                                             [+ " | Package: " + item]         (if item set)
                                                             [+ " | Packages: " + "cur/tot"]   (if items set)
```

- Fixed bar width **40** (`WIDTH = 40`), `filled = round(40 × ratio)`; the percent is truncated
  (`src/progress.rs:291-304`).
- Default message when none is set: `Working...` (`src/progress.rs:305-307`, locale `:224`).
- Transfer views (Update/Download) append `" {position}/{length} {rate}/s"` **after** the message
  (`src/progress.rs:309-316`).
- Filtering (`src/progress.rs:324-341`): item → `" | Package: <item>"`, items → `" | Packages: cur/tot"`,
  score → `" | Mirrors: cur/tot"`.
- All numbers inside the line use the **widget's** `unit_str` (Binary, precision 1) because they come from
  `ProgressState`, not from `Config` (`src/progress.rs:215,299-344`).

### 5.2 Representative lines (stderr, ANSI-free)

Download, 19/33 MiB, 3.5 s, items 43/80, last completed file:

```text
[=======================                 ]  57% Last completed: libssl3t64_3.5.1-1_arm64.deb 19.0 MiB/33.0 MiB 5.4 MiB/s | Packages: 43/80
```

Install, 43 %, item + dpkg status:

```text
[=================                       ]  43% Status: Configuring libssl3t64:arm64 | Package: libssl3t64:arm64
```

Mirror score, 7 of 12:

```text
[=======================                 ]  58% Finished: https://deb.debian.org/debian | Mirrors: 7/12
```

No message yet:

```text
[                                        ]   0% Working...
```

The percent field is `%3d` (`" 57"`, `"  0"`, `"100"`), so there are two spaces between `]` and `57`.

### 5.3 When nothing is drawn

`render` returns early unless `interactive` (stderr TTY) and not hidden (`src/progress.rs:367-370`). Therefore:

- In plain mode, `nala … 2>log` (stderr redirected) → **no progress lines at all**; only `print()` messages,
  one line each.
- In plain mode, `nala … >log` with a TTY stderr → plain single-line progress on stderr (the enhanced UI
  requires **stdout** to be a TTY).
- With stdout a TTY the enhanced widget is used regardless of where stderr points, because it draws to stdout.
- `print(msg)` always goes through `clear_line()` then `eprintln!` (`src/progress.rs:355-365`), so messages are
  permanent and the live line is re-drawn afterwards on the next render.

### 5.4 Colour quirk in plain mode

Producer messages (dpkg `Unpacking`/`Setting up`, apt `Hit`/`Updated`) are painted with the `color!` macros,
which target **stdout** (`src/config/color.rs:670-681`). In plain mode they are written to **stderr**. Result:
when stdout is piped but stderr is a TTY, those status lines have no colour even though they are visible; when
stdout is a TTY and stderr is redirected, the opposite. nala does not correct for this.

---

## 6. Non-TTY and environment degradation matrix

| Condition | Summary | Progress | Colour |
|---|---|---|---|
| stdout TTY, stderr TTY, `TERM=xterm-256color`, `Ui.mode=Auto` | full inline tables at terminal width | inline rounded widget | full |
| stdout TTY, stderr redirected | as above | inline widget (writes to stdout) | full |
| stdout redirected, stderr TTY, `Auto` | tables at **natural width**, rules at `COLUMNS`/80 | **plain** line on stderr | stdout: none; stderr logs: yes (painted against stderr) |
| stdout and stderr redirected | natural-width tables | **no** progress output; only printed messages | none |
| `TERM=dumb` | tables unchanged | plain path; **no live redraw** (the `dumb` check disables the interactive plain line) | unchanged (`dumb` disables the UI, not colour) |
| `Ui.mode=Plain` | inline tables | plain path | full |
| `Ui.mode=Tui` | full-screen only when no assume flags and TTY | inline | full |
| `--no-color` / `[Color] mode="Never"` | inline tables uncoloured; **no escapes at all** | widget draws with default styles | none |
| `--color always` + pipe | ANSI present, column widths still correct (ANSI-aware measurement) | plain path, ANSI in messages | forced |
| `NO_COLOR=1` env | **ignored** | **ignored** | unchanged |
| all three std streams non-TTY, `COLUMNS=120` | rules 120 wide, tty-less table natural width | n/a (plain) | none |

Piped/non-TTY full summary frame (same fixture; note natural column widths and rules still 80):

```text
================================================================================
 Remove
================================================================================
 Package:  Version:  Size: 
 oldpkg    1.4.2-1   3 MiB 
================================================================================
 Install
================================================================================
 Package:  Version:   Size: 
 hello     2.10-3    54 KiB 
================================================================================
 Upgrade
================================================================================
 Package:     Old Version:  New Version:     Size: 
 libssl3t64   3.5.1-1       3.5.4-1          2 MiB 
 nala         0.18.1-1      0.18.2-1      1024 KiB 
 vim-runtime  9.1.1234-1    9.1.1300-1       8 MiB 
================================================================================
 Held
================================================================================
 Package:                    Old Version:  New Version:  Reason:     Size: 
 linux-image-6.1.0-40-amd64  6.1.140-1     6.1.146-1     Kept back  41 MiB 
================================================================================
 Summary
================================================================================
 Remove 1
 Install 1
 Upgrade 3
 Held 1

 Total download size: 12 MiB
 Disk space required: 18 MiB

Do you want to continue? [Y/n] 


```

Key observation: **the rules are still full width** (`terminal_width()` falls back to `COLUMNS`/80) while the
**table is not** (comfy-table has no width without a TTY). The two widths are computed independently.

---

## 7. Log, prompt and error lines (other inline surfaces)

- Log line format: `"{coloured prefix} {message}\n"` on **stderr**. Prefixes/labels:
  `Error:`, `Notice:`, `Warning:`, `Info:`, `Verbose:`, `Debug:`
  (`src/config/logger.rs:96-120,139-150`, `locales/en-US/main.ftl:10-15`).
- `info!` maps to `Level::Notice` → `Notice` theme → **yellow “Notice:”**, not blue
  (`src/config/logger.rs:42-46,110-120`).
- `warn!` → `Warning` theme (same default colour as Notice), `error!` → `Error`
  (`src/config/logger.rs:48-60`).
- Top-level error path: `error!("{err:?}")` for anyhow errors, and `ESC[1;91m Error: ESC[0m {err:?}` hard-coded
  for bootstrap failures — note the **hard-coded** escape that ignores the configured theme
  (`src/main.rs:44-70`).
- `apt`-style failure lines are stripped of `E: ` / `W: ` and re-emitted as Error/Warning log lines
  (`src/main.rs:57-66`).
- Under `--assume-yes` a downgrade or held-package change aborts with the locale messages
  `summary-downgrade-assume-yes` / `summary-held-assume-yes` (`src/summary.rs:235-261`).
- Protected/essential removal prints `Warning:` + indented names, then an `Error:` and
  `Use '<Notice-coloured switch>' if you are sure.` — the only `Warning`-role inline use besides the logger
  (`src/summary.rs:202-233`).

---

## 8. Full-screen TUI — where it diverges from inline (for #50)

The TUI is a separate render path. Only divergences matter here; the complete TUI design is #50's job.

| Aspect | Inline | Full-screen |
|---|---|---|
| Gate | any UI mode except `Plain`, stdout TTY | `Ui.mode = "Tui"` **and** no `--assume-yes`/`--assume-no` **and** stdout TTY (`src/summary.rs:27-36`, `src/terminal.rs:42-54`) |
| Frame | no box around the whole summary; rounded box only for progress | thick-bordered full-screen block (`BorderType::Thick`, `src/tui/summary/mod.rs:465-469`) with title `Nala Upgrade` (`tui-upgrade-title`, locale `:263`) |
| Section navigation | vertical sections, one table each | tabs across operations (`src/tui/summary/mod.rs:292-312`); `→`/`←`/`h`/`l` |
| Rows | static | selectable, scrollbar (`Primary` thumb, `Secondary` track), `j`/`k`, PgUp/PgDn/Home/End (`:82-148`) |
| Alignment | versions/reason left (alignment lost, §3.6) | versions/reason **centred**, size right (`row.rs:129-145`, `mod.rs:112`) |
| Colour application | ANSI strings; most cells uncoloured | ratatui cell styles per item; header row `Regular`; selected row whole-line `Primary`; help line `Secondary` (`mod.rs:83-121,450-453`) |
| Footer | size block + prompt | summary table `"<Op>:" "N Pkgs"` + optional download/disk rows, then a centred help line from `tui-help-*` (`mod.rs:26-32,377-455`, locale `:266-270`) |
| Confirmation | typed prompt | `(q) quit \| (y) start upgrade` in the help line; keys handled in `run` (`mod.rs:326-340`) |
| Progress | same widget renderer | same renderer; the fullscreen overlays are for **fetch** mirror selection and **release-info** changes only |
| Release info changes | modal prompt via `confirm_with_default(..., false)` → `[y/N]` | ratatui modal; border `Notice`, labels `Primary`, old value `Secondary`, new value `Highlight` (`src/tui/release_info.rs:36-75`) |
| Fetch mirror selection | ✗ | separate TUI: header block `Primary`/`Highlight`, row highlight `Secondary.reversed()` (`src/tui/fetch.rs:49-50,185-211`) |

Implications for #50: the TUI table is **not** the inline table with a box; it is a different widget with
different alignment and styling. Build it as a separate view over the same `SummaryRow`/`Operation` model,
and keep the `Item` alignment/style fields for the TUI (they are dead weight inline).

---

## 9. Resize behaviour

- **Summary:** widths are computed at render time, once per summary print. There is no live redraw. If the
  terminal is resized after printing, earlier lines keep their old width (`src/summary.rs:150`,
  `src/table.rs:5-18`).
- **Progress widget:** Ratatui `Viewport::Inline(height)`; `Terminal::draw` calls `autoresize`, so a size change
  is applied on the **next draw** (next 150 ms tick / dpkg line / pulse). Height stays fixed; width follows the
  new terminal size, still capped at 80 (`ratatui-core 0.1.2 src/terminal/resize.rs:64-72`,
  `src/progress.rs:62-85`, `src/tui/progress.rs:140`).
- On **horizontal shrink** ratatui clears the whole screen region before redrawing to avoid line-wrap artefacts
  (`ratatui-core 0.1.2 src/terminal/resize.rs:45-50`).
- There is **no SIGWINCH handler** in nala; the earlier xbpretty behaviour of reacting immediately to SIGWINCH
  is an extension, not parity.
- If the terminal becomes shorter than the viewport, ratatui recomputes the inline origin and may scroll the
  terminal to keep the fixed-height viewport visible (`ratatui-core 0.1.2 src/terminal/init.rs:118-133`).

---

## 10. English strings inventory (authoritative text)

From `locales/en-US/main.ftl` (v0.18.2):

| Key | String | Line |
|---|---|---|
| `prompt-continue` | `Do you want to continue?` | 24 |
| `prompt-choice` | `Y/y N/n` (rendered as `[Y/n]`) | 26 |
| `summary-title` | `Summary` | 68 |
| `summary-total-download-value` | `Total download size: { $size }` | 73 |
| `summary-disk-required-value` | `Disk space required: { $size }` | 74 |
| `summary-disk-free-value` | `Disk space to free: { $size }` | 75 |
| `summary-op-count` | `{ $operation } { $count }` | 76 |
| `summary-op-count-colon` | `{ $operation }: { $count }` | 77 |
| `summary-nothing` | `Nothing to do.` | 79 |
| `table-package` … `table-size` | `Package:` `Version:` `Old Version:` `New Version:` `Reason:` `Size:` | 98–103 |
| `op-remove` … `op-held` | `Remove` `AutoRemove` `Purge` `AutoPurge` `Install` `ReInstall` `Upgrade` `Downgrade` `Configure` `Held` | 46–55 |
| `reason-*` | `Excluded` `Manual hold` `Phased {n}%` `Phased` `Kept back` | 57–64 |
| `progress-label` | `Progress` | 226 |
| `progress-data` / `progress-packages` / `progress-package` | `Data` / `Packages` / `Package` | 228–230 |
| `progress-speed` / `progress-elapsed` / `progress-remaining` | `Speed` / `Elapsed` / `Remaining` | 233–235 |
| `progress-update-title` … `progress-score-title` | `Updating Package Lists` / `Downloading Packages` / `Installing Packages` / `Testing Mirrors` | 237–240 |
| `progress-last-completed` | `Last completed` | 241 |
| `progress-connections` / `progress-average` / `progress-active` / `progress-state` | `Connections` / `Average` / `Active` / `State` | 242–245 |
| `progress-starting` / `progress-downloading` / `progress-idle` | `starting` / `downloading` / `idle` | 246–248 |
| `progress-more-mirrors` | `+{ $mirrors } more mirrors · { $connections } active connections` | 253 |
| `progress-fetched` | `Fetched { $size } in { $time } ({ $rate }/s)` | 259 |
| `progress-nothing` | `Nothing to fetch` | 225 |
| `progress-working` | `Working...` | 224 |
| `download-complete` | `Downloads Complete:` | 168 |
| `download-written` | `{ $package } was written to { $path }` | 187 |
| `download-exit` | `Exiting at user request` | 176 |
| `log-error` / `log-notice` / `log-warning` / `log-info` | `Error:` / `Notice:` / `Warning:` / `Info:` | 10–13 |
| `tui-upgrade-title` / `tui-help-*` | `Nala Upgrade` / `(↑) move up \| (↓) move down` … | 263, 266–270 |

Note `progress-score-title` is `Testing Mirrors` while the message label inside `nala fetch` is `Finished: `
(`progress-finished`, line 236) — the preview fixture uses `Testing: ` but production uses `Finished: `.

---

## 11. Downstream implications

### 11.1 Corrections to existing xbpretty documents

The map (#37) and `docs/research/tui-rendering-architecture.md` contain claims that v0.18.2 does not support.
They should be corrected in spec rev 6:

| Prior claim (mostly `docs/research/tui-rendering-architecture.md`) | Actual v0.18.2 | Evidence |
|---|---|---|
| Section rules `═` (U+2550) “or ASCII” (doc line 55) | ASCII `=` (U+003D) only, width = terminal columns | `src/summary.rs:151` |
| Installing names bold **green** `ESC[1;32m` (doc line 65) | Install and Upgrade both use `Secondary` = bold **bright blue** `ESC[1;94m` | `src/libnala/transaction.rs:83-84`, `src/config/color.rs:590` |
| “Kept Back / Hold” separate section | No such operation; `Held` section (yellow names) with a `Reason:` column | `src/libnala/transaction.rs:10-21,92-110` |
| Progress title embedded in the top-left border (doc line 119) | Title is **centred** in the top border, `"  {title}  "` | `src/tui/progress.rs:146-153` |
| Progress rows `Total Packages` / `Last Completed` / `Time Remaining` (doc lines 121–124) | Rows are `Mirrors/Connections/Average/State` and `Packages/Data/Remaining/Speed` (or `Package/Elapsed`) | §4.9 |
| Spinner / animated status (doc lines 136, 147, 178) | None in v0.18.2 | `src/tui/progress.rs` (no spinner code) |
| Version columns centred inline | Centre alignment is lost on the inline path; only Size is right-aligned | §3.6 |
| `NO_COLOR` env var | Not implemented | §2.2 |
| Progress border “bold green `ESC[1;32m`” (doc line 118) | `Primary` = bold **bright green `ESC[1;92m`** | `src/config/color.rs:588`, §2.1 |
| Bar unfilled glyph `─` (xbpretty `internal/cli/progress.go:44-52`) | Both bar halves are `━`; colour splits them | `src/tui/progress.rs:512-521` |

The map's “borderless aligned tables” and “rounded box only for the live progress widget” are correct.

### 11.2 What #45 must settle (normative baseline above)

1. Adopt ASCII `=` rules and the exact rule-placement (no blank lines) or record a deliberate deviation.
2. Adopt the role map in §2.3 (including Install/Upgrade = `Secondary`, Held = `Notice`).
3. Decide whether to replicate the **inline alignment loss** (left-aligned versions) or fix it. Parity says
   replicate; a deliberate superset is defensible but must be documented.
4. Decide on the **unstyled cell** behaviour when custom themes set a `Regular` background (§2.4).
5. Drop the spinner from xbpretty's progress widget (or record it as an xbpretty extension). nala has none.
6. Change the bar unfilled glyph to `━` (colour-only split) and the cap/geometry (80 max, `W−4` bar).
7. Keep the no-completion-line-after-install behaviour, or add a completion line as a **documented extension**.
8. Decide on `NO_COLOR` env support as an extension (nala ignores it).
9. Size formatting: summary 0 decimals / configured unit; progress 1 decimal / always binary. Decide whether
   to keep this inconsistency (parity) or normalise (deviation).
10. Prompt bytes: `Do you want to continue? [Y/n] ` on stdout, no newline, default yes.

### 11.3 What #50 needs from this document

- TUI summary is a **different visual system**: thick borders, tabs, selectable rows, footer `N Pkgs`, help
  line; inline is borderless tables + rules. Do not try to share a single renderer without separating layout.
- TUI keeps per-cell alignment; inline loses it. The `Item` type already carries what both need
  (`src/tui/summary/row.rs:22-59`); the inline path converts through `Display`, which is the bug/feature.
- TUI colour application is style-based (`src/tui/style.rs`), so `Switch::Never` must map every role to
  `RatStyle::default()` rather than “no ANSI string”.
- The progress widget renderer is shared; the fullscreen views (fetch, release-info) are separate.
- Key bindings and help strings are in `locales/en-US/main.ftl:266-270` and
  `src/tui/summary/mod.rs:296-340`.

### 11.4 xbps adaptations and N/A (for the parity matrix)

- **Rules/table/summary/prompt:** directly portable; xbpretty has no apt `DiskSpace` API — compute
  `Disk space required/free` from xbps transaction figures or record a documented N/A/adapter rule.
- **Held:** xbps reports holds via `xbps-pkgdb`/hold state and upgrade dry-run; the `Reason:` values map as:
  hold → `Manual hold`, `--exclude`/protected → `Excluded`, remaining upgradable-but-not-selected → `Kept
  back`. `Phased` has no xbps analogue (N/A).
- **Operation labels:** xbps actions are `install`, `update`, `remove`, `configure`, `hold`, `reinstall`
  (`internal/summary/summary.go:11-19`); map `update→Upgrade`, `reinstall→ReInstall`, keep the rest. There is
  no `Purge`/`AutoPurge`/`AutoRemove` in xbps; record N/A (or map autoremove if xbps grows it).
- **Progress views:** xbps has no apt acquire/dpkg status fd; map download → `Download`, unpack/configure
  callbacks → `Install`, `xbps-install -S` index sync → `Update`, mirror benchmarking (if implemented) →
  `MirrorScore`. The widget only needs `{length, position, items, item, message, mirrors}` (§4.10).
- **Update completion line:** `Fetched …` has an xbps analogue from fetched bytes + elapsed; keep the
  `Highlight` style.
- **Release-info modal / reboot notices / dpkg pty:** Debian-only → N/A.

---

## 12. Gaps and limitations

1. **No real nala execution.** The environment has no `libapt-pkg`, so frames were produced by re-executing the
   render paths with the same crate versions, not by capturing a real `nala` run. The progress frames validate
   against upstream test assertions (§0), and the table frames use the exact library and settings, but a
   real-terminal capture on Debian would be a useful cross-check.
2. **Full-screen TUI frames are not byte-rendered.** §8 specifies structure/roles from source; #50 should
   render it and add frames.
3. **Non-English locales** are out of scope (map decision: English-only). Any locale that changes
   `prompt-choice`/labels could change layout.
4. **`time_str` and `terminal_width` live in rust-apt 0.11.3**, not in the nala tag; cited from the crates.io
   tarball. `time_str` boundaries are strict `>` (60 s, 1 h, 1 d).
5. **Two independent width sources** (rust-apt ioctl/COLUMNS vs crossterm size) can disagree in exotic setups;
   reasoned from source, not empirically forced.
6. **Live values** (rate/ETA/elapsed) are timing-dependent; frames use fixture timings and the formulas are
   given so any value can be recomputed.
7. **Broken pipe** behaviour when stdout closes mid-summary was not investigated (nala uses `println!`, so it
   panics on EPIPE — Rust's default — worth checking before claiming pipeline parity; ADR-0005 territory).

---

## Appendix A — raw bytes (`cat -v`, `^[` = ESC)

Full-summary upgrade section at 80 columns (same fixture):

```text
 ^[[1;39mPackage:^[[0m             ^[[1;39mOld Version:^[[0m         ^[[1;39mNew Version:^[[0m                   ^[[1;39mSize:^[[0m 
 ^[[1;94mlibssl3t64^[[0m           3.5.1-1              3.5.^[[1;93m4-1^[[0m                        2 MiB 
 ^[[1;94mnala^[[0m                 0.18.1-1             0.18.^[[1;93m2-1^[[0m                    1024 KiB 
```

Observations:

- Header cells are `ESC[1;39m…ESC[0m` each; the spaces between them are plain.
- Package names `ESC[1;94m…ESC[0m` (Secondary).
- Old version, unchanged new-version prefix, size: **no escapes at all**.
- Changed new-version suffix `ESC[1;93m…ESC[0m` (Notice).
- Line ends with a single plain space (right padding).

Section header line bytes:

```text
================================================================================
 ^[[1;39mUpgrade^[[0m
================================================================================
```

Progress widget output is drawn by ratatui's crossterm backend as cursor-addressed SGR sequences; both the
harness and the reader inspect the rendered `Buffer`, not the escape stream. The colour spans are listed in
§4.9; byte-level escape capture is terminal-dependent and deliberately not quoted here.

---

## Appendix B — reproduction recipe

To regenerate any frame (throwaway; nothing was added to the xbpretty tree):

1. `cargo new` a crate with `comfy-table = { version = "=7.2.2", features = ["tty","custom_styling"] }` and
   `ratatui = { version = "=0.30.2", default-features = false, features = ["crossterm_0_29"] }`.
2. For summary frames: build a table exactly as in `src/table.rs:5-18`, add rows whose cell strings are
   `paint(op_theme, name)`, `old_version`, `version_diff(old,new)`, `reason`, `size` (in that order, per §3.7),
   then `set_width(cols)` to emulate a TTY of that width, or `force_no_tty()` for piped mode; print with
   `println!("{table}")` (not `trim_fmt`).
3. For progress frames: port `render_progress_view` + sub-renderers from `src/tui/progress.rs` verbatim, and
   `ProgressState`/`ProgressMessage`/`MirrorProgress` from `src/progress.rs`; render into
   `Buffer::empty(Rect::new(0,0,width,view.viewport_lines()))`; dump each row by concatenating
   `buf.cell((x,y)).symbol()` and `trim_end()`.
4. Use the fixtures in `src/progress/preview.rs:51-81` for values; they are the upstream gallery inputs.
5. Validate against the render assertions in `src/progress.rs:587-760`: the download frame (587), height table
   (658), mirror state (666), narrow layout (696), wide-terminal width (721) and install frame (734) tests were
   reproduced with this port; the wide-terminal test reduces to "the widget is 80 cells wide and cell (80,0) is
   an untouched space", which the port also yields.

`cargo` environment used: rustc 1.99.0, crates fetched from crates.io; `comfy-table 7.2.2` and
`ratatui 0.30.2` were already pinned by nala (`Cargo.toml:22,37`).

---

## Appendix C — citation index

| Path | Covers |
|---|---|
| `src/summary.rs` | when summary prints, full/simple/read-only renderers, rules, counts, sizes, footer, prompt wiring, completion notices |
| `src/table.rs` | single table factory, preset, arrangement, header colour, last-column right |
| `src/tui/summary/row.rs` | column sets, cell contents, operation colour, version diff, TUI cell alignment/style |
| `src/tui/summary/mod.rs` | TUI layout, tabs, selection, footer, thick borders, help |
| `src/progress.rs` | state model, view heights, plain renderer, `finished_string`, TUI/plain dispatch, fallback, tests |
| `src/tui/progress.rs` | widget geometry, titles, divider, bar, info rows, mirror table, roles |
| `src/progress/preview.rs` | authoritative fixtures (manual gallery test) |
| `src/config/color.rs` | roles, defaults, style/anSI encoding, switch/target, ansi→text |
| `src/config/mod.rs` | `Switch` enum |
| `src/config/configuration.rs` | `color_mode`, `ui_mode`, CLI overrides |
| `src/config/file.rs` | `UiMode`, config defaults |
| `src/config/logger.rs` | log line format and role mapping |
| `src/terminal.rs` | UI gating (`TERM`, TTY), inline viewport, cleanup, alternate screen |
| `src/util/prompt.rs` | prompt bytes, defaults, validation |
| `src/util/version.rs` | version-diff algorithm |
| `src/util/units.rs` | size formatting |
| `src/libnala/transaction.rs` | operation order/labels/roles, held reasons |
| `src/dpkg.rs` | install-phase status parsing and message construction, cleanup |
| `src/download/downloader.rs` | download phase progress updates and completion lines |
| `src/cmd/update.rs` | update view messages, finished line, acquire progress |
| `src/cmd/fetch/score.rs` | mirror-score view messages |
| `nala.conf` | default config incl. theme values |
| `locales/en-US/main.ftl` | all English strings |
| `Cargo.toml` | pinned crate versions |

External primary sources: [comfy-table 7.2.2](https://docs.rs/comfy-table/7.2.2) (preset/width/alignment/ANSI
mechanics), [ratatui 0.30.2](https://docs.rs/ratatui/0.30.2) (inline viewport, resize, symbols),
[rust-apt 0.11.3](https://docs.rs/rust-apt/0.11.3) (`terminal_width`, `time_str`).

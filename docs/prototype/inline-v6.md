# Prototype: nala-faithful inline rendering for xbps (#51)

**Status:** throwaway prototype, branch `prototype/nala-inline-v6`, never merged.
**Question it answers:** how should nala-faithful inline rendering look and
behave on real xbps data — and which of [#45](https://github.com/soubarnak/xbpretty/issues/45)'s
rendering rules survive into rev 6?

**What it is:** a single stdlib-only Go command under
`tools/prototype_inline_v6/` that renders

- the full inline summary (borderless tables, ASCII `=` rules, nine theme
  roles, version-diff spans, footer) at 80 and 100 columns,
- the same summary piped (natural widths), and the `--simple` layout,
- the five rounded progress views (Download, Install, Remove, Update,
  MirrorScore) at 80 and 60 columns, fed from a synthetic xbps telemetry
  stream through a small state machine,
- the 40-cell plain progress line,
- ANSI SGR bytes, plus a `--no-color` mode.

It is **not** wired into the CLI, has no config, no commands, no TUI, and no
tests. It reads real xbps plans read-only and never installs, removes or syncs.

## Run it

```console
$ go run ./tools/prototype_inline_v6                      # live plan, prints every frame
$ go run ./tools/prototype_inline_v6 -plan golden          # #45 §3.7 fixture, xbps versions
$ go run ./tools/prototype_inline_v6 -plan nala            # research #39 frame verbatim (Debian data)
$ go run ./tools/prototype_inline_v6 -plan live -out testdata/prototype/inline/live -width 80,100
$ go run ./tools/prototype_inline_v6 -plan live -simple -out testdata/prototype/inline/live-simple
$ go run ./tools/prototype_inline_v6 -check                # version-diff byte cases
```

`-plan live` runs, read-only:

- `xbps-install -n figlet cowsay` — Install rows;
- `xbps-install -n -u` — Upgrade rows, with the old version and installed size
  from `xbps-query -p pkgver,installed_size`;
- `xbps-remove -n nano` — Remove rows;
- `testdata/parser/0607/hold.txt` — Held rows (this host has no holds of its
  own), with the installed version looked up on the host.

`-plan nala` exists only to diff the renderer against nala's golden frame:
same cell contents in, same bytes out. It is not an xbps plan.

## Frames

`testdata/prototype/inline/{live,nala,simple,live-simple}/` holds every frame
twice: `.sgr` with the exact ANSI bytes and `.txt` with ANSI stripped.

### Live 80-column summary (host's real dry runs)

```text
================================================================================
 Remove
================================================================================
 Package:                    Version:                                     Size: 
 nano                        9.2_1                                        3 MiB 
================================================================================
 Install
================================================================================
 Package:                    Version:                                     Size: 
 figlet                      2.2.5_6                                     92 KiB 
 cowsay                      3.8.4_1                                     19 KiB 
================================================================================
 Upgrade
================================================================================
 Package:           Old Version:           New Version:                   Size: 
 voiced             1.2.0_1                1.3.0_1                       12 MiB 
================================================================================
 Held
================================================================================
 Package:            Version:            Reason:                          Size: 
 acl                 2.4.0_1             Manual hold                     20 KiB 
================================================================================
 Summary
================================================================================
 Remove 1
 Install 2
 Upgrade 1
 Held 1

 Total download size: 12 MiB
 Disk space to free: 2 MiB

```

The Upgrade cell's raw bytes (from `live/summary-80.sgr`) are
`1.` + `ESC[1;93m3ESC[0m` + `.` + `ESC[1;93m0_1ESC[0m`, i.e. the revision rides
inside the highlighted tail, exactly [#45 §3.4](https://github.com/soubarnak/xbpretty/issues/45).

### Download view, 80 columns

```text
╭───────────────────────────  Downloading Packages  ───────────────────────────╮
│  Last completed: glibc-2.41_1.x86_64.xbps                                    │
├────────────────────────────────── Progress ───────────────────────────── 57% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Packages:   43/80                     Data:   19.0 MiB/33.0 MiB             │
│  Remaining:  3s                        Speed:  5.4 MiB/s                     │
╰──────────────────────────────────────────────────────────────────────────────╯
```

### `--simple` (live plan)

```text
Remove: 1
  nano
Install: 2
  figlet, cowsay
Upgrade: 1
  voiced
Held: 1
  acl (Manual hold)

 Total download size: 12 MiB
 Disk space to free: 2 MiB

```

### Plain line (40 cells, stderr in the real design)

```text
[=======================                 ]  57% Last completed: glibc-2.41_1.x86_64.xbps 19.0 MiB/33.0 MiB 5.4 MiB/s | Packages: 43/80
```

## Byte-level comparison with nala's frames

| Frame | Reference | Result |
|---|---|---|
| Full summary, 80 cols | research #39 §3.3 (33 lines incl. footer blank) | **identical**, every line |
| Full summary, 100 cols | research #39 §3.4 | **identical**, every line |
| Full summary, piped | research #39 §6 (natural widths, 80-col rules) | **identical**, every line |
| Upgrade section SGR | research #39 Appendix A raw bytes | **identical**, all three lines |
| `--simple` | research #39 §3.12 | identical except the prompt line, which the prototype does not print (prompt is stderr, #45 §5) |
| Download view, 80 cols | #45 §6.4 / research §4.3 without the mirror table | **identical** after stripping ANSI, all 7 rows |
| Download view, 60 cols | research #39 §4.5, non-mirror rows | **identical**, all six rows including the 56-cell bar and the divider |
| Install view, 80 cols | research §4.6 | identical geometry; status text differs by data (`Unpacking` vs nala's `Configuring`) |
| Plain line | research §5.2 | **identical** |
| Table engine itself | comfy-table 7.2.2, built locally and diffed | identical widths at 80 and 100 cols |

The nala frames were reproduced with the real `comfy-table 7.2.2` crate (the
exact version nala pins) via a throwaway Rust harness, so the 80/100-column
table geometry is checked against the library, not only against the research
transcript.

## Verdict — every #45 rendering rule exercised

| # | Rule (#45) | Status | Evidence |
|---|---|---|---|
| 1 | Section rule is ASCII `=` × terminal width, also when piped | **survives** | piped frame identical, rules still 80 |
| 2 | Rule placement: before/after every label, around ` Summary`, no blank lines inside, one blank after counts and sizes | **survives** | golden 80 frame line-for-line |
| 3 | Section label is `" " + Highlight(label)` | **survives** | SGR bytes `ESC[1;39m` per label |
| 4 | ` Summary` heading is plain (correction C1) | **survives** | no escapes on that line in `.sgr` |
| 5 | Borderless tables, NOTHING preset, 2-space gap, trailing space on every line | **survives** | trailing-space match vs research `cat -A` |
| 6 | DynamicFullWidth: full width on a TTY, natural width piped | **survives** | 80/100/piped frames; width algorithm reproduced from comfy-table 7.2.2 |
| 7 | Only the last column (Size) right-aligned | **survives** | 80/100 frames |
| 8 | Header cells painted `Highlight` individually, gaps plain | **survives** | Appendix A lines identical |
| 9 | Name roles: remove→`Error`, install/upgrade→`Secondary`, held/reinstall/configure→`Notice` | **survives** | `nano` `ESC[1;91m`, `figlet` `ESC[1;94m`, `acl` `ESC[1;93m` |
| 10 | Version diff: `Notice` on changed components, revision inside the last component, plain `.` between spans | **survives** (prototype) but **needs correction** in the current code | `-check` prints `3.5.` + Notice(`4_1`), `2.` + Notice(`39_2`), `1.5.7_` + Notice(`2`), `1.` + Notice(`3`) + `.` + Notice(`0_1`); `internal/summary.VersionDiff` emits ANSI 202 and splits `_rev` out (see corrections) |
| 11 | Old version raw; diff only on the new version | **survives** | Appendix A |
| 12 | Summary sizes: `[Ui] unit`, Binary default, precision 0, strict `>` so 1 MiB → `1024 KiB` | **survives** | golden Upgrade row `1024 KiB` |
| 13 | Footer counts: `" " + label + " " + count`, no colon, no colour | **survives** | golden frame |
| 14 | `Total download size:` only when > 0; disk line always, required/free by sign | **survives** (rendering); the golden frame's numbers are **not derivable** | see correction 1 |
| 15 | `--simple`: `Highlight(label) + ": " + count`, then two-space names, held names ` (reason)`, same size block | **survives** | identical to research §3.12 except the prompt |
| 16 | Widget `W = min(cols, 80)`, inner `W−4`, heights 7 (Download) / 6 (others) | **survives** | 80 and 60 frames |
| 17 | Title `"  {title}  "` centred in the top border | **survives** | all five views at both widths |
| 18 | Divider `├` + fill + centred `" Progress "` + right `" NN% "` + `┤`, drawn on the full frame width (ignoring padding) | **survives** | 80/60 matched; a first version using `W−4` was off by one — the full-width rule is load-bearing |
| 19 | Bar `━` × `(W−4)`, filled uses `round`, the two halves differ by colour only | **survives** | 44/76 at 57 %; 56 cells at 60 cols |
| 20 | Percent truncates (`(ratio*100)`) while fill rounds | **survives** | 57 % with 44/76 |
| 21 | Info rows: two 50 % halves, own `max(label)+2` alignment | **survives** | values at columns 15/49 (80) and 13/… (60), matching research |
| 22 | Widget units always Binary precision 1, independent of `[Ui] unit` (C3) | **survives** | `19.0 MiB/33.0 MiB` |
| 23 | Rate `ceil(position/elapsed)`, ETA `ceil((total−position)/rate)`, elapsed `ceil`, `time_str` | **survives** | `5.4 MiB/s`, `3s`, `8s` |
| 24 | Message row `" " + "{Header}: " + text`, header `Primary` except Update's `Regular` | **survives** | update `.sgr` paints `Updating:` with `ESC[0;39m` |
| 25 | Download view has no mirror table, height 7 (D9) | **survives** | frame is 7 rows |
| 26 | Update view adapted to `Repositories`/`Elapsed`, height 6 (D10) | **survives** | frame |
| 27 | Plain line grammar: 40 cells, `round` fill, truncated `%3d`, filters | **survives** | byte-identical to research §5.2 |
| 28 | `--no-color` emits no ANSI at all; bar becomes a solid `━` band | **survives** | `summary-80.nocolor.txt` contains no `ESC` |
| 29 | Wrapping on squeezed terminals: word-wrap at spaces then hard-split, no ellipsis | **needs correction** | a naive splitter does not reproduce nala at 50 columns; nala's exact widths depend on comfy-table's `optimize_space_after_split` |
| 30 | No spinner, no completion line, teardown clears | **not testable here** | prototype draws single frames only |
| 31 | Prompt bytes on stderr; hold-only plans skip the prompt | **not in prototype** | deliberately out of scope |

## Corrections proposed for rev 6

1. **#45 §3.7's golden footer is not derivable from its own table.** The
   research frame's footer (`Total download size: 12 MiB`,
   `Disk space required: 18 MiB`) comes from libapt's depcache, not from
   summing the `Size:` column (research #39 §3.9 says so explicitly). Under
   #45 §3.5's sum rule the same rows give 11 MiB and a negative delta. The
   prototype reproduces **both** byte sets (the frame with an explicit footer
   override, the live plan with computed totals), so this is a fixture problem,
   not a rendering one. Rev 6's goldens should either derive the footer from
   the rows or be labelled as nala-copied illustrative frames.
2. **The current `internal/summary.VersionDiff` contradicts #45 §3.4.** It
   emits ANSI 202 (`ESC[38;5;202m`) instead of `Notice` (`ESC[1;93m`), and it
   splits the revision off: `3.5.1_1→3.5.4_1` becomes `3.5.` + 202(`4`) +
   `_1` instead of `3.5.` + Notice(`4_1`); `2.38_1→2.39_2` becomes
   `2.` + 202(`39`) + `_` + 202(`2`) instead of `2.` + Notice(`39_2`). This is
   an implementation correction, not a change to #45.
3. **Squeezed-width parity needs the real comfy-table algorithm.** #45 §3.2
   says "word-wrapped on spaces and, for a single long token, hard-split", but
   nala's exact split points also depend on `optimize_space_after_split`
   (`comfy-table 7.2.2 utils/arrangement/dynamic.rs:377-437`). At 50 columns
   the naive splitter produces different column widths. Recommendation: either
   implement that optimisation in rev 6's table engine, or cap the visual
   goldens at ≥80 columns and record narrower terminals as best-effort.

## What survives into rev 6

Everything in the verdict table marked **survives**, with three notes:

- keep the divider's **full-frame-width** rule (`W−2`, not `W−4`) — it is easy
  to get wrong and the prototype caught itself doing so;
- keep `round` fill and truncated percent as **separate** formulas;
- add the two implementation corrections above (version-diff bytes, squeezed
  wrapping) and fix the golden-footer fixture, without changing #45's decisions.

## Known limitations

- Wrapping is plain-text only; a painted cell that exceeded its column would
  not wrap. It never happens in the 80/100-column frames.
- The widget is rendered as static frames, not driven by a terminal; there is
  no teardown, resize or persistent-message path.
- The install/remove/update/score views are fed from synthetic streams whose
  numbers were chosen to land on the researched values; only the download
  stream's position is derived from plan bytes and tick percent.
- The Hold fixture comes from another host's capture; this host has no holds.

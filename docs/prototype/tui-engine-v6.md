# Prototype: zero-dependency TUI engine for xbpretty (#52)

**Status:** throwaway prototype, branch `prototype/tui-engine-v6`, never merged.
**Question it answers:** is a zero-dependency full-screen TUI in Go faithful
enough to nala v0.18.2's shell to justify parity — and which of
[#50](https://github.com/soubarnak/xbpretty/issues/50)'s decisions survive into
rev 6?

**Answer:** **yes, with one correction and three clarifications.** The engine
pieces that #50 §8.10 called risky all work in the standard library: raw/cbreak
termios with restore on signal/panic, `?1049` alternate screen, a cell-diff
renderer, a width table, an input decoder, SIGWINCH, DSR with a working
no-anchor fallback, and — the hard part — an inline viewport that inserts
completed lines into scrollback and re-anchors beside a live child. The 80×24
operation-tabbed summary frame is **byte-identical to #50 §2.7**, not merely
similar.

## What is here

```
internal/term/                 stdlib-only engine (1,428 LOC)
  term_linux.go                TCGETS/TCSETS, TIOCGWINSZ, raw/cbreak, ANSI, DSR select
  guard.go                     ordered enter/restore guard, DSR query + parser
  screen.go                    cell buffer, dirty diff, SGR flush, no-color mode
  width.go                     wcwidth-style intervals (zero/wide, ambiguous=1)
  input.go                     UTF-8/CSI/SS3/SGR-mouse/bracketed-paste decoder
  viewport.go                  inline bottom-anchored box: insert_before, re-anchor, hide/show
tools/prototype_tui_v6/        surfaces + demo (1,738 LOC)
  runner.go                    surface loop: entry gates, SIGWINCH, keys, restore, frame dump
  summary.go                   operation tabs, table, footer, scrollbar, help wrap
  fetch.go                     single-select picker with adaptive columns
  inline.go                    progress box driven by a fake child process
  child.go                     fake telemetry child
  harness_test.go              18-test PTY harness (build, drive, capture, assert)
```

No `require` line was added (`go.mod` is still `module` + `go`), no cgo, no
dependency. `CGO_ENABLED=0 go build ./...` passes; `go vet` is clean.

## Run it

```console
$ go run ./tools/prototype_tui_v6                     # summary, live terminal
$ go run ./tools/prototype_tui_v6 -surface fetch      # mirror picker
$ go run ./tools/prototype_tui_v6 -surface history    # read-only detail
$ go run ./tools/prototype_tui_v6 -surface inline     # inline box + fake child
$ go run ./tools/prototype_tui_v6 -emit testdata/prototype/tui   # static frames
$ PROTO_CAPTURE=1 go test ./tools/prototype_tui_v6 -run TestCaptureFixtures
$ go test ./internal/term ./tools/prototype_tui_v6 -count=1
```

Keys: `j/k/↑/↓` rows, `h/l/←/→` tabs, `Home/End`, `PgUp/PgDn`, wheel, `y`
confirm, `q/Esc/n/Ctrl-C` cancel; fetch `j/k/g/G/Enter/q`, `Esc/Ctrl-C`
cancel; inline reads no keys. Ctrl-C is a signal in the inline viewport
(cbreak keeps `ISIG`) and exit 130.

## Frames

`testdata/prototype/tui/` holds the static frames (`.txt` grid + `.sgr`
exact bytes), the PTY-captured frames, and `pty-inline-sequence.txt`.

### Summary, 80×24 — identical to #50 §2.7

```text
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  xbpretty Upgrade  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ Upgrade                                                                      ┃
┃ ━━━━━━━                                                                      ┃
┃ ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓┏━┓ ┃
┃ ┃     Package:        Old Version:        New Version:         Size:    ┃┃█┃ ┃
┃ ┃❯    libfoo             1.2_1               1.3_1           120 KiB    ┃┃ ┃ ┃
┃ ┃     libbar             0.9_2               0.9_3            44 KiB    ┃┃ ┃ ┃
┃ ┃     libbaz             2.0_1               2.0_2             8 KiB    ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┃                                                                       ┃┃ ┃ ┃
┃ ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛┗━┛ ┃
┃ Upgrade:              3 Pkgs           (↑) move up | (↓) move down           ┃
┃                                      (→) next tab | (←) previous tab         ┃
┃ Total download size:12.4 MiB (s) package info | (q) quit | (y) start upgrade ┃
┃ Disk space required:45.0 MiB                                                 ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

The footer's no-gap quirk (`Total download size:12.4 MiB`), the 47-column help
block, the `❯` gutter and the `━` tab underline all fall out of #50's rules;
`TestSummaryGolden80x24` compares all 24 rows.

### Fetch, 80×24 — rows 0–6 identical to #50 §5.3

```text
╭──────────────────────────────  xbpretty Fetch  ──────────────────────────────╮
│  Mirrors:                             Region: Tier: Latency:    Speed: Score:│
│✓ https://repo.voidlinux.org/current   EU          1    18 ms 4.1 MiB/s     18│
│☐ https://mirror.example.net/voidlinux EU          2    31 ms 3.2 MiB/s     31│
│☐ https://mirror.osuosl.org/voidlinux  NA          2    47 ms 2.8 MiB/s     47│
│☐ https://slow.example.org/voidlinux   AS          3   112 ms 0.9 MiB/s    112│
│                                                                              │
│                                    …                                         │
│                                                                              │
│  Score is how many milliseconds it takes to download the repository index.   │
│                                                                              │
│                  Use ↓↑ to move, Home/End to go top/bottom,                  │
│                      q/Enter to select, Esc to cancel.                       │
╰──────────────────────────────────────────────────────────────────────────────╯
```

Rows 18–23 match #50's band placement (blank, info, blank, two help lines,
border). The `…` row is #50's abbreviation of the omitted rows, not literal
output.

### Inline viewport (7 rows, no alternate screen) — a captured box

```text
╭───────────────────────────  Downloading Packages  ───────────────────────────╮
│  Last completed: —                                                           │
├────────────────────────────────── Progress ────────────────────────────── 0% ┤
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│  Packages: 0/8                                           Data: 0 B/33.0 MiB  │
│  Remaining: 1s                                                 Speed: 0 B/s  │
╰──────────────────────────────────────────────────────────────────────────────╯
```

The box is an illustrative 7-row Download view; #45's exact 6/7-row placement
and spacing were proven by #51 and are not re-litigated here.

The captured scrollback sequence (`pty-inline-sequence.txt`) inserts six
completed lines **above** the box while the fake child keeps emitting
telemetry:

```text
INSERT  Last completed: libfoo-1.3_1.x86_64.xbps
INSERT  Last completed: libbar-0.9_3.x86_64.xbps
INSERT  Last completed: libbaz-2.0_2.x86_64.xbps
INSERT  Last completed: glibc-2.41_1.x86_64.xbps
INSERT  Last completed: figlet-2.2.5_6.x86_64.xbps
INSERT  Last completed: cowsay-3.8.4_1.x86_64.xbps
```

### Too small, 50×15

```text
┏━━━━━━━━━━━━━━  xbpretty Upgrade  ━━━━━━━━━━━━━━┓
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃ Terminal too small (60x18 minimum; now 50x15)  ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┃                                                ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

## Evidence

All of this is executable in `harness_test.go`; there are 18 tests (11 PTY, 6 golden/unit, 1 capture), 774 test LOC.

| Claim | Test | Observed |
|---|---|---|
| termios restored and alt screen left on normal exit | `TestSummaryPTYRestore` | `?1049h/?1049l`, `?25l/?25h`, mouse/paste on/off present; `TCGETS` flags before == after |
| raw entry really disables ISIG | same | `Lflag & ISIG == 0` while the screen is up |
| restore on SIGTERM | `TestSigtermRestore` | exit 143, `?1049l`, `?25h`, termios restored |
| restore on panic | `TestPanicRestore` | panic injection → exit 2, alt screen left, termios restored |
| every §8.6 key decoded | `TestKeysPTY` | arrows, `h/l`, Home/End, PgUp/PgDn, SGR wheel, paste |
| bracketed paste discarded | `TestKeysPTY` | `ESC[200~y ESC[201~` logs `paste-discarded`; exactly one real `rune:y` follows |
| SIGWINCH resize mid-frame | `TestResizePTY` | 80×24 → 100×30 → 50×15 (notice) → 80×24 (frame returns) |
| summary refuse exit 1 | `TestSummaryPTYRestore` | exit 1, refusal line |
| summary confirm exit 0 | `TestKeysPTY` | `y` confirms |
| fetch confirm/cancel | `TestFetchPTYConfirm/Cancel` | exit 0 + written line / exit 1 + cancel line |
| history read-only exit 0, no download row | `TestHistoryPTY` | frame title `xbpretty History`, no `Total download size` |
| DSR timeout falls back | `TestInlineViewportPTY` | `dsr=timeout fallback=no-anchor`, viewport still works |
| DSR reply path | `TestInlineDSRReply` | harness answers `ESC[12;5R`; trace `dsr=reply 12;5` |
| inline viewport: inserts, re-anchors, hides | `TestInlineViewportPTY` | 6 INSERTs, hide/show latch, **no** `?1049h`, cursor shown on teardown |
| Ctrl-C is a real signal in the viewport | `TestInlineCtrlCIsSignal` | exit 130, signal handler restored the screen |
| `--color never` has no SGR | `TestNoColorHasNoSGR` | regexp `ESC[…m` absent; `┏ ❯ ━ ┃` present; layout controls kept |
| width policy | `TestRuneWidth` | box glyphs/`✓☐❯` = 1, CJK = 2, combining/ZWJ/VS16 = 0 |
| cell diff | `TestBufferDiff` | identical frame emits nothing; one-cell change emits one move |

The harness never answers DSR unless a test explicitly does, so the fallback
is exercised on every default run — that is the point of #50 §8.3.

## Verdict — every #50 decision

| # | #50 decision | Verdict | Evidence / note |
|---|---|---|---|
| 1 | Three-layer gates (mode/terminal/command), stdin+UTF-8 added | **survives** | `runScreen` implements TERM=dumb and 60×18 entry gates; the remaining L3 cells are predicates over the plan, not terminal risk. PTY proves the tty/raw path. |
| 2 | Command-derived titles, lowercase brand | **survives** | `xbpretty Upgrade/Install/History/Fetch` frames. |
| 3 | Thick frame, centred title, 1-col padding | **survives** | 80×24 frame byte-identical. |
| 4 | Two-row tab strip + `━` underline | **survives** | frame; `l`/`→` moves tab, underline follows (single-tab fixture clamps). |
| 5 | Table width/SpaceAround/alignment | **survives** | golden columns at 80 and 100; deterministic formula reproduces #50 exactly. |
| 6 | Footer composition, no-gap quirk, 47-col help | **survives** | byte-identical at 80; wraps at 60. |
| 7 | Version diff as cell styles | **survives** | implemented as spans; #51 already proved the byte algorithm; no new engine risk. |
| 8 | Selection, scroll, scrollbar | **survives** | `❯` + whole-row Primary; wheel and PgUp/PgDn decoded; thumb formula needs the clamp in correction C2. |
| 9 | Confirm/cancel keys and exit codes | **survives** | exit 1 refuse, exit 0 `y`, exit 0 history, PTY-proven. |
| 10 | History detail = read-only summary | **survives** | frame + exit 0; footer without download/disk. |
| 11 | Package-info view (`s`) | **survives (not prototyped)** | a text dump in the same frame is the lowest-risk widget; `s` is decoded (trace). |
| 12 | Fetch picker layout/selection/keys | **needs adjustment** | layout and keys survive; #50's drop thresholds contradict its own golden frame (C1). Fixed fit-based rule matches the frame at 80/120 and stays legible at 60. |
| 13 | Release-info = N/A | **survives** | no code path exists; nothing in the engine tempts a modal. |
| 14 | Inline viewport: `insert_before`, re-anchor, cbreak+ISIG, no alt, no `q`, hide latch | **survives** | PTY: 6 inserts, hide/show, no `?1049h`, Ctrl-C → 130. The viewport never reads a key. |
| 15 | DSR optional, no-anchor fallback sufficient | **survives** | default harness never answers; all inline tests pass on the fallback; reply path separately proven. |
| 16 | Restore on normal exit, SIGINT/SIGTERM/HUP, panic | **survives** | termios flags compared before/after; alt screen left on all three exits. |
| 17 | SIGWINCH immediate repaint, 60×18 floor, notice, restore | **survives** | resize test above; clarify that the frame/title stay and only the body becomes the notice (C3). |
| 18 | Colour roles, `Never`/`NO_COLOR`, accessibility cues | **survives** | no SGR under `Never`, every glyph/control stays; `❯`/underline independent of colour. |
| 19 | Mouse wheel in S1/S2, bracketed paste discarded | **survives** | SGR mouse decoded; paste never reaches the key stream. |
| 20 | Width table policy (zero/wide/ambiguous=1) | **needs adjustment (scope)** | compact table is enough for every golden frame and CJK; production still needs the generated ~170-interval table for arbitrary package text. Low risk. |
| 21 | Linux build tags with non-Linux stubs | **not prototyped** | prototype is Linux-only; stubs are mechanical, no design risk. |
| 22 | Fallbacks and one-warning rule | **survives** | return paths exist for init failure, entry floor and `TERM=dumb`; the exact one-line strings are #9.1's job. |
| 23 | LOC 3.8k–5.6k prod | **survives (low end likely)** | 1,428 engine + 1,738 surfaces/demo = 3,166 for a subset; with info view, full gate matrix and production fallbacks, ~3.8k–5k is credible. |

**Nothing is abandoned.** The only thing that does not survive unchanged is
#50 §5.2's fetch drop table.

## Corrections for rev 6 (do not silently change #50)

1. **C1 — fetch drop thresholds contradict the golden frame.** #50 §5.2 drops
   `Location` below 120, `Region` below 100, `Tier` below 80 and `Speed` below
   90 **interior** columns; at the 80×24 golden frame the interior is 78, so
   all four would vanish, yet §5.3 shows Region/Tier/Latency/Speed/Score.
   Fix: show `Location` only from 120 columns, otherwise drop
   `Region → Tier → Speed` only while the URL cannot keep its 24-cell floor;
   `Latency` and `Score` are never dropped. With that rule the prototype
   reproduces §5.3 rows 0–6 exactly and remains legible at 60×18.
2. **C2 — scrollbar thumb formula vs the golden.** #50 §3.4 computes
   `max(1, (A−2)*visible/total)`; with 3 rows in a 13-row viewport that is 60
   cells, while §2.7 draws a one-cell thumb at the first interior row. Rev 6
   should clamp the thumb to the interior and define "all rows visible → a
   one-cell thumb at the top" (what the prototype does to match the frame).
3. **C3 — too-small wording.** §2.6 says "the body paints one centred line;
   the tab strip, table, footer, cursor and mouse are not drawn". The
   prototype keeps the border and title and replaces only the body; the test
   asserts the table/footer are gone. Make that the explicit golden.
4. **C4 — help wrapping at the floor.** §2.5 says help lines are wrapped,
   never truncated, but does not budget for the extra rows. At 60×18 the
   three 31/47-column lines wrap to five; the five-operation footer's `F=8`
   has room, and the prototype wraps into it. State the wrapped-line budget
   (or accept clipping) in rev 6.
5. **C5 — "child writes to the same tty".** The real architecture (and this
   prototype) has the parent read the child's output and insert messages; a
   child writing raw bytes directly to the shared tty cannot be re-anchored
   without a pty/pipe. The prototype's fake child writes telemetry on a pipe,
   which is the nala dpkg-pty shape. The `?1049h/l` hide latch is driven by the
   parent's view of that stream, not by sniffing arbitrary tty traffic.
6. **C6 — DSR is genuinely optional.** The default PTY harness answers nothing
   and every inline test still passes; the reply path works when answered.
   #50's "DSR is an optimisation" is confirmed, not assumed.

## What survives into rev 6

- The engine package split (`internal/term` for primitives, `internal/tui` for
  widgets/surfaces) and the exact primitives in #50 §8.3.
- The raw/cbreak guard with reverse-order restore on return, signal and panic.
- Cell-diff + width-aware flush, and `Never` as "no SGR, all glyphs".
- The inline viewport strategy: cbreak + ISIG, no alternate screen, no keys,
  `insert_before` as move-to-top → clear-down → message + redraw, hide/show
  latch, DSR as an optimisation with the no-anchor fallback.
- Operation tabs, footer composition and the `❯`/underline accessibility
  cues as specified (#50 §2–§4), with C2/C3/C4 clarifications.
- Fetch as a single-select picker with a fit-based column rule (C1).
- History detail as the same widget read-only.
- The PTY harness pattern (openPty + Setctxt + winsize + trace file + frame
  dump) as the model for rev 6's §14.2 interaction tests.
- The LOC estimate, at its low end.

## Known limitations (prototype only)

- The fake child is a pipe, not a real pty; nala's `?1049h/l` latch is
  simulated by a telemetry event rather than parsed from child bytes.
- `s` (package info) only traces; the view itself is not drawn.
- The fetch and summary fixtures are static data; gate wiring to real plans and
  flags belongs to rev 6.
- Scrollbar behaviour is exercised only for non-scrolling tables; the clamp
  path is written but not PTY-tested.
- The width table covers the glyph inventory and CJK, not the full Unicode
  interval set.
- Non-Linux stubs and the real CLI dispatch are out of scope here.

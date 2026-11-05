# ADR-0017: Full-screen surfaces, gates and the zero-dependency terminal engine

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-067–FR-072, AC-049–AC-055
- Amends: ADR-0005 (Tui-mode summary is the stated stdout exception)

## Context

nala has no four-tab shell: only the transaction summary is tabbed, progress is
an inline bottom viewport, and fetch and release-info are separate full-screen
apps. Reproducing that needs three alternate-screen surfaces, a single-select
picker, an inline viewport that co-exists with a child, and a raw-mode engine —
all with zero external modules, because NFR-001 forbids the Bubble Tea family
and terminfo.

## Decision

1. **Surfaces** — S1 transaction summary (opt-in), S2 history detail (opt-in,
   read-only), S3 fetch picker (enhanced gate, single-select), S4 inline
   progress viewport (all modes except `Plain`), S5 release-info **N/A** (xbps
   has no `Release` gate; it fails closed on signature mismatch).
2. **Gates** — three layers: `use_enhanced_ui = mode != Plain && stdout TTY &&
   TERM != dumb`; `use_fullscreen_ui = mode == Tui && enhanced && stdin TTY &&
   UTF-8 locale`. S1 additionally needs root, a non-empty plan and no
   `-y`/`-n`/`--simple`/`--dry-run`; S3 needs root and no `--auto`/`--dry-run`.
   Below 60×18 at entry the text path is used with no warning; only a failed
   init/render warns once.
3. **Look** — thick frame, command-derived `xbpretty <Op>` title, operation
   tabs with a colour-independent `━` underline, table with per-column widths
   and a deterministic SpaceAround, `❯` selection gutter, scrollbar, footer with
   nala's no-separator quirk, help block. The fetch picker is rounded,
   single-select with `✓`/`☐`, and uses the fit-based drop order (C1).
4. **Viewport** — no alternate screen, no log pane, no `q`; cbreak with `ISIG`
   on while no child reads stdin; hide for a `?1049h` child via a two-state
   latch; Ctrl-C is the real signal; xbpretty-owned transfers exit 130.
5. **Engine** — stdlib Go only in `internal/term`/`internal/tui`: termios raw/
   cbreak, `?1049`, cursor, SGR mouse, bracketed paste (content discarded), a
   cell buffer with dirty diff, an embedded width table, an input decoder,
   SIGWINCH, and an ordered restore guard on normal exit, signal and panic.
   DSR is an optimisation with a 100 ms timeout and a no-anchor fallback.
6. **Colour** — the ADR-0012 nine roles become buffer styles; `Never` removes
   SGR only, never glyphs or layout controls; every body cell is based on
   `Regular` so a custom background covers the interior.
7. **Tui stdout** — in `Tui` mode the transaction summary screen replaces
   stdout for that transaction. Every other surface and every non-TUI path keeps
   ADR-0005's durable-output rule.

## Consequences

- The TUI adds ≈3.8k–5.6k production LOC and <100 KB to the binary, measured
  separately from the `net/http`/TLS stack.
- #52 proved the risky pieces (restore guard, inline viewport, DSR fallback,
  cell diff, width, decoder, resize) with zero dependencies; the engine is
  feasible.
- Release-info is a recorded N/A, not a modal with no xbps input.

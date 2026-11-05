# ADR-0012: The nala v0.18.2 visual system

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-061–FR-066, AC-044–AC-048, AC-068
- Supersedes: ADR-0011 (except its decision 1, the zero-dependency engine)

## Context

ADR-0011 described a rounded-box summary, a five-line spinner widget and
`box_style`/`progress_bar` keys. Research #39, the #45 grilling and the #51
prototype measured nala v0.18.2's actual output: borderless full-width tables,
ASCII `=` rules, nine colour roles, no spinner, no completion line, and a
rounded widget only for live progress. Keeping the old rendering would make
"nala parity" false on every screen.

## Decision

The rendering rule is: every glyph, role, string and spacing is nala's unless
xbps cannot supply the element; every adaptation is in Appendix A.4.

1. **Inline summary** — operation sections in the fixed order with `=` rules
   before/after every label and the ` Summary` block; borderless `NOTHING`
   tables with `DynamicFullWidth`, 2-space gaps, one trailing space, only the
   last column right-aligned; full width on a TTY, natural width when piped;
   ASCII `=` at terminal width, also when piped.
2. **Roles** — nine PascalCase roles with nala's defaults and SGR prefixes.
   Install/upgrade names are `Secondary` (bright blue), remove family `Error`,
   held/reinstall/configure and version-diff components `Notice`. ` Summary`,
   footer counts, sizes and the prompt are uncoloured. `Regular` is never
   emitted on the inline path; it is used in the widget and the TUI.
3. **Version diff** — the old version is raw; the new version's changed
   components are `Notice`, split at the last `_` and then on `.`, with the
   revision inside the highlighted tail.
4. **Sizes** — summary uses `[Ui] unit`, precision 0, strict `>` (1 MiB →
   `1024 KiB`); the widget is always Binary, precision 1.
5. **Progress** — five views (Download 7 rows; Install, Remove, Update,
   MirrorScore 6), rounded, `min(columns, 80)`, full-width divider, `━` bar
   split by colour only, round fill / truncated percent, no spinner, no
   completion line. Plain fallback is the 40-cell stderr line.
6. **Streams** — stdout carries durable output only; the prompt, widget, plain
   line, hints and forwarded child output are stderr. The prompt moves from
   nala's stdout to stderr (ADR-0005's piping rule). Child stdout is never
   copied to xbpretty's stdout.
7. **Config** — `box_style`, `progress_bar` and the old `color` key are
   retired; `[Ui] mode = "Plain"` is the supported off switch. Visual config is
   exactly nala's `[Ui] mode`/`unit`, `[Color] mode` and `[Color.theme]`.
8. **Adaptations** — no mirror table in Download, no fetched-byte line, no
   `Nothing to fetch`, `Removing Packages` is an xbps title, held reason is
   only `Manual hold`, there is no `Downgrade` section, and the xbps revision
   separator is `_`.

## Consequences

- Every visual golden is re-cut against nala's frames; #51 proved the geometry
  byte-for-byte and recorded that squeezed widths below 80 columns need
  comfy-table's `optimize_space_after_split`, so those are best-effort.
- `box_style`/`progress_bar` users get no box and no toggle; they get nala's
  one table and `Plain`.
- ADR-0011's decision 1 (stdlib VT100 engine) survives and is extended by
  ADR-0017. ADR-0011 is superseded in full otherwise.

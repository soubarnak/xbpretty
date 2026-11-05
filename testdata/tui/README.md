# TUI frame goldens

These files are the byte goldens for the full-screen surfaces (`internal/tui`).
Each `<name>.txt` is the cell grid; `<name>.sgr` is the exact byte stream from
`Buffer.Flush(nil, true)`; `.nocolor.sgr` is the same flush with colour off.

Provenance:

- `summary-80x24.*` keeps the ADR-0017/nala layout and wording; the SGR bytes
  were re-cut when `internal/tui` adopted full-state SGR (`0;…`) so a
  default-fg Highlight cannot inherit the previous cell's colour.
- `s1-upgrade-80x24.*` is cut from the production `planToSummary` conversion
  for the PTY fixture: three upgrade rows whose derived totals are the ADR
  frame's `12.4 MiB` download and `45.0 MiB` disk.
- `summary-100x30`, `summary-140x40`, `summary-60x18`,
  `summary-multitab-100x30`, `held-diff-80x24`, `scrollable-80x24`,
  `history-80x24` and `reinstall-80x24` are cut from the deterministic
  renderer with the plain-data models in
  `internal/tui/golden_models_test.go`. They cover all tab/footer shapes,
  the 60x18 squeezed layout, a scrollable selection and the read-only history
  variant.
- `toolarge-50x15.txt` is the AC-054 too-small body (border and title stay).

To re-cut a golden after an intentional renderer change, render the same model
in a throwaway test and overwrite the file; the golden test prints a got/want
diff. These are engineering fixtures, not captured terminal output.

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

- `fetch-80x24`, `fetch-60x18` and `fetch-120x30` are the FR-070/AC-052
  picker frames cut from the four-mirror rev 6 fixture with the plain-data
  model in `internal/tui/fetch_test.go`; the 120-column frame is the Location
  threshold, the 60x18 frame exercises C1's Region drop and 24-cell URL floor.
  The selected URL and Score carry Secondary+REVERSED, which is why the SGR
  bytes changed from the prototype.
- `fetch-pty-80x24` is the same picker through the built binary and a
  four-mirror HTTP fixture. Its URL host carries the fixed-width placeholder
  `127.0.0.1:00000`; the PTY test substitutes the live five-digit port before
  comparing bytes, so the frame stays exact without pinning a port.

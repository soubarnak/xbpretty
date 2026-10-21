# 0011. Nala-Style TUI Layout and Execution Progress

Date: 2026-10-22

## Status
Accepted

## Context
`xbpretty` was initially built with a minimalist summary (plain group headers, unaligned raw columns, and standard ANSI color sequences) and streamed raw `xbps` child process output directly to stdout/stderr. While functional, this provided a barebones experience compared to modern package managers like Nala on Debian/Ubuntu (`volitank/nala`), which present rich tabular layouts with version diff highlighting and live in-place execution progress widgets.

Research and prototyping on the repository (`soubarnak/xbpretty#25`) revealed several core constraints:
1. `xbps-install` uses `isatty(STDOUT_FILENO)` to dictate output destination (`stderr` on TTY, `stdout` on pipes) and suffers from standard libc 4KB block-buffering latency when stdout is piped without `fflush()`.
2. Heavy third-party Go TUI frameworks (e.g. Charm Lipgloss/Bubbles, Pterm) introduce 50+ transitive dependencies, add +2.6 MB to binary size, and break offline Void Linux packaging (`xbps-src`).
3. Automation scripts (`xbp upgrade -n | grep pkg`) require clean plaintext output without ANSI or Unicode box characters when piped (ADR-0005).

## Decision
1. **Zero-Dependency VT100 Engine**: Implement a lightweight, zero-dependency ANSI/VT100 rendering engine in standard library Go. Maintain 0 external dependencies, < 2ms startup overhead, and direct deterministic control over cursor manipulation (`\x1b[5A\r\x1b[0J`).
2. **Rounded Grid Summary Layout**: Use Unicode rounded box containers (`╭── ╮`, `╰── ╯`) for package action groups. Represent upgrades using a unified arrow column (`<old> → <new>`), highlighting changed package revisions or upstream version components in vibrant ANSI 202 orange (`\x1b[38;5;202m`). Finish with a matching rounded summary card.
3. **Hybrid PTY/Pipe Execution Engine**:
   - When interactive (`isatty(1) == true`): Attach child `xbps` stdout to a Pseudo-Terminal (PTY) to ensure immediate line buffering during unpacking/configuring, and attach stderr to a pipe to cleanly capture unbuffered download ticks.
   - Collapse download and configuration log streams into a 5-line framed progress widget with live counters, cyan-over-red progress bar, speed, and spinner.
   - On exit code 0, clear the 5-line widget and output a permanent single-line checkmark summary.
4. **Piping & Non-TTY Graceful Degradation**:
   - When stdout is not a TTY or when redirected, drop container borders and ANSI color codes, rendering space-aligned clean plaintext.
   - Suppress the in-place cursor progress box on non-TTY streams.
   - Bypass the progress box and stream raw unbuffered output when `-v`/`--verbose` is passed.
5. **Configuration Overrides**:
   - Introduce `box_style = "rounded"` (`"rounded"`, `"classic"`, `"ascii"`) and `progress_bar = true` in `$XDG_CONFIG_HOME/xbpretty/config.toml`.

## Consequences
- **Positive**: Delivers the visual aesthetics of Nala directly on Void Linux with immediate progress feedback and clean version comparisons.
- **Positive**: Maintains pure Go static binary compilation (`CGO_ENABLED=0`), sub-100ms startup times, and offline `xbps-src` template compatibility.
- **Positive**: Guarantees complete scriptability and zero disruption to shell pipelines (ADR-0005).
- **Negative**: Requires maintaining the lightweight VT100 cursor control and regex parsing logic in `internal/summary` and `internal/cli`.

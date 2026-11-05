package cli

import (
	"fmt"
	"io"

	"github.com/soubarnak/xbpretty/internal/config"
)

// rev5AggregateWarning is printed once per run when any rev-5 key was seen
// (FR-058). Warnings never change the exit code.
const rev5AggregateWarning = "xbpretty: warning: rev 5 config keys are accepted for rev 6 only; see https://github.com/soubarnak/xbpretty/blob/main/docs/migration-rev6.md"

// reportConfig prints the per-key config warnings, the one aggregate rev-5
// warning, and the --debug layer/legacy/resolved report. Everything goes to
// stderr.
func reportConfig(w io.Writer, opts *options, res config.Result, resolved settings) {
	for _, warning := range res.Warnings {
		fmt.Fprintf(w, "xbpretty: warning: %s\n", warning)
	}
	if len(res.Legacy) > 0 {
		fmt.Fprintln(w, rev5AggregateWarning)
	}
	if !opts.debug {
		return
	}
	for _, line := range res.DebugLines() {
		fmt.Fprintf(w, "xbpretty: debug: %s\n", line)
	}
	fmt.Fprintf(w, "xbpretty: debug: resolved effective mode=%s unit=%s color=%s assume_yes=%t auto_remove=%t simple=%t full=%t sync=%t log=%t verbose=%t history_dir=%s\n",
		resolved.mode, resolved.unit, resolved.color, resolved.assumeYes, resolved.autoRemove,
		resolved.simple, resolved.full, resolved.sync, resolved.log, resolved.verbose, resolved.historyDir)
}

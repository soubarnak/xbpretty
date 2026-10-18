package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// runLog implements `xbp log [--limit N] [package...]` (ADR-0008). It only
// reads the transaction log: no xbps call, no root, no prompt, no write.
func (st *state) runLog(opts *options) int {
	switch {
	case opts.dryRun:
		return st.logUsage("--dry-run does not apply to log")
	case opts.yes:
		return st.logUsage("--yes does not apply to log")
	case opts.noSync:
		return st.logUsage("--no-sync does not apply to log")
	case opts.dashDash:
		return st.logUsage("log takes no arguments after --")
	}
	for _, name := range opts.args {
		if name == "" {
			return st.logUsage("package names must not be empty")
		}
	}

	path, err := config.TransactionLogPath()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: cannot locate the transaction log: %v\n", err)
		return exitRuntime
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return st.nothingLogged()
		}
		fmt.Fprintf(st.stderr, "xbpretty: cannot read the transaction log: %v\n", err)
		return exitRuntime
	}
	defer f.Close()

	var matched []string
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if len(opts.args) == 0 || logLineNames(line, opts.args) {
				matched = append(matched, line)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(st.stderr, "xbpretty: cannot read the transaction log: %v\n", err)
				return exitRuntime
			}
			break
		}
	}
	if opts.limit > 0 && len(matched) > opts.limit {
		matched = matched[len(matched)-opts.limit:]
	}
	if len(matched) == 0 {
		if info, err := f.Stat(); err == nil && info.Size() == 0 {
			return st.nothingLogged()
		}
		return exitOK
	}
	for _, line := range matched {
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		fmt.Fprint(st.stdout, line)
	}
	return exitOK
}

func (st *state) logUsage(msg string) int {
	fmt.Fprintf(st.stderr, "xbpretty: %s\nusage: xbp log [--limit N] [package...]\n", msg)
	return exitUsage
}

func (st *state) nothingLogged() int {
	fmt.Fprintln(st.stderr, "xbpretty: nothing has been logged yet")
	return exitOK
}

// logLineNames reports whether any package entry of a log line has exactly
// one of the given names. Fields 1 and 2 are the timestamp and the command.
func logLineNames(line string, names []string) bool {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return false
	}
	for _, entry := range fields[2:] {
		name, _ := xbps.SplitPkgver(entry)
		for _, want := range names {
			if name == want {
				return true
			}
		}
	}
	return false
}

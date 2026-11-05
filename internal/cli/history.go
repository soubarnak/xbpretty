package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// historyPending is one in-flight real transaction's recording context: the
// metadata captured after the prompt and the before-state snapshot from the
// four bulk queries.
type historyPending struct {
	startedAt  time.Time
	command    []string
	targets    []string
	implicit   bool
	replayOf   *uint32
	replayKind string
	before     map[string]history.PackageState
	beforeErr  error
	rows       []xbps.Row
	cmd        string
	opts       *options
}

// beginHistory captures the before state after the prompt and before the real
// xbps child, so a successful run can publish its entry before the line log
// and the reboot hint (FR-031).
func (st *state) beginHistory(cmd string, opts *options, rows []xbps.Row) {
	st.beginHistoryEntry(cmd, opts, rows, st.commandLine, requestedTargets(cmd, opts), false, nil, "")
}

// beginHistoryEntry is the seam #70's replays consume: same capture and
// publication rules, with an explicit argv, replay source and implicit flag.
func (st *state) beginHistoryEntry(cmd string, opts *options, rows []xbps.Row, commandLine []string, targets []string, implicit bool, replayOf *uint32, replayKind string) {
	pending := &historyPending{
		startedAt:  time.Now().UTC(),
		command:    append([]string{}, commandLine...),
		targets:    append([]string{}, targets...),
		implicit:   implicit,
		replayOf:   replayOf,
		replayKind: replayKind,
		rows:       append([]xbps.Row{}, rows...),
		cmd:        cmd,
		opts:       opts,
	}
	pending.before, pending.beforeErr = st.capturePackageStates()
	st.pendingHistory = pending
}

// finishHistory publishes the entry after the child exited 0. Any failure is
// a warning: it never changes the run's exit code (FR-031).
func (st *state) finishHistory() {
	pending := st.pendingHistory
	st.pendingHistory = nil
	if pending == nil {
		return
	}
	if pending.beforeErr != nil {
		st.historyWarning(pending.beforeErr)
		return
	}
	after, err := st.capturePackageStates()
	if err != nil {
		st.historyWarning(err)
		return
	}
	transitions := st.buildTransitions(pending, after)
	if len(transitions) == 0 {
		return
	}
	// FR-031: an entry whose only rows are Held is not written. A plan that
	// must not run at all (hold-only) stops earlier; this guard covers a
	// mixed plan whose snapshot-derived rows collapsed to holds.
	onlyHeld := true
	for _, transition := range transitions {
		if transition.Operation != history.OpHeld {
			onlyHeld = false
			break
		}
	}
	if onlyHeld {
		return
	}
	var replayKind *string
	if pending.replayKind != "" {
		kind := pending.replayKind
		replayKind = &kind
	}
	entry := history.Entry{
		SchemaVersion:    history.SchemaVersion,
		StartedAt:        pending.startedAt.Format(time.RFC3339),
		FinishedAt:       time.Now().UTC().Format(time.RFC3339),
		Status:           history.StatusApplied,
		RequestedBy:      requestedBy(),
		Command:          pending.command,
		RequestedTargets: pending.targets,
		Implicit:         pending.implicit,
		ReplayOf:         pending.replayOf,
		ReplayKind:       replayKind,
		Packages:         transitions,
	}
	if _, err := history.New(st.historyDir).Write(entry); err != nil {
		st.historyWarning(err)
	}
}

func (st *state) historyWarning(err error) {
	fmt.Fprintf(st.stderr, "xbpretty: history entry not saved: %v\n", err)
}

// requestedTargets is FR-030's raw positional list: not deduplicated, and
// empty for the commands that take no package names.
func requestedTargets(cmd string, opts *options) []string {
	switch cmd {
	case "install", "remove", "purge":
		return append([]string{}, opts.args...)
	}
	return []string{}
}

// requestedBy renders FR-030's `<user> (<uid>)`, preferring the sudo/doas
// invoking user over the effective root account.
func requestedBy() string {
	for _, keys := range [][2]string{{"SUDO_USER", "SUDO_UID"}, {"DOAS_USER", "DOAS_UID"}} {
		name := os.Getenv(keys[0])
		if name == "" {
			continue
		}
		// The calling uid is authoritative; the passwd lookup is only a
		// fallback when sudo/doas did not export it.
		if uid, err := strconv.Atoi(os.Getenv(keys[1])); err == nil {
			return fmt.Sprintf("%s (%d)", name, uid)
		}
		if u, err := user.Lookup(name); err == nil {
			if uid, err := strconv.Atoi(u.Uid); err == nil {
				return fmt.Sprintf("%s (%d)", name, uid)
			}
		}
		return fmt.Sprintf("%s (%d)", name, os.Getuid())
	}
	name := os.Getenv("LOGNAME")
	if name == "" {
		name = os.Getenv("USER")
	}
	if name == "" {
		name = "root"
	}
	return fmt.Sprintf("%s (%d)", name, os.Getuid())
}

// capturePackageStates reads the four bulk queries FR-030's snapshots come
// from: installed pkgvers (-l), the manual set (-m), holds (-H) and repolocks
// (--list-repolock-pkgs). A failure is an error, not a partial snapshot.
func (st *state) capturePackageStates() (map[string]history.PackageState, error) {
	s := newSnapshot()
	installed, err := st.historyBulkQuery("-l")
	if err != nil {
		return nil, err
	}
	s.parseInstalled(installed)
	manual, err := st.historyBulkQuery("-m")
	if err != nil {
		return nil, err
	}
	s.parseNames(manual, s.manual)
	holds, err := st.historyBulkQuery("-H")
	if err != nil {
		return nil, err
	}
	s.parseNames(holds, s.holds)
	repolocked, err := st.historyBulkQuery("--list-repolock-pkgs")
	if err != nil {
		return nil, err
	}
	s.parseNames(repolocked, s.repolocked)

	states := make(map[string]history.PackageState, len(s.installed))
	for name, info := range s.installed {
		version := info.Version
		auto := !s.manual[name]
		states[name] = history.PackageState{
			Version:       &version,
			AutoInstalled: &auto,
			Hold:          s.holds[name],
			Repolock:      s.repolocked[name],
		}
	}
	return states, nil
}

// historyBulkQuery runs one snapshot pass. Its stderr is part of the error a
// failed write reports; a read-only run never prints it.
func (st *state) historyBulkQuery(args ...string) (string, error) {
	res, err := st.r.run(st.r.cfg.Xbps.QueryPath, args...)
	if err != nil {
		return "", err
	}
	if res.exitCode != 0 {
		detail := strings.TrimSpace(res.stderr)
		if detail == "" {
			detail = strings.TrimSpace(res.stdout)
		}
		return "", fmt.Errorf("xbps-query %s exited %d: %s", strings.Join(args, " "), res.exitCode, detail)
	}
	return res.stdout, nil
}

// buildTransitions derives one row per plan entry from the before/after
// snapshots and the plan action (FR-030).
func (st *state) buildTransitions(pending *historyPending, after map[string]history.PackageState) []history.Transition {
	transitions := make([]history.Transition, 0, len(pending.rows))
	force := effectiveForce(pending.cmd, pending.opts)
	for _, row := range pending.rows {
		before := statePointer(pending.before, row.Name)
		afterState := statePointer(after, row.Name)
		operation := st.deriveOperation(row, before, afterState, pending.cmd, force)
		if operation == "" {
			continue
		}
		transition := history.Transition{
			Name:          row.Name,
			Arch:          row.Arch,
			Repository:    row.Repo,
			Size:          row.DownloadSize,
			InstalledSize: row.InstalledSize,
			Operation:     operation,
			Before:        before,
			After:         afterState,
		}
		if operation == history.OpHeld {
			transition.HeldReason = "ManualHold"
		}
		transitions = append(transitions, transition)
	}
	return transitions
}

// statePointer copies the recorded state so a pointer never aliases the map.
func statePointer(states map[string]history.PackageState, name string) *history.PackageState {
	state, ok := states[name]
	if !ok {
		return nil
	}
	copy := state
	return &copy
}

// effectiveForce is the transaction's actual -f: an explicit purge, --purge,
// or a passthrough flag cluster such as `-- -f` / `-- -fy`.
func effectiveForce(cmd string, opts *options) bool {
	if purgeRequested(cmd, opts) {
		return true
	}
	for _, arg := range opts.passthru {
		if strings.HasPrefix(arg, "--") {
			continue
		}
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "f") {
			return true
		}
	}
	return false
}

// deriveOperation applies FR-030's snapshot rules.
func (st *state) deriveOperation(row xbps.Row, before, after *history.PackageState, cmd string, force bool) string {
	if row.Action == xbps.ActionHold {
		return history.OpHeld
	}
	if before == nil && after != nil {
		return history.OpInstall
	}
	if before != nil && after == nil {
		switch cmd {
		case "autoremove", "autopurge":
			if force {
				return history.OpAutoPurge
			}
			return history.OpAutoRemove
		}
		if force {
			return history.OpPurge
		}
		return history.OpRemove
	}
	if before != nil && after != nil {
		if before.Version != nil && after.Version != nil && *before.Version != *after.Version {
			if st.compareVersions(*before.Version, *after.Version) > 0 {
				return history.OpDowngrade
			}
			return history.OpUpgrade
		}
		if row.Action == xbps.ActionConfigure {
			return history.OpConfigure
		}
		return history.OpReinstall
	}
	// Neither side is installed: xbps planned a change the snapshots cannot
	// describe. Keeping it as a hidden row would offer nothing to replay, so
	// the transition is dropped.
	return ""
}

// compareVersions asks xbps-uhelper cmpver which version is newer. Its exit
// status is 1 for greater, 0 for equal and 255 for less. A failure warns
// before falling back to the upgrade direction, so a direction is never
// silently invented.
func (st *state) compareVersions(before, after string) int {
	res, err := st.r.run(st.r.cfg.Xbps.UhelperPath, "cmpver", before, after)
	if err == nil {
		switch res.exitCode {
		case 0:
			return 0
		case 1:
			return 1
		case 255:
			return -1
		}
	}
	fmt.Fprintf(st.stderr, "xbpretty: warning: xbps-uhelper cmpver %s %s failed; recording the change as an upgrade\n", before, after)
	return -1
}

// runHistory is FR-032's read surface: list, or one entry's detail. Clear and
// the replay verbs belong to #70.
func (st *state) runHistory(opts *options) int {
	if opts.subcommand == "info" || opts.subcommand == "show" {
		return st.runHistoryDetail(opts.args[0])
	}
	if len(opts.args) == 0 {
		return st.runHistoryList()
	}
	return st.runHistoryDetail(opts.args[0])
}

func (st *state) runHistoryList() int {
	entries, err := history.New(st.historyDir).List()
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	if len(entries) == 0 {
		fmt.Fprintln(st.stdout, "No history entries found.")
		st.lineLogNote()
		return exitOK
	}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{
			strconv.FormatUint(uint64(entry.ID), 10),
			strings.Join(entry.Command, " "),
			formatHistoryTime(entry.StartedAt),
			entry.RequestedBy,
			strconv.Itoa(entry.Altered()),
		})
	}
	writeHistoryTable(st.stdout, []string{"ID", "Command", "Date", "Requested-By", "Altered"}, rows)
	return exitOK
}

// lineLogNote is FR-032's one stderr line when the store is empty but legacy
// or non-history transactions exist in the line log. It never creates the log.
func (st *state) lineLogNote() {
	path, err := config.TransactionLogPath()
	if err != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return
	}
	fmt.Fprintln(st.stderr, "xbpretty: earlier transactions are only in the line log and cannot be replayed; run 'xbp log' to read them")
}

func (st *state) runHistoryDetail(selector string) int {
	id, last, ok := parseHistorySelector(selector)
	if !ok {
		fmt.Fprintf(st.stderr, "xbpretty: invalid history selector %q; use an integer id or 'last'\n", selector)
		return exitUsage
	}
	store := history.New(st.historyDir)
	var entry history.Entry
	if last {
		found, present, err := store.Last()
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		if !present {
			fmt.Fprintln(st.stderr, "xbpretty: history is empty")
			return exitRuntime
		}
		entry = found
	} else {
		found, err := store.Load(id)
		if errors.Is(err, history.ErrNotFound) {
			fmt.Fprintf(st.stderr, "xbpretty: history entry %d not found\n", id)
			return exitRuntime
		}
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		entry = found
	}
	writeHistoryDetail(st.stdout, entry)
	return exitOK
}

func parseHistorySelector(selector string) (id uint32, last bool, ok bool) {
	if selector == "last" {
		return 0, true, true
	}
	value, err := strconv.ParseUint(selector, 10, 32)
	if err != nil {
		return 0, false, false
	}
	return uint32(value), false, true
}

// formatHistoryTime renders a stored UTC timestamp in the local zone, or the
// raw value when it cannot be parsed.
func formatHistoryTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04:05 MST")
}

func writeHistoryDetail(w io.Writer, entry history.Entry) {
	fmt.Fprintf(w, "ID: %d\n", entry.ID)
	fmt.Fprintf(w, "Status: %s\n", entry.Status)
	fmt.Fprintf(w, "Command: %s\n", strings.Join(entry.Command, " "))
	fmt.Fprintf(w, "Requested-By: %s\n", entry.RequestedBy)
	fmt.Fprintf(w, "Started: %s\n", formatHistoryTime(entry.StartedAt))
	fmt.Fprintf(w, "Finished: %s\n", formatHistoryTime(entry.FinishedAt))
	targets := "None"
	if len(entry.RequestedTargets) > 0 {
		targets = strings.Join(entry.RequestedTargets, ", ")
	}
	fmt.Fprintf(w, "Requested Targets: %s\n", targets)
	fmt.Fprintf(w, "Altered: %d\n", entry.Altered())
	if entry.ReplayOf != nil {
		kind := ""
		if entry.ReplayKind != nil {
			kind = *entry.ReplayKind
		}
		fmt.Fprintf(w, "Replay of: %d (%s)\n", *entry.ReplayOf, kind)
	}
	for _, operation := range history.GroupOrder {
		var rows [][]string
		held := operation == history.OpHeld
		for _, transition := range entry.Packages {
			if transition.Operation != operation {
				continue
			}
			row := []string{transition.Name, versionOrDash(transition.Before), versionOrDash(transition.After)}
			if held {
				row = append(row, transition.HeldReason)
			}
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			continue
		}
		headers := []string{"Name", "Old Version", "New Version"}
		if held {
			headers = append(headers, "Reason")
		}
		fmt.Fprintf(w, "\n%s:\n", operation)
		writeHistoryTable(w, headers, rows)
	}
}

func versionOrDash(state *history.PackageState) string {
	if state == nil || state.Version == nil {
		return "-"
	}
	return *state.Version
}

// writeHistoryTable renders a left-aligned, two-space-gapped table with no
// trailing padding.
func writeHistoryTable(w io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(header)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) && len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}
	writeRow := func(cells []string) {
		var b strings.Builder
		for i := range headers {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			b.WriteString(cell)
			if i < len(headers)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len(cell)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	writeRow(headers)
	for _, row := range rows {
		writeRow(row)
	}
}

// runHistoryClear implements FR-035's clear surface: one entry by id/last, or
// every numeric entry with --all. Both forms are root-only.
func (st *state) runHistoryClear(opts *options) int {
	if euidFn() != 0 {
		st.clearRootHint(opts)
		return exitRuntime
	}
	store := history.New(st.historyDir)
	if opts.clearAll {
		count, err := store.Clear()
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		if count == 1 {
			fmt.Fprintln(st.stdout, "Cleared 1 history entry.")
		} else {
			fmt.Fprintf(st.stdout, "Cleared %d history entries.\n", count)
		}
		return exitOK
	}

	selector := opts.args[0]
	id, last, ok := parseHistorySelector(selector)
	if !ok {
		fmt.Fprintf(st.stderr, "xbpretty: invalid history selector %q; use an integer id or 'last'\n", selector)
		return exitUsage
	}
	if last {
		entry, present, err := store.Last()
		if err != nil {
			fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
			return exitRuntime
		}
		if !present {
			fmt.Fprintln(st.stderr, "xbpretty: history is empty")
			return exitRuntime
		}
		id = entry.ID
	}
	if err := store.Delete(id); err != nil {
		if errors.Is(err, history.ErrNotFound) {
			fmt.Fprintf(st.stderr, "xbpretty: history entry %d not found\n", id)
			return exitRuntime
		}
		fmt.Fprintf(st.stderr, "xbpretty: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintf(st.stdout, "Cleared history entry %d.\n", id)
	return exitOK
}

// clearRootHint prints the root hint for clear without inventing a package
// list: the selector or --all is part of the refused invocation.
func (st *state) clearRootHint(opts *options) {
	elevate := "sudo"
	if _, err := os.Stat("/usr/bin/doas"); err == nil {
		elevate = "doas"
	}
	line := "xbp history clear"
	if opts.clearAll {
		line += " --all"
	} else if len(opts.args) > 0 {
		line += " " + opts.args[0]
	}
	fmt.Fprintf(st.stderr, "hint: rerun as root: %s %s\n", elevate, line)
}

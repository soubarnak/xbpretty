//go:build linux

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/history"
	"github.com/soubarnak/xbpretty/internal/term"
)

// historyState is a recorded before/after version.
func historyState(v string) *history.PackageState {
	return &history.PackageState{Version: &v}
}

// historyFullEntry is the AC-051 fixture: four non-empty operation groups in a
// deliberately mixed recording order, so the model must re-sort by GroupOrder.
func historyFullEntry() history.Entry {
	return history.Entry{
		SchemaVersion:    history.SchemaVersion,
		ID:               1,
		Status:           history.StatusApplied,
		RequestedBy:      "tester (1000)",
		Command:          []string{"upgrade"},
		RequestedTargets: []string{},
		Packages: []history.Transition{
			{Name: "figlet", Operation: history.OpInstall, After: historyState("2.2.5_6"), Size: 94208},
			{Name: "nano", Operation: history.OpRemove, Before: historyState("9.2_1"), InstalledSize: 3 * 1024 * 1024},
			{Name: "glibc", Operation: history.OpUpgrade, Before: historyState("2.41_1"), After: historyState("2.41_2"), Size: 12 * 1024 * 1024},
			{Name: "bash", Operation: history.OpHeld, Before: historyState("5.2_1"), After: historyState("5.2_1"), InstalledSize: 1024 * 1024, HeldReason: "ManualHold"},
		},
	}
}

// historyManyRowsEntry has enough Install rows to exercise the row bindings.
func historyManyRowsEntry() history.Entry {
	entry := historyFullEntry()
	entry.Packages = []history.Transition{
		{Name: "nano", Operation: history.OpRemove, Before: historyState("9.2_1"), InstalledSize: 3 * 1024 * 1024},
		{Name: "figlet", Operation: history.OpInstall, After: historyState("2.2.5_6"), Size: 94208},
		{Name: "sl", Operation: history.OpInstall, After: historyState("5.0_1"), Size: 1024},
		{Name: "zsh", Operation: history.OpInstall, After: historyState("5.9_3"), Size: 2 * 1024 * 1024},
		{Name: "glibc", Operation: history.OpUpgrade, Before: historyState("2.41_1"), After: historyState("2.41_2"), Size: 12 * 1024 * 1024},
	}
	return entry
}

// writeHistoryEntry publishes one fixture in the store directory.
func writeHistoryEntry(t *testing.T, dir string, entry history.Entry) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", entry.ID)), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// historyTuiEnv is the S2 run environment. The effective uid is deliberately
// non-root: FR-067's history detail needs no root.
func historyTuiEnv(t *testing.T, dir string, entry history.Entry) map[string]string {
	t.Helper()
	query := tuiOldQuery(t, dir)
	env := tuiScreenEnvMode(t, dir, query, "Tui", "Always")
	env["XBPRETTY_TEST_EUID"] = "1000"
	writeHistoryEntry(t, env["XBPRETTY_HISTORY_DIR"], entry)
	return env
}

// historyFrameEnv adds the #76 test-only frame dump.
func historyFrameEnv(t *testing.T, dir string, entry history.Entry, dump string) map[string]string {
	t.Helper()
	env := historyTuiEnv(t, dir, entry)
	env["XBPRETTY_TEST_TUI_DUMP"] = dump
	return env
}

// TestHistoryDetailScreenFrame is AC-051's frame half: the read-only screen
// opens non-root with the History title, the entry's groups in fixed order,
// counts-only footer rows and the quit help, and q leaves with exit 0.
func TestHistoryDetailScreenFrame(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	dump := t.TempDir()
	env := historyFrameEnv(t, dir, historyFullEntry(), dump)

	r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
	frames := waitFrameCount(t, r, dump, 1)
	frame := frames[0]
	if !strings.Contains(frame, "xbpretty History") {
		t.Errorf("frame missing the History title:\n%s", frame)
	}
	if !strings.Contains(frame, "Remove Install Upgrade Held") {
		t.Errorf("tab strip is not the fixed operation order:\n%s", frame)
	}
	if got := activeTab(t, frame); got != "Remove" {
		t.Errorf("initial active tab = %q, want Remove", got)
	}
	for _, want := range []string{
		"Remove: 1 Pkgs", "Install:1 Pkgs", "Upgrade:1 Pkgs", "Held:   1 Pkgs",
		"Package:", "Version:", "Size:", "nano", "(s) package info | (q) quit",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame missing %q:\n%s", want, frame)
		}
	}
	for _, unwanted := range []string{
		"Total download size:", "Disk space required:", "Disk space to free:", "(y) start",
	} {
		if strings.Contains(frame, unwanted) {
			t.Errorf("history frame still draws %q:\n%s", unwanted, frame)
		}
	}
	if !strings.Contains(frame, "(→) next tab | (←) previous tab") {
		t.Errorf("history frame is missing the navigation help:\n%s", frame)
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("quit exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(r.stderr.String(), "user refused confirmation") {
		t.Errorf("read-only quit printed a refusal:\n%s", r.stderr.String())
	}
	out := waitCapture(t, r, "the screen leave", func(s string) bool {
		return strings.Contains(s, term.AltLeave) && strings.Contains(s, term.CursorShow)
	})
	for _, seq := range []string{term.MouseOff, term.PasteOff, term.AltLeave, term.CursorShow} {
		if !strings.Contains(out, seq) {
			t.Errorf("the restore is missing %q", seq)
		}
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after quit")
	}
}

// TestHistoryDetailNavigationAndPackageInfo is FR-069's navigation half: j/k,
// h/l, Home/End, PgUp/PgDn and the wheel move the read-only selection, s opens
// package info in the same frame, and q returns without leaving the screen.
func TestHistoryDetailNavigationAndPackageInfo(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	dump := t.TempDir()
	env := historyFrameEnv(t, dir, historyManyRowsEntry(), dump)

	r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
	frames := waitFrameCount(t, r, dump, 1)
	if got := activeTab(t, frames[0]); got != "Remove" {
		t.Fatalf("initial active tab = %q, want Remove", got)
	}

	r.send(t, "l")
	frames = waitFrameCount(t, r, dump, 2)
	if got := activeTab(t, frames[1]); got != "Install" {
		t.Fatalf("after l active tab = %q, want Install", got)
	}
	if got := selectedPackage(t, frames[1]); got != "figlet" {
		t.Fatalf("after l selection = %q, want figlet", got)
	}

	r.send(t, "j")
	frames = waitFrameCount(t, r, dump, 3)
	if got := selectedPackage(t, frames[2]); got != "sl" {
		t.Fatalf("after j selection = %q, want sl", got)
	}
	r.send(t, "k")
	frames = waitFrameCount(t, r, dump, 4)
	if got := selectedPackage(t, frames[3]); got != "figlet" {
		t.Fatalf("after k selection = %q, want figlet", got)
	}

	// End/Home and the paging keys clamp inside the Install tab.
	r.send(t, "\x1b[F")
	frames = waitFrameCount(t, r, dump, 5)
	if got := selectedPackage(t, frames[4]); got != "zsh" {
		t.Fatalf("after End selection = %q, want zsh", got)
	}
	r.send(t, "\x1b[H")
	frames = waitFrameCount(t, r, dump, 6)
	if got := selectedPackage(t, frames[5]); got != "figlet" {
		t.Fatalf("after Home selection = %q, want figlet", got)
	}
	r.send(t, "\x1b[6~")
	frames = waitFrameCount(t, r, dump, 7)
	if got := selectedPackage(t, frames[6]); got != "zsh" {
		t.Fatalf("after PgDn selection = %q, want zsh", got)
	}
	r.send(t, "\x1b[5~")
	frames = waitFrameCount(t, r, dump, 8)
	if got := selectedPackage(t, frames[7]); got != "figlet" {
		t.Fatalf("after PgUp selection = %q, want figlet", got)
	}
	r.send(t, "\x1b[<65;10;20M")
	frames = waitFrameCount(t, r, dump, 9)
	if got := selectedPackage(t, frames[8]); got != "sl" {
		t.Fatalf("after wheel-down selection = %q, want sl", got)
	}
	r.send(t, "\x1b[<64;10;20M")
	frames = waitFrameCount(t, r, dump, 10)
	if got := selectedPackage(t, frames[9]); got != "figlet" {
		t.Fatalf("after wheel-up selection = %q, want figlet", got)
	}
	r.send(t, "\x1b[B")
	frames = waitFrameCount(t, r, dump, 11)
	if got := selectedPackage(t, frames[10]); got != "sl" {
		t.Fatalf("after down arrow selection = %q, want sl", got)
	}
	r.send(t, "\x1b[A")
	frames = waitFrameCount(t, r, dump, 12)
	if got := selectedPackage(t, frames[11]); got != "figlet" {
		t.Fatalf("after up arrow selection = %q, want figlet", got)
	}

	// s opens the selected row in the same frame, and q returns to the detail.
	r.send(t, "s")
	frames = waitFrameCount(t, r, dump, 13)
	info := frames[12]
	for _, want := range []string{"xbpretty History", "Package:", "figlet", "Operation:", "Install", "(q) back to history"} {
		if !strings.Contains(info, want) {
			t.Errorf("package-info frame missing %q:\n%s", want, info)
		}
	}
	if strings.Contains(info, "Total download size:") || strings.Contains(info, "Disk space required:") {
		t.Errorf("package info inherited the S1 footer:\n%s", info)
	}
	r.send(t, "q")
	frames = waitFrameCount(t, r, dump, 14)
	if got := selectedPackage(t, frames[13]); got != "figlet" {
		t.Fatalf("selection after returning = %q, want figlet", got)
	}

	// h/l clamp at both ends; tab switches reset the selection.
	r.send(t, "l")
	frames = waitFrameCount(t, r, dump, 15)
	if got := activeTab(t, frames[14]); got != "Upgrade" {
		t.Fatalf("after l active tab = %q, want Upgrade", got)
	}
	r.send(t, "l")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 15 {
		t.Fatalf("clamped l drew a frame: %d frames, want 15", got)
	}
	r.send(t, "h")
	frames = waitFrameCount(t, r, dump, 16)
	if got := activeTab(t, frames[15]); got != "Install" {
		t.Fatalf("after h active tab = %q, want Install", got)
	}
	r.send(t, "\x1b[D")
	frames = waitFrameCount(t, r, dump, 17)
	if got := activeTab(t, frames[16]); got != "Remove" {
		t.Fatalf("after left arrow active tab = %q, want Remove", got)
	}
	r.send(t, "\x1b[C")
	frames = waitFrameCount(t, r, dump, 18)
	if got := activeTab(t, frames[17]); got != "Install" {
		t.Fatalf("after right arrow active tab = %q, want Install", got)
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("quit after navigation exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
}

// TestHistoryDetailQuitKeysExitZero is FR-069's quit matrix: q/Q/Esc/Ctrl-C and
// EOF leave the read-only screen and exit 0, with no refusal line.
func TestHistoryDetailQuitKeysExitZero(t *testing.T) {
	bin := buildTuiBinary(t)
	cases := []struct {
		name string
		send string
		eof  bool
	}{
		{"q", "q", false},
		{"Q", "Q", false},
		{"esc", "\x1b", false},
		{"ctrl-c", "\x03", false},
		{"eof", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dump := t.TempDir()
			env := historyFrameEnv(t, dir, historyFullEntry(), dump)
			if tc.eof {
				r := startPtyDetached(t, bin, env, []string{"history", "1"}, 80, 24)
				waitFrameCount(t, r, dump, 1)
				drainPtyMaster(t, r.master)
				_ = r.master.Close()
				if code := r.exitCode(t); code != 0 {
					t.Fatalf("EOF exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
				}
				return
			}
			r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
			waitFrameCount(t, r, dump, 1)
			r.send(t, tc.send)
			if code := r.exitCode(t); code != 0 {
				t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
			}
			if strings.Contains(r.stderr.String(), "user refused confirmation") {
				t.Errorf("read-only quit printed a refusal:\n%s", r.stderr.String())
			}
			waitCapture(t, r, "the screen leave", func(s string) bool {
				return strings.Contains(s, term.AltLeave)
			})
			after, err := term.GetState(r.slave)
			if err != nil {
				t.Fatalf("GetState after: %v", err)
			}
			if !after.Equal(r.before) {
				t.Errorf("termios flags not restored")
			}
		})
	}
}

// TestHistoryDetailIgnoresConfirmKeys is FR-069's inert-key half: y/Y/n/N never
// confirm, refuse or repaint, not even with package info open.
func TestHistoryDetailIgnoresConfirmKeys(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	dump := t.TempDir()
	env := historyFrameEnv(t, dir, historyFullEntry(), dump)

	r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
	waitFrameCount(t, r, dump, 1)
	for _, key := range []string{"y", "Y", "n", "N"} {
		r.send(t, key)
	}
	time.Sleep(150 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 1 {
		t.Fatalf("confirm keys drew a frame: %d frames, want 1", got)
	}

	// They are inert in package info too.
	r.send(t, "s")
	frames := waitFrameCount(t, r, dump, 2)
	if !strings.Contains(frames[1], "Operation:") {
		t.Fatalf("package info did not open:\n%s", frames[1])
	}
	for _, key := range []string{"n", "N", "y", "Y"} {
		r.send(t, key)
	}
	time.Sleep(150 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 2 {
		t.Fatalf("confirm keys in package info drew a frame: %d frames, want 2", got)
	}
	// q first returns from package info to the detail; the second q leaves.
	r.send(t, "q")
	waitFrameCount(t, r, dump, 3)
	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("quit exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
}

// TestHistoryDetailGateMatrix is AC-051/FR-067 through the real binary: the S2
// screen opens only on the full-screen gate at or above 60x18, needs no root,
// and every excluded cell takes the text detail.
func TestHistoryDetailGateMatrix(t *testing.T) {
	bin := buildTuiBinary(t)
	cases := []struct {
		name       string
		mode       string
		alter      func(map[string]string)
		args       []string
		cols       int
		rows       int
		wantScreen bool
	}{
		{name: "80x24 opens non-root", cols: 80, rows: 24, wantScreen: true},
		{name: "60x18 opens", cols: 60, rows: 18, wantScreen: true},
		{name: "--tui opens from Auto", mode: "Auto", args: []string{"history", "1", "--tui"}, cols: 80, rows: 24, wantScreen: true},
		{name: "59x17 falls back", cols: 59, rows: 17},
		{name: "17 rows falls back", cols: 80, rows: 17},
		{name: "Auto mode falls back", mode: "Auto", cols: 80, rows: 24},
		{name: "Plain mode falls back", mode: "Plain", cols: 80, rows: 24},
		{name: "TERM=dumb falls back", alter: func(env map[string]string) { env["TERM"] = "dumb" }, cols: 80, rows: 24},
		{name: "non-UTF-8 locale falls back", alter: func(env map[string]string) {
			env["LC_ALL"] = "C"
			env["LC_CTYPE"] = "C"
			env["LANG"] = "C"
		}, cols: 80, rows: 24},
		{name: "--no-tui demotes Tui", args: []string{"history", "1", "--no-tui"}, cols: 80, rows: 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mode := tc.mode
			if mode == "" {
				mode = "Tui"
			}
			query := tuiOldQuery(t, dir)
			env := tuiScreenEnvMode(t, dir, query, mode, "Always")
			env["XBPRETTY_TEST_EUID"] = "1000"
			writeHistoryEntry(t, env["XBPRETTY_HISTORY_DIR"], historyFullEntry())
			if tc.alter != nil {
				tc.alter(env)
			}
			args := tc.args
			if args == nil {
				args = []string{"history", "1"}
			}
			r := startPty(t, bin, env, args, tc.cols, tc.rows)
			if tc.wantScreen {
				waitCapture(t, r, "the History frame", func(s string) bool {
					return strings.Contains(frameTail(s), "xbpretty History")
				})
				if !strings.Contains(r.cap.String(), term.AltEnter) {
					t.Errorf("history detail did not open: %q", r.cap.String())
				}
				r.send(t, "q")
				if code := r.exitCode(t); code != 0 {
					t.Errorf("quit exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
				}
				return
			}
			// The text path exits on its own; collect its output before the
			// exit code so the pty reader has drained the stream.
			// Wait for the last line of the text detail so the pty reader has
			// drained the whole stream before the exit code is collected.
			out := waitCapture(t, r, "the text detail", func(s string) bool {
				return strings.Contains(s, "Install:")
			})
			if code := r.exitCode(t); code != 0 {
				t.Fatalf("text fallback exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
			}
			if strings.Contains(out, term.AltEnter) {
				t.Errorf("an excluded gate opened the alternate screen: %q", out)
			}
			if !strings.Contains(out, "ID: 1") {
				t.Errorf("the text detail did not render:\n%q", out)
			}
			if strings.Contains(r.stderr.String(), "full-screen interface") {
				t.Errorf("the expected text fallback warned:\n%s", r.stderr.String())
			}
		})
	}
}

// TestHistoryDetailNoGroupsFallsBackToText is FR-069's empty-entry rule: an
// entry with no group rows never opens the screen.
func TestHistoryDetailNoGroupsFallsBackToText(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	empty := history.Entry{
		SchemaVersion: history.SchemaVersion,
		ID:            1,
		Status:        history.StatusApplied,
		RequestedBy:   "tester (1000)",
		Command:       []string{"clean"},
		Packages:      []history.Transition{},
	}
	env := historyTuiEnv(t, dir, empty)

	r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
	out := waitCapture(t, r, "the text detail", func(s string) bool {
		return strings.Contains(s, "Altered: 0")
	})
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(out, term.AltEnter) {
		t.Errorf("an empty entry opened the history screen: %q", out)
	}
	for _, want := range []string{"ID: 1", "Status: Applied", "Altered: 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("text detail missing %q:\n%q", want, out)
		}
	}
}

// TestHistoryListStaysText is FR-069's list rule: xbp history (no selector) is
// always the text table, even under Tui on a terminal.
func TestHistoryListStaysText(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	env := historyTuiEnv(t, dir, historyFullEntry())

	r := startPty(t, bin, env, []string{"history"}, 80, 24)
	// The list header ends with Altered; waiting for it drains the whole table
	// before the pty reader is asked to stop.
	out := waitCapture(t, r, "the history list", func(s string) bool {
		return strings.Contains(s, "Altered")
	})
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(out, term.AltEnter) {
		t.Errorf("the history list opened the alternate screen: %q", out)
	}
	for _, want := range []string{"ID", "Command", "Date", "Requested-By", "Altered"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%q", want, out)
		}
	}
}

// TestHistoryDetailBelowFloorIgnoresConfirmKeys reuses #76's resize path: the
// too-small body keeps the History title, n/N stay inert, and q still exits 0.
func TestHistoryDetailBelowFloorIgnoresConfirmKeys(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	dump := t.TempDir()
	env := historyFrameEnv(t, dir, historyFullEntry(), dump)

	r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
	waitFrameCount(t, r, dump, 1)
	if err := setPtySize(r.slave, 50, 15); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := r.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("SIGWINCH: %v", err)
	}
	frames := waitFrameCount(t, r, dump, 2)
	small := frames[1]
	if !strings.Contains(small, "Terminal too small (60x18 minimum; now 50x15)") {
		t.Fatalf("too-small frame missing the notice:\n%s", small)
	}
	if !strings.Contains(small, "xbpretty History") {
		t.Fatalf("too-small frame dropped the History title:\n%s", small)
	}
	// n/N and navigation are inert below the floor: no repaint.
	r.send(t, "n")
	r.send(t, "N")
	r.send(t, "j")
	r.send(t, "s")
	time.Sleep(150 * time.Millisecond)
	if got := len(dumpedFrames(t, dump)); got != 2 {
		t.Fatalf("below-floor interaction drew a frame: %d frames, want 2", got)
	}
	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("below-floor quit exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
}

// TestHistoryDetailPackageInfoRemoveRow is FR-069's `s` on a removal row: the
// single recorded version is Version, the duplicate Installed row is gone, and
// the help returns to history.
func TestHistoryDetailPackageInfoRemoveRow(t *testing.T) {
	bin := buildTuiBinary(t)
	dir := t.TempDir()
	dump := t.TempDir()
	env := historyFrameEnv(t, dir, historyFullEntry(), dump)

	r := startPty(t, bin, env, []string{"history", "1"}, 80, 24)
	frames := waitFrameCount(t, r, dump, 1)
	if got := selectedPackage(t, frames[0]); got != "nano" {
		t.Fatalf("initial selection = %q, want nano", got)
	}
	r.send(t, "s")
	frames = waitFrameCount(t, r, dump, 2)
	info := frames[1]
	for _, want := range []string{"Package:", "nano", "Version:", "9.2_1", "(q) back to history"} {
		if !strings.Contains(info, want) {
			t.Errorf("removal package info missing %q:\n%s", want, info)
		}
	}
	for _, absent := range []string{"Installed:", "Recorded:", "Available:", "(q) back to summary"} {
		if strings.Contains(info, absent) {
			t.Errorf("removal package info still shows %q:\n%s", absent, info)
		}
	}
	r.send(t, "q")
	waitFrameCount(t, r, dump, 3)
	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("quit exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
}

//go:build linux

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// TestCLISummaryLoopPanicRestores exercises the production loop's panic path
// directly (AC-054): the terminal is restored, the warning is written after
// the restore, and the panic is re-raised.
func TestCLISummaryLoopPanicRestores(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	before, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState before: %v", err)
	}
	stderr := &bytes.Buffer{}
	st := &state{
		stdin:  slave,
		stdout: slave,
		stderr: stderr,
		color:  false,
		r: &runner{cfg: config.Config{
			Color: config.ColorConfig{Theme: config.DefaultTheme()},
		}},
	}
	model := &tui.SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []tui.Tab{{Label: "Upgrade", Rows: []tui.PkgRow{
			{Name: "libfoo", New: "1.3_1", Size: "1 MiB"},
		}}},
		Download:  "1.0 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "1.0 MiB",
	}

	oldPanic := screenPanic
	screenPanic = func() bool { return true }
	defer func() { screenPanic = oldPanic }()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the CLI loop swallowed the injected panic")
			}
		}()
		st.runSummaryScreen(slave, model)
	}()

	after, err := term.GetState(slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(before) {
		t.Errorf("termios not restored after the CLI loop panic")
	}
	if !strings.Contains(stderr.String(), "full-screen interface failed") {
		t.Errorf("panic warning missing after restore:\n%s", stderr.String())
	}
}

//go:build linux

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// setPtySize sets a pseudo-terminal's window size for the full-width golden
// runs.
func setPtySize(f *os.File, cols, rows int) error {
	ws := winsize{ws_col: uint16(cols), ws_row: uint16(rows)}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return errno
	}
	return nil
}

// TestSummaryFullWidthGoldensThroughTheBuiltBinary drives the built binary on
// a real 80- and 100-column terminal, where FR-062 asks for full-width tables
// and the same rule width. The pty translates LF to CRLF on output, so the
// captured bytes are normalised back to LF before the golden comparison.
func TestSummaryFullWidthGoldensThroughTheBuiltBinary(t *testing.T) {
	bin := buildBinary(t)
	for _, tt := range []struct {
		name   string
		cols   int
		golden string
	}{
		{"80 columns", 80, "summary-80.sgr"},
		{"100 columns", 100, "summary-100.sgr"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			query := writeSummaryGoldenQuery(t, dir)
			cfgDir, logPath := summaryGoldenConfig(t, dir, query)

			master, slave, err := openPty()
			if err != nil {
				t.Skipf("no pseudo-terminal: %v", err)
			}
			defer master.Close()
			if err := setPtySize(slave, tt.cols, 40); err != nil {
				slave.Close()
				t.Skipf("cannot size the pseudo-terminal: %v", err)
			}

			cmd := exec.Command(bin, "install", "--dry-run", "fixture")
			cmd.Env = append(os.Environ(),
				"XDG_CONFIG_HOME="+cfgDir,
				"XDG_STATE_HOME="+filepath.Join(dir, "state"),
				"XB_STUB_LOG="+logPath,
				"XB_STUB_STDOUT="+summaryGoldenPlan,
				"XB_STUB_STDERR=",
				"XB_STUB_HOLD=",
				"XB_STUB_OLD=",
				"COLUMNS=",
				"NO_COLOR=",
			)
			cmd.Stdin = nil
			cmd.Stdout = slave
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				slave.Close()
				t.Fatal(err)
			}
			slave.Close()
			outCh := make(chan string, 1)
			go func() {
				var b strings.Builder
				buf := make([]byte, 4096)
				for {
					n, err := master.Read(buf)
					b.Write(buf[:n])
					if err != nil {
						break
					}
				}
				outCh <- b.String()
			}()
			if err := cmd.Wait(); err != nil {
				t.Fatalf("run: %v\n%s", err, stderr.String())
			}
			got := strings.ReplaceAll(<-outCh, "\r\n", "\n")
			if want := readSummaryGolden(t, tt.golden); got != want {
				t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", tt.golden, got, want)
			}
		})
	}
}

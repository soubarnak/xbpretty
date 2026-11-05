//go:build xbpretty_testroot

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/soubarnak/xbpretty/internal/tui"
)

// The #76 PTY harness needs to inspect every drawn frame and to force a
// failed init/render exactly once. Both hooks exist only in the testroot
// build, so the release binary keeps tui.Open/tui.RenderSummary untouched.
//
//	XBPRETTY_TEST_TUI_DUMP=<dir>  write each frame as <seq>.txt
//	XBPRETTY_TEST_TUI_FAIL=init   fail tui.Open
//	XBPRETTY_TEST_TUI_FAIL=render fail the first Screen.Draw
func init() {
	if dir := os.Getenv("XBPRETTY_TEST_TUI_DUMP"); dir != "" {
		var mu sync.Mutex
		seq := 0
		screenDumpFn = func(buf *tui.Buffer) {
			mu.Lock()
			defer mu.Unlock()
			seq++
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return
			}
			name := filepath.Join(dir, fmt.Sprintf("%04d.txt", seq))
			tmp := name + ".tmp"
			if err := os.WriteFile(tmp, []byte(buf.Text()), 0o644); err != nil {
				return
			}
			_ = os.Rename(tmp, name)
		}
	}
	switch os.Getenv("XBPRETTY_TEST_TUI_FAIL") {
	case "init":
		openScreenFn = func(in, out *os.File, color, mouse bool) (*tui.Screen, error) {
			return nil, errors.New("injected init failure")
		}
	case "render":
		var once sync.Once
		drawScreenFn = func(scr *tui.Screen, buf *tui.Buffer) error {
			failed := false
			once.Do(func() {
				failed = true
			})
			if failed {
				return errors.New("injected render failure")
			}
			return scr.Draw(buf)
		}
	}
}

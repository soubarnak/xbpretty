package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestPredownloadSIGTERMExits143 pins BF-21 (FR-074): a signal that stops the
// cache fill exits 128 plus the signal number, so SIGTERM exits 143 and SIGINT
// keeps 130. The fill removes `.part`, and no child starts.
func TestPredownloadSIGTERMExits143(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		want int
	}{
		{"SIGTERM", syscall.SIGTERM, 143},
		{"SIGINT", syscall.SIGINT, 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.root = true
			cache := t.TempDir()
			e.useCacheDir(cache)
			f := newRepoFixture(t)

			pkgver := "alpha-1.0_1"
			content := []byte("archive")
			sha := f.add(pkgver, "x86_64", content)
			e.set("XB_STUB_STDOUT", planLine(pkgver, f.URL(), len(content)+16))
			e.set("XB_STUB_HASHES", hashLine(pkgver, sha, f.URL()))

			f.mu.Lock()
			f.block = make(chan struct{})
			f.started = make(chan string, 4)
			started := f.started
			block := f.block
			f.mu.Unlock()

			type runResult struct {
				code int
				errs string
			}
			done := make(chan runResult, 1)
			go func() {
				code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
				done <- runResult{code: code, errs: errs}
			}()

			select {
			case <-started:
			case <-time.After(10 * time.Second):
				close(block)
				t.Fatal("the archive transfer never started")
			}
			if err := syscall.Kill(os.Getpid(), tc.sig); err != nil {
				close(block)
				t.Fatalf("signalling the test process: %v", err)
			}

			result := <-done
			close(block)
			if result.code != tc.want {
				t.Fatalf("exit = %d, want %d\n%s", result.code, tc.want, result.errs)
			}
			if parts := partFiles(t, cache); len(parts) != 0 {
				t.Errorf("cancellation left .part files behind: %q", parts)
			}
			if ranRealCommand(e) {
				t.Errorf("a cancelled predownload still ran xbps: %q", e.commands())
			}
			if _, err := os.Stat(filepath.Join(cache, pkgver+".x86_64.xbps")); err == nil {
				t.Error("the cancelled archive was published")
			}
		})
	}
}

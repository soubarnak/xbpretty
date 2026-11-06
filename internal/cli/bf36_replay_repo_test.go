package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/soubarnak/xbpretty/internal/history"
)

// withRepositoryMetadata puts a wrapper ahead of the stub on PATH. The wrapper
// answers the replay's `-R -p pkgver,repository,filename-size` metadata query
// the way xbps prints it, one value per line, and hands every other call to
// the stub unchanged. An empty repo answers nothing, which is an unknown
// metadata answer.
func withRepositoryMetadata(t *testing.T, dir string, env testEnv, repo, size string) testEnv {
	t.Helper()
	stub, err := filepath.Abs("../../testdata/stub/xbps-query")
	if err != nil {
		t.Fatal(err)
	}
	wrapDir := filepath.Join(dir, "meta-bin")
	if err := os.MkdirAll(wrapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  "-R -p pkgver,repository,filename-size "*)
    if [ -n "${XB_TEST_META_REPO:-}" ]; then
      printf '%%s\n%%s\n%%s\n' "$4" "$XB_TEST_META_REPO" "$XB_TEST_META_SIZE"
    fi
    exit 0 ;;
esac
exec '%s' "$@"
`, stub)
	if err := os.WriteFile(filepath.Join(wrapDir, "xbps-query"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return env.
		with("PATH", wrapDir+":"+env["PATH"]).
		with("XB_TEST_META_REPO", repo).
		with("XB_TEST_META_SIZE", size)
}

// TestUndoRestoreUsesResolvedRepositoryAndSize pins BF-36: an undo restores
// the old version from the repository that serves it, and records that
// repository and its archive size, not the after version's. An unknown
// answer keeps the recorded repository and leaves the size at zero, so the
// predownload body limit is not enforced.
func TestUndoRestoreUsesResolvedRepositoryAndSize(t *testing.T) {
	bin := testrootBinary(t)
	cases := []struct {
		name     string
		repo     string // what the metadata query answers; empty is unknown
		size     string
		wantRepo string
		wantSize uint64
	}{
		{
			name:     "resolved",
			repo:     "file:///var/db/xbps/previous",
			size:     "12KB",
			wantRepo: "file:///var/db/xbps/previous",
			wantSize: 12000,
		},
		{
			name:     "unknown",
			wantRepo: replayRepoURL,
			wantSize: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			store := historyDirOf(dir)
			writeReplayEntry(t, store, history.Entry{
				Command:  []string{"upgrade", "figlet"},
				Packages: []history.Transition{replayTransition(history.OpUpgrade, "figlet", "2.2.5_6", "2.2.6_1")},
			})
			env := replayEnv(t, dir, "uu figlet-2.2.6_1 figlet\n", "uu figlet-2.2.5_6 figlet\n")
			env = withRepositoryMetadata(t, dir, env, c.repo, c.size)

			out, errs, code := runBinary(t, bin, env, "undo", "1", "-y")
			if code != 0 {
				t.Fatalf("undo exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
			}
			replay := lastEntry(t, store)
			if len(replay.Packages) != 1 {
				t.Fatalf("replay packages = %+v, want one restore", replay.Packages)
			}
			got := replay.Packages[0]
			if got.Repository != c.wantRepo {
				t.Errorf("restored repository = %q, want %q", got.Repository, c.wantRepo)
			}
			if got.Size != c.wantSize {
				t.Errorf("restored size = %d, want %d", got.Size, c.wantSize)
			}
		})
	}
}

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestZeroDependencyBuild is AC-055's build half: the engine adds no module
// requirement, the whole tree builds with CGO disabled, and the non-Linux
// stubs compile.
func TestZeroDependencyBuild(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	for _, line := range strings.Split(string(gomod), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "require ") || trimmed == "require (" {
			t.Errorf("go.mod gained a module requirement: %q", line)
		}
	}

	build := func(env []string, pkg string) {
		t.Helper()
		cmd := exec.Command("go", "build", pkg)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("go build %s with %v: %v\n%s", pkg, env, err, out)
		}
	}
	build([]string{"CGO_ENABLED=0"}, "./...")
	// The non-Linux termios stub must compile even though the cli's own
	// terminal probe is Linux-only (a pre-existing platform boundary).
	for _, goos := range []string{"darwin", "windows"} {
		build([]string{"CGO_ENABLED=0", "GOOS=" + goos, "GOARCH=amd64"}, "./internal/term")
		build([]string{"CGO_ENABLED=0", "GOOS=" + goos, "GOARCH=amd64"}, "./internal/tui")
	}
}

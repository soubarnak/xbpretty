//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bf16RealRunScript is a fake xbps-install: the plan answers the dry run, and
// the real run prints a package's install message, two message lines, and a
// stderr warning, then succeeds. None of these lines is telemetry the live view
// parses.
func bf16RealRunScript() string {
	return `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
  -*y*)
    printf '[*] curl-8.22.0_1 install message:\n'
    printf 'Run update-grub to refresh the boot menu\n'
    printf 'then reboot to use the new kernel\n'
    sleep 0.1
    printf 'WARNING: x\n' >&2
    exit 0
    ;;
esac
exit 0
`
}

// TestLiveRunInsertsUnparsedChildLines is BF-16 on a real terminal: a successful
// live run keeps the package message and the stderr warning. They appear above
// the box, and the box is torn down afterwards.
func TestLiveRunInsertsUnparsedChildLines(t *testing.T) {
	bin, env := syncRootEnv(t, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xbps-install"), []byte(bf16RealRunScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	env = env.
		with("PATH", dir+string(os.PathListSeparator)+env["PATH"]).
		with("XB_STUB_STDOUT", planFixture)
	p := startPtyAll(t, bin, env, []string{"install", "-y", "--no-auto-remove", "curl"}, 90, 30)
	code := p.exitCode(t)
	out := p.cap.String()
	if code != 0 {
		t.Fatalf("exit = %d\n%q", code, out)
	}
	last := strings.LastIndex(out, "╰")
	for _, line := range []string{
		"[*] curl-8.22.0_1 install message:",
		"Run update-grub to refresh the boot menu",
		"then reboot to use the new kernel",
		"WARNING: x",
	} {
		at := strings.Index(out, line)
		if at < 0 {
			t.Errorf("the child line %q never reached the terminal:\n%q", line, out)
			continue
		}
		if last >= 0 && at > last {
			t.Errorf("the child line %q was printed after the box:\n%q", line, out)
		}
	}
	if clearAt := strings.LastIndex(out, "\x1b[J"); clearAt < last {
		t.Errorf("the box was not torn down after the run:\n%q", out)
	}
}

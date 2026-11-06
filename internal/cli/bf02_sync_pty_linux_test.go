//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bf02SyncScript is a fake xbps-install whose -S step runs the given body.
// The plan branch (-n) answers from XB_STUB_STDOUT, as the stub does.
func bf02SyncScript(syncBody string) string {
	return `#!/bin/sh
case "${1:-}" in
  -*n*)
    printf '%s' "${XB_STUB_STDOUT:-}"
    exit 0
    ;;
  -S)
` + syncBody + `
    ;;
esac
exit 0
`
}

// bf02SyncEnv builds the built-binary environment with a fake xbps-install
// first on PATH, so the sync step runs the script's -S body.
func bf02SyncEnv(t *testing.T, syncBody string) (string, testEnv) {
	t.Helper()
	bin, env := syncRootEnv(t, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xbps-install"), []byte(bf02SyncScript(syncBody)), 0o755); err != nil {
		t.Fatal(err)
	}
	env = env.with("PATH", dir+string(os.PathListSeparator)+env["PATH"])
	env = env.with("XB_STUB_STDOUT", updatePlanFixture)
	return bin, env
}

const bf02TwoRepos = `
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...\n"
    sleep 0.05
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata' ...\n"
    exit 0`

// TestUpgradeSyncUsesUpdateWidget is BF-02 on a real terminal: the upgrade
// family's sync step draws nala's Updating Package Lists widget and does not
// print xbps's raw repository lines.
func TestUpgradeSyncUsesUpdateWidget(t *testing.T) {
	bin, env := bf02SyncEnv(t, bf02TwoRepos)
	p := startPtyAll(t, bin, env, []string{"upgrade", "--dry-run"}, 90, 30)
	code := p.exitCode(t)
	out := p.cap.String()
	if code != 0 {
		t.Fatalf("exit = %d\n%q", code, out)
	}
	if !strings.Contains(out, "Updating Package Lists") {
		t.Errorf("the sync step did not draw the update widget:\n%q", out)
	}
	if strings.Contains(out, "[*] Updating repository") {
		t.Errorf("the raw xbps repository lines reached the terminal:\n%q", out)
	}
}

// bf02KeyImportBody prints xbps's new-key block. The prompt has no newline, as
// xbps prints it, and the step reads one answer from stdin.
const bf02KeyImportBody = `
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...\n"
    printf "https://repo-default.voidlinux.org/current/x86_64-repodata' repository has been RSA signed by \"Void Linux\"\n"
    printf "Fingerprint: fa:d1:00:32:ce:b1:b9:fd:73:05:05:76:75:54:37:25\n"
    printf "Do you want to import this public key? [Y/n] "
    read answer
    printf '%s' "$answer" > "$XB_STUB_ANSWER"
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata' ...\n"
    exit 0`

// TestSyncKeyImportPromptIsVisible is BF-02's key-import rule: the signer and
// fingerprint block and the prompt reach the terminal unboxed, and the answer
// typed on the terminal reaches the child.
func TestSyncKeyImportPromptIsVisible(t *testing.T) {
	bin, env := bf02SyncEnv(t, bf02KeyImportBody)
	answerPath := filepath.Join(t.TempDir(), "answer")
	env = env.with("XB_STUB_ANSWER", answerPath)
	p := startPtyAll(t, bin, env, []string{"upgrade", "--dry-run"}, 90, 30)

	const prompt = "Do you want to import this public key? [Y/n] "
	s := p.waitFor(t, "the key-import prompt", func(s string) bool {
		return strings.Contains(s, prompt)
	})
	fingerprintAt := strings.Index(s, "Fingerprint: fa:d1")
	promptAt := strings.Index(s, prompt)
	if fingerprintAt < 0 || fingerprintAt > promptAt {
		t.Fatalf("the key block is missing or after the prompt:\n%q", s)
	}
	if strings.Contains(s[fingerprintAt:promptAt], "│") {
		t.Errorf("the key block was drawn inside the box:\n%q", s[fingerprintAt:promptAt])
	}

	p.send(t, "y\n")
	if code := p.exitCode(t); code != 0 {
		t.Fatalf("exit = %d\n%q", code, p.cap.String())
	}
	got, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatalf("the child never read an answer: %v", err)
	}
	if string(got) != "y" {
		t.Errorf("the child read %q, want %q", got, "y")
	}
}

// bf02FailBody fails the sync with one stderr error, as xbps does when an
// index cannot be opened.
const bf02FailBody = `
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...\n"
    printf 'ERROR: failed to open repodata: /var/db/xbps/x86_64-repodata\n' >&2
    exit 2`

// TestSyncFailureDumpsOutputAndHint is BF-02's failure rule for every caller:
// the widget is cleared, the child's error and the stale-index hint are
// printed, and the exit code is xbps's. The error text appears once.
func TestSyncFailureDumpsOutputAndHint(t *testing.T) {
	const raw = "ERROR: failed to open repodata"
	for _, args := range [][]string{{"update"}, {"upgrade", "--dry-run"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			bin, env := bf02SyncEnv(t, bf02FailBody)
			p := startPtyAll(t, bin, env, args, 90, 30)
			code := p.exitCode(t)
			out := p.cap.String()
			if code != 2 {
				t.Fatalf("exit = %d, want 2\n%q", code, out)
			}
			if n := strings.Count(out, raw); n != 1 {
				t.Errorf("the error appeared %d times, want once:\n%q", n, out)
			}
			hintAt := strings.Index(out, "hint:")
			if hintAt < 0 || hintAt < strings.Index(out, raw) {
				t.Errorf("the stale-index hint is missing or before the error:\n%q", out)
			}
			if clearAt := strings.LastIndex(out, "\x1b[J"); clearAt < 0 || clearAt > hintAt {
				t.Errorf("the widget was not cleared before the hint:\n%q", out)
			}
		})
	}
}

// TestUpgradeSyncPlainWhenPiped: with stderr redirected there is no live view,
// so the sync's raw repository lines are forwarded as xbps printed them, and no
// box is drawn (FR-013; scripts depend on the raw text).
func TestUpgradeSyncPlainWhenPiped(t *testing.T) {
	bin, env := bf02SyncEnv(t, bf02TwoRepos)
	out, errs, code := runBinary(t, bin, env, "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, errs)
	}
	if !strings.Contains(errs, "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata'") {
		t.Errorf("the raw repository line was not forwarded:\n%s", errs)
	}
	if strings.Contains(errs, "╭") || strings.Contains(errs, "Updating Package Lists") {
		t.Errorf("a live view drew on a redirected stderr:\n%s", errs)
	}
	if strings.Contains(out, "Updating repository") {
		t.Errorf("the sync output reached stdout:\n%s", out)
	}
}

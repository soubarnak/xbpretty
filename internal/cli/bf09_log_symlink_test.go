package cli

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// BF-09: under sudo or doas root appends the transaction log inside the
// invoking user's home, which that user can rewrite. A symlink or a hard link
// on the log path must never make root append to, or chown, a file outside the
// state home. The transaction itself still succeeds; only the log is refused.

const victimContent = "do not touch\n"

// newVictim writes a file outside any state home, the target an attack would
// aim at, and returns its path.
func newVictim(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(path, []byte(victimContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertVictimUnchanged(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading victim: %v", err)
	}
	if string(got) != victimContent {
		t.Errorf("victim was modified:\n%q", got)
	}
}

// TestWriteLogRefusesSymlinkedLogFile is the in-process reproduction. The log
// path is a symlink to a victim file; before the fix root appended to it.
func TestWriteLogRefusesSymlinkedLogFile(t *testing.T) {
	e := newEnv(t)
	e.root = true
	victim := newVictim(t)
	stateDir := filepath.Join(e.dir, "state", "xbpretty")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(stateDir, "transactions.log")); err != nil {
		t.Fatal(err)
	}

	code, _, errs := e.run("xbpretty", "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0: the transaction must still succeed\n%s", code, errs)
	}
	assertVictimUnchanged(t, victim)
	if !strings.Contains(errs, "transaction log unavailable") {
		t.Errorf("stderr = %q, want the transaction log warning", errs)
	}
}

// invokerRootEnv is a built-binary root run whose sudo invoker is the test's own
// user. The invoker's passwd entry resolves, so the uid and gid checks run for
// real, and every chown is to the uid the test already owns, which needs no
// root. It skips when the test user has no passwd entry.
func invokerRootEnv(t *testing.T) (string, testEnv, string) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Skipf("the test user has no passwd entry: %v", err)
	}
	bin := testrootBinary(t)
	dir := t.TempDir()
	home := filepath.Join(dir, "invoker")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	env := cleanEnv(t, dir).
		with("XBPRETTY_TEST_EUID", "0").
		with("XBPRETTY_TEST_TRUST_UID", strconv.Itoa(os.Getuid())).
		with("XBPRETTY_TEST_SUDO_HOME", home).
		with("SUDO_USER", current.Username).
		with("XB_STUB_RUN_STDOUT", "").
		with("XB_STUB_RUN_STDERR", "")
	return bin, env, home
}

// TestWriteLogRefusesSymlinkedStateDirUnderInvoker covers the directory case:
// the xbpretty state directory is a symlink into a victim directory.
func TestWriteLogRefusesSymlinkedStateDirUnderInvoker(t *testing.T) {
	bin, env, home := invokerRootEnv(t)
	victimDir := t.TempDir()
	stateParent := filepath.Join(home, ".local", "state")
	if err := os.MkdirAll(stateParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDir, filepath.Join(stateParent, "xbpretty")); err != nil {
		t.Fatal(err)
	}

	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0: the transaction must still succeed\n%s", code, errs)
	}
	entries, err := os.ReadDir(victimDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a log was written through the symlinked state directory: %v", entries)
	}
	if !strings.Contains(errs, "transaction log unavailable") {
		t.Errorf("stderr = %q, want the transaction log warning", errs)
	}
}

// TestWriteLogRefusesSymlinkedLogUnderInvoker covers the file case under sudo:
// the state directory is real, the log inside it is a symlink.
func TestWriteLogRefusesSymlinkedLogUnderInvoker(t *testing.T) {
	bin, env, home := invokerRootEnv(t)
	victim := newVictim(t)
	stateDir := filepath.Join(home, ".local", "state", "xbpretty")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(stateDir, "transactions.log")); err != nil {
		t.Fatal(err)
	}

	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	assertVictimUnchanged(t, victim)
	if !strings.Contains(errs, "transaction log unavailable") {
		t.Errorf("stderr = %q, want the transaction log warning", errs)
	}
}

// TestWriteLogRefusesHardLinkedLogUnderInvoker covers the other way to point the
// log at a file the invoker does not own: a hard link. O_NOFOLLOW does not stop
// it, so the link count is checked before anything is written or chowned. Under
// a plain root run the path is root's own, so only the invoker case needs it.
func TestWriteLogRefusesHardLinkedLogUnderInvoker(t *testing.T) {
	bin, env, home := invokerRootEnv(t)
	victim := newVictim(t)
	stateDir := filepath.Join(home, ".local", "state", "xbpretty")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(victim, filepath.Join(stateDir, "transactions.log")); err != nil {
		t.Skipf("hard links unavailable here: %v", err)
	}

	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	assertVictimUnchanged(t, victim)
	if !strings.Contains(errs, "transaction log unavailable") {
		t.Errorf("stderr = %q, want the transaction log warning", errs)
	}
}

// TestWriteLogUnderInvokerWritesTheInvokersLog is the positive control: with
// no symlink on the path the entry lands in the invoker's own state home, which
// the walk creates and the log open accepts.
func TestWriteLogUnderInvokerWritesTheInvokersLog(t *testing.T) {
	bin, env, home := invokerRootEnv(t)

	_, errs, code := runBinary(t, bin, env, "install", "figlet", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "state", "xbpretty", "transactions.log"))
	if err != nil {
		t.Fatalf("the log is missing from the invoker's state home: %v\n%s", err, errs)
	}
	if !strings.Contains(string(raw), "install figlet-2.2.5_6") {
		t.Errorf("log line = %q, want the install entry", raw)
	}
	if strings.Contains(errs, "transaction log unavailable") {
		t.Errorf("the log was refused for a clean state home:\n%s", errs)
	}
}

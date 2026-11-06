package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// writeExecutable puts an executable placeholder at a temp path and returns it.
func writeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// ResolveFor("uhelper") checks the configured xbps-uhelper path, the same way
// it checks the other xbps binaries. Before the case existed it was a silent
// no-op, so a missing xbps-uhelper was never reported.
func TestResolveForUhelperChecksTheConfiguredPath(t *testing.T) {
	present := writeExecutable(t, "xbps-uhelper")
	cfg := Defaults()
	cfg.Xbps.UhelperPath = filepath.Join(t.TempDir(), "xbps-uhelper-absent")

	if err := cfg.ResolveFor("uhelper"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("ResolveFor(uhelper) with a missing binary = %v, want ErrNotInstalled", err)
	}

	cfg.Xbps.UhelperPath = present
	if err := cfg.ResolveFor("uhelper"); err != nil {
		t.Fatalf("ResolveFor(uhelper) with an executable = %v, want nil", err)
	}
}

// A command that never names uhelper must not start failing when xbps-uhelper
// is absent: the new case only runs for a list that includes it.
func TestResolveForIgnoresUhelperWhenNotRequested(t *testing.T) {
	cfg := Defaults()
	cfg.Xbps.InstallPath = writeExecutable(t, "xbps-install")
	cfg.Xbps.UhelperPath = filepath.Join(t.TempDir(), "xbps-uhelper-absent")

	if err := cfg.ResolveFor("install"); err != nil {
		t.Fatalf("ResolveFor(install) with xbps-uhelper absent = %v, want nil", err)
	}
}

// fakeSudoInvoker makes the test process act as root under sudo for a user
// whose passwd home is home. The uid and gid are the test's own, so the real
// lchown and chown calls the walk makes succeed without root.
func fakeSudoInvoker(t *testing.T, home string) {
	t.Helper()
	origEuid, origLookup, origLchown, origOwner := euid, lookupUser, lchown, ownerOf
	t.Cleanup(func() {
		euid, lookupUser, lchown, ownerOf = origEuid, origLookup, origLchown, origOwner
	})
	euid = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		if name != "invoker" {
			return nil, fmt.Errorf("no such user %q", name)
		}
		return &user.User{
			Username: name,
			HomeDir:  home,
			Uid:      strconv.Itoa(os.Getuid()),
			Gid:      strconv.Itoa(os.Getgid()),
		}, nil
	}
	t.Setenv("SUDO_USER", "invoker")
	t.Setenv("DOAS_USER", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// recordLchown replaces lchown with a recorder that never touches the disk.
func recordLchown(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	orig := lchown
	t.Cleanup(func() { lchown = orig })
	lchown = func(name string, uid, gid int) error {
		calls = append(calls, fmt.Sprintf("%s %d:%d", name, uid, gid))
		return nil
	}
	return &calls
}

// BF-10: on a fresh account the first sudo run used to create ~/.local and
// ~/.local/state as root. Every directory it creates under the invoker's home
// must now be handed to the invoker, in order from the home down.
func TestStateDirCreatesInvokerOwnedAncestors(t *testing.T) {
	home := t.TempDir()
	fakeSudoInvoker(t, home)
	calls := recordLchown(t)

	dir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "xbpretty")
	if dir != want {
		t.Fatalf("StateDir() = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("state directory was not created: %v", err)
	}
	owner := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	wantCalls := []string{
		filepath.Join(home, ".local") + " " + owner,
		filepath.Join(home, ".local", "state") + " " + owner,
		want + " " + owner,
	}
	if fmt.Sprint(*calls) != fmt.Sprint(wantCalls) {
		t.Errorf("lchown calls = %v, want %v", *calls, wantCalls)
	}
}

// Without an invoking user the state directory is ordinary: plain MkdirAll and
// no ownership change at all.
func TestStateDirWithoutInvokerChownsNothing(t *testing.T) {
	origEuid := euid
	t.Cleanup(func() { euid = origEuid })
	euid = func() int { return 1000 }
	t.Setenv("SUDO_USER", "")
	t.Setenv("DOAS_USER", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	calls := recordLchown(t)

	dir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("state directory was not created: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("an ordinary run changed ownership: %v", *calls)
	}
}

// A symlink anywhere on the path from the invoker's home is refused, and
// nothing is created or chowned on the far side of it.
func TestStateDirRefusesSymlinkedComponent(t *testing.T) {
	home := t.TempDir()
	fakeSudoInvoker(t, home)
	calls := recordLchown(t)
	victim := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(home, ".local")); err != nil {
		t.Fatal(err)
	}

	if _, err := StateDir(); err == nil {
		t.Fatal("StateDir followed a symlink under the invoker's home")
	}
	entries, err := os.ReadDir(victim)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("state was created through the symlink: %v", entries)
	}
	if len(*calls) != 0 {
		t.Errorf("ownership changed behind the symlink: %v", *calls)
	}
}

// A component owned by anyone other than root or the invoker is refused, and
// the walk stops before it creates anything below it.
func TestStateDirRefusesForeignOwner(t *testing.T) {
	home := t.TempDir()
	fakeSudoInvoker(t, home)
	calls := recordLchown(t)
	if err := os.Mkdir(filepath.Join(home, ".local"), 0o755); err != nil {
		t.Fatal(err)
	}
	ownerOf = func(fs.FileInfo) (uint32, bool) { return 4242, true }

	if _, err := StateDir(); err == nil {
		t.Fatal("StateDir accepted a directory owned by a foreign uid")
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "state")); err == nil {
		t.Error("a directory was created below a foreign-owned component")
	}
	if len(*calls) != 0 {
		t.Errorf("ownership changed under a foreign owner: %v", *calls)
	}
}

// A root-owned state directory left by an earlier sudo run is handed to the
// invoker; an existing directory the invoker already owns is left alone.
func TestStateDirChownsRootOwnedLeaf(t *testing.T) {
	home := t.TempDir()
	fakeSudoInvoker(t, home)
	calls := recordLchown(t)
	leaf := filepath.Join(home, ".local", "state", "xbpretty")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	ownerOf = func(fi fs.FileInfo) (uint32, bool) {
		if fi.Name() == "xbpretty" {
			return 0, true
		}
		return uint32(os.Getuid()), true
	}

	if _, err := StateDir(); err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	want := fmt.Sprintf("%s %d:%d", leaf, os.Getuid(), os.Getgid())
	if fmt.Sprint(*calls) != fmt.Sprint([]string{want}) {
		t.Errorf("lchown calls = %v, want only %q", *calls, want)
	}
}

// SafeInvokerDir is the shared walk on its own: a clean existing tree passes,
// and a symlink component fails, both without creating anything.
func TestSafeInvokerDirChecksWithoutCreating(t *testing.T) {
	home := t.TempDir()
	fakeSudoInvoker(t, home)
	clean := filepath.Join(home, ".local", "state")
	if err := os.MkdirAll(clean, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SafeInvokerDir(clean); err != nil {
		t.Fatalf("SafeInvokerDir on a clean tree: %v", err)
	}

	if err := os.Rename(filepath.Join(home, ".local"), filepath.Join(home, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "moved"), filepath.Join(home, ".local")); err != nil {
		t.Fatal(err)
	}
	if err := SafeInvokerDir(clean); err == nil {
		t.Fatal("SafeInvokerDir accepted a symlinked component")
	}
	if _, err := os.Stat(filepath.Join(home, "moved", "state", "xbpretty")); err == nil {
		t.Error("SafeInvokerDir created a directory")
	}
}

// OpenTransactionLog never follows a symlink at the log path, even without an
// invoking user.
func TestOpenTransactionLogRefusesSymlink(t *testing.T) {
	origEuid := euid
	t.Cleanup(func() { euid = origEuid })
	euid = func() int { return 1000 }
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "transactions.log")
	if err := os.Symlink(victim, logPath); err != nil {
		t.Fatal(err)
	}

	if f, err := OpenTransactionLog(logPath); err == nil {
		f.Close()
		t.Fatal("OpenTransactionLog followed a symlink")
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep\n" {
		t.Errorf("the symlink target changed: %q", got)
	}
}

// Under an invoking user a hard link to a file root could write is refused:
// writing through it would change a file the invoker does not own.
func TestOpenTransactionLogRefusesHardLinkUnderInvoker(t *testing.T) {
	home := t.TempDir()
	fakeSudoInvoker(t, home)
	stateDir := filepath.Join(home, ".local", "state", "xbpretty")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(stateDir, "transactions.log")
	if err := os.Link(victim, logPath); err != nil {
		t.Skipf("hard links unavailable here: %v", err)
	}

	if f, err := OpenTransactionLog(logPath); err == nil {
		f.Close()
		t.Fatal("OpenTransactionLog accepted a hard-linked log under an invoker")
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep\n" {
		t.Errorf("the hard-linked file changed: %q", got)
	}
}

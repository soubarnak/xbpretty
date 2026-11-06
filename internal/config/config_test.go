package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// isolateSystemConfig points the system layer at an absent temp file so a
// developer's real /etc/xbpretty/config.toml cannot change a unit test.
func isolateSystemConfig(t *testing.T) {
	t.Helper()
	orig := systemConfigPath
	systemConfigPath = filepath.Join(t.TempDir(), "system-absent.toml")
	t.Cleanup(func() { systemConfigPath = orig })
}

func TestLoadMissingFileIsSilent(t *testing.T) {
	isolateSystemConfig(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with no config file: %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("got %+v, want defaults", cfg)
	}
}

func TestLoadReadsFile(t *testing.T) {
	isolateSystemConfig(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "xbpretty", "config.toml")
	if err := os.WriteFile(path, []byte("[Nala]\nassume_yes = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Nala.AssumeYes {
		t.Error("assume_yes was not read from the config file")
	}
	if !cfg.Nala.AutoRemove || !cfg.Nala.AutoUpdate || !cfg.Xbps.TransactionLog {
		t.Errorf("defaults were not preserved: %+v", cfg)
	}
}

// An existing default file that does not validate is a hard error, unlike a
// missing one (FR-053).
func TestLoadInvalidExistingFileIsHardError(t *testing.T) {
	isolateSystemConfig(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "xbpretty", "config.toml")
	if err := os.WriteFile(path, []byte("[Ui]\nmode = \"nope\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted an invalid file")
	}
	if !strings.Contains(err.Error(), path+":2:") {
		t.Errorf("error %q does not name the file and line", err)
	}
}

func TestLoadFileMissingIsHardError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile accepted a missing file")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the file", err)
	}
}

// A dot-relative program path is a path error, not a float error: TOML never
// accepts a leading dot in a float, so the number check must not claim it.
func TestLegacyDotPathMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.toml")
	if err := os.WriteFile(path, []byte("xbps_query_path = ./xbps-query\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile accepted ./xbps-query")
	}
	message := err.Error()
	if !strings.Contains(message, "must be a bare program name or an absolute path") {
		t.Errorf("error %q does not name the path rule", message)
	}
	if strings.Contains(message, "floats") {
		t.Errorf("error %q blames a float", message)
	}
}

func TestLoadFileReadsExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(path, []byte("[Color]\nmode = \"Always\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Color.Mode != ColorAlways {
		t.Errorf("Color.Mode = %q", cfg.Color.Mode)
	}
}

func TestErrorFormat(t *testing.T) {
	err := &Error{Path: "/etc/xbpretty/config.toml", Line: 7, Message: "floats are not supported"}
	if got, want := err.Error(), "config /etc/xbpretty/config.toml:7: floats are not supported"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	fileErr := &Error{Path: "/etc/xbpretty/config.toml", Message: "byte-order mark (BOM) is not supported"}
	if got, want := fileErr.Error(), "config /etc/xbpretty/config.toml: byte-order mark (BOM) is not supported"; got != want {
		t.Errorf("file-level Error() = %q, want %q", got, want)
	}
}

func TestPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join("/tmp/xdgtest", "xbpretty", "config.toml")
	if path != want {
		t.Errorf("got %q, want %q", path, want)
	}
}

// TestUserHomeFollowsSudoAndDoas pins FR-019's home resolution, the half of
// AC-012 and AC-019 the CLI cannot reach on its own: under sudo or doas the
// config file and the state home belong to the invoking user, not to root.
func TestUserHomeFollowsSudoAndDoas(t *testing.T) {
	origEuid, origLookup := euid, lookupUser
	t.Cleanup(func() { euid, lookupUser = origEuid, origLookup })

	rootHome := t.TempDir()
	aliceHome := t.TempDir()
	lookupUser = func(name string) (*user.User, error) {
		if name != "alice" {
			return nil, fmt.Errorf("no such user %q", name)
		}
		// The uid and gid are the test's own, so the state directory the
		// sudo case creates is chowned to a user the test may chown to.
		return &user.User{
			Username: "alice",
			HomeDir:  aliceHome,
			Uid:      strconv.Itoa(os.Getuid()),
			Gid:      strconv.Itoa(os.Getgid()),
		}, nil
	}

	tests := []struct {
		name     string
		root     bool
		sudoUser string
		doasUser string
		wantHome string
	}{
		{"sudo reads the invoking user's home", true, "alice", "", aliceHome},
		{"doas reads the invoking user's home", true, "", "alice", aliceHome},
		{"root outside sudo keeps root's own home", true, "", "", rootHome},
		{"an unelevated run ignores SUDO_USER", false, "alice", "", rootHome},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			euid = func() int {
				if tt.root {
					return 0
				}
				return 1000
			}
			t.Setenv("SUDO_USER", tt.sudoUser)
			t.Setenv("DOAS_USER", tt.doasUser)
			t.Setenv("HOME", rootHome)
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("XDG_STATE_HOME", "")

			path, err := Path()
			if err != nil {
				t.Fatalf("Path: %v", err)
			}
			wantPath := filepath.Join(tt.wantHome, ".config", "xbpretty", "config.toml")
			if path != wantPath {
				t.Errorf("Path() = %q, want %q", path, wantPath)
			}
			logPath, err := TransactionLog()
			if err != nil {
				t.Fatalf("TransactionLog: %v", err)
			}
			wantLog := filepath.Join(tt.wantHome, ".local", "state", "xbpretty", "transactions.log")
			if logPath != wantLog {
				t.Errorf("TransactionLog() = %q, want %q", logPath, wantLog)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	got := FormatTimestamp(time.Date(2026, 10, 15, 3, 41, 1, 0, time.UTC))
	if got != "2026-10-15T03:41:01Z" {
		t.Errorf("got %q", got)
	}
}

func TestTransactionLogPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, err := TransactionLog()
	if err != nil {
		t.Fatalf("TransactionLog: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("xbpretty", "transactions.log")) {
		t.Errorf("unexpected path %q", path)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("state directory not created: %v", err)
	}
}

// TestInvokerFollowsSudoAndDoas pins FR-013's ownership rule: state written
// under a sudo or doas run belongs to the invoking user, and a run that is
// not elevated owes nobody a chown.
func TestInvokerFollowsSudoAndDoas(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("cannot resolve the current user: %v", err)
	}
	wantUID, err := strconv.Atoi(me.Uid)
	if err != nil {
		t.Skipf("the current user has a non-numeric uid %q", me.Uid)
	}
	wantGID, err := strconv.Atoi(me.Gid)
	if err != nil {
		t.Skipf("the current user has a non-numeric gid %q", me.Gid)
	}

	orig := euid
	t.Cleanup(func() { euid = orig })

	tests := []struct {
		name     string
		root     bool
		sudoUser string
		doasUser string
		wantOK   bool
	}{
		{name: "an unelevated run owes no chown", root: false, sudoUser: me.Username},
		{name: "sudo names the invoking user", root: true, sudoUser: me.Username, wantOK: true},
		{name: "doas names the invoking user", root: true, doasUser: me.Username, wantOK: true},
		{name: "root outside sudo owes no chown", root: true},
		{name: "an unknown invoking user owes no chown", root: true, sudoUser: "no-such-user-xbpretty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			euid = func() int {
				if tt.root {
					return 0
				}
				return 1000
			}
			t.Setenv("SUDO_USER", tt.sudoUser)
			t.Setenv("DOAS_USER", tt.doasUser)
			uid, gid, ok := Invoker()
			if ok != tt.wantOK {
				t.Fatalf("Invoker ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if uid != wantUID || gid != wantGID {
				t.Errorf("Invoker = %d:%d, want %d:%d", uid, gid, wantUID, wantGID)
			}
		})
	}
}

// TestTransactionLogPathReadsWithoutCreating pins ticket #21: the path a
// reader resolves follows the sudo/doas invoker like the writer's, and
// resolving it creates nothing.
func TestTransactionLogPathReadsWithoutCreating(t *testing.T) {
	origEuid, origLookup := euid, lookupUser
	t.Cleanup(func() { euid, lookupUser = origEuid, origLookup })
	aliceHome := t.TempDir()
	lookupUser = func(name string) (*user.User, error) {
		return &user.User{Username: name, HomeDir: aliceHome}, nil
	}
	euid = func() int { return 0 }
	t.Setenv("SUDO_USER", "alice")
	t.Setenv("DOAS_USER", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", "")

	got, err := TransactionLogPath()
	if err != nil {
		t.Fatalf("TransactionLogPath: %v", err)
	}
	want := filepath.Join(aliceHome, ".local", "state", "xbpretty", "transactions.log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(aliceHome, ".local")); err == nil {
		t.Error("resolving the path created directories")
	}
}

// TestSudoIgnoresXDGStateHome pins the second half of FR-054's invoking-user
// rule: the state directory follows the passwd home, not XDG_STATE_HOME.
func TestSudoIgnoresXDGStateHome(t *testing.T) {
	origEuid, origLookup := euid, lookupUser
	t.Cleanup(func() { euid, lookupUser = origEuid, origLookup })
	aliceHome := t.TempDir()
	euid = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		return &user.User{Username: name, HomeDir: aliceHome}, nil
	}
	t.Setenv("SUDO_USER", "alice")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, err := TransactionLogPath()
	if err != nil {
		t.Fatalf("TransactionLogPath: %v", err)
	}
	want := filepath.Join(aliceHome, ".local", "state", "xbpretty", "transactions.log")
	if path != want {
		t.Errorf("TransactionLogPath() = %q, want %q", path, want)
	}
	cfgPath, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if wantCfg := filepath.Join(aliceHome, ".config", "xbpretty", "config.toml"); cfgPath != wantCfg {
		t.Errorf("Path() = %q, want %q", cfgPath, wantCfg)
	}
}

// TestRelativeXDGStateHomeFallsBack pins the XDG base-directory rule: a
// relative path is not a usable state location.
func TestRelativeXDGStateHomeFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "relative/state")
	path, err := TransactionLogPath()
	if err != nil {
		t.Fatalf("TransactionLogPath: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "xbpretty", "transactions.log")
	if path != want {
		t.Errorf("TransactionLogPath() = %q, want %q", path, want)
	}
}

// TestSudoUserWinsOverDoasUser pins the precedence inside FR-054's invoking
// user rule.
func TestSudoUserWinsOverDoasUser(t *testing.T) {
	origEuid, origLookup := euid, lookupUser
	t.Cleanup(func() { euid, lookupUser = origEuid, origLookup })
	sudoHome, doasHome := t.TempDir(), t.TempDir()
	euid = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		switch name {
		case "sudo-user":
			return &user.User{Username: name, HomeDir: sudoHome}, nil
		case "doas-user":
			return &user.User{Username: name, HomeDir: doasHome}, nil
		}
		return nil, fmt.Errorf("no such user %q", name)
	}
	t.Setenv("SUDO_USER", "sudo-user")
	t.Setenv("DOAS_USER", "doas-user")
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(sudoHome, ".config", "xbpretty", "config.toml"); path != want {
		t.Errorf("Path() = %q, want SUDO_USER home %q", path, want)
	}
}

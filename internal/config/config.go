// Package config loads xbpretty's user configuration file.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Exit codes used when the config file itself is unusable.
const (
	ExitParseError = 2
)

// Error is a configuration rejection. It carries the file and, for a value
// or table error, the line. Line zero means the error is about the file as a
// whole (BOM, invalid UTF-8, unreadable file, size).
type Error struct {
	Path    string
	Line    int
	Message string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("config %s:%d: %s", e.Path, e.Line, e.Message)
	}
	return fmt.Sprintf("config %s: %s", e.Path, e.Message)
}

func fileError(path, message string) *Error {
	return &Error{Path: path, Message: message}
}

// Load reads the default layers (system, then user) and resolves them over the
// built-in defaults. Missing default layers are silent; an existing invalid
// layer is a hard error (FR-053, FR-054).
func Load() (Config, error) {
	res, err := LoadSelected("")
	if err != nil {
		return Defaults(), err
	}
	return res.Config, nil
}

// LoadFile reads one explicit configuration file. Unlike a default layer a
// missing or unreadable file is a hard error: this is the seam --config uses.
func LoadFile(path string) (Config, error) {
	res, err := LoadSelected(path)
	if err != nil {
		return Defaults(), err
	}
	return res.Config, nil
}

// Parse reads one TOML-subset configuration document and resolves it over the
// built-in defaults. The ten rev-5 keys are understood, but their warnings are
// discarded here: Parse is a single-document seam, not a layered run.
func Parse(path string, r io.Reader) (Config, error) {
	data, err := readConfigBytes(path, r)
	if err != nil {
		return Defaults(), err
	}
	doc, err := parseDocument(path, data)
	if err != nil {
		return Defaults(), err
	}
	partial, _, err := buildPartial(doc)
	if err != nil {
		if cfgErr, ok := err.(*Error); ok {
			cfgErr.Path = path
		}
		return Defaults(), err
	}
	cfg := Defaults()
	partial.mergeInto(&cfg)
	return cfg, nil
}

// readConfigBytes reads at most one mebibyte plus one byte so the parser can still
// name an oversized file.
func readConfigBytes(path string, r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxConfigBytes+1))
	if err != nil {
		return nil, fileError(path, fmt.Sprintf("cannot read configuration file: %v", err))
	}
	return data, nil
}

// Path resolves the default user config file location, honouring the absolute
// XDG_CONFIG_HOME rule and the sudo/doas calling-user lookup (FR-054).
func Path() (string, error) {
	base, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "xbpretty", "config.toml"), nil
}

// configHome is the user layer's base directory. When an invoking sudo/doas
// user is present that user's passwd home wins and XDG_CONFIG_HOME is ignored.
func configHome() (string, error) {
	home, invoking, err := invokingUser()
	if err != nil {
		return "", err
	}
	if invoking {
		return filepath.Join(home, ".config"), nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" && filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err = os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// invokerHomeOverride is empty in production. The build-tagged test hook sets
// it so the sudo/doas home rule is testable without a second account.
var invokerHomeOverride string

// invokingUser returns the passwd home of the user behind sudo/doas when
// xbpretty runs as root. ok is false for an ordinary run or plain root.
func invokingUser() (home string, ok bool, err error) {
	if euid() != 0 {
		return "", false, nil
	}
	name := os.Getenv("SUDO_USER")
	if name == "" {
		name = os.Getenv("DOAS_USER")
	}
	if name == "" {
		return "", false, nil
	}
	if invokerHomeOverride != "" {
		return invokerHomeOverride, true, nil
	}
	dir, err := homeOfUser(name)
	if err != nil {
		return "", false, err
	}
	return dir, true, nil
}

// userHome returns the home directory whose config applies to this run. When
// xbpretty runs as root through sudo or doas, the calling user's home wins.
func userHome() (string, error) {
	home, invoking, err := invokingUser()
	if err != nil {
		return "", err
	}
	if invoking {
		return home, nil
	}
	return os.UserHomeDir()
}

func homeOfUser(name string) (string, error) {
	u, err := lookupUser(name)
	if err != nil {
		return "", fmt.Errorf("looking up user %s: %w", name, err)
	}
	if u.HomeDir == "" {
		return "", fmt.Errorf("user %s has no home directory", name)
	}
	return u.HomeDir, nil
}

// euid is the effective-user check, replaceable so the root path is testable
// without running the suite as root.
var euid = os.Geteuid

// lookupUser resolves a user by name, replaceable so the sudo/doas home
// resolution is testable without a second account on the machine.
var lookupUser = user.Lookup

// Invoker returns the uid and gid of the user behind sudo or doas when
// xbpretty runs as root. State files written into that user's home belong to
// them, not to root (FR-013, AC-012). ok is false when the process runs as
// itself, or when no invoking user can be resolved; no chown is due then.
func Invoker() (uid, gid int, ok bool) {
	if euid() != 0 {
		return 0, 0, false
	}
	name := os.Getenv("SUDO_USER")
	if name == "" {
		name = os.Getenv("DOAS_USER")
	}
	if name == "" {
		return 0, 0, false
	}
	u, err := lookupUser(name)
	if err != nil {
		return 0, 0, false
	}
	uidNum, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, false
	}
	gidNum, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, false
	}
	return uidNum, gidNum, true
}

// ErrNotInstalled is reported when a configured xbps binary cannot be found.
var ErrNotInstalled = errors.New("xbps binary not found")

// ResolveFor checks that only the configured binaries the selected command
// uses exist and are executable (FR-053). The command table in internal/spec
// names the programs; this method maps those names to the configured paths.
// A duplicate is checked once, and an unknown name is ignored.
func (c Config) ResolveFor(names ...string) error {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		var path string
		switch name {
		case "install":
			path = c.Xbps.InstallPath
		case "remove":
			path = c.Xbps.RemovePath
		case "query":
			path = c.Xbps.QueryPath
		case "rindex":
			path = c.Xbps.RindexPath
		case "pkgdb":
			path = c.Xbps.PkgdbPath
		case "uhelper":
			path = c.Xbps.UhelperPath
		default:
			continue
		}
		if _, err := exec.LookPath(path); err != nil {
			return fmt.Errorf("%w: %s", ErrNotInstalled, path)
		}
	}
	return nil
}

// StateDir returns the directory holding xbpretty's state files, creating it
// on first use. Under sudo or doas the directories it creates belong to the
// invoking user, and a symlink on the way down is refused (BF-09, BF-10).
func StateDir() (string, error) {
	dir, err := stateDirPath()
	if err != nil {
		return "", err
	}
	if err := makeStateDir(dir); err != nil {
		return "", fmt.Errorf("creating state directory: %w", err)
	}
	return dir, nil
}

// makeStateDir creates dir. An ordinary run uses plain MkdirAll. An invoking
// user's directories are created one component at a time from their home, and
// each new one is handed to the invoker.
func makeStateDir(dir string) error {
	id, invoking, err := invoker()
	if err != nil {
		return err
	}
	if !invoking {
		return os.MkdirAll(dir, 0o755)
	}
	return walkInvokerDir(id, dir, true)
}

// invokerID is the identity behind sudo or doas. Files and directories under
// the invoker's home must belong to root or to the invoker.
type invokerID struct {
	home string
	uid  int
	gid  int
}

// invoker returns the user behind sudo or doas when xbpretty runs as root. ok
// is false for an ordinary run and for plain root. An invoker whose uid cannot
// be resolved is an error, because ownership could then not be checked.
func invoker() (id invokerID, ok bool, err error) {
	home, invoking, err := invokingUser()
	if err != nil || !invoking {
		return invokerID{}, false, err
	}
	uid, gid, resolved := Invoker()
	if !resolved {
		return invokerID{}, false, errors.New("cannot resolve the uid of the invoking user")
	}
	return invokerID{home: home, uid: uid, gid: gid}, true, nil
}

// lchown is os.Lchown in production. Tests replace it to observe the owner
// given to each directory the walk creates, without root.
var lchown = os.Lchown

// ownerOf reads the owning uid of a file. Tests replace it to exercise the
// foreign-owner refusal.
var ownerOf = fileOwner

// SafeInvokerDir checks dir and every component from the invoking user's home
// down to it. Each must exist, be a real directory rather than a symlink, and
// be owned by root or by the invoker. It creates nothing, and it returns nil
// when no invoking user is present, because an ordinary run owns its own state.
func SafeInvokerDir(dir string) error {
	id, invoking, err := invoker()
	if err != nil || !invoking {
		return err
	}
	return walkInvokerDir(id, dir, false)
}

// walkInvokerDir is the component walk BF-09 and BF-10 share. With create set, a
// missing component below the home is made with Mkdir and handed to the
// invoker with lchown before the walk goes on. A component that exists must
// pass the same checks either way. The home itself is never created.
func walkInvokerDir(id invokerID, dir string, create bool) error {
	rel, err := filepath.Rel(id.home, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is outside the invoking user's home %s", dir, id.home)
	}
	if _, err := checkInvokerComponent(id, id.home); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := id.home
	for i, part := range parts {
		current = filepath.Join(current, part)
		if _, err := os.Lstat(current); errors.Is(err, fs.ErrNotExist) && create {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			if err := lchown(current, id.uid, id.gid); err != nil {
				return fmt.Errorf("giving %s to the invoking user: %w", current, err)
			}
			continue
		}
		owner, err := checkInvokerComponent(id, current)
		if err != nil {
			return err
		}
		// The state directory itself may be a root-owned leftover from an
		// earlier sudo run; hand it to the invoker, as the log write always did.
		if create && i == len(parts)-1 && owner != uint32(id.uid) {
			if err := lchown(current, id.uid, id.gid); err != nil {
				return fmt.Errorf("giving %s to the invoking user: %w", current, err)
			}
		}
	}
	return nil
}

// checkInvokerComponent refuses p unless it is a real directory owned by root or
// by the invoker. It returns the owner it found.
func checkInvokerComponent(id invokerID, p string) (uint32, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return 0, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return 0, fmt.Errorf("%s is a symbolic link", p)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", p)
	}
	owner, ok := ownerOf(info)
	if !ok {
		return 0, fmt.Errorf("cannot read the owner of %s", p)
	}
	if owner != 0 && owner != uint32(id.uid) {
		return 0, fmt.Errorf("%s is owned by uid %d, not root or the invoking user", p, owner)
	}
	return owner, nil
}

// OpenTransactionLog opens the transaction log for appending, creating it when
// missing. The open refuses a symlink at the path. The file must be a regular
// file. Under an invoking user it must also be owned by root or by the invoker
// and have exactly one hard link, so a link to a file the invoker can write
// cannot make root append to it. The caller chowns the returned descriptor and
// never the path.
func OpenTransactionLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|logOpenFlags, 0o644)
	if err != nil {
		return nil, err
	}
	if err := checkTransactionLog(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// checkTransactionLog judges the opened log through its descriptor.
func checkTransactionLog(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", f.Name())
	}
	id, invoking, err := invoker()
	if err != nil || !invoking {
		return err
	}
	owner, ok := ownerOf(info)
	if !ok {
		return fmt.Errorf("cannot read the owner of %s", f.Name())
	}
	if owner != 0 && owner != uint32(id.uid) {
		return fmt.Errorf("%s is owned by uid %d, not root or the invoking user", f.Name(), owner)
	}
	links, ok := fileNlink(info)
	if !ok {
		return fmt.Errorf("cannot count the hard links of %s", f.Name())
	}
	if links != 1 {
		return fmt.Errorf("%s has %d hard links", f.Name(), links)
	}
	return nil
}

// transactionLogName is the file name of the transaction log in the state
// directory.
const transactionLogName = "transactions.log"

// stateDirPath is where the state directory is, without creating it.
func stateDirPath() (string, error) {
	home, invoking, err := invokingUser()
	if err != nil {
		return "", err
	}
	if invoking {
		return filepath.Join(home, ".local", "state", "xbpretty"), nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base != "" && filepath.IsAbs(base) {
		return filepath.Join(base, "xbpretty"), nil
	}
	home, err = os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "xbpretty"), nil
}

// TransactionLogPath is the path TransactionLog returns, for readers: it
// creates nothing, so reading the log never leaves a directory behind.
func TransactionLogPath() (string, error) {
	dir, err := stateDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, transactionLogName), nil
}

// TransactionLog is the path of the append-only transaction log.
func TransactionLog() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, transactionLogName), nil
}

// FormatTimestamp renders t in the ISO-8601 form the transaction log uses.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

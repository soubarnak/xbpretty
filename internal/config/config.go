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

// Load reads the default user configuration layer and resolves it over the
// built-in defaults. A missing file is not an error; an existing file that
// does not validate is (FR-053). The system layer and --config selection are
// later tickets; LoadFile is the seam they call.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Defaults(), err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Defaults(), nil
		}
		return Defaults(), fileError(path, fmt.Sprintf("cannot read configuration file: %v", err))
	}
	defer f.Close()
	return Parse(path, f)
}

// LoadFile reads one explicit configuration file. Unlike the default layer a
// missing or unreadable file is a hard error: this is the seam --config uses.
func LoadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Defaults(), fileError(path, fmt.Sprintf("cannot read configuration file: %v", err))
	}
	defer f.Close()
	return Parse(path, f)
}

// Parse reads one TOML-subset configuration document and resolves the rev 6
// schema over the defaults.
func Parse(path string, r io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxConfigBytes+1))
	if err != nil {
		return Defaults(), fileError(path, fmt.Sprintf("cannot read configuration file: %v", err))
	}
	doc, err := parseDocument(path, data, false)
	if err != nil {
		return Defaults(), err
	}
	cfg, err := resolve(doc)
	if err != nil {
		if cfgErr, ok := err.(*Error); ok {
			cfgErr.Path = path
		}
		return Defaults(), err
	}
	return cfg, nil
}

// Path resolves the default user config file location, honouring
// XDG_CONFIG_HOME and the sudo/doas calling-user lookup.
//
// #60 owns layered selection: the /etc layer, the absolute-XDG rule and
// ignoring XDG when an invoking SUDO_USER/DOAS_USER is present.
func Path() (string, error) {
	base, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "xbpretty", "config.toml"), nil
}

func configHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// userHome returns the home directory whose config applies to this run. When
// xbpretty runs as root through sudo or doas, the calling user's config wins.
func userHome() (string, error) {
	if euid() != 0 {
		return os.UserHomeDir()
	}
	if name := os.Getenv("SUDO_USER"); name != "" {
		return homeOfUser(name)
	}
	if name := os.Getenv("DOAS_USER"); name != "" {
		return homeOfUser(name)
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

// Resolve checks that every configured xbps binary exists and is executable,
// returning a per-command error naming what is missing.
//
// Rev 6 requires existence to be checked only for the binaries the selected
// command uses (FR-053); the shared command table that supplies command→binary
// ownership belongs to #62.
func (c Config) Resolve() error {
	for _, path := range []string{c.Xbps.InstallPath, c.Xbps.RemovePath, c.Xbps.QueryPath} {
		if _, err := exec.LookPath(path); err != nil {
			return fmt.Errorf("%w: %s", ErrNotInstalled, path)
		}
	}
	return nil
}

// StateDir returns the directory holding xbpretty's state files, creating it
// on first use.
func StateDir() (string, error) {
	dir, err := stateDirPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating state directory: %w", err)
	}
	return dir, nil
}

// transactionLogName is the file name of the transaction log in the state
// directory.
const transactionLogName = "transactions.log"

// stateDirPath is where the state directory is, without creating it.
func stateDirPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := userHome()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "xbpretty"), nil
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

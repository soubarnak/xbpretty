// Package config loads xbpretty's user configuration file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// Exit codes used when the config file itself is unusable.
const (
	ExitParseError = 2
)

// Config holds the resolved settings for one run.
type Config struct {
	AssumeYes   bool
	Color       string
	Verbose     bool
	Sync        bool
	Log         bool
	XBPSInstall string
	XBPSRemove  string
	XBPSQuery   string
}

// Defaults returns the built-in settings used when neither the config file
// nor the command line says otherwise.
func Defaults() Config {
	return Config{
		AssumeYes:   false,
		Color:       "auto",
		Verbose:     false,
		Sync:        true,
		Log:         true,
		XBPSInstall: "xbps-install",
		XBPSRemove:  "xbps-remove",
		XBPSQuery:   "xbps-query",
	}
}

// ParseError is a malformed config file, carrying the line it was found on.
type ParseError struct {
	Line int
	Text string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("config line %d: %s", e.Line, e.Text)
}

// Load reads the config file for the current user and merges it over the
// built-in defaults. A missing file is not an error.
func Load() (Config, error) {
	cfg := Defaults()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}
	defer f.Close()
	loaded, err := Parse(f)
	if err != nil {
		return cfg, err
	}
	merge(&cfg, loaded)
	return cfg, nil
}

// Path resolves the config file location, honouring XDG_CONFIG_HOME and the
// sudo/doas calling-user lookup.
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
	if os.Geteuid() != 0 {
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
	u, err := user.Lookup(name)
	if err != nil {
		return "", fmt.Errorf("looking up user %s: %w", name, err)
	}
	if u.HomeDir == "" {
		return "", fmt.Errorf("user %s has no home directory", name)
	}
	return u.HomeDir, nil
}

// Parse reads TOML-shaped key/value pairs. Only the keys xbpretty understands
// are read; anything else is an error, so a typo is never silently ignored.
func Parse(r io.Reader) (Config, error) {
	cfg := Defaults()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, &ParseError{Line: lineNo, Text: "expected key = value"}
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if err := apply(&cfg, key, value); err != nil {
			return cfg, &ParseError{Line: lineNo, Text: err.Error()}
		}
	}
	if err := scanner.Err(); err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}
	return cfg, nil
}

func apply(cfg *Config, key, value string) error {
	switch key {
	case "assume_yes":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		cfg.AssumeYes = b
	case "color":
		v := unquote(value)
		if !validColor(v) {
			return fmt.Errorf("color must be one of auto, always, never, got %q", v)
		}
		cfg.Color = v
	case "verbose":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		cfg.Verbose = b
	case "sync":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		cfg.Sync = b
	case "log":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		cfg.Log = b
	case "xbps_install_path":
		cfg.XBPSInstall = unquote(value)
	case "xbps_remove_path":
		cfg.XBPSRemove = unquote(value)
	case "xbps_query_path":
		cfg.XBPSQuery = unquote(value)
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

func parseBool(value string) (bool, error) {
	switch v := unquote(value); v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("expected true or false, got %q", value)
}

func validColor(value string) bool {
	switch value {
	case "auto", "always", "never":
		return true
	}
	return false
}

func unquote(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func merge(dst *Config, src Config) { *dst = src }

// ErrNotInstalled is reported when a configured xbps binary cannot be found.
var ErrNotInstalled = errors.New("xbps binary not found")

// Resolve checks that every configured xbps binary exists and is executable,
// returning a per-command error naming what is missing.
func (c Config) Resolve() error {
	for _, path := range []string{c.XBPSInstall, c.XBPSRemove, c.XBPSQuery} {
		if _, err := exec.LookPath(path); err != nil {
			return fmt.Errorf("%w: %s", ErrNotInstalled, path)
		}
	}
	return nil
}

// StateDir returns the directory holding xbpretty's state files, creating it
// on first use.
func StateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	var err error
	if base == "" {
		var home string
		home, err = userHome()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "xbpretty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating state directory: %w", err)
	}
	return dir, nil
}

// TransactionLog is the path of the append-only transaction log.
func TransactionLog() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "transactions.log"), nil
}

// FormatTimestamp renders t in the ISO-8601 form the transaction log uses.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

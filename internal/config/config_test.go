package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseValidFile(t *testing.T) {
	in := `
# a comment
assume_yes = true
color = "never"
verbose = true
sync = false
log = false
xbps_install_path = "/usr/local/bin/xbps-install"
xbps_remove_path = '/usr/local/bin/xbps-remove'
xbps_query_path = /usr/local/bin/xbps-query
`
	cfg, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Config{
		AssumeYes:   true,
		Color:       "never",
		Verbose:     true,
		Sync:        false,
		Log:         false,
		XBPSInstall: "/usr/local/bin/xbps-install",
		XBPSRemove:  "/usr/local/bin/xbps-remove",
		XBPSQuery:   "/usr/local/bin/xbps-query",
	}
	if cfg != want {
		t.Errorf("Parse:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestParseMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("got %+v, want defaults %+v", cfg, Defaults())
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantLine int
	}{
		{"no equals sign", "assume_yes\n", 1},
		{"bad bool", "assume_yes = maybe\n", 1},
		{"bad color", "color = \"blue\"\n", 1},
		{"unknown key", "verbose = true\nnope = 1\n", 2},
		{"line number after comments", "# c\n\nsync = 1\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.in))
			if err == nil {
				t.Fatal("Parse succeeded, want error")
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("got %T %v, want *ParseError", err, err)
			}
			if pe.Line != tt.wantLine {
				t.Errorf("line = %d, want %d (error: %v)", pe.Line, tt.wantLine, pe)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
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
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "xbpretty", "config.toml")
	if err := os.WriteFile(path, []byte("assume_yes = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AssumeYes {
		t.Error("assume_yes was not read from the config file")
	}
	if cfg.Sync != true || cfg.Log != true {
		t.Errorf("defaults were not preserved: %+v", cfg)
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

func TestParseErrorOutput(t *testing.T) {
	_, err := Parse(strings.NewReader("color = \"blue\"\n"))
	if err == nil {
		t.Fatal("Parse succeeded, want error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error %q does not name the line", err)
	}
	if !strings.Contains(err.Error(), "color") {
		t.Errorf("error %q does not name the key", err)
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

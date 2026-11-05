package cli

import (
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
)

// TestResolveSettingsPrecedence pins FR-057's matrix: only an explicitly
// passed CLI value overrides the file, and the file overrides the default.
func TestResolveSettingsPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		argv  []string
		alter func(*config.Config)
		want  settings
	}{
		{
			name: "file wins over the defaults",
			alter: func(c *config.Config) {
				c.Color.Mode = config.ColorAlways
				c.Ui.Mode = config.ModePlain
				c.Nala.AutoUpdate = false
				c.Nala.AssumeYes = true
				c.Nala.AutoRemove = false
				c.Nala.Simple = true
				c.Nala.FullUpgrade = true
				c.Xbps.TransactionLog = false
			},
			want: settings{mode: config.ModePlain, unit: config.UnitBinary, color: config.ColorAlways,
				assumeYes: true, autoRemove: false, sync: false, simple: true, full: true, log: false,
				historyDir: defaultHistoryDir},
		},
		{
			name: "explicit CLI values win",
			argv: []string{"--color=never", "--tui", "--no-update", "--assume-no", "--no-auto-remove", "--no-full", "--no-log", "install"},
			alter: func(c *config.Config) {
				c.Color.Mode = config.ColorAlways
				c.Ui.Mode = config.ModePlain
				c.Nala.AutoUpdate = true
				c.Nala.AssumeYes = true
				c.Nala.AutoRemove = true
				c.Nala.FullUpgrade = true
				c.Xbps.TransactionLog = true
			},
			want: settings{mode: config.ModeTui, unit: config.UnitBinary, color: config.ColorNever,
				assumeYes: false, autoRemove: false, sync: false, full: false, log: false,
				historyDir: defaultHistoryDir},
		},
		{
			name: "no-color beats an explicit color",
			argv: []string{"--color=always", "--no-color", "install"},
			want: settings{mode: config.ModeAuto, unit: config.UnitBinary, color: config.ColorNever,
				autoRemove: true, sync: true, log: true, historyDir: defaultHistoryDir},
		},
		{
			name:  "no-tui demotes Tui to Auto",
			argv:  []string{"--no-tui", "install"},
			alter: func(c *config.Config) { c.Ui.Mode = config.ModeTui },
			want: settings{mode: config.ModeAuto, unit: config.UnitBinary, color: config.ColorAuto,
				autoRemove: true, sync: true, log: true, historyDir: defaultHistoryDir},
		},
		{
			name:  "no-tui leaves Plain alone",
			argv:  []string{"--no-tui", "install"},
			alter: func(c *config.Config) { c.Ui.Mode = config.ModePlain },
			want: settings{mode: config.ModePlain, unit: config.UnitBinary, color: config.ColorAuto,
				autoRemove: true, sync: true, log: true, historyDir: defaultHistoryDir},
		},
		{
			name: "no-full wins over full",
			argv: []string{"--full", "--no-full", "install"},
			want: settings{mode: config.ModeAuto, unit: config.UnitBinary, color: config.ColorAuto,
				autoRemove: true, sync: true, log: true, historyDir: defaultHistoryDir},
		},
		{
			name: "debug implies verbose",
			argv: []string{"--debug", "install"},
			want: settings{mode: config.ModeAuto, unit: config.UnitBinary, color: config.ColorAuto,
				autoRemove: true, sync: true, log: true, verbose: true, historyDir: defaultHistoryDir},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			if tt.alter != nil {
				tt.alter(&cfg)
			}
			opts, err := parseArgs(tt.argv, "")
			if err != nil {
				t.Fatalf("parseArgs(%v): %v", tt.argv, err)
			}
			got := resolveSettings(cfg, opts)
			if got != tt.want {
				t.Errorf("resolveSettings = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestHistoryDirPrecedence(t *testing.T) {
	cfg := config.Defaults()
	t.Setenv("XBPRETTY_HISTORY_DIR", "/env/history")
	opts, err := parseArgs([]string{"install"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveSettings(cfg, opts).historyDir; got != "/env/history" {
		t.Errorf("environment history dir = %q", got)
	}
	opts, err = parseArgs([]string{"--history-dir", "/cli/history", "install"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveSettings(cfg, opts).historyDir; got != "/cli/history" {
		t.Errorf("CLI history dir = %q", got)
	}
}

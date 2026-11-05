package cli

import (
	"os"

	"github.com/soubarnak/xbpretty/internal/config"
)

// settings is the fully resolved precedence surface of FR-057: CLI value, then
// config, then the built-in default. Only an explicitly passed flag overrides
// the file.
type settings struct {
	mode       config.UIMode
	unit       config.Unit
	color      config.ColorMode
	assumeYes  bool
	autoRemove bool
	sync       bool
	showPkgs   bool
	simple     bool
	full       bool
	log        bool
	verbose    bool
	historyDir string
}

// defaultHistoryDir is FR-029's history store when neither the flag nor the
// environment names one.
const defaultHistoryDir = "/var/lib/xbpretty/history"

// resolveSettings applies FR-057's precedence to one resolved config.
func resolveSettings(cfg config.Config, opts *options) settings {
	s := settings{
		mode:       cfg.Ui.Mode,
		unit:       cfg.Ui.Unit,
		color:      cfg.Color.Mode,
		assumeYes:  cfg.Nala.AssumeYes,
		autoRemove: cfg.Nala.AutoRemove,
		sync:       cfg.Nala.AutoUpdate,
		showPkgs:   cfg.Nala.UpdateShowPackages,
		simple:     cfg.Nala.Simple,
		full:       cfg.Nala.FullUpgrade,
		log:        cfg.Xbps.TransactionLog,
		verbose:    opts.verbose || opts.debug, // --debug implies verbose
		historyDir: defaultHistoryDir,
	}

	// Colour: --no-color > explicit --color > [Color] mode.
	if opts.explicit["color"] {
		s.color = colorModeFromFlag(opts.color)
	}
	if opts.noColor {
		s.color = config.ColorNever
	}
	// UI mode: --tui upgrades Plain to Tui; --no-tui demotes Tui to Auto.
	if opts.explicit["tui"] {
		s.mode = config.ModeTui
	}
	if opts.explicit["no_tui"] && s.mode == config.ModeTui {
		s.mode = config.ModeAuto
	}
	// Assume-yes: -y > --assume-no > file.
	if opts.yes {
		s.assumeYes = true
	}
	if opts.assumeNo {
		s.assumeYes = false
	}
	// Autoremove: flag > file > true.
	if opts.autoRemove != nil {
		s.autoRemove = *opts.autoRemove
	}
	// Sync: --update / --no-update / --no-sync > auto_update.
	if opts.update {
		s.sync = true
	}
	if opts.noUpdate {
		s.sync = false
	}
	// Simple: --simple or file.
	if opts.simple {
		s.simple = true
	}
	// Full: --no-full > --full/--safe > file.
	if opts.full {
		s.full = true
	}
	if opts.noFull {
		s.full = false
	}
	// Log: --no-log > transaction_log.
	if opts.noLog {
		s.log = false
	}
	// History dir: --history-dir > XBPRETTY_HISTORY_DIR > default.
	if dir := os.Getenv("XBPRETTY_HISTORY_DIR"); dir != "" {
		s.historyDir = dir
	}
	if opts.historyDir != "" {
		s.historyDir = opts.historyDir
	}
	return s
}

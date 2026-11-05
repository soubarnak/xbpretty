package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// FR-054 selects layers in this order: the system file, the user file, and a
// long-only --config that replaces both. Each existing layer is parsed and
// validated independently; later layers win per key and theme roles merge.

// defaultSystemConfigPath is the host-wide layer. It is absent by default and
// the package never creates it (FR-060).
const defaultSystemConfigPath = "/etc/xbpretty/config.toml"

// systemConfigPath is a variable only so the build-tagged test hook can point
// the system layer at a fixture instead of /etc (see testroot_testhook.go).
var systemConfigPath = defaultSystemConfigPath

// trustedUID is the owner a privileged binary-path layer must have. Production
// always requires uid 0; the build-tagged test hook may replace it so the trust
// gate is exercisable without running the suite as root.
var trustedUID uint32

// LayerRole names one layer for --debug reporting.
type LayerRole string

// The three layer roles.
const (
	LayerSystem LayerRole = "system"
	LayerUser   LayerRole = "user"
	LayerConfig LayerRole = "config"
)

// Layer reports one layer's selection outcome.
type Layer struct {
	Role    LayerRole
	Path    string
	Present bool
	Trusted bool
}

// Warning is one config warning. Line zero means the warning is about the file
// or its trust rather than a specific key. The caller adds the
// "xbpretty: warning: " prefix.
type Warning struct {
	Path    string
	Line    int
	Message string
}

func (w Warning) String() string {
	if w.Line > 0 {
		return fmt.Sprintf("config %s:%d: %s", w.Path, w.Line, w.Message)
	}
	return fmt.Sprintf("config %s: %s", w.Path, w.Message)
}

// Result is one run's resolved configuration plus everything the CLI prints
// about it: per-key warnings, layer selection and the rev-5 keys seen.
type Result struct {
	Config   Config
	Warnings []Warning
	Layers   []Layer
	Legacy   []LegacyHit
}

// LoadSelected loads the default layers, or exact replaces both with one file
// when explicit is non-empty. A missing default layer is silent; a missing
// explicit layer is a hard error (FR-053).
func LoadSelected(explicit string) (Result, error) {
	res := Result{Config: Defaults()}
	type selection struct {
		role     LayerRole
		path     string
		optional bool
	}
	var selections []selection
	if explicit != "" {
		selections = []selection{{role: LayerConfig, path: explicit}}
	} else {
		userPath, err := Path()
		if err != nil {
			return res, err
		}
		selections = []selection{
			{role: LayerSystem, path: systemConfigPath, optional: true},
			{role: LayerUser, path: userPath, optional: true},
		}
	}
	for _, sel := range selections {
		layer, partial, hits, warnings, err := loadLayer(sel.role, sel.path, sel.optional)
		if err != nil {
			return res, err
		}
		res.Layers = append(res.Layers, layer)
		res.Legacy = append(res.Legacy, hits...)
		res.Warnings = append(res.Warnings, warnings...)
		if !layer.Present {
			continue
		}
		partial.mergeInto(&res.Config)
	}
	return res, nil
}

// loadLayer opens, validates and interprets one layer. Ownership and mode are
// read from the same descriptor the contents come from, so a path swap between
// check and read cannot defeat the trust gate (FR-055).
func loadLayer(role LayerRole, path string, optional bool) (Layer, partialConfig, []LegacyHit, []Warning, error) {
	layer := Layer{Role: role, Path: path}
	partial := newPartial()
	f, err := os.Open(path)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return layer, partial, nil, nil, nil
		}
		return layer, partial, nil, nil, fileError(path, fmt.Sprintf("cannot read configuration file: %v", err))
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return layer, partial, nil, nil, fileError(path, fmt.Sprintf("cannot read configuration file: %v", err))
	}
	layer.Present = true

	data, err := readConfigBytes(path, f)
	if err != nil {
		return layer, partial, nil, nil, err
	}
	doc, err := parseDocument(path, data)
	if err != nil {
		return layer, partial, nil, nil, err
	}
	partial, hits, err := buildPartial(doc)
	if err != nil {
		if cfgErr, ok := err.(*Error); ok {
			cfgErr.Path = path
		}
		return layer, partial, hits, nil, err
	}

	warnings := make([]Warning, 0, len(hits))
	for _, hit := range hits {
		warnings = append(warnings, hit.warning())
	}

	layer.Trusted = true
	if euid() == 0 {
		trusted, uid, mode := trustOf(info)
		layer.Trusted = trusted
		if !trusted {
			for _, pv := range partial.pathValues() {
				if !pv.target.set {
					continue
				}
				warnings = append(warnings, Warning{Path: path, Message: fmt.Sprintf(
					"ignoring [Xbps] %s: file is not owned by root or is group/world-writable (uid %d, mode %04o)",
					pv.key, uid, mode)})
				*pv.target = opt[string]{}
			}
		}
	}
	return layer, partial, hits, warnings, nil
}

// trustOf applies FR-055's rule: a regular file, owned by trustedUID, with no
// group or world write bit. When the owner cannot be read the gate fails
// closed.
func trustOf(info fs.FileInfo) (trusted bool, uid uint32, mode fs.FileMode) {
	mode = info.Mode().Perm()
	owner, ok := fileOwner(info)
	if !ok {
		return false, 0, mode
	}
	if !info.Mode().IsRegular() {
		return false, owner, mode
	}
	return owner == trustedUID && mode&0o022 == 0, owner, mode
}

// DebugLines returns the extra --debug report: layer selection, each rev-5 key
// with its mapping, a legacy summary, and the fully resolved configuration
// (FR-057, FR-058). The caller prefixes each line with "xbpretty: debug: ".
func (r Result) DebugLines() []string {
	lines := make([]string, 0, len(r.Layers)+len(r.Legacy)+16)
	for _, layer := range r.Layers {
		if !layer.Present {
			lines = append(lines, fmt.Sprintf("config layer %s %s (missing)", layer.Role, layer.Path))
			continue
		}
		state := "read, trusted"
		if !layer.Trusted {
			state = "read, untrusted"
		}
		lines = append(lines, fmt.Sprintf("config layer %s %s (%s)", layer.Role, layer.Path, state))
	}
	for _, hit := range r.Legacy {
		lines = append(lines, hit.debugLine())
	}
	if len(r.Legacy) > 0 {
		retired := 0
		for _, hit := range r.Legacy {
			if hit.Retired {
				retired++
			}
		}
		lines = append(lines, fmt.Sprintf("legacy keys: %d seen (%d mapped, %d retired)",
			len(r.Legacy), len(r.Legacy)-retired, retired))
	}
	lines = append(lines, r.Config.debugLines()...)
	return lines
}

// debugLines renders the resolved config one table per line.
func (c Config) debugLines() []string {
	theme := c.Color.Theme
	return []string{
		fmt.Sprintf("resolved [Nala] auto_remove=%t auto_update=%t update_show_packages=%t full_upgrade=%t simple=%t assume_yes=%t",
			c.Nala.AutoRemove, c.Nala.AutoUpdate, c.Nala.UpdateShowPackages, c.Nala.FullUpgrade, c.Nala.Simple, c.Nala.AssumeYes),
		fmt.Sprintf("resolved [Ui] mode=%s unit=%s", c.Ui.Mode, c.Ui.Unit),
		fmt.Sprintf("resolved [Color] mode=%s", c.Color.Mode),
		"resolved [Color.theme] Primary=" + theme.Primary.String(),
		"resolved [Color.theme] Secondary=" + theme.Secondary.String(),
		"resolved [Color.theme] Highlight=" + theme.Highlight.String(),
		"resolved [Color.theme] Regular=" + theme.Regular.String(),
		"resolved [Color.theme] ProgressFilled=" + theme.ProgressFilled.String(),
		"resolved [Color.theme] ProgressUnfilled=" + theme.ProgressUnfilled.String(),
		"resolved [Color.theme] Notice=" + theme.Notice.String(),
		"resolved [Color.theme] Warning=" + theme.Warning.String(),
		"resolved [Color.theme] Error=" + theme.Error.String(),
		fmt.Sprintf("resolved [Xbps] install_path=%s remove_path=%s query_path=%s rindex_path=%s uhelper_path=%s pkgdb_path=%s transaction_log=%t",
			c.Xbps.InstallPath, c.Xbps.RemovePath, c.Xbps.QueryPath,
			c.Xbps.RindexPath, c.Xbps.UhelperPath, c.Xbps.PkgdbPath, c.Xbps.TransactionLog),
	}
}

// String renders a Style for --debug.
func (s Style) String() string {
	parts := []string{"fg=" + s.Fg.String()}
	if s.HasBg {
		parts = append(parts, "bg="+s.Bg.String())
	}
	parts = append(parts, "modifier="+modifierName(s.Modifier))
	return "{" + strings.Join(parts, " ") + "}"
}

// String renders a Color for --debug.
func (c Color) String() string {
	switch c.Kind {
	case ColorReset:
		return "Reset"
	case ColorBlack:
		return "Black"
	case ColorRed:
		return "Red"
	case ColorGreen:
		return "Green"
	case ColorYellow:
		return "Yellow"
	case ColorBlue:
		return "Blue"
	case ColorMagenta:
		return "Magenta"
	case ColorCyan:
		return "Cyan"
	case ColorGray:
		return "Gray"
	case ColorDarkGray:
		return "DarkGray"
	case ColorLightRed:
		return "LightRed"
	case ColorLightGreen:
		return "LightGreen"
	case ColorLightYellow:
		return "LightYellow"
	case ColorLightBlue:
		return "LightBlue"
	case ColorLightMagenta:
		return "LightMagenta"
	case ColorLightCyan:
		return "LightCyan"
	case ColorWhite:
		return "White"
	case ColorIndexed:
		return fmt.Sprintf("Indexed(%d)", c.Index)
	case ColorRGB:
		return fmt.Sprintf("Rgb(%d,%d,%d)", c.R, c.G, c.B)
	}
	return "Unknown"
}

func modifierName(m Modifiers) string {
	if m == 0 {
		return "none"
	}
	var names []string
	for _, bit := range []struct {
		mask Modifiers
		name string
	}{
		{ModifierBold, "bold"}, {ModifierDim, "dim"}, {ModifierItalic, "italic"},
		{ModifierUnderlined, "underlined"}, {ModifierSlowBlink, "slow_blink"},
		{ModifierRapidBlink, "rapid_blink"}, {ModifierReversed, "reversed"},
		{ModifierHidden, "hidden"}, {ModifierCrossedOut, "crossed_out"},
	} {
		if m.Has(bit.mask) {
			names = append(names, bit.name)
		}
	}
	return strings.Join(names, "|")
}

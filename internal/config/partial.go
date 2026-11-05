package config

// This file holds the per-key "partial" configuration a single layer supplies.
// Presence is tracked separately from value so a key that happens to equal the
// built-in default is still a layer's explicit choice (FR-054), and so theme
// roles can merge role-by-role across layers.

// opt is one optional value: set reports whether the layer named the key.
type opt[T any] struct {
	value T
	set   bool
}

func (o *opt[T]) assign(v T) {
	o.value = v
	o.set = true
}

// partialConfig is one layer's keys before they are merged over the defaults.
type partialConfig struct {
	autoRemove         opt[bool]
	autoUpdate         opt[bool]
	updateShowPackages opt[bool]
	fullUpgrade        opt[bool]
	simple             opt[bool]
	assumeYes          opt[bool]

	uiMode opt[UIMode]
	uiUnit opt[Unit]

	colorMode opt[ColorMode]
	theme     map[string]Style

	installPath    opt[string]
	removePath     opt[string]
	queryPath      opt[string]
	rindexPath     opt[string]
	uhelperPath    opt[string]
	pkgdbPath      opt[string]
	transactionLog opt[bool]
}

func newPartial() partialConfig {
	return partialConfig{theme: map[string]Style{}}
}

// mergeInto applies only the keys this layer supplied, so later layers win per
// key and absent keys keep the earlier value or the built-in default.
func (p *partialConfig) mergeInto(cfg *Config) {
	if p.autoRemove.set {
		cfg.Nala.AutoRemove = p.autoRemove.value
	}
	if p.autoUpdate.set {
		cfg.Nala.AutoUpdate = p.autoUpdate.value
	}
	if p.updateShowPackages.set {
		cfg.Nala.UpdateShowPackages = p.updateShowPackages.value
	}
	if p.fullUpgrade.set {
		cfg.Nala.FullUpgrade = p.fullUpgrade.value
	}
	if p.simple.set {
		cfg.Nala.Simple = p.simple.value
	}
	if p.assumeYes.set {
		cfg.Nala.AssumeYes = p.assumeYes.value
	}
	if p.uiMode.set {
		cfg.Ui.Mode = p.uiMode.value
	}
	if p.uiUnit.set {
		cfg.Ui.Unit = p.uiUnit.value
	}
	if p.colorMode.set {
		cfg.Color.Mode = p.colorMode.value
	}
	for role, style := range p.theme {
		setThemeRole(&cfg.Color.Theme, role, style)
	}
	if p.installPath.set {
		cfg.Xbps.InstallPath = p.installPath.value
	}
	if p.removePath.set {
		cfg.Xbps.RemovePath = p.removePath.value
	}
	if p.queryPath.set {
		cfg.Xbps.QueryPath = p.queryPath.value
	}
	if p.rindexPath.set {
		cfg.Xbps.RindexPath = p.rindexPath.value
	}
	if p.uhelperPath.set {
		cfg.Xbps.UhelperPath = p.uhelperPath.value
	}
	if p.pkgdbPath.set {
		cfg.Xbps.PkgdbPath = p.pkgdbPath.value
	}
	if p.transactionLog.set {
		cfg.Xbps.TransactionLog = p.transactionLog.value
	}
}

// pathValue is one [Xbps] path key in the partial config.
type pathValue struct {
	key    string
	target *opt[string]
}

// pathValues lists the six trust-gated path keys in a stable order.
func (p *partialConfig) pathValues() []pathValue {
	return []pathValue{
		{"install_path", &p.installPath},
		{"remove_path", &p.removePath},
		{"query_path", &p.queryPath},
		{"rindex_path", &p.rindexPath},
		{"uhelper_path", &p.uhelperPath},
		{"pkgdb_path", &p.pkgdbPath},
	}
}

// buildPartial interprets one parsed document. The ten rev-5 top-level keys are
// read first, then the tables; a canonical key therefore wins over its legacy
// alias within the same file (FR-056).
func buildPartial(doc *tomlDoc) (partialConfig, []LegacyHit, error) {
	p := newPartial()
	var hits []LegacyHit
	canonical := canonicalTargets(doc)
	for _, entry := range doc.top {
		spec := legacyKeys[entry.key]
		ignored := spec.table != "" && canonical[spec.table][spec.key]
		hit, err := applyLegacy(&p, entry, ignored)
		if err != nil {
			return p, hits, err
		}
		hit.Path = doc.path
		hits = append(hits, hit)
	}
	for _, table := range doc.tables {
		if err := applyTable(&p, table); err != nil {
			return p, hits, err
		}
	}
	return p, hits, nil
}

// canonicalTargets records which canonical table/key pairs a document sets, so
// a legacy alias can warn with the "is ignored" variant when its target is
// also present in the same file.
func canonicalTargets(doc *tomlDoc) map[string]map[string]bool {
	present := map[string]map[string]bool{}
	for _, table := range doc.tables {
		if present[table.name] == nil {
			present[table.name] = map[string]bool{}
		}
		for _, entry := range table.entries {
			present[table.name][entry.key] = true
		}
	}
	return present
}

func applyTable(p *partialConfig, table tomlTable) error {
	switch table.name {
	case "Nala":
		return applyNala(p, table)
	case "Ui":
		return applyUi(p, table)
	case "Color":
		return applyColor(p, table)
	case "Color.theme":
		return applyTheme(p, table)
	case "Xbps":
		return applyXbps(p, table)
	}
	return &Error{Line: table.line, Message: "unknown table [" + table.name + "]"}
}

func applyNala(p *partialConfig, table tomlTable) error {
	for _, entry := range table.entries {
		var target *opt[bool]
		switch entry.key {
		case "auto_remove":
			target = &p.autoRemove
		case "auto_update":
			target = &p.autoUpdate
		case "update_show_packages":
			target = &p.updateShowPackages
		case "full_upgrade":
			target = &p.fullUpgrade
		case "simple":
			target = &p.simple
		case "assume_yes":
			target = &p.assumeYes
		default:
			return unknownKey(table.name, entry)
		}
		value, err := boolValue(table.name, entry)
		if err != nil {
			return err
		}
		target.assign(value)
	}
	return nil
}

func applyUi(p *partialConfig, table tomlTable) error {
	for _, entry := range table.entries {
		switch entry.key {
		case "mode":
			mode, err := enumValue[UIMode](table.name, entry, []UIMode{ModeAuto, ModePlain, ModeTui})
			if err != nil {
				return err
			}
			p.uiMode.assign(mode)
		case "unit":
			unit, err := enumValue[Unit](table.name, entry, []Unit{UnitBinary, UnitDecimal})
			if err != nil {
				return err
			}
			p.uiUnit.assign(unit)
		default:
			return unknownKey(table.name, entry)
		}
	}
	return nil
}

func applyColor(p *partialConfig, table tomlTable) error {
	for _, entry := range table.entries {
		switch entry.key {
		case "mode":
			mode, err := enumValue[ColorMode](table.name, entry, []ColorMode{ColorAuto, ColorAlways, ColorNever})
			if err != nil {
				return err
			}
			p.colorMode.assign(mode)
		case "theme":
			return &Error{Line: entry.line, Message: "[Color] theme is not supported; write roles in [Color.theme]"}
		default:
			return unknownKey(table.name, entry)
		}
	}
	return nil
}

func applyXbps(p *partialConfig, table tomlTable) error {
	for _, entry := range table.entries {
		if entry.key == "transaction_log" {
			value, err := boolValue(table.name, entry)
			if err != nil {
				return err
			}
			p.transactionLog.assign(value)
			continue
		}
		var target *opt[string]
		switch entry.key {
		case "install_path":
			target = &p.installPath
		case "remove_path":
			target = &p.removePath
		case "query_path":
			target = &p.queryPath
		case "rindex_path":
			target = &p.rindexPath
		case "uhelper_path":
			target = &p.uhelperPath
		case "pkgdb_path":
			target = &p.pkgdbPath
		default:
			return unknownKey(table.name, entry)
		}
		value, err := stringValue(table.name, entry)
		if err != nil {
			return err
		}
		if err := validatePath(entry.key, value); err != nil {
			return &Error{Line: entry.line, Message: err.Error()}
		}
		target.assign(value)
	}
	return nil
}

func applyTheme(p *partialConfig, table tomlTable) error {
	for _, entry := range table.entries {
		if !isThemeRole(entry.key) {
			return &Error{Line: entry.line, Message: `unknown theme role "` + entry.key + `"`}
		}
		style, err := parseStyle(entry)
		if err != nil {
			return err
		}
		p.theme[entry.key] = style
	}
	return nil
}

func setThemeRole(theme *Theme, role string, style Style) {
	switch role {
	case "Primary":
		theme.Primary = style
	case "Secondary":
		theme.Secondary = style
	case "Highlight":
		theme.Highlight = style
	case "Regular":
		theme.Regular = style
	case "ProgressFilled":
		theme.ProgressFilled = style
	case "ProgressUnfilled":
		theme.ProgressUnfilled = style
	case "Notice":
		theme.Notice = style
	case "Warning":
		theme.Warning = style
	case "Error":
		theme.Error = style
	}
}

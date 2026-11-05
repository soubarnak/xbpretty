package config

// SchemaTable describes one configuration table, its accepted lowercase
// aliases and its keys. The manual page renders KEYS from this manifest so a
// schema change and its documentation move together.
type SchemaTable struct {
	Name    string
	Aliases []string
	Keys    []SchemaKey
}

// SchemaKey is one configuration key. Default is a TOML literal that parses
// to the built-in default; Values is the closed set for an enum key.
type SchemaKey struct {
	Name    string
	Default string
	Summary string
	Values  []string
}

// SchemaThemeRole is one [Color.theme] role and its default style literal.
type SchemaThemeRole struct {
	Name    string
	Default string
	Summary string
}

// SchemaLegacyKey is one rev-5 flat key. Retired keys have no target.
type SchemaLegacyKey struct {
	Key     string
	Target  string
	Summary string
	Retired bool
}

// SchemaManifest is the complete public configuration schema.
type SchemaManifest struct {
	Tables     []SchemaTable
	ThemeRoles []SchemaThemeRole
	Legacy     []SchemaLegacyKey
}

func schemaKey(name, def, summary string, values ...string) SchemaKey {
	return SchemaKey{Name: name, Default: def, Summary: summary, Values: values}
}

// Manifest returns the configuration schema in documentation order. Defaults
// return the same values as Defaults.
func Manifest() SchemaManifest {
	return SchemaManifest{
		Tables: []SchemaTable{
			{Name: "Nala", Aliases: []string{"nala"}, Keys: []SchemaKey{
				schemaKey("auto_remove", "true", "Plan and offer the implicit orphan removal after a successful transaction."),
				schemaKey("auto_update", "true", "Refresh the repository index before the upgrade family."),
				schemaKey("update_show_packages", "false", "With update, list each upgradable package instead of only the count."),
				schemaKey("full_upgrade", "false", "Accepted compatibility switch; xbps -u never removes packages."),
				schemaKey("simple", "false", "Use the one-line-per-operation simple summary."),
				schemaKey("assume_yes", "false", "Skip the confirmation prompt, as -y does."),
			}},
			{Name: "Ui", Aliases: []string{"ui"}, Keys: []SchemaKey{
				schemaKey("mode", `"Auto"`, "Interface mode.", "Auto", "Plain", "Tui"),
				schemaKey("unit", `"Binary"`, "Size unit.", "Binary", "Decimal"),
			}},
			{Name: "Color", Aliases: []string{"color"}, Keys: []SchemaKey{
				schemaKey("mode", `"Auto"`, "Colour mode.", "Auto", "Always", "Never"),
			}},
			{Name: "Color.theme", Aliases: []string{"color.theme"}},
			{Name: "Xbps", Aliases: []string{"xbps"}, Keys: []SchemaKey{
				schemaKey("install_path", `"xbps-install"`, "Path to xbps-install; privileged and trust-gated under root."),
				schemaKey("remove_path", `"xbps-remove"`, "Path to xbps-remove; privileged and trust-gated under root."),
				schemaKey("query_path", `"xbps-query"`, "Path to xbps-query; privileged and trust-gated under root."),
				schemaKey("rindex_path", `"xbps-rindex"`, "Path to xbps-rindex; privileged and trust-gated under root."),
				schemaKey("uhelper_path", `"xbps-uhelper"`, "Path to xbps-uhelper; privileged and trust-gated under root."),
				schemaKey("pkgdb_path", `"xbps-pkgdb"`, "Path to xbps-pkgdb; privileged and trust-gated under root."),
				schemaKey("transaction_log", "true", "Append a line to the transaction log after a successful run."),
			}},
		},
		ThemeRoles: []SchemaThemeRole{
			{Name: "Primary", Default: `{ fg = "BrightGreen" }`, Summary: "Section rules, labels and headings."},
			{Name: "Secondary", Default: `{ fg = "BrightBlue" }`, Summary: "Secondary labels."},
			{Name: "Highlight", Default: `{ fg = "Reset" }`, Summary: "Package names and highlighted values."},
			{Name: "Regular", Default: `{ fg = "Reset", modifier = "" }`, Summary: "Regular body text."},
			{Name: "ProgressFilled", Default: `{ fg = "BrightGreen" }`, Summary: "Filled part of the progress bar."},
			{Name: "ProgressUnfilled", Default: `{ fg = "BrightRed" }`, Summary: "Unfilled part of the progress bar."},
			{Name: "Notice", Default: `{ fg = "BrightYellow" }`, Summary: "Notices and version highlights."},
			{Name: "Warning", Default: `{ fg = "BrightYellow" }`, Summary: "Warnings."},
			{Name: "Error", Default: `{ fg = "BrightRed" }`, Summary: "Errors."},
		},
		Legacy: []SchemaLegacyKey{
			{Key: "assume_yes", Target: "[Nala] assume_yes", Summary: "Rename to [Nala] assume_yes."},
			{Key: "sync", Target: "[Nala] auto_update", Summary: "Rename to [Nala] auto_update."},
			{Key: "log", Target: "[Xbps] transaction_log", Summary: "Rename to [Xbps] transaction_log."},
			{Key: "xbps_install_path", Target: "[Xbps] install_path", Summary: "Rename to [Xbps] install_path."},
			{Key: "xbps_remove_path", Target: "[Xbps] remove_path", Summary: "Rename to [Xbps] remove_path."},
			{Key: "xbps_query_path", Target: "[Xbps] query_path", Summary: "Rename to [Xbps] query_path."},
			{Key: "color", Target: "[Color] mode", Summary: "Rename to [Color] mode and map auto/always/never to Auto/Always/Never."},
			{Key: "verbose", Retired: true, Summary: "Retired; pass -v on the command line."},
			{Key: "box_style", Retired: true, Summary: "Retired; the summary has no box."},
			{Key: "progress_bar", Retired: true, Summary: `Retired; use [Ui] mode = "Plain".`},
		},
	}
}

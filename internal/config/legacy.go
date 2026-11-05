package config

import (
	"fmt"
	"strings"
)

// FR-056: rev 6 reads the ten rev-5 top-level flat keys for one release. Seven
// alias onto rev-6 keys; three are retired and ignored. Nothing is ever
// rewritten, converted or backed up.

const legacyRemovedNote = "(legacy keys are removed in rev 7)"

// legacySpec is one rev-5 key's disposition.
type legacySpec struct {
	target         string
	retiredMessage string
}

// legacyKeys is the closed set of rev-5 flat keys. The order of this map does
// not matter: warnings follow the file's own order.
var legacyKeys = map[string]legacySpec{
	"assume_yes":        {target: "[Nala] assume_yes"},
	"sync":              {target: "[Nala] auto_update"},
	"log":               {target: "[Xbps] transaction_log"},
	"xbps_install_path": {target: "[Xbps] install_path"},
	"xbps_remove_path":  {target: "[Xbps] remove_path"},
	"xbps_query_path":   {target: "[Xbps] query_path"},
	"color":             {target: "[Color] mode"},
	"verbose":           {retiredMessage: "key 'verbose' is retired; pass -v on the command line"},
	"box_style":         {retiredMessage: "key 'box_style' is retired and has no effect"},
	"progress_bar":      {retiredMessage: `key 'progress_bar' is retired; use [Ui] mode = "Plain"`},
}

func isLegacyKey(key string) bool {
	_, ok := legacyKeys[key]
	return ok
}

// canonicalKeyInTable reports whether key is a valid rev-6 key of the named
// table. It tells a legacy key that landed after a table header (an error) from
// a canonical key that happens to share a rev-5 name, such as [Nala]
// assume_yes.
func canonicalKeyInTable(table, key string) bool {
	switch table {
	case "Nala":
		switch key {
		case "auto_remove", "auto_update", "update_show_packages", "full_upgrade", "simple", "assume_yes":
			return true
		}
	case "Ui":
		return key == "mode" || key == "unit"
	case "Color":
		return key == "mode" || key == "theme"
	case "Xbps":
		switch key {
		case "install_path", "remove_path", "query_path", "rindex_path", "uhelper_path", "pkgdb_path", "transaction_log":
			return true
		}
	}
	return false
}

// LegacyHit records one rev-5 key for warnings and --debug reporting.
type LegacyHit struct {
	Path    string
	Line    int
	Key     string
	Target  string
	Retired bool
}

// warning is the exact per-key deprecation line body (FR-056).
func (h LegacyHit) warning() Warning {
	spec := legacyKeys[h.Key]
	message := spec.retiredMessage
	if !h.Retired {
		message = fmt.Sprintf("key '%s' is deprecated; use %s %s", h.Key, spec.target, legacyRemovedNote)
	}
	return Warning{Path: h.Path, Line: h.Line, Message: message}
}

// debugLine is the extra --debug line for one legacy key (FR-058).
func (h LegacyHit) debugLine() string {
	where := fmt.Sprintf("legacy key '%s' at %s:%d -> ", h.Key, h.Path, h.Line)
	if h.Retired {
		return where + "retired"
	}
	return where + legacyKeys[h.Key].target
}

// applyLegacy maps one top-level rev-5 key. Unknown top-level keys stay hard
// errors, exactly as they are for rev-6 files.
func applyLegacy(p *partialConfig, entry tomlEntry) (LegacyHit, error) {
	spec, ok := legacyKeys[entry.key]
	if !ok {
		return LegacyHit{}, &Error{Line: entry.line, Message: fmt.Sprintf(
			"unknown key %q; configuration keys belong in a table", entry.key)}
	}
	hit := LegacyHit{Line: entry.line, Key: entry.key, Target: spec.target, Retired: spec.retiredMessage != ""}
	if hit.Retired {
		// Retired keys have no effect; any TOML value they carried is ignored.
		return hit, nil
	}

	switch entry.key {
	case "assume_yes":
		value, err := legacyBool(entry)
		if err != nil {
			return hit, err
		}
		p.assumeYes.assign(value)
	case "sync":
		value, err := legacyBool(entry)
		if err != nil {
			return hit, err
		}
		p.autoUpdate.assign(value)
	case "log":
		value, err := legacyBool(entry)
		if err != nil {
			return hit, err
		}
		p.transactionLog.assign(value)
	case "color":
		value, err := legacyString(entry)
		if err != nil {
			return hit, err
		}
		switch strings.ToLower(value) {
		case "auto":
			p.colorMode.assign(ColorAuto)
		case "always":
			p.colorMode.assign(ColorAlways)
		case "never":
			p.colorMode.assign(ColorNever)
		default:
			return hit, &Error{Line: entry.line, Message: fmt.Sprintf(
				"key 'color' must be auto, always or never, got %q", value)}
		}
	case "xbps_install_path", "xbps_remove_path", "xbps_query_path":
		value, err := legacyString(entry)
		if err != nil {
			return hit, err
		}
		if err := validatePath(strings.TrimPrefix(spec.target, "[Xbps] "), value); err != nil {
			return hit, &Error{Line: entry.line, Message: fmt.Sprintf("key '%s': %s", entry.key, err.Error())}
		}
		switch entry.key {
		case "xbps_install_path":
			p.installPath.assign(value)
		case "xbps_remove_path":
			p.removePath.assign(value)
		case "xbps_query_path":
			p.queryPath.assign(value)
		}
	}
	return hit, nil
}

// legacyString accepts the rev-5 bare and quoted string forms for the ten
// legacy keys only.
func legacyString(entry tomlEntry) (string, error) {
	switch entry.value.kind {
	case tomlString, tomlBare:
		return entry.value.str, nil
	}
	return "", &Error{Line: entry.line, Message: fmt.Sprintf(
		"key '%s' must be a string, got %s", entry.key, entry.value.kind)}
}

// legacyBool accepts true/false and the rev-5 quoted form ("true"/"false").
func legacyBool(entry tomlEntry) (bool, error) {
	switch entry.value.kind {
	case tomlBool:
		return entry.value.boolean, nil
	case tomlString, tomlBare:
		switch strings.ToLower(strings.TrimSpace(entry.value.str)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, &Error{Line: entry.line, Message: fmt.Sprintf(
		"key '%s' must be true or false, got %s", entry.key, entry.value.kind)}
}

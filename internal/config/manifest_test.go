package config

import (
	"reflect"
	"strings"
	"testing"
)

func manifestDocument(m SchemaManifest) string {
	var b strings.Builder
	for _, table := range m.Tables {
		if table.Name == "Color.theme" {
			b.WriteString("[Color.theme]\n")
			for _, role := range m.ThemeRoles {
				b.WriteString(role.Name + " = " + role.Default + "\n")
			}
			continue
		}
		b.WriteString("[" + table.Name + "]\n")
		for _, key := range table.Keys {
			b.WriteString(key.Name + " = " + key.Default + "\n")
		}
	}
	return b.String()
}

// TestManifestDefaultsParseToDefaults is the no-drift check the manual page
// depends on: every table, key and default in the manifest is a valid rev-6
// document that resolves to Defaults().
func TestManifestDefaultsParseToDefaults(t *testing.T) {
	manifest := Manifest()
	cfg, err := Parse("manifest.toml", strings.NewReader(manifestDocument(manifest)))
	if err != nil {
		t.Fatalf("manifest defaults do not parse: %v\n%s", err, manifestDocument(manifest))
	}
	if cfg != Defaults() {
		t.Errorf("manifest defaults resolve to %+v, want Defaults() %+v", cfg, Defaults())
	}
}

// TestManifestTableAliasesAreAccepted keeps the documented lowercase aliases
// honest.
func TestManifestTableAliasesAreAccepted(t *testing.T) {
	for _, table := range Manifest().Tables {
		if canonicalTable(table.Name) != table.Name {
			t.Errorf("canonicalTable(%q) = %q", table.Name, canonicalTable(table.Name))
		}
		for _, alias := range table.Aliases {
			if canonicalTable(alias) != table.Name {
				t.Errorf("canonicalTable(%q) = %q, want %q", alias, canonicalTable(alias), table.Name)
			}
			document := "[" + alias + "]\n"
			if table.Name == "Color.theme" {
				document += "Primary = { fg = \"Red\" }\n"
			} else if len(table.Keys) > 0 {
				document += table.Keys[0].Name + " = " + table.Keys[0].Default + "\n"
			}
			if _, err := Parse("alias.toml", strings.NewReader(document)); err != nil {
				t.Errorf("[%s] alias does not parse: %v", alias, err)
			}
		}
	}
}

// TestManifestMatchesTheClosedSets compares the manifest against the schema
// implementation: theme roles, legacy keys, legacy targets and enum values.
func TestManifestMatchesTheClosedSets(t *testing.T) {
	manifest := Manifest()

	var roles []string
	for _, role := range manifest.ThemeRoles {
		roles = append(roles, role.Name)
	}
	if !sameSet(roles, themeRoles) {
		t.Errorf("manifest theme roles = %v, schema roles = %v", roles, themeRoles)
	}

	var legacy []string
	for _, key := range manifest.Legacy {
		legacy = append(legacy, key.Key)
		spec, ok := legacyKeys[key.Key]
		if !ok {
			t.Errorf("manifest legacy key %q is not in legacyKeys", key.Key)
			continue
		}
		if key.Retired != (spec.retiredMessage != "") {
			t.Errorf("%s retired = %v, schema retired = %v", key.Key, key.Retired, spec.retiredMessage != "")
		}
		if !key.Retired && key.Target != spec.target {
			t.Errorf("%s target = %q, schema target = %q", key.Key, key.Target, spec.target)
		}
	}
	var schemaLegacy []string
	for key := range legacyKeys {
		schemaLegacy = append(schemaLegacy, key)
	}
	if !sameSet(legacy, schemaLegacy) {
		t.Errorf("manifest legacy keys = %v, schema keys = %v", legacy, schemaLegacy)
	}

	wantEnums := map[string][]string{
		"Ui/mode":    {string(ModeAuto), string(ModePlain), string(ModeTui)},
		"Ui/unit":    {string(UnitBinary), string(UnitDecimal)},
		"Color/mode": {string(ColorAuto), string(ColorAlways), string(ColorNever)},
	}
	for _, table := range manifest.Tables {
		for _, key := range table.Keys {
			want, ok := wantEnums[table.Name+"/"+key.Name]
			if !ok {
				continue
			}
			if !sameSet(key.Values, want) {
				t.Errorf("%s/%s values = %v, want %v", table.Name, key.Name, key.Values, want)
			}
		}
	}
}

// TestManifestMatchesConfigStructs compares the manifest's table and key sets
// with the Go schema types, so a field added to the schema cannot be missing
// from the documentation manifest.
func TestManifestMatchesConfigStructs(t *testing.T) {
	manifest := Manifest()
	expected := map[string][]string{
		"Nala": fieldKeys(reflect.TypeOf(NalaConfig{})),
		"Ui":   fieldKeys(reflect.TypeOf(UiConfig{})),
		"Xbps": fieldKeys(reflect.TypeOf(XbpsConfig{})),
	}
	// [Color] carries only mode; theme is the separate Color.theme table.
	expected["Color"] = []string{"mode"}

	for _, table := range manifest.Tables {
		want, ok := expected[table.Name]
		if !ok {
			continue // Color.theme is checked through ThemeRoles below.
		}
		var got []string
		for _, key := range table.Keys {
			got = append(got, key.Name)
		}
		if !sameSet(got, want) {
			t.Errorf("manifest [%s] keys = %v, schema fields = %v", table.Name, got, want)
		}
	}
	var roles []string
	for _, role := range manifest.ThemeRoles {
		roles = append(roles, role.Name)
	}
	if want := fieldNames(reflect.TypeOf(Theme{})); !sameSet(roles, want) {
		t.Errorf("manifest theme roles = %v, schema Theme fields = %v", roles, want)
	}
}

func fieldNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	return names
}

func fieldKeys(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		keys = append(keys, snakeCase(t.Field(i).Name))
	}
	return keys
}

func snakeCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, item := range a {
		seen[item] = true
	}
	for _, item := range b {
		if !seen[item] {
			return false
		}
	}
	return true
}

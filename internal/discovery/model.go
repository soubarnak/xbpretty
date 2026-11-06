// Package discovery holds the row model shared by list, search, show and the
// discovery NDJSON schema. The CLI fills rows from the fixed offline
// xbps-query passes; policy (#68) consumes the same repository and package
// state without re-querying.
package discovery

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
)

// SchemaVersion is the NDJSON schema every machine row carries.
const SchemaVersion = 1

// Row is one package variant in the discovery universe. Repository is the raw
// repository URL, or empty for the synthetic local row.
type Row struct {
	Name     string
	Pkgver   string
	Version  string // upstream version, without the _revision suffix
	Revision string // revision suffix, without the leading underscore

	Architecture string
	Repository   string

	Installed        bool
	InstalledVersion string
	Unpacked         bool
	HalfRemoved      bool
	Automatic        bool
	AutoRemovable    bool
	Held             bool
	Repolocked       bool
	UpgradableFrom   string

	ShortDesc string
	LongDesc  string

	// Exact plan sizes, set only on rows selected by list/search -u.
	InstalledSize *uint64
	DownloadSize  *uint64

	// Virtual marks a provides row rather than a package variant.
	Virtual   bool
	Providers []string
}

// SplitPkgverVersion splits `name-1.2.3_4` into the package name and the
// version with its revision. It returns empty strings when the field carries
// no version.
func SplitPkgverVersion(pkgver string) (name, version string) {
	cut := strings.LastIndex(pkgver, "-")
	if cut <= 0 || cut == len(pkgver)-1 {
		return "", ""
	}
	return pkgver[:cut], pkgver[cut+1:]
}

// SplitRevision splits an xbps version such as `0.60.7_1` into the upstream
// version and the numeric revision. A version without a revision is returned
// whole.
func SplitRevision(version string) (upstream, revision string) {
	cut := strings.LastIndex(version, "_")
	if cut <= 0 || cut == len(version)-1 {
		return version, ""
	}
	if _, err := strconv.ParseUint(version[cut+1:], 10, 32); err != nil {
		return version, ""
	}
	return version[:cut], version[cut+1:]
}

// FullVersion is the version as the human line prints it.
func (r Row) FullVersion() string {
	if r.Revision == "" {
		return r.Version
	}
	return r.Version + "_" + r.Revision
}

// DisplayRepository is the repository text inside the human brackets:
// the URL without its scheme, or `local` for the synthetic row.
func (r Row) DisplayRepository() string {
	if r.Repository == "" {
		return "local"
	}
	repo := strings.TrimPrefix(r.Repository, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	return repo
}

// Attributes returns the row's attributes in the fixed FR-024 order.
func (r Row) Attributes() []string {
	if r.Virtual {
		return nil
	}
	var attrs []string
	if r.Installed {
		attrs = append(attrs, "Installed")
	}
	if r.Unpacked {
		attrs = append(attrs, "Unpacked")
	}
	if r.HalfRemoved {
		attrs = append(attrs, "Half-removed")
	}
	if r.Automatic {
		attrs = append(attrs, "Automatic")
	}
	if r.AutoRemovable {
		attrs = append(attrs, "Auto-Removable")
	}
	if r.Held {
		attrs = append(attrs, "Held")
	}
	if r.Repolocked {
		attrs = append(attrs, "Repolocked")
	}
	if r.UpgradableFrom != "" {
		attrs = append(attrs, "Upgradable from: "+r.UpgradableFrom)
	}
	return attrs
}

// AttributeText is the bracketed attribute list the human line carries,
// always `[...]`, including `[]` when empty.
func (r Row) AttributeText() string {
	return "[" + strings.Join(r.Attributes(), ", ") + "]"
}

// Line renders the FR-024 human row.
func (r Row) Line() string {
	if r.Virtual {
		providers := append([]string(nil), r.Providers...)
		sort.Strings(providers)
		return r.Name + " (Virtual) [" + strings.Join(providers, ", ") + "]"
	}
	return r.Name + " (" + r.FullVersion() + ") [" + r.DisplayRepository() + "] " + r.AttributeText()
}

// MachinePackage is the schema-1 package object. Field order is the spec's.
type MachinePackage struct {
	Schema           int            `json:"schema"`
	Kind             string         `json:"kind"`
	Name             string         `json:"name"`
	Pkgver           string         `json:"pkgver"`
	Version          string         `json:"version"`
	Revision         string         `json:"revision"`
	Architecture     string         `json:"architecture"`
	Repository       string         `json:"repository,omitempty"`
	Installed        bool           `json:"installed"`
	InstalledVersion string         `json:"installed_version,omitempty"`
	Automatic        bool           `json:"automatic"`
	Held             bool           `json:"held"`
	Repolocked       bool           `json:"repolocked"`
	AutoRemovable    bool           `json:"auto_removable"`
	ShortDesc        string         `json:"short_desc"`
	InstalledSize    *uint64        `json:"installed_size"`
	DownloadSize     *uint64        `json:"download_size"`
	Properties       map[string]any `json:"properties,omitempty"`
}

// MachineVirtual is the schema-1 virtual object.
type MachineVirtual struct {
	Schema    int      `json:"schema"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Providers []string `json:"providers"`
}

// Machine converts a package row to its schema-1 object. Properties is only
// set by show -m.
func (r Row) Machine() MachinePackage {
	return MachinePackage{
		Schema:           SchemaVersion,
		Kind:             "package",
		Name:             r.Name,
		Pkgver:           r.Pkgver,
		Version:          r.Version,
		Revision:         r.Revision,
		Architecture:     r.Architecture,
		Repository:       r.Repository,
		Installed:        r.Installed,
		InstalledVersion: r.InstalledVersion,
		Automatic:        r.Automatic,
		Held:             r.Held,
		Repolocked:       r.Repolocked,
		AutoRemovable:    r.AutoRemovable,
		ShortDesc:        r.ShortDesc,
		InstalledSize:    r.InstalledSize,
		DownloadSize:     r.DownloadSize,
	}
}

// Machine converts a virtual row to its schema-1 object.
func (r Row) VirtualMachine() MachineVirtual {
	providers := append([]string(nil), r.Providers...)
	sort.Strings(providers)
	return MachineVirtual{Schema: SchemaVersion, Kind: "virtual", Name: r.Name, Providers: providers}
}

// WriteNDJSON writes one compact JSON object and a newline. JSON is never
// HTML-escaped: these rows are data, not markup, and scripts expect the
// literal characters.
func WriteNDJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

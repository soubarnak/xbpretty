package fetch

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultManifestPath is the fetch manifest #74 writes and clean --fetch
// reads (FR-047). #73 reads only its previous mirror, for managed-line
// classification.
const DefaultManifestPath = "/var/lib/xbpretty/fetch.json"

// DefaultRepositoryConfigDir is where fetch writes masking files and the only
// directory a manifest entry may name. The xbpretty_testroot hook points it at
// a fixture; the release binary always uses /etc/xbps.d.
const DefaultRepositoryConfigDir = "/etc/xbps.d"

// ReadManifest reads and validates a schema-1 manifest against the release
// config directory. A missing file returns an error satisfying os.IsNotExist.
func ReadManifest(path string) (Manifest, error) {
	return ReadManifestFor(path, DefaultRepositoryConfigDir)
}

// ReadManifestFor reads and validates a schema-1 manifest. Every entry must be
// an absolute file directly under configDir, with a matching backup name and a
// full sha256; anything else is a corrupt manifest and is rejected before a
// single file is touched.
func ReadManifestFor(path, configDir string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return Manifest{}, fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	cleanDir := filepath.Clean(configDir)
	for _, entry := range manifest.Files {
		switch {
		case entry.Path == "":
			return Manifest{}, fmt.Errorf("manifest entry has an empty path")
		case !filepath.IsAbs(entry.Path):
			return Manifest{}, fmt.Errorf("manifest entry path %q is not absolute", entry.Path)
		case filepath.Clean(filepath.Dir(entry.Path)) != cleanDir:
			return Manifest{}, fmt.Errorf("manifest entry path %q is outside %s", entry.Path, cleanDir)
		case entry.Backup != "" && entry.Backup != entry.Path+BackupSuffix:
			return Manifest{}, fmt.Errorf("manifest entry backup %q does not match %q", entry.Backup, entry.Path+BackupSuffix)
		case len(entry.SHA256) != 64:
			return Manifest{}, fmt.Errorf("manifest entry %q has an invalid sha256", entry.Path)
		}
		if _, err := hex.DecodeString(entry.SHA256); err != nil {
			return Manifest{}, fmt.Errorf("manifest entry %q has an invalid sha256", entry.Path)
		}
	}
	return manifest, nil
}

// manifestMirror is the one field classification needs from schema 1.
type manifestMirror struct {
	Mirror string `json:"mirror"`
}

// ManifestMirrorHosts returns the host of the previous chosen mirror recorded
// in the manifest, or nil when the manifest is absent or unreadable. Reading
// it is best-effort: classification must still work after a corrupt manifest.
func ManifestMirrorHosts(path string) []string {
	return hostsOf(ManifestMirrorURLs(path))
}

// ManifestMirrorURLs returns the previous chosen mirror URL recorded in the
// manifest, or nil when it is absent or unreadable. The full URL matters for
// classification because most embedded mirrors publish under a base path.
func ManifestMirrorURLs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var manifest manifestMirror
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Mirror == "" {
		return nil
	}
	u, err := url.Parse(manifest.Mirror)
	if err != nil || u.Host == "" {
		return nil
	}
	return []string{manifest.Mirror}
}

func hostsOf(rawURLs []string) []string {
	var out []string
	for _, raw := range rawURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		out = append(out, strings.ToLower(u.Hostname()))
	}
	return out
}

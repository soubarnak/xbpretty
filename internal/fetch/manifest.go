package fetch

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
)

// DefaultManifestPath is the fetch manifest #74 writes and clean --fetch
// reads (FR-047). #73 reads only its previous mirror, for managed-line
// classification.
const DefaultManifestPath = "/var/lib/xbpretty/fetch.json"

// manifestMirror is the one field classification needs from schema 1.
type manifestMirror struct {
	Mirror string `json:"mirror"`
}

// ManifestMirrorHosts returns the host of the previous chosen mirror recorded
// in the manifest, or nil when the manifest is absent or unreadable. Reading
// it is best-effort: classification must still work after a corrupt manifest.
func ManifestMirrorHosts(path string) []string {
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
	return []string{strings.ToLower(u.Hostname())}
}

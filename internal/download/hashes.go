package download

import (
	"fmt"
	"strings"
)

// ParseHashes reads the bulk `filename-sha256` pass's answer and keys it by
// (repository, pkgver) (FR-037). The expected line shape is
//
//	<pkgver>: <sha256> (<repository>)
//
// with xbps's optional `[*] `/`[-] ` repository markers. A line that does not
// match is ignored: one unparseable row must not cost the whole run its
// hashes.
func ParseHashes(stdout string) map[HashKey]string {
	out := map[HashKey]string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "[*] ")
		line = strings.TrimPrefix(line, "[-] ")
		left, right, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		value, repository := splitRepositorySuffix(right)
		if left == "" || value == "" || repository == "" {
			continue
		}
		out[HashKeyFor(repository, left)] = value
	}
	return out
}

// splitRepositorySuffix separates a trailing `(URL)` from a property value.
func splitRepositorySuffix(value string) (string, string) {
	value = strings.TrimSpace(value)
	if !strings.HasSuffix(value, ")") {
		return value, ""
	}
	open := strings.LastIndex(value, " (")
	if open < 0 {
		return value, ""
	}
	return strings.TrimSpace(value[:open]), strings.TrimSpace(value[open+2 : len(value)-1])
}

// Resolved is one `xbps-query -R -S NAME -p pkgver,repository,architecture,filename-sha256`
// answer (FR-037): four lines mean the metadata hash is present, three mean it
// is absent, fewer is a resolution error.
type Resolved struct {
	Pkgver     string
	Repository string
	Arch       string
	SHA256     string
	HasHash    bool
}

// ParseResolved reads the per-name resolution answer.
func ParseResolved(stdout string) (Resolved, error) {
	var lines []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 3 && len(lines) != 4 {
		return Resolved{}, fmt.Errorf("xbps-query returned %d lines, want 3 or 4", len(lines))
	}
	resolved := Resolved{Pkgver: lines[0], Repository: lines[1], Arch: lines[2]}
	if len(lines) == 4 {
		resolved.SHA256 = lines[3]
		resolved.HasHash = true
	}
	return resolved, nil
}

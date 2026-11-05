// Package fetch implements `xbp fetch`: the Void mirror list, repository
// discovery, the 256 KiB repodata probe, ranking and the ranking table, plus
// the same-basename /etc/xbps.d writes, the backup and the schema-1 manifest
// that revert through clean --fetch.
package fetch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MirrorListURL is the live Void mirror list (FR-043).
const MirrorListURL = "https://xmirror.voidlinux.org/raw/mirrors.lst"

// Mirror is one row of the Void mirror list.
type Mirror struct {
	Region   string
	URL      string // normalised: no trailing slash
	Location string // may be empty; the table prints an em dash
	Tier     int
}

// ParseWarning names one malformed mirror-list row skipped by the parser.
// FR-043 reports these only under -v.
type ParseWarning struct {
	Line   int
	Reason string
}

func (w ParseWarning) String() string { return fmt.Sprintf("line %d: %s", w.Line, w.Reason) }

// ParseMirrors parses the TSV mirror list. Invalid rows are skipped; an error
// is returned only when the source cannot be read (FR-043).
func ParseMirrors(r io.Reader) ([]Mirror, error) {
	mirrors, _, err := ParseMirrorsDetailed(r)
	return mirrors, err
}

// ParseMirrorsDetailed is ParseMirrors plus the skipped-row warnings.
func ParseMirrorsDetailed(r io.Reader) ([]Mirror, []ParseWarning, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var mirrors []Mirror
	var warnings []ParseWarning
	seen := map[string]bool{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			warnings = append(warnings, ParseWarning{Line: lineNo, Reason: "fewer than four tab-separated fields"})
			continue
		}
		region := strings.TrimSpace(fields[0])
		rawURL := strings.TrimSpace(fields[1])
		location := strings.TrimSpace(fields[2])
		tierText := strings.TrimSpace(fields[3])
		if region == "" {
			warnings = append(warnings, ParseWarning{Line: lineNo, Reason: "empty region"})
			continue
		}
		tier, err := strconv.Atoi(tierText)
		if err != nil {
			warnings = append(warnings, ParseWarning{Line: lineNo, Reason: "tier is not an integer"})
			continue
		}
		if tier < 1 {
			warnings = append(warnings, ParseWarning{Line: lineNo, Reason: "tier is not positive"})
			continue
		}
		normalised, err := NormalizeMirrorURL(rawURL)
		if err != nil {
			warnings = append(warnings, ParseWarning{Line: lineNo, Reason: err.Error()})
			continue
		}
		if seen[normalised] {
			continue
		}
		seen[normalised] = true
		mirrors = append(mirrors, Mirror{
			Region:   region,
			URL:      normalised,
			Location: location,
			Tier:     tier,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, warnings, err
	}
	return mirrors, warnings, nil
}

// NormalizeMirrorURL validates an http(s) URL and trims every trailing slash.
func NormalizeMirrorURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("URL has no host")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// RewriteToHTTPS applies --https-only to the probe and written URL (FR-046).
func RewriteToHTTPS(m Mirror) Mirror {
	if strings.HasPrefix(m.URL, "http://") {
		m.URL = "https://" + strings.TrimPrefix(m.URL, "http://")
	}
	return m
}

// FilterMirrors applies -c/--country (FR-046): a case-insensitive exact match
// on the region, or on a comma/space-delimited word of the location. Several
// names are a union; an empty name list keeps every mirror.
func FilterMirrors(mirrors []Mirror, names []string) []Mirror {
	if len(names) == 0 {
		return mirrors
	}
	wanted := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			wanted[strings.ToLower(name)] = true
		}
	}
	var out []Mirror
	for _, m := range mirrors {
		if wanted[strings.ToLower(m.Region)] {
			out = append(out, m)
			continue
		}
		for _, word := range splitLocationWords(m.Location) {
			if wanted[strings.ToLower(word)] {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// splitLocationWords splits a location or a user's country value on commas and
// whitespace, dropping empty words.
func splitLocationWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

// FetchMirrors downloads and parses the live mirror list. The caller owns the
// timeout; the CLI applies FR-043's 10 seconds. There is no disk cache.
func FetchMirrors(ctx context.Context, client *http.Client, listURL, userAgent string) ([]Mirror, error) {
	mirrors, _, err := FetchMirrorsDetailed(ctx, client, listURL, userAgent)
	return mirrors, err
}

// FetchMirrorsDetailed is FetchMirrors plus the skipped-row warnings.
func FetchMirrorsDetailed(ctx context.Context, client *http.Client, listURL, userAgent string) ([]Mirror, []ParseWarning, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("mirror list: %s", resp.Status)
	}
	mirrors, warnings, err := ParseMirrorsDetailed(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, warnings, err
	}
	if len(mirrors) == 0 {
		return nil, warnings, errors.New("mirror list is empty")
	}
	return mirrors, warnings, nil
}

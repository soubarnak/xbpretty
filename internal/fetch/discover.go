package fetch

import (
	"bufio"
	"errors"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultRepositoryHost is the packaged Void repository host (dec:#48 §3.2).
const DefaultRepositoryHost = "repo-default.voidlinux.org"

var (
	// ErrNoRepoConfig is dec:#48 §3.1's message when no repository= line
	// exists at all.
	ErrNoRepoConfig = errors.New("no xbps repository configuration found under /usr/share/xbps.d")
	// ErrNoManagedRepo is dec:#48 §3.1's message when lines exist but none is
	// a Void mirror/default repository.
	ErrNoManagedRepo = errors.New("no Void mirror repository found to rewrite")
)

// RepositoryLine is one raw repository= line discovered in xbps.d.
type RepositoryLine struct {
	File     string // full path
	Basename string // file name, for /etc/xbps.d masking and #74
	Value    string // repository= value exactly as read
	Host     string // lower-case hostname for a remote URL, else empty
	Suffix   string // URL path without a trailing slash, else empty
	Managed  bool   // rewriteable by fetch (dec:#48 §3.2)
}

// RepositoryConfig is the raw xbps.d scan before the mirror list is known.
// Managed classification needs the live-list hosts, so it is a second step.
type RepositoryConfig struct {
	Lines []RepositoryLine
	Arch  string
	Files []string // masked *.conf files in xbps's load order
}

// RepositorySet is the managed subset of a RepositoryConfig.
type RepositorySet struct {
	Primary  string
	Suffixes []string
	URLs     []string
	Lines    []RepositoryLine
	Arch     string
}

// DiscoverRepositories scans the xbps.d directories in load order, honouring
// xbps's masking rule by basename. Only *-repository-*.conf files contribute
// repository= lines, but architecture= is read from every masked *.conf file
// (dec:#48 §3.1). It does not classify a line as managed: that needs the
// mirror list first.
func DiscoverRepositories(dirs []string) (RepositoryConfig, error) {
	type configFile struct {
		path     string
		basename string
	}
	var files []configFile
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".conf") || seen[name] {
				continue
			}
			seen[name] = true
			files = append(files, configFile{path: filepath.Join(dir, name), basename: name})
		}
	}

	var cfg RepositoryConfig
	for _, file := range files {
		cfg.Files = append(cfg.Files, file.path)
		arch, repos := parseXbpsDConfig(file.path)
		if arch != "" {
			cfg.Arch = arch
		}
		if !isRepositoryConf(file.basename) {
			continue
		}
		for _, value := range repos {
			cfg.Lines = append(cfg.Lines, RepositoryLine{
				File:     file.path,
				Basename: file.basename,
				Value:    value,
			})
		}
	}
	if len(cfg.Lines) == 0 {
		return cfg, ErrNoRepoConfig
	}
	return cfg, nil
}

// isRepositoryConf reports whether name matches dec:#48 §3.1's glob
// *-repository-*.conf.
func isRepositoryConf(name string) bool {
	ok, _ := path.Match("*-repository-*.conf", name)
	return ok
}

// parseXbpsDConfig reads architecture= and repository= values. Inline
// comments and blank lines are ignored; repository= values are returned raw.
func parseXbpsDConfig(path string) (arch string, repositories []string) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch strings.TrimSpace(key) {
		case "architecture":
			arch = value
		case "repository":
			repositories = append(repositories, value)
		}
	}
	return arch, repositories
}

// ClassifyRepositories marks the rewriteable repository lines and derives the
// primary path and required suffix set. A line is managed when its host is
// repo-default.voidlinux.org, a host from the mirror list, or the previous
// chosen mirror recorded in the manifest (dec:#48 §3.2). Custom/local/file://
// lines are preserved verbatim and never probed or validated.
//
// mirrorBases and previousBases are mirror URLs or bare hostnames. Their paths
// are the bases a written repository URL is relative to: the embedded mirror
// list has path-bearing entries (26 of 33), so a second fetch must not treat
// <base>/current as a new /base/current suffix and double the base.
func ClassifyRepositories(cfg RepositoryConfig, mirrorBases, previousBases []string) (RepositorySet, error) {
	allowed := map[string]bool{DefaultRepositoryHost: true}
	basesByHost := map[string][]string{}
	addBase := func(raw string) {
		host, base := splitRepositoryBase(raw)
		if host == "" {
			return
		}
		allowed[host] = true
		for _, existing := range basesByHost[host] {
			if existing == base {
				return
			}
		}
		basesByHost[host] = append(basesByHost[host], base)
	}
	for _, base := range mirrorBases {
		addBase(base)
	}
	for _, base := range previousBases {
		addBase(base)
	}

	set := RepositorySet{Arch: cfg.Arch, Lines: append([]RepositoryLine(nil), cfg.Lines...)}
	seenSuffix := map[string]bool{}
	seenURL := map[string]bool{}
	for i := range set.Lines {
		line := &set.Lines[i]
		u, err := url.Parse(line.Value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue // local path or file://: preserved, never managed
		}
		line.Host = strings.ToLower(u.Hostname())
		fullPath := strings.TrimRight(u.Path, "/")
		if fullPath == "/" {
			fullPath = ""
		}
		line.Suffix = relativeSuffix(fullPath, basesByHost[line.Host])
		line.Managed = allowed[line.Host]
		if !line.Managed {
			continue
		}
		normalised := strings.TrimRight(u.String(), "/")
		if !seenURL[normalised] {
			seenURL[normalised] = true
			set.URLs = append(set.URLs, normalised)
		}
		if !seenSuffix[line.Suffix] {
			seenSuffix[line.Suffix] = true
			set.Suffixes = append(set.Suffixes, line.Suffix)
		}
	}
	if len(set.Suffixes) == 0 {
		return RepositorySet{}, ErrNoManagedRepo
	}
	set.Primary = set.Suffixes[0]
	return set, nil
}

// splitRepositoryBase accepts a mirror URL or a bare hostname and returns the
// lower-case host and its path base without a trailing slash.
func splitRepositoryBase(raw string) (host, base string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", ""
		}
		base = strings.TrimRight(u.Path, "/")
		if base == "/" {
			base = ""
		}
		return strings.ToLower(u.Hostname()), base
	}
	host = normaliseHost(raw)
	return host, ""
}

// relativeSuffix returns the configured path relative to the longest matching
// mirror base for that host. No matching base keeps the whole path.
func relativeSuffix(fullPath string, bases []string) string {
	fullPath = strings.TrimRight(fullPath, "/")
	if fullPath == "/" {
		fullPath = ""
	}
	best := ""
	for _, base := range bases {
		base = strings.TrimRight(base, "/")
		if base == "" {
			continue
		}
		if fullPath == base || strings.HasPrefix(fullPath, base+"/") {
			if len(base) > len(best) {
				best = base
			}
		}
	}
	if best != "" {
		return strings.TrimPrefix(fullPath, best)
	}
	return fullPath
}

// MirrorHosts returns the hostnames of every mirror in the loaded list.
func MirrorHosts(mirrors []Mirror) []string {
	hosts := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		u, err := url.Parse(mirror.URL)
		if err != nil {
			continue
		}
		if host := u.Hostname(); host != "" {
			hosts = append(hosts, strings.ToLower(host))
		}
	}
	return hosts
}

// normaliseHost accepts either a bare hostname or a URL and returns the
// lower-case hostname.
func normaliseHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			return strings.ToLower(u.Hostname())
		}
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.Trim(raw, "[]"))
}

// NonFreeSuffixes derives --non-free's validation set (dec:#48 §6.1):
// <primary>/nonfree plus <suffix>/nonfree for every managed suffix ending in
// /multilib. It never synthesises a multilib path that was not configured.
func NonFreeSuffixes(suffixes []string) []string {
	if len(suffixes) == 0 {
		return nil
	}
	out := []string{strings.TrimRight(suffixes[0], "/") + "/nonfree"}
	seen := map[string]bool{out[0]: true}
	for _, suffix := range suffixes {
		if !strings.HasSuffix(suffix, "/multilib") {
			continue
		}
		derived := strings.TrimRight(suffix, "/") + "/nonfree"
		if !seen[derived] {
			seen[derived] = true
			out = append(out, derived)
		}
	}
	return out
}

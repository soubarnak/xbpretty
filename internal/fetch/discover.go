package fetch

import (
	"bufio"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoRepositories is returned when no configured repository can be probed.
var ErrNoRepositories = errors.New("no repositories are configured; add one in /etc/xbps.d")

// RepositorySet is the repository configuration discovery result. Primary is
// the path suffix of the first remote repository, and Suffixes is every
// distinct remote path suffix in discovery order (primary first).
type RepositorySet struct {
	Primary  string
	Suffixes []string
	URLs     []string
}

// DiscoverRepositories reads repository= lines from the given directories in
// order. The first directory that yields a remote repository wins, so
// /etc/xbps.d masks /usr/share/xbps.d. Files are read in lexical order, which
// is the order xbps uses. Missing directories and local repositories are
// skipped.
func DiscoverRepositories(dirs []string) (RepositorySet, error) {
	var set RepositorySet
	seenSuffix := map[string]bool{}
	seenURL := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		var foundRemote bool
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
				continue
			}
			repos := repositoriesInFile(filepath.Join(dir, entry.Name()))
			for _, raw := range repos {
				u, err := url.Parse(raw)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					continue
				}
				foundRemote = true
				normalised := strings.TrimRight(u.String(), "/")
				if !seenURL[normalised] {
					seenURL[normalised] = true
					set.URLs = append(set.URLs, normalised)
				}
				suffix := strings.TrimRight(u.Path, "/")
				if suffix == "/" {
					suffix = ""
				}
				if !seenSuffix[suffix] {
					seenSuffix[suffix] = true
					set.Suffixes = append(set.Suffixes, suffix)
				}
			}
		}
		if foundRemote {
			break
		}
	}
	if len(set.URLs) == 0 {
		return RepositorySet{}, ErrNoRepositories
	}
	set.Primary = set.Suffixes[0]
	return set, nil
}

// repositoriesInFile returns the repository= values in one xbps config file.
func repositoriesInFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "repository" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

// NonFreeSuffixes returns the validation suffixes --non-free adds (FR-045).
// They are relative to the primary repository path, so /current becomes
// /current/nonfree and /current/multilib/nonfree.
func NonFreeSuffixes(primary string) []string {
	primary = strings.TrimRight(primary, "/")
	return []string{
		primary + "/nonfree",
		primary + "/multilib/nonfree",
	}
}

package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupSuffix names the pre-fetch copy kept beside a masked file (FR-047).
const BackupSuffix = ".xbpretty-backup"

// ManifestSchemaVersion is the only fetch manifest schema this package reads
// and writes (FR-047).
const ManifestSchemaVersion = 1

// Manifest is the schema-1 record at /var/lib/xbpretty/fetch.json. It is the
// sole source clean --fetch reverts through (FR-047, FR-048).
type Manifest struct {
	SchemaVersion int            `json:"schema_version"`
	WrittenAt     string         `json:"written_at"`
	Mirror        string         `json:"mirror"`
	Arch          string         `json:"arch"`
	Files         []ManifestFile `json:"files"`
}

// ManifestFile is one file fetch wrote, or carried over from an earlier run.
type ManifestFile struct {
	Path    string `json:"path"`
	Backup  string `json:"backup,omitempty"`
	Created bool   `json:"created"`
	SHA256  string `json:"sha256"`
}

// PlannedWrite is one masking file fetch will write: the same-basename target,
// the system file it masks, and the exact bytes.
type PlannedWrite struct {
	Basename string
	Source   string
	Path     string
	Content  []byte
	SHA256   string
}

// PlanWrites builds one target per managed basename. The source file is the
// one discovery read, so an existing /etc override is never lost; the target is
// always under targetDir with the same basename xbps masks by. Files with no
// managed repository= line produce no write (FR-047).
func PlanWrites(set RepositorySet, mirror string, writtenAt time.Time, targetDir string, nonFree bool) ([]PlannedWrite, error) {
	type group struct {
		source  string
		managed map[string]bool
	}
	order := make([]string, 0)
	groups := map[string]*group{}
	for _, line := range set.Lines {
		if !line.Managed {
			continue
		}
		g, ok := groups[line.Basename]
		if !ok {
			g = &group{source: line.File, managed: map[string]bool{}}
			groups[line.Basename] = g
			order = append(order, line.Basename)
		}
		g.managed[line.Value] = true
	}

	writes := make([]PlannedWrite, 0, len(order))
	for _, basename := range order {
		g := groups[basename]
		original, err := os.ReadFile(g.source)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", g.source, err)
		}
		content, rewritten := rewriteRepositoryLines(original, mirror, g.managed, writtenAt)
		if rewritten == 0 {
			continue
		}
		sum := sha256.Sum256(content)
		writes = append(writes, PlannedWrite{
			Basename: basename,
			Source:   g.source,
			Path:     filepath.Join(targetDir, basename),
			Content:  content,
			SHA256:   hex.EncodeToString(sum[:]),
		})
	}
	if nonFree {
		writes = append(writes, nonFreeWrites(set, mirror, writtenAt, targetDir)...)
	}
	return writes, nil
}

// nonFreeWrites derives FR-046's non-free files only when the system does not
// already configure that repository path. The primary gets 10-repository-
// nonfree.conf; every managed /multilib path also gets its nonfree sibling.
func nonFreeWrites(set RepositorySet, mirror string, writtenAt time.Time, targetDir string) []PlannedWrite {
	configured := map[string]bool{}
	for _, line := range set.Lines {
		if line.Suffix != "" {
			configured[line.Suffix] = true
		}
	}
	type derived struct {
		suffix   string
		basename string
	}
	var wanted []derived
	add := func(suffix, basename string) {
		suffix = strings.TrimRight(suffix, "/")
		if suffix == "" || configured[suffix] {
			return
		}
		for _, d := range wanted {
			if d.suffix == suffix {
				return
			}
		}
		configured[suffix] = true
		wanted = append(wanted, derived{suffix: suffix, basename: basename})
	}
	primary := ""
	if len(set.Suffixes) > 0 {
		primary = set.Suffixes[0]
	}
	if primary != "" {
		add(primary+"/nonfree", "10-repository-nonfree.conf")
	}
	for _, suffix := range set.Suffixes {
		if strings.HasSuffix(suffix, "/multilib") {
			add(suffix+"/nonfree", "10-repository-multilib-nonfree.conf")
		}
	}

	base := strings.TrimRight(mirror, "/")
	out := make([]PlannedWrite, 0, len(wanted))
	for _, d := range wanted {
		content := fmt.Sprintf("# Written by xbpretty fetch on %s; revert with 'xbp clean --fetch'.\nrepository=%s%s\n",
			writtenAt.Format(time.RFC3339), base, d.suffix)
		sum := sha256.Sum256([]byte(content))
		out = append(out, PlannedWrite{
			Basename: d.basename,
			Path:     filepath.Join(targetDir, d.basename),
			Content:  []byte(content),
			SHA256:   hex.EncodeToString(sum[:]),
		})
	}
	return out
}

// rewriteRepositoryLines prepends FR-047's provenance header and rewrites only
// the managed repository= values. Every other byte of the original line stays
// as it was, including whitespace, comments and unrelated keys.
func rewriteRepositoryLines(original []byte, mirror string, managed map[string]bool, writtenAt time.Time) ([]byte, int) {
	header := fmt.Sprintf("# Written by xbpretty fetch on %s; revert with 'xbp clean --fetch'.\n", writtenAt.Format(time.RFC3339))
	var b strings.Builder
	b.WriteString(header)
	rewritten := 0
	for _, line := range strings.SplitAfter(string(original), "\n") {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			b.WriteString(line)
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) != "repository" {
			b.WriteString(line)
			continue
		}
		raw := strings.TrimSpace(value)
		if !managed[raw] {
			b.WriteString(line)
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			b.WriteString(line)
			continue
		}
		suffix := strings.TrimRight(u.Path, "/")
		if suffix == "/" {
			suffix = ""
		}
		replacement := strings.TrimRight(mirror, "/") + suffix
		if eq := strings.Index(line, "="); eq >= 0 {
			line = line[:eq+1] + strings.Replace(line[eq+1:], raw, replacement, 1)
		}
		b.WriteString(line)
		rewritten++
	}
	if rewritten == 0 {
		return nil, 0
	}
	return []byte(b.String()), rewritten
}

// WrittenRepositoryURLs returns the repository URLs this run would write: the
// chosen mirror plus each managed line's preserved path suffix. FR-049 scopes
// sync errors to exactly these URLs.
func WrittenRepositoryURLs(set RepositorySet, mirror string) []string {
	base := strings.TrimRight(mirror, "/")
	var out []string
	seen := map[string]bool{}
	for _, line := range set.Lines {
		if !line.Managed {
			continue
		}
		u := base + strings.TrimRight(line.Suffix, "/")
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// ScanSyncErrors inspects the captured xbps-install -S output. xbps exits 0
// even when a repository fails, so FR-049 fails the run only when an ERROR:
// line names one of writtenURLs; other ERROR: lines are foreign and merely
// warned about.
func ScanSyncErrors(stdout, stderr string, writtenURLs []string) (failed, foreign bool) {
	for _, stream := range []string{stdout, stderr} {
		for _, line := range strings.Split(stream, "\n") {
			line = strings.TrimSuffix(line, "\r")
			if !strings.HasPrefix(line, "ERROR:") {
				continue
			}
			matchesWritten := false
			for _, written := range writtenURLs {
				if written != "" && strings.Contains(line, written) {
					matchesWritten = true
					break
				}
			}
			if matchesWritten {
				failed = true
			} else {
				foreign = true
			}
		}
	}
	return failed, foreign
}

// ReadManifest reads and validates one schema-1 manifest. A missing file
// returns an error satisfying os.IsNotExist.
func ReadManifest(path string) (Manifest, error) {
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
	return manifest, nil
}

// WriteManifest writes the schema-1 manifest atomically at 0644.
func WriteManifest(path string, manifest Manifest) error {
	if manifest.SchemaVersion == 0 {
		manifest.SchemaVersion = ManifestSchemaVersion
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if manifest.Files == nil {
		manifest.Files = []ManifestFile{}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}

// ApplyWrites commits every planned file and the manifest as one unit. Backups
// are made before any target changes, targets are renamed into place only after
// every temp exists, and a failure rolls the renames back. Entries for files
// this run did not touch are carried over from previous (FR-047).
func ApplyWrites(writes []PlannedWrite, previous Manifest, manifestPath string, writtenAt time.Time, mirror, arch string) (Manifest, error) {
	previousByPath := map[string]ManifestFile{}
	for _, entry := range previous.Files {
		previousByPath[entry.Path] = entry
	}

	targets := make([]writeTarget, 0, len(writes))
	for _, write := range writes {
		if err := os.MkdirAll(filepath.Dir(write.Path), 0o755); err != nil {
			return Manifest{}, fmt.Errorf("writing %s: %w", write.Path, err)
		}
		info, statErr := os.Lstat(write.Path)
		exists := statErr == nil && !info.IsDir()
		if statErr != nil && !os.IsNotExist(statErr) {
			return Manifest{}, fmt.Errorf("writing %s: %w", write.Path, statErr)
		}
		var original []byte
		if exists {
			var err error
			original, err = os.ReadFile(write.Path)
			if err != nil {
				return Manifest{}, fmt.Errorf("writing %s: %w", write.Path, err)
			}
		}

		t := writeTarget{write: write, exists: exists, original: original}
		if prev, ok := previousByPath[write.Path]; ok {
			switch {
			case prev.Backup != "" && fileExists(prev.Backup):
				t.backup = prev.Backup
			case prev.Created:
				t.created = true
			case exists:
				t.backup = write.Path + BackupSuffix
			default:
				t.created = true
			}
		} else if exists {
			t.backup = write.Path + BackupSuffix
		} else {
			t.created = true
		}
		targets = append(targets, t)
	}

	// Backups first: a failure here touches no target and removes the backups
	// this run created.
	createdBackups := make([]string, 0)
	for _, t := range targets {
		if t.backup == "" || fileExists(t.backup) || !t.exists {
			continue
		}
		if err := writeFileAtomic(t.backup, t.original); err != nil {
			removeAll(createdBackups)
			return Manifest{}, fmt.Errorf("writing %s: %w", t.backup, err)
		}
		createdBackups = append(createdBackups, t.backup)
	}

	// Prepare every target temp before renaming any of them.
	temps := make([]string, len(targets))
	for i, t := range targets {
		temp, err := prepareFileAtomic(t.write.Path, t.write.Content)
		if err != nil {
			removeAll(temps)
			removeAll(createdBackups)
			return Manifest{}, fmt.Errorf("writing %s: %w", t.write.Path, err)
		}
		temps[i] = temp
	}

	renamed := make([]int, 0, len(targets))
	for i, t := range targets {
		if err := os.Rename(temps[i], t.write.Path); err != nil {
			removeAll(temps[i:])
			rollbackTargets(targets, renamed)
			removeAll(createdBackups)
			return Manifest{}, fmt.Errorf("writing %s: %w", t.write.Path, err)
		}
		temps[i] = ""
		renamed = append(renamed, i)
	}

	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		WrittenAt:     writtenAt.Format(time.RFC3339),
		Mirror:        mirror,
		Arch:          arch,
		Files:         make([]ManifestFile, 0, len(targets)+len(previous.Files)),
	}
	writtenPaths := map[string]bool{}
	for _, t := range targets {
		writtenPaths[t.write.Path] = true
		manifest.Files = append(manifest.Files, ManifestFile{
			Path:    t.write.Path,
			Backup:  t.backup,
			Created: t.created,
			SHA256:  t.write.SHA256,
		})
	}
	for _, entry := range previous.Files {
		if !writtenPaths[entry.Path] {
			manifest.Files = append(manifest.Files, entry)
		}
	}
	if err := WriteManifest(manifestPath, manifest); err != nil {
		rollbackTargets(targets, renamed)
		removeAll(createdBackups)
		return Manifest{}, fmt.Errorf("writing %s: %w", manifestPath, err)
	}
	return manifest, nil
}

// writeTarget is one planned file with the pre-run state rollback needs.
type writeTarget struct {
	write    PlannedWrite
	exists   bool
	original []byte
	backup   string
	created  bool
}

// rollbackTargets restores the pre-run state of every renamed target.
func rollbackTargets(targets []writeTarget, renamed []int) {
	for i := len(renamed) - 1; i >= 0; i-- {
		t := targets[renamed[i]]
		if t.exists {
			_ = writeFileAtomic(t.write.Path, t.original)
		} else {
			_ = os.Remove(t.write.Path)
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func removeAll(paths []string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

// prepareFileAtomic writes content to a temp file in the target's directory,
// so a later rename stays on one filesystem. The caller renames it.
func prepareFileAtomic(path string, content []byte) (string, error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".xbpretty.tmp-")
	if err != nil {
		return "", err
	}
	name := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(name)
	}
	if err := f.Chmod(0o644); err != nil {
		cleanup()
		return "", err
	}
	if _, err := f.Write(content); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if os.Geteuid() == 0 {
		if err := f.Chown(0, 0); err != nil {
			cleanup()
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// writeFileAtomic writes one file at 0644 root:root by temp+rename. It is the
// manifest, backup and clean --fetch restore writer.
func writeFileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := prepareFileAtomic(path, content)
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	syncDir(filepath.Dir(path))
	return nil
}

func syncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// WriteAtomic exposes the atomic 0644 writer clean --fetch uses to restore a
// backup. It never creates the xbps.d directory's parents beyond one level of
// MkdirAll, which the release binary only ever calls for /etc/xbps.d.
func WriteAtomic(path string, content []byte) error { return writeFileAtomic(path, content) }

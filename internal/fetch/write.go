package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
// the system file it masks, and the exact bytes. Derived marks a file
// --non-free synthesised rather than one discovered on the system.
type PlannedWrite struct {
	Basename string
	Source   string
	Path     string
	Content  []byte
	SHA256   string
	Derived  bool
	URLs     []string // repository URLs this file contributes (managed only)
}

// PreImage is one target's bytes immediately before this run wrote it. A sync
// failure restores these, not the first fetch's .xbpretty-backup, so a re-fetch
// that fails returns to the configuration that was live when it started.
type PreImage struct {
	Path    string
	Existed bool
	Content []byte
}

// PlanWrites builds one target per managed basename. The source file is the
// one discovery read, so an existing /etc override is never lost; the target is
// always under targetDir with the same basename xbps masks by. Files with no
// managed repository= line produce no write (FR-047).
func PlanWrites(set RepositorySet, mirror string, writtenAt time.Time, targetDir string, nonFree bool) ([]PlannedWrite, error) {
	type group struct {
		source  string
		managed map[string]string // raw repository= value -> relative path suffix
	}
	order := make([]string, 0)
	groups := map[string]*group{}
	for _, line := range set.Lines {
		if !line.Managed {
			continue
		}
		g, ok := groups[line.Basename]
		if !ok {
			g = &group{source: line.File, managed: map[string]string{}}
			groups[line.Basename] = g
			order = append(order, line.Basename)
		}
		g.managed[line.Value] = line.Suffix
	}

	writes := make([]PlannedWrite, 0, len(order))
	byPath := map[string]bool{}
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
		path := filepath.Join(targetDir, basename)
		if byPath[path] {
			continue
		}
		byPath[path] = true
		writes = append(writes, PlannedWrite{
			Basename: basename,
			Source:   g.source,
			Path:     path,
			Content:  content,
			SHA256:   hex.EncodeToString(sum[:]),
			URLs:     managedURLs(g.managed, mirror),
		})
	}
	if nonFree {
		writes = appendNonFreeWrites(writes, set, mirror, writtenAt, targetDir, byPath)
	}
	return writes, nil
}

// appendNonFreeWrites derives FR-046's non-free files only when the system
// does not already configure that repository path. The primary base is the
// first managed suffix that is not itself non-free; every managed /multilib
// path also gets its nonfree sibling. A derived basename already planned from a
// discovered file is never duplicated.
func appendNonFreeWrites(writes []PlannedWrite, set RepositorySet, mirror string, writtenAt time.Time, targetDir string, byPath map[string]bool) []PlannedWrite {
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
		path := filepath.Join(targetDir, basename)
		if byPath[path] {
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
	for _, suffix := range set.Suffixes {
		if suffix != "" && !strings.HasSuffix(suffix, "/nonfree") {
			primary = suffix
			break
		}
	}
	if primary != "" {
		basename := "10-repository-nonfree.conf"
		if strings.HasSuffix(primary, "/multilib") {
			basename = "10-repository-multilib-nonfree.conf"
		}
		add(primary+"/nonfree", basename)
	}
	for _, suffix := range set.Suffixes {
		if strings.HasSuffix(suffix, "/multilib") {
			add(suffix+"/nonfree", "10-repository-multilib-nonfree.conf")
		}
	}

	base := strings.TrimRight(mirror, "/")
	for _, d := range wanted {
		content := fmt.Sprintf("# Written by xbpretty fetch on %s; revert with 'xbp clean --fetch'.\nrepository=%s%s\n",
			writtenAt.Format(time.RFC3339), base, d.suffix)
		sum := sha256.Sum256([]byte(content))
		path := filepath.Join(targetDir, d.basename)
		byPath[path] = true
		writes = append(writes, PlannedWrite{
			Basename: d.basename,
			Path:     path,
			Content:  []byte(content),
			SHA256:   hex.EncodeToString(sum[:]),
			Derived:  true,
			URLs:     []string{base + d.suffix},
		})
	}
	return writes
}

// rewriteRepositoryLines prepends FR-047's provenance header and rewrites only
// the managed repository= values. Every other byte of the original line stays
// as it was, including whitespace, comments and unrelated keys. An earlier
// fetch's header is replaced, never stacked.
func rewriteRepositoryLines(original []byte, mirror string, managed map[string]string, writtenAt time.Time) ([]byte, int) {
	header := fmt.Sprintf("# Written by xbpretty fetch on %s; revert with 'xbp clean --fetch'.\n", writtenAt.Format(time.RFC3339))
	var b strings.Builder
	b.WriteString(header)
	rewritten := 0
	lines := strings.SplitAfter(string(original), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		if i == 0 && isProvenanceHeader(line) {
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
		suffix, ok := managed[raw]
		if !ok {
			b.WriteString(line)
			continue
		}
		replacement := strings.TrimRight(mirror, "/") + strings.TrimRight(suffix, "/")
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

// isProvenanceHeader reports whether a line is the header a previous fetch
// wrote at the top of a masking file.
func isProvenanceHeader(line string) bool {
	const prefix = "# Written by xbpretty fetch on "
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	return strings.HasSuffix(strings.TrimSuffix(line, "\n"), "; revert with 'xbp clean --fetch'.")
}

// managedURLs returns the rewritten repository URLs a file contributes: the
// chosen mirror plus each managed line's relative suffix, deduplicated.
func managedURLs(managed map[string]string, mirror string) []string {
	base := strings.TrimRight(mirror, "/")
	var out []string
	seen := map[string]bool{}
	for _, suffix := range managed {
		value := base + strings.TrimRight(suffix, "/")
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// WrittenURLs returns the repository URLs the files this run writes actually
// contribute, including --non-free's derived files and excluding preserved
// custom/local lines. FR-049 scopes sync errors to exactly these URLs.
func WrittenURLs(writes []PlannedWrite) []string {
	var out []string
	seen := map[string]bool{}
	for _, write := range writes {
		for _, value := range write.URLs {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// errorLineNamesURL reports whether a sync ERROR line names url as a whole
// repository path, so /current does not match /current-extra.
func errorLineNamesURL(line, url string) bool {
	offset := 0
	for {
		index := strings.Index(line[offset:], url)
		if index < 0 {
			return false
		}
		end := offset + index + len(url)
		if end == len(line) {
			return true
		}
		switch line[end] {
		case '/', '\\', '\'', '"', ' ', '\t', ':', ',', ')', ']':
			return true
		}
		offset += index + 1
	}
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
				if written != "" && errorLineNamesURL(line, written) {
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
// this run did not touch are carried over from previous (FR-047). The returned
// pre-images restore each target, and the returned backup paths are the ones
// this run wrote, which a revert removes once the configuration is back.
func ApplyWrites(writes []PlannedWrite, previous Manifest, manifestPath string, writtenAt time.Time, mirror, arch string) (Manifest, []PreImage, []string, error) {
	previousByPath := map[string]ManifestFile{}
	for _, entry := range previous.Files {
		previousByPath[entry.Path] = entry
	}

	targets := make([]writeTarget, 0, len(writes))
	preImages := make([]PreImage, 0, len(writes))
	seenPath := map[string]bool{}
	for _, write := range writes {
		if seenPath[write.Path] {
			continue
		}
		seenPath[write.Path] = true
		info, statErr := os.Lstat(write.Path)
		exists := statErr == nil && !info.IsDir()
		if statErr != nil && !os.IsNotExist(statErr) {
			return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", write.Path, statErr)
		}
		if write.Derived && exists {
			prev, managed := previousByPath[write.Path]
			if !managed || !prev.Created {
				return Manifest{}, nil, nil, fmt.Errorf("refusing to overwrite %s: not created by xbp fetch", write.Path)
			}
		}
		if err := os.MkdirAll(filepath.Dir(write.Path), 0o755); err != nil {
			return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", write.Path, err)
		}
		var original []byte
		if exists {
			var err error
			original, err = os.ReadFile(write.Path)
			if err != nil {
				return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", write.Path, err)
			}
		}
		preImages = append(preImages, PreImage{Path: write.Path, Existed: exists, Content: original})

		t := writeTarget{write: write, exists: exists, original: original}
		prev, managed := previousByPath[write.Path]
		switch {
		case managed && prev.Backup != "" && fileExists(prev.Backup):
			// The manifest's backup is the pre-first-fetch file; keep it.
			t.backup = prev.Backup
		case managed && prev.Created:
			// The pre-fetch state was absence, whatever the file holds now (a
			// re-fetch wrote it), so no backup is taken and clean removes it.
			t.created = true
		case exists:
			// Nothing trustworthy is recorded, so the bytes the target holds now
			// are the pre-run state. Snapshot them afresh, never reusing a file
			// already at the backup path: it may be stale.
			t.backup = write.Path + BackupSuffix
			t.snapshot = true
		default:
			t.created = true
		}
		targets = append(targets, t)
	}

	// Backups first: a failure here touches no target and removes the backups
	// this run wrote. A reused manifest backup is left as it is.
	createdBackups := make([]string, 0)
	for _, t := range targets {
		takesSnapshot := t.backup != "" && t.exists && (t.snapshot || !fileExists(t.backup))
		if !takesSnapshot {
			continue
		}
		if err := writeFileAtomic(t.backup, t.original); err != nil {
			removeAll(createdBackups)
			return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", t.backup, err)
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
			return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", t.write.Path, err)
		}
		temps[i] = temp
	}

	renamed := make([]int, 0, len(targets))
	for i, t := range targets {
		if err := os.Rename(temps[i], t.write.Path); err != nil {
			removeAll(temps[i:])
			rollbackTargets(targets, renamed)
			removeAll(createdBackups)
			return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", t.write.Path, err)
		}
		temps[i] = ""
		renamed = append(renamed, i)
	}
	dirs := map[string]bool{}
	for _, t := range targets {
		dirs[filepath.Dir(t.write.Path)] = true
	}
	for dir := range dirs {
		syncDir(dir)
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
		return Manifest{}, nil, nil, fmt.Errorf("writing %s: %w", manifestPath, err)
	}
	return manifest, preImages, createdBackups, nil
}

// writeTarget is one planned file with the pre-run state rollback needs.
// snapshot marks a backup taken from the bytes the target holds now, which
// replaces whatever is at the backup path, rather than a manifest's backup.
type writeTarget struct {
	write    PlannedWrite
	exists   bool
	original []byte
	backup   string
	created  bool
	snapshot bool
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

// ReadRegularFileNoFollow reads a manifest backup or target only when it is a
// regular file and not a symlink, so a crafted link cannot redirect a restore.
func ReadRegularFileNoFollow(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

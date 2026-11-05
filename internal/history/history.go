// Package history is the schema-1 history store and its replayable entries
// (ADR-0013, FR-029 .. FR-032). The store is system state: one pretty-printed
// JSON file per successful transaction, published atomically with a temp file
// and an exclusive link. Reads never write.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the only schema the store reads and writes.
const SchemaVersion = 1

// DefaultDir is the history store used when neither --history-dir nor
// XBPRETTY_HISTORY_DIR names one.
const DefaultDir = "/var/lib/xbpretty/history"

const (
	// maxEntryBytes caps a single entry file. A schema-1 entry is a few
	// kilobytes; a larger file is corrupt, not a history record.
	maxEntryBytes = 4 << 20
	// staleTempAge is how old a .json.tmp must be before a write may sweep
	// it. A live writer's temp is seconds old, so a concurrent sweep never
	// steals it.
	staleTempAge = time.Hour
)

// Status is the entry's run outcome. Applied is the only status written;
// Unknown is accepted on read but never replayable.
const (
	StatusApplied = "Applied"
	StatusUnknown = "Unknown"
)

// Operations, in the fixed group order FR-032 renders them.
const (
	OpRemove     = "Remove"
	OpAutoRemove = "AutoRemove"
	OpPurge      = "Purge"
	OpAutoPurge  = "AutoPurge"
	OpInstall    = "Install"
	OpReinstall  = "Reinstall"
	OpUpgrade    = "Upgrade"
	OpDowngrade  = "Downgrade"
	OpConfigure  = "Configure"
	OpHeld       = "Held"
)

// GroupOrder is FR-032's fixed operation order for the detail view.
var GroupOrder = []string{
	OpRemove, OpAutoRemove, OpPurge, OpAutoPurge, OpInstall,
	OpReinstall, OpUpgrade, OpDowngrade, OpConfigure, OpHeld,
}

// ErrNotFound reports a missing or non-numeric history entry.
var ErrNotFound = errors.New("history entry not found")

// PackageState is one package's recorded state. A nil state means absent.
type PackageState struct {
	Version       *string `json:"version"`
	AutoInstalled *bool   `json:"auto_installed"`
	Hold          bool    `json:"hold"`
	Repolock      bool    `json:"repolock"`
}

// Transition is one package change inside an entry.
type Transition struct {
	Name          string        `json:"name"`
	Arch          string        `json:"arch"`
	Repository    string        `json:"repository"`
	Size          uint64        `json:"size"`
	InstalledSize uint64        `json:"installed_size"`
	Operation     string        `json:"operation"`
	Before        *PackageState `json:"before"`
	After         *PackageState `json:"after"`
	HeldReason    string        `json:"held_reason,omitempty"`
}

// Entry is one schema-1 history entry.
type Entry struct {
	SchemaVersion    int          `json:"schema_version"`
	ID               uint32       `json:"id"`
	StartedAt        string       `json:"started_at"`
	FinishedAt       string       `json:"finished_at"`
	Status           string       `json:"status"`
	RequestedBy      string       `json:"requested_by"`
	Command          []string     `json:"command"`
	RequestedTargets []string     `json:"requested_targets"`
	Implicit         bool         `json:"implicit"`
	ReplayOf         *uint32      `json:"replay_of"`
	ReplayKind       *string      `json:"replay_kind"`
	Packages         []Transition `json:"packages"`
}

// Altered is FR-030's derived count: every transition that is not a hold.
func (e Entry) Altered() int {
	n := 0
	for _, t := range e.Packages {
		if t.Operation != OpHeld {
			n++
		}
	}
	return n
}

// Store is one history directory.
type Store struct {
	dir string
}

// New returns a store rooted at dir.
func New(dir string) *Store { return &Store{dir: dir} }

// Dir returns the store directory.
func (s *Store) Dir() string { return s.dir }

// List reads every entry in ascending id order. Missing directories are empty.
// Any unreadable or invalid entry fails the whole read.
func (s *Store) List() ([]Entry, error) {
	names, err := s.entryNames()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		entry, err := s.readFile(name)
		if err != nil {
			return nil, err
		}
		fileID, _ := strconv.ParseUint(strings.TrimSuffix(name, ".json"), 10, 32)
		if entry.ID != uint32(fileID) {
			return nil, fmt.Errorf("history entry %s: id %d does not match the filename", s.path(name), entry.ID)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Last returns the highest-id entry. ok is false when the store is empty.
func (s *Store) Last() (Entry, bool, error) {
	entries, err := s.List()
	if err != nil {
		return Entry{}, false, err
	}
	if len(entries) == 0 {
		return Entry{}, false, nil
	}
	return entries[len(entries)-1], true, nil
}

// Load reads one entry. An absent store or file is ErrNotFound; a corrupt
// file is an error naming the path.
func (s *Store) Load(id uint32) (Entry, error) {
	name := strconv.FormatUint(uint64(id), 10) + ".json"
	entry, err := s.readFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	if entry.ID != id {
		return Entry{}, fmt.Errorf("history entry %s: id %d does not match the filename", s.path(name), entry.ID)
	}
	return entry, nil
}

// Write publishes one entry and returns it with the allocated id. The id is
// max existing + 1; a lost race retries with id+1. The file is serialized to
// <id>.json.tmp, fsynced, linked to <id>.json and the directory is fsynced.
// Stale temp files are removed first.
func (s *Store) Write(entry Entry) (Entry, error) {
	if entry.SchemaVersion == 0 {
		entry.SchemaVersion = SchemaVersion
	}
	if entry.SchemaVersion != SchemaVersion {
		return Entry{}, fmt.Errorf("history entry: unsupported schema_version %d", entry.SchemaVersion)
	}
	if entry.Command == nil {
		entry.Command = []string{}
	}
	if entry.RequestedTargets == nil {
		entry.RequestedTargets = []string{}
	}
	if entry.Packages == nil {
		entry.Packages = []Transition{}
	}
	// The store directory and every ancestor this call creates are 0755
	// root:root. A directory that already exists keeps its permissions, so
	// an operator-supplied read-only --history-dir fails closed instead of
	// being silently widened.
	if err := ensureDir(s.dir); err != nil {
		return Entry{}, err
	}
	if err := s.removeStaleTemps(); err != nil {
		return Entry{}, err
	}
	id, err := s.nextID()
	if err != nil {
		return Entry{}, err
	}
	attempts := 0
	for {
		attempts++
		if attempts > 1000 {
			return Entry{}, fmt.Errorf("allocating a history id: too many collisions")
		}
		entry.ID = id
		final := s.path(strconv.FormatUint(uint64(id), 10) + ".json")
		tmp := final + ".tmp"
		if err := writeTemp(tmp, entry); err != nil {
			if errors.Is(err, os.ErrExist) {
				id++
				continue
			}
			return Entry{}, err
		}
		if err := os.Link(tmp, final); err != nil {
			_ = os.Remove(tmp)
			if errors.Is(err, os.ErrExist) {
				id++
				continue
			}
			// A concurrent writer's stale-temp sweep can remove our temp
			// between serialization and the link; serialise it again.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Entry{}, fmt.Errorf("publishing %s: %w", final, err)
		}
		if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Entry{}, fmt.Errorf("removing %s: %w", tmp, err)
		}
		if err := syncDir(s.dir); err != nil {
			return Entry{}, fmt.Errorf("syncing %s: %w", s.dir, err)
		}
		return entry, nil
	}
}

// Delete removes one entry and sweeps stale temp files. #70's history clear
// consumes it.
func (s *Store) Delete(id uint32) error {
	if err := s.removeStaleTemps(); err != nil {
		return err
	}
	err := os.Remove(s.path(strconv.FormatUint(uint64(id), 10) + ".json"))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("removing history entry %d: %w", id, err)
	}
	return syncDir(s.dir)
}

// Clear removes every numeric entry plus stale temp files and returns the
// number of entries deleted. #70's `history clear --all` consumes it.
func (s *Store) Clear() (int, error) {
	names, err := s.entryNames()
	if err != nil {
		return 0, err
	}
	if err := s.removeStaleTemps(); err != nil {
		return 0, err
	}
	deleted := 0
	var failures []error
	for _, name := range names {
		if err := os.Remove(s.path(name)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			failures = append(failures, fmt.Errorf("removing history entry %s: %w", s.path(name), err))
			continue
		}
		deleted++
	}
	if deleted > 0 {
		if err := syncDir(s.dir); err != nil {
			failures = append(failures, err)
		}
	}
	return deleted, errors.Join(failures...)
}

// readFile parses one entry file and validates the schema contract. Only a
// regular file is an entry: a FIFO or device at an entry name is corrupt, and
// the read never blocks on it.
func (s *Store) readFile(name string) (Entry, error) {
	path := s.path(name)
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Entry{}, fmt.Errorf("history entry %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Entry{}, fmt.Errorf("history entry %s: not a regular file", path)
	}
	if info.Size() > maxEntryBytes {
		return Entry{}, fmt.Errorf("history entry %s: file is larger than %d bytes", path, maxEntryBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEntryBytes+1))
	if err != nil {
		return Entry{}, fmt.Errorf("history entry %s: %w", path, err)
	}
	if len(data) > maxEntryBytes {
		return Entry{}, fmt.Errorf("history entry %s: file is larger than %d bytes", path, maxEntryBytes)
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, fmt.Errorf("history entry %s: %w", path, err)
	}
	if err := validateEntry(path, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// validOperations is the closed FR-030 operation set.
var validOperations = func() map[string]bool {
	set := make(map[string]bool, len(GroupOrder))
	for _, operation := range GroupOrder {
		set[operation] = true
	}
	return set
}()

// ValidPackageField reports whether a recorded name, arch or version can
// safely become part of a cache filename or an xbps argv without escaping the
// directory or being read as a flag. It rejects path separators, NUL and `..`;
// callers additionally require a non-empty name that does not start with `-`
// or `.`.
func ValidPackageField(value string) bool {
	return !strings.ContainsAny(value, "/\\\x00") && !strings.Contains(value, "..")
}

// ValidPackageName is ValidPackageField plus the non-empty, non-flag,
// non-hidden-name rule the replay argv and cache path require.
func ValidPackageName(value string) bool {
	return value != "" && value[0] != '-' && value[0] != '.' && ValidPackageField(value)
}

// validateEntry enforces the required schema-1 fields the read surface uses.
// Timestamps are deliberately not validated: FR-032 shows an unparseable one
// verbatim.
func validateEntry(path string, entry Entry) error {
	if entry.SchemaVersion != SchemaVersion {
		return fmt.Errorf("history entry %s: unsupported schema_version %d", path, entry.SchemaVersion)
	}
	if entry.Status != StatusApplied && entry.Status != StatusUnknown {
		return fmt.Errorf("history entry %s: invalid status %q", path, entry.Status)
	}
	if entry.RequestedBy == "" {
		return fmt.Errorf("history entry %s: missing requested_by", path)
	}
	if entry.Command == nil {
		return fmt.Errorf("history entry %s: missing command", path)
	}
	if entry.Packages == nil {
		return fmt.Errorf("history entry %s: missing packages", path)
	}
	for i, transition := range entry.Packages {
		if transition.Name == "" {
			return fmt.Errorf("history entry %s: packages[%d] has no name", path, i)
		}
		if !ValidPackageName(transition.Name) {
			return fmt.Errorf("history entry %s: packages[%d] has invalid name %q", path, i, transition.Name)
		}
		if transition.Arch != "" && !ValidPackageField(transition.Arch) {
			return fmt.Errorf("history entry %s: packages[%d] has invalid arch %q", path, i, transition.Arch)
		}
		for _, state := range []*PackageState{transition.Before, transition.After} {
			if state == nil || state.Version == nil {
				continue
			}
			if !ValidPackageField(*state.Version) {
				return fmt.Errorf("history entry %s: packages[%d] has invalid version %q", path, i, *state.Version)
			}
		}
		if !validOperations[transition.Operation] {
			return fmt.Errorf("history entry %s: packages[%d] has invalid operation %q", path, i, transition.Operation)
		}
	}
	return nil
}

// entryNames returns the store's entry filenames in ascending id order.
// A missing store has no entries.
func (s *Store) entryNames() ([]string, error) {
	dir, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading history directory %s: %w", s.dir, err)
	}
	type entryFile struct {
		id   uint32
		name string
	}
	var files []entryFile
	for _, de := range dir {
		if !de.Type().IsRegular() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		id64, err := strconv.ParseUint(base, 10, 32)
		if err != nil || strconv.FormatUint(id64, 10) != base {
			continue
		}
		files = append(files, entryFile{id: uint32(id64), name: name})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].id < files[j].id })
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.name
	}
	return names, nil
}

// nextID reads the max numeric filename without parsing contents; a corrupt
// entry never blocks allocation.
func (s *Store) nextID() (uint32, error) {
	dir, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, fmt.Errorf("reading history directory %s: %w", s.dir, err)
	}
	var max uint64
	for _, de := range dir {
		if !de.Type().IsRegular() {
			continue
		}
		name := de.Name()
		if strings.HasSuffix(name, ".json.tmp") {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		if base == name {
			continue
		}
		id, err := strconv.ParseUint(base, 10, 32)
		if err != nil || strconv.FormatUint(id, 10) != base {
			continue
		}
		if id > max {
			max = id
		}
	}
	if max >= uint64(^uint32(0)) {
		return 0, fmt.Errorf("history ids are exhausted")
	}
	return uint32(max + 1), nil
}

// removeStaleTemps deletes leftover <id>.json.tmp files older than
// staleTempAge. It runs only from mutating operations, never from a read, and
// the age gate keeps a concurrent writer's live temp safe from the sweep.
func (s *Store) removeStaleTemps() error {
	dir, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading history directory %s: %w", s.dir, err)
	}
	cutoff := time.Now().Add(-staleTempAge)
	for _, de := range dir {
		if !de.Type().IsRegular() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".json.tmp") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			// The writer that owns a live temp may have published and
			// removed it between ReadDir and this stat.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("reading %s: %w", s.path(name), err)
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(s.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing stale temp file %s: %w", s.path(name), err)
		}
	}
	return nil
}

// writeTemp serializes entry into tmp with O_CREAT|O_EXCL and fsyncs it.
func writeTemp(tmp string, entry Entry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing history entry: %w", err)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return fmt.Errorf("setting permissions on %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	remove = false
	return nil
}

// ensureDir creates dir and every missing ancestor at 0755, defeating a
// restrictive umask. Directories that already exist are untouched: a
// read-only operator-supplied store must fail closed, not be widened.
func ensureDir(dir string) error {
	var missing []string
	for p := dir; ; p = filepath.Dir(p) {
		_, err := os.Stat(p)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
		missing = append(missing, p)
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	for _, p := range missing {
		if err := os.Chmod(p, 0o755); err != nil {
			return fmt.Errorf("setting permissions on %s: %w", p, err)
		}
	}
	return nil
}

// syncDir fsyncs a directory so a link survives a crash.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (s *Store) path(name string) string { return s.dir + string(os.PathSeparator) + name }

package download

import (
	"fmt"
	"path/filepath"
)

// ArchiveNeedSize is the archive bytes a cache fill would transfer for target:
// the plan's size when the archive is absent and zero when it is present. The
// disk preflight sums it, so a fully cached plan asks for nothing and a
// signature-only repair adds no archive bytes (signatures are tiny and capped
// separately).
//
// It stats instead of hashing on purpose: the engine's scan owns the hash
// pass, and the preflight must not read every cached archive a second time.
// The cost of that choice is that a present-but-mismatching archive is not
// counted here; the scan discovers it and refetches it anyway, and a genuine
// space shortage then fails the transfer with the fallback warning.
func ArchiveNeedSize(target Target, destDir string) uint64 {
	if destDir == "" {
		destDir = "."
	}
	if isRegularFile(filepath.Join(destDir, target.Filename)) {
		return 0
	}
	return target.Size
}

// Preflight checks that destDir has at least need bytes free before a transfer
// starts (FR-038). A need of zero is always satisfied; a platform without a
// statvfs equivalent skips the check.
func Preflight(destDir string, need uint64) error {
	if need == 0 {
		return nil
	}
	free, err := FreeBytes(destDir)
	if err != nil {
		return err
	}
	if free < need {
		return fmt.Errorf("not enough free space in %s: need %d bytes, have %d", destDir, need, free)
	}
	return nil
}

package download

import (
	"fmt"
	"path/filepath"
	"strings"
)

// NeedsFetch reports whether the cache would transfer at least one file for
// this target under FR-038's scan rules. It is the read-only half of scan: the
// disk preflight asks it so a fully cached plan never trips the check, and it
// never mutates the cache.
func NeedsFetch(target Target, destDir string) bool {
	if destDir == "" {
		destDir = "."
	}
	archive := filepath.Join(destDir, target.Filename)
	signature := archive + ".sig2"
	if !isRegularFile(archive) {
		return true
	}
	if target.SHA256 != "" {
		got, err := fileSHA256(archive)
		if err != nil || !strings.EqualFold(got, target.SHA256) {
			return true
		}
	}
	return !isRegularFile(signature)
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

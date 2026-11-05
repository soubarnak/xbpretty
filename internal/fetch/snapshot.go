package fetch

import (
	"bytes"
	_ "embed"
	"errors"
)

// SnapshotDate is the date the embedded mirror list was refreshed (FR-043,
// FR-076). `make mirrors` regenerates the list at release time.
const SnapshotDate = "2026-11-05"

//go:embed snapshot/mirrors.lst
var snapshotList []byte

// Snapshot parses the compiled-in 33-row mirror list fallback (NFR-008). It
// reads no disk file: the snapshot is part of the binary.
func Snapshot() ([]Mirror, error) {
	mirrors, err := ParseMirrors(bytes.NewReader(snapshotList))
	if err != nil {
		return nil, err
	}
	if len(mirrors) == 0 {
		return nil, errors.New("the built-in mirror snapshot is empty")
	}
	return mirrors, nil
}

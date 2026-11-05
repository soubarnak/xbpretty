// Command parity validates the two normative parity TSVs and regenerates the
// renderings derived from them: the generated region of xbpretty-spec.md's
// Appendix A and docs/parity/matrix.md.
//
// Usage:
//
//	go run ./tools/parity            # check and print a summary
//	go run ./tools/parity -check     # check only
//	go run ./tools/parity -write     # regenerate the renderings
//	go test ./tools/parity           # the same check, as a test
//
// The TSVs are the only source. A rendering is never hand-edited.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	write := flag.Bool("write", false, "regenerate xbpretty-spec.md Appendix A and docs/parity/matrix.md")
	check := flag.Bool("check", false, "check only (default when -write is absent)")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	m, err := Load(root)
	if err != nil {
		fail(err)
	}
	if err := m.Validate(root); err != nil {
		fail(err)
	}
	if *write {
		if err := m.Write(root); err != nil {
			fail(err)
		}
		fmt.Printf("parity: regenerated Appendix A and docs/parity/matrix.md (%d rows)\n", len(m.Matrix))
		return
	}
	_ = check
	if err := m.CheckFresh(root); err != nil {
		fail(err)
	}
	fmt.Printf("parity: %d inventory rows, %d matrix rows, %d n/a rows; renderings fresh\n",
		len(m.Inventory), len(m.Matrix), m.NACount())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

// repoRoot walks up from the working directory until it finds go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("parity: cannot find the repository root (no go.mod above %s)", dir)
		}
		dir = parent
	}
}

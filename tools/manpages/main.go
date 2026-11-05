// Command manpages regenerates the committed manual pages from internal/spec.
// Run it from the repository root: `make manpages`.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/soubarnak/xbpretty/internal/manpage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	for _, file := range manpage.Files() {
		if err := os.MkdirAll(filepath.Dir(file.Name), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file.Name, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	for _, link := range manpage.AliasLinks() {
		path := filepath.Join("man", link)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink("xbpretty.8", path); err != nil {
			return err
		}
	}
	return nil
}

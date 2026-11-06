// Command mirrors refreshes internal/fetch/snapshot/mirrors.lst from the live
// Void mirror list and stamps its date in snapshot.go (FR-043, FR-076).
//
// It is the implementation of `make mirrors`; the release manager refreshes
// when the embedded snapshot is older than 90 days.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

var snapshotDatePattern = regexp.MustCompile(`const SnapshotDate = "[^"]*"`)

func main() {
	url := flag.String("url", fetch.MirrorListURL, "mirror list URL")
	snapshotPath := flag.String("snapshot", "internal/fetch/snapshot/mirrors.lst", "embedded snapshot path")
	sourcePath := flag.String("source", "internal/fetch/snapshot.go", "Go file holding SnapshotDate")
	flag.Parse()

	body, err := download(*url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mirrors: %v\n", err)
		os.Exit(1)
	}
	mirrors, err := fetch.ParseMirrors(bytes.NewReader(body))
	if err != nil || len(mirrors) == 0 {
		fmt.Fprintf(os.Stderr, "mirrors: parsing %s: %v\n", *url, err)
		os.Exit(1)
	}
	if err := writeFile(*snapshotPath, body); err != nil {
		fmt.Fprintf(os.Stderr, "mirrors: writing %s: %v\n", *snapshotPath, err)
		os.Exit(1)
	}
	if err := updateSnapshotDate(*sourcePath, time.Now().UTC().Format("2006-01-02")); err != nil {
		fmt.Fprintf(os.Stderr, "mirrors: updating %s: %v\n", *sourcePath, err)
		os.Exit(1)
	}
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xbpretty/mirrors")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%s: empty mirror list", url)
	}
	return body, nil
}

func updateSnapshotDate(path, date string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := snapshotDatePattern.ReplaceAllString(string(data), `const SnapshotDate = "`+date+`"`)
	if updated == string(data) {
		return fmt.Errorf("%s has no SnapshotDate constant", path)
	}
	return writeFile(path, []byte(updated))
}

// writeFile writes through a temporary file in the same directory and renames
// it, so a failed refresh never leaves a half-written snapshot.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mirrors-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

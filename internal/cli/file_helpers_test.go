package cli

import "os"

// writeFile writes one test file.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

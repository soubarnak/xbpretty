package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenTransactionLogReadRefusesSymlinkAndMissing(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "log")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenTransactionLogRead(link); err == nil {
		f.Close()
		t.Fatal("symlinked log was opened for reading")
	}
	if _, err := OpenTransactionLogRead(filepath.Join(dir, "none")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing log error = %v, want ErrNotExist", err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenTransactionLogRead(plain)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

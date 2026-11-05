package download

import (
	"strings"
	"testing"
)

// parityHashOnlySHA256 proves P-BEH-036: filename-sha256 is the only accepted
// content hash. SHA-512, MD5 and empty values are not treated as hashes, so a
// size-only or wrong-algorithm answer is never accepted.
func parityHashOnlySHA256(t *testing.T) {
	valid := strings.Repeat("a", 64)
	sha512 := strings.Repeat("b", 128)
	md5 := strings.Repeat("c", 32)
	stdout := strings.Join([]string{
		"good-1.0_1: " + valid + " (https://repo.example/current)",
		"sha512-1.0_1: " + sha512 + " (https://repo.example/current)",
		"md5-1.0_1: " + md5 + " (https://repo.example/current)",
		"empty-1.0_1:  (https://repo.example/current)",
	}, "\n")
	hashes := ParseHashes(stdout)
	if got := hashes[HashKeyFor("https://repo.example/current", "good-1.0_1")]; got != valid {
		t.Errorf("valid SHA-256 = %q, want %q", got, valid)
	}
	for _, pkgver := range []string{"sha512-1.0_1", "md5-1.0_1", "empty-1.0_1"} {
		if got := hashes[HashKeyFor("https://repo.example/current", pkgver)]; got != "" {
			t.Errorf("%s hash = %q, want no hash", pkgver, got)
		}
	}

	for name, hash := range map[string]string{
		"sha256": valid,
		"sha512": sha512,
		"md5":    md5,
		"empty":  "",
	} {
		resolved, err := ParseResolved(strings.Join([]string{
			"pkg-1.0_1", "https://repo.example/current", "x86_64", hash,
		}, "\n"))
		if err != nil {
			t.Fatalf("%s resolution: %v", name, err)
		}
		wantHash := name == "sha256"
		if resolved.HasHash != wantHash {
			t.Errorf("%s: HasHash = %v, want %v (%+v)", name, resolved.HasHash, wantHash, resolved)
		}
		if wantHash && resolved.SHA256 != valid {
			t.Errorf("%s: SHA256 = %q, want %q", name, resolved.SHA256, valid)
		}
		if !wantHash && resolved.SHA256 != "" {
			t.Errorf("%s: SHA256 = %q, want empty", name, resolved.SHA256)
		}
	}
}

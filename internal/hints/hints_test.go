package hints

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const fixtureDir = "../../testdata/hints/"

// loadFixture reads a captured stderr file and the exit code in its .meta.
func loadFixture(t *testing.T, path string) (string, int) {
	t.Helper()
	raw, err := os.ReadFile(fixtureDir + path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	meta, err := os.ReadFile(fixtureDir + strings.Replace(path, ".txt", ".meta", 1))
	if err != nil {
		t.Fatalf("reading fixture meta for %s: %v", path, err)
	}
	var exitCode int
	for _, line := range strings.Split(string(meta), "\n") {
		if rest, ok := strings.CutPrefix(line, "EXIT: "); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				t.Fatalf("parsing exit code in %s: %v", path, err)
			}
			exitCode = n
		}
	}
	return string(raw), exitCode
}

// TestCapturedCasesMatch is NFR-004's promise: every captured failure must
// still match its hint. If xbps changes its wording, this fails.
//
// The not-installed case is absent on purpose: xbps prints "is not currently
// installed" on stdout, not stderr, so a stderr-only match cannot fire for it
// (testdata/hints/0607/not-installed.out carries the capture).
func TestCapturedCasesMatch(t *testing.T) {
	tests := []struct {
		fixture string
		hint    string
	}{
		{"0607/stale-index.txt", "stale-index"},
		{"0607/stale-index-upgrade.txt", "stale-index"},
		{"0607/not-found.txt", "not-found"},
		{"0607/lock-wait.txt", "lock-wait"},
		{"0591/xbps-unsigned-update.txt", "xbps-unsigned-update"},
		{"0591/not-found.txt", "not-found"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			stderr, exitCode := loadFixture(t, tt.fixture)
			hint := Lookup(exitCode, stderr)
			if hint == nil {
				t.Fatalf("exit %d with captured stderr matched no hint:\n%s", exitCode, stderr)
			}
			if hint.Name != tt.hint {
				t.Errorf("got hint %q, want %q", hint.Name, tt.hint)
			}
			if hint.Text == "" {
				t.Errorf("hint %q has empty text", hint.Name)
			}
		})
	}
}

// TestStaleIndexFiresOnBothExitCodes records the capture where a stale index
// fails an install with exit 2 but leaves an upgrade with exit 0.
func TestStaleIndexFiresOnBothExitCodes(t *testing.T) {
	stderr, exitCode := loadFixture(t, "0607/stale-index-upgrade.txt")
	if exitCode != 0 {
		t.Fatalf("fixture exit code = %d, want 0", exitCode)
	}
	hint := Lookup(exitCode, stderr)
	if hint == nil {
		t.Fatal("expected a hint for a stale index regardless of exit code")
	}
	if hint.Name != "stale-index" {
		t.Errorf("got %q, want stale-index", hint.Name)
	}
}

// TestUnmatchedFailureGetsNoHint is AC-016: no guessed hints.
func TestUnmatchedFailureGetsNoHint(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		stderr   string
	}{
		{"unknown message", 2, "ERROR: something unprecedented happened.\n"},
		{"right code wrong text", 2, "ERROR: the moon is in the wrong phase.\n"},
		{"right text wrong code", 16, "ERROR: something unprecedented happened.\n"},
		{"success with no warning", 0, ""},
		{"success with unrelated text", 0, "ERROR: something unprecedented happened.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if hint := Lookup(tt.exitCode, tt.stderr); hint != nil {
				t.Errorf("Lookup(%d, %q) = %q, want nil", tt.exitCode, tt.stderr, hint.Name)
			}
		})
	}
}

// TestBothDimensionsMustMatch proves neither the exit code nor the stderr
// substring fires a hint on its own.
func TestBothDimensionsMustMatch(t *testing.T) {
	if hint := Lookup(16, "failed to open repodata"); hint != nil {
		t.Errorf("exit code 16 with a stale-index message matched %q", hint.Name)
	}
	if hint := Lookup(2, "package database locked"); hint != nil {
		t.Errorf("exit code 2 with a lock message matched %q", hint.Name)
	}
	if hint := Lookup(2, "failed to open repodata"); hint == nil {
		t.Error("exit code 2 with a stale-index message matched nothing")
	}
}

// TestShortSubstrings pins the patterns to short substrings, never full lines.
func TestShortSubstrings(t *testing.T) {
	for _, h := range Table {
		if len(h.StderrContains) > 60 {
			t.Errorf("hint %q substring is %d characters; keep patterns short", h.Name, len(h.StderrContains))
		}
		if strings.Contains(h.StderrContains, "\n") {
			t.Errorf("hint %q substring spans lines", h.Name)
		}
	}
}

func TestParseRevdeps(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"several", "boinc-8.0.2_2\nbootiso-4.2.0_3\nclevis-23_1\n", []string{"boinc-8.0.2_2", "bootiso-4.2.0_3", "clevis-23_1"}},
		{"skips blanks", "\nclevis-23_1\n\n", []string{"clevis-23_1"}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRevdeps(strings.NewReader(tt.in))
			if err != nil {
				t.Fatalf("ParseRevdeps: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestParseRevdepsRejectsGarbage(t *testing.T) {
	_, err := ParseRevdeps(strings.NewReader("boinc-8.0.2_2\nnotapackageversion\n"))
	if err == nil {
		t.Fatal("ParseRevdeps succeeded, want error")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error %q does not name line 2", err)
	}
}

func TestForceRemoveHint(t *testing.T) {
	got := ForceRemoveHint("foo", []string{"bar-1.0_1", "baz-2.0_1"})
	if !strings.Contains(got, "bar-1.0_1") || !strings.Contains(got, "baz-2.0_1") {
		t.Errorf("hint %q does not name the dependents", got)
	}
	if !strings.Contains(got, "xbp remove -- -F foo") {
		t.Errorf("hint %q does not contain the force flag", got)
	}
	if !strings.Contains(got, "break those dependents") {
		t.Errorf("hint %q does not warn about breakage", got)
	}
	if ForceRemoveHint("foo", nil) != "" {
		t.Error("no dependents should produce no hint")
	}
}

func TestFixtureCorpusHasTwoVersions(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("reading fixture dir: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) < 2 {
		t.Fatalf("fixture corpus has %d version directories, want at least 2", len(dirs))
	}
	for _, d := range dirs {
		files, err := filepath.Glob(filepath.Join(fixtureDir, d, "*.txt"))
		if err != nil || len(files) == 0 {
			t.Errorf("version directory %s has no captured stderr", d)
		}
	}
}

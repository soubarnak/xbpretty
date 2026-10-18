package hints

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const fixtureDir = "../../testdata/hints/"

// loadFixture reads a captured case: the stderr in <case>.txt, the stdout in
// <case>.out and the exit code in <case>.meta. A case with no .out predates
// the stdout half of the capture, so it carries stderr alone.
func loadFixture(t *testing.T, name string) (Output, int) {
	t.Helper()
	read := func(ext string) (string, bool) {
		raw, err := os.ReadFile(fixtureDir + name + ext)
		if os.IsNotExist(err) {
			return "", false
		}
		if err != nil {
			t.Fatalf("reading fixture %s%s: %v", name, ext, err)
		}
		return string(raw), true
	}
	stderr, ok := read(".txt")
	if !ok {
		t.Fatalf("fixture %s has no stderr capture", name)
	}
	stdout, _ := read(".out")
	meta, ok := read(".meta")
	if !ok {
		t.Fatalf("fixture %s has no meta", name)
	}
	var exitCode int
	for _, line := range strings.Split(meta, "\n") {
		if rest, ok := strings.CutPrefix(line, "EXIT: "); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				t.Fatalf("parsing exit code in %s: %v", name, err)
			}
			exitCode = n
		}
	}
	return Output{Stdout: stdout, Stderr: stderr}, exitCode
}

// TestCapturedCasesMatch is NFR-004's promise: every captured failure must
// still match its hint. If xbps changes its wording, this fails.
func TestCapturedCasesMatch(t *testing.T) {
	tests := []struct {
		fixture string
		hint    string
	}{
		{"0607/stale-index", "stale-index"},
		{"0607/stale-index-upgrade", "stale-index"},
		{"0607/not-found", "not-found"},
		{"0607/not-installed", "not-installed"},
		{"0607/lock-wait", "lock-wait"},
		{"0591/xbps-unsigned-update", "xbps-unsigned-update"},
		{"0591/xbps-unsigned-update-install", "xbps-unsigned-update"},
		{"0591/xbps-unsigned-update-upgrade", "xbps-unsigned-update"},
		{"0591/not-found", "not-found"},
		{"0591/not-installed", "not-installed"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			out, exitCode := loadFixture(t, tt.fixture)
			hint := Lookup(exitCode, out)
			if hint == nil {
				t.Fatalf("exit %d with captured output matched no hint:\nstdout:\n%s\nstderr:\n%s", exitCode, out.Stdout, out.Stderr)
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

// TestHintsMatchWhereXbpsWritesThem is ADR-0006: a message xbps writes to one
// stream does not fire its hint when it turns up on the other, so a captured
// message is matched on its own stream and only there. xbps-unsigned-update
// is the one entry measured on both, and the one that fires either way.
func TestHintsMatchWhereXbpsWritesThem(t *testing.T) {
	tests := []struct {
		fixture string
		// onStream is where the capture holds the message; fires reports
		// whether the same text moved to the other stream still matches.
		onStream Stream
		fires    bool
	}{
		{"0607/stale-index", Stderr, false},
		{"0607/not-found", Stderr, false},
		{"0607/lock-wait", Stderr, false},
		{"0591/not-found", Stderr, false},
		{"0607/not-installed", Stdout, false},
		{"0591/not-installed", Stdout, false},
		{"0591/xbps-unsigned-update", Stderr, true},
		{"0591/xbps-unsigned-update-install", Stderr, true},
		{"0591/xbps-unsigned-update-upgrade", Stdout, true},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			out, exitCode := loadFixture(t, tt.fixture)
			message := out.on(tt.onStream)
			if message == "" {
				t.Fatalf("fixture holds nothing on the stream it is meant to carry")
			}
			if Lookup(exitCode, Output{Stdout: out.Stdout, Stderr: out.Stderr}) == nil {
				t.Fatal("the captured output matched no hint")
			}
			moved := Output{Stderr: message}
			if tt.onStream == Stderr {
				moved = Output{Stdout: message}
			}
			got := Lookup(exitCode, moved)
			if tt.fires && got == nil {
				t.Errorf("the message on the other stream matched nothing; want a hint")
			}
			if !tt.fires && got != nil {
				t.Errorf("the message on the other stream matched %q; want none", got.Name)
			}
		})
	}
}

// TestNotInstalledIsReachableFromStdoutAlone is the ticket's own failure: the
// message is on stdout with stderr empty, so a stderr-only match never fired.
func TestNotInstalledIsReachableFromStdoutAlone(t *testing.T) {
	out, exitCode := loadFixture(t, "0607/not-installed")
	if out.Stderr != "" {
		t.Fatalf("fixture stderr = %q, want empty", out.Stderr)
	}
	hint := Lookup(exitCode, out)
	if hint == nil || hint.Name != "not-installed" {
		t.Fatalf("Lookup = %v, want not-installed", hint)
	}
}

// TestEveryHintNamesItsStreams keeps the table honest: an entry with no stream
// could never fire, and one with no substring would fire on anything.
func TestEveryHintNamesItsStreams(t *testing.T) {
	for _, h := range Table {
		if len(h.Streams) == 0 {
			t.Errorf("hint %q names no stream", h.Name)
		}
		if h.Contains == "" {
			t.Errorf("hint %q has an empty substring", h.Name)
		}
	}
}

// TestStaleIndexFiresOnBothExitCodes records the capture where a stale index
// fails an install with exit 2 but leaves an upgrade with exit 0.
func TestStaleIndexFiresOnBothExitCodes(t *testing.T) {
	out, exitCode := loadFixture(t, "0607/stale-index-upgrade")
	if exitCode != 0 {
		t.Fatalf("fixture exit code = %d, want 0", exitCode)
	}
	hint := Lookup(exitCode, out)
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
		out      Output
	}{
		{"unknown message", 2, Output{Stderr: "ERROR: something unprecedented happened.\n"}},
		{"unknown message on stdout", 2, Output{Stdout: "ERROR: something unprecedented happened.\n"}},
		{"right code wrong text", 2, Output{Stderr: "ERROR: the moon is in the wrong phase.\n"}},
		{"right text wrong code", 16, Output{Stderr: "ERROR: something unprecedented happened.\n"}},
		{"success with no warning", 0, Output{}},
		{"success with unrelated text", 0, Output{Stderr: "ERROR: something unprecedented happened.\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if hint := Lookup(tt.exitCode, tt.out); hint != nil {
				t.Errorf("Lookup(%d, %+v) = %q, want nil", tt.exitCode, tt.out, hint.Name)
			}
		})
	}
}

// TestBothDimensionsMustMatch proves neither the exit code nor the stderr
// substring fires a hint on its own.
func TestBothDimensionsMustMatch(t *testing.T) {
	if hint := Lookup(16, Output{Stderr: "failed to open repodata"}); hint != nil {
		t.Errorf("exit code 16 with a stale-index message matched %q", hint.Name)
	}
	if hint := Lookup(2, Output{Stderr: "package database locked"}); hint != nil {
		t.Errorf("exit code 2 with a lock message matched %q", hint.Name)
	}
	if hint := Lookup(2, Output{Stderr: "failed to open repodata"}); hint == nil {
		t.Error("exit code 2 with a stale-index message matched nothing")
	}
}

// TestShortSubstrings pins the patterns to short substrings, never full lines.
func TestShortSubstrings(t *testing.T) {
	for _, h := range Table {
		if len(h.Contains) > 60 {
			t.Errorf("hint %q substring is %d characters; keep patterns short", h.Name, len(h.Contains))
		}
		if strings.Contains(h.Contains, "\n") {
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

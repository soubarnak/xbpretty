package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const logFixture = `2026-10-01T08:00:00Z install curl-8.10.1_1 libcurl-8.10.1_1
2026-10-02T08:00:00Z upgrade figlet-2.2.5_6 libcurl-8.11.0_1
2026-10-03T08:00:00Z update
2026-10-04T08:00:00Z remove curl-8.11.0_1
2026-10-05T08:00:00Z install figlet-2.2.5_7
`

// writeLogFixture puts content at the transaction log path the test's
// XDG_STATE_HOME resolves to.
func (e *env) writeLogFixture(content string) string {
	e.t.Helper()
	dir := filepath.Join(e.dir, "state", "xbpretty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	path := filepath.Join(dir, "transactions.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return path
}

func logLines(lines ...int) string {
	all := strings.SplitAfter(logFixture, "\n")
	var b strings.Builder
	for _, i := range lines {
		b.WriteString(all[i])
	}
	return b.String()
}

func TestLogPrintsEveryLineVerbatimOldestFirst(t *testing.T) {
	e := newEnv(t)
	e.writeLogFixture(logFixture)
	code, out, errs := e.run("xbp", "log")
	if code != 0 || out != logFixture || errs != "" {
		t.Fatalf("code=%d out=%q err=%q, want 0 and the fixture verbatim", code, out, errs)
	}
	if calls := e.commands(); len(calls) != 0 {
		t.Errorf("xbp log ran xbps: %v", calls)
	}
}

func TestLogFilterMatchesExactNames(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"curl is not libcurl", []string{"curl"}, logLines(0, 3)},
		{"libcurl alone", []string{"libcurl"}, logLines(0, 1)},
		{"several names match any", []string{"curl", "figlet"}, logLines(0, 1, 3, 4)},
		{"update never matches", []string{"update"}, ""},
		{"a prefix is not a name", []string{"fig"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.writeLogFixture(logFixture)
			code, out, errs := e.run(append([]string{"xbp", "log"}, tt.args...)...)
			if code != 0 || out != tt.want {
				t.Fatalf("code=%d out=%q want %q err=%q", code, out, tt.want, errs)
			}
		})
	}
}

func TestLogLimit(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"alone", []string{"--limit", "2"}, logLines(3, 4)},
		{"equals form", []string{"--limit=1"}, logLines(4)},
		{"after the package", []string{"curl", "--limit", "1"}, logLines(3)},
		{"with a filter", []string{"--limit", "2", "figlet"}, logLines(1, 4)},
		{"larger than the log", []string{"--limit", "99"}, logFixture},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.writeLogFixture(logFixture)
			code, out, errs := e.run(append([]string{"xbp", "log"}, tt.args...)...)
			if code != 0 || out != tt.want {
				t.Fatalf("code=%d out=%q want %q err=%q", code, out, tt.want, errs)
			}
		})
	}
}

func TestLogUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--limit", "0"}, {"--limit", "-3"}, {"--limit", "x"}, {"--limit"}, {"--limit="},
		{"--dry-run"}, {"-n"}, {"--yes"}, {"-y"}, {"--no-sync"}, {"--", "curl"}, {"curl", "--"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			e := newEnv(t)
			e.writeLogFixture(logFixture)
			code, out, errs := e.run(append([]string{"xbp", "log"}, args...)...)
			if code != 2 || out != "" || errs == "" {
				t.Fatalf("code=%d out=%q err=%q, want exit 2, empty stdout, a message", code, out, errs)
			}
			if calls := e.commands(); len(calls) != 0 {
				t.Errorf("xbps ran: %v", calls)
			}
		})
	}
}

func TestLimitIsOnlyForLog(t *testing.T) {
	e := newEnv(t)
	code, _, _ := e.run("xbp", "install", "--limit", "2", "figlet")
	if code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
}

func TestLogMissingOrEmptySaysNothingLogged(t *testing.T) {
	for _, tt := range []struct {
		name  string
		write bool
	}{{"missing", false}, {"empty", true}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.write {
				e.writeLogFixture("")
			}
			code, out, errs := e.run("xbp", "log")
			if code != 0 || out != "" {
				t.Fatalf("code=%d out=%q", code, out)
			}
			if strings.Count(errs, "\n") != 1 || !strings.Contains(errs, "nothing has been logged yet") {
				t.Errorf("stderr = %q, want one line saying nothing has been logged yet", errs)
			}
			if _, err := os.Stat(filepath.Join(e.dir, "state")); !tt.write && err == nil {
				t.Error("xbp log created the state directory")
			}
		})
	}
}

func TestLogUnreadableIsAnError(t *testing.T) {
	e := newEnv(t)
	path := e.writeLogFixture(logFixture)
	// A directory at the log path cannot be read, whoever runs the test.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errs := e.run("xbp", "log")
	if code != 1 || out != "" || errs == "" {
		t.Fatalf("code=%d out=%q err=%q, want exit 1 and a message on stderr", code, out, errs)
	}
}

func TestLogNeedsNoRootAndLeavesTheLogAlone(t *testing.T) {
	e := newEnv(t)
	path := e.writeLogFixture(logFixture)
	e.root = false
	code, _, _ := e.run("xbp", "log", "curl")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	got, _ := os.ReadFile(path)
	if string(got) != logFixture {
		t.Errorf("log changed:\n%s", got)
	}
}

// Under sudo or doas the state home is the invoking user's, which the config
// module resolves (see TestAC012LogFollowsTheInvokersStateHome).
func TestLogReadsTheInvokersStateHome(t *testing.T) {
	e := newEnv(t)
	e.root = true
	home := t.TempDir()
	e.set("XDG_STATE_HOME", "")
	e.set("HOME", home)
	dir := filepath.Join(home, ".local", "state", "xbpretty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transactions.log"), []byte(logFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := e.run("xbp", "log")
	if code != 0 || out != logFixture {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestLogHasNoLinkName(t *testing.T) {
	e := newEnv(t)
	code, _, _ := e.run("xbp-log")
	if code == 0 {
		t.Error("xbp-log was accepted as a command")
	}
}

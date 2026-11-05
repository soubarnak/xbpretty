package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// summaryGoldenPlan is the plan fixture the golden frames were captured from.
// It carries the same cell contents as the prototype's live 80/100 frames so
// the built binary can be diffed against them byte-for-byte.
const summaryGoldenPlan = `nano-9.2_1 remove x86_64 https://repo-default.voidlinux.org/current 3145728 0
figlet-2.2.5_6 install x86_64 https://repo-default.voidlinux.org/current 0 94208
cowsay-3.8.4_1 install x86_64 https://repo-default.voidlinux.org/current 0 19456
voiced-1.3.0_1 update x86_64 https://repo-default.voidlinux.org/current 12705792 12582912
acl-2.4.0_1 hold x86_64 https://repo-default.voidlinux.org/current 20480 20480
`

// writeSummaryGoldenQuery writes an xbps-query stub that answers the two old
// version lookups the fixture needs, and an empty held list.
func writeSummaryGoldenQuery(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "query")
	script := `#!/bin/sh
case "$*" in
  *"-H"*) exit 0 ;;
  *installed_size*)
    case "$*" in
      *voiced*) printf 'voiced-1.2.0_1\n11657216B\n'; exit 0 ;;
      *acl*) printf 'acl-2.4.0_1\n20480B\n'; exit 0 ;;
      *) exit 2 ;;
    esac
    ;;
esac
exit 2
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func summaryGoldenConfig(t *testing.T, dir, query string) (cfgDir, logPath string) {
	return summaryGoldenConfigExtra(t, dir, query)
}

func summaryGoldenConfigExtra(t *testing.T, dir, query string, extra ...string) (cfgDir, logPath string) {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir = filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"[Xbps]",
		`install_path = "` + filepath.Join(stubDir, "xbps-install") + `"`,
		`remove_path = "` + filepath.Join(stubDir, "xbps-remove") + `"`,
		`query_path = "` + query + `"`,
		"",
		"[Color]",
		`mode = "Always"`,
		"",
	}, "\n")
	body += strings.Join(extra, "\n")
	if len(extra) > 0 && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "xbpretty", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgDir, filepath.Join(dir, "commands.log")
}

func readSummaryGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "summary", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return string(raw)
}

// TestSummaryGoldensThroughTheBuiltBinary is AC-044's subprocess seam: the
// real binary, its real config and its real stdout path produce the captured
// rev 6 frames byte-for-byte. COLUMNS supplies the rule width on a pipe while
// the tables keep natural widths.
func TestSummaryGoldensThroughTheBuiltBinary(t *testing.T) {
	bin := buildBinary(t)
	cases := []struct {
		name   string
		args   []string
		column string
		golden string
	}{
		{"piped 80", []string{"install", "--dry-run", "fixture"}, "80", "summary-80.piped.sgr"},
		{"piped 100", []string{"install", "--dry-run", "fixture"}, "100", "summary-100.piped.sgr"},
		{"simple", []string{"install", "--dry-run", "fixture", "--simple"}, "80", "summary-simple.sgr"},
		{"no colour", []string{"install", "--dry-run", "fixture", "--color=never"}, "80", "summary-80.piped.txt"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			query := writeSummaryGoldenQuery(t, dir)
			cfgDir, logPath := summaryGoldenConfig(t, dir, query)
			cmd := exec.Command(bin, tt.args...)
			cmd.Env = append(os.Environ(),
				"XDG_CONFIG_HOME="+cfgDir,
				"XDG_STATE_HOME="+filepath.Join(dir, "state"),
				"XB_STUB_LOG="+logPath,
				"XB_STUB_STDOUT="+summaryGoldenPlan,
				"XB_STUB_STDERR=",
				"XB_STUB_HOLD=",
				"XB_STUB_OLD=",
				"COLUMNS="+tt.column,
				"NO_COLOR=",
			)
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("run: %v\n%s", err, stderr.String())
			}
			if got, want := stdout.String(), readSummaryGolden(t, tt.golden); got != want {
				t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", tt.golden, got, want)
			}
		})
	}
}

// TestCustomThemeOverrideBytes drives [Color.theme] through the built binary
// and pins the exact SGR bytes: a secondary role keeps bold but changes fg,
// while an indexed Notice with no modifier emits 0;38;5;202.
func TestCustomThemeOverrideBytes(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	query := writeSummaryGoldenQuery(t, dir)
	cfgDir, logPath := summaryGoldenConfigExtra(t, dir, query,
		"[Color.theme]",
		`Secondary = { fg = "Red" }`,
		`Notice = { fg = 202, modifier = "" }`,
	)
	cmd := exec.Command(bin, "install", "--dry-run", "fixture")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+cfgDir,
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
		"XB_STUB_LOG="+logPath,
		"XB_STUB_STDOUT="+summaryGoldenPlan,
		"XB_STUB_STDERR=",
		"XB_STUB_HOLD=",
		"XB_STUB_OLD=",
		"COLUMNS=80",
		"NO_COLOR=",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"\x1b[1;31mfiglet\x1b[0m",
		"\x1b[0;38;5;202macl\x1b[0m",
		"1.\x1b[0;38;5;202m3\x1b[0m.\x1b[0;38;5;202m0_1\x1b[0m",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("custom theme output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[1;94m") {
		t.Errorf("the default Secondary prefix leaked through a custom theme:\n%s", out)
	}
}

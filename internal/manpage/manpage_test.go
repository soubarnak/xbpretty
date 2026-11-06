package manpage

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/config"
	"github.com/soubarnak/xbpretty/internal/spec"
)

// TestGeneratedManpagesAreCurrent keeps man/, which the xbps-src template and
// tools/build-xbps.sh install, in step with the generator. A cross build cannot
// run the target binary, so the pages are committed; regenerate them with
// `make manpages` (AC-059).
func TestGeneratedManpagesAreCurrent(t *testing.T) {
	for _, file := range Files() {
		got, err := os.ReadFile(filepath.Join("..", "..", file.Name))
		if err != nil {
			t.Fatalf("read shipped %s: %v (run `make manpages`)", file.Name, err)
		}
		if string(got) != file.Content {
			t.Errorf("manpages are stale: run make manpages (%s)", file.Name)
		}
	}
}

func TestAliasMan8LinkSetIsExact(t *testing.T) {
	got := append([]string(nil), AliasLinks()...)
	want := []string{"xbp.8", "xbp-install.8", "xbp-remove.8", "xbp-upgrade.8", "xbp-autoremove.8", "xbp-update.8"}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("AliasLinks() = %v, want exactly %v", got, want)
	}
}

func TestAliasMan8LinksResolve(t *testing.T) {
	for _, link := range AliasLinks() {
		path := filepath.Join("..", "..", "man", link)
		info, err := os.Lstat(path)
		if err != nil {
			t.Errorf("alias link %s: %v (run `make manpages`)", link, err)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink", link)
			continue
		}
		if target, err := os.Readlink(path); err != nil || target != "xbpretty.8" {
			t.Errorf("%s points to %q, %v; want xbpretty.8", link, target, err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s does not resolve: %v", link, err)
		}
	}
}

func TestMan8CoversTheCommandSurface(t *testing.T) {
	text := Man8()
	for _, command := range spec.Commands {
		if !strings.Contains(text, ".It Cm "+command.Name) {
			t.Errorf("xbpretty.8 is missing the command item for %q", command.Name)
		}
		for _, alias := range command.Aliases {
			if !strings.Contains(text, alias) {
				t.Errorf("xbpretty.8 is missing command alias %q", alias)
			}
		}
	}
	for _, name := range spec.LinkNames() {
		if !strings.Contains(text, name) {
			t.Errorf("xbpretty.8 is missing link name %q", name)
		}
	}
	for _, flag := range spec.Flags {
		for _, spelling := range spec.FlagSpellings(flag) {
			if !strings.Contains(text, spelling) {
				t.Errorf("xbpretty.8 is missing flag %q", spelling)
			}
		}
	}
	for _, retired := range []string{"--box-style", "--progress-bar", "--no-progress", "--no-progress-bar"} {
		if strings.Contains(text, retired) {
			t.Errorf("xbpretty.8 still documents the retired flag %q", retired)
		}
	}
	for _, want := range []string{
		"auto", "always", "never", "bash", "zsh", "fish",
		"standard output", "standard error", "standard input",
		"keystrokes", "inherited by xbps children",
		"after list or search", "--virtual",
		"EXIT STATUS", "FILES", "SECURITY", "SEE ALSO",
		"UID 0", "trust", "history", "--print-uris", "--machine",
		".It Cm --auto Op Ar N",
		"/var/lib/xbpretty/fetch.json",
		".xbpretty-backup",
		"/usr/share/xbps.d",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("xbpretty.8 is missing %q", want)
		}
	}
}

func section(text, heading string) string {
	marker := ".Ss " + heading + "\n"
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	rest := text[start+len(marker):]
	for _, end := range []string{"\n.Ss ", "\n.Sh "} {
		if i := strings.Index(rest, end); i >= 0 {
			rest = rest[:i]
		}
	}
	return rest
}

func TestConf5CoversConfig(t *testing.T) {
	text := Conf5()
	manifest := config.Manifest()
	for _, table := range manifest.Tables {
		body := section(text, table.Name)
		if body == "" {
			t.Errorf("xbpretty.conf.5 is missing the %s section", table.Name)
			continue
		}
		if table.Name == "Color.theme" {
			for _, role := range manifest.ThemeRoles {
				if !strings.Contains(body, ".It Cm "+role.Name) {
					t.Errorf("[Color.theme] section is missing role %q", role.Name)
				}
			}
			continue
		}
		for _, key := range table.Keys {
			if !strings.Contains(body, ".It Cm "+key.Name) {
				t.Errorf("[%s] section is missing key %q", table.Name, key.Name)
			}
		}
		for _, alias := range table.Aliases {
			if !strings.Contains(text, "["+alias+"]") {
				t.Errorf("xbpretty.conf.5 does not document the [%s] alias", alias)
			}
		}
	}
	legacy := text[strings.Index(text, ".Sh LEGACY KEYS"):]
	for _, key := range manifest.Legacy {
		if !strings.Contains(legacy, ".It Cm "+key.Key) {
			t.Errorf("LEGACY KEYS is missing %q", key.Key)
		}
		if key.Target != "" && !strings.Contains(legacy, key.Target) {
			t.Errorf("LEGACY KEYS for %q is missing target %q", key.Key, key.Target)
		}
	}
	for _, want := range []string{
		"system layer", "user layer", "--config", "trust", "UID 0",
		"group", "world", "rev 7",
		"#rrggbb", "{ Indexed = N }", "{ Rgb = [r, g, b] }",
		"BrightGreen", "BrightRed",
		"EXIT STATUS", "SECURITY", "SEE ALSO",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("xbpretty.conf.5 is missing %q", want)
		}
	}
	if strings.Contains(text, "rgb(") {
		t.Error("xbpretty.conf.5 documents rgb(...), which the parser rejects")
	}
}

func TestRoffTextLinesAreEscaped(t *testing.T) {
	tests := map[string]string{
		".nm injected": `\&.nm injected`,
		"'br injected": `\&'br injected`,
		`a\b`:          `a\eb`,
		"plain text":   "plain text",
	}
	for in, want := range tests {
		if got := escapeRoffLine(in); got != want {
			t.Errorf("escapeRoffLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestManpagesPassMandocLint is AC-059's lint gate. mandoc is a check-time
// dependency only; environments without it skip the test.
func TestManpagesPassMandocLint(t *testing.T) {
	if _, err := exec.LookPath("mandoc"); err != nil {
		t.Skip("mandoc is not installed")
	}
	dir := t.TempDir()
	var paths []string
	for _, file := range Files() {
		path := filepath.Join(dir, filepath.Base(file.Name))
		if err := os.WriteFile(path, []byte(file.Content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	out, err := exec.Command("mandoc", append([]string{"-T", "lint"}, paths...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("mandoc -T lint: %v\n%s", err, out)
	}
	if len(out) != 0 {
		t.Fatalf("mandoc -T lint was not silent:\n%s", out)
	}
}

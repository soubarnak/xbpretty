package manpage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
		if !strings.Contains(text, command.Name) {
			t.Errorf("xbpretty.8 is missing command %q", command.Name)
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
	for _, want := range []string{
		"auto", "always", "never", "bash", "zsh", "fish",
		"standard output", "standard error", "standard input",
		"EXIT STATUS", "FILES", "SECURITY", "SEE ALSO",
		"UID 0", "trust", "history", "--print-uris", "--machine",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("xbpretty.8 is missing %q", want)
		}
	}
}

func TestConf5CoversConfig(t *testing.T) {
	text := Conf5()
	for _, want := range []string{
		"[Nala]", "[Ui]", "[Color]", "[Color.theme]", "[Xbps]",
		"auto_remove", "auto_update", "update_show_packages", "full_upgrade",
		"simple", "assume_yes", "mode", "unit", "Auto", "Plain", "Tui",
		"Binary", "Decimal", "Always", "Never", "Primary", "Secondary",
		"Highlight", "Regular", "ProgressFilled", "ProgressUnfilled",
		"Notice", "Warning", "Error", "install_path", "remove_path",
		"query_path", "rindex_path", "uhelper_path", "pkgdb_path",
		"transaction_log", "system layer", "user layer", "--config",
		"trust", "UID 0", "group", "world", "assume_yes",
		"xbps_install_path", "xbps_query_path", "color", "sync", "log",
		"verbose", "box_style", "progress_bar", "rev 7",
		"EXIT STATUS", "SECURITY", "SEE ALSO",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("xbpretty.conf.5 is missing %q", want)
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

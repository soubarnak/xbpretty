package cli

import "testing"

// TestBF40RepositoryDescriptionDropsRepositorySuffix pins BF-40: the
// repository-mode long_desc pass strips xbps's trailing `(URL)` and keeps the
// description's own parentheses.
func TestBF40RepositoryDescriptionDropsRepositorySuffix(t *testing.T) {
	stdout := "" +
		"alpha-2.0_1: Tool with optional (plugins) support (https://repo-de.voidlinux.org/current)\n" +
		"beta-1.0_1: Plain description (https://repo-de.voidlinux.org/current/multilib)\n"
	got := parseDescriptionProperty(stdout, true)
	if got["alpha-2.0_1"] != "Tool with optional (plugins) support" {
		t.Errorf("alpha long_desc = %q", got["alpha-2.0_1"])
	}
	if got["beta-1.0_1"] != "Plain description" {
		t.Errorf("beta long_desc = %q", got["beta-1.0_1"])
	}
}

// TestBF40InstalledDescriptionKeepsURLTail confirms the installed-mode pass
// does not strip a trailing URL: that pass has no repository suffix to drop.
func TestBF40InstalledDescriptionKeepsURLTail(t *testing.T) {
	got := parseDescriptionProperty("tool-1.0_1: Homepage example (https://example.test/tool)\n", false)
	if got["tool-1.0_1"] != "Homepage example (https://example.test/tool)" {
		t.Errorf("installed long_desc = %q, want the URL tail kept", got["tool-1.0_1"])
	}
}

// TestBF40RepositoryDescriptionWithoutSuffixIsKept covers a bare repository
// value, which has no `(URL)` to strip.
func TestBF40RepositoryDescriptionWithoutSuffixIsKept(t *testing.T) {
	got := parseDescriptionProperty("gamma-1.0_1: Bare text\n", true)
	if got["gamma-1.0_1"] != "Bare text" {
		t.Errorf("bare repository long_desc = %q", got["gamma-1.0_1"])
	}
}

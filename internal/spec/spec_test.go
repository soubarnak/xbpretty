package spec

import (
	"strings"
	"testing"
)

func TestCommandTableIsCanonicalAndClosed(t *testing.T) {
	want := []string{
		"install", "remove", "purge", "autoremove", "autopurge",
		"upgrade", "full-upgrade", "safe-upgrade", "update", "download",
		"clean", "list", "search", "show", "policy", "history", "undo",
		"redo", "fetch", "completion", "link", "log", "moo",
	}
	if len(Commands) != len(want) {
		t.Fatalf("Commands has %d entries, want %d", len(Commands), len(want))
	}
	for i, name := range want {
		if Commands[i].Name != name {
			t.Errorf("Commands[%d] = %q, want %q", i, Commands[i].Name, name)
		}
	}

	aliases := map[string]string{
		"uninstall":    "remove",
		"info":         "show",
		"dist-upgrade": "full-upgrade",
		"up":           "upgrade",
		"dist-up":      "upgrade",
	}
	for alias, canonical := range aliases {
		got, ok := ResolveCommand(alias)
		if !ok || got != canonical {
			t.Errorf("ResolveCommand(%q) = %q, %v; want %q, true", alias, got, ok, canonical)
		}
	}

	for _, name := range []string{"clear", "help", "xbp-history", "xbp-fetch", "xbp-undo", "xbp-redo", "xbp-clean", "xbp-log", "purge-all"} {
		if got, ok := ResolveCommand(name); ok {
			t.Errorf("ResolveCommand(%q) = %q, true; want no command", name, got)
		}
	}
}

func TestSixLinkNames(t *testing.T) {
	want := []string{"xbp", "xbp-install", "xbp-remove", "xbp-upgrade", "xbp-autoremove", "xbp-update"}
	got := LinkNames()
	if len(got) != len(want) {
		t.Fatalf("LinkNames() = %v, want %d names", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("LinkNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHistorySubcommands(t *testing.T) {
	tests := []struct {
		in       []string
		wantCmd  string
		wantSub  string
		wantRest []string
	}{
		{[]string{"undo"}, "undo", "", nil},
		{[]string{"redo", "5"}, "redo", "", []string{"5"}},
		{[]string{"clear"}, "history", "clear", nil},
		{[]string{"clear", "last"}, "history", "clear", []string{"last"}},
		{[]string{"info", "3"}, "history", "info", []string{"3"}},
		{[]string{"show", "last"}, "history", "show", []string{"last"}},
		{[]string{"3"}, "history", "", []string{"3"}},
		{[]string{"last"}, "history", "", []string{"last"}},
		{nil, "history", "", nil},
	}
	for _, tt := range tests {
		cmd, sub, rest := NormalizeHistory("history", tt.in)
		if cmd != tt.wantCmd || sub != tt.wantSub || !equalStrings(rest, tt.wantRest) {
			t.Errorf("NormalizeHistory(%v) = %q, %q, %v; want %q, %q, %v",
				tt.in, cmd, sub, rest, tt.wantCmd, tt.wantSub, tt.wantRest)
		}
	}
	// A top-level verb is never rewritten.
	if cmd, sub, rest := NormalizeHistory("undo", []string{"clear"}); cmd != "undo" || sub != "" || !equalStrings(rest, []string{"clear"}) {
		t.Errorf("NormalizeHistory on undo rewrote: %q %q %v", cmd, sub, rest)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFlagsFollowTheirCommands(t *testing.T) {
	transaction := []string{"install", "remove", "purge", "autoremove", "autopurge", "upgrade", "full-upgrade", "safe-upgrade"}
	for _, cmd := range transaction {
		for _, name := range []string{"fix-broken", "allow-unauthenticated", "remove-essential", "install-recommends", "install-suggests", "option", "target-release", "exclude", "remove-config"} {
			if !hasFlag(FlagsFor(cmd), name) {
				t.Errorf("%s does not accept the N/A flag %q", cmd, name)
			}
		}
		for _, name := range []string{"no-fix-broken", "no-install-recommends", "no-install-suggests"} {
			if !hasFlag(FlagsFor(cmd), name) {
				t.Errorf("%s does not accept the no-op flag %q", cmd, name)
			}
		}
	}
	if hasFlag(FlagsFor("list"), "fix-broken") {
		t.Error("list accepts fix-broken, which belongs to the transaction commands")
	}
	if hasFlag(FlagsFor("fetch"), "yes") {
		t.Error("fetch accepts --yes, which FR-046 forbids")
	}
	if !hasFlag(FlagsFor("fetch"), "dry-run") {
		t.Error("fetch does not accept its --dry-run extension")
	}
	if !hasFlag(FlagsFor("fetch"), "country") {
		t.Error("fetch does not accept --country")
	}
	if hasFlag(FlagsFor("list"), "country") {
		t.Error("list accepts fetch's --country")
	}
	if !hasFlag(FlagsFor("search"), "names") {
		t.Error("search does not accept --names")
	}
	if !hasFlag(FlagsFor("log"), "limit") {
		t.Error("log does not accept --limit")
	}
	if hasFlag(FlagsFor("install"), "limit") {
		t.Error("install accepts log's --limit")
	}
	for _, name := range []string{"virtual", "version"} {
		if !hasFlag(FlagsFor("list"), name) {
			t.Errorf("list does not accept %q", name)
		}
	}
}

func TestEveryReferencedFlagExistsAndNoDuplicates(t *testing.T) {
	seen := map[string]string{}
	for _, f := range Flags {
		for _, key := range append([]string{f.Long}, f.Aliases...) {
			if other, ok := seen[key]; ok {
				t.Errorf("flag %q is declared twice (%s and %s)", key, other, f.Long)
			}
			seen[key] = f.Long
		}
	}
	for _, cmd := range Commands {
		seenFlag := map[string]bool{}
		for _, name := range cmd.Flags {
			if seenFlag[name] {
				t.Errorf("%s lists flag %q twice", cmd.Name, name)
			}
			seenFlag[name] = true
			if _, ok := LookupFlag(name); !ok {
				t.Errorf("%s references unknown flag %q", cmd.Name, name)
			}
		}
	}
}

func TestRefusalTableIsExact(t *testing.T) {
	want := map[string]string{
		"allow-unauthenticated": "--allow-unauthenticated is not available on xbps: xbps verifies repository signatures and has no unauthenticated mode.",
		"option":                "-o/--option is not available on xbps: apt configuration options have no xbps equivalent; repository settings live in /etc/xbps.d/*.conf.",
		"fix-broken":            "-f/--fix-broken is not available on xbps: xbps has no broken-dependency repair mode. Check the pkgdb with xbps-pkgdb --checks dependencies, or force a reinstall with xbp install -- -f PKG.",
		"target-release":        "-t/--target-release is not available on xbps: xbps has no default-release targeting; pin a repository with a repository= line in /etc/xbps.d.",
		"install-recommends":    "--install-recommends is not available on xbps: xbps has no recommends metadata; every declared run_depends dependency is installed.",
		"install-suggests":      "--install-suggests is not available on xbps: xbps has no suggests metadata.",
		"remove-essential":      "--remove-essential is not available on xbps: xbps has no essential-package protection to override.",
		"remove-config":         "--remove-config is not available on xbps: xbps has no config-files state; xbps-remove -f removes files, it does not create a config-only removal.",
		"exclude":               "--exclude is not available on xbps: xbps has no upgrade exclusion; hold the package with xbps-pkgdb -m hold PKG (release with -m unhold).",
		"sources":               "--sources is not available: Void repositories contain no source packages",
		"debian":                "--debian is not available: xbpretty targets Void Linux only",
		"ubuntu":                "--ubuntu is not available: xbpretty targets Void Linux only",
		"devuan":                "--devuan is not available: xbpretty targets Void Linux only",
	}
	for name, tail := range want {
		f, ok := LookupFlag(name)
		if !ok {
			t.Errorf("missing refusal flag %q", name)
			continue
		}
		if f.Refusal != tail {
			t.Errorf("%s refusal = %q, want %q", name, f.Refusal, tail)
		}
		if !hasFlag(FlagsFor("fetch"), name) && name != "sources" {
			// sources is checked through fetch like the other fetch refusals
		}
	}
}

func TestNoOpNegativesAreAccepted(t *testing.T) {
	for _, name := range []string{"no-fix-broken", "no-install-recommends", "no-install-suggests"} {
		f, ok := LookupFlag(name)
		if !ok {
			t.Fatalf("missing no-op flag %q", name)
		}
		if !f.NoOp || f.Refusal != "" {
			t.Errorf("%s NoOp=%v Refusal=%q, want accepted silently", name, f.NoOp, f.Refusal)
		}
	}
}

func TestUsageIsGeneratedAndHidesMoo(t *testing.T) {
	text := Usage()
	for _, cmd := range VisibleCommands() {
		if !strings.Contains(text, cmd.Name) {
			t.Errorf("usage does not list %q", cmd.Name)
		}
	}
	if strings.Contains(text, "moo") {
		t.Error("usage advertises the hidden moo command")
	}
	for _, want := range []string{"--help", "--version", "--color", "--config", "--tui", "--no-tui"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage does not mention %q", want)
		}
	}
}

func hasFlag(flags []Flag, name string) bool {
	for _, f := range flags {
		if f.Long == name {
			return true
		}
	}
	return false
}

func TestDryRunIsLongOnlyAndAssumeNoTakesTheShortN(t *testing.T) {
	dry, ok := LookupFlag("dry-run")
	if !ok {
		t.Fatal("dry-run is missing")
	}
	if dry.Short != 0 {
		t.Errorf("dry-run has short -%c; rev 6 makes the dry run long-only", dry.Short)
	}
	assume, ok := LookupFlag("assume-no")
	if !ok {
		t.Fatal("assume-no is missing")
	}
	if assume.Short != 'n' || assume.Global {
		t.Errorf("assume-no = %+v; want short n on the transaction commands", assume)
	}
	if hasFlag(FlagsFor("fetch"), "assume-no") {
		t.Error("fetch accepts --assume-no")
	}
}

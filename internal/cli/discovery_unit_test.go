package cli

import (
	"testing"
)

func TestRepositorySuffixAcceptsSchemesAndLocalPaths(t *testing.T) {
	for _, repo := range []string{
		"https://repo.example/current",
		"http://repo.example/current",
		"file:///tmp/repo",
		"ftp://repo.example/current",
		"/tmp/repo",
		"./repo",
		"../repo",
	} {
		value, got := splitRepositorySuffix("x86_64 (" + repo + ")")
		if value != "x86_64" || got != repo {
			t.Errorf("splitRepositorySuffix(%q) = %q, %q", repo, value, got)
		}
	}
}

func TestLongDescriptionKeepsURLTails(t *testing.T) {
	desc := "A tool that ends with (https://example.test/tool)"
	got := parseDescriptionProperty("tool-1.0_1: " + desc + "\n")
	if got["tool-1.0_1"] != desc {
		t.Errorf("long_desc = %q, want the literal description %q", got["tool-1.0_1"], desc)
	}
}

func TestParseRecordAlternativesAndLists(t *testing.T) {
	raw := "alternatives:\n  sh:\n  \tsh:/usr/bin/bash\n  \tsh.1:/usr/share/man/man1/bash.1\n" +
		"provides:\n\tcmd:bash-5.3_2\n\tcmd:sh-0_1\n"
	rec := parseRecord(raw)
	if len(rec.alternativeOrder) != 1 || rec.alternativeOrder[0] != "sh" {
		t.Fatalf("alternative order = %v", rec.alternativeOrder)
	}
	if got := rec.alternatives["sh"]; len(got) != 2 || got[0] != "sh:/usr/bin/bash" {
		t.Fatalf("alternatives[sh] = %v", got)
	}
	if got := rec.list["provides"]; len(got) != 2 || got[1] != "cmd:sh-0_1" {
		t.Fatalf("provides = %v", got)
	}
	if _, ok := rec.properties[" sh"]; ok {
		t.Errorf("bogus key parsed from alternatives group")
	}
	if _, ok := rec.properties["alternatives"].(map[string][]string); !ok {
		t.Errorf("properties[alternatives] = %T", rec.properties["alternatives"])
	}
}

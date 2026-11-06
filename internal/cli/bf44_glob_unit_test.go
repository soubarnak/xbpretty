package cli

import (
	"errors"
	"path"
	"testing"
)

func TestBF44SplitGlobArchKnownArchesOnly(t *testing.T) {
	cases := []struct {
		token, name, arch string
		ok                bool
	}{
		{"beta:x86_64", "beta", "x86_64", true},
		{"beta:x86_64-musl", "beta", "x86_64-musl", true},
		{"beta:noarch", "beta", "noarch", true},
		{"beta:armv7l-musl", "beta", "armv7l-musl", true},
		{"beta:ppc64le", "beta", "ppc64le", true},
		{"beta:mipsel", "beta", "mipsel", true},
		{"cmdpack:x86_64", "cmdpack", "x86_64", true},
		{"pc:zlib-ng", "pc:zlib-ng", "", false},
		{"font:*", "font:*", "", false},
		{"cmd:thing", "cmd:thing", "", false},
		{"shlib:libz.so.1", "shlib:libz.so.1", "", false},
		{"cmd:x86_64", "cmd:x86_64", "", false},
		{"beta:", "beta:", "", false},
		{":x86_64", ":x86_64", "", false},
		{"plain", "plain", "", false},
	}
	for _, tc := range cases {
		name, arch, ok := splitGlobArch(tc.token)
		if name != tc.name || arch != tc.arch || ok != tc.ok {
			t.Errorf("splitGlobArch(%q) = %q, %q, %v; want %q, %q, %v",
				tc.token, name, arch, ok, tc.name, tc.arch, tc.ok)
		}
	}
}

func TestBF44PathGlobNegatesWithCaret(t *testing.T) {
	if got := pathGlob("[!A]*"); got != "[^a]*" {
		t.Fatalf("pathGlob = %q, want [^a]*", got)
	}
	for name, want := range map[string]bool{"alpha": false, "beta": true, "!bang": true} {
		ok, err := path.Match(pathGlob("[!a]*"), name)
		if err != nil {
			t.Fatalf("path.Match on negated class: %v", err)
		}
		if ok != want {
			t.Errorf("[!a]* vs %q = %v, want %v", name, ok, want)
		}
	}
}

func TestBF44ExpandBracesUnbalancedIsTyped(t *testing.T) {
	for _, pattern := range []string{"a{b", "{a,b", "{a{b}", "x{y,z"} {
		if _, err := expandBraces(pattern); !errors.Is(err, errUnbalancedBrace) {
			t.Errorf("expandBraces(%q) error = %v, want errUnbalancedBrace", pattern, err)
		}
	}
	got, err := expandBraces("a}b")
	if err != nil || len(got) != 1 || got[0] != "a}b" {
		t.Errorf("stray close brace = %v, %v; want the literal", got, err)
	}
	got, err = expandBraces("{a,b}c")
	if err != nil || len(got) != 2 || got[0] != "ac" || got[1] != "bc" {
		t.Errorf("balanced braces = %v, %v", got, err)
	}
}

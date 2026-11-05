package discovery

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitVersionAndPkgver(t *testing.T) {
	name, version := SplitPkgverVersion("base-files-0.146_2")
	if name != "base-files" || version != "0.146_2" {
		t.Fatalf("split pkgver = %q, %q", name, version)
	}
	upstream, revision := SplitRevision("0.146_2")
	if upstream != "0.146" || revision != "2" {
		t.Fatalf("split revision = %q, %q", upstream, revision)
	}
	upstream, revision = SplitRevision("1.0")
	if upstream != "1.0" || revision != "" {
		t.Fatalf("revisionless split = %q, %q", upstream, revision)
	}
}

func TestRowLineAttributesAndRepository(t *testing.T) {
	row := Row{
		Name: "alpha", Pkgver: "alpha-2.0_1", Version: "2.0", Revision: "1",
		Repository: "https://repo.example/current", Installed: true, Automatic: true,
		Held: true, Repolocked: true, AutoRemovable: true, Unpacked: true, HalfRemoved: true,
		UpgradableFrom: "1.0_1",
	}
	want := "alpha (2.0_1) [repo.example/current] [Installed, Unpacked, Half-removed, Automatic, Auto-Removable, Held, Repolocked, Upgradable from: 1.0_1]"
	if got := row.Line(); got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
	row.Repository = ""
	if !strings.Contains(row.Line(), "[local]") {
		t.Errorf("local row = %q", row.Line())
	}

	virtual := Row{Name: "cmd:thing", Virtual: true, Providers: []string{"z", "a"}}
	if got := virtual.Line(); got != "cmd:thing (Virtual) [a, z]" {
		t.Errorf("virtual line = %q", got)
	}
}

func TestMachinePackageKeepsFullRepositoryAndOmitsLocal(t *testing.T) {
	row := Row{
		Name: "alpha", Pkgver: "alpha-2.0_1", Version: "2.0", Revision: "1",
		Architecture: "x86_64", Repository: "https://repo.example/current",
		Installed: true, InstalledVersion: "1.0_1", Automatic: true,
	}
	machine := row.Machine()
	if machine.Repository != "https://repo.example/current" {
		t.Errorf("repository = %q, want full URL", machine.Repository)
	}
	if machine.Schema != 1 || machine.Kind != "package" {
		t.Errorf("identity = %d/%q", machine.Schema, machine.Kind)
	}

	row.Repository = ""
	raw, err := json.Marshal(row.Machine())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"repository"`)) {
		t.Errorf("local machine row carries repository: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"installed_size":null`)) || !bytes.Contains(raw, []byte(`"download_size":null`)) {
		t.Errorf("machine sizes are not null: %s", raw)
	}
}

func TestWriteNDJSONIsCompactAndNotHTMLEscaped(t *testing.T) {
	var buf bytes.Buffer
	row := Row{Name: "a<b", Pkgver: "a<b-1.0_1", Version: "1.0", Revision: "1"}
	if err := WriteNDJSON(&buf, row.Machine()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
		t.Errorf("NDJSON is not one line: %q", out)
	}
	if strings.Contains(out, "a\\u003cb") || !strings.Contains(out, "a<b") {
		t.Errorf("NDJSON HTML-escaped a literal: %q", out)
	}
	if !strings.Contains(out, `"schema":1`) {
		t.Errorf("NDJSON lacks schema 1: %q", out)
	}
}

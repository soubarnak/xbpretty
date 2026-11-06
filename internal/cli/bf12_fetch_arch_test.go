package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// bf12WriteMuslConfig replaces the fixture's repository configuration with a
// musl repository and no architecture= line, so the arch must be detected.
func bf12WriteMuslConfig(t *testing.T, xbpsd string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(xbpsd, "00-repository-main.conf"),
		[]byte("repository=https://repo-default.voidlinux.org/current/musl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// bf12FakeUhelper puts a fake xbps-uhelper first on PATH, so the default
// uhelper_path resolves to it. It answers `arch` with arch and exits with code.
func bf12FakeUhelper(t *testing.T, dir, arch string, code int) {
	t.Helper()
	bin := filepath.Join(dir, "uhelper-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"[ \"$1\" = arch ] || exit 2\n" +
		"printf '%s\\n' '" + arch + "'\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "xbps-uhelper"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestBF12FetchArchFromUhelper: with no architecture= line and no XBPS_ARCH,
// fetch asks xbps-uhelper arch, so a musl system probes its musl repodata.
func TestBF12FetchArchFromUhelper(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	fetchArch = ""
	bf12WriteMuslConfig(t, xbpsd)
	t.Setenv("XBPS_ARCH", "")
	bf12FakeUhelper(t, e.dir, "x86_64-musl", 0)

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != "/one/current/musl/x86_64-musl-repodata" {
		t.Errorf("probe paths = %v, want /one/current/musl/x86_64-musl-repodata", probes)
	}
}

// TestBF12FetchArchFromEnv: XBPS_ARCH outranks xbps-uhelper arch, which here
// would answer x86_64-musl.
func TestBF12FetchArchFromEnv(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	fetchArch = ""
	bf12WriteMuslConfig(t, xbpsd)
	bf12FakeUhelper(t, e.dir, "x86_64-musl", 0)
	e.set("XBPS_ARCH", "aarch64-musl")

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != "/one/current/musl/aarch64-musl-repodata" {
		t.Errorf("probe paths = %v, want /one/current/musl/aarch64-musl-repodata", probes)
	}
}

// TestBF12FetchArchHookOutranksEnvironment: the fetchArch test hook still wins
// over XBPS_ARCH and xbps-uhelper arch.
func TestBF12FetchArchHookOutranksEnvironment(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	bf12WriteMuslConfig(t, xbpsd)
	bf12FakeUhelper(t, e.dir, "x86_64-musl", 0)
	e.set("XBPS_ARCH", "aarch64-musl")

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != "/one/current/musl/x86_64-repodata" {
		t.Errorf("probe paths = %v, want /one/current/musl/x86_64-repodata", probes)
	}
}

// TestBF12FetchArchFallsBackToRuntimeWhenUhelperFails: a failing xbps-uhelper
// and an unset XBPS_ARCH leave the runtime mapping as the last resort.
func TestBF12FetchArchFallsBackToRuntimeWhenUhelperFails(t *testing.T) {
	fixture := newFetchFixture(t, []fetchMirror{{name: "one", region: "EU", location: "", tier: 1}})
	e, xbpsd := newFetchEnv(t, fixture)
	fetchArch = ""
	bf12WriteMuslConfig(t, xbpsd)
	t.Setenv("XBPS_ARCH", "")
	bf12FakeUhelper(t, e.dir, "", 1)

	code, out, errs := e.run("xbpretty", "fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", code, out, errs)
	}
	want := "/one/current/musl/" + runtimeXBPSArch() + "-repodata"
	probes := fixture.probePaths()
	if len(probes) != 1 || probes[0] != want {
		t.Errorf("probe paths = %v, want %s", probes, want)
	}
}

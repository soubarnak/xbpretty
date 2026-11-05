package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upgradePlanFixture is a captured-shape `xbps-install -n -u` plan with two
// downloadable updates.
const upgradePlanFixture = "figlet-2.2.5_6 update x86_64 https://repo-default.voidlinux.org/current 672593 94372\n" +
	"wget-1.25.0_1 update x86_64 https://repo-default.voidlinux.org/current 3673038 824658\n"

// TestPrintURIsEmitsOneObjectPerPlannedDownload pins FR-028/AC-023: exact
// fields, non-root, no sync, no summary, prompt or download, and no child
// other than the plan and the bulk hash pass.
func TestPrintURIsEmitsOneObjectPerPlannedDownload(t *testing.T) {
	e := newEnv(t)
	e.root = false
	e.tty = false
	e.set("XB_STUB_STDOUT", upgradePlanFixture)
	e.set("XB_STUB_HASHES", strings.Join([]string{
		"figlet-2.2.5_6: 94372aabbccddeeff00112233445566778899aabbccddeeff0011223344556677 (https://repo-default.voidlinux.org/current)",
		"wget-1.25.0_1: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef (https://repo-default.voidlinux.org/current)",
	}, "\n"))

	code, out, errs := e.run("xbpretty", "upgrade", "--print-uris")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, errs)
	}
	if errs != "" {
		t.Errorf("stderr = %q, want nothing (no sync warning, no summary)", errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout has %d lines, want 2:\n%s", len(lines), out)
	}
	want := []map[string]any{
		{
			"schema":        float64(1),
			"package":       "figlet",
			"pkgver":        "figlet-2.2.5_6",
			"repository":    "https://repo-default.voidlinux.org/current",
			"url":           "https://repo-default.voidlinux.org/current/figlet-2.2.5_6.x86_64.xbps",
			"signature_url": "https://repo-default.voidlinux.org/current/figlet-2.2.5_6.x86_64.xbps.sig2",
			"size":          float64(94372),
			"sha256":        "94372aabbccddeeff00112233445566778899aabbccddeeff0011223344556677",
		},
		{
			"schema":        float64(1),
			"package":       "wget",
			"pkgver":        "wget-1.25.0_1",
			"repository":    "https://repo-default.voidlinux.org/current",
			"url":           "https://repo-default.voidlinux.org/current/wget-1.25.0_1.x86_64.xbps",
			"signature_url": "https://repo-default.voidlinux.org/current/wget-1.25.0_1.x86_64.xbps.sig2",
			"size":          float64(824658),
			"sha256":        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}
	for i, line := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%s", i, err, line)
		}
		if len(got) != len(want[i]) {
			t.Errorf("line %d has %d fields, want %d: %v", i, len(got), len(want[i]), got)
		}
		for key, value := range want[i] {
			if got[key] != value {
				t.Errorf("line %d %s = %v, want %v", i, key, got[key], value)
			}
		}
	}

	for _, line := range e.commandsWithBinary() {
		binary, args := splitStubLine(line)
		if strings.HasPrefix(args, "-S") || strings.HasPrefix(args, "-y") {
			t.Errorf("--print-uris ran a sync or transaction: %s %q", binary, args)
		}
		if args == "-n -u" {
			continue
		}
		if strings.HasPrefix(args, "-R -s  -p filename-sha256") {
			continue
		}
		t.Errorf("--print-uris ran an unexpected child: %s %q", binary, args)
	}
	logPath := filepath.Join(e.dir, "state", "xbpretty", "transactions.log")
	if _, err := os.Stat(logPath); err == nil {
		t.Error("--print-uris wrote a transaction log line")
	}
}

// TestPrintURIsHashUnavailableIsNull pins FR-028's null sha256.
func TestPrintURIsHashUnavailableIsNull(t *testing.T) {
	e := newEnv(t)
	e.root = false
	e.set("XB_STUB_STDOUT", "figlet-2.2.5_6 update x86_64 https://repo-default.voidlinux.org/current 672593 94372\n")
	e.set("XB_STUB_HASHES", "")
	code, out, errs := e.run("xbpretty", "upgrade", "--print-uris")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, errs)
	}
	if !strings.Contains(out, `"sha256":null`) {
		t.Errorf("stdout = %q, want a null sha256", out)
	}
}

// TestPrintURIsEmptyPlanPrintsNothing pins AC-023's empty plan.
func TestPrintURIsEmptyPlanPrintsNothing(t *testing.T) {
	e := newEnv(t)
	e.root = false
	e.set("XB_STUB_STDOUT", "")
	e.set("XB_STUB_HASHES", "")
	code, out, errs := e.run("xbpretty", "upgrade", "--print-uris")
	if code != 0 {
		t.Errorf("exit = %d, want 0\n%s", code, errs)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
}

// TestPrintURIsStopsAtTheUpgradeFamily pins FR-028's command scope.
func TestPrintURIsStopsAtTheUpgradeFamily(t *testing.T) {
	e := newEnv(t)
	for _, argv := range [][]string{
		{"install", "figlet", "--print-uris"},
		{"remove", "figlet", "--print-uris"},
		{"download", "figlet", "--print-uris"},
		{"list", "--print-uris"},
	} {
		if code, _, _ := e.run(append([]string{"xbpretty"}, argv...)...); code != 2 {
			t.Errorf("%v exit = %d, want 2", argv, code)
		}
	}
}

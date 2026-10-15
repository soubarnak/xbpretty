package xbps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir is the repo-root capture corpus, shared with the hint and summary tests.
const fixtureDir = "../../testdata/parser/"

func parseFixture(t *testing.T, path string) []Row {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening fixture %s: %v", path, err)
	}
	defer f.Close()
	rows, err := ParsePlan(f)
	if err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	return rows
}

func histogram(rows []Row) map[string]int {
	counts := make(map[string]int)
	for _, row := range rows {
		counts[row.Action]++
	}
	return counts
}

func TestParsePlanFixtures(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantRows  int
		wantHist  map[string]int
		first     Row
		firstFull bool
	}{
		{
			name:     "install plan 0.60.7",
			fixture:  fixtureDir + "0607/install.txt",
			wantRows: 1,
			wantHist: map[string]int{ActionInstall: 1},
			first: Row{
				Name: "figlet", Version: "2.2.5_6", Action: ActionInstall,
				Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
				InstalledSize: 672593, HasInstalled: true,
				DownloadSize: 94372, HasDownload: true,
			},
			firstFull: true,
		},
		{
			name:     "upgrade plan 0.60.7",
			fixture:  fixtureDir + "0607/upgrade.txt",
			wantRows: 56,
			wantHist: map[string]int{ActionInstall: 5, ActionUpdate: 51},
			first: Row{
				Name: "acl", Version: "2.4.0_1", Action: ActionUpdate,
				Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
				InstalledSize: 43080, HasInstalled: true,
				DownloadSize: 20653, HasDownload: true,
			},
			firstFull: true,
		},
		{
			name:     "remove plan 0.60.7",
			fixture:  fixtureDir + "0607/remove.txt",
			wantRows: 1,
			wantHist: map[string]int{ActionRemove: 1},
			first: Row{
				Name: "wget", Version: "1.25.0_1", Action: ActionRemove,
				Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
				InstalledSize: 3673038, HasInstalled: true,
				DownloadSize: 824658, HasDownload: true,
			},
			firstFull: true,
		},
		{
			name:     "autoremove plan 0.60.7",
			fixture:  fixtureDir + "0607/autoremove.txt",
			wantRows: 5,
			wantHist: map[string]int{ActionRemove: 5},
			first: Row{
				Name: "cups", Version: "2.4.19_1", Action: ActionRemove,
				Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
				InstalledSize: 13764009, HasInstalled: true,
				DownloadSize: 5564250, HasDownload: true,
			},
			firstFull: true,
		},
		{
			name:     "nothing-to-do plan 0.60.7 is empty",
			fixture:  fixtureDir + "0607/nothing.txt",
			wantRows: 0,
			wantHist: map[string]int{},
		},
		{
			name:     "reinstall plan reports the reinstall verb",
			fixture:  fixtureDir + "0607/reinstall.txt",
			wantRows: 1,
			wantHist: map[string]int{ActionReinstall: 1},
			first: Row{
				Name: "curl", Version: "8.22.0_1", Action: ActionReinstall,
				Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
				InstalledSize: 682921, HasInstalled: true,
				DownloadSize: 274590, HasDownload: true,
			},
			firstFull: true,
		},
		{
			name:     "upgrade plan 0.59.1",
			fixture:  fixtureDir + "0591/upgrade.txt",
			wantRows: 8,
			wantHist: map[string]int{ActionInstall: 4, ActionUpdate: 4},
			first: Row{
				Name: "libxcrypt-compat", Version: "4.4.38_1", Action: ActionInstall,
				Arch: "x86_64", Repo: "https://repo-default.voidlinux.org/current",
				InstalledSize: 206920, HasInstalled: true,
				DownloadSize: 89590, HasDownload: true,
			},
			firstFull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := parseFixture(t, tt.fixture)
			if len(rows) != tt.wantRows {
				t.Fatalf("got %d rows, want %d", len(rows), tt.wantRows)
			}
			got := histogram(rows)
			for action, want := range tt.wantHist {
				if got[action] != want {
					t.Errorf("action %q: got %d rows, want %d", action, got[action], want)
				}
			}
			if len(got) != len(tt.wantHist) {
				t.Errorf("got actions %v, want %v", got, tt.wantHist)
			}
			if tt.firstFull && rows[0] != tt.first {
				t.Errorf("first row:\n got %+v\nwant %+v", rows[0], tt.first)
			}
		})
	}
}

func TestParsePlanSingleSizeColumn(t *testing.T) {
	rows := parseFixture(t, fixtureDir+"0591/upgrade.txt")
	var found bool
	for _, row := range rows {
		if row.Name != "base-container" {
			continue
		}
		found = true
		if !row.HasInstalled || row.InstalledSize != 683 {
			t.Errorf("base-container installed size: got %d (has=%v), want 683", row.InstalledSize, row.HasInstalled)
		}
		if row.HasDownload {
			t.Errorf("base-container has download size %d, want none", row.DownloadSize)
		}
	}
	if !found {
		t.Fatal("base-container row not found in 0.59.1 upgrade fixture")
	}
}

func TestParsePlanZeroInstalledSize(t *testing.T) {
	rows := parseFixture(t, fixtureDir+"0607/upgrade.txt")
	var found bool
	for _, row := range rows {
		if row.Name != "removed-packages" {
			continue
		}
		found = true
		if !row.HasInstalled || row.InstalledSize != 0 {
			t.Errorf("removed-packages installed size: got %d (has=%v), want 0", row.InstalledSize, row.HasInstalled)
		}
		if !row.HasDownload || row.DownloadSize != 9644 {
			t.Errorf("removed-packages download size: got %d (has=%v), want 9644", row.DownloadSize, row.HasDownload)
		}
	}
	if !found {
		t.Fatal("removed-packages row not found in upgrade fixture")
	}
}

func TestParsePlanHyphenatedNames(t *testing.T) {
	rows := parseFixture(t, fixtureDir+"0607/upgrade.txt")
	want := map[string]string{
		"base-files":        "0.146_1",
		"libxcrypt":         "4.4.38_1",
		"removed-packages":  "0.1.20260823_1",
		"ca-certificates":   "20250419+3.127_1",
		"eudev-libudev":     "3.2.14_2",
		"util-linux-common": "2.41.4_1",
	}
	versions := make(map[string]string)
	for _, row := range rows {
		versions[row.Name] = row.Version
		if row.Pkgver() != row.Name+"-"+row.Version {
			t.Errorf("Pkgver() = %q, want %q", row.Pkgver(), row.Name+"-"+row.Version)
		}
	}
	for name, wantVersion := range want {
		if versions[name] != wantVersion {
			t.Errorf("%s version: got %q, want %q", name, versions[name], wantVersion)
		}
	}
}

func TestParsePlanMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"too few tokens", "figlet-2.2.5_6 install x86_64\n"},
		{"package without version", "figlet install x86_64 repo 100 200\n"},
		{"trailing hyphen", "figlet- install x86_64 repo 100 200\n"},
		{"non numeric size", "figlet-2.2.5_6 install x86_64 repo 100 20x\n"},
		{"too many tokens", "figlet-2.2.5_6 install x86_64 repo 100 200 300\n"},
		{"empty column", "figlet-2.2.5_6 install x86_64 repo 100 20 0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParsePlan(strings.NewReader(tt.in)); err == nil {
				t.Fatalf("ParsePlan(%q) succeeded, want error", tt.in)
			}
		})
	}
}

func TestParsePlanErrorNamesLineNumber(t *testing.T) {
	in := "figlet-2.2.5_6 install x86_64 repo 100 200\n\nnot a plan line\n"
	_, err := ParsePlan(strings.NewReader(in))
	if err == nil {
		t.Fatal("ParsePlan succeeded, want error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error %q does not name line 3", err)
	}
}

func TestParsePlanBlankLinesSkipped(t *testing.T) {
	in := "\nfiglet-2.2.5_6 install x86_64 repo 100 200\n\n\n"
	rows, err := ParsePlan(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
}

func TestParsePlanEmptyInput(t *testing.T) {
	rows, err := ParsePlan(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestRowIsKnownAction(t *testing.T) {
	for _, action := range []string{ActionInstall, ActionUpdate, ActionRemove, ActionConfigure, ActionHold, ActionReinstall} {
		if !(Row{Action: action}).IsKnownAction() {
			t.Errorf("action %q reported unknown", action)
		}
	}
	for _, action := range []string{"", "downgrade", "Install"} {
		if (Row{Action: action}).IsKnownAction() {
			t.Errorf("action %q reported known", action)
		}
	}
}

// TestParsePlanCapturedHoldAndConfigure covers the two actions a plan carries
// only for state xbps had to be put in: a package put on hold, and one left
// configurable by an interrupted transaction.
func TestParsePlanCapturedHoldAndConfigure(t *testing.T) {
	tests := []struct {
		fixture string
		action  string
	}{
		{"0607/hold.txt", ActionHold},
		{"0607/configure.txt", ActionConfigure},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			rows := parseFixture(t, fixtureDir+tt.fixture)
			var found int
			for _, row := range rows {
				if !row.IsKnownAction() {
					t.Errorf("%s has action %q, which the parser does not know", row.Name, row.Action)
				}
				if row.Action != tt.action {
					continue
				}
				found++
				if row.Name == "" || row.Version == "" || row.Arch == "" || row.Repo == "" {
					t.Errorf("%s row lost a column: %+v", tt.action, row)
				}
				if !row.HasInstalled || !row.HasDownload {
					t.Errorf("%s row lost its sizes: %+v", tt.action, row)
				}
			}
			if found == 0 {
				t.Fatalf("no %s row in %s", tt.action, tt.fixture)
			}
		})
	}
}

func TestFixtureDirLayout(t *testing.T) {
	matches, err := filepath.Glob(fixtureDir + "*/*.txt")
	if err != nil {
		t.Fatalf("globbing fixtures: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("got %d fixture files, want at least two xbps versions", len(matches))
	}
}

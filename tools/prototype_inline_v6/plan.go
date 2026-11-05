package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/soubarnak/xbpretty/internal/xbps"
)

const mib = 1024 * 1024
const kib = 1024

// goldenPlan is #45 §3.7's fixture, converted to xbps package versions. The
// cell contents match the golden frame so the prototype's table engine can be
// compared byte-for-byte with nala's frame; the footer is computed from the
// rows (#45 §3.5), which the research frame's own footer is not.
func goldenPlan() Plan {
	return Plan{Command: "install", Rows: []PlanRow{
		{Name: "oldpkg", Version: "1.4.2_1", Action: actionRemove,
			InstalledSize: 3 * mib, HasInstalled: true},
		{Name: "hello", Version: "2.10_3", Action: actionInstall,
			DownloadSize: 54 * kib, HasDownload: true,
			InstalledSize: 70 * kib, HasInstalled: true},
		{Name: "libssl3t64", Version: "3.5.4_1", OldVersion: "3.5.1_1", Action: actionUpdate,
			DownloadSize: 2 * mib, HasDownload: true,
			InstalledSize: 2 * mib, HasInstalled: true,
			OldInstalledSize: 2 * mib, HasOldInstalled: true},
		{Name: "nala", Version: "0.18.2_1", OldVersion: "0.18.1_1", Action: actionUpdate,
			DownloadSize: 1 * mib, HasDownload: true,
			InstalledSize: 1 * mib, HasInstalled: true,
			OldInstalledSize: 1 * mib, HasOldInstalled: true},
		{Name: "vim-runtime", Version: "9.1.1300_1", OldVersion: "9.1.1234_1", Action: actionUpdate,
			DownloadSize: 8 * mib, HasDownload: true,
			InstalledSize: 8 * mib, HasInstalled: true,
			OldInstalledSize: 8 * mib, HasOldInstalled: true},
		{Name: "linux-image-6.1.0-40-amd64", Version: "6.1.146_1", OldVersion: "6.1.140_1",
			Action: actionHold, Reason: "Manual hold",
			DownloadSize: 41 * mib, HasDownload: true,
			InstalledSize: 41 * mib, HasInstalled: true,
			OldInstalledSize: 41 * mib, HasOldInstalled: true},
	}}
}

// nalaPlan is research #39 §3.3's frame verbatim — Debian version strings,
// "Kept back" and apt's 12 MiB/18 MiB footer — so the renderer can be diffed
// against nala's golden frame byte-for-byte. It is not an xbps plan; it only
// proves "same cell contents, same bytes" (#45 §3.7).
func nalaPlan() Plan {
	foot := totals{Download: 12 * mib, HasDownload: true, Delta: 18 * mib}
	return Plan{Command: "install", Foot: &foot, Rows: []PlanRow{
		{Name: "oldpkg", Version: "1.4.2-1", Action: actionRemove,
			InstalledSize: 3 * mib, HasInstalled: true,
			DownloadSize: 3 * mib, HasDownload: true},
		{Name: "hello", Version: "2.10-3", Action: actionInstall,
			InstalledSize: 54 * kib, HasInstalled: true,
			DownloadSize: 54 * kib, HasDownload: true},
		{Name: "libssl3t64", Version: "3.5.4-1", OldVersion: "3.5.1-1", Action: actionUpdate,
			InstalledSize: 2 * mib, HasInstalled: true, DownloadSize: 2 * mib, HasDownload: true},
		{Name: "nala", Version: "0.18.2-1", OldVersion: "0.18.1-1", Action: actionUpdate,
			InstalledSize: mib, HasInstalled: true, DownloadSize: mib, HasDownload: true},
		{Name: "vim-runtime", Version: "9.1.1300-1", OldVersion: "9.1.1234-1", Action: actionUpdate,
			InstalledSize: 8 * mib, HasInstalled: true, DownloadSize: 8 * mib, HasDownload: true},
		{Name: "linux-image-6.1.0-40-amd64", Version: "6.1.146-1", OldVersion: "6.1.140-1",
			Action: actionHold, Reason: "Kept back",
			InstalledSize: 41 * mib, HasInstalled: true, DownloadSize: 41 * mib, HasDownload: true},
	}}
}

// runPlan executes one read-only xbps dry run and parses its stdout with the
// real internal/xbps parser.
func runPlan(bin string, args ...string) ([]xbps.Row, error) {
	cmd := exec.Command(bin, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
	}
	return xbps.ParsePlan(strings.NewReader(string(out)))
}

// queryOld reads `xbps-query -p pkgver,installed_size <pkg>`: the pkgver, then
// xbps's humanised installed size (base 1024).
func queryOld(name string) (version string, size uint64, ok bool) {
	out, err := exec.Command("xbps-query", "-p", "pkgver,installed_size", name).Output()
	if err != nil {
		return "", 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return "", 0, false
	}
	_, version = xbps.SplitPkgver(strings.TrimSpace(lines[0]))
	if len(lines) > 1 {
		if n, err := parseHumanSize(strings.TrimSpace(lines[1])); err == nil {
			size = n
		}
	}
	return version, size, version != ""
}

// parseHumanSize turns xbps's humanised size ("22MB", "521KB") into bytes.
func parseHumanSize(s string) (uint64, error) {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("not a size: %q", s)
	}
	n, err := strconv.ParseUint(s[:i], 10, 64)
	if err != nil {
		return 0, err
	}
	unit := strings.ToUpper(strings.TrimSpace(s[i:]))
	switch unit {
	case "", "B":
		return n, nil
	case "KB":
		return n * kib, nil
	case "MB":
		return n * mib, nil
	case "GB":
		return n * 1024 * mib, nil
	case "TB":
		return n * 1024 * 1024 * mib, nil
	}
	return 0, fmt.Errorf("unknown unit in %q", s)
}

// fixtureHoldRows reads the captured hold plan and returns its `hold` rows.
func fixtureHoldRows(testdata string, limit int) []PlanRow {
	f, err := os.Open(testdata + "/parser/0607/hold.txt")
	if err != nil {
		return nil
	}
	defer f.Close()
	rows, err := xbps.ParsePlan(f)
	if err != nil {
		return nil
	}
	var out []PlanRow
	for _, r := range rows {
		if r.Action != xbps.ActionHold {
			continue
		}
		out = append(out, PlanRow{
			Name: r.Name, Version: r.Version, Action: actionHold,
			Reason:        "Manual hold",
			InstalledSize: r.InstalledSize, HasInstalled: r.HasInstalled,
			DownloadSize: r.DownloadSize, HasDownload: r.HasDownload,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// livePlan builds a real, read-only plan from this host's xbps:
//   - xbps-install -n figlet cowsay                  (install rows)
//   - xbps-install -n -u                             (upgrade rows, plus old
//     version/size from xbps-query)
//   - xbps-remove -n nano                            (remove rows)
//   - testdata/parser/0607/hold.txt                  (held rows; the host has
//     no holds of its own)
//
// It never installs, removes or syncs anything.
func livePlan(limit int, testdata string) (Plan, error) {
	p := Plan{Command: "install"}

	installRows, err := runPlan("xbps-install", "-n", "figlet", "cowsay")
	if err != nil {
		return p, err
	}
	for i, r := range installRows {
		if i >= limit {
			break
		}
		p.Rows = append(p.Rows, PlanRow{
			Name: r.Name, Version: r.Version, Action: actionInstall,
			InstalledSize: r.InstalledSize, HasInstalled: r.HasInstalled,
			DownloadSize: r.DownloadSize, HasDownload: r.HasDownload,
		})
	}

	upgradeRows, err := runPlan("xbps-install", "-n", "-u")
	if err != nil {
		return p, err
	}
	for i, r := range upgradeRows {
		if i >= limit {
			break
		}
		row := PlanRow{
			Name: r.Name, Version: r.Version, Action: actionUpdate,
			InstalledSize: r.InstalledSize, HasInstalled: r.HasInstalled,
			DownloadSize: r.DownloadSize, HasDownload: r.HasDownload,
		}
		if v, size, ok := queryOld(r.Name); ok {
			row.OldVersion = v
			row.OldInstalledSize, row.HasOldInstalled = size, size > 0
		}
		p.Rows = append(p.Rows, row)
	}

	removeRows, err := runPlan("xbps-remove", "-n", "nano")
	if err != nil {
		return p, err
	}
	for i, r := range removeRows {
		if i >= limit {
			break
		}
		p.Rows = append(p.Rows, PlanRow{
			Name: r.Name, Version: r.Version, Action: actionRemove,
			InstalledSize: r.InstalledSize, HasInstalled: r.HasInstalled,
			DownloadSize: r.DownloadSize, HasDownload: r.HasDownload,
		})
	}

	for _, row := range fixtureHoldRows(testdata, limit) {
		if v, size, ok := queryOld(row.Name); ok {
			row.OldVersion = v
			row.OldInstalledSize, row.HasOldInstalled = size, size > 0
		}
		p.Rows = append(p.Rows, row)
	}
	return p, nil
}

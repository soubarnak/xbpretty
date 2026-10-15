package xbps

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Action verbs xbps uses in dry-run transaction plans.
const (
	ActionInstall   = "install"
	ActionUpdate    = "update"
	ActionRemove    = "remove"
	ActionConfigure = "configure"
	ActionHold      = "hold"
	ActionReinstall = "reinstall"
)

// Row is one package line of an xbps dry-run transaction plan.
type Row struct {
	Name          string
	Version       string
	Action        string
	Arch          string
	Repo          string
	InstalledSize uint64
	DownloadSize  uint64
	HasInstalled  bool
	HasDownload   bool
}

// Pkgver returns the package name and version joined as xbps prints them.
func (r Row) Pkgver() string {
	return r.Name + "-" + r.Version
}

// IsKnownAction reports whether the row's action is one xbps is known to emit.
// A held package is in the plan as `hold` and a package left configurable as
// `configure` (testdata/parser/0607/hold.txt, configure.txt).
func (r Row) IsKnownAction() bool {
	switch r.Action {
	case ActionInstall, ActionUpdate, ActionRemove, ActionConfigure, ActionHold, ActionReinstall:
		return true
	}
	return false
}

// ParsePlan reads dry-run output of xbps-install or xbps-remove and returns
// one Row per package line. Blank lines are skipped. Any other unparseable
// line is an error naming the line number, never a silently dropped row.
func ParsePlan(r io.Reader) ([]Row, error) {
	var rows []Row
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || len(fields) > 6 {
			return nil, fmt.Errorf("plan line %d: cannot parse %q", lineNo, line)
		}
		row, err := parseRow(fields)
		if err != nil {
			return nil, fmt.Errorf("plan line %d: %v", lineNo, err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading plan: %w", err)
	}
	return rows, nil
}

func parseRow(fields []string) (Row, error) {
	name, version := SplitPkgver(fields[0])
	if name == "" || version == "" {
		return Row{}, fmt.Errorf("cannot read package name and version from %q", fields[0])
	}
	row := Row{
		Name:    name,
		Version: version,
		Action:  fields[1],
		Arch:    fields[2],
		Repo:    fields[3],
	}
	sizes := fields[4:]
	if len(sizes) > 2 {
		return Row{}, fmt.Errorf("unexpected columns after %q", fields[3])
	}
	for i, size := range sizes {
		n, ok := parseSize(size)
		if !ok {
			return Row{}, fmt.Errorf("%q is not a byte count", size)
		}
		switch {
		case i == 0 && len(sizes) == 2:
			row.InstalledSize, row.HasInstalled = n, true
		case i == 0 && len(sizes) == 1:
			row.InstalledSize, row.HasInstalled = n, true
		case i == 1:
			row.DownloadSize, row.HasDownload = n, true
		}
	}
	return row, nil
}

// SplitPkgver splits a pkgver such as `xbps-0.60.7_1` into the package name
// and the bare version, the form the summary shows an upgrade in (FR-011).
// Both are empty when the field holds no version.
func SplitPkgver(field string) (name, version string) {
	cut := strings.LastIndex(field, "-")
	if cut <= 0 || cut == len(field)-1 {
		return "", ""
	}
	return field[:cut], field[cut+1:]
}

func parseSize(field string) (uint64, bool) {
	if field == "" {
		return 0, false
	}
	var n uint64
	for _, c := range field {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}

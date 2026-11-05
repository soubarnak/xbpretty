package main

import (
	"strings"
)

// Plan-level action verbs, as internal/xbps spells them.
const (
	actionInstall   = "install"
	actionUpdate    = "update"
	actionRemove    = "remove"
	actionConfigure = "configure"
	actionHold      = "hold"
	actionReinstall = "reinstall"
)

// PlanRow is one package line of an xbps dry-run plan plus the old-version
// lookup a summary needs (#45 §3.3).
type PlanRow struct {
	Name          string
	Version       string
	OldVersion    string
	Action        string
	Reason        string
	InstalledSize uint64
	DownloadSize  uint64
	HasInstalled  bool
	HasDownload   bool

	OldInstalledSize uint64
	HasOldInstalled  bool
}

// Plan is one transaction's rows and the command that produced them.
type Plan struct {
	Command string
	Rows    []PlanRow
	// Foot overrides the computed footer. It exists only so the prototype can
	// reproduce nala's research frame, whose footer comes from apt's depcache
	// rather than from the Size column (research #39 §3.9); live plans leave it
	// nil and compute #45 §3.5's figures.
	Foot *totals
}

// Section is one rendered operation block.
type Section struct {
	Label string
	Op    string
	Rows  []PlanRow
}

// sectionOrder is nala's fixed operation order (#45 §3.3).
var sectionOrder = []string{
	"Remove", "AutoRemove", "Purge", "AutoPurge", "Install",
	"ReInstall", "Upgrade", "Downgrade", "Configure", "Held",
}

// labelFor maps an xbps action (plus the command, for the remove family) to a
// nala section label (#45 §3.3 and D14).
func labelFor(command, action string) (label, op string, ok bool) {
	switch action {
	case actionRemove:
		switch command {
		case "purge":
			return "Purge", "remove", true
		case "autopurge":
			return "AutoPurge", "remove", true
		case "autoremove":
			return "AutoRemove", "remove", true
		default:
			return "Remove", "remove", true
		}
	case actionInstall:
		return "Install", "install", true
	case actionReinstall:
		return "ReInstall", "reinstall", true
	case actionUpdate:
		return "Upgrade", "upgrade", true
	case actionConfigure:
		return "Configure", "configure", true
	case actionHold:
		return "Held", "held", true
	}
	return "", "", false
}

// buildSections groups rows into nala's fixed operation order, keeping only
// non-empty sections. xbps plans are homogeneous per action.
func buildSections(p Plan) []Section {
	byLabel := map[string]*Section{}
	for _, row := range p.Rows {
		label, op, ok := labelFor(p.Command, row.Action)
		if !ok {
			continue
		}
		s := byLabel[label]
		if s == nil {
			s = &Section{Label: label, Op: op}
			byLabel[label] = s
		}
		s.Rows = append(s.Rows, row)
	}
	var out []Section
	for _, label := range sectionOrder {
		if s, ok := byLabel[label]; ok && len(s.Rows) > 0 {
			out = append(out, *s)
		}
	}
	return out
}

// diffVersion reports whether the section shows Old Version / New Version
// columns (#45 §3.3: nala's display_old_version is "both exist and differ",
// except that Held rows with a candidate always carry both).
func hasVersionDiff(s Section) bool {
	for _, row := range s.Rows {
		if row.OldVersion != "" && row.OldVersion != row.Version {
			return true
		}
	}
	return false
}

// sectionSize applies #45 §3.3's "what Size: means" table.
func sectionSize(op string, row PlanRow) (uint64, bool) {
	switch op {
	case "remove", "configure":
		return row.InstalledSize, row.HasInstalled
	default: // install, reinstall, upgrade, held: candidate/download size
		if row.HasDownload {
			return row.DownloadSize, true
		}
		return row.InstalledSize, row.HasInstalled
	}
}

// rowCells produces one row's cells in the section's column shape.
func rowCells(s Section, row PlanRow, sys UnitSystem, th Theme) []string {
	size, hasSize := sectionSize(s.Op, row)
	sizeText := summarySize(size, sys)
	if !hasSize {
		sizeText = "—"
	}
	nameRole := RoleSecondary
	switch s.Op {
	case "remove":
		nameRole = RoleError
	case "held", "reinstall", "configure":
		nameRole = RoleNotice
	}
	name := th.Paint(nameRole, row.Name)
	version := row.Version
	if version == "" {
		version = "Unknown"
	}
	switch {
	case hasVersionDiff(s) && row.Reason != "":
		return []string{name, row.OldVersion, versionDiff(row.OldVersion, row.Version, th), row.Reason, sizeText}
	case hasVersionDiff(s):
		return []string{name, row.OldVersion, versionDiff(row.OldVersion, row.Version, th), sizeText}
	case row.Reason != "":
		return []string{name, version, row.Reason, sizeText}
	default:
		return []string{name, version, sizeText}
	}
}

// headers for a section's column shape (#45 §3.3).
func sectionHeaders(s Section) []string {
	switch {
	case hasVersionDiff(s) && s.Rows[0].Reason != "":
		return []string{"Package:", "Old Version:", "New Version:", "Reason:", "Size:"}
	case hasVersionDiff(s):
		return []string{"Package:", "Old Version:", "New Version:", "Size:"}
	case s.Rows[0].Reason != "":
		return []string{"Package:", "Version:", "Reason:", "Size:"}
	default:
		return []string{"Package:", "Version:", "Size:"}
	}
}

// totals are the footer figures (#45 §3.5).
type totals struct {
	Download     uint64
	HasDownload  bool
	NewInstalled uint64
	OldInstalled uint64
	Removed      uint64
	Delta        int64
}

func planTotals(p Plan, sections []Section) totals {
	if p.Foot != nil {
		return *p.Foot
	}
	var t totals
	for _, s := range sections {
		for _, row := range s.Rows {
			switch s.Op {
			case "install", "reinstall", "upgrade":
				if row.HasDownload && row.DownloadSize > 0 {
					t.Download += row.DownloadSize
					t.HasDownload = true
				}
				if row.HasInstalled {
					t.NewInstalled += row.InstalledSize
				}
				if s.Op == "upgrade" && row.HasOldInstalled {
					t.OldInstalled += row.OldInstalledSize
				}
			case "remove":
				if row.HasInstalled {
					t.Removed += row.InstalledSize
				}
			}
		}
	}
	t.Delta = int64(t.NewInstalled) - int64(t.OldInstalled) - int64(t.Removed)
	return t
}

// renderSummary renders the full inline summary block. width is the rule
// width (terminal columns, even when piped — #45 §3.1); fullWidth controls
// only the tables (piped stdout gets natural widths).
func renderSummary(p Plan, width int, fullWidth bool, sys UnitSystem, th Theme) []string {
	sections := buildSections(p)
	if len(sections) == 0 {
		return []string{"Nothing to do."}
	}
	rule := repeat("=", width)
	var lines []string
	for _, s := range sections {
		lines = append(lines, rule)
		lines = append(lines, " "+th.Paint(RoleHighlight, s.Label))
		lines = append(lines, rule)
		t := Table{Header: sectionHeaders(s)}
		for _, row := range s.Rows {
			t.Rows = append(t.Rows, rowCells(s, row, sys, th))
		}
		lines = append(lines, t.render(width, fullWidth, th)...)
	}
	lines = append(lines, rule)
	lines = append(lines, " Summary") // plain, no role (#45 §3.1 correction C1)
	lines = append(lines, rule)
	for _, s := range sections {
		lines = append(lines, " "+s.Label+" "+itoa(len(s.Rows)))
	}
	lines = append(lines, "")
	t := planTotals(p, sections)
	if t.HasDownload && t.Download > 0 {
		lines = append(lines, " Total download size: "+summarySize(t.Download, sys))
	}
	if t.Delta >= 0 {
		lines = append(lines, " Disk space required: "+summarySize(uint64(t.Delta), sys))
	} else {
		lines = append(lines, " Disk space to free: "+summarySize(uint64(-t.Delta), sys))
	}
	lines = append(lines, "")
	return lines
}

// renderSimple renders nala's --simple layout (#45 §4): no rules, no table, no
// version columns; Highlight label + ": " + count, then two spaces and the
// comma-joined names.
func renderSimple(p Plan, sys UnitSystem, th Theme) []string {
	sections := buildSections(p)
	if len(sections) == 0 {
		return []string{"Nothing to do."}
	}
	var lines []string
	for _, s := range sections {
		lines = append(lines, th.Paint(RoleHighlight, s.Label)+": "+itoa(len(s.Rows)))
		names := make([]string, 0, len(s.Rows))
		for _, row := range s.Rows {
			name := row.Name
			if row.Reason != "" {
				name += " (" + row.Reason + ")"
			}
			names = append(names, name)
		}
		lines = append(lines, "  "+strings.Join(names, ", "))
	}
	lines = append(lines, "")
	t := planTotals(p, sections)
	if t.HasDownload && t.Download > 0 {
		lines = append(lines, " Total download size: "+summarySize(t.Download, sys))
	}
	if t.Delta >= 0 {
		lines = append(lines, " Disk space required: "+summarySize(uint64(t.Delta), sys))
	} else {
		lines = append(lines, " Disk space to free: "+summarySize(uint64(-t.Delta), sys))
	}
	lines = append(lines, "")
	return lines
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

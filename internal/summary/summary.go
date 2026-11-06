// Package summary renders the nala-faithful inline transaction summary.
package summary

import (
	"fmt"
	"io"
	"strings"
)

// The actions an xbps plan row carries, as internal/xbps spells them.
const (
	actionInstall   = "install"
	actionUpdate    = "update"
	actionRemove    = "remove"
	actionConfigure = "configure"
	actionHold      = "hold"
	actionReinstall = "reinstall"
)

// manualHold is the only held reason xbps can supply.
const manualHold = "Manual hold"

// Row is one package line of an xbps dry-run transaction plan, as parsed by
// internal/xbps.
type Row struct {
	Name          string
	Version       string
	Action        string
	Reason        string
	Repo          string
	InstalledSize uint64
	DownloadSize  uint64
	HasInstalled  bool
	HasDownload   bool
}

// Old holds what the package looked like before an upgrade, from
// xbps-query. It is nil when the old version could not be looked up.
type Old struct {
	Version       string
	InstalledSize uint64
	HasSize       bool
}

// Summary is one transaction's worth of rows, grouped by nala operation and
// ready to render.
type Summary struct {
	// Command is the xbps command that produced the plan. It selects the
	// remove-family section label (Remove, AutoRemove, Purge, AutoPurge).
	Command string

	Remove    []Row
	Install   []Row
	ReInstall []Row
	Upgrade   []Row
	// Configure is listed separately from Install in rev 6.
	Configure []Row
	Hold      []Row

	// Old maps upgraded package names to their installed version and size.
	Old map[string]Old
	// Download is the total bytes the transaction fetches.
	Download uint64
	// AddBytes is the installed size of what the transaction unpacks,
	// RemoveBytes is the size removals free, and OldBytes is the size the
	// packages an upgrade or a reinstall replaces hold today.
	AddBytes, RemoveBytes, OldBytes uint64
}

// Build groups rows into the nala operation groups and totals the figures the
// footer reports.
func Build(rows []Row, held map[string]bool, old map[string]Old) *Summary {
	s := &Summary{Old: old}
	if old == nil {
		s.Old = map[string]Old{}
	}
	for _, row := range rows {
		isHeld := held[row.Name] || row.Action == actionHold
		if isHeld {
			// A held package is left out of the transaction: xbps fetches
			// and unpacks nothing for it, so it is reported under Held and
			// nowhere else (FR-011). xbps has exactly one hold reason; apt's
			// Excluded/Kept back/Phased vocabulary can never render (FR-064).
			row.Reason = manualHold
			s.Hold = append(s.Hold, row)
			continue
		}
		switch row.Action {
		case actionRemove:
			s.Remove = append(s.Remove, row)
			if row.HasInstalled {
				s.RemoveBytes += row.InstalledSize
			}
		case actionUpdate:
			s.Upgrade = append(s.Upgrade, row)
			s.addFetched(row)
			if old, ok := s.Old[row.Name]; ok && old.HasSize {
				s.OldBytes += old.InstalledSize
			}
		case actionReinstall:
			s.ReInstall = append(s.ReInstall, row)
			s.addFetched(row)
			// A reinstall replaces files the package already holds, so the
			// installed size it occupies today is netted out of FR-012's delta.
			if old, ok := s.Old[row.Name]; ok && old.HasSize {
				s.OldBytes += old.InstalledSize
			}
		case actionInstall:
			s.Install = append(s.Install, row)
			s.addFetched(row)
		case actionConfigure:
			// Rev 6 lists configure rows in their own section; their files
			// are already unpacked, so they contribute no fetch or disk bytes.
			s.Configure = append(s.Configure, row)
		}
	}
	return s
}

// addFetched adds the download and installed bytes of an install-family row.
func (s *Summary) addFetched(row Row) {
	if row.HasDownload {
		s.Download += row.DownloadSize
	}
	if row.HasInstalled {
		s.AddBytes += row.InstalledSize
	}
}

// NetDiskBytes is FR-012's net delta: new installed size minus old installed
// size minus removed size.
func (s *Summary) NetDiskBytes() int64 {
	return int64(s.AddBytes) - int64(s.OldBytes) - int64(s.RemoveBytes)
}

// SizeUnit selects the size base the summary prints. Binary uses 1024-based
// KiB/MiB/GiB; Decimal uses 1000-based kB/MB/GB. The zero value is Binary.
type SizeUnit string

// The two [Ui] unit values.
const (
	SizeBinary  SizeUnit = "Binary"
	SizeDecimal SizeUnit = "Decimal"
)

// Renderer draws a summary to a writer, optionally with ANSI colour.
type Renderer struct {
	Color bool
	// Simple selects FR-062's --simple layout.
	Simple bool
	// Width is the rule width: the stdout terminal width, else COLUMNS, else
	// 80. It is used even when piped.
	Width int
	// Piped disables full-width table expansion; tables keep natural widths.
	Piped bool
	Unit  SizeUnit
	Theme Theme
}

// Section is one rendered operation block: the label, the operation key the
// size and name rules switch on, and its rows.
type Section struct {
	Label string
	Op    string
	Rows  []Row
}

// Sections returns the non-empty sections in nala's fixed operation order.
// The inline renderer and the full-screen model both consume this table.
func (s *Summary) Sections() []Section {
	var out []Section
	if len(s.Remove) > 0 {
		out = append(out, Section{Label: RemoveLabel(s.Command), Op: "remove", Rows: s.Remove})
	}
	if len(s.Install) > 0 {
		out = append(out, Section{Label: "Install", Op: "install", Rows: s.Install})
	}
	if len(s.ReInstall) > 0 {
		out = append(out, Section{Label: "ReInstall", Op: "reinstall", Rows: s.ReInstall})
	}
	if len(s.Upgrade) > 0 {
		out = append(out, Section{Label: "Upgrade", Op: "upgrade", Rows: s.Upgrade})
	}
	// There is no Downgrade section on xbps: a downgrade is planned as an
	// install or an update and lands under those sections (FR-062).
	if len(s.Configure) > 0 {
		out = append(out, Section{Label: "Configure", Op: "configure", Rows: s.Configure})
	}
	if len(s.Hold) > 0 {
		out = append(out, Section{Label: "Held", Op: "held", Rows: s.Hold})
	}
	return out
}

// RemoveLabel maps the command that produced a remove-family plan to nala's
// operation label.
func RemoveLabel(command string) string {
	switch command {
	case "autoremove":
		return "AutoRemove"
	case "purge":
		return "Purge"
	case "autopurge":
		return "AutoPurge"
	default:
		return "Remove"
	}
}

// Render writes the grouped summary and its footer to w.
func (r Renderer) Render(w io.Writer, s *Summary) error {
	sections := s.Sections()
	if len(sections) == 0 {
		_, err := io.WriteString(w, "Nothing to do.\n")
		return err
	}
	var lines []string
	if r.Simple {
		lines = r.simpleLines(s, sections)
	} else {
		lines = r.fullLines(s, sections)
	}
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

// fullLines renders FR-062's full inline summary.
func (r Renderer) fullLines(s *Summary, sections []Section) []string {
	rule := strings.Repeat("=", r.ruleWidth())
	var lines []string
	for _, sec := range sections {
		lines = append(lines, rule)
		lines = append(lines, " "+r.paint(RoleHighlight, sec.Label))
		lines = append(lines, rule)
		lines = append(lines, r.table(s, sec).render(r.ruleWidth(), !r.Piped, r)...)
	}
	lines = append(lines, rule)
	lines = append(lines, " Summary")
	lines = append(lines, rule)
	for _, sec := range sections {
		lines = append(lines, " "+sec.Label+" "+itoa(uint64(len(sec.Rows))))
	}
	lines = append(lines, "")
	lines = append(lines, r.sizeLines(s)...)
	lines = append(lines, "")
	return lines
}

// simpleLines renders FR-062's --simple layout: no rules, no Summary heading,
// no version diff.
func (r Renderer) simpleLines(s *Summary, sections []Section) []string {
	var lines []string
	for _, sec := range sections {
		lines = append(lines, r.paint(RoleHighlight, sec.Label)+": "+itoa(uint64(len(sec.Rows))))
		names := make([]string, 0, len(sec.Rows))
		for _, row := range sec.Rows {
			name := row.Name
			if row.Reason != "" {
				name += " (" + row.Reason + ")"
			}
			names = append(names, name)
		}
		lines = append(lines, "  "+strings.Join(names, ", "))
	}
	lines = append(lines, "")
	lines = append(lines, r.sizeLines(s)...)
	lines = append(lines, "")
	return lines
}

// sizeLines is the size block shared by the full and simple layouts.
func (r Renderer) sizeLines(s *Summary) []string {
	var out []string
	if s.Download > 0 {
		out = append(out, " Total download size: "+HumanUnit(s.Download, r.Unit))
	}
	delta := s.NetDiskBytes()
	if delta >= 0 {
		out = append(out, " Disk space required: "+HumanUnit(uint64(delta), r.Unit))
	} else {
		out = append(out, " Disk space to free: "+HumanUnit(uint64(-delta), r.Unit))
	}
	return out
}

func (r Renderer) table(s *Summary, sec Section) table {
	rows := make([][]string, 0, len(sec.Rows))
	for _, row := range sec.Rows {
		rows = append(rows, r.rowCells(s, sec, row))
	}
	return table{header: sectionHeaders(s, sec), rows: rows}
}

// sectionHeaders returns the column set for a section. nala derives it from
// the section's rows; rev 6 normalises over the whole section so a held row
// whose versions differ cannot shift a later row's cells.
func sectionHeaders(s *Summary, sec Section) []string {
	hasDiff := sectionHasVersionDiff(s, sec)
	hasReason := sectionHasReason(sec)
	switch {
	case hasDiff && hasReason:
		return []string{"Package:", "Old Version:", "New Version:", "Reason:", "Size:"}
	case hasDiff:
		return []string{"Package:", "Old Version:", "New Version:", "Size:"}
	case hasReason:
		return []string{"Package:", "Version:", "Reason:", "Size:"}
	default:
		return []string{"Package:", "Version:", "Size:"}
	}
}

func sectionHasReason(sec Section) bool {
	for _, row := range sec.Rows {
		if row.Reason != "" {
			return true
		}
	}
	return false
}

func sectionHasVersionDiff(s *Summary, sec Section) bool {
	for _, row := range sec.Rows {
		if o, ok := s.oldVersion(row); ok && o != row.Version {
			return true
		}
	}
	return false
}

// oldVersion returns a row's installed version from the lookup, if known.
func (s *Summary) oldVersion(row Row) (string, bool) {
	o, ok := s.Old[row.Name]
	if !ok || o.Version == "" {
		return "", false
	}
	return o.Version, true
}

// rowCells produces one row's cells in the section's column shape.
func (r Renderer) rowCells(s *Summary, sec Section, row Row) []string {
	nameRole := RoleSecondary
	switch sec.Op {
	case "remove":
		nameRole = RoleError
	case "held", "reinstall", "configure":
		nameRole = RoleNotice
	}
	name := r.paint(nameRole, row.Name)
	version := row.Version
	if version == "" {
		version = "Unknown"
	}
	sizeText, hasSize := sectionSize(sec.Op, row)
	size := "—"
	if hasSize {
		size = HumanUnit(sizeText, r.Unit)
	}
	hasDiff := sectionHasVersionDiff(s, sec)
	oldVer, hasOld := s.oldVersion(row)
	switch {
	case hasDiff && sec.Rows[0].Reason != "":
		return []string{name, oldVersionCell(oldVer, hasOld), r.diffCell(oldVer, version), row.Reason, size}
	case hasDiff:
		return []string{name, oldVersionCell(oldVer, hasOld), r.diffCell(oldVer, version), size}
	case row.Reason != "":
		return []string{name, version, row.Reason, size}
	default:
		return []string{name, version, size}
	}
}

// oldVersionCell keeps the old version raw (FR-063).
func oldVersionCell(oldVer string, ok bool) string {
	if !ok {
		return "—"
	}
	return oldVer
}

// diffCell highlights only the new version's changed components. VersionParts
// leaves the text raw when the old version is missing or identical.
func (r Renderer) diffCell(oldVer, newVer string) string {
	var b strings.Builder
	for _, part := range VersionParts(oldVer, newVer) {
		if part.Changed {
			b.WriteString(r.paint(RoleNotice, part.Text))
		} else {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// sectionSize applies the operation's Size: semantics. Removals and
// configure rows show the installed size they free; the install family shows
// the fetch size when there is one and falls back to the installed size.
func sectionSize(op string, row Row) (uint64, bool) {
	switch op {
	case "remove", "configure":
		return row.InstalledSize, row.HasInstalled
	default:
		if row.HasDownload {
			return row.DownloadSize, true
		}
		return row.InstalledSize, row.HasInstalled
	}
}

func (r Renderer) ruleWidth() int {
	if r.Width > 0 {
		return r.Width
	}
	return 80
}

func (r Renderer) paint(role Role, text string) string {
	if !r.Color || text == "" {
		return text
	}
	prefix := r.Theme.prefix(role)
	if prefix == "" {
		return text
	}
	return prefix + text + resetSeq
}

// Human renders a widget size: Binary, precision 1 (FR-064). It is kept as
// the precision-1 compatibility helper alongside WidgetSize.
func Human(n uint64) string { return WidgetSize(n) }

// WidgetSize renders a byte count the way the progress widget does: always
// Binary, precision 1, independent of [Ui] unit (FR-064, ADR-0012 §5).
func WidgetSize(n uint64) string { return formatUnit(n, SizeBinary, 1) }

// HumanUnit renders a summary size: the requested [Ui] unit, precision 0, and
// nala's strict '>' unit switch (FR-062).
func HumanUnit(n uint64, unit SizeUnit) string { return formatUnit(n, unit, 0) }

// WidgetUnit is the full-screen footer's size format: the requested [Ui] unit
// with the progress widget's precision 1 (ADR-0017's 12.4 MiB golden). It is
// the unit-aware counterpart of WidgetSize, which is always Binary.
func WidgetUnit(n uint64, unit SizeUnit) string { return formatUnit(n, unit, 1) }

// formatUnit reproduces nala's UnitStr::str: precision digits, a strict '>'
// unit switch, and no unit below the base.
func formatUnit(n uint64, unit SizeUnit, precision int) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	divisor := 1024.0
	if unit == SizeDecimal {
		units = []string{"B", "KB", "MB", "GB", "TB", "PB"}
		divisor = 1000.0
	}
	value := float64(n)
	u := 0
	for u < len(units)-1 && value > divisor {
		value /= divisor
		u++
	}
	return fmt.Sprintf("%.*f %s", precision, value, units[u])
}

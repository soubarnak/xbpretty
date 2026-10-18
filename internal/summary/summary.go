// Package summary renders the colored transaction summary printed after a
// dry run.
package summary

import (
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

// Row is one package line of an xbps dry-run transaction plan, as parsed by
// internal/xbps.
type Row struct {
	Name          string
	Version       string
	Action        string
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

// Summary is one transaction's worth of rows, grouped and ready to render.
type Summary struct {
	Install []Row
	Upgrade []Row
	Remove  []Row
	Hold    []Row

	// Old maps upgraded package names to their installed version and size.
	Old map[string]Old
	// Download is the total bytes the transaction fetches.
	Download uint64
	// AddBytes is the installed size of what the transaction unpacks,
	// RemoveBytes is the size removals free, and OldBytes is the size the
	// packages an upgrade replaces hold today.
	AddBytes, RemoveBytes, OldBytes uint64
}

// Build groups rows into the Install, Upgrade, Remove and Hold groups and
// totals the figures the footer reports.
func Build(rows []Row, held map[string]bool, old map[string]Old) *Summary {
	s := &Summary{Old: old}
	if old == nil {
		s.Old = map[string]Old{}
	}
	for _, row := range rows {
		if held[row.Name] || row.Action == actionHold {
			// A held package is left out of the transaction: xbps fetches
			// and unpacks nothing for it, so it is reported under Hold and
			// nowhere else (FR-011).
			s.Hold = append(s.Hold, row)
			continue
		}
		switch row.Action {
		case actionRemove:
			// A removal fetches nothing, so its download size stays out of
			// the total; its installed size is what the transaction frees.
			s.Remove = append(s.Remove, row)
			if row.HasInstalled {
				s.RemoveBytes += row.InstalledSize
			}
		case actionUpdate, actionReinstall:
			s.Upgrade = append(s.Upgrade, row)
			if row.HasDownload {
				s.Download += row.DownloadSize
			}
			if row.HasInstalled {
				s.AddBytes += row.InstalledSize
			}
			if o, ok := s.Old[row.Name]; ok && o.HasSize {
				s.OldBytes += o.InstalledSize
			}
		case actionInstall:
			s.Install = append(s.Install, row)
			if row.HasDownload {
				s.Download += row.DownloadSize
			}
			if row.HasInstalled {
				s.AddBytes += row.InstalledSize
			}
		case actionConfigure:
			// FR-011 counts configure actions but does not list them. The
			// package's files are already unpacked, so nothing is fetched or
			// added to disk either.
			s.Install = append(s.Install, row)
		}
	}
	return s
}

// listable drops the rows a group holds but does not list, which is the
// configure action (FR-011).
func listable(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Action != actionConfigure {
			out = append(out, row)
		}
	}
	return out
}

// hasListable reports whether a group has any row to list.
func hasListable(rows []Row) bool {
	return len(listable(rows)) > 0
}

// Counts holds the number of packages in each group.
type Counts struct {
	Install, Upgrade, Remove, Hold int
}

// Counts returns how many packages landed in each group.
func (s *Summary) Counts() Counts {
	return Counts{
		Install: len(s.Install),
		Upgrade: len(s.Upgrade),
		Remove:  len(s.Remove),
		Hold:    len(s.Hold),
	}
}

// Total returns whether the transaction would change anything.
func (s *Summary) Total() int {
	return len(s.Install) + len(s.Upgrade) + len(s.Remove)
}

// NetDiskBytes is FR-012's net delta: new installed size minus old installed
// size minus removed size. An upgrade the lookups did not answer for brings
// no old size with it, so only its new bytes count.
func (s *Summary) NetDiskBytes() int64 {
	return int64(s.AddBytes) - int64(s.OldBytes) - int64(s.RemoveBytes)
}

// Renderer draws a summary to a writer, optionally with ANSI colour.
type Renderer struct {
	Color bool
}

// Render writes the grouped summary and its footer to w.
func (r Renderer) Render(w io.Writer, s *Summary) error {
	if hasListable(s.Install) {
		if err := r.group(w, "Install", s.Install, s); err != nil {
			return err
		}
	}
	if hasListable(s.Upgrade) {
		if err := r.group(w, "Upgrade", s.Upgrade, s); err != nil {
			return err
		}
	}
	if hasListable(s.Remove) {
		if err := r.group(w, "Remove", s.Remove, s); err != nil {
			return err
		}
	}
	if hasListable(s.Hold) {
		if err := r.group(w, "Hold", s.Hold, s); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, r.footer(s)+"\n")
	return err
}

func (r Renderer) group(w io.Writer, title string, rows []Row, s *Summary) error {
	if _, err := io.WriteString(w, r.paint(boldSeq, title)+"\n"); err != nil {
		return err
	}
	shown := listable(rows)
	nameWidth := widest(func(i int) string { return shown[i].Name }, len(shown))
	verWidth := widest(func(i int) string { return s.version(shown[i]) }, len(shown))
	for i := range shown {
		line := padRight(shown[i].Name, nameWidth) + "  " +
			padRight(s.version(shown[i]), verWidth) + "  " +
			r.size(shown[i])
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func (s *Summary) version(row Row) string {
	if row.Action != actionUpdate && row.Action != actionReinstall {
		return row.Version
	}
	if o, ok := s.Old[row.Name]; ok && o.Version != "" {
		return o.Version + " → " + row.Version
	}
	return row.Version
}

// size is the figure a row's column shows. A package the transaction fetches
// is shown by what it downloads; a removal or a held package, which is not
// fetched, by what it occupies on disk.
func (r Renderer) size(row Row) string {
	fetched := row.Action != actionRemove && row.Action != actionHold
	switch {
	case fetched && row.HasDownload:
		return Human(row.DownloadSize)
	case row.HasInstalled:
		return Human(row.InstalledSize)
	}
	return "-"
}

// Human renders a byte count the way the summary shows sizes, e.g. `48.2 MiB`
// (FR-012).
func Human(n uint64) string {
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case n >= gib:
		return trimZero(float64(n)/gib) + " GiB"
	case n >= mib:
		return trimZero(float64(n)/mib) + " MiB"
	case n >= kib:
		return trimZero(float64(n)/kib) + " KiB"
	}
	return formatInt(n) + " B"
}

func trimZero(v float64) string {
	s := sprintf("%.1f", v)
	if strings.HasSuffix(s, ".0") {
		return s[:len(s)-2]
	}
	return s
}

func formatInt(n uint64) string {
	return itoa(n)
}

// Footer is the counts, download total and net disk delta line.
func (r Renderer) footer(s *Summary) string {
	c := s.Counts()
	parts := []string{}
	if c.Install > 0 {
		parts = append(parts, count(c.Install, "install"))
	}
	if c.Upgrade > 0 {
		parts = append(parts, count(c.Upgrade, "upgrade"))
	}
	if c.Remove > 0 {
		parts = append(parts, count(c.Remove, "remove"))
	}
	if c.Hold > 0 {
		parts = append(parts, count(c.Hold, "hold"))
	}
	counts := strings.Join(parts, ", ")
	line := counts
	if s.Download > 0 {
		line += " · " + Human(s.Download) + " download"
	}
	net := s.NetDiskBytes()
	if net == 0 {
		return line
	}
	sign := "+"
	abs := uint64(net)
	if net < 0 {
		sign = "-"
		abs = uint64(-net)
	}
	line += " · " + sign + Human(abs) + " disk"
	return line
}

// count renders one group's tally. The nouns stay as FR-012's example spells
// them, so three installs read `3 install`.
func count(n int, noun string) string {
	return itoa(uint64(n)) + " " + noun
}

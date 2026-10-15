// Package summary renders the colored transaction summary printed after a
// dry run.
package summary

import (
	"io"
	"sort"
	"strings"
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
	// AddBytes is newly installed bytes, RemoveBytes is bytes freed by
	// removals, and ObsoleteBytes is old bytes an upgrade frees when the
	// package shrinks on disk.
	AddBytes, RemoveBytes, ObsoleteBytes uint64
}

// Build groups rows into the Install, Upgrade, Remove and Hold groups and
// totals the figures the footer reports.
func Build(rows []Row, held map[string]bool, old map[string]Old) *Summary {
	s := &Summary{Old: old}
	if old == nil {
		s.Old = map[string]Old{}
	}
	for _, row := range rows {
		if s.hasDownload(row) {
			s.Download += row.DownloadSize
		}
		switch row.Action {
		case "remove":
			s.Remove = append(s.Remove, row)
			if row.HasInstalled {
				s.RemoveBytes += row.InstalledSize
			}
		case "update", "reinstall":
			s.Upgrade = append(s.Upgrade, row)
			if row.HasInstalled {
				s.AddBytes += row.InstalledSize
			}
			if o, ok := s.Old[row.Name]; ok && o.HasSize && o.InstalledSize > row.InstalledSize {
				s.ObsoleteBytes += o.InstalledSize - row.InstalledSize
			}
			if held[row.Name] {
				s.Hold = append(s.Hold, row)
			}
		case "install", "configure":
			s.Install = append(s.Install, row)
			if row.HasInstalled {
				s.AddBytes += row.InstalledSize
			}
			if held[row.Name] {
				s.Hold = append(s.Hold, row)
			}
		case "hold":
			s.Hold = append(s.Hold, row)
		}
	}
	return s
}

func (s *Summary) hasDownload(row Row) bool {
	return row.HasDownload
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

// NetDiskBytes is the net change in disk usage: new bytes plus upgrade
// growth, minus obsolete and removed bytes.
func (s *Summary) NetDiskBytes() int64 {
	net := int64(s.AddBytes) - int64(s.ObsoleteBytes) - int64(s.RemoveBytes)
	return net
}

// Renderer draws a summary to a writer, optionally with ANSI colour.
type Renderer struct {
	Color bool
}

// Render writes the grouped summary and its footer to w.
func (r Renderer) Render(w io.Writer, s *Summary) error {
	holdNote := r.holdNote(s)
	if len(s.Install) > 0 {
		if err := r.group(w, "Install", s.Install, s, holdNote); err != nil {
			return err
		}
	}
	if len(s.Upgrade) > 0 {
		if err := r.group(w, "Upgrade", s.Upgrade, s, holdNote); err != nil {
			return err
		}
	}
	if len(s.Remove) > 0 {
		if err := r.group(w, "Remove", s.Remove, s, holdNote); err != nil {
			return err
		}
	}
	if len(s.Hold) > 0 {
		if err := r.group(w, "Hold", s.Hold, s, holdNote); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, r.footer(s)+"\n")
	return err
}

func (r Renderer) holdNote(s *Summary) string {
	if len(s.Hold) == 0 {
		return ""
	}
	names := make([]string, 0, len(s.Hold))
	for _, row := range s.Hold {
		names = append(names, row.Name)
	}
	sort.Strings(names)
	return "held: " + strings.Join(names, ", ")
}

func (r Renderer) group(w io.Writer, title string, rows []Row, s *Summary, holdNote string) error {
	if _, err := io.WriteString(w, r.paint(boldSeq, title)+"\n"); err != nil {
		return err
	}
	nameWidth := widest(func(i int) string { return rows[i].Name }, len(rows))
	verWidth := widest(func(i int) string { return s.version(rows[i]) }, len(rows))
	sizeWidth := widest(func(i int) string { return r.size(rows[i]) }, len(rows))
	for i := range rows {
		line := padRight(rows[i].Name, nameWidth) + "  " +
			padRight(s.version(rows[i]), verWidth) + "  " +
			padRight(r.size(rows[i]), sizeWidth)
		if rows[i].Action == "remove" && rows[i].HasDownload {
			line += "  -" + r.size(Row{DownloadSize: rows[i].DownloadSize, HasDownload: true})
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	if holdNote != "" && title != "Hold" {
		if _, err := io.WriteString(w, "  "+r.paint(dimSeq, holdNote)+"\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func (s *Summary) version(row Row) string {
	if row.Action != "update" && row.Action != "reinstall" {
		return row.Version
	}
	if o, ok := s.Old[row.Name]; ok && o.Version != "" {
		return o.Version + " → " + row.Version
	}
	return row.Version
}

func (r Renderer) size(row Row) string {
	switch {
	case row.HasDownload:
		return Human(row.DownloadSize)
	case row.HasInstalled:
		return Human(row.InstalledSize)
	}
	return "-"
}

// Human renders a byte count the way the summary shows sizes.
func Human(n uint64) string {
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case n >= gib:
		return trimZero(float64(n)/gib) + "GiB"
	case n >= mib:
		return trimZero(float64(n)/mib) + "MiB"
	case n >= kib:
		return trimZero(float64(n)/kib) + "KiB"
	}
	return formatInt(n) + "B"
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
		parts = append(parts, plural(c.Install, "install"))
	}
	if c.Upgrade > 0 {
		parts = append(parts, plural(c.Upgrade, "upgrade"))
	}
	if c.Remove > 0 {
		parts = append(parts, plural(c.Remove, "remove"))
	}
	if c.Hold > 0 {
		parts = append(parts, plural(c.Hold, "hold"))
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

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(uint64(n)) + " " + noun + "s"
}

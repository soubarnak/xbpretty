// Package summary renders the colored transaction summary printed after a
// dry run.
package summary

import (
	"io"
	"strings"
	"unicode/utf8"
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
	Plain bool
	Width int
}

// Render writes the grouped summary and its footer to w.
func (r Renderer) Render(w io.Writer, s *Summary) error {
	if r.Plain || !r.Color {
		return r.renderPlain(w, s)
	}
	return r.renderBoxes(w, s)
}

func (r Renderer) renderPlain(w io.Writer, s *Summary) error {
	if hasListable(s.Install) {
		if err := r.groupPlain(w, "Install", s.Install, s); err != nil {
			return err
		}
	}
	if hasListable(s.Upgrade) {
		if err := r.groupPlain(w, "Upgrade", s.Upgrade, s); err != nil {
			return err
		}
	}
	if hasListable(s.Remove) {
		if err := r.groupPlain(w, "Remove", s.Remove, s); err != nil {
			return err
		}
	}
	if hasListable(s.Hold) {
		if err := r.groupPlain(w, "Hold", s.Hold, s); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, r.footer(s)+"\n")
	return err
}

func (r Renderer) groupPlain(w io.Writer, title string, rows []Row, s *Summary) error {
	header := title
	if !r.Plain {
		header = r.paint(boldSeq, title)
	}
	if _, err := io.WriteString(w, header+"\n"); err != nil {
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

func (r Renderer) renderBoxes(w io.Writer, s *Summary) error {
	termWidth := r.Width
	if termWidth <= 0 {
		termWidth = 80
	}

	type groupData struct {
		title     string
		colorCode string
		headers   []string
		rows      [][]string
	}
	var groups []groupData

	if hasListable(s.Install) {
		shown := listable(s.Install)
		var rows [][]string
		for _, row := range shown {
			rows = append(rows, []string{
				greenSeq + row.Name + resetSeq,
				row.Version,
				r.size(row),
			})
		}
		groups = append(groups, groupData{
			title:     sprintf("Packages To Install (%d)", len(shown)),
			colorCode: greenSeq,
			headers:   []string{"Package", "Version", "Size"},
			rows:      rows,
		})
	}

	if hasListable(s.Upgrade) {
		shown := listable(s.Upgrade)
		var rows [][]string
		for _, row := range shown {
			name := blueSeq + row.Name + resetSeq
			vDiff := row.Version
			if old, ok := s.Old[row.Name]; ok && old.Version != "" {
				vDiff = dimSeq + old.Version + resetSeq + " → " + VersionDiff(old.Version, row.Version, true)
			}
			rows = append(rows, []string{
				name,
				vDiff,
				r.size(row),
			})
		}
		groups = append(groups, groupData{
			title:     sprintf("Packages To Upgrade (%d)", len(shown)),
			colorCode: blueSeq,
			headers:   []string{"Package", "Version Delta", "Size"},
			rows:      rows,
		})
	}

	if hasListable(s.Remove) {
		shown := listable(s.Remove)
		var rows [][]string
		for _, row := range shown {
			rows = append(rows, []string{
				redSeq + row.Name + resetSeq,
				row.Version,
				r.size(row),
			})
		}
		groups = append(groups, groupData{
			title:     sprintf("Packages To Remove (%d)", len(shown)),
			colorCode: redSeq,
			headers:   []string{"Package", "Version", "Size"},
			rows:      rows,
		})
	}

	if hasListable(s.Hold) {
		shown := listable(s.Hold)
		hasDelta := false
		for _, row := range shown {
			if old, ok := s.Old[row.Name]; ok && old.Version != "" {
				hasDelta = true
				break
			}
		}
		headers := []string{"Package", "Version", "Size"}
		if hasDelta {
			headers[1] = "Version Delta"
		}
		var rows [][]string
		for _, row := range shown {
			name := boldOrangeSeq + row.Name + resetSeq
			vDiff := row.Version
			if old, ok := s.Old[row.Name]; ok && old.Version != "" {
				vDiff = dimSeq + old.Version + resetSeq + " → " + VersionDiff(old.Version, row.Version, true)
			}
			rows = append(rows, []string{
				name,
				vDiff,
				r.size(row),
			})
		}
		groups = append(groups, groupData{
			title:     sprintf("Packages To Hold (%d)", len(shown)),
			colorCode: boldOrangeSeq,
			headers:   headers,
			rows:      rows,
		})
	}

	for _, g := range groups {
		if err := r.renderBox(w, g.title, g.colorCode, g.headers, g.rows, termWidth); err != nil {
			return err
		}
	}

	return r.renderSummaryCard(w, s, termWidth)
}

func (r Renderer) renderBox(w io.Writer, title, colorCode string, headers []string, rows [][]string, termWidth int) error {
	col0Min := visibleWidth(headers[0])
	col1Min := visibleWidth(headers[1])
	col2Min := visibleWidth(headers[2])
	for _, row := range rows {
		if v := visibleWidth(row[0]); v > col0Min {
			col0Min = v
		}
		if v := visibleWidth(row[1]); v > col1Min {
			col1Min = v
		}
		if v := visibleWidth(row[2]); v > col2Min {
			col2Min = v
		}
	}

	col0Width := col0Min + 2
	if col0Width < 24 {
		col0Width = 24
	}
	if len(headers) > 1 && headers[1] == "Version" && col0Width < 34 {
		col0Width = 34
	}
	col2Width := col2Min + 2
	if col2Width < 12 {
		col2Width = 12
	}
	col1Width := col1Min + 2
	if col1Width < 28 {
		col1Width = 28
	}

	width := termWidth
	needed := 5 + col0Width + col1Width + col2Width
	if needed > width {
		width = needed
	} else {
		col1Width = width - 5 - col0Width - col2Width
	}

	titleRunes := utf8.RuneCountInString(title)
	dashes := width - titleRunes - 6
	if dashes < 1 {
		dashes = 1
	}
	top := "╭── " + colorCode + title + resetSeq + " " + strings.Repeat("─", dashes) + "╮\n"
	if _, err := io.WriteString(w, top); err != nil {
		return err
	}

	hLine := "│  " + padRight(boldSeq+headers[0]+resetSeq, col0Width) +
		padRight(boldSeq+headers[1]+resetSeq, col1Width) +
		padLeft(boldSeq+headers[2]+resetSeq, col2Width)
	if _, err := io.WriteString(w, padRight(hLine, width-1)+"│\n"); err != nil {
		return err
	}

	div := "├" + strings.Repeat("─", width-2) + "┤\n"
	if _, err := io.WriteString(w, div); err != nil {
		return err
	}

	for _, row := range rows {
		rowLine := "│  " + padRight(row[0], col0Width) +
			padRight(row[1], col1Width) +
			padLeft(row[2], col2Width)
		if _, err := io.WriteString(w, padRight(rowLine, width-1)+"│\n"); err != nil {
			return err
		}
	}

	bottom := "╰" + strings.Repeat("─", width-2) + "╯\n\n"
	_, err := io.WriteString(w, bottom)
	return err
}

func (r Renderer) renderSummaryCard(w io.Writer, s *Summary, termWidth int) error {
	width := termWidth
	c := s.Counts()
	var parts []string
	if c.Install > 0 {
		parts = append(parts, greenSeq+count(c.Install, "install")+resetSeq)
	}
	if c.Upgrade > 0 {
		parts = append(parts, blueSeq+count(c.Upgrade, "upgrade")+resetSeq)
	}
	if c.Remove > 0 {
		parts = append(parts, redSeq+count(c.Remove, "remove")+resetSeq)
	}
	if c.Hold > 0 {
		parts = append(parts, boldOrangeSeq+count(c.Hold, "hold")+resetSeq)
	}
	countsText := strings.Join(parts, ", ")

	var statParts []string
	if s.Download > 0 {
		statParts = append(statParts, sprintf("Transfer: %s download", Human(s.Download)))
	}
	net := s.NetDiskBytes()
	if net != 0 {
		sign := "+"
		abs := uint64(net)
		if net < 0 {
			sign = "-"
			abs = uint64(-net)
		}
		statParts = append(statParts, sprintf("Disk: %s%s", sign, Human(abs)))
	}
	if len(statParts) == 0 {
		statParts = append(statParts, "Transfer: 0 B  •  Disk: 0 B")
	}
	statsText := strings.Join(statParts, "  •  ")

	title := "Summary"
	dashes := width - utf8.RuneCountInString(title) - 6
	if dashes < 1 {
		dashes = 1
	}
	top := "╭── " + boldSeq + title + resetSeq + " " + strings.Repeat("─", dashes) + "╮\n"
	if _, err := io.WriteString(w, top); err != nil {
		return err
	}

	line1 := "│  Changes:  " + countsText
	if _, err := io.WriteString(w, padRight(line1, width-1)+"│\n"); err != nil {
		return err
	}

	line2 := "│  " + statsText
	if _, err := io.WriteString(w, padRight(line2, width-1)+"│\n"); err != nil {
		return err
	}

	bottom := "╰" + strings.Repeat("─", width-2) + "╯\n"
	_, err := io.WriteString(w, bottom)
	return err
}

func (r Renderer) paint(code, text string) string {
	if !r.Color || r.Plain || code == "" {
		return text
	}
	return code + text + resetSeq
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

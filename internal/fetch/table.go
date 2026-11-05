package fetch

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FormatSpeed renders a byte rate as Binary precision 1 plus /s (FR-050).
func FormatSpeed(bytesPerSecond float64) string {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	const (
		kib = 1024.0
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case bytesPerSecond >= gib:
		return fmt.Sprintf("%.1f GiB/s", bytesPerSecond/gib)
	case bytesPerSecond >= mib:
		return fmt.Sprintf("%.1f MiB/s", bytesPerSecond/mib)
	case bytesPerSecond >= kib:
		return fmt.Sprintf("%.1f KiB/s", bytesPerSecond/kib)
	default:
		return fmt.Sprintf("%.1f B/s", bytesPerSecond)
	}
}

// RenderTable writes FR-050's borderless ranking table to w. The chosen row
// gets a star in the last column. Column widths are the widest cell; the
// first three columns are left-aligned and the rest right-aligned, with two
// spaces between columns and no trailing whitespace.
func RenderTable(w io.Writer, results []Result, chosenURL string) error {
	header := []string{"Mirror", "Region", "Location", "Tier", "Latency", "Speed", "Score", "Chosen"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		location := r.Location
		if location == "" {
			location = "—"
		}
		marker := ""
		if chosenURL != "" && r.URL == chosenURL {
			marker = "*"
		}
		rows = append(rows, []string{
			r.URL,
			r.Region,
			location,
			strconv.Itoa(r.Tier),
			fmt.Sprintf("%d ms", r.LatencyMS),
			FormatSpeed(r.Speed),
			fmt.Sprintf("%d ms", r.ScoreMS),
			marker,
		})
	}
	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = width(cell)
	}
	for _, row := range rows {
		for i, cell := range row {
			if got := width(cell); got > widths[i] {
				widths[i] = got
			}
		}
	}
	leftAligned := []bool{true, true, true, false, false, false, false, false}

	var b strings.Builder
	writeLine := func(cells []string) error {
		parts := make([]string, len(cells))
		for i, cell := range cells {
			parts[i] = pad(cell, widths[i], leftAligned[i])
		}
		line := strings.TrimRight(strings.Join(parts, "  "), " ")
		b.WriteString(line)
		b.WriteByte('\n')
		return nil
	}
	if err := writeLine(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeLine(row); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func width(s string) int { return utf8.RuneCountInString(s) }

func pad(s string, w int, left bool) string {
	gap := w - width(s)
	if gap <= 0 {
		return s
	}
	spaces := strings.Repeat(" ", gap)
	if left {
		return s + spaces
	}
	return spaces + s
}

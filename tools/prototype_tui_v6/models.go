package main

import (
	"strings"

	"github.com/soubarnak/xbpretty/internal/term"
)

// goldenUpgrade is #50 §2.7's illustrative frame data.
func goldenUpgrade() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []Tab{{Label: "Upgrade", Rows: []PkgRow{
			{Name: "libfoo", Old: "1.2_1", New: "1.3_1", Size: "120 KiB", NewSpans: versionSpans("1.2_1", "1.3_1")},
			{Name: "libbar", Old: "0.9_2", New: "0.9_3", Size: "44 KiB", NewSpans: versionSpans("0.9_2", "0.9_3")},
			{Name: "libbaz", Old: "2.0_1", New: "2.0_2", Size: "8 KiB", NewSpans: versionSpans("2.0_1", "2.0_2")},
		}}},
		Download:  "12.4 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "45.0 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start upgrade",
		},
	}
}

// fiveOpUndo is the 60x18 floor fixture: five operation tabs.
func fiveOpUndo() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Undo",
		Tabs: []Tab{
			{Label: "Remove", Rows: []PkgRow{
				{Name: "nano", Old: "9.2_1", Size: "3 MiB"},
				{Name: "curl", Old: "8.17.0_1", Size: "2 MiB"},
			}},
			{Label: "AutoRemove", Rows: []PkgRow{{Name: "libtinfo", Old: "6.5_1", Size: "1 MiB"}}},
			{Label: "Install", Rows: []PkgRow{
				{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"},
				{Name: "cowsay", New: "3.8.4_1", Size: "19 KiB"},
			}},
			{Label: "Upgrade", Rows: []PkgRow{
				{Name: "voiced", Old: "1.2.0_1", New: "1.3.0_1", Size: "12 MiB", NewSpans: versionSpans("1.2.0_1", "1.3.0_1")},
			}},
			{Label: "Held", Rows: []PkgRow{
				{Name: "acl", Old: "2.4.0_1", New: "2.4.0_1", Reason: "Manual hold", Size: "20 KiB"},
			}},
		},
		Download:  "12 MiB",
		DiskLabel: "Disk space to free:",
		Disk:      "2 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start upgrade",
		},
	}
}

// goldenInstall is a one-operation, three-row install.
func goldenInstall() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Install",
		Tabs: []Tab{{Label: "Install", Rows: []PkgRow{
			{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"},
			{Name: "cowsay", New: "3.8.4_1", Size: "19 KiB"},
			{Name: "toilet", New: "0.3_2", Size: "1 MiB"},
		}}},
		Download:  "1 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "4 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start install",
		},
	}
}

// goldenHistory is the read-only history detail variant (#50 §4).
func goldenHistory() *SummaryModel {
	return &SummaryModel{
		Title:   "xbpretty History",
		History: true,
		Tabs: []Tab{
			{Label: "Remove", Rows: []PkgRow{{Name: "nano", Old: "9.2_1", Size: "3 MiB"}}},
			{Label: "Install", Rows: []PkgRow{
				{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"},
				{Name: "cowsay", New: "3.8.4_1", Size: "19 KiB"},
			}},
		},
		Help: []string{
			"(↑↓) move | (→←) tabs",
			"(s) package info",
			"(q) quit",
		},
	}
}

// versionSpans implements #45 §3.4's diff as cell styles (no embedded SGR).
func versionSpans(oldV, newV string) []Span {
	decompose := func(v string) (up, rev string) {
		if i := strings.LastIndex(v, "_"); i >= 0 {
			return v[:i], v[i+1:]
		}
		return v, ""
	}
	oldUp, oldRev := decompose(oldV)
	newUp, newRev := decompose(newV)
	if oldUp == newUp {
		if oldRev == newRev {
			return []Span{{Text: newV, Style: Regular}}
		}
		prefix := newUp
		if newRev != "" {
			prefix += "_"
		}
		if prefix == "" {
			return []Span{{Text: newRev, Style: Notice}}
		}
		return []Span{{Text: prefix, Style: Regular}, {Text: newRev, Style: Notice}}
	}
	op := strings.Split(oldUp, ".")
	np := strings.Split(newUp, ".")
	k := 0
	for k < len(op) && k < len(np) && op[k] == np[k] {
		k++
	}
	var spans []Span
	if k > 0 {
		spans = append(spans, Span{Text: strings.Join(np[:k], ".") + ".", Style: Regular})
	}
	for j := k; j < len(np); j++ {
		text := np[j]
		if j == len(np)-1 && newRev != "" {
			text += "_" + newRev
		}
		spans = append(spans, Span{Text: text, Style: Notice})
		if j < len(np)-1 {
			spans = append(spans, Span{Text: ".", Style: Regular})
		}
	}
	if len(spans) == 0 {
		return []Span{{Text: newV, Style: Regular}}
	}
	return spans
}

// Mirror is #48 §5.5's picker row.
type Mirror struct {
	URL      string
	Location string
	Region   string
	Tier     int
	Latency  string
	Speed    string
	Score    int
}

// goldenMirrors is #50 §5.3's frame data.
func goldenMirrors() []Mirror {
	return []Mirror{
		{"https://repo.voidlinux.org/current", "Finland", "EU", 1, "18 ms", "4.1 MiB/s", 18},
		{"https://mirror.example.net/voidlinux", "Germany", "EU", 2, "31 ms", "3.2 MiB/s", 31},
		{"https://mirror.osuosl.org/voidlinux", "United States", "NA", 2, "47 ms", "2.8 MiB/s", 47},
		{"https://slow.example.org/voidlinux", "Japan", "AS", 3, "112 ms", "0.9 MiB/s", 112},
	}
}

var _ = term.StringWidth

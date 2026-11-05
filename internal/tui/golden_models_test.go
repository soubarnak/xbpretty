package tui

// Golden fixtures for the frame tests. They are plain data so the expected
// bytes are independent of the plan-to-model conversion in the cli package.

func goldenMultiTab() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Undo",
		Tabs: []Tab{
			{Label: "Remove", Rows: []PkgRow{
				{Name: "nano", Old: "9.2_1", Size: "3 MiB"},
				{Name: "curl", Old: "8.17.0_1", Size: "2 MiB"},
			}},
			{Label: "Install", Rows: []PkgRow{
				{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"},
			}},
			{Label: "ReInstall", Rows: []PkgRow{
				{Name: "zsh", New: "5.9_3", Size: "2 MiB"},
			}},
			{Label: "Upgrade", Rows: []PkgRow{
				{Name: "glibc", Old: "2.41_1", New: "2.41_2", Size: "12 MiB", NewSpans: versionSpans("2.41_1", "2.41_2")},
			}},
			{Label: "Held", Rows: []PkgRow{
				{Name: "bash", Old: "5.2_1", New: "5.2_2", Reason: "Manual hold", Size: "1 MiB", NewSpans: versionSpans("5.2_1", "5.2_2")},
				{Name: "tzdata", Old: "2025a_1", New: "2025a_1", Reason: "Manual hold", Size: "100 KiB"},
			}},
		},
		Download:  "9.2 MiB",
		DiskLabel: "Disk space to free:",
		Disk:      "2.0 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start undo",
		},
	}
}

func goldenHeldMixed() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []Tab{{Label: "Held", Rows: []PkgRow{
			{Name: "bash", Old: "5.2_1", New: "5.2_2", Reason: "Manual hold", Size: "1 MiB", NewSpans: versionSpans("5.2_1", "5.2_2")},
			{Name: "tzdata", Old: "2025a_1", New: "2025a_1", Reason: "Manual hold", Size: "100 KiB"},
			{Name: "openssl", Old: "3.3.1_1", New: "3.3.2_1", Reason: "Manual hold", Size: "4 MiB", NewSpans: versionSpans("3.3.1_1", "3.3.2_1")},
		}}},
		Sel:       1,
		Download:  "2.0 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "1.5 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start upgrade",
		},
	}
}

func goldenScrollable() *SummaryModel {
	rows := make([]PkgRow, 0, 30)
	for i := 0; i < 30; i++ {
		name := "pkg" + itoa(i)
		rows = append(rows, PkgRow{
			Name: name, Old: "1.0_" + itoa(i), New: "1.1_" + itoa(i), Size: "1 MiB",
			NewSpans: versionSpans("1.0_"+itoa(i), "1.1_"+itoa(i)),
		})
	}
	return &SummaryModel{
		Title:  "xbpretty Upgrade",
		Tabs:   []Tab{{Label: "Upgrade", Rows: rows}},
		Active: 0, Sel: 25,
		Download:  "30 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "45 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start upgrade",
		},
	}
}

func goldenHistory() *SummaryModel {
	return &SummaryModel{
		Title:   "xbpretty History",
		History: true,
		Tabs: []Tab{
			{Label: "Install", Rows: []PkgRow{
				{Name: "figlet", New: "2.2.5_6", Size: "92 KiB"},
			}},
			{Label: "Remove", Rows: []PkgRow{
				{Name: "nano", Old: "9.2_1", Size: "3 MiB"},
			}},
		},
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit",
		},
	}
}

// goldenDowngrade is the two-version history shape: a Downgrade group must
// keep the recorded old version, exactly like Upgrade.
func goldenDowngrade() *SummaryModel {
	return &SummaryModel{
		Title:   "xbpretty History",
		History: true,
		Tabs: []Tab{{Label: "Downgrade", Rows: []PkgRow{
			{Name: "curl", Old: "8.17.0_1", New: "8.16.0_1", Size: "2 MiB", NewSpans: versionSpans("8.17.0_1", "8.16.0_1")},
			{Name: "wget", Old: "1.24.5_1", New: "1.24.4_1", Size: "1 MiB", NewSpans: versionSpans("1.24.5_1", "1.24.4_1")},
		}}},
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit",
		},
	}
}

func goldenReInstall() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Undo",
		Tabs: []Tab{{Label: "ReInstall", Rows: []PkgRow{
			{Name: "zsh", New: "5.9_3", Size: "2 MiB"},
			{Name: "bash", New: "5.2_2", Size: "1 MiB"},
		}}},
		Sel:       1,
		Download:  "2 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "4 MiB",
		Help:      []string{"(s) package info | (q) quit | (y) start undo"},
	}
}

func goldenRealistic60x18() *SummaryModel {
	return &SummaryModel{
		Title: "xbpretty Upgrade",
		Tabs: []Tab{{Label: "Upgrade", Rows: []PkgRow{
			{Name: "python3-requests", Old: "2.32.3_1", New: "2.32.4_1", Size: "412 KiB", NewSpans: versionSpans("2.32.3_1", "2.32.4_1")},
			{Name: "libglib-2.0-devel", Old: "2.82.1_1", New: "2.82.2_1", Size: "1.4 MiB", NewSpans: versionSpans("2.82.1_1", "2.82.2_1")},
			{Name: "python3-cryptography", Old: "43.0.1_1", New: "43.0.3_1", Size: "1.1 MiB", NewSpans: versionSpans("43.0.1_1", "43.0.3_1")},
		}}},
		Download:  "2.9 MiB",
		DiskLabel: "Disk space required:",
		Disk:      "11.0 MiB",
		Help: []string{
			"(↑) move up | (↓) move down",
			"(→) next tab | (←) previous tab",
			"(s) package info | (q) quit | (y) start upgrade",
		},
	}
}

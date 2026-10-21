package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ANSI Color Constants matching Nala exactly
const (
	reset    = "\x1b[0m"
	bold     = "\x1b[1m"
	dim      = "\x1b[2m"
	red      = "\x1b[1;31m"
	green    = "\x1b[1;32m"
	orange   = "\x1b[38;5;202m" // Nala's signature bright orange/yellow (ANSI 202)
	blue     = "\x1b[1;34m"
	magenta  = "\x1b[1;35m"
	cyan     = "\x1b[1;36m"
	white    = "\x1b[1;37m"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func visibleWidth(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
}

func padRight(s string, w int) string {
	cur := visibleWidth(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

func padLeft(s string, w int) string {
	cur := visibleWidth(s)
	if cur >= w {
		return s
	}
	return strings.Repeat(" ", w-cur) + s
}

type PackageItem struct {
	Name        string
	OldVersion  string
	NewVersion  string
	Size        string
	Action      string // install, upgrade, remove, autoremove, hold
}

// sampleTransaction provides realistic Void Linux package transactions
var sampleTransaction = []PackageItem{
	// Upgrades
	{Name: "linux6.8", OldVersion: "6.8.0_1", NewVersion: "6.8.101_1", Size: "154.2 MiB", Action: "upgrade"},
	{Name: "linux6.8-headers", OldVersion: "6.8.0_1", NewVersion: "6.8.101_1", Size: "12.8 MiB", Action: "upgrade"},
	{Name: "glibc", OldVersion: "2.38_1", NewVersion: "2.38_2", Size: "9.4 MiB", Action: "upgrade"},
	{Name: "curl", OldVersion: "8.1.0_1", NewVersion: "8.2.1_1", Size: "1.2 MiB", Action: "upgrade"},
	{Name: "firefox", OldVersion: "124.0.1_1", NewVersion: "125.0.2_1", Size: "65.4 MiB", Action: "upgrade"},

	// Installs
	{Name: "ripgrep", NewVersion: "14.1.0_1", Size: "2.1 MiB", Action: "install"},
	{Name: "fd", NewVersion: "9.0.0_1", Size: "1.4 MiB", Action: "install"},
	{Name: "bat", NewVersion: "0.24.0_2", Size: "3.2 MiB", Action: "install"},

	// Auto-Removals / Removals
	{Name: "linux6.6", OldVersion: "6.6.21_1", Size: "148.0 MiB", Action: "autoremove"},
	{Name: "linux6.6-headers", OldVersion: "6.6.21_1", Size: "11.9 MiB", Action: "autoremove"},
	{Name: "old-lib-compat", OldVersion: "1.0.0_1", Size: "450 KiB", Action: "remove"},

	// Held packages
	{Name: "xbps", OldVersion: "0.60.7_1", NewVersion: "0.61.0_1", Size: "1.8 MiB", Action: "hold"},
}

// versionDiff implements Nala's version diffing algorithm adapted for XBPS (handles '_' revision)
func versionDiff(oldVer, newVer string, useColor bool) string {
	if oldVer == "" || !useColor {
		return newVer
	}

	// 1. Check revision change only (e.g. 2.38_1 -> 2.38_2)
	oldRevParts := strings.Split(oldVer, "_")
	newRevParts := strings.Split(newVer, "_")
	if len(oldRevParts) == 2 && len(newRevParts) == 2 && oldRevParts[0] == newRevParts[0] {
		return fmt.Sprintf("%s_%s%s%s", newRevParts[0], orange, newRevParts[1], reset)
	}

	// 2. Upstream version comparison by '.'
	oldDots := strings.Split(oldRevParts[0], ".")
	newDots := strings.Split(newRevParts[0], ".")

	startColor := len(newDots)
	for i := 0; i < len(oldDots) && i < len(newDots); i++ {
		if oldDots[i] != newDots[i] {
			startColor = i
			break
		}
	}

	var coloredParts []string
	for i, part := range newDots {
		if i >= startColor {
			coloredParts = append(coloredParts, orange+part+reset)
		} else {
			coloredParts = append(coloredParts, part)
		}
	}

	res := strings.Join(coloredParts, ".")
	if len(newRevParts) == 2 {
		if startColor < len(newDots) {
			// Upstream changed, revision is also highlighted
			res += "_" + orange + newRevParts[1] + reset
		} else {
			res += "_" + newRevParts[1]
		}
	}
	return res
}

func getTerminalWidth() int {
	return 80 // Default standard width
}

// Variant A: Exact Classic Nala Style (Double-rule ASCII '====')
func renderVariantA() {
	w := getTerminalWidth()
	rule := strings.Repeat("=", w)

	fmt.Println(bold + "=== VARIANT A: Classic Nala (Exact Screenshot & Python Match) ===" + reset)
	fmt.Println()

	// Upgrades: 4 columns
	fmt.Println(rule)
	fmt.Println(" " + bold + "Upgrading" + reset)
	fmt.Println(rule)
	fmt.Println("  " + padRight(bold+"Package:"+reset, 28) +
		padRight(bold+"Old Version:"+reset, 20) +
		padRight(bold+"New Version:"+reset, 20) +
		padLeft(bold+"Size:"+reset, 10))

	for _, item := range sampleTransaction {
		if item.Action == "upgrade" {
			name := blue + item.Name + reset
			vDiff := versionDiff(item.OldVersion, item.NewVersion, true)
			fmt.Println("  " + padRight(name, 28) +
				padRight(item.OldVersion, 20) +
				padRight(vDiff, 20) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Installs: 3 columns
	fmt.Println(rule)
	fmt.Println(" " + bold + "Installing" + reset)
	fmt.Println(rule)
	fmt.Println("  " + padRight(bold+"Package:"+reset, 38) +
		padRight(bold+"Version:"+reset, 30) +
		padLeft(bold+"Size:"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "install" {
			name := green + item.Name + reset
			fmt.Println("  " + padRight(name, 38) +
				padRight(item.NewVersion, 30) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Auto-Removals / Removals: 3 columns
	fmt.Println(rule)
	fmt.Println(" " + bold + "Auto-Removing" + reset)
	fmt.Println(rule)
	fmt.Println("  " + padRight(bold+"Package:"+reset, 38) +
		padRight(bold+"Version:"+reset, 30) +
		padLeft(bold+"Size:"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "autoremove" || item.Action == "remove" {
			name := red + item.Name + reset
			fmt.Println("  " + padRight(name, 38) +
				padRight(item.OldVersion, 30) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Kept Back: 4 columns
	fmt.Println(rule)
	fmt.Println(" " + bold + "Kept Back, Will Not Be Upgraded" + reset)
	fmt.Println(rule)
	fmt.Println("  " + padRight(bold+"Package:"+reset, 28) +
		padRight(bold+"Old Version:"+reset, 20) +
		padRight(bold+"New Version:"+reset, 20) +
		padLeft(bold+"Size:"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "hold" {
			name := orange + item.Name + reset
			vDiff := versionDiff(item.OldVersion, item.NewVersion, true)
			fmt.Println("  " + padRight(name, 28) +
				padRight(item.OldVersion, 20) +
				padRight(vDiff, 20) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Summary Block
	fmt.Println(rule)
	fmt.Println(" " + bold + "Summary" + reset)
	fmt.Println(rule)
	fmt.Printf("  %-12s %3d Packages\n", "Install", 3)
	fmt.Printf("  %-12s %3d Packages\n", "Upgrade", 5)
	fmt.Printf("  %-12s %3d Packages\n", "Auto-Remove", 3)
	fmt.Printf("  %-12s %3d Packages\n", "Kept Back", 1)
	fmt.Println()
	fmt.Println("  Total download size:   246.1 MiB")
	fmt.Println("  Disk space required:    85.4 MiB")
	fmt.Println()
	fmt.Print(bold + "Do you want to continue? [Y/n] " + reset)
	fmt.Println()
}

// Variant B: Modern Unicode Double-Box (════ with inline section headers & pill summary)
func renderVariantB() {
	w := getTerminalWidth()
	fmt.Println(bold + "=== VARIANT B: Modern Unicode Double-Box (Inline Pill Headers) ===" + reset)
	fmt.Println()

	printHeader := func(title, colorCode string) {
		titleText := " " + colorCode + title + reset + " "
		d := w - utf8.RuneCountInString(title) - 6
		if d < 4 {
			d = 4
		}
		fmt.Println("══" + titleText + strings.Repeat("═", d))
	}

	// Upgrades
	printHeader("Upgrading", blue)
	fmt.Println("  " + padRight(bold+"Package"+reset, 28) +
		padRight(bold+"Current"+reset, 20) +
		padRight(bold+"New Version"+reset, 20) +
		padLeft(bold+"Download"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "upgrade" {
			name := blue + item.Name + reset
			vDiff := versionDiff(item.OldVersion, item.NewVersion, true)
			fmt.Println("  " + padRight(name, 28) +
				padRight(dim+item.OldVersion+reset, 20) +
				padRight(vDiff, 20) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Installs
	printHeader("Installing", green)
	fmt.Println("  " + padRight(bold+"Package"+reset, 38) +
		padRight(bold+"Version"+reset, 30) +
		padLeft(bold+"Download"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "install" {
			name := green + item.Name + reset
			fmt.Println("  " + padRight(name, 38) +
				padRight(item.NewVersion, 30) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Removals
	printHeader("Auto-Removing", red)
	fmt.Println("  " + padRight(bold+"Package"+reset, 38) +
		padRight(bold+"Version"+reset, 30) +
		padLeft(bold+"Freed Size"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "autoremove" || item.Action == "remove" {
			name := red + item.Name + reset
			fmt.Println("  " + padRight(name, 38) +
				padRight(item.OldVersion, 30) +
				padLeft(item.Size, 10))
		}
	}
	fmt.Println()

	// Kept Back
	printHeader("Kept Back (Hold)", orange)
	fmt.Println("  " + padRight(bold+"Package"+reset, 28) +
		padRight(bold+"Current"+reset, 20) +
		padRight(bold+"Available"+reset, 20) +
		padLeft(bold+"Status"+reset, 10))
	for _, item := range sampleTransaction {
		if item.Action == "hold" {
			name := orange + item.Name + reset
			vDiff := versionDiff(item.OldVersion, item.NewVersion, true)
			fmt.Println("  " + padRight(name, 28) +
				padRight(item.OldVersion, 20) +
				padRight(vDiff, 20) +
				padLeft(dim+"pinned"+reset, 10))
		}
	}
	fmt.Println()

	// Framed Modern Card for Summary
	fmt.Println("┌─ " + bold + "Transaction Overview" + reset + " " + strings.Repeat("─", w-25) + "┐")
	inner1 := fmt.Sprintf("  %s3 to install%s  •  %s5 to upgrade%s  •  %s3 to remove%s  •  %s1 kept back%s",
		green, reset, blue, reset, red, reset, orange, reset)
	fmt.Println("│" + padRight(inner1, w-2) + "│")

	inner2 := fmt.Sprintf("  Net download: %s246.1 MiB%s  •  Disk space impact: %s+85.4 MiB%s",
		bold+green, reset, bold+orange, reset)
	fmt.Println("│" + padRight(inner2, w-2) + "│")
	fmt.Println("└" + strings.Repeat("─", w-2) + "┘")
	fmt.Print(bold + "Proceed with transaction? [Y/n]: " + reset)
	fmt.Println()
}

// Variant C: Compact Rounded Border Grid (╭── ╮ with unified arrow diffs)
func renderVariantC() {
	w := getTerminalWidth()
	fmt.Println(bold + "=== VARIANT C: Rounded Box Grid (Compact Unified Arrow) ===" + reset)
	fmt.Println()

	renderBox := func(title, colorCode string, headers []string, colWidths []int, rows [][]string) {
		top := "╭── " + colorCode + title + reset + " " + strings.Repeat("─", w-utf8.RuneCountInString(title)-7) + "╮"
		bottom := "╰" + strings.Repeat("─", w-2) + "╯"
		fmt.Println(top)

		// Header
		hLine := "│  " + padRight(bold+headers[0]+reset, colWidths[0]) +
			padRight(bold+headers[1]+reset, colWidths[1]) +
			padLeft(bold+headers[2]+reset, colWidths[2])
		fmt.Println(padRight(hLine, w-1) + "│")
		fmt.Println("├" + strings.Repeat("─", w-2) + "┤")

		for _, r := range rows {
			rowLine := "│  " + padRight(r[0], colWidths[0]) +
				padRight(r[1], colWidths[1]) +
				padLeft(r[2], colWidths[2])
			fmt.Println(padRight(rowLine, w-1) + "│")
		}
		fmt.Println(bottom)
		fmt.Println()
	}

	// Upgrade Box
	var upRows [][]string
	for _, item := range sampleTransaction {
		if item.Action == "upgrade" {
			name := blue + item.Name + reset
			vDiff := dim + item.OldVersion + reset + " → " + versionDiff(item.OldVersion, item.NewVersion, true)
			upRows = append(upRows, []string{name, vDiff, item.Size})
		}
	}
	renderBox("Packages To Upgrade (5)", blue, []string{"Package", "Version Delta", "Size"}, []int{24, 38, 12}, upRows)

	// Install Box
	var inRows [][]string
	for _, item := range sampleTransaction {
		if item.Action == "install" {
			name := green + item.Name + reset
			inRows = append(inRows, []string{name, item.NewVersion, item.Size})
		}
	}
	renderBox("New Packages To Install (3)", green, []string{"Package", "Version", "Size"}, []int{34, 28, 12}, inRows)

	// Summary Box
	fmt.Println("╭── " + bold + "Summary" + reset + " " + strings.Repeat("─", w-13) + "╮")
	fmt.Println("│  Changes:  3 install, 5 upgrade, 3 remove, 1 hold                    │")
	fmt.Println("│  Transfer: 246.1 MiB download  •  Disk: +85.4 MiB space required     │")
	fmt.Println("╰" + strings.Repeat("─", w-2) + "╯")
	fmt.Print(bold + "Apply transaction? [Y/n] " + reset)
	fmt.Println()
}

func main() {
	variant := flag.String("variant", "all", "UI Variant to preview: A, B, C, or all")
	flag.Parse()

	switch strings.ToUpper(*variant) {
	case "A":
		renderVariantA()
	case "B":
		renderVariantB()
	case "C":
		renderVariantC()
	case "ALL":
		renderVariantA()
		fmt.Println("\n" + strings.Repeat("-", 80) + "\n")
		renderVariantB()
		fmt.Println("\n" + strings.Repeat("-", 80) + "\n")
		renderVariantC()
	default:
		fmt.Fprintf(os.Stderr, "Unknown variant: %s. Use A, B, C, or all.\n", *variant)
		os.Exit(1)
	}
}

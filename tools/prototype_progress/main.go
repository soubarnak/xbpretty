package main

import (
	"flag"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ANSI Color Constants matching Nala exactly
const (
	reset      = "\x1b[0m"
	bold       = "\x1b[1m"
	dim        = "\x1b[2m"
	red        = "\x1b[1;31m"
	green      = "\x1b[1;32m"
	orange     = "\x1b[38;5;202m" // Nala ANSI 202
	blue       = "\x1b[1;34m"
	cyan       = "\x1b[1;36m"
	white      = "\x1b[1;37m"
	clearLine  = "\x1b[2K\r"
	cursorUp   = "\x1b[%dA"
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

func progressBar(percent float64, width int) string {
	if width < 5 {
		width = 20
	}
	filledCount := int(float64(width) * (percent / 100.0))
	if filledCount > width {
		filledCount = width
	}
	if filledCount < 0 {
		filledCount = 0
	}
	unfilledCount := width - filledCount

	// Nala signature: filled=cyan, unfilled=red background/track
	barFilled := cyan + strings.Repeat("━", filledCount) + reset
	barUnfilled := "\x1b[31m" + strings.Repeat("─", unfilledCount) + reset
	return barFilled + barUnfilled
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type BoxStyle struct {
	TopLeft, TopRight, BottomLeft, BottomRight, Horizontal, Vertical string
}

var (
	StyleRounded = BoxStyle{"╭", "╮", "╰", "╯", "─", "│"}
	StyleClassic = BoxStyle{"┌", "┐", "└", "┘", "─", "│"}
)

func runSimulation(boxStyle BoxStyle, layout string, delay time.Duration) {
	w := 80
	horiz := boxStyle.Horizontal

	// Packages to simulate
	pkgs := []struct {
		name string
		size float64 // MiB
	}{
		{"glibc-2.38_2.x86_64.xbps", 9.4},
		{"curl-8.2.1_1.x86_64.xbps", 1.2},
		{"ripgrep-14.1.0_1.x86_64.xbps", 2.1},
		{"linux6.8-headers-6.8.101_1.x86_64.xbps", 12.8},
		{"linux6.8-6.8.101_1.x86_64.xbps", 154.2},
	}

	totalBytes := 179.7
	totalCount := len(pkgs)

	// Step 1: Downloading Phase
	title := " Downloading... "
	topBorder := green + boxStyle.TopLeft + strings.Repeat(horiz, 2) + reset + bold + title + reset + green + strings.Repeat(horiz, w-visibleWidth(title)-3) + boxStyle.TopRight + reset
	bottomBorder := green + boxStyle.BottomLeft + strings.Repeat(horiz, w-2) + boxStyle.BottomRight + reset

	// Initial render
	fmt.Println(topBorder)
	fmt.Printf("%s%s%s%s\n", green+boxStyle.Vertical+reset, padRight("  "+green+"Total Packages:"+reset+" 0/"+fmt.Sprint(totalCount), w-2), green+boxStyle.Vertical+reset, "")
	fmt.Printf("%s%s%s%s\n", green+boxStyle.Vertical+reset, padRight("  "+green+"Last Completed:"+reset+" starting...", w-2), green+boxStyle.Vertical+reset, "")
	fmt.Printf("%s%s%s%s\n", green+boxStyle.Vertical+reset, padRight("  Time Remaining: -:--:--  ["+progressBar(0, 20)+"] 0.0% • 0.0/"+fmt.Sprintf("%.1f MiB", totalBytes)+" • 0.0 MB/s", w-2), green+boxStyle.Vertical+reset, "")
	fmt.Println(bottomBorder)

	downloadedBytes := 0.0
	for i, pkg := range pkgs {
		steps := 8
		for s := 1; s <= steps; s++ {
			time.Sleep(delay)
			currentDownloaded := downloadedBytes + (pkg.size * float64(s) / float64(steps))
			pct := (currentDownloaded / totalBytes) * 100.0
			speed := 4.2 + float64(s%3)*0.5
			remainSecs := int((totalBytes - currentDownloaded) / speed)
			timeRem := fmt.Sprintf("%02d:%02d", remainSecs/60, remainSecs%60)

			bar := progressBar(pct, 20)
			line1 := "  " + green + "Total Packages:" + reset + fmt.Sprintf(" %d/%d", i, totalCount)
			line2 := "  " + green + "Downloading:" + reset + " " + pkg.name
			line3 := fmt.Sprintf("  Time Remaining: %s  [%s] %s%.1f%%%s • %s%.1f/%.1f MiB%s • %s%.1f MB/s%s",
				timeRem, bar, bold+blue, pct, reset, bold+green, currentDownloaded, totalBytes, reset, bold+green, speed, reset)

			fmt.Print(fmt.Sprintf(cursorUp, 4))
			fmt.Printf("%s%s%s\n", green+boxStyle.Vertical+reset, padRight(line1, w-2), green+boxStyle.Vertical+reset)
			fmt.Printf("%s%s%s\n", green+boxStyle.Vertical+reset, padRight(line2, w-2), green+boxStyle.Vertical+reset)
			fmt.Printf("%s%s%s\n", green+boxStyle.Vertical+reset, padRight(line3, w-2), green+boxStyle.Vertical+reset)
			fmt.Println(bottomBorder)
		}
		downloadedBytes += pkg.size
	}

	// Step 2: Unpacking & Configuring Phase
	time.Sleep(delay * 2)
	runTitle := " Running xbps... "
	runTopBorder := blue + boxStyle.TopLeft + strings.Repeat(horiz, 2) + reset + bold + runTitle + reset + blue + strings.Repeat(horiz, w-visibleWidth(runTitle)-3) + boxStyle.TopRight + reset
	runBottomBorder := blue + boxStyle.BottomLeft + strings.Repeat(horiz, w-2) + boxStyle.BottomRight + reset

	fmt.Print(fmt.Sprintf(cursorUp, 5))
	fmt.Println(runTopBorder)

	phases := []string{"Unpacking", "Configuring"}
	pkgNames := []string{"glibc-2.38_2", "curl-8.2.1_1", "ripgrep-14.1.0_1", "linux6.8-headers-6.8.101_1", "linux6.8-6.8.101_1"}

	spinIdx := 0
	totalOps := len(phases) * len(pkgNames)
	currentOp := 0

	for _, phase := range phases {
		for _, pkgName := range pkgNames {
			currentOp++
			pct := (float64(currentOp) / float64(totalOps)) * 100.0

			for tick := 0; tick < 3; tick++ {
				time.Sleep(delay)
				spinner := cyan + spinnerFrames[spinIdx%len(spinnerFrames)] + reset
				spinIdx++
				bar := progressBar(pct, 24)

				var l1, l2, l3 string
				if layout == "compact" {
					l1 = fmt.Sprintf("  %s %s: %s", spinner, bold+phase+reset, bold+pkgName+reset)
					l2 = fmt.Sprintf("  Progress: [%s] %s%.1f%%%s • %d/%d operations", bar, bold+blue, pct, reset, currentOp, totalOps)
					l3 = "  " + dim + "Updating system file links and dependencies..." + reset
				} else { // detailed
					l1 = fmt.Sprintf("  Phase:           %s %s packages", spinner, bold+phase+reset)
					l2 = fmt.Sprintf("  Current Target:  %s", bold+pkgName+reset)
					l3 = fmt.Sprintf("  Status:          [%s] %s%.1f%%%s • %d/%d actions", bar, bold+blue, pct, reset, currentOp, totalOps)
				}

				fmt.Print(fmt.Sprintf(cursorUp, 4))
				fmt.Printf("%s%s%s\n", blue+boxStyle.Vertical+reset, padRight(l1, w-2), blue+boxStyle.Vertical+reset)
				fmt.Printf("%s%s%s\n", blue+boxStyle.Vertical+reset, padRight(l2, w-2), blue+boxStyle.Vertical+reset)
				fmt.Printf("%s%s%s\n", blue+boxStyle.Vertical+reset, padRight(l3, w-2), blue+boxStyle.Vertical+reset)
				fmt.Println(runBottomBorder)
			}
		}
	}

	// Completion
	time.Sleep(delay * 2)
	fmt.Print(fmt.Sprintf(cursorUp, 5))
	for i := 0; i < 5; i++ {
		fmt.Print(clearLine + "\n")
	}
	fmt.Print(fmt.Sprintf(cursorUp, 5))

	fmt.Println(bold + green + "✔ Transaction complete:" + reset + " 1 installed, 4 updated, 0 removed in 4.8s")
}

func main() {
	styleFlag := flag.String("style", "rounded", "Box border style: rounded or classic")
	layoutFlag := flag.String("layout", "detailed", "Execution phase layout: detailed or compact")
	fastFlag := flag.Bool("fast", false, "Fast simulation mode")
	flag.Parse()

	boxStyle := StyleRounded
	if *styleFlag == "classic" {
		boxStyle = StyleClassic
	}

	delay := 60 * time.Millisecond
	if *fastFlag {
		delay = 15 * time.Millisecond
	}

	fmt.Printf(bold+"Running Execution Progress Box Prototype (Style: %s, Layout: %s)"+reset+"\n\n", *styleFlag, *layoutFlag)
	runSimulation(boxStyle, *layoutFlag, delay)
}

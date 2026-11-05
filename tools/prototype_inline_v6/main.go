package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	planMode := flag.String("plan", "live", "plan source: live (real read-only xbps dry runs), golden (#45 §3.7 xbps fixture) or nala (research #39 frame verbatim)")
	outDir := flag.String("out", "", "write raw frames under this directory instead of printing them")
	widthList := flag.String("width", "80,100", "comma-separated widths for the summary frames")
	noColor := flag.Bool("no-color", false, "emit no ANSI at all")
	simple := flag.Bool("simple", false, "render the --simple layout instead of the full summary")
	limit := flag.Int("limit", 2, "max rows per live plan section")
	testdata := flag.String("testdata", "testdata", "testdata root (for the captured hold fixture)")
	check := flag.Bool("check", false, "print the version-diff cases as quoted Go strings and exit")
	flag.Parse()

	if *check {
		for _, c := range [][2]string{
			{"3.5.1_1", "3.5.4_1"},
			{"2.38_1", "2.39_2"},
			{"1.5.7_1", "1.5.7_2"},
			{"1.2.3_1", "1.3.0_1"},
		} {
			fmt.Printf("%-10s -> %-10s  %q\n", c[0], c[1], versionDiff(c[0], c[1], Theme{Color: true}))
		}
		return
	}

	th := Theme{Color: !*noColor}

	var plan Plan
	var err error
	switch *planMode {
	case "golden":
		plan = goldenPlan()
	case "nala":
		plan = nalaPlan()
	case "live":
		plan, err = livePlan(*limit, *testdata)
		if err != nil {
			fmt.Fprintf(os.Stderr, "prototype_inline_v6: live plan unavailable (%v); falling back to the golden fixture\n", err)
			plan = goldenPlan()
		}
	default:
		fail("unknown -plan %q (want live or golden)", *planMode)
	}

	widths := parseWidths(*widthList)
	if *outDir != "" {
		if err := writeFrames(*outDir, plan, widths, th, *simple); err != nil {
			fail("%v", err)
		}
		fmt.Printf("wrote frames for %d widths under %s\n", len(widths), *outDir)
		return
	}
	printFrames(plan, widths, th, *simple)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "prototype_inline_v6: "+format+"\n", args...)
	os.Exit(1)
}

func parseWidths(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 20 {
			fail("bad width %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		out = []int{80}
	}
	return out
}

// frames is every artefact the prototype can emit, keyed by file name.
func frames(plan Plan, widths []int, th Theme, simple bool) map[string][]string {
	out := map[string][]string{}
	if simple {
		out["summary-simple.txt"] = renderSimple(plan, Binary, th)
		return out
	}
	for _, w := range widths {
		render := func() []string {
			if simple {
				return renderSimple(plan, Binary, th)
			}
			return renderSummary(plan, w, true, Binary, th)
		}
		if !simple {
			out[fmt.Sprintf("summary-%d.piped.txt", w)] = renderSummary(plan, w, false, Binary, th)
		}
		out[fmt.Sprintf("summary-%d.txt", w)] = render()
	}
	out["summary-simple.txt"] = renderSimple(plan, Binary, Theme{Color: th.Color})

	dlPlan := syntheticDownloadPlan()
	widgets := []struct {
		name string
		w    Widget
	}{
		{"progress-download", downloadWidget(dlPlan, 3.5)},
		{"progress-install", installWidget(7.5)},
		{"progress-remove", removeWidget(2.5)},
		{"progress-update", updateWidget(7, 12, "https://repo-default.voidlinux.org/current", 3.5)},
		{"progress-score", scoreWidget(7, 12, "https://repo-default.voidlinux.org/current", 3.5)},
	}
	for _, entry := range widgets {
		for _, w := range []int{80, 60} {
			out[fmt.Sprintf("%s-%d.txt", entry.name, w)] = renderWidget(entry.w, w, th)
		}
	}
	snap := applyDownloadEvents(dlPlan, downloadEvents(dlPlan))
	rate := ceilRate(snap.position, 3.5)
	out["plain-download.txt"] = []string{renderPlainLine(PlainLine{
		Ratio:       float64(snap.position) / float64(snap.total),
		Message:     "Last completed: " + snap.lastFile,
		Position:    snap.position,
		Total:       snap.total,
		Rate:        rate,
		HasTransfer: true,
		ItemsDone:   snap.completedPkgs,
		ItemsTotal:  len(dlPlan),
		HasItems:    true,
	})}
	out["plain-installing.txt"] = []string{renderPlainLine(PlainLine{
		Ratio:   7.0 / 16.0,
		Message: "Status: Unpacking glibc-2.41_1",
		Item:    "glibc-2.41_1",
		HasItem: true,
	})}
	out["plain-update.txt"] = []string{renderPlainLine(PlainLine{
		Ratio:       7.0 / 12.0,
		Message:     "Updating: https://repo-default.voidlinux.org/current",
		MirrorsDone: 7, MirrorsTotal: 12, HasMirrors: true,
	})}
	return out
}

// writeFrames writes each artefact twice: raw bytes with ANSI (.sgr) and
// ANSI-stripped text (.txt), so the verdict can quote exact SGR sequences.
func writeFrames(dir string, plan Plan, widths []int, th Theme, simple bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	all := frames(plan, widths, th, simple)
	plain := frames(plan, widths, Theme{Color: false}, simple)
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	for _, name := range names {
		lines := all[name]
		raw := strings.Join(lines, "\n") + "\n"
		if strings.HasSuffix(name, ".txt") {
			// .sgr keeps the exact bytes; .txt is the same frame with ANSI
			// stripped so the layout can be read in a diff.
			sgr := strings.TrimSuffix(name, ".txt") + ".sgr"
			if err := os.WriteFile(filepath.Join(dir, sgr), []byte(raw), 0o644); err != nil {
				return err
			}
			stripped := make([]string, len(lines))
			for i, line := range lines {
				stripped[i] = stripANSI(line)
			}
			raw = strings.Join(stripped, "\n") + "\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o644); err != nil {
			return err
		}
	}
	// The no-colour summary must contain no ESC byte at all (#45 §2.3).
	for _, w := range widths {
		name := fmt.Sprintf("summary-%d.nocolor.txt", w)
		var lines []string
		if simple {
			continue
		}
		lines = renderSummary(plan, w, true, Binary, Theme{Color: false})
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	_ = plain
	return nil
}

func printFrames(plan Plan, widths []int, th Theme, simple bool) {
	all := frames(plan, widths, th, simple)
	order := []string{}
	if simple {
		order = append(order, "summary-simple.txt")
	}
	for _, w := range widths {
		if !simple {
			order = append(order, fmt.Sprintf("summary-%d.txt", w), fmt.Sprintf("summary-%d.piped.txt", w))
		}
	}
	if !simple {
		order = append(order, "summary-simple.txt",
			"progress-download-80.txt", "progress-download-60.txt",
			"progress-install-80.txt", "progress-install-60.txt",
			"progress-remove-80.txt", "progress-remove-60.txt",
			"progress-update-80.txt", "progress-update-60.txt",
			"progress-score-80.txt", "progress-score-60.txt",
			"plain-download.txt", "plain-installing.txt", "plain-update.txt")
	}
	for _, name := range order {
		lines, ok := all[name]
		if !ok {
			continue
		}
		fmt.Printf("----- %s -----\n", name)
		for _, line := range lines {
			fmt.Println(line)
		}
		fmt.Println()
	}
}

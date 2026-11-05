package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/soubarnak/xbpretty/internal/term"
)

func main() {
	surface := flag.String("surface", "summary", "summary | history | fetch | inline | emit")
	colorMode := flag.String("color", "auto", "auto | always | never")
	width := flag.Int("width", 80, "width for -surface emit")
	height := flag.Int("height", 24, "height for -surface emit")
	emitDir := flag.String("emit", "", "write static fixtures under this directory and exit")
	frames := flag.String("frames", os.Getenv("PROTO_FRAME_DIR"), "write each drawn frame here")
	tracePath := flag.String("trace", os.Getenv("PROTO_TRACE"), "append harness events here")
	noDSR := flag.Bool("no-dsr", false, "skip the ESC[6n query and use the no-anchor fallback")
	child := flag.Bool("child", false, "run the fake telemetry child")
	flag.Parse()

	if *child {
		runFakeChild()
		return
	}

	color := true
	switch *colorMode {
	case "never":
		color = false
	case "always":
		color = true
	default:
		if os.Getenv("NO_COLOR") != "" {
			color = false
		}
	}

	trace := NewTrace(*tracePath)
	defer trace.Close()

	if *emitDir != "" {
		if err := writeFixtures(*emitDir); err != nil {
			fmt.Fprintf(os.Stderr, "prototype_tui_v6: %v\n", err)
			os.Exit(2)
		}
		return
	}

	switch *surface {
	case "inline":
		os.Exit(runInline(color, !*noDSR, *frames, trace))
	case "emit":
		b := fixture(*surface, *width, *height, color)
		payload := b.Flush(nil, color)
		_, _ = os.Stdout.Write(payload)
		_ = os.Stdout.Sync()
		return
	case "summary", "history", "fetch":
		os.Exit(runScreen(*surface, color, *frames, trace))
	default:
		fmt.Fprintf(os.Stderr, "prototype_tui_v6: unknown surface %q\n", *surface)
		os.Exit(2)
	}
}

func fixture(_ string, w, h int, color bool) *term.Buffer {
	return renderSummary(goldenUpgrade(), w, h, Theme{Color: color})
}

// writeFixtures emits the static frames committed under testdata/prototype/tui.
func writeFixtures(dir string) error {
	type fx struct {
		name string
		b    *term.Buffer
	}
	fixtures := []fx{
		{"summary-80x24", renderSummary(goldenUpgrade(), 80, 24, Theme{Color: true})},
		{"summary-100x30", renderSummary(goldenUpgrade(), 100, 30, Theme{Color: true})},
		{"summary-60x18", renderSummary(fiveOpUndo(), 60, 18, Theme{Color: true})},
		{"summary-140x40", renderSummary(goldenUpgrade(), 140, 40, Theme{Color: true})},
		{"summary-install-80x24", renderSummary(goldenInstall(), 80, 24, Theme{Color: true})},
		{"history-80x24", renderSummary(goldenHistory(), 80, 24, Theme{Color: true})},
		{"fetch-80x24", renderFetch(&FetchModel{Items: goldenMirrors()}, 80, 24, Theme{Color: true})},
		{"fetch-120x30", renderFetch(&FetchModel{Items: goldenMirrors()}, 120, 30, Theme{Color: true})},
		{"fetch-60x18", renderFetch(&FetchModel{Items: goldenMirrors()}, 60, 18, Theme{Color: true})},
		{"toolarge-50x15", renderSummary(goldenUpgrade(), 50, 15, Theme{Color: true})},
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range fixtures {
		payload := f.b.Flush(nil, true)
		if err := os.WriteFile(filepath.Join(dir, f.name+".txt"), []byte(f.b.Text()), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.name+".sgr"), payload, 0o644); err != nil {
			return err
		}
	}
	// --color never: every glyph present, no SGR bytes.
	never := renderSummary(goldenUpgrade(), 80, 24, Theme{Color: false})
	if err := os.WriteFile(filepath.Join(dir, "summary-80x24.nocolor.txt"), []byte(never.Text()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "summary-80x24.nocolor.sgr"), never.Flush(nil, false), 0o644); err != nil {
		return err
	}
	neverFetch := renderFetch(&FetchModel{Items: goldenMirrors()}, 80, 24, Theme{Color: false})
	if err := os.WriteFile(filepath.Join(dir, "fetch-80x24.nocolor.txt"), []byte(neverFetch.Text()), 0o644); err != nil {
		return err
	}
	return nil
}

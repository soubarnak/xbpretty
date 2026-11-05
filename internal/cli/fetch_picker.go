package cli

import (
	"fmt"
	"strconv"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/fetch"
	"github.com/soubarnak/xbpretty/internal/term"
	"github.com/soubarnak/xbpretty/internal/tui"
)

// fetchPickerUnavailable is runFetchPicker's "no screen" code: the caller
// falls back to the non-TTY shortlist without treating it as a cancellation.
const fetchPickerUnavailable = -1

// fetchRows converts the ranked probe results into the picker's display rows.
// The conversion is the only place the ms and binary-rate rules are applied,
// so the widget stays a pure layout component.
func fetchRows(ranked []fetch.Result) []tui.FetchRow {
	rows := make([]tui.FetchRow, 0, len(ranked))
	for _, r := range ranked {
		rows = append(rows, tui.FetchRow{
			URL:      r.URL,
			Location: r.Location,
			Region:   r.Region,
			Tier:     r.Tier,
			Latency:  strconv.Itoa(r.LatencyMS) + " ms",
			Speed:    fetch.FormatSpeed(r.Speed),
			Score:    strconv.Itoa(r.ScoreMS),
		})
	}
	return rows
}

// runFetchPicker opens S3 and owns its key loop. It returns the selected
// index and ok=true on Enter/q/Q; a cancel is ok=false with code 0, a signal
// is ok=false with the 128+signal code, and an unavailable screen is code -1.
// The picker writes nothing: the caller validates and writes the one choice.
func (st *state) runFetchPicker(ranked []fetch.Result) (index int, ok bool, code int) {
	out, outOK := asFile(st.stdout)
	in, inOK := asFile(st.stdin)
	if !outOK || !inOK {
		return 0, false, fetchPickerUnavailable
	}
	// FR-067/FR-071: below 60x18 at entry the text path runs with no warning.
	if w, h, err := term.Size(out); err != nil || w < 60 || h < 18 {
		return 0, false, fetchPickerUnavailable
	}
	scr, err := openScreenFn(in, out, st.color, false)
	if err != nil {
		fmt.Fprintf(st.stderr, "xbpretty: full-screen interface unavailable (%v); showing the text summary\n", err)
		return 0, false, fetchPickerUnavailable
	}
	defer scr.Close()
	// FR-074/AC-054: a panic restores the terminal before it is re-raised, and
	// the one warning line names the failed surface.
	defer func() {
		if r := recover(); r != nil {
			scr.Close()
			fmt.Fprintf(st.stderr, "xbpretty: full-screen interface failed (%v)\n", r)
			panic(r)
		}
	}()

	model := &tui.FetchModel{Rows: fetchRows(ranked)}
	draw := func(w, h int) error {
		buf := tui.RenderFetch(model, w, h, screenTheme(st.r.cfg.Color.Theme))
		if screenDumpFn != nil {
			screenDumpFn(buf)
		}
		return drawScreenFn(scr, buf)
	}
	fail := func(err error) (int, bool, int) {
		// Restore before warning so the line survives the alternate screen.
		scr.Close()
		fmt.Fprintf(st.stderr, "xbpretty: full-screen interface failed (%v); showing the text summary\n", err)
		return 0, false, fetchPickerUnavailable
	}

	w, h := scr.Size()
	small := w < 60 || h < 18
	// Mouse reporting stays off: FR-070 ignores mouse input entirely.
	if err := draw(w, h); err != nil {
		return fail(err)
	}
	if screenPanic() {
		panic("injected full-screen panic")
	}
	for {
		ev := <-scr.Events()
		switch ev.Kind {
		case tui.EventResize:
			w, h = ev.Width, ev.Height
			small = w < 60 || h < 18
			if err := draw(w, h); err != nil {
				return fail(err)
			}
		case tui.EventSignal:
			scr.Close()
			if sig, isSignal := ev.Signal.(syscall.Signal); isSignal {
				return 0, false, 128 + int(sig)
			}
			return 0, false, exitRuntime
		case tui.EventEOF:
			return 0, false, 0
		case tui.EventKey:
			if small {
				// FR-071: below the floor only cancellation works; no
				// cursor, navigation or confirmation.
				if ev.Key.Type == term.KeyEsc || ev.Key.Type == term.KeyCtrlC {
					return 0, false, 0
				}
				continue
			}
			before := model.Sel
			switch ev.Key.Type {
			case term.KeyRune:
				switch ev.Key.Rune {
				case 'j':
					model.Move(1)
				case 'k':
					model.Move(-1)
				case 'g':
					model.SetSelection(0)
				case 'G':
					model.SetSelection(len(model.Rows) - 1)
				case 'q', 'Q':
					return model.Sel, true, 0
				}
			case term.KeyDown:
				model.Move(1)
			case term.KeyUp:
				model.Move(-1)
			case term.KeyHome:
				model.SetSelection(0)
			case term.KeyEnd:
				model.SetSelection(len(model.Rows) - 1)
			case term.KeyEnter:
				return model.Sel, true, 0
			case term.KeyEsc, term.KeyCtrlC:
				return 0, false, 0
			}
			if model.Sel == before {
				continue
			}
			if err := draw(w, h); err != nil {
				return fail(err)
			}
		}
	}
}

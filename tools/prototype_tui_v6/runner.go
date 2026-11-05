package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/soubarnak/xbpretty/internal/term"
)

// Trace is the harness-visible event log. The PTY test asserts on it because
// screen bytes alone cannot prove which key was decoded.
type Trace struct {
	mu sync.Mutex
	f  *os.File
}

// NewTrace opens the trace file ("" means discard).
func NewTrace(path string) *Trace {
	t := &Trace{}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			t.f = f
		}
	}
	return t
}

// Log writes one trace line.
func (t *Trace) Log(format string, args ...any) {
	if t == nil || t.f == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.f, format+"\n", args...)
}

// Close flushes the trace file.
func (t *Trace) Close() {
	if t != nil && t.f != nil {
		_ = t.f.Close()
	}
}

var frameSeq int

// dumpFrame writes the cell grid and the exact SGR bytes for one draw.
func dumpFrame(dir string, surface string, b *term.Buffer, payload []byte, color bool) {
	if dir == "" {
		return
	}
	frameSeq++
	tag := fmt.Sprintf("%03d-%s-%dx%d", frameSeq, surface, b.W, b.H)
	_ = os.MkdirAll(dir, 0o755)
	name := tag + ".txt"
	if !color {
		name = tag + ".nocolor.txt"
	}
	_ = os.WriteFile(filepath.Join(dir, name), []byte(b.Text()), 0o644)
	if payload != nil {
		_ = os.WriteFile(filepath.Join(dir, strings.TrimSuffix(name, ".txt")+".sgr"), payload, 0o644)
	}
}

func appendSequence(dir, line string) {
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "inline-sequence.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// runScreen is the full-screen surface runner (#50 §8.7): guard, signals,
// SIGWINCH, key loop, restore on every path.
func runScreen(surface string, color bool, frameDir string, trace *Trace) (code int) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		fmt.Fprintln(os.Stderr, "xbpretty: full-screen interface unavailable (TERM=dumb); showing the text summary")
		trace.Log("SCREEN dumb-terminal exit=0")
		return 0
	}
	w, h, err := term.Size(os.Stdout)
	if err != nil {
		w, h = 80, 24
	}
	if w < 60 || h < 18 {
		fmt.Fprintf(os.Stderr, "xbpretty: terminal too small (%dx%d; 60x18 minimum); showing the text summary\n", w, h)
		trace.Log("SCREEN entry-too-small %dx%d exit=0", w, h)
		return 0
	}
	mouse := surface != "fetch"
	guard := term.NewGuard(os.Stdin, os.Stdout)
	if err := guard.EnterFullscreen(mouse); err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: full-screen interface unavailable (%v); showing the text summary\n", err)
		trace.Log("SCREEN init-failed %v", err)
		return 1
	}
	defer guard.Restore()
	defer func() {
		if r := recover(); r != nil {
			guard.Restore()
			trace.Log("SCREEN panic=%v restored=true", r)
			fmt.Fprintf(os.Stderr, "xbpretty: full-screen interface failed (%v)\n", r)
			panic(r)
		}
	}()

	summary := goldenUpgrade()
	fetch := &FetchModel{Items: goldenMirrors()}
	tabs := summary.Tabs
	if surface == "history" {
		tabs = goldenHistory().Tabs
	}
	active, sel := 0, 0
	prev := (*term.Buffer)(nil)

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	keyCh := make(chan term.Key, 8)
	dec := term.NewDecoder(os.Stdin)
	go func() {
		for {
			k, err := dec.ReadKey()
			if err != nil {
				close(keyCh)
				return
			}
			keyCh <- k
		}
	}()

	var panicTimer <-chan time.Time
	if d := panicAfter(); d > 0 {
		panicTimer = time.After(d)
	}

	draw := func() {
		var b *term.Buffer
		switch surface {
		case "summary":
			summary.Tabs, summary.Active, summary.Sel = tabs, active, sel
			b = renderSummary(summary, w, h, Theme{Color: color})
		case "history":
			hist := goldenHistory()
			hist.Tabs = tabs
			hist.Active, hist.Sel = active, sel
			b = renderSummary(hist, w, h, Theme{Color: color})
		case "fetch":
			b = renderFetch(fetch, w, h, Theme{Color: color})
		}
		payload := b.Flush(prev, color)
		_, _ = os.Stdout.Write(payload)
		dumpFrame(frameDir, surface, b, payload, color)
		trace.Log("SCREEN draw surface=%s %dx%d", surface, w, h)
		prev = b
	}

	trace.Log("SCREEN enter surface=%s %dx%d", surface, w, h)
	draw()
	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGWINCH:
				nw, nh, err := term.Size(os.Stdout)
				if err == nil {
					w, h = nw, nh
				}
				trace.Log("SCREEN resize %dx%d", w, h)
				prev = nil
				draw()
				continue
			case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
				guard.Restore()
				n := 128 + int(sig.(syscall.Signal))
				trace.Log("SCREEN signal=%v exit=%d restored=true", sig, n)
				return n
			}
		case k, ok := <-keyCh:
			if !ok {
				trace.Log("SCREEN eof")
				guard.Restore()
				if surface == "history" {
					return 0
				}
				return 1
			}
			trace.Log("KEY %s", k.String())
			switch surface {
			case "fetch":
				switch k.Type {
				case term.KeyRune:
					switch k.Rune {
					case 'j':
						fetch.move(1)
					case 'k':
						fetch.move(-1)
					case 'g':
						fetch.Sel = 0
					case 'G':
						fetch.Sel = len(fetch.Items) - 1
					case 'q', 'Q':
						return confirmFetch(guard, trace)
					}
				case term.KeyDown:
					fetch.move(1)
				case term.KeyUp:
					fetch.move(-1)
				case term.KeyHome:
					fetch.Sel = 0
				case term.KeyEnd:
					fetch.Sel = len(fetch.Items) - 1
				case term.KeyEnter:
					return confirmFetch(guard, trace)
				case term.KeyEsc, term.KeyCtrlC:
					return cancelFetch(guard, trace)
				}
			case "summary":
				switch k.Type {
				case term.KeyRune:
					switch k.Rune {
					case 'j':
						sel = clamp(sel+1, 0, len(tabs[active].Rows)-1)
					case 'k':
						sel = clamp(sel-1, 0, len(tabs[active].Rows)-1)
					case 'h':
						active = clamp(active-1, 0, len(tabs)-1)
						sel = 0
					case 'l':
						active = clamp(active+1, 0, len(tabs)-1)
						sel = 0
					case 's':
						trace.Log("SCREEN package-info requested")
					case 'y', 'Y':
						guard.Restore()
						trace.Log("SCREEN confirm exit=0")
						return 0
					case 'q', 'Q', 'n', 'N':
						return refuse(guard, trace)
					}
				case term.KeyDown:
					sel = clamp(sel+1, 0, len(tabs[active].Rows)-1)
				case term.KeyUp:
					sel = clamp(sel-1, 0, len(tabs[active].Rows)-1)
				case term.KeyLeft:
					active = clamp(active-1, 0, len(tabs)-1)
					sel = 0
				case term.KeyRight:
					active = clamp(active+1, 0, len(tabs)-1)
					sel = 0
				case term.KeyHome:
					sel = 0
				case term.KeyEnd:
					sel = len(tabs[active].Rows) - 1
				case term.KeyPgUp:
					sel = clamp(sel-10, 0, len(tabs[active].Rows)-1)
				case term.KeyPgDn:
					sel = clamp(sel+10, 0, len(tabs[active].Rows)-1)
				case term.KeyWheelUp:
					sel = clamp(sel-1, 0, len(tabs[active].Rows)-1)
				case term.KeyWheelDown:
					sel = clamp(sel+1, 0, len(tabs[active].Rows)-1)
				case term.KeyEsc, term.KeyCtrlC:
					return refuse(guard, trace)
				}
			case "history":
				switch k.Type {
				case term.KeyRune:
					switch k.Rune {
					case 'j':
						sel = clamp(sel+1, 0, len(tabs[active].Rows)-1)
					case 'k':
						sel = clamp(sel-1, 0, len(tabs[active].Rows)-1)
					case 'h':
						active = clamp(active-1, 0, len(tabs)-1)
						sel = 0
					case 'l':
						active = clamp(active+1, 0, len(tabs)-1)
						sel = 0
					case 'q', 'Q':
						guard.Restore()
						trace.Log("SCREEN history-quit exit=0")
						return 0
					}
				case term.KeyEsc, term.KeyCtrlC:
					guard.Restore()
					trace.Log("SCREEN history-quit exit=0")
					return 0
				case term.KeyDown:
					sel = clamp(sel+1, 0, len(tabs[active].Rows)-1)
				case term.KeyUp:
					sel = clamp(sel-1, 0, len(tabs[active].Rows)-1)
				}
			}
			_ = k
			draw()
		case <-panicTimer:
			panic("prototype panic injection")
		}
	}
}

func panicAfter() time.Duration {
	v := os.Getenv("PROTO_PANIC_AFTER")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func refuse(guard *term.Guard, trace *Trace) int {
	guard.Restore()
	trace.Log("SCREEN refuse exit=1")
	fmt.Fprintln(os.Stderr, "xbpretty: user refused confirmation")
	return 1
}

func cancelCode(surface string, trace *Trace) int {
	if surface == "history" {
		return 0
	}
	return 1
}

func confirmFetch(guard *term.Guard, trace *Trace) int {
	guard.Restore()
	trace.Log("SCREEN fetch-confirm exit=0")
	fmt.Fprintln(os.Stderr, "Sources have been written to /etc/xbps.d/00-repository-main.conf")
	return 0
}

func cancelFetch(guard *term.Guard, trace *Trace) int {
	guard.Restore()
	trace.Log("SCREEN fetch-cancel exit=1")
	fmt.Fprintln(os.Stderr, "xbpretty: no mirror was selected")
	return 1
}

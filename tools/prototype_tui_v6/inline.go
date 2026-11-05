package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/soubarnak/xbpretty/internal/term"
)

// inlineState is the tiny slice of #45's progress model the prototype needs.
type inlineState struct {
	Ratio   float64
	Pkg     string
	Done    int
	Total   int
	Bytes   string
	Speed   string
	LastMsg string
}

func (s *inlineState) percent() int { return int(s.Ratio * 100) }

// buildInlineBox renders a 7-line rounded view with the heavy progress bar.
func buildInlineBox(w int, st inlineState, t Theme) *term.Buffer {
	if w < 20 {
		w = 20
	}
	if w > 80 {
		w = 80
	}
	buf := term.NewBuffer(w, 7)
	blk := &Block{X: 0, Y: 0, W: w, H: 7, Rounded: true, Style: Primary}
	blk.fill(buf, t)
	blk.title(buf, "Downloading Packages", Highlight)

	// Message row: " Last completed: <archive>" (#45's Download view).
	msg := st.LastMsg
	if msg == "" {
		msg = "Last completed: —"
	}
	buf.DrawTextLimit(2, 1, " "+msg, Regular, w-4)

	// Divider: full frame width, centred " Progress ", right " NN% ".
	line := make([]rune, w)
	line[0] = '├'
	line[w-1] = '┤'
	for i := 1; i < w-1; i++ {
		line[i] = '─'
	}
	label := " Progress "
	pct := fmt.Sprintf(" %d%% ", st.percent())
	mid := 1 + (w-2-term.StringWidth(label))/2
	for i, r := range []rune(label) {
		if mid+i < w-1 {
			line[mid+i] = r
		}
	}
	pr := []rune(pct)
	for i, r := range pr {
		pos := w - 1 - len(pr) + i
		if pos >= 1 {
			line[pos] = r
		}
	}
	for x, r := range line {
		buf.Set(x, 2, r, Primary)
	}

	// Bar: fill and unfill use the same glyph, colour is the only split.
	inner := w - 4
	filled := int(st.Ratio*float64(inner) + 0.5)
	for i := 0; i < inner; i++ {
		st := Primary
		if i >= filled {
			st = Secondary
		}
		buf.Set(2+i, 3, '━', st)
	}

	left := fmt.Sprintf(" Packages: %d/%d", st.Done, st.Total)
	right := fmt.Sprintf("Data: %s ", st.Bytes)
	buf.DrawTextLimit(2, 4, left, Regular, w/2-2)
	buf.DrawTextRight(w-2, 4, right, Regular)
	left2 := " Remaining: 1s"
	right2 := fmt.Sprintf("Speed: %s ", st.Speed)
	buf.DrawTextLimit(2, 5, left2, Regular, w/2-2)
	buf.DrawTextRight(w-2, 5, right2, Regular)
	return buf
}

// runInline drives the inline viewport beside a fake child. It reads no keys;
// Ctrl-C is a real signal because cbreak keeps ISIG.
func runInline(color bool, useDSR bool, frameDir string, trace *Trace) int {
	w, _, err := term.Size(os.Stdout)
	if err != nil {
		w = 80
	}
	guard := term.NewGuard(os.Stdin, os.Stdout)
	if err := guard.EnterInline(); err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: live progress unavailable (%v); forwarding xbps output\n", err)
		return 1
	}
	defer guard.Restore()

	vp := term.NewViewport(os.Stdout, w, 7, color)
	dsr := vp.Start(os.Stdin, useDSR, 100*time.Millisecond)
	if dsr.OK {
		trace.Log("INLINE dsr=reply %d;%d", dsr.Row, dsr.Col)
	} else {
		trace.Log("INLINE dsr=timeout fallback=no-anchor")
	}

	var (
		mu  sync.Mutex
		st  = inlineState{Total: 8, Bytes: "0 B/33.0 MiB", Speed: "0 B/s"}
		seq int
	)
	draw := func() {
		mu.Lock()
		defer mu.Unlock()
		b := buildInlineBox(w, st, Theme{Color: color})
		vp.Draw(b)
		seq++
		dumpFrame(frameDir, "inline", b, nil, color)
		trace.Log("INLINE draw seq=%d ratio=%d pkg=%s", seq, st.percent(), st.Pkg)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: live progress unavailable (%v)\n", err)
		return 1
	}
	cmd := exec.Command(exe, "-child")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: live progress unavailable (%v)\n", err)
		return 1
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "xbpretty: live progress unavailable (%v)\n", err)
		return 1
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	events := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			events <- sc.Text()
		}
		close(events)
	}()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	draw()
	for {
		select {
		case sig := <-sigCh:
			vp.Teardown()
			guard.Restore()
			code := 128 + int(sig.(syscall.Signal))
			trace.Log("INLINE signal=%v exit=%d", sig, code)
			if sig == syscall.SIGINT {
				fmt.Fprintln(os.Stderr, "xbpretty: interrupted")
				return 130
			}
			return code
		case line, ok := <-events:
			if !ok {
				mu.Lock()
				st.Ratio = 1
				mu.Unlock()
				draw()
				vp.Teardown()
				guard.Restore()
				trace.Log("INLINE complete exit=0")
				return 0
			}
			handleInlineEvent(line, vp, &st, &mu, draw, frameDir, &seq, trace, color)
		}
	}
}

func handleInlineEvent(line string, vp *term.Viewport, st *inlineState, mu *sync.Mutex,
	draw func(), frameDir string, seq *int, trace *Trace, color bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "PROGRESS":
		mu.Lock()
		if len(fields) >= 5 {
			st.Pkg = fields[1]
			st.Done, _ = strconv.Atoi(fields[2])
			st.Total, _ = strconv.Atoi(fields[3])
			st.Ratio = float64(st.Done) / float64(maxInt(st.Total, 1))
			st.Bytes = fields[4] + "/33.0 MiB"
			st.Speed = "5.4 MiB/s"
		}
		mu.Unlock()
		draw()
	case "DONE":
		if len(fields) < 2 {
			return
		}
		mu.Lock()
		st.LastMsg = "Last completed: " + fields[1]
		msg := " " + st.LastMsg
		mu.Unlock()
		vp.Insert(msg, Primary)
		*seq++
		if frameDir != "" {
			appendSequence(frameDir, fmt.Sprintf("INSERT %s", msg))
		}
		trace.Log("INLINE insert %q", msg)
		draw()
	case "ALT":
		if len(fields) < 2 {
			return
		}
		switch fields[1] {
		case "enter":
			vp.Hide()
			trace.Log("INLINE hide reason=child-alt-screen")
		case "leave":
			mu.Lock()
			b := buildInlineBox(vp.Width(), *st, Theme{Color: color})
			mu.Unlock()
			vp.Show(b)
			trace.Log("INLINE show reason=child-alt-screen")
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = io.EOF

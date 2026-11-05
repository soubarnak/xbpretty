//go:build linux

package cli

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/term"
)

// fetchPickerPlaceholder is the fixed-width stand-in for the httptest port in
// the PTY goldens. Linux ephemeral ports are five digits, so replacing it with
// the live port keeps every column width and lets the frame bytes be compared
// exactly.
const fetchPickerPlaceholder = "00000"

// fetchPickerHarness is one built-binary picker environment.
type fetchPickerHarness struct {
	env   map[string]string
	dir   string
	xbpsd string
	repo  string
	rank  string
	dump  string
}

// writeFetchPickerRanked writes the deterministic four-mirror ranked fixture
// consumed by the xbpretty_testroot hook.
func writeFetchPickerRanked(t *testing.T, path, base string) {
	t.Helper()
	rows := []struct {
		name, location, region string
		tier, latency, score   int
		speed                  int
	}{
		{"eu", "Finland", "EU", 1, 18, 18, 4299162},
		{"de", "Germany", "EU", 2, 31, 31, 3355443},
		{"us", "United States", "NA", 2, 47, 47, 2936013},
		{"jp", "Japan", "AS", 3, 112, 112, 1992294},
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s/%s|%s|%s|%d|%d|%d|%d\n",
			base, r.name, r.location, r.region, r.tier, r.latency, r.speed, r.score)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newFetchPickerHarness builds an isolated config, xbps.d and ranked fixture
// for one pty run. The repositories file names the fixture as the configured
// mirror so the write path has a managed line to rewrite.
func newFetchPickerHarness(t *testing.T, fixture *fetchFixture, mode string) *fetchPickerHarness {
	t.Helper()
	dir := t.TempDir()
	xbpsd := filepath.Join(dir, "xbps.d")
	if err := os.MkdirAll(xbpsd, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(xbpsd, "00-repository-main.conf")
	if err := os.WriteFile(repo, []byte("repository="+fixture.srv.URL+"/eu/current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rank := filepath.Join(dir, "ranked.txt")
	writeFetchPickerRanked(t, rank, fixture.srv.URL)
	dump := filepath.Join(dir, "frames")

	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"[Xbps]",
		`install_path = "` + filepath.Join(stubDir, "xbps-install") + `"`,
		`remove_path = "` + filepath.Join(stubDir, "xbps-remove") + `"`,
		`query_path = "` + filepath.Join(stubDir, "xbps-query") + `"`,
		"",
		"[Color]",
		`mode = "Always"`,
		"",
		"[Ui]",
		`mode = "` + mode + `"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "xbpretty", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &fetchPickerHarness{
		dir:   dir,
		xbpsd: xbpsd,
		repo:  repo,
		rank:  rank,
		dump:  dump,
		env: map[string]string{
			"XDG_CONFIG_HOME":                 cfgDir,
			"XDG_STATE_HOME":                  filepath.Join(dir, "state"),
			"XB_STUB_LOG":                     filepath.Join(dir, "commands.log"),
			"XB_STUB_RUN_STDOUT":              "",
			"XB_STUB_RUN_STDERR":              "",
			"XBPRETTY_TEST_EUID":              "0",
			"XBPRETTY_TEST_TRUST_UID":         strconv.Itoa(os.Getuid()),
			"XBPRETTY_TEST_FETCH_XBPSD":       xbpsd,
			"XBPRETTY_TEST_FETCH_WRITE_DIR":   xbpsd,
			"XBPRETTY_TEST_FETCH_MANIFEST":    filepath.Join(dir, "fetch.json"),
			"XBPRETTY_TEST_FETCH_MIRROR_LIST": fixture.srv.URL + "/mirrors.lst",
			"XBPRETTY_TEST_FETCH_ARCH":        "x86_64",
			"XBPRETTY_TEST_FETCH_RANKED":      rank,
			"XBPRETTY_TEST_TUI_DUMP":          dump,
			"XBPRETTY_HISTORY_DIR":            filepath.Join(dir, "history"),
			"TERM":                            "xterm-256color",
			"LANG":                            "C.UTF-8",
			"LC_ALL":                          "C.UTF-8",
			"LC_CTYPE":                        "C.UTF-8",
			"NO_COLOR":                        "",
			"COLUMNS":                         "",
		},
	}
	return h
}

// with returns a copy of the environment with overrides applied.
func (h *fetchPickerHarness) with(overrides map[string]string) map[string]string {
	out := make(map[string]string, len(h.env)+len(overrides))
	for k, v := range h.env {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

// fiveDigitListener forces the fixture onto a five-digit port so the
// fixed-width placeholder in the PTY golden always substitutes one-for-one.
func fiveDigitListener(t *testing.T) net.Listener {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listening for the picker fixture: %v", err)
		}
		if port := listener.Addr().(*net.TCPAddr).Port; port >= 10000 && port <= 99999 {
			return listener
		}
		listener.Close()
	}
	t.Fatal("could not reserve a five-digit fixture port")
	return nil
}

func fetchPickerFixture(t *testing.T) *fetchFixture {
	t.Helper()
	f := &fetchFixture{t: t, mirrors: []fetchMirror{
		{name: "eu", region: "EU", location: "Finland", tier: 1},
		{name: "de", region: "EU", location: "Germany", tier: 2},
		{name: "us", region: "NA", location: "United States", tier: 2},
		{name: "jp", region: "AS", location: "Japan", tier: 3},
	}}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.handle))
	// Replace the unstarted server's ephemeral listener with the forced port.
	srv.Listener.Close()
	srv.Listener = fiveDigitListener(t)
	srv.Start()
	f.srv = srv
	t.Cleanup(srv.Close)
	return f
}

// fixturePort returns the five-digit fixture port the goldens replace.
func fixturePort(t *testing.T, base string) string {
	t.Helper()
	port := base[strings.LastIndex(base, ":")+1:]
	if len(port) != len(fetchPickerPlaceholder) {
		t.Fatalf("fixture port %q is not five digits", port)
	}
	return port
}

// ptyGolden replaces the fixed-width port placeholder.
func ptyGolden(t *testing.T, name, port string) []byte {
	t.Helper()
	raw := goldenBytes(t, name)
	return []byte(strings.ReplaceAll(string(raw), fetchPickerPlaceholder, port))
}

// fetchFrameGrid turns a flushed frame into the trimmed cell grid.
func fetchFrameGrid(frame string) string {
	rows := regexp.MustCompile(`\x1b\[\d+;1H`).ReplaceAllString(frame, "\n")
	stripped := ansiFrame.ReplaceAllString(rows, "")
	var lines []string
	for _, line := range strings.Split(stripped, "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return strings.Join(lines, "\n")
}

// fetchSelectedURL returns the URL on the ✓ row.
func fetchSelectedURL(t *testing.T, frame string) string {
	t.Helper()
	for _, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, "✓") {
			continue
		}
		fields := strings.Fields(strings.ReplaceAll(line, "✓", " "))
		if len(fields) >= 2 {
			return fields[1]
		}
	}
	t.Fatalf("frame has no ✓ row:\n%s", frame)
	return ""
}

// waitFetchFrame waits for one frame and returns it.
func waitFetchFrame(t *testing.T, r *ptyRun, dump string, n int) string {
	t.Helper()
	frames := waitFrameCount(t, r, dump, n)
	return frames[n-1]
}

// TestAC052FetchPickerFrame asserts the PTY frame bytes against the checked-in
// 80x24 four-mirror golden: rounded border, title, columns, ✓/☐ markers, the
// Secondary+REVERSED URL and Score highlight, and both bands.
func TestAC052FetchPickerFrame(t *testing.T) {
	bin := buildTuiBinary(t)
	fixture := fetchPickerFixture(t)
	h := newFetchPickerHarness(t, fixture, "Auto")
	port := fixturePort(t, fixture.srv.URL)
	want := ptyGolden(t, "fetch-pty-80x24.sgr", port)

	r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
	waitCapture(t, r, "the picker frame", func(s string) bool {
		i := strings.LastIndex(s, term.ClearAll)
		return i >= 0 && len(s) >= i+len(want)
	})
	snap := r.cap.String()
	start := strings.LastIndex(snap, term.ClearAll)
	got := snap[start : start+len(want)]
	if got != string(want) {
		t.Errorf("picker frame bytes differ from the golden\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
	wantGrid := ptyGolden(t, "fetch-pty-80x24.txt", port)
	if gotGrid := fetchFrameGrid(got); gotGrid != string(wantGrid) {
		t.Errorf("picker grid differs from the golden\n--- got ---\n%s\n--- want ---\n%s", gotGrid, wantGrid)
	}
	for _, seq := range []string{term.AltEnter, term.CursorHide, term.PasteOn} {
		if !strings.Contains(snap, seq) {
			t.Errorf("screen entry is missing %q", seq)
		}
	}
	if strings.Contains(snap, term.MouseOn) {
		t.Errorf("the picker enabled mouse reporting; FR-070 ignores mouse input")
	}
	// The Session is idle after its initial draw.
	time.Sleep(50 * time.Millisecond)
	if extra := len(r.cap.String()) - (start + len(want)); extra != 0 {
		t.Errorf("capture has %d trailing bytes after the frame", extra)
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Errorf("q-confirm exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(r.stderr.String(), "no mirror was selected") {
		t.Errorf("confirm printed the cancel line:\n%s", r.stderr.String())
	}
}

// TestAC052FetchPickerKeysAndWrap drives the PTY key loop: j/k wrap, g/G,
// Home/End, arrows, and the ignored Space and mouse events.
func TestAC052FetchPickerKeysAndWrap(t *testing.T) {
	bin := buildTuiBinary(t)
	fixture := fetchPickerFixture(t)
	h := newFetchPickerHarness(t, fixture, "Auto")

	r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
	if got := fetchSelectedURL(t, waitFetchFrame(t, r, h.dump, 1)); !strings.Contains(got, "/eu") {
		t.Fatalf("initial selection = %q, want the fastest mirror /eu", got)
	}
	step := func(n int, keys, want string) {
		t.Helper()
		r.send(t, keys)
		frame := waitFetchFrame(t, r, h.dump, n)
		if got := fetchSelectedURL(t, frame); !strings.Contains(got, want) {
			t.Fatalf("after %q selection = %q, want %q", keys, got, want)
		}
	}
	step(2, "j", "/de")
	step(3, "j", "/us")
	step(4, "j", "/jp")
	step(5, "j", "/eu") // wrap at the bottom
	step(6, "k", "/jp") // wrap at the top
	step(7, "g", "/eu")
	step(8, "G", "/jp")
	step(9, "\x1b[H", "/eu")  // Home
	step(10, "\x1b[F", "/jp") // End
	step(11, "\x1b[A", "/us") // Up
	step(12, "\x1b[B", "/jp") // Down

	// Space and mouse are ignored: no frame is drawn and the ✓ row stays.
	r.send(t, " ")
	r.send(t, "\x1b[<65;10;20M")
	r.send(t, "\x1b[<64;10;20M")
	time.Sleep(120 * time.Millisecond)
	if got := len(dumpedFrames(t, h.dump)); got != 12 {
		t.Fatalf("ignored Space/mouse drew a frame: %d frames, want 12", got)
	}
	if got := fetchSelectedURL(t, dumpedFrames(t, h.dump)[11]); !strings.Contains(got, "/jp") {
		t.Errorf("ignored keys changed the selection to %q", got)
	}

	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("q-confirm exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	written, err := os.ReadFile(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), fixture.srv.URL+"/jp") {
		t.Errorf("confirm did not write the selected /jp mirror:\n%s", written)
	}
}

// TestAC052FetchPickerConfirmCancel covers Enter/q/Q confirm and Esc/Ctrl-C
// cancel, including the exact cancel line and exit 1.
func TestAC052FetchPickerConfirmCancel(t *testing.T) {
	fixture := fetchPickerFixture(t)
	for _, tc := range []struct {
		name    string
		key     string
		mirror  string
		confirm bool
	}{
		{"Enter", "\r", "/eu", true},
		{"q", "q", "/eu", true},
		{"Q", "Q", "/eu", true},
		{"Esc", "\x1b", "", false},
		{"Ctrl-C", "\x03", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := buildTuiBinary(t)
			h := newFetchPickerHarness(t, fixture, "Auto")
			original, err := os.ReadFile(h.repo)
			if err != nil {
				t.Fatal(err)
			}
			r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
			waitFetchFrame(t, r, h.dump, 1)
			r.send(t, tc.key)
			code := r.exitCode(t)
			if tc.confirm {
				if code != 0 {
					t.Fatalf("%s exit = %d, want 0\nstderr:\n%s", tc.name, code, r.stderr.String())
				}
				written, err := os.ReadFile(h.repo)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(written), fixture.srv.URL+tc.mirror+"/current") {
					t.Errorf("%s wrote the wrong mirror:\n%s", tc.name, written)
				}
				return
			}
			if code != 1 {
				t.Errorf("%s exit = %d, want 1", tc.name, code)
			}
			if !strings.Contains(r.stderr.String(), "xbpretty: no mirror was selected") {
				t.Errorf("%s stderr = %q, want the exact cancel line", tc.name, r.stderr.String())
			}
			if _, err := os.Stat(h.repo); err != nil {
				t.Fatalf("cancel left the repository file missing: %v", err)
			}
			if got, readErr := os.ReadFile(h.repo); readErr != nil || string(got) != string(original) {
				t.Errorf("cancel changed the repository file: %q, %v; want %q", got, readErr, original)
			}
		})
	}
}

// TestFetchPickerValidatesOneChoice proves the chosen mirror is validated
// exactly once and a failed validation is not retried with a second mirror.
func TestFetchPickerValidatesOneChoice(t *testing.T) {
	fixture := fetchPickerFixture(t)
	bin := buildTuiBinary(t)
	h := newFetchPickerHarness(t, fixture, "Auto")
	r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
	waitFetchFrame(t, r, h.dump, 1)
	r.send(t, "j\r")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("confirm exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	_, _, heads, _, headPaths := fixture.counts()
	if heads != 1 {
		t.Fatalf("HEADs = %d (%v), want exactly one validation for the chosen mirror", heads, headPaths)
	}
	if !strings.HasSuffix(headPaths[0], "/de/current/x86_64-repodata") {
		t.Errorf("validated %q, want the chosen /de mirror", headPaths[0])
	}
	written, err := os.ReadFile(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), fixture.srv.URL+"/de") {
		t.Errorf("written repository is not the chosen mirror:\n%s", written)
	}

	// A failed validation of the chosen row exits 1 without a silent second
	// choice and without writing.
	fixture.mirrors[0].suffixFail = true
	h2 := newFetchPickerHarness(t, fixture, "Auto")
	r2 := startPty(t, bin, h2.with(nil), []string{"fetch"}, 80, 24)
	waitFetchFrame(t, r2, h2.dump, 1)
	r2.send(t, "\r")
	if code := r2.exitCode(t); code != 1 {
		t.Errorf("failed-validation exit = %d, want 1\nstderr:\n%s", code, r2.stderr.String())
	}
	if !strings.Contains(r2.stderr.String(), "xbpretty: no candidate mirror serves all configured repositories") {
		t.Errorf("failed validation stderr = %q", r2.stderr.String())
	}
	if _, _, heads, _, _ := fixture.counts(); heads != 2 {
		t.Errorf("HEADs after failed validation = %d, want exactly one more", heads)
	}
}

// TestFetchPickerC1At60x18 is AC-052's fit half: at the 60x18 floor Location
// and Region are gone, the URL keeps its 24-cell floor, and Latency/Score stay.
func TestFetchPickerC1At60x18(t *testing.T) {
	bin := buildTuiBinary(t)
	fixture := fetchPickerFixture(t)
	h := newFetchPickerHarness(t, fixture, "Auto")
	r := startPty(t, bin, h.with(nil), []string{"fetch"}, 60, 18)
	frame := waitFetchFrame(t, r, h.dump, 1)
	if strings.Contains(frame, "Location:") || strings.Contains(frame, "Region:") {
		t.Errorf("60x18 frame kept a dropped column:\n%s", frame)
	}
	for _, col := range []string{"Mirrors:", "Tier:", "Latency:", "Speed:", "Score:"} {
		if !strings.Contains(frame, col) {
			t.Errorf("60x18 frame is missing %q:\n%s", col, frame)
		}
	}
	// The URL column is exactly the 24-cell floor: the clipped /eu URL fills
	// the 24 cells before the one-space gap and Tier.
	line := strings.Split(frame, "\n")[2]
	idx := strings.Index(line, "✓ ")
	if idx < 0 {
		t.Fatalf("selected row missing:\n%s", frame)
	}
	rest := line[idx+len("✓ "):]
	url := rest[:24]
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("URL column = %q, want the first 24 URL cells", url)
	}
	if rest[24] != ' ' {
		t.Errorf("URL column is not 24 cells wide: %q", rest[:26])
	}
	r.send(t, "q")
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
}

// assertPickerRestored proves the terminal guard unwound on the normal,
// cancel, signal and panic paths: termios equal to the pre-run state and the
// alternate screen, cursor and paste modes restored.
func assertPickerRestored(t *testing.T, r *ptyRun) {
	t.Helper()
	out := waitCapture(t, r, "the screen restore", func(s string) bool {
		return strings.Contains(s, term.AltLeave) &&
			strings.Contains(s, term.CursorShow) &&
			strings.Contains(s, term.PasteOff)
	})
	if !strings.Contains(out, term.MouseOff) && strings.Contains(out, term.MouseOn) {
		t.Errorf("mouse reporting was enabled but not restored")
	}
	after, err := term.GetState(r.slave)
	if err != nil {
		t.Fatalf("GetState after: %v", err)
	}
	if !after.Equal(r.before) {
		t.Errorf("termios flags not restored after the picker")
	}
}

// TestFetchPickerRestoresTerminal covers AC-054's lifecycle on the picker:
// confirm and SIGTERM both leave the alternate screen, cursor and paste clean.
func TestFetchPickerRestoresTerminal(t *testing.T) {
	bin := buildTuiBinary(t)
	t.Run("confirm", func(t *testing.T) {
		fixture := fetchPickerFixture(t)
		h := newFetchPickerHarness(t, fixture, "Auto")
		r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
		waitFetchFrame(t, r, h.dump, 1)
		r.send(t, "q")
		if code := r.exitCode(t); code != 0 {
			t.Fatalf("confirm exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
		}
		assertPickerRestored(t, r)
	})
	t.Run("SIGTERM", func(t *testing.T) {
		fixture := fetchPickerFixture(t)
		h := newFetchPickerHarness(t, fixture, "Auto")
		r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
		waitFetchFrame(t, r, h.dump, 1)
		if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("SIGTERM: %v", err)
		}
		if code := r.exitCode(t); code != 143 {
			t.Errorf("SIGTERM exit = %d, want 143\nstderr:\n%s", code, r.stderr.String())
		}
		assertPickerRestored(t, r)
	})
}

// TestFetchPickerResizeNoticeAndReturn is FR-071's below-floor path: a 50x15
// resize paints the notice, q is ignored there, and growing back restores the
// full frame. Esc cancels from the small frame in the second subtest.
func TestFetchPickerResizeNoticeAndReturn(t *testing.T) {
	bin := buildTuiBinary(t)
	fixture := fetchPickerFixture(t)
	start := func(t *testing.T) (*fetchPickerHarness, *ptyRun) {
		t.Helper()
		h := newFetchPickerHarness(t, fixture, "Auto")
		r := startPty(t, bin, h.with(nil), []string{"fetch"}, 80, 24)
		waitFetchFrame(t, r, h.dump, 1)
		if err := setPtySize(r.slave, 50, 15); err != nil {
			t.Fatalf("setPtySize 50x15: %v", err)
		}
		if err := r.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
			t.Fatalf("SIGWINCH: %v", err)
		}
		return h, r
	}
	t.Run("q ignored and back", func(t *testing.T) {
		h, r := start(t)
		frames := waitFrameCount(t, r, h.dump, 2)
		if !strings.Contains(frames[1], "Terminal too small (60x18 minimum; now 50x15)") {
			t.Fatalf("50x15 notice missing:\n%s", frames[1])
		}
		r.send(t, "q")
		time.Sleep(120 * time.Millisecond)
		if got := len(dumpedFrames(t, h.dump)); got != 2 {
			t.Fatalf("q below the floor drew a frame: %d, want 2", got)
		}
		if err := setPtySize(r.slave, 80, 24); err != nil {
			t.Fatalf("setPtySize 80x24: %v", err)
		}
		if err := r.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
			t.Fatalf("SIGWINCH back: %v", err)
		}
		frames = waitFrameCount(t, r, h.dump, 3)
		if strings.Contains(frames[2], "Terminal too small") || !strings.Contains(frames[2], "xbpretty Fetch") {
			t.Fatalf("frame did not return after growing back:\n%s", frames[2])
		}
		r.send(t, "q")
		if code := r.exitCode(t); code != 0 {
			t.Fatalf("confirm after resize exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
		}
		assertPickerRestored(t, r)
	})
	t.Run("Esc cancels", func(t *testing.T) {
		h, r := start(t)
		frames := waitFrameCount(t, r, h.dump, 2)
		if !strings.Contains(frames[1], "Terminal too small (60x18 minimum; now 50x15)") {
			t.Fatalf("50x15 notice missing:\n%s", frames[1])
		}
		r.send(t, "\x1b")
		if code := r.exitCode(t); code != 1 {
			t.Errorf("small-frame Esc exit = %d, want 1\nstderr:\n%s", code, r.stderr.String())
		}
		if !strings.Contains(r.stderr.String(), "xbpretty: no mirror was selected") {
			t.Errorf("small-frame cancel line = %q", r.stderr.String())
		}
		assertPickerRestored(t, r)
	})
}

// TestFetchPickerEntryBelowFloor uses the text path with no screen and no
// warning (FR-067), writing the shortlist's choice.
func TestFetchPickerEntryBelowFloor(t *testing.T) {
	bin := buildTuiBinary(t)
	fixture := fetchPickerFixture(t)
	h := newFetchPickerHarness(t, fixture, "Auto")
	r := startPty(t, bin, h.with(nil), []string{"fetch"}, 50, 15)
	if code := r.exitCode(t); code != 0 {
		t.Fatalf("below-floor exit = %d, want 0\nstderr:\n%s", code, r.stderr.String())
	}
	if strings.Contains(r.cap.String(), term.AltEnter) || strings.Contains(r.cap.String(), "xbpretty Fetch") {
		t.Errorf("the picker opened below 60x18:\n%q", r.cap.String())
	}
	if strings.Contains(r.stderr.String(), "full-screen interface") {
		t.Errorf("below-floor entry warned:\n%s", r.stderr.String())
	}
	written, err := os.ReadFile(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), fixture.srv.URL+"/eu/current") {
		t.Errorf("below-floor shortlist did not write its choice:\n%s", written)
	}
}

// TestFetchPickerFailedScreenWarnsOnceAndFallsBack covers init and render
// failures: one exact FR-067 warning, no picker, and the shortlist write path.
func TestFetchPickerFailedScreenWarnsOnceAndFallsBack(t *testing.T) {
	bin := buildTuiBinary(t)
	for _, tc := range []struct {
		name string
		fail string
		want string
	}{
		{"init", "init", "xbpretty: full-screen interface unavailable (injected init failure); showing the text summary"},
		{"render", "render", "xbpretty: full-screen interface failed (injected render failure); showing the text summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := fetchPickerFixture(t)
			h := newFetchPickerHarness(t, fixture, "Auto")
			r := startPty(t, bin, h.with(map[string]string{"XBPRETTY_TEST_TUI_FAIL": tc.fail}), []string{"fetch"}, 80, 24)
			if code := r.exitCode(t); code != 0 {
				t.Fatalf("%s-failure exit = %d, want 0\nstderr:\n%s", tc.name, code, r.stderr.String())
			}
			if got := strings.Count(r.stderr.String(), "full-screen interface"); got != 1 {
				t.Errorf("%s warning count = %d, want 1\nstderr:\n%s", tc.name, got, r.stderr.String())
			}
			if !strings.Contains(r.stderr.String(), tc.want) {
				t.Errorf("%s warning = %q, want %q", tc.name, r.stderr.String(), tc.want)
			}
			if tc.name == "init" && strings.Contains(r.cap.String(), term.AltEnter) {
				t.Errorf("a failed init still entered the alternate screen")
			}
			written, err := os.ReadFile(h.repo)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(written), fixture.srv.URL+"/eu/current") {
				t.Errorf("%s fallback did not write the shortlist choice:\n%s", tc.name, written)
			}
		})
	}
}

// TestFetchPickerPanicRestores is AC-054's panic path through the built
// binary: the guard restores, the exact warning lands after the restore, and
// the panic is re-raised.
func TestFetchPickerPanicRestores(t *testing.T) {
	bin := buildTuiBinary(t)
	fixture := fetchPickerFixture(t)
	h := newFetchPickerHarness(t, fixture, "Auto")
	original, err := os.ReadFile(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	r := startPty(t, bin, h.with(map[string]string{"XBPRETTY_TEST_TUI_FAIL": "panic"}), []string{"fetch"}, 80, 24)
	if code := r.exitCode(t); code != 2 {
		t.Errorf("panic exit = %d, want 2\nstderr:\n%s", code, r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "xbpretty: full-screen interface failed (injected full-screen panic)") {
		t.Errorf("panic warning missing:\n%s", r.stderr.String())
	}
	assertPickerRestored(t, r)
	if got, readErr := os.ReadFile(h.repo); readErr != nil || string(got) != string(original) {
		t.Errorf("a panic wrote the repository: %q, %v; want %q", got, readErr, original)
	}
}

// TestFetchPickerGateMatrix is AC-049/AC-052's gate: the picker appears under
// Auto/Tui and --no-tui (which demotes Tui to Auto), and never under Plain,
// --auto, --dry-run, TERM=dumb or a rootless run.
func TestFetchPickerGateMatrix(t *testing.T) {
	bin := buildTuiBinary(t)
	cases := []struct {
		name     string
		mode     string
		args     []string
		euid     string
		term     string
		wantOpen bool
		wantCode int
	}{
		{"Auto", "Auto", []string{"fetch"}, "0", "xterm-256color", true, 0},
		{"Tui", "Tui", []string{"fetch"}, "0", "xterm-256color", true, 0},
		{"no-tui demotes Tui to Auto", "Tui", []string{"fetch", "--no-tui"}, "0", "xterm-256color", true, 0},
		{"tui upgrades Auto", "Auto", []string{"fetch", "--tui"}, "0", "xterm-256color", true, 0},
		{"Plain", "Plain", []string{"fetch"}, "0", "xterm-256color", false, 0},
		{"auto", "Auto", []string{"fetch", "--auto", "2"}, "0", "xterm-256color", false, 0},
		{"dry-run", "Auto", []string{"fetch", "--dry-run"}, "0", "xterm-256color", false, 0},
		{"TERM=dumb", "Auto", []string{"fetch"}, "0", "dumb", false, 0},
		{"rootless", "Auto", []string{"fetch"}, "1000", "xterm-256color", false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := fetchPickerFixture(t)
			h := newFetchPickerHarness(t, fixture, tc.mode)
			env := h.with(map[string]string{"XBPRETTY_TEST_EUID": tc.euid, "TERM": tc.term})
			r := startPty(t, bin, env, tc.args, 80, 24)
			if tc.wantOpen {
				waitCapture(t, r, "the picker frame", func(s string) bool {
					return strings.Contains(s, "xbpretty Fetch")
				})
				r.send(t, "q")
			}
			code := r.exitCode(t)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, tc.wantCode, r.stderr.String())
			}
			opened := strings.Contains(r.cap.String(), term.AltEnter) && strings.Contains(r.cap.String(), "xbpretty Fetch")
			if opened != tc.wantOpen {
				t.Fatalf("picker opened = %v, want %v\nstdout:\n%q\nstderr:\n%s", opened, tc.wantOpen, r.cap.String(), r.stderr.String())
			}
			if !tc.wantOpen && strings.Contains(r.cap.String(), "xbpretty Fetch") {
				t.Errorf("the picker title leaked into the text path")
			}
		})
	}
}

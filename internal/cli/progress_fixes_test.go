package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

// TestWidgetFillRoundsAndPercentTruncates separates the two formulas: fill
// rounds (0.51*76 = 38.76 -> 39) while the displayed percent truncates
// (0.579 -> 57).
func TestWidgetFillRoundsAndPercentTruncates(t *testing.T) {
	if got := widgetFill(76, 0.51); got != 39 {
		t.Errorf("widgetFill(76, 0.51) = %d, want 39 (round, not truncate 38)", got)
	}
	if got := widgetFill(76, 0.49); got != 37 {
		t.Errorf("widgetFill(76, 0.49) = %d, want 37", got)
	}
	if got := widgetPercent(0.579); got != 57 {
		t.Errorf("widgetPercent(0.579) = %d, want 57", got)
	}
	if got := widgetPercent(0.999); got != 99 {
		t.Errorf("widgetPercent(0.999) = %d, want 99", got)
	}

	// The coloured bar's filled run must be 39 glyphs at 0.51, not 38.
	view := progressView{kind: viewInstall, msgHeader: "Status", msgText: "x", ratio: 0.51}
	line := renderProgressView(view, 80, true)[3]
	filledRun := countSGRRun(line, rolePrefix(summary.DefaultTheme(), roleProgressFilled))
	if filledRun != 39 {
		t.Errorf("filled colour run = %d glyphs, want 39", filledRun)
	}
}

// countSGRRun counts the glyphs between one SGR prefix and its reset.
func countSGRRun(line, prefix string) int {
	start := strings.Index(line, prefix)
	if start < 0 {
		return 0
	}
	rest := line[start+len(prefix):]
	end := strings.Index(rest, ansiResetSeq)
	if end < 0 {
		return 0
	}
	return strings.Count(rest[:end], "━")
}

// TestArchiveResolutionHandlesMuslAndSpecialPkgvers pins fixes 2 and 3: exact
// pkgver.arch.xbps stems resolve for musl arches, and `+`/`~` in the pkgver
// never stall the Download view.
func TestArchiveResolutionHandlesMuslAndSpecialPkgvers(t *testing.T) {
	for _, tc := range []struct {
		arch     string
		pkgver   string
		filename string
	}{
		{"x86_64-musl", "ca-certificates-20250419+3.127_1", "ca-certificates-20250419+3.127_1.x86_64-musl.xbps"},
		{"aarch64", "foo-1.0~rc1+2_1", "foo-1.0~rc1+2_1.aarch64.xbps"},
		{"armv7l-musl", "bar-2.1_3", "bar-2.1_3.armv7l-musl.xbps"},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			rows := []xbps.Row{{
				Name: "pkg", Version: "1_1", Action: "install", Arch: tc.arch,
				DownloadSize: 1000,
			}}
			// Use the row's pkgver as the plan key by making Name/Version
			// match the filename's pkgver.
			name, version := splitPkgverForTest(tc.pkgver)
			rows[0].Name, rows[0].Version = name, version

			m := newProgressModel("install", rows, nil)
			m.downloadTick(DownloadTick{Filename: tc.filename, FilePercent: 50})
			if got := m.position(); got != 500 {
				t.Fatalf("in-flight position = %d, want 500", got)
			}
			m.downloadDone(DownloadComplete{Filename: tc.filename})
			if m.completedCount != 1 {
				t.Fatalf("completed count = %d, want 1", m.completedCount)
			}
			if got := packageNameFromFilename(tc.filename); got != tc.pkgver {
				t.Errorf("packageNameFromFilename = %q, want %q", got, tc.pkgver)
			}
		})
	}
}

// splitPkgverForTest splits "<name>-<version>" at the last hyphen.
func splitPkgverForTest(pkgver string) (string, string) {
	i := strings.LastIndex(pkgver, "-")
	if i <= 0 {
		return pkgver, ""
	}
	return pkgver[:i], pkgver[i+1:]
}

// TestTelemetryAcceptsPlusAndTildePkgvers covers every stream class the views
// read, not just the download tick.
func TestTelemetryAcceptsPlusAndTildePkgvers(t *testing.T) {
	tick, ok := ParseDownloadTick("[ 50%] ca-certificates-20250419+3.127_1.x86_64.xbps: [45KB 50%] 10MB/s ETA: 00m01s\x1b[K\r")
	if !ok || tick.PackageName != "ca-certificates-20250419+3.127_1" {
		t.Fatalf("plus tick = %+v, %v", tick, ok)
	}
	done, ok := ParseDownloadComplete("ca-certificates-20250419+3.127_1.x86_64.xbps: 92KB [avg rate: 10MB/s]")
	if !ok || done.PackageName != "ca-certificates-20250419+3.127_1" {
		t.Fatalf("plus completion = %+v, %v", done, ok)
	}
	event, ok := ParsePackageEvent("ca-certificates-20250419+3.127_1: unpacking ...")
	if !ok || event.Package != "ca-certificates-20250419+3.127_1" {
		t.Fatalf("plus package event = %+v, %v", event, ok)
	}
	tilde, ok := ParsePackageEvent("foo-1.0~rc1+2_1: configuring ...")
	if !ok || tilde.Package != "foo-1.0~rc1+2_1" {
		t.Fatalf("tilde package event = %+v, %v", tilde, ok)
	}
	removed, ok := ParsePackageEvent("Removing 'foo-1.0~rc1+2_1' ...")
	if !ok || removed.Package != "foo-1.0~rc1+2_1" {
		t.Fatalf("tilde remove event = %+v, %v", removed, ok)
	}
}

// TestMirrorModelStartsOnMirrorScore pins fix 4: a mirror model's first frame
// is the Testing Mirrors title, never the transaction fallback.
func TestMirrorModelStartsOnMirrorScore(t *testing.T) {
	m := newProgressModel("mirror", nil, nil)
	m.mirrorTotal = 3
	if view := m.view(); view.kind != viewMirrorScore || view.titleText() != "Testing Mirrors" {
		t.Fatalf("first mirror view = kind %d title %q", view.kind, view.titleText())
	}
	fetch := newProgressModel("fetch", nil, nil)
	if view := fetch.view(); view.kind != viewMirrorScore {
		t.Errorf("fetch model kind = %d, want MirrorScore", view.kind)
	}
}

// TestUpdateUnknownRepositoryTotal shows `done/?` in the plain line, never a
// false 100%, when xbps-query -L named no total (fix 15, BF-03, BF-04). The
// widget carries no repository counter; its bar follows the bytes instead.
func TestUpdateUnknownRepositoryTotal(t *testing.T) {
	m := newProgressModel("update", nil, nil)
	m.repository("https://repo.voidlinux.org/current/x86_64-repodata")
	state := m.plain()
	if !state.hasRepos || repositoryPair(state.reposDone, state.reposTotal) != "0/?" {
		t.Fatalf("plain repositories = %+v, want 0/?", state)
	}
	if got := renderPlainLine(state); !strings.Contains(got, "Repositories: 0/?") {
		t.Errorf("plain line = %q, want Repositories: 0/?", got)
	}
	if view := m.view(); view.ratio != 0 {
		t.Errorf("unknown total ratio = %v, want 0 (never a false 100%%)", view.ratio)
	}
	for _, info := range m.view().left {
		if info.label == "Repositories" {
			t.Errorf("the widget still shows a Repositories counter: %+v", info)
		}
	}
}

// TestConfigureOnlyPlanReachesOne pins fix 16: configure-only rows contribute
// one configure unit each, so the two-unit model still ends at 100%.
func TestConfigureOnlyPlanReachesOne(t *testing.T) {
	rows := []xbps.Row{
		{Name: "a", Version: "1_1", Action: "configure"},
		{Name: "b", Version: "1_1", Action: "configure"},
	}
	m := newProgressModel("install", rows, nil)
	if got := m.ratio(); got != 0 {
		t.Errorf("configure-only start ratio = %v, want 0", got)
	}
	m.packageEvent(PackageEvent{Package: "a-1_1", Action: "configuring"})
	if got, want := m.ratio(), 0.5; got != want {
		t.Errorf("half-configured ratio = %v, want %v", got, want)
	}
	m.packageEvent(PackageEvent{Package: "b-1_1", Action: "configuring"})
	if got := m.ratio(); got != 1 {
		t.Errorf("fully configured ratio = %v, want 1", got)
	}
}

// TestPlainInstallMessageAndFilters pins fix 17: the plain line carries the
// prototype's `Status: ` prefix and no empty filter is emitted.
func TestPlainInstallMessageAndFilters(t *testing.T) {
	m := newProgressModel("install", []xbps.Row{{Name: "a", Version: "1_1", Action: "install"}}, nil)
	state := m.plain()
	if state.message != "Working..." {
		t.Errorf("message before data = %q, want Working...", state.message)
	}
	if state.hasItem {
		t.Error("an empty Package filter was emitted before data arrived")
	}
	m.packageEvent(PackageEvent{Package: "a-1_1", Action: "unpacking"})
	state = m.plain()
	if state.message != "Status: Unpacking a-1_1" {
		t.Errorf("message = %q, want Status: Unpacking a-1_1", state.message)
	}
	line := renderPlainLine(state)
	if !strings.Contains(line, "Status: Unpacking a-1_1 | Package: a-1_1") {
		t.Errorf("plain line = %q", line)
	}
}

// TestPlainRepositoriesFilterGolden pins the update fallback's filter.
func TestPlainRepositoriesFilterGolden(t *testing.T) {
	state := plainState{
		ratio: 0.5, message: "Updating: https://repo.voidlinux.org/current",
		hasRepos: true, reposDone: 1, reposTotal: 2,
	}
	want := "[====================                    ]  50% Updating: https://repo.voidlinux.org/current | Repositories: 1/2"
	if got := renderPlainLine(state); got != want {
		t.Errorf("plain update line:\n got %q\nwant %q", got, want)
	}
}

// TestRendererClipsExtraInfoRows pins fix 11: extra rows never grow the frame.
func TestRendererClipsExtraInfoRows(t *testing.T) {
	view := progressView{
		kind: viewInstall, msgHeader: "Status", msgText: "x", ratio: 0.5,
		left:  []progressInfo{{"A", "1"}, {"B", "2"}, {"C", "3"}},
		right: []progressInfo{{"D", "4"}, {"E", "5"}, {"F", "6"}},
	}
	lines := renderProgressView(view, 80, false)
	if len(lines) != heightOther {
		t.Fatalf("height = %d with extra rows, want fixed %d", len(lines), heightOther)
	}
}

// TestThemePrimaryReachesWidget pins fix 6: a custom [Color.theme] Primary
// paints the frame instead of the hardcoded default.
func TestThemePrimaryReachesWidget(t *testing.T) {
	theme := summary.DefaultTheme()
	theme.Primary = "\x1b[1;35m"
	view := progressView{kind: viewInstall, msgHeader: "Status", msgText: "x", ratio: 0.5}
	lines := renderProgressViewThemed(view, 80, true, theme)
	if !strings.HasPrefix(lines[0], "╭\x1b[1;35m") {
		t.Errorf("custom Primary not used on the border: %q", lines[0])
	}
	if strings.Contains(lines[0], "\x1b[1;92m") {
		t.Errorf("default Primary leaked through: %q", lines[0])
	}
}

// TestLiveApplySerializesConcurrentCallbacks drives the model from two
// goroutines through Apply; the race detector enforces the mutex.
func TestLiveApplySerializesConcurrentCallbacks(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	live.now = func() time.Time { return time.Unix(0, 0) }
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			live.Apply(false, func(m *progressModel) {
				m.downloadTick(DownloadTick{Filename: "alpha-1_1.x86_64.xbps", FilePercent: n % 100})
			})
		}(i)
		go func() {
			defer wg.Done()
			live.Apply(true, func(m *progressModel) {
				m.packageEvent(PackageEvent{Package: "a-1_1", Action: "unpacking"})
			})
		}()
	}
	wg.Wait()
	live.Finish()
}

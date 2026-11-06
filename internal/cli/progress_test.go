package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/summary"
	"github.com/soubarnak/xbpretty/internal/xbps"
)

func TestWidgetTimeUsesStrictBoundaries(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0s"},
		{60, "60s"},
		{61, "1min 1s"},
		{3600, "60min 0s"},
		{3601, "1h 0min 1s"},
		{86400, "24h 0min 0s"},
		{86401, "1d 0h 0min 1s"},
	}
	for _, tc := range cases {
		if got := widgetTime(tc.in); got != tc.want {
			t.Errorf("widgetTime(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRateAndRemainingFormulas(t *testing.T) {
	if got := ceilRate(19456*1024, 3.2); got != 6_225_920 {
		t.Errorf("ceilRate = %d, want ceil(19922944/3.2) = 6225920", got)
	}
	if got, ok := etaSeconds(100, 400, 50); !ok || got != 6 {
		t.Errorf("etaSeconds = %d,%v want 6,true", got, ok)
	}
	if _, ok := etaSeconds(0, 400, 50); ok {
		t.Error("position 0 must show no ETA")
	}
	if _, ok := etaSeconds(400, 400, 50); ok {
		t.Error("a finished download must show no ETA")
	}
	if _, ok := etaSeconds(100, 400, 0); ok {
		t.Error("speed 0 must show no ETA")
	}
}

func downloadPlan() []xbps.Row {
	return []xbps.Row{
		{Name: "alpha", Version: "1_1", Action: "install", HasDownload: true, DownloadSize: 400 * 1024},
		{Name: "beta", Version: "1_1", Action: "install", HasDownload: true, DownloadSize: 600 * 1024},
	}
}

// TestDownloadPositionUsesExactPlanBytes pins FR-064: position sums the plan
// bytes, not the humanised tick sizes, and a .sig2 tick moves nothing.
func TestDownloadPositionUsesExactPlanBytes(t *testing.T) {
	now := time.Unix(100, 0)
	m := newProgressModel("install", downloadPlan(), func() time.Time { return now })
	m.downloadTick(DownloadTick{Filename: "alpha-1_1.x86_64.xbps.sig2", FilePercent: 100})
	if got := m.position(); got != 0 {
		t.Fatalf("a .sig2 tick moved position to %d", got)
	}
	m.downloadTick(DownloadTick{Filename: "alpha-1_1.x86_64.xbps", FilePercent: 50})
	if got, want := m.position(), uint64(200*1024); got != want {
		t.Errorf("in-flight position = %d, want %d", got, want)
	}
	// A completion line without a tick is a cache hit: it still counts the
	// exact plan bytes and immediately increases the package count.
	m.downloadDone(DownloadComplete{Filename: "beta-1_1.x86_64.xbps", SizeStr: "1KB"})
	if got, want := m.position(), uint64(200*1024+600*1024); got != want {
		t.Errorf("cache-hit position = %d, want %d", got, want)
	}
	if m.completedCount != 1 {
		t.Errorf("completed packages = %d, want 1", m.completedCount)
	}
	if m.lastCompleted != "beta-1_1.x86_64.xbps" {
		t.Errorf("last completed = %q", m.lastCompleted)
	}
	// Replaying the same completion must not double-count.
	m.downloadDone(DownloadComplete{Filename: "beta-1_1.x86_64.xbps"})
	if m.completedCount != 1 || m.completedBytes != 600*1024 {
		t.Errorf("duplicate completion double-counted: %+v", m)
	}
}

func TestDownloadPositionClampsToTotal(t *testing.T) {
	m := newProgressModel("install", downloadPlan(), nil)
	m.completed["alpha"] = true
	m.completedBytes = 500 * 1024
	m.inFlight["beta"] = 500 * 1024
	if got := m.position(); got != 1000*1024 {
		t.Errorf("position = %d, want clamped 1024000", got)
	}
}

// TestVerificationSnapsToOneFrameThenInstall pins FR-041/FR-064's handover:
// the Verifying header draws the Download view at 100% once, then the model
// switches to the Install view.
func TestVerificationSnapsToOneFrameThenInstall(t *testing.T) {
	m := newProgressModel("install", downloadPlan(), nil)
	m.downloadDone(DownloadComplete{Filename: "alpha-1_1.x86_64.xbps"})
	m.phase("Verifying")
	first := m.view()
	if first.kind != viewDownload || first.ratio != 1 {
		t.Fatalf("verification frame = kind %d ratio %v, want a 100%% Download view", first.kind, first.ratio)
	}
	second := m.view()
	if second.kind != viewInstall {
		t.Errorf("next frame kind = %d, want Install", second.kind)
	}
}

// TestInstallTwoUnitModel pins U = 2·A: one unpack plus one configure of two
// packages is 2/4 = 50%.
func TestInstallTwoUnitModel(t *testing.T) {
	rows := []xbps.Row{
		{Name: "alpha", Version: "1_1", Action: "install"},
		{Name: "beta", Version: "1_1", Action: "install"},
	}
	m := newProgressModel("install", rows, nil)
	m.packageEvent(PackageEvent{Package: "alpha-1_1", Action: "unpacking"})
	if got, want := m.ratio(), 0.25; got != want {
		t.Errorf("after one unpack ratio = %v, want %v", got, want)
	}
	m.packageEvent(PackageEvent{Package: "alpha-1_1", Action: "configuring"})
	if got, want := m.ratio(), 0.5; got != want {
		t.Errorf("after unpack+configure ratio = %v, want %v", got, want)
	}
	// Repeated events for the same package never exceed the model.
	m.packageEvent(PackageEvent{Package: "alpha-1_1", Action: "unpacking"})
	if got, want := m.ratio(), 0.5; got != want {
		t.Errorf("duplicate event ratio = %v, want %v", got, want)
	}
}

func TestRemoveViewRatio(t *testing.T) {
	rows := []xbps.Row{
		{Name: "alpha", Version: "1_1", Action: "remove"},
		{Name: "beta", Version: "1_1", Action: "remove"},
	}
	m := newProgressModel("remove", rows, nil)
	if m.kind != viewRemove {
		t.Fatalf("kind = %d, want Remove", m.kind)
	}
	m.packageEvent(PackageEvent{Package: "alpha-1_1", Action: "removing"})
	if got, want := m.ratio(), 0.0; got != want {
		t.Errorf("remove ratio before completion = %v, want 0", got)
	}
	m.packageEvent(PackageEvent{Package: "alpha-1_1", Action: "removed"})
	if got, want := m.ratio(), 0.5; got != want {
		t.Errorf("remove ratio = %v, want %v", got, want)
	}
}

func TestUpdateAndMirrorRatios(t *testing.T) {
	up := newProgressModel("update", nil, nil)
	up.setRepositoryTotal(2)
	up.repository("https://repo.voidlinux.org/current")
	if got, want := up.ratio(), 0.5; got != want {
		t.Errorf("update ratio = %v, want %v", got, want)
	}
	if view := up.view(); view.msgRole != roleRegular || view.kind != viewUpdate {
		t.Errorf("update view = %+v", view)
	}
	mirror := newProgressModel("fetch", nil, nil)
	mirror.mirrorEvent("https://repo.voidlinux.org/current", 3, 4, 2*time.Second)
	view := mirror.view()
	if view.kind != viewMirrorScore || view.ratio != 0.75 {
		t.Errorf("mirror view = %+v", view)
	}
	if view.msgHeader != "Finished" || view.msgText != "https://repo.voidlinux.org/current" {
		t.Errorf("mirror message = %q %q", view.msgHeader, view.msgText)
	}
	if len(view.left) != 1 || view.left[0].value != "3/4" {
		t.Errorf("mirror counters = %+v", view.left)
	}
}

// TestPlainLineGolden pins the 40-cell grammar byte-for-byte: round fill,
// truncated %3d, transfer suffix and the Packages filter.
func TestPlainLineGolden(t *testing.T) {
	state := plainState{
		ratio:       19456.0 / 33792.0,
		message:     "Last completed: glibc-2.41_1.x86_64.xbps",
		hasTransfer: true,
		position:    19456 * 1024,
		total:       33792 * 1024,
		rate:        ceilRate(19456*1024, 1),
		hasItems:    true,
		itemsDone:   43,
		itemsTotal:  80,
	}
	state.rate = 5_662_789 // 5.4 MiB/s after the widget's ceil
	want := "[" + strings.Repeat("=", 23) + strings.Repeat(" ", 17) + "] " +
		" 57% Last completed: glibc-2.41_1.x86_64.xbps " +
		"19.0 MiB/33.0 MiB 5.4 MiB/s | Packages: 43/80"
	got := renderPlainLine(state)
	if got != want {
		t.Errorf("plain line:\n got %q\nwant %q", got, want)
	}
	bar := []rune(got[1:41])
	for _, r := range bar {
		if r != '=' && r != ' ' {
			t.Errorf("bar contains %q, want only '=' and spaces", r)
		}
	}
}

func TestPlainLineFilters(t *testing.T) {
	cases := []struct {
		name  string
		state plainState
		want  string
	}{
		{
			name:  "install package",
			state: plainState{ratio: 0, message: "Unpacking alpha", hasItem: true, item: "alpha-1_1"},
			want:  " | Package: alpha-1_1",
		},
		{
			name:  "update repositories",
			state: plainState{ratio: 1, message: "Updating: https://repo"},
			want:  "Updating: https://repo",
		},
		{
			name:  "mirror count",
			state: plainState{ratio: 0.5, message: "Finished: https://repo", hasMirrors: true, mirrorDone: 1, mirrorTotal: 2},
			want:  "| Mirrors: 1/2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderPlainLine(tc.state)
			if !strings.Contains(got, tc.want) {
				t.Errorf("plain line %q missing %q", got, tc.want)
			}
			if strings.Contains(got, "\x1b[") {
				t.Errorf("plain line carries colour: %q", got)
			}
		})
	}
}

// TestLiveCoalescing pins the 150 ms window: a burst draws once, a forced
// completion draws immediately, and teardown writes no completion line.
func TestLiveCoalescing(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(&out, false, 80, liveWidgetMode, model, summary.DefaultTheme())
	now := time.Unix(0, 0)
	var pending func()
	var waits []time.Duration
	live.now = func() time.Time { return now }
	live.after = func(d time.Duration, f func()) *time.Timer {
		waits = append(waits, d)
		pending = f
		return time.NewTimer(time.Hour)
	}

	live.Request(true)
	if got := strings.Count(out.String(), "╭"); got != 1 {
		t.Fatalf("initial draw count = %d, want 1", got)
	}
	for i := 0; i < 5; i++ {
		model.downloadTick(DownloadTick{Filename: "alpha-1_1.x86_64.xbps", FilePercent: 10 * (i + 1)})
		live.Request(false)
	}
	if got := strings.Count(out.String(), "╭"); got != 1 {
		t.Errorf("burst drew %d frames, want 1 coalesced frame", got)
	}
	if len(waits) != 1 || waits[0] != 150*time.Millisecond {
		t.Errorf("scheduled waits = %v, want one 150ms", waits)
	}

	now = now.Add(flushInterval)
	if pending == nil {
		t.Fatal("coalescing never scheduled the deferred draw")
	}
	pending()
	if got := strings.Count(out.String(), "╭"); got != 2 {
		t.Errorf("deferred draw count = %d, want 2", got)
	}

	model.downloadDone(DownloadComplete{Filename: "alpha-1_1.x86_64.xbps"})
	live.Request(true)
	if got := strings.Count(out.String(), "╭"); got != 3 {
		t.Errorf("forced completion draw count = %d, want immediate third frame", got)
	}

	live.Finish()
	if strings.Contains(out.String(), "Transaction complete") || strings.Contains(out.String(), "✔") {
		t.Error("teardown printed a completion line")
	}
	if !strings.HasSuffix(out.String(), "\x1b[J") {
		t.Errorf("teardown did not clear the frame: %q", out.String())
	}
}

func TestPlainLiveRedrawsInPlaceWithoutColour(t *testing.T) {
	var out bytes.Buffer
	model := newProgressModel("install", downloadPlan(), nil)
	live := newLiveProgress(&out, true, 80, livePlainMode, model, summary.DefaultTheme()) // colour requested, plain must ignore it
	live.Request(true)
	model.downloadTick(DownloadTick{Filename: "alpha-1_1.x86_64.xbps", FilePercent: 50})
	live.Request(true)
	got := out.String()
	if !strings.HasPrefix(got, "\r\x1b[2K[") {
		t.Errorf("plain line does not redraw in place: %q", got)
	}
	if strings.Contains(got, "\x1b[1;") || strings.Contains(got, "\x1b[0;") {
		t.Errorf("plain line carries colour: %q", got)
	}
	if strings.Count(got, "\r\x1b[2K") != 2 {
		t.Errorf("plain redraw count = %d, want 2", strings.Count(got, "\r\x1b[2K"))
	}
	live.Finish()
	if !strings.HasSuffix(out.String(), "\r\x1b[2K") {
		t.Errorf("plain teardown did not clear the line: %q", out.String())
	}
}

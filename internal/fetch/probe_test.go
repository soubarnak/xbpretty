package fetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

func zstdBody(n int) []byte {
	body := make([]byte, n)
	if n >= 4 {
		copy(body, []byte{0x28, 0xB5, 0x2F, 0xFD})
	}
	for i := 4; i < n; i++ {
		body[i] = byte(i)
	}
	return body
}

func replayServer(t *testing.T, fn http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(fn)
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeSendsRangeAndValidatesZstd(t *testing.T) {
	body := zstdBody(262144)
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/current/x86_64-repodata" {
			t.Errorf("path = %q, want /current/x86_64-repodata", r.URL.Path)
		}
		if got := r.Header.Get("Range"); got != "bytes=0-262143" {
			t.Errorf("Range = %q, want bytes=0-262143", got)
		}
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		if got := r.Header.Get("User-Agent"); got != "xbpretty/test" {
			t.Errorf("User-Agent = %q, want xbpretty/test", got)
		}
		w.Header().Set("Content-Range", "bytes 0-262143/1048576")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	})

	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Mirrors:     []fetch.Mirror{{Region: "EU", URL: srv.URL, Tier: 1}},
		PrimaryPath: "/current",
		Arch:        "x86_64",
		UserAgent:   "xbpretty/test",
	})
	if len(report.Failures) != 0 {
		t.Fatalf("probe failed: %+v", report.Failures)
	}
	if len(report.Results) != 1 {
		t.Fatalf("results = %+v", report.Results)
	}
	got := report.Results[0]
	if got.Bytes != int64(len(body)) {
		t.Errorf("Bytes = %d, want %d", got.Bytes, len(body))
	}
	if got.LatencyMS < 1 || got.ScoreMS < 1 {
		t.Errorf("latency/score must be at least 1 ms: %+v", got)
	}
	if got.Speed <= 0 {
		t.Errorf("Speed = %v, want > 0", got.Speed)
	}
}

func TestProbeServerIgnoringRangeIsReadTo262144(t *testing.T) {
	body := zstdBody(300000)
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 200, not 206: the server ignored Range.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Mirrors:     []fetch.Mirror{{URL: srv.URL, Tier: 1}},
		PrimaryPath: "/current",
		Arch:        "x86_64",
		UserAgent:   "xbpretty/test",
	})
	if len(report.Failures) != 0 {
		t.Fatalf("probe failed: %+v", report.Failures)
	}
	if got := report.Results[0].Bytes; got != 262144 {
		t.Errorf("Bytes = %d, want 262144 (the range cap)", got)
	}
}

func TestProbeRejectsStatusMagicAndShortBodies(t *testing.T) {
	for _, tt := range []struct {
		name string
		fn   http.HandlerFunc
		want string
	}{
		{"status", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "missing", http.StatusNotFound)
		}, "404"},
		{"magic", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("not zstd"))
		}, "zstd"},
		{"short", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0x28, 0xB5})
		}, "short"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := replayServer(t, tt.fn)
			report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
				Mirrors:     []fetch.Mirror{{URL: srv.URL, Tier: 1}},
				PrimaryPath: "/current",
				Arch:        "x86_64",
				UserAgent:   "xbpretty/test",
			})
			if len(report.Results) != 0 || len(report.Failures) != 1 {
				t.Fatalf("results=%+v failures=%+v", report.Results, report.Failures)
			}
			if !strings.Contains(strings.ToLower(report.Failures[0].Err.Error()), tt.want) {
				t.Errorf("failure = %q, want it to mention %q", report.Failures[0].Err, tt.want)
			}
		})
	}
}

func TestProbeFollowsRedirects(t *testing.T) {
	final := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(zstdBody(64))
	})
	redirect := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/current/x86_64-repodata", http.StatusFound)
	})
	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Mirrors:     []fetch.Mirror{{URL: redirect.URL, Tier: 1}},
		PrimaryPath: "/current",
		Arch:        "x86_64",
		UserAgent:   "xbpretty/test",
	})
	if len(report.Failures) != 0 {
		t.Fatalf("probe failed: %+v", report.Failures)
	}
}

func TestProbeAllCapsConcurrencyAtTen(t *testing.T) {
	var active, peak atomic.Int32
	mirrors := make([]fetch.Mirror, 25)
	for i := range mirrors {
		mirrors[i] = fetch.Mirror{URL: "http://mirror.invalid", Tier: 1, Region: "EU"}
	}
	// One server hands out all requests; the mirror URL is replaced per
	// candidate below.
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		cur := active.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(zstdBody(64))
	})
	for i := range mirrors {
		mirrors[i].URL = srv.URL
	}
	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Mirrors:     mirrors,
		PrimaryPath: "/current",
		Arch:        "x86_64",
		UserAgent:   "xbpretty/test",
	})
	if len(report.Results) != len(mirrors) {
		t.Fatalf("results = %d, want %d", len(report.Results), len(mirrors))
	}
	if got := peak.Load(); got > 10 {
		t.Errorf("peak concurrency = %d, want at most 10", got)
	}
}

func TestProbeTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	defer close(release)

	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Mirrors:      []fetch.Mirror{{URL: srv.URL, Tier: 1}},
		PrimaryPath:  "/current",
		Arch:         "x86_64",
		UserAgent:    "xbpretty/test",
		ProbeTimeout: 40 * time.Millisecond,
	})
	if len(report.Failures) != 1 {
		t.Fatalf("failures = %+v, want one timeout", report.Failures)
	}
	if !errors.Is(report.Failures[0].Err, context.DeadlineExceeded) &&
		!strings.Contains(strings.ToLower(report.Failures[0].Err.Error()), "deadline") {
		t.Errorf("failure = %v, want a deadline", report.Failures[0].Err)
	}
}

func TestProbeProgressEvents(t *testing.T) {
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(zstdBody(64))
	})
	failing := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	})

	var mu sync.Mutex
	var events []fetch.ProgressEvent
	report := fetch.ProbeAll(context.Background(), fetch.ProbeOptions{
		Mirrors: []fetch.Mirror{
			{URL: srv.URL, Tier: 1},
			{URL: failing.URL, Tier: 1},
			{URL: srv.URL + "/other", Tier: 1},
		},
		PrimaryPath: "/current",
		Arch:        "x86_64",
		UserAgent:   "xbpretty/test",
		Progress: func(e fetch.ProgressEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
	})
	if len(report.Results) != 2 || len(report.Failures) != 1 {
		t.Fatalf("results=%d failures=%d", len(report.Results), len(report.Failures))
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	for i, e := range events {
		if e.Total != 3 || e.Done != i+1 {
			t.Errorf("event %d = %+v, want done=%d total=3", i, e, i+1)
		}
		if e.Ratio != float64(i+1)/3 {
			t.Errorf("event %d ratio = %v, want %v", i, e.Ratio, float64(i+1)/3)
		}
	}
	if events[len(events)-1].Finished == "" {
		t.Errorf("last event has no Finished mirror: %+v", events[len(events)-1])
	}
}

func TestRankIsDeterministic(t *testing.T) {
	results := []fetch.Result{
		{Mirror: fetch.Mirror{URL: "https://b", Tier: 1}, ScoreMS: 10, LatencyMS: 5},
		{Mirror: fetch.Mirror{URL: "https://a", Tier: 2}, ScoreMS: 10, LatencyMS: 5},
		{Mirror: fetch.Mirror{URL: "https://c", Tier: 1}, ScoreMS: 10, LatencyMS: 5},
		{Mirror: fetch.Mirror{URL: "https://d", Tier: 1}, ScoreMS: 9, LatencyMS: 8},
		{Mirror: fetch.Mirror{URL: "https://e", Tier: 1}, ScoreMS: 10, LatencyMS: 4},
	}
	ranked := fetch.Rank(results)
	var got []string
	for _, r := range ranked {
		got = append(got, r.URL)
	}
	want := []string{"https://d", "https://e", "https://b", "https://c", "https://a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Rank = %v, want %v", got, want)
	}
}

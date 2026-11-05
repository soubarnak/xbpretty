package fetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

func TestParseMirrorsValidatesAndNormalises(t *testing.T) {
	raw := strings.Join([]string{
		"# generated mirror list",
		"",
		"EU\thttps://repo.example/void/\tHelsinki, Finland\t1",
		"EU\thttps://repo.example/void\tHelsinki, Finland\t1", // duplicate after normalising
		"NA\thttp://mirror.example/void///\tChicago, USA\t2",
		"AS\thttps://empty.example/\t\t2",
		"World\tftp://ftp.example/void\tEverywhere\t1", // not http(s)
		"\thttps://blank-region.example/\tNowhere\t1",  // empty region
		"EU\thttps://no-tier.example/\tNowhere",        // only three fields
		"EU\thttps://zero-tier.example/\tNowhere\t0",   // tier must be positive
		"EU\thttps://bad-tier.example/\tNowhere\tone",  // tier must be an integer
	}, "\n")

	mirrors, err := fetch.ParseMirrors(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMirrors: %v", err)
	}
	if len(mirrors) != 3 {
		t.Fatalf("parsed %d mirrors, want 3: %+v", len(mirrors), mirrors)
	}
	want := []fetch.Mirror{
		{Region: "EU", URL: "https://repo.example/void", Location: "Helsinki, Finland", Tier: 1},
		{Region: "NA", URL: "http://mirror.example/void", Location: "Chicago, USA", Tier: 2},
		{Region: "AS", URL: "https://empty.example", Location: "", Tier: 2},
	}
	for i := range want {
		if mirrors[i] != want[i] {
			t.Errorf("mirror %d = %+v, want %+v", i, mirrors[i], want[i])
		}
	}
}

func TestParseMirrorsRejectsReadErrors(t *testing.T) {
	_, err := fetch.ParseMirrors(errReader{})
	if err == nil {
		t.Fatal("ParseMirrors accepted a read error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, context.Canceled }

func TestSnapshotHas33RowsAndDate(t *testing.T) {
	if fetch.SnapshotDate != "2026-11-05" {
		t.Errorf("SnapshotDate = %q, want 2026-11-05", fetch.SnapshotDate)
	}
	mirrors, err := fetch.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(mirrors) != 33 {
		t.Errorf("snapshot has %d rows, want 33", len(mirrors))
	}
	if mirrors[0].Region == "" || mirrors[0].Tier == 0 {
		t.Errorf("snapshot first row was not parsed: %+v", mirrors[0])
	}
}

func TestFilterMirrorsRegionAndLocationWords(t *testing.T) {
	mirrors := []fetch.Mirror{
		{Region: "EU", URL: "https://a", Location: "Helsinki, Finland", Tier: 1},
		{Region: "NA", URL: "https://b", Location: "Chicago, USA", Tier: 1},
		{Region: "AS", URL: "https://c", Location: "Singapore", Tier: 1},
		{Region: "EU", URL: "https://d", Location: "", Tier: 1},
	}

	for _, tt := range []struct {
		names []string
		want  []string
	}{
		{[]string{"finland"}, []string{"https://a"}},
		{[]string{"USA"}, []string{"https://b"}},
		{[]string{"eu"}, []string{"https://a", "https://d"}},
		{[]string{"singapore", "Finland"}, []string{"https://a", "https://c"}},
		{[]string{"helsinki, finland"}, nil}, // the user's value is not split into words
		{[]string{"no-such-place"}, nil},
	} {
		got := fetch.FilterMirrors(mirrors, tt.names)
		var urls []string
		for _, m := range got {
			urls = append(urls, m.URL)
		}
		if strings.Join(urls, ",") != strings.Join(tt.want, ",") {
			t.Errorf("FilterMirrors(%v) = %v, want %v", tt.names, urls, tt.want)
		}
	}
}

func TestFetchMirrorsUsesOneRequestAndReturnsList(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "xbpretty/") {
			t.Errorf("User-Agent = %q, want xbpretty/<version>", got)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("EU\thttps://mirror.example/void/\tHelsinki, Finland\t1\n"))
	}))
	defer srv.Close()

	mirrors, err := fetch.FetchMirrors(context.Background(), srv.Client(), srv.URL, "xbpretty/test")
	if err != nil {
		t.Fatalf("FetchMirrors: %v", err)
	}
	if len(mirrors) != 1 || mirrors[0].URL != "https://mirror.example/void" {
		t.Fatalf("FetchMirrors = %+v", mirrors)
	}
	if requests.Load() != 1 {
		t.Errorf("made %d requests, want 1", requests.Load())
	}
	// A second call is another network request: there is no runtime cache.
	if _, err := fetch.FetchMirrors(context.Background(), srv.Client(), srv.URL, "xbpretty/test"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Errorf("made %d requests after two calls, want 2", requests.Load())
	}
}

func TestFetchMirrorsFailureAndEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := fetch.FetchMirrors(context.Background(), srv.Client(), srv.URL, "xbpretty/test"); err == nil {
		t.Error("FetchMirrors accepted a 503")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# nothing here\n"))
	}))
	defer empty.Close()
	if _, err := fetch.FetchMirrors(context.Background(), empty.Client(), empty.URL, "xbpretty/test"); err == nil {
		t.Error("FetchMirrors accepted an empty list")
	}
}

func TestFetchMirrorsHonoursContextTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := fetch.FetchMirrors(ctx, srv.Client(), srv.URL, "xbpretty/test"); err == nil {
		t.Fatal("FetchMirrors ignored the context deadline")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("FetchMirrors took %v, want it to stop at the context deadline", elapsed)
	}
}

func TestRewriteToHTTPS(t *testing.T) {
	got := fetch.RewriteToHTTPS(fetch.Mirror{URL: "http://mirror.example/void"})
	if got.URL != "https://mirror.example/void" {
		t.Errorf("RewriteToHTTPS = %q, want https", got.URL)
	}
	already := fetch.Mirror{URL: "https://mirror.example/void"}
	if fetch.RewriteToHTTPS(already) != already {
		t.Errorf("RewriteToHTTPS changed an https mirror: %+v", fetch.RewriteToHTTPS(already))
	}
}

func TestParseMirrorsReportsSkippedRows(t *testing.T) {
	raw := strings.Join([]string{
		"EU\thttps://good.example/void\tHelsinki, Finland\t1",
		"only three fields",
		"\thttps://blank.example/void\tNowhere\t1",
		"EU\thttps://tier.example/void\tNowhere\tnot-a-number",
		"EU\tftp://ftp.example/void\tNowhere\t1",
	}, "\n")
	mirrors, warnings, err := fetch.ParseMirrorsDetailed(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(mirrors) != 1 {
		t.Fatalf("mirrors = %+v, want the one valid row", mirrors)
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings = %+v, want four skipped rows", warnings)
	}
	for _, warning := range warnings {
		if !strings.Contains(warning.String(), "line ") {
			t.Errorf("warning = %q, want a line number", warning)
		}
	}
}

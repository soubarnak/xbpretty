package fetch_test

import (
	"strings"
	"testing"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

func TestFormatSpeedBinaryPrecisionOne(t *testing.T) {
	for _, tt := range []struct {
		bps  float64
		want string
	}{
		{0, "0.0 B/s"},
		{512, "512.0 B/s"},
		{1023, "1023.0 B/s"},
		{1024, "1.0 KiB/s"},
		{1024 * 1024 * 1.25, "1.2 MiB/s"},
		{1024 * 1024 * 1024 * 2, "2.0 GiB/s"},
	} {
		if got := fetch.FormatSpeed(tt.bps); got != tt.want {
			t.Errorf("FormatSpeed(%v) = %q, want %q", tt.bps, got, tt.want)
		}
	}
}

func TestRenderTable(t *testing.T) {
	results := []fetch.Result{
		{
			Mirror:    fetch.Mirror{Region: "EU", URL: "https://repo-fi.voidlinux.org", Location: "Helsinki, Finland", Tier: 1},
			LatencyMS: 12,
			Speed:     1024 * 1024,
			ScoreMS:   13,
		},
		{
			Mirror:    fetch.Mirror{Region: "EU", URL: "http://ftp.dk.xemacs.org/voidlinux", Location: "", Tier: 2},
			LatencyMS: 120,
			Speed:     512 * 1024,
			ScoreMS:   130,
		},
	}
	var b strings.Builder
	if err := fetch.RenderTable(&b, results, "https://repo-fi.voidlinux.org"); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	want := strings.Join([]string{
		"Mirror                              Region  Location           Tier  Latency        Speed   Score  Chosen",
		"https://repo-fi.voidlinux.org       EU      Helsinki, Finland     1    12 ms    1.0 MiB/s   13 ms  *",
		"http://ftp.dk.xemacs.org/voidlinux  EU      —                     2   120 ms  512.0 KiB/s  130 ms",
		"",
	}, "\n")
	if b.String() != want {
		t.Errorf("RenderTable output:\n%q\nwant:\n%q", b.String(), want)
	}
}

func TestRenderTableNoChosenRow(t *testing.T) {
	results := []fetch.Result{{
		Mirror:    fetch.Mirror{Region: "EU", URL: "https://a", Location: "X", Tier: 1},
		LatencyMS: 1, Speed: 2048, ScoreMS: 2,
	}}
	var b strings.Builder
	if err := fetch.RenderTable(&b, results, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "*") {
		t.Errorf("table marked a chosen row when none was chosen:\n%s", b.String())
	}
}

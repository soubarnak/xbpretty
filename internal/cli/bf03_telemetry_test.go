package cli

import (
	"testing"
)

// repoStartLine is one `[*] Updating repository` line as xbps 0.60 prints it:
// the URL ends in the arch-specific repodata file.
const repoStartLine = "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ..."

// TestRepoFetchedOnBothStreams: xbps prints a repodata completion on stdout
// when stdout is not a TTY and on stderr when it is, so the parser must read
// both (BF-03).
func TestRepoFetchedOnBothStreams(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			p := NewStreamParser(0, 0)
			var fetched []string
			var sizes []int64
			p.OnRepoFetched = func(file string, size int64) {
				fetched = append(fetched, file)
				sizes = append(sizes, size)
			}
			var pkgCalls int
			p.OnDownloadComplete = func(DownloadComplete) { pkgCalls++ }

			p.FeedStdout(repoStartLine)
			feed := p.FeedStdout
			if stream == "stderr" {
				feed = p.FeedStderr
			}
			if !feed("x86_64-repodata: 1854KB [avg rate: 2391KB/s]") {
				t.Errorf("the repodata completion was not consumed")
			}
			if len(fetched) != 1 || fetched[0] != "x86_64-repodata" {
				t.Fatalf("OnRepoFetched calls = %q, want one for x86_64-repodata", fetched)
			}
			if want := int64(1854 * 1024); sizes[0] != want {
				t.Errorf("fetched size = %d, want %d", sizes[0], want)
			}
			if pkgCalls != 0 {
				t.Errorf("a repodata completion reached the package download path")
			}
		})
	}
}

// TestRepoTickIsNotADownloadTick: a repodata transfer tick feeds the repository
// view, never the package download counters.
func TestRepoTickIsNotADownloadTick(t *testing.T) {
	p := NewStreamParser(0, 0)
	var ticks []DownloadTick
	var downloads int
	p.OnRepoTick = func(tick DownloadTick) { ticks = append(ticks, tick) }
	p.OnDownloadTick = func(DownloadTick) { downloads++ }

	if !p.FeedStderr("[ 40%] x86_64-repodata: [1854KB 40%] 2391KB/s ETA: 00m01s") {
		t.Fatal("the repodata tick was not consumed")
	}
	if len(ticks) != 1 {
		t.Fatalf("OnRepoTick calls = %d, want 1", len(ticks))
	}
	tick := ticks[0]
	if tick.Filename != "x86_64-repodata" || tick.FilePercent != 40 {
		t.Errorf("tick = %+v, want x86_64-repodata at 40%%", tick)
	}
	if want := int64(1854 * 1024); tick.TotalSizeBytes != want {
		t.Errorf("tick total = %d, want %d", tick.TotalSizeBytes, want)
	}
	if downloads != 0 {
		t.Errorf("a repodata tick reached the package download path")
	}
}

// TestRepoErrorRegex: a failed repository fetch names the URL and the reason.
// It is an stderr line (xbps writes it there on both TTY and pipe), and xbps
// has quoted the URL with a backtick in some releases, so both quotes parse.
func TestRepoErrorRegex(t *testing.T) {
	cases := []struct {
		line, url, reason string
	}{
		{
			"ERROR: [reposync] failed to fetch file `https://repo-default.voidlinux.org/current/x86_64-repodata': Connection refused",
			"https://repo-default.voidlinux.org/current/x86_64-repodata",
			"Connection refused",
		},
		{
			"ERROR: [reposync] failed to fetch file 'https://mirror.example/void/current/x86_64-repodata': 404 Not Found",
			"https://mirror.example/void/current/x86_64-repodata",
			"404 Not Found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			p := NewStreamParser(0, 0)
			var gotURL, gotReason string
			var calls int
			p.OnRepoError = func(url, reason string) {
				calls++
				gotURL, gotReason = url, reason
			}
			if !p.FeedStderr(tc.line) {
				t.Fatal("the repository error was not consumed")
			}
			if calls != 1 || gotURL != tc.url || gotReason != tc.reason {
				t.Errorf("OnRepoError(%q, %q) calls=%d, want (%q, %q)", gotURL, gotReason, calls, tc.url, tc.reason)
			}
		})
	}
	t.Run("other errors are not repository errors", func(t *testing.T) {
		p := NewStreamParser(0, 0)
		p.OnRepoError = func(string, string) { t.Error("a plain ERROR line was read as a repository error") }
		if p.FeedStderr("ERROR: failed to open repodata: /var/db/xbps/x86_64-repodata") {
			t.Error("a plain ERROR line was consumed")
		}
	})
}

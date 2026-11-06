//go:build linux

package cli

import (
	"strings"
	"testing"
)

// bf03SyncBody is a two-repository sync. The first repository is unchanged; the
// second fetches its repodata, with a transfer tick on stderr and the completion
// line. Sleeps keep the stdout and stderr writes in xbps's order.
const bf03SyncBody = `
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/x86_64-repodata' ...\n"
    sleep 0.2
    printf "[*] Updating repository 'https://repo-default.voidlinux.org/current/nonfree/x86_64-repodata' ...\n"
    sleep 0.1
    printf "[ 40%%] x86_64-repodata: [1854KB 40%%] 2391KB/s ETA: 00m01s\033[K\r" >&2
    sleep 0.1
    printf "x86_64-repodata: 1854KB [avg rate: 2391KB/s]\033[K\n" >&2
    exit 0`

// TestUpgradeSyncPrintsRepositoryLinesAboveWidget is BF-03 on a real terminal:
// the per-repository status lines land above the widget, the widget is cleared,
// then the Fetched line, the count line and the summary follow in that order.
func TestUpgradeSyncPrintsRepositoryLinesAboveWidget(t *testing.T) {
	bin, env := bf02SyncEnv(t, bf03SyncBody)
	p := startPtyAll(t, bin, env, []string{"upgrade", "--dry-run"}, 90, 30)
	code := p.exitCode(t)
	out := p.cap.String()
	if code != 0 {
		t.Fatalf("exit = %d\n%q", code, out)
	}
	if strings.Contains(out, "[*] Updating repository") {
		t.Errorf("the raw repository lines reached the terminal:\n%q", out)
	}

	noChangeAt := strings.Index(out, "No Change: https://repo-default.voidlinux.org/current x86_64-repodata")
	updatedAt := strings.Index(out, "Updated: https://repo-default.voidlinux.org/current/nonfree x86_64-repodata [1.8 MiB]")
	fetchedAt := strings.Index(out, "Fetched 1.8 MiB in ")
	countAt := strings.Index(out, "3 packages can be upgraded. Run 'xbp list --upgradable' to see them.")
	summaryAt := strings.Index(out, " Upgrade\r\n")
	if noChangeAt < 0 || updatedAt < 0 || fetchedAt < 0 || countAt < 0 || summaryAt < 0 {
		t.Fatalf("missing a line (no-change=%d updated=%d fetched=%d count=%d summary=%d):\n%q",
			noChangeAt, updatedAt, fetchedAt, countAt, summaryAt, out)
	}
	if !(noChangeAt < updatedAt && updatedAt < fetchedAt && fetchedAt < countAt && countAt < summaryAt) {
		t.Errorf("order no-change=%d updated=%d fetched=%d count=%d summary=%d; want ascending:\n%q",
			noChangeAt, updatedAt, fetchedAt, countAt, summaryAt, out)
	}
	if clearAt := strings.LastIndex(out[:fetchedAt], "\x1b[J"); clearAt < updatedAt {
		t.Errorf("the widget was not cleared between the status lines and the Fetched line:\n%q", out)
	}
}

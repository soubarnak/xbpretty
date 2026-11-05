//go:build xbpretty_testroot

package cli

import (
	"os"
	"strconv"
	"strings"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

// Fetch test hook, compiled only with the xbpretty_testroot build tag. It lets
// the built-binary tests point fetch at a local fixture without adding any
// escape hatch to the release binary (see docs/adr/0015 and issue #73).
//
//	XBPRETTY_TEST_FETCH_XBPSD=<dir>       read repository= lines from <dir>
//	XBPRETTY_TEST_FETCH_MIRROR_LIST=<url> fetch the mirror list from <url>
//	XBPRETTY_TEST_FETCH_ARCH=<arch>       probe <arch>-repodata
//	XBPRETTY_TEST_FETCH_WRITE_DIR=<dir>   write masking files under <dir>
//	XBPRETTY_TEST_FETCH_MANIFEST=<path>   read/write the fetch manifest at <path>
//	XBPRETTY_TEST_FETCH_RANKED=<file>     replace the ranked rows with the file:
//	                                      url|location|region|tier|latencyMS|speedBytes|scoreMS
func init() {
	if v := os.Getenv("XBPRETTY_TEST_FETCH_XBPSD"); v != "" {
		fetchXBPSDDirs = []string{v}
	}
	if v := os.Getenv("XBPRETTY_TEST_FETCH_MIRROR_LIST"); v != "" {
		fetchMirrorListURL = v
	}
	if v := os.Getenv("XBPRETTY_TEST_FETCH_ARCH"); v != "" {
		fetchArch = v
	}
	if v := os.Getenv("XBPRETTY_TEST_FETCH_WRITE_DIR"); v != "" {
		fetchWriteDir = v
	}
	if v := os.Getenv("XBPRETTY_TEST_FETCH_MANIFEST"); v != "" {
		fetchManifestPath = v
	}
	if path := os.Getenv("XBPRETTY_TEST_FETCH_RANKED"); path != "" {
		if rows, err := readRankedFixture(path); err == nil {
			fetchRankedHook = func([]fetch.Result) []fetch.Result { return rows }
		}
	}
}

// readRankedFixture parses the PTY golden's deterministic ranked rows.
func readRankedFixture(path string) ([]fetch.Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []fetch.Result
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 7 {
			return nil, os.ErrInvalid
		}
		tier, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, err
		}
		latency, err := strconv.Atoi(parts[4])
		if err != nil {
			return nil, err
		}
		speed, err := strconv.ParseFloat(parts[5], 64)
		if err != nil {
			return nil, err
		}
		score, err := strconv.Atoi(parts[6])
		if err != nil {
			return nil, err
		}
		rows = append(rows, fetch.Result{
			Mirror:    fetch.Mirror{URL: parts[0], Location: parts[1], Region: parts[2], Tier: tier},
			LatencyMS: latency,
			Speed:     speed,
			ScoreMS:   score,
		})
	}
	return rows, nil
}

package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"sort"
	"time"
)

const (
	// ProbeRangeBytes is FR-044's 256 KiB ranged read.
	ProbeRangeBytes = 262144
	// DefaultProbeConcurrency is FR-044's ten concurrent probes.
	DefaultProbeConcurrency = 10

	defaultProbeTimeout = 5 * time.Second
	defaultDialTimeout  = 2 * time.Second
	defaultTLSTimeout   = 2 * time.Second
)

// Result is one successful probe.
type Result struct {
	Mirror
	LatencyMS int     // time to first byte, rounded up from zero to at least 1
	ScoreMS   int     // total probe time, rounded up from zero to at least 1
	Speed     float64 // bytes per second while the body arrived
	Bytes     int64
}

// Failure is one mirror whose probe did not produce valid repodata.
type Failure struct {
	Mirror Mirror
	Err    error
}

// ProbeReport is the outcome of one probe round.
type ProbeReport struct {
	Results  []Result
	Failures []Failure
}

// ProgressEvent is the MirrorScore progress payload consumed by #66. Rendering
// belongs to that ticket; this package only emits the values.
type ProgressEvent struct {
	Finished string        // URL of the most recent successful probe
	Done     int           // candidates completed so far
	Total    int           // candidates in this round
	Elapsed  time.Duration // since the round started
	Ratio    float64       // Done/Total
}

// ProgressFunc receives MirrorScore progress events.
type ProgressFunc func(ProgressEvent)

// ProbeOptions configures one probe round.
type ProbeOptions struct {
	Client       *http.Client
	Mirrors      []Mirror
	PrimaryPath  string // repository path suffix, e.g. /current
	Arch         string // xbps architecture name, e.g. x86_64
	UserAgent    string
	Concurrency  int
	ProbeTimeout time.Duration
	Progress     ProgressFunc
}

// ProbeAll probes every mirror, at most ten at a time, and returns successful
// results in completion order. It never retries (FR-044).
func ProbeAll(ctx context.Context, opts ProbeOptions) ProbeReport {
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultProbeConcurrency
	}
	timeout := opts.ProbeTimeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	client := opts.Client
	if client == nil {
		client = NewHTTPClient()
	}
	total := len(opts.Mirrors)
	if total == 0 {
		return ProbeReport{}
	}

	type outcome struct {
		result  *Result
		failure *Failure
	}
	jobs := make(chan Mirror)
	done := make(chan outcome)
	start := time.Now()

	workers := concurrency
	if workers > total {
		workers = total
	}
	for i := 0; i < workers; i++ {
		go func() {
			for mirror := range jobs {
				result, err := Probe(ctx, client, mirror, opts.PrimaryPath, opts.Arch, opts.UserAgent, timeout)
				if err != nil {
					done <- outcome{failure: &Failure{Mirror: mirror, Err: err}}
					continue
				}
				done <- outcome{result: &result}
			}
		}()
	}
	go func() {
		for _, mirror := range opts.Mirrors {
			jobs <- mirror
		}
		close(jobs)
	}()

	var report ProbeReport
	var finished string
	for completed := 1; completed <= total; completed++ {
		out := <-done
		switch {
		case out.result != nil:
			report.Results = append(report.Results, *out.result)
			finished = out.result.URL
		case out.failure != nil:
			report.Failures = append(report.Failures, *out.failure)
		}
		if opts.Progress != nil {
			opts.Progress(ProgressEvent{
				Finished: finished,
				Done:     completed,
				Total:    total,
				Elapsed:  time.Since(start),
				Ratio:    float64(completed) / float64(total),
			})
		}
	}
	return report
}

// Probe performs one ranged GET and validates the zstd repodata magic.
func Probe(ctx context.Context, client *http.Client, mirror Mirror, primaryPath, arch, userAgent string, timeout time.Duration) (Result, error) {
	if client == nil {
		client = NewHTTPClient()
	}
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	target := RepositoryFileURL(mirror.URL, primaryPath, arch)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", ProbeRangeBytes-1))
	req.Header.Set("Accept-Encoding", "identity")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	var firstByte time.Time
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() { firstByte = time.Now() },
	}
	req = req.WithContext(httptrace.WithClientTrace(probeCtx, trace))

	tStart := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, probeError(err)
	}
	defer resp.Body.Close()
	tFirst := firstByte
	if tFirst.IsZero() {
		tFirst = time.Now()
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return Result{}, fmt.Errorf("unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, ProbeRangeBytes))
	if err != nil {
		return Result{}, probeError(err)
	}
	tEnd := time.Now()
	if len(body) < 4 {
		return Result{}, fmt.Errorf("body is too short for zstd repodata (%d bytes)", len(body))
	}
	if !IsZstdRepodata(body) {
		return Result{}, fmt.Errorf("body is not zstd repodata (magic % X)", body[:4])
	}

	latencyMS := roundedMilliseconds(tFirst.Sub(tStart))
	scoreMS := roundedMilliseconds(tEnd.Sub(tStart))
	elapsed := tEnd.Sub(tFirst)
	if elapsed < time.Millisecond {
		elapsed = time.Millisecond
	}
	return Result{
		Mirror:    mirror,
		LatencyMS: latencyMS,
		ScoreMS:   scoreMS,
		Speed:     float64(len(body)) / elapsed.Seconds(),
		Bytes:     int64(len(body)),
	}, nil
}

// IsZstdRepodata reports whether body starts with the zstd frame magic
// 28 B5 2F FD (FR-044). It deliberately does not decompress: a dependency-free
// magic check is the contract.
func IsZstdRepodata(body []byte) bool {
	return len(body) >= 4 &&
		body[0] == 0x28 && body[1] == 0xB5 && body[2] == 0x2F && body[3] == 0xFD
}

// probeError names a transport failure without the request URL noise.
func probeError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("timed out: %w", err)
	}
	return err
}

func roundedMilliseconds(d time.Duration) int {
	ms := int(math.Round(float64(d) / float64(time.Millisecond)))
	if ms < 1 {
		return 1
	}
	return ms
}

// RepositoryFileURL joins a mirror base, a repository path suffix and
// <arch>-repodata without doubling slashes.
func RepositoryFileURL(base, suffix, arch string) string {
	base = trimSlash(base)
	if suffix != "" {
		if suffix[0] != '/' {
			suffix = "/" + suffix
		}
		base += trimSlash(suffix)
	}
	return base + "/" + arch + "-repodata"
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// Rank sorts results exactly as FR-044 says: score ascending, then latency,
// then tier, then URL. There is no tier weighting.
func Rank(results []Result) []Result {
	out := make([]Result, len(results))
	copy(out, results)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ScoreMS != b.ScoreMS {
			return a.ScoreMS < b.ScoreMS
		}
		if a.LatencyMS != b.LatencyMS {
			return a.LatencyMS < b.LatencyMS
		}
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		return a.URL < b.URL
	})
	return out
}

// NewHTTPClient builds the fetch HTTP client: environment proxy, system TLS
// roots, HTTP/2 attempts, no automatic compression and FR-044's 2 s dial and
// TLS timeouts. Whole-request timeouts come from each request's context.
func NewHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: defaultDialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   defaultTLSTimeout,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 0,
	}
	return &http.Client{Transport: transport}
}

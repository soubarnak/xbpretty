package download

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Transfer defaults from FR-038.
const (
	DefaultConcurrency     = 3
	DefaultAttempts        = 4
	DefaultDialTimeout     = 30 * time.Second
	DefaultTLSTimeout      = 30 * time.Second
	DefaultHeaderTimeout   = 120 * time.Second
	DefaultIdleBodyTimeout = 120 * time.Second
)

// Version is the xbpretty version used in the default User-Agent. The CLI
// sets it from its ldflags value; the engine never imports the CLI.
var Version = "dev"

// maxBodyBytes is the sanity ceiling for an archive of unknown size. A
// response larger than this is treated as hostile metadata rather than
// copied to disk and rejected only after the hash check.
const maxBodyBytes int64 = 8 << 30

// maxSignatureBytes caps a `.sig2` body; signatures are tiny.
const maxSignatureBytes int64 = 1 << 20

// EventKind is one FR-041 download progress event.
type EventKind string

const (
	EventStart      EventKind = "start"
	EventCacheHit   EventKind = "cache-hit"
	EventFileStart  EventKind = "file-start"
	EventFileTick   EventKind = "file-tick"
	EventFileDone   EventKind = "file-done"
	EventRetry      EventKind = "retry"
	EventFileFailed EventKind = "file-failed"
	EventFinished   EventKind = "finished"
)

// Event is one progress event. #72 renders it; the engine only emits.
// OnEvent may be called from several transfer goroutines, so it is serialised.
type Event struct {
	Kind    EventKind
	Target  Target
	File    string // base name: <pkgver>.<arch>.xbps or its .sig2
	Bytes   int64  // bytes written by this write (accumulate for position)
	Total   int64  // expected bytes, -1 when unknown
	Attempt int
	Done    int // completed archives, on Start/Finished
	Failed  int // failed archives, on Start/Finished
	Err     error
}

// Mode selects the destination semantics.
type Mode int

const (
	// ModeCache fills the xbps cache: a missing metadata hash is tolerated
	// (xbps verifies the archive later) and the files are root-owned.
	ModeCache Mode = iota
	// ModeDownload writes into the current directory: the caller must have a
	// metadata hash, and ownership is left alone.
	ModeDownload
)

// Options configures one engine. Zero timeouts, attempts and concurrency take
// the FR-038 defaults.
type Options struct {
	DestDir     string
	Mode        Mode
	UserAgent   string
	Client      *http.Client
	Concurrency int
	Attempts    int

	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	HeaderTimeout       time.Duration
	IdleTimeout         time.Duration

	// ChownRoot makes cache files 0644 root:root (FR-038). It is best effort:
	// a non-root caller cannot chown and that must not fail the transfer.
	ChownRoot bool

	// Log receives verbose transfer lines: `GET <url> (attempt n/4)`.
	Log func(format string, args ...any)

	// OnEvent receives FR-041 progress events. It is serialised by the
	// engine and may be nil.
	OnEvent func(Event)
}

// Result is one target's outcome. A failed target keeps whatever verified
// files earlier targets published.
type Result struct {
	Target          Target
	ArchivePath     string
	SignaturePath   string
	ReusedArchive   bool
	ReusedSignature bool
	Err             error
}

// Engine is the shared predownload/download engine. It is safe for concurrent
// use; one Engine should serve one run.
type Engine struct {
	opts    Options
	client  *http.Client
	limiter *hostLimiter

	eventMu sync.Mutex
}

// New builds an engine with FR-038's transport defaults.
func New(opts Options) *Engine {
	if opts.Attempts <= 0 {
		opts.Attempts = DefaultAttempts
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = DefaultDialTimeout
	}
	if opts.TLSHandshakeTimeout <= 0 {
		opts.TLSHandshakeTimeout = DefaultTLSTimeout
	}
	if opts.HeaderTimeout <= 0 {
		opts.HeaderTimeout = DefaultHeaderTimeout
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = DefaultIdleBodyTimeout
	}
	// The cache is root-owned (FR-038); a cwd download belongs to its user.
	if opts.Mode == ModeCache {
		opts.ChownRoot = true
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "xbpretty/" + Version
	}
	client := opts.Client
	if client == nil {
		client = newHTTPClient(opts)
	} else if client.CheckRedirect == nil {
		// Even an injected client keeps FR-038's redirect policy.
		copied := *client
		copied.CheckRedirect = checkRedirect
		client = &copied
	}
	return &Engine{
		opts:    opts,
		client:  client,
		limiter: newHostLimiter(DefaultConcurrency),
	}
}

// NewHTTPClient builds the download transport: environment proxy, system TLS
// roots, HTTP/2 attempted, compression disabled, 30 s dial and TLS, 120 s
// header and body-stall timeouts, and no https→http redirect (FR-038).
func NewHTTPClient(opts Options) *http.Client {
	return newHTTPClient(opts)
}

// emit serialises one progress event to the hook.
func (e *Engine) emit(ev Event) {
	if e.opts.OnEvent == nil {
		return
	}
	e.eventMu.Lock()
	defer e.eventMu.Unlock()
	e.opts.OnEvent(ev)
}

func newHTTPClient(opts Options) *http.Client {
	dialer := &net.Dialer{Timeout: opts.DialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   opts.TLSHandshakeTimeout,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   DefaultConcurrency,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: opts.HeaderTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
}

var (
	errRedirectDowngrade = errors.New("refusing to follow an https to http redirect")
	errTooManyRedirects  = errors.New("stopped after 10 redirects")
	errIdleBodyTimeout   = errors.New("body stalled")
)

func checkRedirect(req *http.Request, via []*http.Request) error {
	// via holds the requests already made: one initial request plus one per
	// followed redirect. Allow exactly MaxRedirects hops.
	if len(via) > MaxRedirects {
		return errTooManyRedirects
	}
	previous := via[len(via)-1]
	if previous.URL.Scheme == "https" && req.URL.Scheme == "http" {
		return errRedirectDowngrade
	}
	return nil
}

// Fetch transfers every target and returns one result per target in plan
// order. A target's signature is fetched before its archive; at most three
// transfers run per URL host:port and there is no global cap unless Options
// constrains it.
func (e *Engine) Fetch(ctx context.Context, targets []Target) []Result {
	e.emit(Event{Kind: EventStart})
	results := make([]Result, len(targets))
	var wg sync.WaitGroup
	var slots chan struct{}
	if e.opts.Concurrency > 0 {
		slots = make(chan struct{}, e.opts.Concurrency)
	}
	for i := range targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if slots != nil {
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					results[i] = Result{Target: targets[i], Err: ctx.Err()}
					return
				}
			}
			results[i] = e.fetchOne(ctx, targets[i])
		}(i)
	}
	wg.Wait()
	done, failed := 0, 0
	for _, result := range results {
		if result.Err != nil {
			failed++
		} else {
			done++
		}
	}
	e.emit(Event{Kind: EventFinished, Done: done, Failed: failed})
	return results
}

// FetchOne transfers one target. `download` uses it to keep plan order and
// abort on the first failure; predownload uses Fetch for the parallel fill.
func (e *Engine) FetchOne(ctx context.Context, target Target) Result {
	e.emit(Event{Kind: EventStart})
	result := e.fetchOne(ctx, target)
	done, failed := 0, 0
	if result.Err != nil {
		failed = 1
	} else {
		done = 1
	}
	e.emit(Event{Kind: EventFinished, Done: done, Failed: failed})
	return result
}

// fetchOne runs the cache scan then the transfers for one target.
func (e *Engine) fetchOne(ctx context.Context, target Target) Result {
	dest := e.opts.DestDir
	if dest == "" {
		dest = "."
	}
	res := Result{
		Target:        target,
		ArchivePath:   filepath.Join(dest, target.Filename),
		SignaturePath: filepath.Join(dest, target.Filename+".sig2"),
	}
	if ctx.Err() != nil {
		res.Err = ctx.Err()
		return res
	}
	if IsFTPRepository(target.Repository) {
		// Cache mode filters ftp rows before target building (TargetsFromRows);
		// this text must never claim to be about `xbp download` there.
		if e.opts.Mode == ModeCache {
			res.Err = fmt.Errorf("%s: ftp:// repositories are excluded from predownload", target.Pkgver)
		} else {
			res.Err = fmt.Errorf("%s: ftp:// repositories are not supported by xbp download", target.Pkgver)
		}
		return res
	}
	localPath, local, err := localSource(target.URL)
	if err != nil {
		res.Err = err
		return res
	}
	if local {
		res.Err = e.copyLocal(ctx, target, res, localPath)
		return res
	}

	needSig, needArchive, err := e.scan(ctx, target, res)
	if err != nil {
		res.Err = err
		return res
	}
	res.ReusedSignature = !needSig
	res.ReusedArchive = !needArchive
	if !needArchive {
		e.emit(Event{Kind: EventCacheHit, Target: target, File: target.Filename,
			Bytes: int64(target.Size), Total: int64(target.Size)})
	}

	if needSig {
		if err := e.transfer(ctx, target, target.SignatureURL, res.SignaturePath, "", true); err != nil {
			res.Err = err
			return res
		}
		res.ReusedSignature = false
	}
	if needArchive {
		if err := e.transfer(ctx, target, target.URL, res.ArchivePath, target.SHA256, false); err != nil {
			res.Err = err
			return res
		}
		res.ReusedArchive = false
	}
	return res
}

// scan applies FR-038's cache rules: a matching pair is a hit, a missing
// signature is fetched alone, a missing archive is fetched as a pair, and a
// hash mismatch deletes both so they are refetched.
func (e *Engine) scan(ctx context.Context, target Target, res Result) (needSig, needArchive bool, err error) {
	archiveExists := isRegularFile(res.ArchivePath)
	sigExists := isRegularFile(res.SignaturePath)
	if !archiveExists {
		return true, true, nil
	}
	if target.SHA256 != "" {
		got, err := fileSHA256(res.ArchivePath)
		if err != nil {
			return false, false, fmt.Errorf("%s: %w", target.Filename, err)
		}
		if !strings.EqualFold(got, target.SHA256) {
			if e.opts.Mode == ModeCache {
				// The cache may delete and refetch; a real transaction falls
				// back to xbps anyway.
				_ = os.Remove(res.ArchivePath)
				_ = os.Remove(res.SignaturePath)
			}
			// In download mode the old file stays until the atomic rename
			// publishes the verified replacement, so a failed refetch never
			// destroys it.
			return true, true, nil
		}
	}
	if !sigExists {
		return true, false, nil
	}
	return false, false, nil
}

// copyLocal implements FR-037's local/file:// behaviour: copy the archive and
// a signature when present, verifying the SHA-256 before publishing.
func (e *Engine) copyLocal(ctx context.Context, target Target, res Result, source string) error {
	needSig, needArchive, err := e.scan(ctx, target, res)
	if err != nil {
		return err
	}
	res.ReusedSignature = !needSig
	res.ReusedArchive = !needArchive
	if !needArchive {
		e.emit(Event{Kind: EventCacheHit, Target: target, File: target.Filename,
			Bytes: int64(target.Size), Total: int64(target.Size)})
	}
	if needArchive {
		total := e.localTotal(source, target.Size)
		e.emit(Event{Kind: EventFileStart, Target: target, File: target.Filename, Total: total})
		// copyFile already names the destination in its errors; wrapping it
		// again would print the filename twice.
		n, err := e.copyFile(source, res.ArchivePath, target.SHA256, total)
		if err != nil {
			e.emit(Event{Kind: EventFileFailed, Target: target, File: target.Filename, Err: err})
			return err
		}
		e.emit(Event{Kind: EventFileDone, Target: target, File: target.Filename, Bytes: n, Total: total})
		res.ReusedArchive = false
	}
	if needSig {
		// A local repository's signature is optional; "when present" is the
		// whole contract.
		if isRegularFile(source + ".sig2") {
			file := target.Filename + ".sig2"
			total := e.localTotal(source+".sig2", 0)
			e.emit(Event{Kind: EventFileStart, Target: target, File: file, Total: total})
			n, err := e.copyFile(source+".sig2", res.SignaturePath, "", total)
			if err != nil {
				e.emit(Event{Kind: EventFileFailed, Target: target, File: file, Err: err})
				return fmt.Errorf("%s.sig2: %w", target.Filename, err)
			}
			e.emit(Event{Kind: EventFileDone, Target: target, File: file, Bytes: n, Total: total})
			res.ReusedSignature = false
		}
	}
	return nil
}

// localTotal is the expected byte count for a local copy: the plan size when
// known, else the source's own size.
func (e *Engine) localTotal(source string, planSize uint64) int64 {
	if planSize > 0 {
		return int64(planSize)
	}
	if info, err := os.Stat(source); err == nil {
		return info.Size()
	}
	return -1
}

// copyFile copies a local archive to `<dest>.part`, verifies it, then
// publishes it with the same atomic rename the HTTP path uses.
func (e *Engine) copyFile(source, dest, wantSHA string, total int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	in, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	part := dest + ".part"
	out, err := openPart(part)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = out.Close()
			_ = os.Remove(part)
		}
	}()
	hash := sha256.New()
	tick := &tickWriter{w: out, total: total, emit: func(written, total int64) {
		e.emit(Event{Kind: EventFileTick, File: filepath.Base(dest), Bytes: written, Total: total})
	}}
	writer := io.Writer(tick)
	if wantSHA != "" {
		writer = io.MultiWriter(tick, hash)
	}
	n, err := io.Copy(writer, in)
	if err != nil {
		return 0, err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if wantSHA != "" && !strings.EqualFold(got, wantSHA) {
		return 0, fmt.Errorf("%s: SHA-256 mismatch: expected %s, got %s", filepath.Base(dest), wantSHA, got)
	}
	if err := out.Sync(); err != nil {
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	if err := publish(part, dest, e.opts.ChownRoot); err != nil {
		return 0, err
	}
	ok = true
	return n, nil
}

// transfer runs one URL with four attempts and no backoff, retrying only the
// classes FR-038 allows.
func (e *Engine) transfer(ctx context.Context, target Target, sourceURL, dest, wantSHA string, signature bool) error {
	file := filepath.Base(dest)
	total := int64(-1)
	if !signature && target.Size > 0 {
		total = int64(target.Size)
	}
	e.emit(Event{Kind: EventFileStart, Target: target, File: file, Total: total})
	var lastErr error
	for attempt := 1; attempt <= e.opts.Attempts; attempt++ {
		if ctx.Err() != nil {
			e.emit(Event{Kind: EventFileFailed, Target: target, File: file, Attempt: attempt, Err: ctx.Err()})
			return ctx.Err()
		}
		if e.opts.Log != nil {
			e.opts.Log("GET %s (attempt %d/%d)", sourceURL, attempt, e.opts.Attempts)
		}
		n, err := e.attempt(ctx, target, sourceURL, dest, wantSHA, signature, attempt, total)
		if err == nil {
			done := n
			if total > 0 {
				done = total
			}
			e.emit(Event{Kind: EventFileDone, Target: target, File: file, Bytes: done, Total: total, Attempt: attempt})
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			e.emit(Event{Kind: EventFileFailed, Target: target, File: file, Attempt: attempt, Err: err})
			return ctx.Err()
		}
		if !retryable(err) {
			e.emit(Event{Kind: EventFileFailed, Target: target, File: file, Attempt: attempt, Err: err})
			return err
		}
		e.emit(Event{Kind: EventRetry, Target: target, File: file, Attempt: attempt, Err: err})
	}
	e.emit(Event{Kind: EventFileFailed, Target: target, File: file, Attempt: e.opts.Attempts, Err: lastErr})
	return lastErr
}

// attempt is one transfer: semaphore, GET, stream to `.part`, hash, fsync and
// atomic rename. The body is capped by the plan size (or a sanity ceiling) so
// a hostile server cannot fill the disk before the hash check.
func (e *Engine) attempt(ctx context.Context, target Target, sourceURL, dest, wantSHA string, signature bool, attempt int, total int64) (int64, error) {
	limit, err := bodyLimit(target, signature)
	if err != nil {
		return 0, err
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return 0, err
	}
	release, err := e.limiter.acquire(ctx, parsed.Host)
	if err != nil {
		return 0, err
	}
	defer release()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	part := dest + ".part"
	out, err := openPart(part)
	if err != nil {
		return 0, err
	}
	published := false
	defer func() {
		if !published {
			_ = out.Close()
			_ = os.Remove(part)
		}
	}()

	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	idle := time.AfterFunc(e.opts.IdleTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer idle.Stop()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return 0, err
	}
	if e.opts.UserAgent != "" {
		req.Header.Set("User-Agent", e.opts.UserAgent)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		if stalled.Load() {
			return 0, errIdleBodyTimeout
		}
		return 0, err
	}
	defer resp.Body.Close()
	file := filepath.Base(dest)
	if resp.StatusCode != http.StatusOK {
		status := &httpStatusError{Code: resp.StatusCode, Text: resp.Status}
		if signature && resp.StatusCode == http.StatusNotFound {
			return 0, fmt.Errorf("%s.sig2: 404 Not Found; the repository no longer serves the signature", target.Filename)
		}
		return 0, fmt.Errorf("%s: %w", file, status)
	}
	if resp.ContentLength < -1 {
		return 0, fmt.Errorf("%s: malformed Content-Length %d", file, resp.ContentLength)
	}
	if resp.ContentLength > limit {
		return 0, fmt.Errorf("%s: response Content-Length %d exceeds the %d-byte limit", file, resp.ContentLength, limit)
	}
	if total < 0 && resp.ContentLength >= 0 {
		total = resp.ContentLength
	}

	hash := sha256.New()
	body := &idleReader{r: resp.Body, timer: idle, timeout: e.opts.IdleTimeout}
	tick := &tickWriter{w: out, total: total, emit: func(written, total int64) {
		e.emit(Event{Kind: EventFileTick, Target: target, File: file, Bytes: written, Total: total, Attempt: attempt})
	}}
	writer := io.Writer(tick)
	if wantSHA != "" {
		writer = io.MultiWriter(tick, hash)
	}
	// One extra byte detects an over-long body even when the server hides its
	// length behind chunked encoding.
	n, err := io.Copy(writer, io.LimitReader(body, limit+1))
	if err != nil {
		if stalled.Load() {
			return 0, errIdleBodyTimeout
		}
		return 0, err
	}
	if n > limit {
		return 0, fmt.Errorf("%s: response exceeded the %d-byte limit", file, limit)
	}
	if wantSHA != "" {
		got := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(got, wantSHA) {
			return 0, fmt.Errorf("%s: SHA-256 mismatch: expected %s, got %s", file, wantSHA, got)
		}
	}
	if err := out.Sync(); err != nil {
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	if err := publish(part, dest, e.opts.ChownRoot); err != nil {
		return 0, err
	}
	published = true
	return n, nil
}

// bodyLimit is the maximum response body the engine will write: the plan's
// exact size when known, otherwise a sanity ceiling. Signatures get a small
// fixed cap.
func bodyLimit(target Target, signature bool) (int64, error) {
	if signature {
		return maxSignatureBytes, nil
	}
	if target.Size == 0 {
		return maxBodyBytes, nil
	}
	if target.Size > uint64(maxBodyBytes) {
		return 0, fmt.Errorf("%s: implausible download size %d bytes", target.Filename, target.Size)
	}
	return int64(target.Size), nil
}

// tickWriter reports progress after every successful write.
type tickWriter struct {
	w     io.Writer
	total int64
	emit  func(written, total int64)
}

func (t *tickWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 && t.emit != nil {
		t.emit(int64(n), t.total)
	}
	return n, err
}

// httpStatusError is one non-200 answer, with FR-038's retry classes.
type httpStatusError struct {
	Code int
	Text string
}

func (e *httpStatusError) Error() string { return e.Text }

func (e *httpStatusError) retryable() bool {
	return e.Code == http.StatusRequestTimeout || e.Code == http.StatusTooManyRequests || e.Code >= 500
}

// retryable reports whether another attempt could change the outcome.
func retryable(err error) bool {
	var status *httpStatusError
	if errors.As(err, &status) {
		return status.retryable()
	}
	if errors.Is(err, errRedirectDowngrade) || errors.Is(err, errTooManyRedirects) {
		return false
	}
	if errors.Is(err, errIdleBodyTimeout) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// idleReader re-arms the idle watchdog every time the body makes progress.
type idleReader struct {
	r       io.Reader
	timer   *time.Timer
	timeout time.Duration
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(r.timeout)
	}
	return n, err
}

// hostLimiter enforces FR-038's three concurrent transfers per URL host:port.
type hostLimiter struct {
	mu  sync.Mutex
	sem map[string]chan struct{}
	cap int
}

func newHostLimiter(capacity int) *hostLimiter {
	return &hostLimiter{sem: map[string]chan struct{}{}, cap: capacity}
}

func (h *hostLimiter) acquire(ctx context.Context, host string) (func(), error) {
	h.mu.Lock()
	ch, ok := h.sem[host]
	if !ok {
		ch = make(chan struct{}, h.cap)
		h.sem[host] = ch
	}
	h.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// openPart creates `<dest>.part` truncated, with the cache file's mode. The
// mode is set explicitly because the umask must not change the contract.
func openPart(path string) (*os.File, error) {
	// A planted symlink must never be followed and later renamed into place.
	// Lstat catches it before the open; O_NOFOLLOW closes the race.
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// publish chmods the finished file, optionally makes it root:root, then moves
// it into place with one atomic rename (FR-038).
func publish(part, dest string, chownRoot bool) error {
	if err := os.Chmod(part, 0o644); err != nil {
		return err
	}
	if chownRoot {
		_ = os.Chown(part, 0, 0)
	}
	return os.Rename(part, dest)
}

// localSource reports whether a URL is a local path or a file:// URL and
// returns the filesystem path.
func localSource(rawURL string) (string, bool, error) {
	if strings.HasPrefix(rawURL, "file://") {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", false, err
		}
		return u.Path, true, nil
	}
	if strings.Contains(rawURL, "://") {
		return "", false, nil
	}
	return rawURL, true, nil
}

func isRegularFile(path string) bool {
	// Lstat: a symlink at the destination is not a cache hit and must never
	// be followed by the scan or the hash pass.
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

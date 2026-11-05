package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ValidationOptions configures the HEAD/GET suffix check (FR-045).
type ValidationOptions struct {
	Client    *http.Client
	BaseURL   string
	Suffixes  []string
	Arch      string
	UserAgent string
	Timeout   time.Duration
}

// Validate checks every required repository suffix on one mirror. A suffix is
// satisfied by HEAD; 405/501 falls back to a ranged GET of one byte. The first
// failing suffix fails the candidate.
func Validate(ctx context.Context, opts ValidationOptions) error {
	client := opts.Client
	if client == nil {
		client = NewHTTPClient()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	for _, suffix := range opts.Suffixes {
		if err := validateSuffix(ctx, client, opts.BaseURL, suffix, opts.Arch, opts.UserAgent, timeout); err != nil {
			return fmt.Errorf("%s: %w", suffixLabel(suffix), err)
		}
	}
	return nil
}

func validateSuffix(ctx context.Context, client *http.Client, base, suffix, arch, userAgent string, timeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	target := RepositoryFileURL(base, suffix, arch)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, target, nil)
	if err != nil {
		return err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		return nil
	case resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented:
		return validateSuffixWithGET(ctx, client, target, userAgent, timeout)
	default:
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
}

func validateSuffixWithGET(ctx context.Context, client *http.Client, target, userAgent string, timeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", "bytes=0-0")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}

func suffixLabel(suffix string) string {
	if suffix == "" {
		suffix = "/"
	}
	return suffix
}

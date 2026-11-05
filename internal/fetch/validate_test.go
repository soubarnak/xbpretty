package fetch_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soubarnak/xbpretty/internal/fetch"
)

func TestValidateUsesHEADForEverySuffix(t *testing.T) {
	var mu sync.Mutex
	var headPaths []string
	var gets atomic.Int32
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			mu.Lock()
			headPaths = append(headPaths, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			gets.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	})

	err := fetch.Validate(context.Background(), fetch.ValidationOptions{
		Client:    srv.Client(),
		BaseURL:   srv.URL,
		Suffixes:  []string{"/current", "/current/musl"},
		Arch:      "x86_64",
		UserAgent: "xbpretty/test",
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := []string{"/current/x86_64-repodata", "/current/musl/x86_64-repodata"}
	if strings.Join(headPaths, ",") != strings.Join(want, ",") {
		t.Errorf("HEAD paths = %v, want %v", headPaths, want)
	}
	if gets.Load() != 0 {
		t.Errorf("made %d GETs, want none when HEAD succeeds", gets.Load())
	}
}

func TestValidateFallsBackToRangedGETOn405And501(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var mu sync.Mutex
			var ranges []string
			srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(status)
					return
				}
				mu.Lock()
				ranges = append(ranges, r.Header.Get("Range"))
				mu.Unlock()
				w.WriteHeader(http.StatusPartialContent)
			})
			err := fetch.Validate(context.Background(), fetch.ValidationOptions{
				Client:    srv.Client(),
				BaseURL:   srv.URL,
				Suffixes:  []string{"/current"},
				Arch:      "x86_64",
				UserAgent: "xbpretty/test",
			})
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if len(ranges) != 1 || ranges[0] != "bytes=0-0" {
				t.Errorf("GET ranges = %v, want bytes=0-0", ranges)
			}
		})
	}
}

func TestValidateSkipsACandidateMissingAnySuffix(t *testing.T) {
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/musl/") {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	err := fetch.Validate(context.Background(), fetch.ValidationOptions{
		Client:    srv.Client(),
		BaseURL:   srv.URL,
		Suffixes:  []string{"/current", "/current/musl"},
		Arch:      "x86_64",
		UserAgent: "xbpretty/test",
	})
	if err == nil {
		t.Fatal("Validate accepted a mirror missing /current/musl")
	}
}

func TestValidateTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := replayServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	defer close(release)
	err := fetch.Validate(context.Background(), fetch.ValidationOptions{
		Client:    srv.Client(),
		BaseURL:   srv.URL,
		Suffixes:  []string{"/current"},
		Arch:      "x86_64",
		UserAgent: "xbpretty/test",
		Timeout:   40 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Validate ignored its timeout")
	}
}

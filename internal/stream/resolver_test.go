package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/tidal"
)

var t0 = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// stubFetcher returns manifests valid for `validity` from the fake clock and
// counts calls per track.
type stubFetcher struct {
	mu       sync.Mutex
	calls    map[string]int
	formats  [][]tidal.Format
	ctxErrs  []error
	errs     []error // returned in order, then nil
	gate     chan struct{}
	clock    *fakeClock
	validity time.Duration
	duration time.Duration
}

func newStub(clock *fakeClock) *stubFetcher {
	return &stubFetcher{calls: map[string]int{}, clock: clock, validity: time.Hour, duration: 4 * time.Minute}
}

func (s *stubFetcher) TrackManifest(ctx context.Context, id string, formats []tidal.Format) (tidal.Manifest, error) {
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[id]++
	s.formats = append(s.formats, formats)
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		if err != nil {
			return tidal.Manifest{}, err
		}
	}
	now := s.clock.now()
	return tidal.Manifest{
		TrackID: id, Formats: []tidal.Format{tidal.FormatFLAC}, Presentation: tidal.PresentationFull,
		MPD: []byte("Signature=SECRETSIG"), Duration: s.duration,
		FetchedAt: now, ExpiresAt: now.Add(s.validity), Hash: fmt.Sprint(s.calls[id]),
	}, nil
}

func (s *stubFetcher) count(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[id]
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestResolver(t *testing.T) (*Resolver, *stubFetcher, *fakeClock, *lockedWriter) {
	t.Helper()
	clock := &fakeClock{t: t0}
	stub := newStub(clock)
	logs := &lockedWriter{w: &bytes.Buffer{}}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	r, err := NewResolver(stub, config.QualityHiRes, logger)
	if err != nil {
		t.Fatal(err)
	}
	r.now = clock.now
	return r, stub, clock, logs
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func TestFormatsFor(t *testing.T) {
	tests := []struct {
		q    config.Quality
		want []tidal.Format
	}{
		{config.QualityAAC, []tidal.Format{tidal.FormatAACLC}},
		{config.QualityLossless, []tidal.Format{tidal.FormatFLAC, tidal.FormatAACLC}},
		{config.QualityHiRes, []tidal.Format{tidal.FormatFLACHires, tidal.FormatFLAC, tidal.FormatAACLC}},
	}
	for _, tt := range tests {
		got, err := FormatsFor(tt.q)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("FormatsFor(%s) = %v, %v; want %v", tt.q, got, err, tt.want)
		}
		r, err := NewResolver(newStub(&fakeClock{t: t0}), tt.q, slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.Formats(), tt.want) {
			t.Errorf("Resolver.Formats() = %v", r.Formats())
		}
	}
	if _, err := FormatsFor("max"); err == nil {
		t.Error("unknown quality accepted")
	}
	if _, err := NewResolver(nil, "max", slog.Default()); err == nil {
		t.Error("NewResolver accepted unknown quality")
	}
}

func TestResolveSendsConfiguredFormats(t *testing.T) {
	for _, q := range []config.Quality{config.QualityAAC, config.QualityLossless, config.QualityHiRes} {
		stub := newStub(&fakeClock{t: t0})
		r, _ := NewResolver(stub, q, slog.New(slog.DiscardHandler))
		if _, err := r.Resolve(context.Background(), "1"); err != nil {
			t.Fatal(err)
		}
		want, _ := FormatsFor(q)
		if !slices.Equal(stub.formats[0], want) {
			t.Errorf("%s: fetched with %v, want %v", q, stub.formats[0], want)
		}
	}
}

func TestResolveCachingAndRefetch(t *testing.T) {
	tests := []struct {
		name      string
		validity  time.Duration
		duration  time.Duration
		between   func(r *Resolver, c *fakeClock)
		wantCalls int
	}{
		{name: "fresh manifest is reused", wantCalls: 1},
		{name: "near expiry is re-fetched", between: func(r *Resolver, c *fakeClock) { c.advance(54*time.Minute + time.Second) }, wantCalls: 2},
		{name: "still enough time is reused", between: func(r *Resolver, c *fakeClock) { c.advance(53 * time.Minute) }, wantCalls: 1},
		{name: "invalidate forces a re-fetch", between: func(r *Resolver, c *fakeClock) { r.Invalidate("1") }, wantCalls: 2},
		{name: "invalidate of another track keeps the cache", between: func(r *Resolver, c *fakeClock) { r.Invalidate("2") }, wantCalls: 1},
		{name: "a track longer than the URL validity is fetched each time without looping",
			validity: 60 * time.Minute, duration: 70 * time.Minute, wantCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, stub, clock, _ := newTestResolver(t)
			if tt.validity != 0 {
				stub.validity, stub.duration = tt.validity, tt.duration
			}
			first, err := r.Resolve(context.Background(), "1")
			if err != nil {
				t.Fatal(err)
			}
			if tt.between != nil {
				tt.between(r, clock)
			}
			second, err := r.Resolve(context.Background(), "1")
			if err != nil {
				t.Fatal(err)
			}
			if n := stub.count("1"); n != tt.wantCalls {
				t.Errorf("fetches = %d, want %d", n, tt.wantCalls)
			}
			if same := first.Hash == second.Hash; same != (tt.wantCalls == 1) {
				t.Errorf("second manifest reused = %v, want %v", same, tt.wantCalls == 1)
			}
		})
	}
}

func TestResolveErrorsAreNotCached(t *testing.T) {
	r, stub, _, logs := newTestResolver(t)
	boom := fmt.Errorf("wrapped: %w", tidal.ErrConcurrentPlayback)
	stub.errs = []error{boom}

	if _, err := r.Resolve(context.Background(), "7"); !errors.Is(err, tidal.ErrConcurrentPlayback) {
		t.Fatalf("err = %v", err)
	}
	r.mu.Lock()
	cached := len(r.cache)
	r.mu.Unlock()
	if cached != 0 {
		t.Errorf("cache holds %d entries after a failed fetch, want 0", cached)
	}
	if _, err := r.Resolve(context.Background(), "7"); err != nil {
		t.Fatalf("retry err = %v", err)
	}
	if n := stub.count("7"); n != 2 {
		t.Errorf("fetches = %d, want 2", n)
	}
	waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="manifest resolved" track_id=7`) })
	out := logs.String()
	if !strings.Contains(out, `msg="manifest fetch failed" track_id=7`) {
		t.Errorf("logs missing the failure record:\n%s", out)
	}
	if strings.Contains(out, "SECRETSIG") {
		t.Errorf("log leaks the MPD:\n%s", out)
	}
}

func TestConcurrentResolveSharesOneFetch(t *testing.T) {
	r, stub, _, _ := newTestResolver(t)
	stub.gate = make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan string, 10)
	for range 10 {
		wg.Go(func() {
			m, err := r.Resolve(context.Background(), "5")
			if err != nil {
				t.Error(err)
			}
			results <- m.Hash
		})
	}
	// Let every caller reach the in-flight wait, then release the fetch.
	waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.inflight["5"] != nil })
	time.Sleep(10 * time.Millisecond)
	close(stub.gate)
	wg.Wait()
	close(results)
	for h := range results {
		if h != "1" {
			t.Errorf("caller got manifest %q, want the shared one", h)
		}
	}
	if n := stub.count("5"); n != 1 {
		t.Errorf("fetches = %d, want 1", n)
	}
}

func TestPrefetch(t *testing.T) {
	r, stub, _, _ := newTestResolver(t)
	r.Prefetch(context.Background(), "9")
	waitFor(t, func() bool { return stub.count("9") == 1 })
	waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); _, ok := r.cache["9"]; return ok })

	r.Prefetch(context.Background(), "9") // already cached: no new fetch
	if _, err := r.Resolve(context.Background(), "9"); err != nil {
		t.Fatal(err)
	}
	if n := stub.count("9"); n != 1 {
		t.Errorf("fetches = %d, want 1", n)
	}

	// A prefetch whose context is canceled right away still completes.
	ctx, cancel := context.WithCancel(context.Background())
	r.Prefetch(ctx, "10")
	cancel()
	waitFor(t, func() bool { return stub.count("10") == 1 })
	stub.mu.Lock()
	defer stub.mu.Unlock()
	for i, err := range stub.ctxErrs {
		if err != nil {
			t.Errorf("fetch %d saw a canceled context: %v", i, err)
		}
	}
}

func TestResolveCallerCancelDoesNotAbortSharedFetch(t *testing.T) {
	r, stub, _, _ := newTestResolver(t)
	stub.gate = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := r.Resolve(ctx, "3"); done <- err }()
	waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.inflight["3"] != nil })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("canceled caller err = %v", err)
	}
	close(stub.gate)
	if _, err := r.Resolve(context.Background(), "3"); err != nil {
		t.Fatal(err)
	}
	if n := stub.count("3"); n != 1 {
		t.Errorf("fetches = %d, want 1 (the shared fetch should have completed and been cached)", n)
	}
}

func TestCacheIsBounded(t *testing.T) {
	r, stub, clock, _ := newTestResolver(t)
	// One entry that expires soon, then enough to exceed the bound.
	stub.validity = 10 * time.Minute
	stub.duration = time.Minute
	if _, err := r.Resolve(context.Background(), "short"); err != nil {
		t.Fatal(err)
	}
	stub.validity = time.Hour
	for i := range maxCached {
		clock.advance(time.Second)
		if _, err := r.Resolve(context.Background(), fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	_, shortKept := r.cache["short"]
	_, firstKept := r.cache["0"]
	size := len(r.cache)
	r.mu.Unlock()
	if size != maxCached || shortKept || !firstKept {
		t.Errorf("cache size %d, short kept %v, first kept %v; want %d, false, true", size, shortKept, firstKept, maxCached)
	}

	// Expired entries are dropped on the next store.
	clock.advance(2 * time.Hour)
	if _, err := r.Resolve(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cache) != 1 {
		t.Errorf("cache size after expiry = %d, want 1", len(r.cache))
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}

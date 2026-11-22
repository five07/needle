package tidal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	tokenA = "access-token-AAAA"
	tokenB = "access-token-BBBB"
)

// stubTokens counts calls and returns scripted tokens or errors.
type stubTokens struct {
	mu           sync.Mutex
	token        string
	refreshed    string
	tokenErr     error
	refreshErr   error
	tokenCalls   int
	refreshCalls int
}

func (s *stubTokens) Token(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenCalls++
	return s.token, s.tokenErr
}

func (s *stubTokens) Refresh(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCalls++
	return s.refreshed, s.refreshErr
}

// reply is one scripted server response.
type reply struct {
	status int
	header map[string]string
	body   string
}

// recorded is what the fake server saw for one request.
type recorded struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   string
}

// fakeAPI serves scripted replies in order and records requests.
type fakeAPI struct {
	t       *testing.T
	mu      sync.Mutex
	replies []reply
	reqs    []recorded
	srv     *httptest.Server
}

func newFakeAPI(t *testing.T, replies ...reply) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, replies: replies}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.URL.Query(), r.Header.Clone(), string(body)})
	if len(f.replies) == 0 {
		f.mu.Unlock()
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	rep := f.replies[0]
	f.replies = f.replies[1:]
	f.mu.Unlock()
	for k, v := range rep.header {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", MediaType)
	w.WriteHeader(rep.status)
	_, _ = io.WriteString(w, rep.body)
}

func (f *fakeAPI) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

// newTestClient builds a client against f with recorded, instant sleeps.
func newTestClient(t *testing.T, f *fakeAPI, tokens *stubTokens, opts ...Option) (*Client, *[]time.Duration) {
	t.Helper()
	c, err := New(tokens, append([]Option{WithBaseURL(f.srv.URL + "/v2"), WithUserAgent("needle/test")}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	var sleeps []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return ctx.Err()
	}
	c.now = func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) }
	return c, &sleeps
}

const userDoc = `{"data":{"id":"208583796","type":"users","attributes":{"country":"US"}},"links":{"self":"/users/me"}}`

func TestGetSendsHeadersAndDecodes(t *testing.T) {
	f := newFakeAPI(t, reply{status: 200, body: userDoc})
	tokens := &stubTokens{token: tokenA}
	c, _ := newTestClient(t, f, tokens)

	doc, err := c.Get(context.Background(), "/users/me", url.Values{"include": {"artist"}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := doc.One()
	if err != nil || r.ID != "208583796" || r.Type != "users" {
		t.Fatalf("One() = %+v, %v", r, err)
	}
	if doc.Links.Self != "/users/me" {
		t.Errorf("Links.Self = %q", doc.Links.Self)
	}

	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	got := reqs[0]
	checks := map[string]string{
		"path":          got.path,
		"include":       got.query.Get("include"),
		"Authorization": got.header.Get("Authorization"),
		"Accept":        got.header.Get("Accept"),
		"User-Agent":    got.header.Get("User-Agent"),
		"Idempotency":   got.header.Get("Idempotency-Key"),
		"Content-Type":  got.header.Get("Content-Type"),
	}
	want := map[string]string{
		"path":          "/v2/users/me",
		"include":       "artist",
		"Authorization": "Bearer " + tokenA,
		"Accept":        MediaType,
		"User-Agent":    "needle/test",
		"Idempotency":   "",
		"Content-Type":  "",
	}
	for k, w := range want {
		if checks[k] != w {
			t.Errorf("%s = %q, want %q", k, checks[k], w)
		}
	}
	if tokens.tokenCalls != 1 || tokens.refreshCalls != 0 {
		t.Errorf("token calls = %d, refresh calls = %d; want 1, 0", tokens.tokenCalls, tokens.refreshCalls)
	}
}

func TestContextIsPropagated(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(block)
	}))
	t.Cleanup(srv.Close)
	c, err := New(&stubTokens{token: tokenA}, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = c.Get(ctx, "/slow", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	select {
	case <-block:
	case <-time.After(2 * time.Second):
		t.Fatal("server never saw the request context end")
	}
}

func TestUnauthorizedHandling(t *testing.T) {
	unauthorized := reply{status: 401, body: `{"errors":[{"code":"UNAUTHORIZED","detail":"token expired"}]}`}
	tests := []struct {
		name         string
		replies      []reply
		tokens       func() *stubTokens
		wantErr      error
		wantRefresh  int
		wantRequests int
		wantTokens   []string // Authorization tokens per request
	}{
		{
			name:         "refresh then succeed",
			replies:      []reply{unauthorized, {status: 200, body: userDoc}},
			tokens:       func() *stubTokens { return &stubTokens{token: tokenA, refreshed: tokenB} },
			wantRefresh:  1,
			wantRequests: 2,
			wantTokens:   []string{tokenA, tokenB},
		},
		{
			name:         "second 401 returns ErrUnauthorized",
			replies:      []reply{unauthorized, unauthorized},
			tokens:       func() *stubTokens { return &stubTokens{token: tokenA, refreshed: tokenB} },
			wantErr:      ErrUnauthorized,
			wantRefresh:  1,
			wantRequests: 2,
			wantTokens:   []string{tokenA, tokenB},
		},
		{
			name:         "refresh failure returns ErrUnauthorized",
			replies:      []reply{unauthorized},
			tokens:       func() *stubTokens { return &stubTokens{token: tokenA, refreshErr: errors.New("refresh token revoked")} },
			wantErr:      ErrUnauthorized,
			wantRefresh:  1,
			wantRequests: 1,
			wantTokens:   []string{tokenA},
		},
		{
			name:         "token failure returns ErrUnauthorized without a request",
			tokens:       func() *stubTokens { return &stubTokens{tokenErr: errors.New("not logged in")} },
			wantErr:      ErrUnauthorized,
			wantRequests: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeAPI(t, tt.replies...)
			tokens := tt.tokens()
			c, _ := newTestClient(t, f, tokens)

			_, err := c.Get(context.Background(), "/users/me", nil)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tokens.refreshCalls != tt.wantRefresh {
				t.Errorf("refresh calls = %d, want %d", tokens.refreshCalls, tt.wantRefresh)
			}
			reqs := f.requests()
			if len(reqs) != tt.wantRequests {
				t.Fatalf("requests = %d, want %d", len(reqs), tt.wantRequests)
			}
			for i, tok := range tt.wantTokens {
				if got := reqs[i].header.Get("Authorization"); got != "Bearer "+tok {
					t.Errorf("request %d Authorization = %q, want Bearer %s", i, got, tok)
				}
			}
		})
	}
}

func TestRetries(t *testing.T) {
	ok := reply{status: 200, body: userDoc}
	limited := reply{status: 429, body: `{"errors":[{"status":"429","code":"RATE_LIMITED","detail":"Rate limit exceeded"}]}`}
	tests := []struct {
		name       string
		replies    []reply
		policy     *RetryPolicy
		wantErr    error
		wantStatus int
		wantSleeps []time.Duration
		wantAfter  time.Duration
	}{
		{
			name:       "429 without Retry-After backs off exponentially",
			replies:    []reply{limited, limited, limited, ok},
			wantSleeps: []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second},
		},
		{
			name:       "429 with Retry-After seconds",
			replies:    []reply{{status: 429, header: map[string]string{"Retry-After": "7"}}, ok},
			wantSleeps: []time.Duration{7 * time.Second},
		},
		{
			name:       "429 with Retry-After HTTP date",
			replies:    []reply{{status: 429, header: map[string]string{"Retry-After": "Sun, 22 Nov 2026 12:00:04 GMT"}}, ok},
			wantSleeps: []time.Duration{4 * time.Second},
		},
		{
			name:       "429 with Retry-After date in the past retries immediately",
			replies:    []reply{{status: 429, header: map[string]string{"Retry-After": "Sun, 22 Nov 2026 11:00:00 GMT"}}, ok},
			wantSleeps: []time.Duration{0},
		},
		{
			name:       "unparseable Retry-After falls back to backoff",
			replies:    []reply{{status: 429, header: map[string]string{"Retry-After": "soon"}}, ok},
			wantSleeps: []time.Duration{500 * time.Millisecond},
		},
		{
			name:       "429 retries exhausted",
			replies:    []reply{limited, limited, limited, limited},
			wantErr:    ErrRateLimited,
			wantStatus: 429,
			wantSleeps: []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second},
		},
		{
			name:       "Retry-After beyond the cap returns at once",
			replies:    []reply{{status: 429, header: map[string]string{"Retry-After": "120"}}},
			wantErr:    ErrRateLimited,
			wantStatus: 429,
			wantAfter:  120 * time.Second,
		},
		{
			name:       "backoff is capped",
			policy:     &RetryPolicy{MaxRetries: 3, BaseDelay: time.Second, MaxDelay: 3 * time.Second},
			replies:    []reply{limited, limited, limited, ok},
			wantSleeps: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second},
		},
		{
			name:       "zero retries",
			policy:     &RetryPolicy{MaxRetries: 0, BaseDelay: time.Second, MaxDelay: time.Second},
			replies:    []reply{limited},
			wantErr:    ErrRateLimited,
			wantStatus: 429,
		},
		{
			name:       "503 is retried",
			replies:    []reply{{status: 503, body: "<html>unavailable</html>"}, ok},
			wantSleeps: []time.Duration{500 * time.Millisecond},
		},
		{
			name:       "502 and 504 are retried",
			replies:    []reply{{status: 502}, {status: 504}, ok},
			wantSleeps: []time.Duration{500 * time.Millisecond, time.Second},
		},
		{
			name:       "500 is not retried",
			replies:    []reply{{status: 500, body: `{"errors":[{"code":"INTERNAL","detail":"boom"}]}`}},
			wantStatus: 500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeAPI(t, tt.replies...)
			var opts []Option
			if tt.policy != nil {
				opts = append(opts, WithRetryPolicy(*tt.policy))
			}
			c, sleeps := newTestClient(t, f, &stubTokens{token: tokenA}, opts...)

			_, err := c.Get(context.Background(), "/users/me", nil)

			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Status != tt.wantStatus {
					t.Fatalf("err = %v, want *APIError with status %d", err, tt.wantStatus)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("errors.Is(err, %v) = false", tt.wantErr)
				}
				if apiErr.RetryAfter != tt.wantAfter {
					t.Errorf("RetryAfter = %v, want %v", apiErr.RetryAfter, tt.wantAfter)
				}
			}
			if !equalDurations(*sleeps, tt.wantSleeps) {
				t.Errorf("sleeps = %v, want %v", *sleeps, tt.wantSleeps)
			}
		})
	}
}

func TestBackoffSleepObservesContext(t *testing.T) {
	f := newFakeAPI(t, reply{status: 429})
	c, err := New(&stubTokens{token: tokenA}, WithBaseURL(f.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.sleep = func(context.Context, time.Duration) error { cancel(); return sleepCtx(ctx, time.Hour) }

	_, err = c.Get(ctx, "/x", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepCtx elapsed = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx canceled = %v, want context.Canceled", err)
	}
}

func TestTypedErrors(t *testing.T) {
	tests := []struct {
		name     string
		reply    reply
		want     []error
		notWant  []error
		wantCode string
	}{
		{
			name:     "concurrent playback",
			reply:    reply{status: 403, body: `{"errors":[{"status":"403","code":"CONCURRENT_PLAYBACK","detail":"Account is playing on another app or device"}]}`},
			want:     []error{ErrConcurrentPlayback},
			notWant:  []error{ErrGeoRestricted, ErrUnauthorized},
			wantCode: "CONCURRENT_PLAYBACK",
		},
		{
			name:     "geo restricted",
			reply:    reply{status: 403, body: `{"errors":[{"code":"GEO_RESTRICTED"}]}`},
			want:     []error{ErrGeoRestricted},
			wantCode: "GEO_RESTRICTED",
		},
		{
			name:  "purchase required",
			reply: reply{status: 403, body: `{"errors":[{"code":"PURCHASE_REQUIRED"}]}`},
			want:  []error{ErrPurchaseRequired},
		},
		{
			name:  "client not entitled (observed live for Dolby Atmos)",
			reply: reply{status: 403, body: `{"errors":[{"status":"403","code":"CLIENT_NOT_ENTITLED","detail":"Client cannot access this content"}]}`},
			want:  []error{ErrClientNotEntitled},
		},
		{
			name:  "prerequisite missing",
			reply: reply{status: 403, body: `{"errors":[{"code":"PREREQUISITE_MISSING"}]}`},
			want:  []error{ErrPrerequisiteMissing},
		},
		{
			name:  "invalid resource ID (observed live as 400)",
			reply: reply{status: 400, body: `{"errors":[{"code":"INVALID_RESOURCE_ID","detail":"Invalid resource ID: Must be a positive integer","source":{"pointer":"/data/id"}}]}`},
			want:  []error{ErrInvalidResourceID},
		},
		{
			name:    "code found in a later error object",
			reply:   reply{status: 403, body: `{"errors":[{"code":"OTHER"},{"code":"GEO_RESTRICTED"}]}`},
			want:    []error{ErrGeoRestricted},
			notWant: []error{ErrNotFound},
		},
		{
			name:    "404 without a body",
			reply:   reply{status: 404},
			want:    []error{ErrNotFound},
			notWant: []error{ErrRateLimited},
		},
		{
			name:    "non-JSON error body",
			reply:   reply{status: 403, body: "<html>Forbidden</html>"},
			notWant: []error{ErrNotFound, ErrClientNotEntitled},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeAPI(t, tt.reply)
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA})

			_, err := c.Get(context.Background(), "/trackManifests/1", nil)

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Status != tt.reply.status {
				t.Errorf("Status = %d, want %d", apiErr.Status, tt.reply.status)
			}
			if tt.wantCode != "" && apiErr.Code() != tt.wantCode {
				t.Errorf("Code() = %q, want %q", apiErr.Code(), tt.wantCode)
			}
			for _, w := range tt.want {
				if !errors.Is(err, w) {
					t.Errorf("errors.Is(err, %v) = false", w)
				}
			}
			for _, nw := range tt.notWant {
				if errors.Is(err, nw) {
					t.Errorf("errors.Is(err, %v) = true", nw)
				}
			}
		})
	}
}

func TestErrorMessagesNeverLeakTokensOrBodies(t *testing.T) {
	secretBody := strings.Repeat("SECRETBODY", 100)
	tests := []struct {
		name    string
		replies []reply
		// detailShown allows a truncated server detail in the message.
		detailShown bool
	}{
		{"JSON:API error with long detail", []reply{{status: 403, body: `{"errors":[{"code":"X","detail":"` + secretBody + `"}]}`}}, true},
		{"non-JSON error body", []reply{{status: 500, body: secretBody}}, false},
		{"malformed success body", []reply{{status: 200, body: `{"data": ` + secretBody}}, false},
		{"second 401", []reply{{status: 401, body: secretBody}, {status: 401, body: secretBody}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeAPI(t, tt.replies...)
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA, refreshed: tokenB})

			_, err := c.Get(context.Background(), "/users/me", url.Values{"countryCode": {"US"}})
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error()
			leaks := []string{tokenA, tokenB, secretBody}
			if !tt.detailShown {
				leaks = append(leaks, "SECRETBODY")
			}
			for _, leak := range leaks {
				if strings.Contains(msg, leak) {
					t.Errorf("error message leaks %q: %s", leak[:10], msg)
				}
			}
			if len(msg) > 400 {
				t.Errorf("error message is %d bytes, want it bounded", len(msg))
			}
		})
	}
}

func TestMalformedJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"truncated object", `{"data": {"id": "1"`},
		{"not JSON", `<html>oops</html>`},
		{"wrong shape for links", `{"data": null, "links": "nope"}`},
		{"wrong shape for a link value", `{"data": null, "links": {"next": 5}}`},
		{"wrong shape for self", `{"data": null, "links": {"self": ["a"]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeAPI(t, reply{status: 200, body: tt.body})
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
			_, err := c.Get(context.Background(), "/users/me", nil)
			if err == nil || !strings.Contains(err.Error(), "decode response") {
				t.Fatalf("err = %v, want a decode error", err)
			}
		})
	}
}

func TestEmptySuccessBody(t *testing.T) {
	f := newFakeAPI(t, reply{status: 204})
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
	doc, err := c.Do(context.Background(), http.MethodDelete, "/userCollectionTracks/me/relationships/items", nil,
		map[string]any{"data": []map[string]string{{"id": "1", "type": "tracks"}}})
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := doc.Many(); err != nil || len(rs) != 0 {
		t.Errorf("Many() = %v, %v; want empty", rs, err)
	}
}

func TestMutatingRequestsUseStableIdempotencyKey(t *testing.T) {
	f := newFakeAPI(t, reply{status: 503}, reply{status: 201, body: `{"data":{"id":"p1","type":"playlists"}}`})
	c, sleeps := newTestClient(t, f, &stubTokens{token: tokenA})
	keys := 0
	c.newKey = func() string { keys++; return "idem-key-0001" }

	body := map[string]any{"data": map[string]any{"type": "playlists", "attributes": map[string]string{"name": "Mix"}}}
	doc, err := c.Do(context.Background(), http.MethodPost, "/playlists", url.Values{"countryCode": {"US"}}, body)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := doc.One(); r.ID != "p1" {
		t.Errorf("created ID = %q, want p1", r.ID)
	}
	reqs := f.requests()
	if len(reqs) != 2 || len(*sleeps) != 1 {
		t.Fatalf("requests = %d, sleeps = %d; want 2, 1", len(reqs), len(*sleeps))
	}
	for i, r := range reqs {
		if got := r.header.Get("Idempotency-Key"); got != "idem-key-0001" {
			t.Errorf("request %d Idempotency-Key = %q", i, got)
		}
		if r.method != http.MethodPost || r.header.Get("Content-Type") != MediaType {
			t.Errorf("request %d method/content-type = %s/%s", i, r.method, r.header.Get("Content-Type"))
		}
		if !strings.Contains(r.body, `"name":"Mix"`) {
			t.Errorf("request %d body = %q, want the payload re-sent", i, r.body)
		}
	}
	if keys != 1 {
		t.Errorf("idempotency keys generated = %d, want 1 per logical request", keys)
	}
}

func TestIdempotencyKeyPerMethod(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			f := newFakeAPI(t, reply{status: 204})
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
			if _, err := c.Do(context.Background(), method, "/x", nil, nil); err != nil {
				t.Fatal(err)
			}
			key := f.requests()[0].header.Get("Idempotency-Key")
			if method == http.MethodGet && key != "" {
				t.Errorf("GET sent Idempotency-Key %q", key)
			}
			if method != http.MethodGet && len(key) != 32 {
				t.Errorf("%s Idempotency-Key = %q, want 32 hex chars", method, key)
			}
		})
	}
}

func TestDoRejectsBadInput(t *testing.T) {
	f := newFakeAPI(t)
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
	tests := []struct {
		name string
		path string
		body any
		want string
	}{
		{"unencodable body", "/x", make(chan int), "encode request body"},
		{"unparseable path", "/%zz", nil, "invalid path"},
		{"foreign absolute URL", "https://evil.example/v2/users/me", nil, "foreign host"},
		{"scheme-relative foreign host", "//evil.example/v2/users/me", nil, "foreign host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Do(context.Background(), http.MethodPost, tt.path, nil, tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("requests sent = %d, want 0", n)
	}
}

func TestResolveSameHostAbsoluteURL(t *testing.T) {
	f := newFakeAPI(t, reply{status: 200, body: userDoc})
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
	if _, err := c.Get(context.Background(), f.srv.URL+"/v2/users/me?locale=en-US", url.Values{"countryCode": {"US"}}); err != nil {
		t.Fatal(err)
	}
	r := f.requests()[0]
	if r.path != "/v2/users/me" || r.query.Get("locale") != "en-US" || r.query.Get("countryCode") != "US" {
		t.Errorf("request = %s ?%s", r.path, r.query.Encode())
	}
}

func TestTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // connection refused from here on
	c, err := New(&stubTokens{token: tokenA}, WithBaseURL(base))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(context.Background(), "/users/me", url.Values{"secretish": {"cursor"}})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "secretish") {
		t.Errorf("transport error includes the query string: %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Error("New(nil) succeeded, want an error")
	}
	tests := []struct {
		name string
		opt  Option
	}{
		{"relative base URL", WithBaseURL("/v2")},
		{"unparseable base URL", WithBaseURL("http://[::1")},
		{"negative retries", WithRetryPolicy(RetryPolicy{MaxRetries: -1})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(&stubTokens{}, tt.opt); err == nil {
				t.Error("New succeeded, want an error")
			}
		})
	}
	c, err := New(&stubTokens{}, WithHTTPClient(&http.Client{}), WithLogger(nil))
	if err != nil || c.base.String() != DefaultBaseURL {
		t.Errorf("defaults: base = %v, err = %v", c.base, err)
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

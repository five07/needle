// Package tidal is a client for the official TIDAL API
// (https://openapi.tidal.com/v2). It handles authentication headers, JSON:API
// documents, cursor pagination, retries, and typed errors. Endpoint-specific
// calls build on Client.Do.
package tidal

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the official TIDAL API base URL.
const DefaultBaseURL = "https://openapi.tidal.com/v2"

// MediaType is the JSON:API media type.
const MediaType = "application/vnd.api+json"

const (
	maxSuccessBody = 16 << 20
	maxErrorBody   = 1 << 20
)

// TokenSource supplies OAuth access tokens.
type TokenSource interface {
	// Token returns the current access token, refreshing it first if it is
	// known to be expired.
	Token(ctx context.Context) (string, error)
	// Refresh obtains a new access token after the API rejected the current
	// one, and returns it.
	Refresh(ctx context.Context) (string, error)
}

// RetryPolicy bounds retries for rate limiting (429) and transient server
// errors (502, 503, 504).
type RetryPolicy struct {
	// MaxRetries is the number of retries after the first attempt.
	MaxRetries int
	// BaseDelay is the first backoff delay; each retry doubles it.
	BaseDelay time.Duration
	// MaxDelay caps any single wait. A Retry-After longer than this ends
	// retrying and returns the error with RetryAfter set.
	MaxDelay time.Duration
}

// DefaultRetryPolicy is used unless WithRetryPolicy overrides it.
var DefaultRetryPolicy = RetryPolicy{MaxRetries: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}

// Client calls the TIDAL API. It is safe for concurrent use.
type Client struct {
	base      *url.URL
	http      *http.Client
	tokens    TokenSource
	userAgent string
	logger    *slog.Logger
	retry     RetryPolicy
	sleep     func(ctx context.Context, d time.Duration) error
	newKey    func() string
	now       func() time.Time
}

// Option configures a Client.
type Option func(*Client) error

// WithBaseURL overrides the API base URL (used by tests).
func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		u, err := url.Parse(strings.TrimRight(raw, "/"))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("tidal: invalid base URL %q", raw)
		}
		c.base = u
		return nil
	}
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) error { c.http = h; return nil }
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) error { c.userAgent = ua; return nil }
}

// WithLogger sets the logger for per-request debug records.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) error { c.logger = l; return nil }
}

// WithRetryPolicy overrides the retry policy.
func WithRetryPolicy(p RetryPolicy) Option {
	return func(c *Client) error {
		if p.MaxRetries < 0 || p.BaseDelay < 0 || p.MaxDelay < 0 {
			return errors.New("tidal: retry policy values must not be negative")
		}
		c.retry = p
		return nil
	}
}

// New returns a Client that authenticates with tokens.
func New(tokens TokenSource, opts ...Option) (*Client, error) {
	if tokens == nil {
		return nil, errors.New("tidal: token source is required")
	}
	base, _ := url.Parse(DefaultBaseURL)
	c := &Client{
		base:      base,
		http:      &http.Client{Timeout: 30 * time.Second},
		tokens:    tokens,
		userAgent: "tidal-tui",
		logger:    slog.New(slog.DiscardHandler),
		retry:     DefaultRetryPolicy,
		sleep:     sleepCtx,
		newKey:    randomKey,
		now:       time.Now,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Get fetches path (relative to the base URL) with query parameters.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (*Document, error) {
	return c.Do(ctx, http.MethodGet, path, query, nil)
}

// Do sends a request and decodes the JSON:API response. body, when non-nil,
// is marshaled to JSON. Mutating methods carry an Idempotency-Key that stays
// the same across retries of this call. A 204 or empty response yields an
// empty Document.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (*Document, error) {
	u, err := c.resolve(path, query)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if body != nil {
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("tidal: encode request body: %w", err)
		}
	}
	return c.send(ctx, method, u, payload)
}

// resolve joins a base-relative path (which may carry its own query, as
// links.next does) with extra query parameters.
func (c *Client) resolve(path string, query url.Values) (*url.URL, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("tidal: invalid path: %w", err)
	}
	if ref.IsAbs() || ref.Host != "" {
		if ref.Scheme != c.base.Scheme || ref.Host != c.base.Host {
			// Never send the access token to another origin.
			return nil, fmt.Errorf("tidal: refusing request to foreign host %q", ref.Host)
		}
		ref.Path = strings.TrimPrefix(ref.Path, c.base.Path)
	}
	u := *c.base
	u.Path = c.base.Path + "/" + strings.TrimLeft(ref.Path, "/")
	q := ref.Query()
	for k, vs := range query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return &u, nil
}

func (c *Client) send(ctx context.Context, method string, u *url.URL, payload []byte) (*Document, error) {
	requestID := randomKey()[:12]
	var idemKey string
	if method != http.MethodGet && method != http.MethodHead {
		idemKey = c.newKey()
	}
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: get token: %w", ErrUnauthorized, err)
	}

	refreshed := false
	retries := 0
	for attempt := 1; ; attempt++ {
		start := c.now()
		status, header, body, err := c.attempt(ctx, method, u, payload, token, idemKey)
		c.logger.Debug("tidal request",
			"request_id", requestID, "method", method, "path", u.Path,
			"status", status, "attempt", attempt, "duration", c.now().Sub(start))
		if err != nil {
			return nil, err
		}

		switch {
		case status >= 200 && status < 300:
			return decodeDocument(method, u.Path, body)

		case status == http.StatusUnauthorized && !refreshed:
			refreshed = true
			if token, err = c.tokens.Refresh(ctx); err != nil {
				return nil, fmt.Errorf("%w: refresh token: %w", ErrUnauthorized, err)
			}
			continue

		case retryable(status):
			wait, hasHeader := retryAfter(header, c.now())
			apiErr := newAPIError(method, u.Path, status, body, wait)
			if retries >= c.retry.MaxRetries || wait > c.retry.MaxDelay {
				return nil, apiErr
			}
			if !hasHeader {
				wait = c.backoff(retries)
			}
			retries++
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue

		default:
			return nil, newAPIError(method, u.Path, status, body, 0)
		}
	}
}

func (c *Client) attempt(ctx context.Context, method string, u *url.URL, payload []byte, token, idemKey string) (int, http.Header, []byte, error) {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("tidal: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", MediaType)
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", MediaType)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, nil, nil, ctxErr
		}
		// url.Error includes the URL (with query) but never headers.
		return 0, nil, nil, fmt.Errorf("tidal: %s %s: %w", method, u.Path, errors.Unwrap(err))
	}
	defer resp.Body.Close()
	limit := int64(maxErrorBody)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		limit = maxSuccessBody
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("tidal: %s %s: read response: %w", method, u.Path, err)
	}
	return resp.StatusCode, resp.Header, body, nil
}

func decodeDocument(method, path string, body []byte) (*Document, error) {
	doc := &Document{}
	if len(bytes.TrimSpace(body)) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(body, doc); err != nil {
		// json errors describe position and type, not the body content.
		return nil, fmt.Errorf("tidal: %s %s: decode response: %w", method, path, err)
	}
	return doc, nil
}

func retryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter parses a Retry-After header in seconds or HTTP-date form.
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

func (c *Client) backoff(retry int) time.Duration {
	d := c.retry.BaseDelay << retry
	if d > c.retry.MaxDelay || d <= 0 {
		return c.retry.MaxDelay
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randomKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return hex.EncodeToString(b)
}

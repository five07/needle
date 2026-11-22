// Package auth handles TIDAL login (OAuth 2 authorization code with PKCE and
// a loopback redirect), token storage, and token refresh.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Official TIDAL OAuth endpoints and defaults.
const (
	DefaultAuthorizeURL = "https://login.tidal.com/authorize"
	DefaultTokenURL     = "https://auth.tidal.com/v1/oauth2/token"
	// DefaultListenAddr must match a redirect URI registered for the app:
	// http://127.0.0.1:8765/callback.
	DefaultListenAddr = "127.0.0.1:8765"
	// CallbackPath is the redirect URI path.
	CallbackPath = "/callback"
)

// DefaultScopes are the THIRD_PARTY scopes tidal-tui requests.
var DefaultScopes = []string{
	"collection.read", "collection.write", "entitlements.read", "playback",
	"playlists.read", "playlists.write", "recommendations.read",
	"search.read", "search.write", "user.read",
}

var (
	// ErrLoginRequired means there are no usable credentials and the user
	// must log in (again).
	ErrLoginRequired = errors.New("auth: login required")
	// ErrNoClientID means no OAuth client ID is configured.
	ErrNoClientID = errors.New("auth: no TIDAL client ID configured")
	// ErrNonLoopback means the listen address is not a loopback address.
	ErrNonLoopback = errors.New("auth: login listener must bind to a loopback address")
)

// OAuthError is an error response from the token endpoint.
type OAuthError struct {
	Status      int
	Code        string
	Description string
}

// Error implements error. The description is server-provided and bounded.
func (e *OAuthError) Error() string {
	msg := fmt.Sprintf("auth: token endpoint returned %d", e.Status)
	if e.Code != "" {
		msg += ": " + bounded(e.Code)
	}
	if e.Description != "" {
		msg += ": " + bounded(e.Description)
	}
	return msg
}

// Tokens are OAuth tokens for one account.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope"`
	UserID       string    `json:"user_id"`
	Expiry       time.Time `json:"expiry"`
}

// Expired reports whether the access token expires within skew of now.
func (t Tokens) Expired(now time.Time, skew time.Duration) bool {
	return t.AccessToken == "" || !now.Add(skew).Before(t.Expiry)
}

// OAuth runs the login and refresh flows against TIDAL's OAuth endpoints.
type OAuth struct {
	ClientID     string
	AuthorizeURL string
	TokenURL     string
	ListenAddr   string
	Scopes       []string
	// Timeout bounds how long Login waits for the browser callback.
	Timeout time.Duration
	HTTP    *http.Client
	// OpenBrowser opens the authorize URL; failures are not fatal because
	// the URL is also printed.
	OpenBrowser func(url string) error
	Now         func() time.Time
}

// NewOAuth returns an OAuth with production defaults for clientID.
func NewOAuth(clientID string) *OAuth {
	return &OAuth{
		ClientID:     clientID,
		AuthorizeURL: DefaultAuthorizeURL,
		TokenURL:     DefaultTokenURL,
		ListenAddr:   DefaultListenAddr,
		Scopes:       DefaultScopes,
		Timeout:      5 * time.Minute,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
		OpenBrowser:  xdgOpen,
		Now:          time.Now,
	}
}

func xdgOpen(u string) error { return exec.Command("xdg-open", u).Start() }

type callbackResult struct {
	code string
	err  error
}

// Login runs the PKCE authorization code flow. It listens on the loopback
// address, prints the authorize URL to out, tries to open the browser, waits
// for one valid callback (or the timeout), and exchanges the code.
func (o *OAuth) Login(ctx context.Context, out io.Writer) (Tokens, error) {
	if o.ClientID == "" {
		return Tokens{}, ErrNoClientID
	}
	host, _, err := net.SplitHostPort(o.ListenAddr)
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: listen address: %w", err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return Tokens{}, ErrNonLoopback
	}
	verifier := randomString(48)
	state := randomString(16)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	ln, err := net.Listen("tcp", o.ListenAddr)
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: start login listener on %s: %w", o.ListenAddr, err)
	}
	redirectURI := "http://" + ln.Addr().String() + CallbackPath

	results := make(chan callbackResult, 1)
	srv := &http.Server{
		Handler:           o.callbackHandler(state, results),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", o.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(o.Scopes, " "))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	authURL := o.AuthorizeURL + "?" + q.Encode()

	fmt.Fprintf(out, "Log in to TIDAL in your browser. If it does not open, visit:\n\n  %s\n\n", authURL)
	if o.OpenBrowser != nil {
		_ = o.OpenBrowser(authURL)
	}

	timer := time.NewTimer(o.Timeout)
	defer timer.Stop()
	var res callbackResult
	select {
	case res = <-results:
	case <-timer.C:
		return Tokens{}, errors.New("auth: timed out waiting for the browser login")
	case <-ctx.Done():
		return Tokens{}, ctx.Err()
	}
	if res.err != nil {
		return Tokens{}, res.err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", o.ClientID)
	form.Set("code", res.code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	return o.tokenRequest(ctx, form)
}

// callbackHandler accepts the first callback carrying the expected state.
// Requests with a wrong or missing state are rejected and ignored, so a stray
// request cannot complete or abort the login.
func (o *OAuth) callbackHandler(state string, results chan<- callbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != CallbackPath || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		p := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(p.Get("state")), []byte(state)) != 1 {
			http.Error(w, "tidal-tui: unexpected login callback", http.StatusBadRequest)
			return
		}
		var res callbackResult
		switch {
		case p.Get("error") != "":
			res.err = &OAuthError{Code: p.Get("error"), Description: p.Get("error_description")}
		case p.Get("code") == "":
			res.err = errors.New("auth: login callback had no authorization code")
		default:
			res.code = p.Get("code")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if res.err != nil {
			fmt.Fprintln(w, "tidal-tui: login failed. Return to the terminal for details.")
		} else {
			fmt.Fprintln(w, "tidal-tui: login complete. You can close this tab.")
		}
		select {
		case results <- res:
		default: // a result was already delivered
		}
	})
}

// Refresh exchanges a refresh token for new tokens. A rejected refresh token
// (invalid_grant) returns an error wrapping ErrLoginRequired.
func (o *OAuth) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if o.ClientID == "" {
		return Tokens{}, ErrNoClientID
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", o.ClientID)
	form.Set("refresh_token", refreshToken)
	return o.tokenRequest(ctx, form)
}

type tokenResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	TokenType    string      `json:"token_type"`
	Scope        string      `json:"scope"`
	ExpiresIn    int64       `json:"expires_in"`
	UserID       json.Number `json:"user_id"`
}

func (o *OAuth) tokenRequest(ctx context.Context, form url.Values) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Tokens{}, ctx.Err()
		}
		// Unwrap the url.Error so the message carries no URL or form data.
		return Tokens{}, fmt.Errorf("auth: token request: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		oe := &OAuthError{Status: resp.StatusCode}
		var eb struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(body, &eb) == nil {
			oe.Code, oe.Description = eb.Error, eb.ErrorDescription
		}
		if oe.Code == "invalid_grant" {
			return Tokens{}, fmt.Errorf("%w: %w", ErrLoginRequired, oe)
		}
		return Tokens{}, oe
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return Tokens{}, fmt.Errorf("auth: decode token response: %w", err)
	}
	if tr.AccessToken == "" || tr.ExpiresIn <= 0 {
		return Tokens{}, errors.New("auth: token response is missing access_token or expires_in")
	}
	return Tokens{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Scope:        tr.Scope,
		UserID:       tr.UserID.String(),
		Expiry:       o.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return base64.RawURLEncoding.EncodeToString(b)
}

func bounded(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "..."
	}
	return s
}

package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// fakeIDP imitates login.tidal.com/authorize and auth.tidal.com token.
type fakeIDP struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	challenge   string
	redirectURI string
	tokenForms  []url.Values

	// authorize behavior: extra query params for the redirect, or a
	// replacement state.
	redirectExtra url.Values
	overrideState *string
	// token behavior
	tokenStatus int
	tokenBody   string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	f := &fakeIDP{t: t, tokenStatus: 200}
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", f.authorize)
	mux.HandleFunc("/token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	want := map[string]string{"response_type": "code", "client_id": "client-123", "code_challenge_method": "S256"}
	for k, v := range want {
		if q.Get(k) != v {
			f.t.Errorf("authorize %s = %q, want %q", k, q.Get(k), v)
		}
	}
	if got := q.Get("scope"); got != strings.Join(DefaultScopes, " ") {
		f.t.Errorf("authorize scope = %q", got)
	}
	f.mu.Lock()
	f.challenge = q.Get("code_challenge")
	f.redirectURI = q.Get("redirect_uri")
	f.mu.Unlock()

	cb := url.Values{"code": {"auth-code-1"}, "state": {q.Get("state")}}
	if f.overrideState != nil {
		cb.Set("state", *f.overrideState)
	}
	maps.Copy(cb, f.redirectExtra)
	http.Redirect(w, r, q.Get("redirect_uri")+"?"+cb.Encode(), http.StatusFound)
}

func (f *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Error(err)
	}
	f.mu.Lock()
	f.tokenForms = append(f.tokenForms, r.PostForm)
	challenge, redirectURI := f.challenge, f.redirectURI
	f.mu.Unlock()
	if r.PostForm.Has("client_secret") {
		f.t.Error("token request sent a client_secret")
	}
	if r.PostForm.Get("grant_type") == "authorization_code" {
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			f.t.Error("code_verifier does not match code_challenge")
		}
		if r.PostForm.Get("redirect_uri") != redirectURI || r.PostForm.Get("code") != "auth-code-1" {
			f.t.Errorf("token exchange form = %v", r.PostForm)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.tokenStatus)
	body := f.tokenBody
	if body == "" {
		body = `{"access_token":"at-1","refresh_token":"rt-1","token_type":"Bearer","expires_in":14400,"scope":"playback user.read","user_id":208583796}`
	}
	_, _ = io.WriteString(w, body)
}

func (f *fakeIDP) oauth() *OAuth {
	o := NewOAuth("client-123")
	o.AuthorizeURL = f.srv.URL + "/authorize"
	o.TokenURL = f.srv.URL + "/token"
	o.ListenAddr = "127.0.0.1:0"
	o.Timeout = 5 * time.Second
	o.Now = func() time.Time { return fixedNow }
	o.OpenBrowser = browse
	return o
}

// browse acts as the user's browser: it follows the authorize redirect to the
// loopback callback.
func browse(u string) error {
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func TestLoginSuccess(t *testing.T) {
	idp := newFakeIDP(t)
	o := idp.oauth()
	var out bytes.Buffer

	tok, err := o.Login(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	want := Tokens{AccessToken: "at-1", RefreshToken: "rt-1", TokenType: "Bearer", Scope: "playback user.read",
		UserID: "208583796", Expiry: fixedNow.Add(4 * time.Hour)}
	if tok != want {
		t.Errorf("tokens = %+v, want %+v", tok, want)
	}
	if !strings.Contains(out.String(), idp.srv.URL+"/authorize?") {
		t.Errorf("output does not show the authorize URL: %q", out.String())
	}
	if !strings.HasPrefix(idp.redirectURI, "http://127.0.0.1:") || !strings.HasSuffix(idp.redirectURI, "/callback") {
		t.Errorf("redirect_uri = %q", idp.redirectURI)
	}
	// The listener must be shut down after login.
	host := strings.TrimSuffix(strings.TrimPrefix(idp.redirectURI, "http://"), "/callback")
	if conn, err := net.DialTimeout("tcp", host, 200*time.Millisecond); err == nil {
		conn.Close()
		t.Error("login listener still accepting connections")
	}
}

func TestLoginStrayCallbackIsIgnored(t *testing.T) {
	idp := newFakeIDP(t)
	o := idp.oauth()
	o.OpenBrowser = func(u string) error {
		au, _ := url.Parse(u)
		redirect := au.Query().Get("redirect_uri")
		// A request with the wrong state, then a non-callback path, then a
		// POST: all must be rejected without ending the login.
		for _, probe := range []struct{ method, target string }{
			{http.MethodGet, redirect + "?code=evil&state=wrong"},
			{http.MethodGet, strings.TrimSuffix(redirect, "/callback") + "/other"},
			{http.MethodPost, redirect + "?code=x&state=" + au.Query().Get("state")},
		} {
			req, _ := http.NewRequest(probe.method, probe.target, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Errorf("%s %s = %d, want rejection", probe.method, probe.target, resp.StatusCode)
			}
		}
		return browse(u)
	}
	tok, err := o.Login(context.Background(), io.Discard)
	if err != nil || tok.AccessToken != "at-1" {
		t.Fatalf("Login = %+v, %v", tok, err)
	}
}

func TestLoginFailures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(idp *fakeIDP, o *OAuth)
		ctx     func() (context.Context, context.CancelFunc)
		wantErr string
		wantIs  error
	}{
		{
			name: "user denies access",
			setup: func(idp *fakeIDP, o *OAuth) {
				idp.redirectExtra = url.Values{"error": {"access_denied"}, "error_description": {"User denied"}}
			},
			wantErr: "access_denied: User denied",
		},
		{
			name: "callback without a code",
			setup: func(idp *fakeIDP, o *OAuth) {
				o.OpenBrowser = func(u string) error {
					au, _ := url.Parse(u)
					return browse(au.Query().Get("redirect_uri") + "?state=" + au.Query().Get("state"))
				}
			},
			wantErr: "no authorization code",
		},
		{
			name: "timeout waiting for the browser",
			setup: func(idp *fakeIDP, o *OAuth) {
				o.Timeout = 50 * time.Millisecond
				o.OpenBrowser = func(string) error { return errors.New("no browser") }
			},
			wantErr: "timed out",
		},
		{
			name:  "context canceled",
			setup: func(idp *fakeIDP, o *OAuth) { o.OpenBrowser = nil },
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			wantIs: context.DeadlineExceeded,
		},
		{
			name:   "no client ID",
			setup:  func(idp *fakeIDP, o *OAuth) { o.ClientID = "" },
			wantIs: ErrNoClientID,
		},
		{
			name:   "non-loopback listen address",
			setup:  func(idp *fakeIDP, o *OAuth) { o.ListenAddr = "0.0.0.0:8765" },
			wantIs: ErrNonLoopback,
		},
		{
			name:   "hostname listen address",
			setup:  func(idp *fakeIDP, o *OAuth) { o.ListenAddr = "localhost:8765" },
			wantIs: ErrNonLoopback,
		},
		{
			name:    "malformed listen address",
			setup:   func(idp *fakeIDP, o *OAuth) { o.ListenAddr = "127.0.0.1" },
			wantErr: "listen address",
		},
		{
			name: "port already in use",
			setup: func(idp *fakeIDP, o *OAuth) {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					idp.t.Fatal(err)
				}
				idp.t.Cleanup(func() { ln.Close() })
				o.ListenAddr = ln.Addr().String()
			},
			wantErr: "start login listener",
		},
		{
			name: "token exchange rejected",
			setup: func(idp *fakeIDP, o *OAuth) {
				idp.tokenStatus = 400
				idp.tokenBody = `{"error":"invalid_request","error_description":"bad redirect_uri"}`
			},
			wantErr: "400: invalid_request: bad redirect_uri",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := newFakeIDP(t)
			o := idp.oauth()
			tt.setup(idp, o)
			ctx, cancel := context.WithCancel(context.Background())
			if tt.ctx != nil {
				ctx, cancel = tt.ctx()
			}
			defer cancel()

			_, err := o.Login(ctx, io.Discard)

			if err == nil {
				t.Fatal("Login succeeded, want an error")
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v, want %v", err, tt.wantIs)
			}
		})
	}
}

func TestRefresh(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    Tokens
		wantIs  error
		wantErr string
	}{
		{
			name: "success without rotation",
			body: `{"access_token":"at-2","token_type":"Bearer","expires_in":14400,"scope":"playback","user_id":208583796}`,
			want: Tokens{AccessToken: "at-2", TokenType: "Bearer", Scope: "playback", UserID: "208583796", Expiry: fixedNow.Add(4 * time.Hour)},
		},
		{
			name:   "revoked refresh token",
			status: 400,
			body:   `{"error":"invalid_grant","error_description":"Refresh token revoked"}`,
			wantIs: ErrLoginRequired,
		},
		{
			name:    "server error with HTML body",
			status:  502,
			body:    "<html>bad gateway</html>",
			wantErr: "token endpoint returned 502",
		},
		{
			name:    "malformed JSON",
			body:    `{"access_token":`,
			wantErr: "decode token response",
		},
		{
			name:    "missing access token",
			body:    `{"expires_in":14400}`,
			wantErr: "missing access_token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := newFakeIDP(t)
			if tt.status != 0 {
				idp.tokenStatus = tt.status
			}
			idp.tokenBody = tt.body
			o := idp.oauth()

			got, err := o.Refresh(context.Background(), "rt-1")

			if tt.wantIs == nil && tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if got != tt.want {
					t.Errorf("Refresh = %+v, want %+v", got, tt.want)
				}
				form := idp.tokenForms[0]
				if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "rt-1" || form.Get("client_id") != "client-123" {
					t.Errorf("refresh form = %v", form)
				}
				return
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v, want %v", err, tt.wantIs)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "rt-1") {
				t.Errorf("error leaks the refresh token: %v", err)
			}
		})
	}
}

func TestRefreshTransportAndInputErrors(t *testing.T) {
	o := NewOAuth("")
	if _, err := o.Refresh(context.Background(), "rt"); !errors.Is(err, ErrNoClientID) {
		t.Errorf("no client ID: err = %v", err)
	}

	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	o = NewOAuth("client-123")
	o.TokenURL = srv.URL + "/token"
	_, err := o.Refresh(context.Background(), "secret-refresh")
	if err == nil || strings.Contains(err.Error(), "secret-refresh") || strings.Contains(err.Error(), srv.URL) {
		t.Errorf("transport error = %v, want an error without the URL or token", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := o.Refresh(ctx, "rt"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: err = %v", err)
	}

	o.TokenURL = "http://[::1"
	if _, err := o.Refresh(context.Background(), "rt"); err == nil || !strings.Contains(err.Error(), "build token request") {
		t.Errorf("bad URL: err = %v", err)
	}
}

func TestOAuthErrorBounded(t *testing.T) {
	e := &OAuthError{Status: 400, Code: "x", Description: strings.Repeat("d", 500) + "\n"}
	if n := len(e.Error()); n > 260 {
		t.Errorf("error length = %d, want bounded", n)
	}
	if got := (&OAuthError{Status: 500}).Error(); got != "auth: token endpoint returned 500" {
		t.Errorf("Error() = %q", got)
	}
}

func TestTokensExpired(t *testing.T) {
	tok := Tokens{AccessToken: "a", Expiry: fixedNow.Add(2 * time.Minute)}
	tests := []struct {
		now  time.Time
		want bool
	}{
		{fixedNow, false},
		{fixedNow.Add(61 * time.Second), true}, // within the 60 s skew
		{fixedNow.Add(3 * time.Minute), true},
	}
	for i, tt := range tests {
		if got := tok.Expired(tt.now, 60*time.Second); got != tt.want {
			t.Errorf("case %d: Expired = %v, want %v", i, got, tt.want)
		}
	}
	if !(Tokens{}).Expired(fixedNow, 0) {
		t.Error("empty tokens not expired")
	}
}

func TestDefaultsAreProduction(t *testing.T) {
	o := NewOAuth("id")
	got := fmt.Sprint(o.AuthorizeURL, o.TokenURL, o.ListenAddr, o.OpenBrowser != nil)
	want := fmt.Sprint(DefaultAuthorizeURL, DefaultTokenURL, "127.0.0.1:8765", true)
	if got != want {
		t.Errorf("defaults = %s, want %s", got, want)
	}
}

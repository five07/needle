package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// tokens is the token endpoint response, plus the time it was obtained.
type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	UserID       any    `json:"user_id,omitempty"`
	ObtainedAt   int64  `json:"obtained_at"`
}

func (t tokens) expired() bool {
	return time.Now().Unix() > t.ObtainedAt+int64(t.ExpiresIn)-30
}

type client struct {
	clientID  string
	tokenPath string
	http      http.Client
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c *client) login(ctx context.Context) error {
	verifier, err := randomURLSafe(48)
	if err != nil {
		return err
	}
	state, err := randomURLSafe(16)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(requestedScopes, " "))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	authURL := authorizeURL + "?" + q.Encode()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenAddr, err)
	}

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		p := r.URL.Query()
		var res result
		switch {
		case p.Get("error") != "":
			res.err = fmt.Errorf("authorization error: %s: %s", p.Get("error"), p.Get("error_description"))
		case p.Get("state") != state:
			res.err = errors.New("state mismatch in callback")
		case p.Get("code") == "":
			res.err = errors.New("callback had no code")
		default:
			res.code = p.Get("code")
		}
		if res.err != nil {
			fmt.Fprintln(w, "tidal-spike: login failed, see terminal.")
		} else {
			fmt.Fprintln(w, "tidal-spike: login complete, you can close this tab.")
		}
		select {
		case done <- res:
		default:
		}
	})
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	fmt.Println("Open this URL to log in (trying your browser too):")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	_ = exec.Command("xdg-open", authURL).Start()

	var res result
	select {
	case res = <-done:
	case <-time.After(5 * time.Minute):
		return errors.New("timed out waiting for the callback")
	case <-ctx.Done():
		return ctx.Err()
	}
	if res.err != nil {
		return res.err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.clientID)
	form.Set("code", res.code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	t, err := c.tokenRequest(ctx, form)
	if err != nil {
		return err
	}
	if err := c.saveTokens(t); err != nil {
		return err
	}
	fmt.Println("Login succeeded. Tokens saved to", c.tokenPath)
	describeTokens(t)
	return nil
}

func (c *client) refreshCmd(ctx context.Context) error {
	t, err := c.loadTokens()
	if err != nil {
		return err
	}
	nt, err := c.refresh(ctx, t)
	if err != nil {
		return err
	}
	fmt.Println("Refresh succeeded.")
	describeTokens(nt)
	fmt.Println("refresh token rotated:", nt.RefreshToken != "" && nt.RefreshToken != t.RefreshToken)
	return nil
}

func (c *client) refresh(ctx context.Context, t tokens) (tokens, error) {
	if t.RefreshToken == "" {
		return tokens{}, errors.New("no refresh token stored")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", c.clientID)
	form.Set("refresh_token", t.RefreshToken)
	nt, err := c.tokenRequest(ctx, form)
	if err != nil {
		return tokens{}, err
	}
	if nt.RefreshToken == "" {
		nt.RefreshToken = t.RefreshToken
	}
	return nt, c.saveTokens(nt)
}

func (c *client) tokenRequest(ctx context.Context, form url.Values) (tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return tokens{}, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokens{}, fmt.Errorf("token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// OAuth error bodies carry error/error_description only, no secrets.
		return tokens{}, fmt.Errorf("token endpoint returned %s: %s", resp.Status, truncate(string(body), 500))
	}
	var t tokens
	if err := json.Unmarshal(body, &t); err != nil {
		return tokens{}, fmt.Errorf("decode token response: %w", err)
	}
	t.ObtainedAt = time.Now().Unix()
	return t, nil
}

// describeTokens prints token metadata, never the token values.
func describeTokens(t tokens) {
	fmt.Println("  token_type:   ", t.TokenType)
	fmt.Println("  expires_in:   ", t.ExpiresIn, "seconds")
	fmt.Println("  refresh token:", t.RefreshToken != "")
	fmt.Println("  user_id:      ", t.UserID)
	fmt.Println("  granted scope:", t.Scope)
}

func (c *client) saveTokens(t tokens) error {
	if err := os.MkdirAll(filepath.Dir(c.tokenPath), 0o700); err != nil {
		return fmt.Errorf("token dir: %w", err)
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.tokenPath, b, 0o600); err != nil {
		return fmt.Errorf("write tokens: %w", err)
	}
	return os.Chmod(c.tokenPath, 0o600)
}

func (c *client) loadTokens() (tokens, error) {
	b, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return tokens{}, fmt.Errorf("read tokens (run login first): %w", err)
	}
	var t tokens
	if err := json.Unmarshal(b, &t); err != nil {
		return tokens{}, fmt.Errorf("decode tokens: %w", err)
	}
	return t, nil
}

// accessToken returns a valid access token, refreshing it when expired.
func (c *client) accessToken(ctx context.Context) (string, error) {
	t, err := c.loadTokens()
	if err != nil {
		return "", err
	}
	if t.expired() {
		if t, err = c.refresh(ctx, t); err != nil {
			return "", fmt.Errorf("refresh: %w", err)
		}
	}
	return t.AccessToken, nil
}

// loadDotEnv reads KEY=value lines, tolerating spaces around "=" and quotes.
func loadDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return env, sc.Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

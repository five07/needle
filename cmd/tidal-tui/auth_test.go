package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"

	"github.com/five07/tidal-tui/internal/auth"
	"github.com/five07/tidal-tui/internal/tidal"
)

func TestMain(m *testing.M) {
	keyring.MockInit() // never touch the developer's real keyring
	os.Exit(m.Run())
}

// fakeTIDAL serves /v2/users/me and the OAuth token endpoint, and stands in
// for the browser login.
type fakeTIDAL struct {
	t   *testing.T
	srv *httptest.Server
	mux *http.ServeMux // more handlers can be added before requests

	logins     atomic.Int32
	loginErr   error
	meStatus   int
	meCountry  string
	tokenReply string // token endpoint body; status 400 when it has "error"
	lastBearer atomic.Value
	uiRuns     atomic.Int32
	uiErr      error
}

func newFakeTIDAL(t *testing.T) *fakeTIDAL {
	mux := http.NewServeMux()
	f := &fakeTIDAL{t: t, meStatus: 200, meCountry: "US", mux: mux}
	mux.HandleFunc("/v2/users/me", func(w http.ResponseWriter, r *http.Request) {
		f.lastBearer.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(f.meStatus)
		if f.meStatus == 200 {
			io.WriteString(w, `{"data":{"id":"42","type":"users","attributes":{"username":"listener","country":"`+f.meCountry+`","email":"l@example.com"}}}`)
		}
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(f.tokenReply, `"error"`) {
			w.WriteHeader(400)
		}
		io.WriteString(w, f.tokenReply)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTIDAL) deps() deps {
	return deps{
		runUI: func(context.Context, tea.Model) error {
			f.uiRuns.Add(1)
			return f.uiErr
		},
		mpvDeviceHelp: func(context.Context) ([]byte, error) {
			return []byte("  'auto' (Autoselect device)\n  'pipewire/alsa_output.usb-x' (USB DAC)\n"), nil
		},
		newOAuth: func(id string) *auth.OAuth {
			o := auth.NewOAuth(id)
			o.TokenURL = f.srv.URL + "/token"
			o.OpenBrowser = nil
			return o
		},
		login: func(_ context.Context, o *auth.OAuth, out io.Writer) (auth.Tokens, error) {
			f.logins.Add(1)
			io.WriteString(out, "Log in to TIDAL in your browser.\n")
			if f.loginErr != nil {
				return auth.Tokens{}, f.loginErr
			}
			return auth.Tokens{AccessToken: "at-login", RefreshToken: "rt-login", UserID: "42", Expiry: time.Now().Add(4 * time.Hour)}, nil
		},
		apiOptions: []tidal.Option{tidal.WithBaseURL(f.srv.URL + "/v2")},
	}
}

func storeKeyring(t *testing.T, tok auth.Tokens) {
	t.Helper()
	b, _ := json.Marshal(tok)
	if err := keyring.Set("tidal-tui", "tidal-account", string(b)); err != nil {
		t.Fatal(err)
	}
}

func TestRunAuth(t *testing.T) {
	stored := auth.Tokens{AccessToken: "at-stored", RefreshToken: "rt-stored", UserID: "42", Expiry: time.Now().Add(time.Hour)}
	expiredTok := stored
	expiredTok.Expiry = time.Now().Add(-time.Hour)

	tests := []struct {
		name          string
		args          []string
		noClientID    bool
		keyringErr    bool
		setup         func(t *testing.T, f *fakeTIDAL, configDir string)
		wantCode      int
		wantStdout    string
		wantStderr    string
		wantLogins    int32
		wantBearer    string
		wantLog       []string
		wantKeyringRT string // refresh token in the keyring afterwards; "-" = empty
		wantFile      bool   // credentials file exists afterwards
	}{
		{
			name:          "first launch logs in and stores tokens in the keyring",
			wantStdout:    "Logged in as listener (US).\n",
			wantStderr:    "Log in to TIDAL in your browser",
			wantLogins:    1,
			wantBearer:    "Bearer at-login",
			wantLog:       []string{"msg=\"logged in\"", "store=\"system keyring\"", "user_id=42"},
			wantKeyringRT: "rt-login",
		},
		{
			name:          "stored credentials skip login",
			setup:         func(t *testing.T, f *fakeTIDAL, _ string) { storeKeyring(t, stored) },
			wantStdout:    "Logged in as listener (US).\n",
			wantBearer:    "Bearer at-stored",
			wantKeyringRT: "rt-stored",
		},
		{
			name: "expired access token refreshes silently",
			setup: func(t *testing.T, f *fakeTIDAL, _ string) {
				storeKeyring(t, expiredTok)
				f.tokenReply = `{"access_token":"at-refreshed","expires_in":14400,"token_type":"Bearer"}`
			},
			wantStdout:    "Logged in as listener (US).\n",
			wantBearer:    "Bearer at-refreshed",
			wantKeyringRT: "rt-stored",
		},
		{
			name: "revoked refresh token returns to login",
			setup: func(t *testing.T, f *fakeTIDAL, _ string) {
				storeKeyring(t, expiredTok)
				f.tokenReply = `{"error":"invalid_grant","error_description":"revoked"}`
			},
			wantStdout:    "Logged in as listener (US).\n",
			wantStderr:    "session expired",
			wantLogins:    1,
			wantBearer:    "Bearer at-login",
			wantLog:       []string{"no longer valid"},
			wantKeyringRT: "rt-login",
		},
		{
			name:       "keyring unavailable falls back to a 0600 file",
			keyringErr: true,
			wantStdout: "Logged in as listener (US).\n",
			wantLogins: 1,
			wantLog:    []string{"level=WARN", "keyring unavailable"},
			wantFile:   true,
		},
		{
			name: "login failure exits one",
			setup: func(t *testing.T, f *fakeTIDAL, _ string) {
				f.loginErr = errors.New("auth: timed out waiting for the browser login")
			},
			wantCode:      1,
			wantStderr:    "timed out waiting",
			wantLogins:    1,
			wantLog:       []string{"msg=\"login failed\""},
			wantKeyringRT: "-",
		},
		{
			name: "login failure after revocation exits one",
			setup: func(t *testing.T, f *fakeTIDAL, _ string) {
				storeKeyring(t, expiredTok)
				f.tokenReply = `{"error":"invalid_grant"}`
				f.loginErr = errors.New("auth: user canceled")
			},
			wantCode:      1,
			wantStderr:    "user canceled",
			wantLogins:    1,
			wantKeyringRT: "-",
		},
		{
			name:          "account request failure exits one",
			setup:         func(t *testing.T, f *fakeTIDAL, _ string) { storeKeyring(t, stored); f.meStatus = 500 },
			wantCode:      1,
			wantStderr:    "500",
			wantKeyringRT: "rt-stored",
		},
		{
			name:       "unreadable credentials file is an error, not a login",
			keyringErr: true,
			setup: func(t *testing.T, f *fakeTIDAL, configDir string) {
				os.MkdirAll(configDir, 0o700)
				os.WriteFile(filepath.Join(configDir, auth.CredentialsFile), []byte(`{}`), 0o644)
			},
			wantCode:   1,
			wantStderr: "accessible by other users",
			wantFile:   true,
		},
		{
			name:       "saving tokens after login fails",
			keyringErr: true,
			setup: func(t *testing.T, f *fakeTIDAL, configDir string) {
				if os.Geteuid() == 0 {
					t.Skip("permission checks do not apply to root")
				}
				if err := os.MkdirAll(configDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(configDir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(configDir, 0o700) })
			},
			wantCode:   1,
			wantStderr: "create temp credentials file",
			wantLogins: 1,
		},
		{
			name: "invalid API base URL exits one",
			setup: func(t *testing.T, f *fakeTIDAL, _ string) {
				f.srv.URL = "no-scheme"
			},
			wantCode:      1,
			wantStderr:    "tidal",
			wantKeyringRT: "-",
		},
		{
			name:       "no client ID",
			noClientID: true,
			wantCode:   1,
			wantStderr: "TIDAL_CLIENT_ID",
		},
		{
			name:       "logout removes keyring and file credentials without a client ID",
			args:       []string{"-logout"},
			noClientID: true,
			setup: func(t *testing.T, f *fakeTIDAL, configDir string) {
				storeKeyring(t, stored)
				if err := (auth.FileStore{Path: filepath.Join(configDir, auth.CredentialsFile)}).Save(stored); err != nil {
					t.Fatal(err)
				}
			},
			wantStdout:    "Logged out.\n",
			wantLog:       []string{"msg=\"logged out\""},
			wantKeyringRT: "-",
		},
		{
			name: "logout failure exits one",
			args: []string{"-logout"},
			setup: func(t *testing.T, f *fakeTIDAL, configDir string) {
				os.MkdirAll(filepath.Join(configDir, auth.CredentialsFile, "child"), 0o700)
			},
			wantCode:   1,
			wantStderr: "delete credentials file",
			wantLog:    []string{"msg=\"logout failed\""},
			wantFile:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyring.MockInit()
			if tt.keyringErr {
				keyring.MockInitWithError(errors.New("no secret service"))
				t.Cleanup(keyring.MockInit)
			}
			env := map[string]string{"TIDAL_CLIENT_ID": "client-123"}
			if tt.noClientID {
				env = nil
			}
			getenv, configDir, stateDir := testEnv(t, env)
			f := newFakeTIDAL(t)
			if tt.setup != nil {
				tt.setup(t, f, configDir)
			}
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), f.deps(), tt.args, getenv, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if n := f.logins.Load(); n != tt.wantLogins {
				t.Errorf("logins = %d, want %d", n, tt.wantLogins)
			}
			if tt.wantBearer != "" && f.lastBearer.Load() != tt.wantBearer {
				t.Errorf("API saw %v, want %q", f.lastBearer.Load(), tt.wantBearer)
			}

			logBytes, _ := os.ReadFile(filepath.Join(stateDir, "tidal-tui.log"))
			for _, want := range tt.wantLog {
				if !strings.Contains(string(logBytes), want) {
					t.Errorf("log missing %q:\n%s", want, logBytes)
				}
			}
			for _, secret := range []string{"at-login", "rt-login", "at-stored", "rt-stored", "at-refreshed"} {
				if strings.Contains(string(logBytes), secret) || strings.Contains(stdout.String()+stderr.String(), secret) {
					t.Errorf("token %q leaked to the log or terminal", secret)
				}
			}

			if tt.wantKeyringRT != "" && !tt.keyringErr {
				raw, err := keyring.Get("tidal-tui", "tidal-account")
				var got auth.Tokens
				_ = json.Unmarshal([]byte(raw), &got)
				switch {
				case tt.wantKeyringRT == "-" && !errors.Is(err, keyring.ErrNotFound):
					t.Errorf("keyring still holds credentials: %v", err)
				case tt.wantKeyringRT != "-" && got.RefreshToken != tt.wantKeyringRT:
					t.Errorf("keyring refresh token = %q, want %q", got.RefreshToken, tt.wantKeyringRT)
				}
			}
			credPath := filepath.Join(configDir, auth.CredentialsFile)
			info, err := os.Stat(credPath)
			if tt.wantFile != (err == nil) {
				t.Errorf("credentials file exists = %v, want %v", err == nil, tt.wantFile)
			}
			if tt.wantFile && tt.keyringErr && tt.wantCode == 0 && info.Mode().Perm() != 0o600 {
				t.Errorf("credentials file mode = %o, want 600", info.Mode().Perm())
			}
		})
	}
}

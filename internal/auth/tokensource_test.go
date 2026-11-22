package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memStore is an in-memory Store that counts calls and can fail.
type memStore struct {
	mu                 sync.Mutex
	tokens             *Tokens
	loads, saves, dels int
	loadErr, saveErr   error
	deleteErr          error
}

func (m *memStore) Name() string { return "memory" }

func (m *memStore) Load() (Tokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loads++
	if m.loadErr != nil {
		return Tokens{}, m.loadErr
	}
	if m.tokens == nil {
		return Tokens{}, ErrNoCredentials
	}
	return *m.tokens, nil
}

func (m *memStore) Save(t Tokens) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.tokens = &t
	return nil
}

func (m *memStore) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dels++
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.tokens = nil
	return nil
}

// stubRefresher returns a fixed result and counts calls.
type stubRefresher struct {
	calls atomic.Int32
	got   atomic.Value // last refresh token
	delay time.Duration
	out   Tokens
	err   error
}

func (r *stubRefresher) Refresh(_ context.Context, rt string) (Tokens, error) {
	r.calls.Add(1)
	r.got.Store(rt)
	time.Sleep(r.delay)
	return r.out, r.err
}

func valid() *Tokens {
	return &Tokens{AccessToken: "at-old", RefreshToken: "rt-1", UserID: "42", Expiry: fixedNow.Add(time.Hour)}
}

func expired() *Tokens {
	t := valid()
	t.Expiry = fixedNow.Add(30 * time.Second) // inside the 60 s skew
	return t
}

func newSource(store *memStore, ref *stubRefresher) *TokenSource {
	s := NewTokenSource(store, ref)
	s.now = func() time.Time { return fixedNow }
	return s
}

func TestTokenSourceToken(t *testing.T) {
	fresh := Tokens{AccessToken: "at-new", Expiry: fixedNow.Add(4 * time.Hour)}
	tests := []struct {
		name                        string
		stored                      *Tokens
		loadErr, saveErr, deleteErr error
		refOut                      Tokens
		refErr                      error
		want                        string
		wantIs                      error
		wantErr                     string
		wantRefresh                 int32
		wantSaves                   int
		wantDels                    int
		wantStoredRT                string // refresh token persisted after the call; "" = check absent
	}{
		{name: "valid token is cached", stored: valid(), want: "at-old"},
		{name: "expired token refreshes and keeps the refresh token", stored: expired(), refOut: fresh,
			want: "at-new", wantRefresh: 1, wantSaves: 1, wantStoredRT: "rt-1"},
		{name: "rotated refresh token is persisted", stored: expired(),
			refOut: Tokens{AccessToken: "at-new", RefreshToken: "rt-2", UserID: "42", Expiry: fresh.Expiry},
			want:   "at-new", wantRefresh: 1, wantSaves: 1, wantStoredRT: "rt-2"},
		{name: "no stored credentials", wantIs: ErrLoginRequired},
		{name: "store read failure is not a login prompt", loadErr: errors.New("dbus down"), wantErr: "dbus down"},
		{name: "revoked refresh token deletes credentials", stored: expired(),
			refErr: errors.Join(ErrLoginRequired, errors.New("invalid_grant")), wantIs: ErrLoginRequired, wantRefresh: 1, wantDels: 1},
		{name: "revoked and delete fails", stored: expired(), deleteErr: errors.New("locked"),
			refErr: ErrLoginRequired, wantIs: ErrLoginRequired, wantErr: "locked", wantRefresh: 1, wantDels: 1},
		{name: "no refresh token", stored: &Tokens{AccessToken: "x", Expiry: fixedNow}, wantIs: ErrLoginRequired, wantDels: 1},
		{name: "transient refresh failure keeps credentials", stored: expired(), refErr: errors.New("network down"),
			wantErr: "network down", wantRefresh: 1, wantStoredRT: "rt-1"},
		{name: "save failure after refresh", stored: expired(), saveErr: errors.New("disk full"), refOut: fresh,
			wantErr: "disk full", wantRefresh: 1, wantSaves: 1, wantStoredRT: "rt-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &memStore{loadErr: tt.loadErr, saveErr: tt.saveErr, deleteErr: tt.deleteErr, tokens: tt.stored}
			ref := &stubRefresher{out: tt.refOut, err: tt.refErr}
			src := newSource(store, ref)

			got, err := src.Token(context.Background())

			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v, want %v", err, tt.wantIs)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if tt.wantIs == nil && tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("token = %q, want %q", got, tt.want)
			}
			if n := ref.calls.Load(); n != tt.wantRefresh {
				t.Errorf("refresh calls = %d, want %d", n, tt.wantRefresh)
			}
			if store.saves != tt.wantSaves || store.dels != tt.wantDels {
				t.Errorf("saves=%d dels=%d, want %d/%d", store.saves, store.dels, tt.wantSaves, tt.wantDels)
			}
			if tt.wantRefresh > 0 && ref.got.Load() != "rt-1" {
				t.Errorf("refreshed with %v, want rt-1", ref.got.Load())
			}
			if tt.wantStoredRT != "" && (store.tokens == nil || store.tokens.RefreshToken != tt.wantStoredRT) {
				t.Errorf("stored tokens = %+v, want refresh token %q", store.tokens, tt.wantStoredRT)
			}
			if tt.wantStoredRT == "rt-1" && tt.wantSaves == 1 && store.saveErr == nil && store.tokens.UserID != "42" {
				t.Errorf("user ID lost on refresh: %+v", store.tokens)
			}
		})
	}
}

func TestTokenSourceCachesAfterFirstLoad(t *testing.T) {
	store := &memStore{tokens: valid()}
	src := newSource(store, &stubRefresher{})
	for range 3 {
		if _, err := src.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if store.loads != 1 {
		t.Errorf("store loads = %d, want 1", store.loads)
	}
}

func TestTokenSourceForcedRefresh(t *testing.T) {
	store := &memStore{tokens: valid()}
	ref := &stubRefresher{out: Tokens{AccessToken: "at-new", Expiry: fixedNow.Add(4 * time.Hour)}}
	src := newSource(store, ref)

	got, err := src.Refresh(context.Background())
	if err != nil || got != "at-new" || ref.calls.Load() != 1 {
		t.Errorf("Refresh = %q, %v, calls %d", got, err, ref.calls.Load())
	}

	empty := newSource(&memStore{}, ref)
	if _, err := empty.Refresh(context.Background()); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("Refresh without credentials err = %v", err)
	}
	revoked := newSource(&memStore{tokens: valid()}, &stubRefresher{err: ErrLoginRequired})
	if _, err := revoked.Refresh(context.Background()); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("revoked Refresh err = %v", err)
	}
	// After revocation the source asks for login rather than reusing the
	// stale cached token.
	if _, err := revoked.Token(context.Background()); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("Token after revocation err = %v", err)
	}
}

func TestTokenSourceSetAndHasCredentials(t *testing.T) {
	store := &memStore{}
	src := newSource(store, &stubRefresher{})

	if ok, err := src.HasCredentials(); ok || err != nil {
		t.Errorf("HasCredentials empty = %v, %v", ok, err)
	}
	if err := src.Set(*valid()); err != nil {
		t.Fatal(err)
	}
	if ok, err := src.HasCredentials(); !ok || err != nil {
		t.Errorf("HasCredentials after Set = %v, %v", ok, err)
	}
	if tok, _ := src.Token(context.Background()); tok != "at-old" {
		t.Errorf("Token after Set = %q", tok)
	}
	if store.tokens == nil {
		t.Error("Set did not persist")
	}

	failing := newSource(&memStore{saveErr: errors.New("disk full")}, &stubRefresher{})
	if err := failing.Set(*valid()); err == nil {
		t.Error("Set with failing store succeeded")
	}
	if ok, _ := failing.HasCredentials(); ok {
		t.Error("failed Set left credentials cached")
	}

	broken := newSource(&memStore{loadErr: errors.New("dbus down")}, &stubRefresher{})
	if ok, err := broken.HasCredentials(); ok || err == nil {
		t.Errorf("HasCredentials with broken store = %v, %v", ok, err)
	}
}

func TestTokenSourceConcurrentRefreshIsSingle(t *testing.T) {
	store := &memStore{tokens: expired()}
	ref := &stubRefresher{delay: 20 * time.Millisecond, out: Tokens{AccessToken: "at-new", Expiry: fixedNow.Add(4 * time.Hour)}}
	src := newSource(store, ref)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if tok, err := src.Token(context.Background()); err != nil || tok != "at-new" {
				t.Errorf("Token = %q, %v", tok, err)
			}
		})
	}
	wg.Wait()
	if n := ref.calls.Load(); n != 1 {
		t.Errorf("refresh calls = %d, want 1", n)
	}
}

func TestOAuthIsARefresher(t *testing.T) {
	var _ Refresher = (*OAuth)(nil)
}

package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// expirySkew refreshes tokens slightly before they expire.
const expirySkew = 60 * time.Second

// Refresher exchanges a refresh token for new tokens. *OAuth implements it.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (Tokens, error)
}

// TokenSource supplies access tokens from a Store, refreshing and persisting
// them as needed. It implements tidal.TokenSource and is safe for concurrent
// use; refreshes are serialized.
type TokenSource struct {
	mu        sync.Mutex
	store     Store
	refresher Refresher
	now       func() time.Time
	tokens    Tokens
	loaded    bool
}

// NewTokenSource returns a TokenSource over store.
func NewTokenSource(store Store, refresher Refresher) *TokenSource {
	return &TokenSource{store: store, refresher: refresher, now: time.Now}
}

// Token returns a valid access token, refreshing it when it is about to
// expire. It returns an error wrapping ErrLoginRequired when there are no
// usable credentials.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	if s.tokens.Expired(s.now(), expirySkew) {
		if err := s.refreshLocked(ctx); err != nil {
			return "", err
		}
	}
	return s.tokens.AccessToken, nil
}

// Refresh forces a refresh after the API rejected the current token.
func (s *TokenSource) Refresh(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	if err := s.refreshLocked(ctx); err != nil {
		return "", err
	}
	return s.tokens.AccessToken, nil
}

// Set stores freshly obtained tokens (after Login) and caches them.
func (s *TokenSource) Set(t Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.Save(t); err != nil {
		return err
	}
	s.tokens, s.loaded = t, true
	return nil
}

// HasCredentials reports whether stored credentials exist.
func (s *TokenSource) HasCredentials() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.loadLocked()
	if errors.Is(err, ErrLoginRequired) {
		return false, nil
	}
	return err == nil, err
}

func (s *TokenSource) loadLocked() error {
	if s.loaded {
		return nil
	}
	t, err := s.store.Load()
	if errors.Is(err, ErrNoCredentials) {
		return fmt.Errorf("%w: %w", ErrLoginRequired, err)
	}
	if err != nil {
		return err
	}
	s.tokens, s.loaded = t, true
	return nil
}

func (s *TokenSource) refreshLocked(ctx context.Context) error {
	if s.tokens.RefreshToken == "" {
		return s.forgetLocked()
	}
	t, err := s.refresher.Refresh(ctx, s.tokens.RefreshToken)
	if errors.Is(err, ErrLoginRequired) {
		return errors.Join(s.forgetLocked(), err)
	}
	if err != nil {
		return err
	}
	if t.RefreshToken == "" {
		t.RefreshToken = s.tokens.RefreshToken // TIDAL does not rotate it
	}
	if t.UserID == "" {
		t.UserID = s.tokens.UserID
	}
	if err := s.store.Save(t); err != nil {
		return err
	}
	s.tokens = t
	return nil
}

// forgetLocked drops credentials that can no longer be refreshed.
func (s *TokenSource) forgetLocked() error {
	s.tokens, s.loaded = Tokens{}, false
	if err := s.store.Delete(); err != nil {
		return fmt.Errorf("%w (and removing stale credentials failed: %w)", ErrLoginRequired, err)
	}
	return ErrLoginRequired
}

package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// ErrNoCredentials means no credentials are stored.
var ErrNoCredentials = errors.New("auth: no stored credentials")

// Keyring entry names.
const (
	keyringService = "tidal-tui"
	keyringUser    = "tidal-account"
)

// CredentialsFile is the fallback file name inside the config directory.
const CredentialsFile = "credentials.json"

// Store persists one account's tokens.
type Store interface {
	// Load returns the stored tokens or ErrNoCredentials.
	Load() (Tokens, error)
	// Save replaces the stored tokens.
	Save(Tokens) error
	// Delete removes the stored tokens; deleting nothing is not an error.
	Delete() error
	// Name describes the backend for messages and logs.
	Name() string
}

// KeyringStore keeps tokens in the OS keyring (Secret Service on Linux).
type KeyringStore struct{}

// Name implements Store.
func (KeyringStore) Name() string { return "system keyring" }

// Load implements Store.
func (KeyringStore) Load() (Tokens, error) {
	s, err := keyring.Get(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return Tokens{}, ErrNoCredentials
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: read keyring: %w", err)
	}
	var t Tokens
	if err := json.Unmarshal([]byte(s), &t); err != nil {
		return Tokens{}, fmt.Errorf("auth: decode keyring credentials: %w", err)
	}
	return t, nil
}

// Save implements Store.
func (KeyringStore) Save(t Tokens) error {
	b, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("auth: encode credentials: %w", err)
	}
	if err := keyring.Set(keyringService, keyringUser, string(b)); err != nil {
		return fmt.Errorf("auth: write keyring: %w", err)
	}
	return nil
}

// Delete implements Store.
func (KeyringStore) Delete() error {
	err := keyring.Delete(keyringService, keyringUser)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("auth: delete keyring entry: %w", err)
	}
	return nil
}

// FileStore keeps tokens in a file readable only by the user.
type FileStore struct {
	Path string
}

// Name implements Store.
func (s FileStore) Name() string { return "file " + s.Path }

// Load implements Store. It refuses a file that other users can read.
func (s FileStore) Load() (Tokens, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Tokens{}, ErrNoCredentials
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: open credentials file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: stat credentials file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Tokens{}, fmt.Errorf("auth: credentials file %s is accessible by other users (mode %o); run chmod 600 on it", s.Path, info.Mode().Perm())
	}
	var t Tokens
	if err := json.NewDecoder(f).Decode(&t); err != nil {
		return Tokens{}, fmt.Errorf("auth: decode credentials file: %w", err)
	}
	return t, nil
}

// Save implements Store. It writes a temporary file with mode 0600 and
// renames it into place so a crash never leaves a partial file.
func (s FileStore) Save(t Tokens) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("auth: create credentials dir: %w", err)
	}
	b, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("auth: encode credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return fmt.Errorf("auth: create temp credentials file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("auth: write credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("auth: write credentials: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return fmt.Errorf("auth: save credentials file: %w", err)
	}
	return nil
}

// Delete implements Store.
func (s FileStore) Delete() error {
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("auth: delete credentials file: %w", err)
	}
	return nil
}

// keyringAvailable probes the keyring with a read.
func keyringAvailable() bool {
	_, err := keyring.Get(keyringService, keyringUser)
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

// NewStore returns the keyring store when a keyring is reachable, and
// otherwise the 0600 file store under configDir, logging a warning.
func NewStore(configDir string, logger *slog.Logger) Store {
	if keyringAvailable() {
		return KeyringStore{}
	}
	fsStore := FileStore{Path: filepath.Join(configDir, CredentialsFile)}
	logger.Warn("system keyring unavailable, storing credentials in a file", "path", fsStore.Path)
	return fsStore
}

// DeleteAll removes credentials from both the keyring (when reachable) and
// the fallback file, so logout works whichever store holds them.
func DeleteAll(configDir string) error {
	var errs []error
	if keyringAvailable() {
		errs = append(errs, KeyringStore{}.Delete())
	}
	errs = append(errs, FileStore{Path: filepath.Join(configDir, CredentialsFile)}.Delete())
	return errors.Join(errs...)
}

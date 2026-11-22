package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

var sampleTokens = Tokens{
	AccessToken: "at-1", RefreshToken: "rt-1", TokenType: "Bearer",
	Scope: "playback", UserID: "42", Expiry: time.Date(2026, 11, 22, 16, 0, 0, 0, time.UTC),
}

func TestKeyringStore(t *testing.T) {
	keyring.MockInit()
	s := KeyringStore{}

	if _, err := s.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("empty Load err = %v, want ErrNoCredentials", err)
	}
	if err := s.Delete(); err != nil {
		t.Errorf("Delete of nothing = %v", err)
	}
	if err := s.Save(sampleTokens); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || !got.Expiry.Equal(sampleTokens.Expiry) || got.RefreshToken != "rt-1" || got.UserID != "42" {
		t.Errorf("Load = %+v, %v", got, err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("Load after Delete err = %v", err)
	}
	if s.Name() != "system keyring" {
		t.Errorf("Name = %q", s.Name())
	}

	_ = keyring.Set(keyringService, keyringUser, "{not json")
	if _, err := s.Load(); err == nil || !strings.Contains(err.Error(), "decode keyring") {
		t.Errorf("corrupt entry err = %v", err)
	}
}

func TestKeyringStoreUnavailable(t *testing.T) {
	boom := errors.New("dbus: no session bus")
	keyring.MockInitWithError(boom)
	t.Cleanup(keyring.MockInit)
	s := KeyringStore{}
	if _, err := s.Load(); !errors.Is(err, boom) {
		t.Errorf("Load err = %v", err)
	}
	if err := s.Save(sampleTokens); !errors.Is(err, boom) {
		t.Errorf("Save err = %v", err)
	}
	if err := s.Delete(); !errors.Is(err, boom) {
		t.Errorf("Delete err = %v", err)
	}
}

func TestFileStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg", "tidal-tui")
	s := FileStore{Path: filepath.Join(dir, CredentialsFile)}

	if _, err := s.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("empty Load err = %v", err)
	}
	if err := s.Save(sampleTokens); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, s.Path: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
	got, err := s.Load()
	if err != nil || got.AccessToken != "at-1" || !got.Expiry.Equal(sampleTokens.Expiry) {
		t.Errorf("Load = %+v, %v", got, err)
	}
	// Re-login replaces the single account.
	second := sampleTokens
	second.UserID = "99"
	if err := s.Save(second); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Load(); got.UserID != "99" {
		t.Errorf("after replace UserID = %q", got.UserID)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover temp files: %v", entries)
	}
	if !strings.Contains(s.Name(), s.Path) {
		t.Errorf("Name = %q", s.Name())
	}

	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Errorf("second Delete = %v", err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("Load after Delete err = %v", err)
	}
}

func TestFileStoreErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, CredentialsFile)
	s := FileStore{Path: path}

	tests := []struct {
		name    string
		setup   func(t *testing.T)
		op      func() error
		wantErr string
	}{
		{
			name:    "world-readable file is refused",
			setup:   func(t *testing.T) { write(t, path, `{}`, 0o644) },
			op:      func() error { _, err := s.Load(); return err },
			wantErr: "accessible by other users",
		},
		{
			name:    "corrupt file",
			setup:   func(t *testing.T) { write(t, path, `{nope`, 0o600) },
			op:      func() error { _, err := s.Load(); return err },
			wantErr: "decode credentials file",
		},
		{
			name:    "unreadable file",
			setup:   func(t *testing.T) { write(t, path, `{}`, 0o000) },
			op:      func() error { _, err := s.Load(); return err },
			wantErr: "open credentials file",
		},
		{
			name: "config dir is a file",
			op: func() error {
				blocker := filepath.Join(dir, "blocker")
				write(t, blocker, "", 0o600)
				return FileStore{Path: filepath.Join(blocker, "sub", CredentialsFile)}.Save(sampleTokens)
			},
			wantErr: "create credentials dir",
		},
		{
			name: "read-only dir",
			op: func() error {
				ro := filepath.Join(dir, "ro")
				if err := os.Mkdir(ro, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(ro, 0o700) })
				return FileStore{Path: filepath.Join(ro, CredentialsFile)}.Save(sampleTokens)
			},
			wantErr: "create temp credentials file",
		},
		{
			name: "target is a directory",
			op: func() error {
				target := filepath.Join(dir, "isdir")
				if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
					t.Fatal(err)
				}
				return FileStore{Path: target}.Save(sampleTokens)
			},
			wantErr: "save credentials file",
		},
		{
			name: "delete a non-empty directory",
			op: func() error {
				target := filepath.Join(dir, "deldir")
				if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
					t.Fatal(err)
				}
				return FileStore{Path: target}.Delete()
			},
			wantErr: "delete credentials file",
		},
	}
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Remove(path)
			if tt.setup != nil {
				tt.setup(t)
			}
			err := tt.op()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestNewStore(t *testing.T) {
	dir := t.TempDir()

	keyring.MockInit()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if _, ok := NewStore(dir, logger).(KeyringStore); !ok {
		t.Error("keyring available: want KeyringStore")
	}
	if logs.Len() != 0 {
		t.Errorf("unexpected log: %s", logs.String())
	}

	keyring.MockInitWithError(errors.New("no secret service"))
	t.Cleanup(keyring.MockInit)
	s := NewStore(dir, logger)
	fsStore, ok := s.(FileStore)
	if !ok || fsStore.Path != filepath.Join(dir, CredentialsFile) {
		t.Errorf("keyring unavailable: store = %#v", s)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "keyring unavailable") {
		t.Errorf("missing fallback warning: %s", logs.String())
	}
}

func TestDeleteAll(t *testing.T) {
	dir := t.TempDir()
	fileStore := FileStore{Path: filepath.Join(dir, CredentialsFile)}

	keyring.MockInit()
	if err := (KeyringStore{}).Save(sampleTokens); err != nil {
		t.Fatal(err)
	}
	if err := fileStore.Save(sampleTokens); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := (KeyringStore{}).Load(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("keyring entry survived logout: %v", err)
	}
	if _, err := fileStore.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("file survived logout: %v", err)
	}

	// Keyring unreachable: the file is still removed.
	if err := fileStore.Save(sampleTokens); err != nil {
		t.Fatal(err)
	}
	keyring.MockInitWithError(errors.New("no secret service"))
	t.Cleanup(keyring.MockInit)
	if err := DeleteAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := fileStore.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("file survived logout without keyring: %v", err)
	}
}

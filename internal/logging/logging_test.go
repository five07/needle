package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesPrivateFileAndAppends(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state", "needle")

	for i, msg := range []string{"first", "second"} {
		logger, closer, err := Open(stateDir, slog.LevelInfo)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		logger.Info(msg, "track_id", "330930030")
		logger.Debug("hidden at info level")
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	dirInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("state dir mode = %o, want 700", got)
	}
	path := filepath.Join(stateDir, FileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("log file mode = %o, want 600", got)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, want := range []string{"msg=first", "msg=second", "track_id=330930030"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden at info level") {
		t.Errorf("debug record written at info level:\n%s", out)
	}
}

func TestOpenTightensExistingFileMode(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, FileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, closer, err := Open(stateDir, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("log file mode = %o, want 600", got)
	}
}

func TestOpenErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
		want  string
	}{
		{
			name: "state dir path is a file",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(p, "needle")
			},
			want: "create state dir",
		},
		{
			name: "log path is a directory",
			setup: func(t *testing.T) string {
				d := t.TempDir()
				if err := os.Mkdir(filepath.Join(d, FileName), 0o700); err != nil {
					t.Fatal(err)
				}
				return d
			},
			want: "open log file",
		},
	}
	tests = append(tests, struct {
		name  string
		setup func(t *testing.T) string
		want  string
	}{
		name: "permission change fails",
		setup: func(t *testing.T) string {
			old := chmod
			chmod = func(*os.File, os.FileMode) error { return os.ErrPermission }
			t.Cleanup(func() { chmod = old })
			return t.TempDir()
		},
		want: "set log file mode",
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Open(tt.setup(t), slog.LevelInfo)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

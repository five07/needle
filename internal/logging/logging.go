// Package logging sets up the application's structured log file. A terminal
// UI owns stdout and stderr, so all logs go to a file under the XDG state
// directory.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// FileName is the log file name inside the state directory.
const FileName = "tidal-tui.log"

// chmod is a seam so tests can simulate a permission change failure.
var chmod = (*os.File).Chmod

// Open creates stateDir if needed (mode 0700) and returns a logger that
// appends to stateDir/tidal-tui.log (mode 0600). The caller must close the
// returned io.Closer when done.
func Open(stateDir string, level slog.Level) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("logging: create state dir: %w", err)
	}
	path := filepath.Join(stateDir, FileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("logging: open log file: %w", err)
	}
	// Tighten permissions on files created by older runs or a lax umask.
	if err := chmod(f, 0o600); err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("logging: set log file mode: %w", err)
	}
	h := slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})
	return slog.New(h), f, nil
}

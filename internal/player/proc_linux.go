package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// configureProcess makes the kernel stop mpv if needle dies, so a crash
// never leaves an orphaned player holding the audio device.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

// privateDir returns a directory only this user can access, for the IPC
// socket and manifest files. It prefers $XDG_RUNTIME_DIR/needle and falls
// back to a new temporary directory (owned=true: remove it on Close).
func privateDir(getenv func(string) string) (dir string, owned bool, err error) {
	if rd := getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(rd) {
		dir = filepath.Join(rd, "needle")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", false, fmt.Errorf("player: create runtime dir: %w", err)
		}
		if err := checkPrivate(dir); err != nil {
			return "", false, err
		}
		return dir, false, nil
	}
	dir, err = os.MkdirTemp("", "needle-")
	if err != nil {
		return "", false, fmt.Errorf("player: create runtime dir: %w", err)
	}
	return dir, true, nil
}

// checkPrivate requires a real directory owned by this user with no group or
// other access, tightening the mode when only that is wrong.
func checkPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("player: runtime dir: %w", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok {
		return errors.New("player: runtime dir is not a directory")
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("player: runtime dir %s is owned by another user", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("player: runtime dir: %w", err)
		}
	}
	return nil
}

// PrivateDir is privateDir for other packages (the visualizer's decoder
// keeps its manifest copies there too).
func PrivateDir(getenv func(string) string) (dir string, owned bool, err error) {
	return privateDir(getenv)
}

// Package xdg resolves the per-user directories tidal-tui uses, following the
// XDG Base Directory specification.
package xdg

import (
	"errors"
	"path/filepath"
)

// AppName is the directory name used under each XDG base directory.
const AppName = "tidal-tui"

// ErrNoHome is returned when neither the XDG variable nor HOME is usable.
var ErrNoHome = errors.New("xdg: HOME is not set and no absolute XDG directory was given")

// Dirs holds the application's resolved directories.
type Dirs struct {
	// Config is the directory holding config.toml.
	Config string
	// State is the directory holding logs and other persistent state.
	State string
}

// Resolve returns the application directories, reading the environment
// through getenv. Relative XDG values are ignored, as the specification
// requires, and the HOME-based defaults are used instead.
func Resolve(getenv func(string) string) (Dirs, error) {
	cfg, err := base(getenv, "XDG_CONFIG_HOME", ".config")
	if err != nil {
		return Dirs{}, err
	}
	state, err := base(getenv, "XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return Dirs{}, err
	}
	return Dirs{
		Config: filepath.Join(cfg, AppName),
		State:  filepath.Join(state, AppName),
	}, nil
}

func base(getenv func(string) string, envVar, homeRel string) (string, error) {
	if v := getenv(envVar); filepath.IsAbs(v) {
		return filepath.Clean(v), nil
	}
	home := getenv("HOME")
	if !filepath.IsAbs(home) {
		return "", ErrNoHome
	}
	return filepath.Join(home, homeRel), nil
}

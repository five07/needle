// Command tidal-tui is a keyboard-driven terminal client for TIDAL with
// bit-perfect playback.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/logging"
	"github.com/five07/tidal-tui/internal/xdg"
)

// version is the release version, set at build time with
// -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run parses args and executes the program, returning the process exit code.
// The environment is read through getenv so tests can control it.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tidal-tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: tidal-tui [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "tidal-tui: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "tidal-tui %s\n", version)
		return 0
	}

	dirs, err := xdg.Resolve(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	cfg, err := config.Load(filepath.Join(dirs.Config, config.FileName), getenv)
	if err != nil {
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	logger, closer, err := logging.Open(dirs.State, slog.LevelInfo)
	if err != nil {
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	defer closer.Close()
	logger.Info("starting",
		"version", version,
		"quality", cfg.Quality,
		"audio_mode", cfg.Audio.Mode,
		"audio_device", cfg.Audio.Device,
		"client_id_set", cfg.Auth.ClientID != "",
	)

	// The interactive UI arrives in TASK-009.
	fmt.Fprintln(stderr, "tidal-tui: the terminal UI is not implemented yet")
	fs.Usage()
	return 0
}

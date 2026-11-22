// Command tidal-tui is a keyboard-driven terminal client for TIDAL with
// bit-perfect playback.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/five07/tidal-tui/internal/auth"
	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/logging"
	"github.com/five07/tidal-tui/internal/player"
	"github.com/five07/tidal-tui/internal/tidal"
	"github.com/five07/tidal-tui/internal/tui"
	"github.com/five07/tidal-tui/internal/xdg"
)

// version is the release version, set at build time with
// -ldflags "-X main.version=v1.2.3".
var version = "dev"

// deps holds the collaborators that tests replace: the browser login step and
// the API endpoint.
type deps struct {
	newOAuth   func(clientID string) *auth.OAuth
	login      func(ctx context.Context, o *auth.OAuth, out io.Writer) (auth.Tokens, error)
	apiOptions []tidal.Option
	// asoundRoot is the procfs ALSA directory ("" means /proc/asound).
	asoundRoot string
	// mpvDeviceHelp returns `mpv --audio-device=help` output.
	mpvDeviceHelp func(ctx context.Context) ([]byte, error)
	// runUI runs the terminal UI until it quits.
	runUI func(ctx context.Context, m tea.Model) error
}

var defaultDeps = deps{
	runUI: func(ctx context.Context, m tea.Model) error {
		_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
		return err
	},
	mpvDeviceHelp: func(ctx context.Context) ([]byte, error) {
		return player.MPVDeviceHelp(ctx, "")
	},
	newOAuth: auth.NewOAuth,
	login: func(ctx context.Context, o *auth.OAuth, out io.Writer) (auth.Tokens, error) {
		return o.Login(ctx, out)
	},
}

// exit is os.Exit, replaced in tests.
var exit = os.Exit

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, defaultDeps, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	exit(code)
}

// run parses args and executes the program, returning the process exit code.
// The environment is read through getenv so tests can control it.
func run(ctx context.Context, d deps, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tidal-tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	logout := fs.Bool("logout", false, "remove stored TIDAL credentials and exit")
	listDevices := fs.Bool("list-devices", false, "list audio devices for audio.device and exit")
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

	if *listDevices {
		return printDevices(ctx, d, stdout, stderr)
	}

	dirs, err := xdg.Resolve(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	configPath := filepath.Join(dirs.Config, config.FileName)
	cfg, err := config.Load(configPath, getenv)
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

	if *logout {
		if err := auth.DeleteAll(dirs.Config); err != nil {
			logger.Error("logout failed", "err", err)
			fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
			return 1
		}
		logger.Info("logged out")
		fmt.Fprintln(stdout, "Logged out.")
		return 0
	}
	if cfg.Auth.ClientID == "" {
		fmt.Fprintf(stderr, "tidal-tui: no TIDAL client ID: set auth.client_id in %s or the %s environment variable\n",
			configPath, config.ClientIDEnv)
		return 1
	}

	oauth := d.newOAuth(cfg.Auth.ClientID)
	store := auth.NewStore(dirs.Config, logger)
	tokens := auth.NewTokenSource(store, oauth)
	opts := append([]tidal.Option{tidal.WithLogger(logger), tidal.WithUserAgent("tidal-tui/" + version)}, d.apiOptions...)
	client, err := tidal.New(tokens, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	user, err := account(ctx, d, oauth, store, tokens, client, logger, stderr)
	if err != nil {
		logger.Error("login failed", "err", err)
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Logged in as %s (%s).\n", user.Username, user.Country)

	logger.Info("ui started")
	if err := d.runUI(ctx, tui.New(tui.Config{})); err != nil && ctx.Err() == nil {
		logger.Error("ui failed", "err", err)
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	logger.Info("ui stopped")
	return 0
}

// account returns the logged-in user, running the browser login first when
// there are no stored credentials, or again once when stored credentials
// were revoked.
func account(ctx context.Context, d deps, oauth *auth.OAuth, store auth.Store, tokens *auth.TokenSource,
	client *tidal.Client, logger *slog.Logger, out io.Writer) (tidal.User, error) {
	login := func() error {
		t, err := d.login(ctx, oauth, out)
		if err != nil {
			return err
		}
		if err := tokens.Set(t); err != nil {
			return err
		}
		logger.Info("logged in", "store", store.Name(), "user_id", t.UserID)
		return nil
	}

	has, err := tokens.HasCredentials()
	if err != nil {
		return tidal.User{}, err
	}
	loggedIn := false
	if !has {
		if err := login(); err != nil {
			return tidal.User{}, err
		}
		loggedIn = true
	}
	user, err := client.Me(ctx)
	if errors.Is(err, auth.ErrLoginRequired) && !loggedIn {
		logger.Warn("stored credentials are no longer valid, logging in again")
		fmt.Fprintln(out, "Your TIDAL session expired. Please log in again.")
		if err := login(); err != nil {
			return tidal.User{}, err
		}
		user, err = client.Me(ctx)
	}
	return user, err
}

// printDevices lists exclusive (ALSA hw) and shared (PipeWire) devices.
func printDevices(ctx context.Context, d deps, stdout, stderr io.Writer) int {
	exclusive, err := player.ExclusiveDevices(d.asoundRoot)
	if err != nil {
		fmt.Fprintf(stderr, "tidal-tui: %v\n", err)
		return 1
	}
	var shared []player.Device
	help, sharedErr := d.mpvDeviceHelp(ctx)
	if sharedErr == nil {
		shared = player.SharedDevices(help)
	}
	player.WriteDeviceList(stdout, exclusive, shared, sharedErr)
	return 0
}

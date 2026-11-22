// Command needle is a keyboard-driven terminal client for TIDAL with
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
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/five07/needle/internal/auth"
	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/logging"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/stream"
	"github.com/five07/needle/internal/tidal"
	"github.com/five07/needle/internal/tui"
	"github.com/five07/needle/internal/xdg"
	"github.com/godbus/dbus/v5"
)

// version is the release version, set at build time with
// -ldflags "-X main.version=v1.2.3".
var version = "dev"

// affiliation is the notice printed by -version and in the README
// (decision 39).
const affiliation = "needle is an independent project and is not affiliated with or endorsed by TIDAL."

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
	// newEngine starts the audio engine on first playback.
	newEngine func(ctx context.Context, cfg player.MPVConfig) (player.Engine, error)
	// sessionBus connects to the D-Bus session bus for MPRIS. Nil turns
	// MPRIS off (tests).
	sessionBus func() (*dbus.Conn, error)
	// visualizerMPV is the mpv the visualizer decodes with (decision 67).
	visualizerMPV string
}

var defaultDeps = deps{
	visualizerMPV: "mpv",
	runUI: func(ctx context.Context, m tea.Model) error {
		_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
		return err
	},
	mpvDeviceHelp: func(ctx context.Context) ([]byte, error) {
		return player.MPVDeviceHelp(ctx, "")
	},
	newEngine: func(ctx context.Context, cfg player.MPVConfig) (player.Engine, error) {
		m, err := player.StartMPV(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return m, nil
	},
	newOAuth:   auth.NewOAuth,
	sessionBus: func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() },
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
	fs := flag.NewFlagSet("needle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	logout := fs.Bool("logout", false, "remove stored TIDAL credentials and exit")
	listDevices := fs.Bool("list-devices", false, "list audio devices for audio.device and exit")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: needle [flags]")
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
		fmt.Fprintf(stderr, "needle: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "needle %s\n%s\n", version, affiliation)
		return 0
	}

	if *listDevices {
		return printDevices(ctx, d, stdout, stderr)
	}

	dirs, err := xdg.Resolve(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "needle: %v\n", err)
		return 1
	}
	configPath := filepath.Join(dirs.Config, config.FileName)
	cfg, err := config.Load(configPath, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "needle: %v\n", err)
		return 1
	}
	logger, closer, err := logging.Open(dirs.State, slog.LevelInfo)
	if err != nil {
		fmt.Fprintf(stderr, "needle: %v\n", err)
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
			fmt.Fprintf(stderr, "needle: %v\n", err)
			return 1
		}
		for _, forget := range []func() error{
			func() error { return forgetBestQuality(dirs.State) },
			func() error { return forgetFile(dirs.State, QueueFile) },
		} {
			if err := forget(); err != nil {
				logger.Error("logout failed", "err", err)
				fmt.Fprintf(stderr, "needle: %v\n", err)
				return 1
			}
		}
		logger.Info("logged out")
		fmt.Fprintln(stdout, "Logged out.")
		return 0
	}
	if cfg.Auth.ClientID == "" {
		fmt.Fprintf(stderr, "needle: no TIDAL client ID: set auth.client_id in %s or the %s environment variable\n",
			configPath, config.ClientIDEnv)
		return 1
	}

	oauth := d.newOAuth(cfg.Auth.ClientID)
	store := auth.NewStore(dirs.Config, logger)
	tokens := auth.NewTokenSource(store, oauth)
	opts := append([]tidal.Option{tidal.WithLogger(logger), tidal.WithUserAgent("needle/" + version)}, d.apiOptions...)
	client, err := tidal.New(tokens, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "needle: %v\n", err)
		return 1
	}
	user, err := account(ctx, d, oauth, store, tokens, client, logger, stderr)
	if err != nil {
		logger.Error("login failed", "err", err)
		fmt.Fprintf(stderr, "needle: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Logged in as %s (%s).\n", user.Username, user.Country)

	model, stopPlayer, err := newUI(ctx, d, cfg, configPath, client, user, localeFromEnv(getenv), dirs.State, logger)
	if err != nil {
		logger.Error("ui setup failed", "err", err)
		fmt.Fprintf(stderr, "needle: %v\n", err)
		return 1
	}
	logger.Info("ui started")
	err = d.runUI(ctx, model)
	stopPlayer()
	if err != nil && ctx.Err() == nil {
		logger.Error("ui failed", "err", err)
		fmt.Fprintf(stderr, "needle: %v\n", err)
		return 1
	}
	logger.Info("ui stopped")
	return 0
}

// newUI builds the terminal UI with the catalog and a running player (which
// starts mpv on first playback). stop ends the player and waits for it to
// close mpv.
func newUI(ctx context.Context, d deps, cfg config.Config, configPath string, client *tidal.Client, user tidal.User,
	locale, stateDir string, logger *slog.Logger) (model tea.Model, stop func(), err error) {
	catalog, err := tidal.NewCatalog(client, user.Country)
	if err != nil {
		return nil, nil, err
	}
	if locale != "" {
		if err := catalog.SetLocale(locale); err != nil {
			logger.Info("locale not used for mix names", "locale", locale) // TIDAL's default applies
		}
	}
	resolver, err := stream.NewResolver(client, cfg.Quality, logger)
	if err != nil {
		return nil, nil, err
	}
	q := queue.New[player.Track]()
	var saved *savedQueue
	restored := false
	if stateDir != "" {
		saved, restored = loadSavedQueue(filepath.Join(stateDir, QueueFile), q, logger)
	}
	p := player.New(playerConfig(d, cfg, resolver, q, logger))

	uiCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := p.Run(uiCtx); err != nil && uiCtx.Err() == nil {
			logger.Error("player stopped", "err", err)
		}
	}()
	notices := make(chan string, 4)
	status := make(chan tui.NowPlaying, 1)
	pb := &playback{q: q, p: p, changed: make(chan struct{}, 1)}
	pb.autoplay.Store(cfg.Autoplay)
	var best *bestQuality
	if stateDir != "" {
		best = loadBestQuality(filepath.Join(stateDir, BestQualityFile), logger)
	} else {
		best = &bestQuality{logger: logger} // not remembered
	}
	w := watch{notices: notices, status: status, q: q, changed: pb.changed, mode: cfg.Audio.Mode, best: best, saved: saved,
		auto: newAutoplay(catalog, pb, logger)}
	vis, stopVis := startVisualizer(uiCtx, p, d.visualizerMPV, cfg.Visualizer.Enabled, os.Getenv, logger)
	w.vis = vis
	mp, stopMPRIS := startMPRIS(d.sessionBus, pb, logger)
	if mp != nil {
		w.mpris = mp // only when registered: a nil *Service in the interface would not be nil
	}
	watched := make(chan struct{})
	go func() { defer close(watched); watchStatus(uiCtx, p, w) }()
	if restored {
		pb.edited() // the bar and Queue view show the restored queue at once
	}
	acct := tui.AccountInfo{Username: user.Username, Country: user.Country, Email: user.Email, EmailVerified: user.EmailVerified}
	settings := tui.SettingsInfo{Quality: string(cfg.Quality), AudioMode: string(cfg.Audio.Mode), AudioDevice: cfg.Audio.Device,
		Autoplay: cfg.Autoplay, Visualizer: cfg.Visualizer.Enabled, Theme: cfg.Visualizer.Theme, Custom: cfg.Visualizer.Custom, Themes: config.VisualizerThemes}

	settingsSvc := newSettingsService(configPath, cfg.Audio, logger, func(text string) {
		select {
		case notices <- text:
		default: // the hint line is busy; the log has it
		}
	})
	settingsSvc.setQuality = func(q config.Quality) {
		if err := resolver.SetQuality(q); err != nil {
			logger.Warn("quality not changed", "quality", q, "err", err)
			return
		}
		if err := p.DropPreload(uiCtx); err != nil && uiCtx.Err() == nil {
			logger.Warn("preload not dropped", "err", err)
		}
	}
	settingsSvc.setAutoplay = func(on bool) {
		if err := pb.SetAutoplay(uiCtx, on); err != nil && uiCtx.Err() == nil {
			logger.Warn("autoplay not changed", "err", err)
		}
	}
	settingsSvc.setVisualizer = vis.SetOn
	settingsSvc.switchAudio = func(a config.Audio) {
		if err := p.SetAudio(uiCtx, audioFor(d, a, logger)); err != nil && uiCtx.Err() == nil {
			logger.Warn("audio output not switched", "mode", a.Mode, "err", err)
		}
	}
	settingsSvc.devices = listDevices(d)

	svc := &tui.Services{Ctx: uiCtx, Catalog: catalog, Library: catalog, Discovery: catalog,
		Playback: pb, Transport: pb, Visualizer: vis, Settings: settingsSvc, Logger: logger}
	model = tui.New(tui.Config{
		Services:  svc,
		Account:   tui.NewAccountView(svc, acct),
		Settings:  tui.NewSettingsView(svc, settings),
		Notices:   notices,
		Status:    status,
		Spectrum:  vis.Frames(),
		OpenQueue: restored,
	})
	// Waiting for the watcher lets its last queue save finish before exit.
	return model, func() {
		settingsSvc.Close()
		cancel()
		<-done
		<-watched
		stopVis()
		stopMPRIS()
	}, nil
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
		fmt.Fprintf(stderr, "needle: %v\n", err)
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

// playerConfig configures the player for the audio settings in cfg.
func playerConfig(d deps, cfg config.Config, r player.Resolver, q *queue.Queue[player.Track], logger *slog.Logger) player.Config {
	a := audioFor(d, cfg.Audio, logger)
	return player.Config{
		Mode:      a.Mode,
		NewEngine: a.NewEngine,
		Resolver:  r,
		Queue:     q,
		Prober:    a.Prober,
		Logger:    logger,
	}
}

// localeFromEnv turns the POSIX locale (LC_ALL, then LC_MESSAGES, then
// LANG, for example "en_US.UTF-8") into a BCP 47 tag ("en-US") for mix
// names. "C", "POSIX", and unset give "", which leaves TIDAL's default.
func localeFromEnv(getenv func(string) string) string {
	v := ""
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v = getenv(k); v != "" {
			break
		}
	}
	v, _, _ = strings.Cut(v, ".") // drop the codeset
	v, _, _ = strings.Cut(v, "@") // and the modifier
	if v == "C" || v == "POSIX" {
		return ""
	}
	return strings.ReplaceAll(v, "_", "-")
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/tui"
)

// settleDelay is how long settings wait without further changes before
// they are saved and the audio output is switched (decisions 76, 80, 81).
const settleDelay = time.Second

// stopper is a pending timer.
type stopper interface{ Stop() bool }

// settingsService implements tui.SettingsService. Quality, autoplay, the
// volume control, and the visualizer apply to the session at once; the audio mode and device
// switch, and every save, wait for settleDelay without further changes.
type settingsService struct {
	path   string
	logger *slog.Logger
	notify func(string)
	delay  time.Duration
	after  func(time.Duration, func()) stopper

	setQuality    func(config.Quality)
	setAutoplay   func(bool)
	setVisualizer func(bool)
	setVolume     func(config.VolumeControl)
	switchAudio   func(config.Audio)
	devices       func(ctx context.Context, mode config.AudioMode) ([]player.Device, error)

	flushMu sync.Mutex // one flush at a time
	mu      sync.Mutex
	pending []config.Change
	audio   config.Audio // the session's audio settings
	switchA bool         // audio changed since the last switch
	timer   stopper
	closed  bool
}

func newSettingsService(path string, audio config.Audio, logger *slog.Logger, notify func(string)) *settingsService {
	return &settingsService{path: path, audio: audio, logger: logger, notify: notify, delay: settleDelay,
		after: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) }}
}

// Apply implements tui.SettingsService.
func (s *settingsService) Apply(key string, value any) {
	str, _ := value.(string)
	on, _ := value.(bool)
	switch key {
	case "quality":
		if s.setQuality != nil {
			s.setQuality(config.Quality(str))
		}
	case "autoplay":
		if s.setAutoplay != nil {
			s.setAutoplay(on)
		}
	case "visualizer.enabled":
		if s.setVisualizer != nil {
			s.setVisualizer(on)
		}
	case "audio.volume":
		if s.setVolume != nil {
			s.setVolume(config.VolumeControl(str))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	switch key {
	case "audio.mode":
		s.audio.Mode, s.switchA = config.AudioMode(str), true
	case "audio.device":
		s.audio.Device, s.switchA = str, true
	}
	s.pending = slices.DeleteFunc(s.pending, func(c config.Change) bool { return c.Key == key })
	s.pending = append(s.pending, config.Change{Key: key, Value: value})
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = s.after(s.delay, func() { s.flush(true) })
}

// Close saves anything still pending, at once. Later changes are ignored.
func (s *settingsService) Close() {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	s.flush(false) // quitting: save, but do not start a new engine
}

// flush switches the audio output if it changed (and switching is wanted)
// and saves the pending changes.
func (s *settingsService) flush(switching bool) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	changes, audio, sw := s.pending, s.audio, s.switchA
	s.pending, s.switchA = nil, false
	s.mu.Unlock()
	if sw && switching && s.switchAudio != nil {
		s.switchAudio(audio)
	}
	s.save(changes)
}

// save writes changes to config.toml. A key the file writes in a form
// Edit cannot update is left out, with a notice naming it, and the rest
// are still saved.
func (s *settingsService) save(changes []config.Change) {
	if s.path == "" {
		return // not saved (tests)
	}
	for len(changes) > 0 {
		err := config.Edit(s.path, changes...)
		if err == nil {
			s.logger.Info("settings saved", "keys", keysOf(changes))
			return
		}
		var ue *config.UnsupportedError
		if !errors.As(err, &ue) {
			s.logger.Warn("settings not saved", "keys", keysOf(changes), "err", err)
			s.notify("Settings not saved: " + err.Error())
			return
		}
		s.logger.Warn("setting not saved", "key", ue.Key, "err", err)
		s.notify(fmt.Sprintf("%s not saved: change it in config.toml by hand.", ue.Key))
		changes = slices.DeleteFunc(changes, func(c config.Change) bool { return c.Key == ue.Key })
	}
}

func keysOf(changes []config.Change) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Key
	}
	return out
}

// Devices implements tui.SettingsService: the devices -list-devices shows
// for mode, with "auto" first in shared mode.
func (s *settingsService) Devices(ctx context.Context, mode string) ([]tui.Device, error) {
	found, err := s.devices(ctx, config.AudioMode(mode))
	if err != nil {
		if config.AudioMode(mode) == config.AudioExclusive {
			return nil, err
		}
		s.logger.Warn("shared audio devices not listed", "err", err)
		found = nil // only "auto"
	}
	var out []tui.Device
	if config.AudioMode(mode) == config.AudioShared {
		out = append(out, tui.Device{Name: config.DefaultAudioDevice, Description: "System default"})
	}
	for _, d := range found {
		if d.Name == config.DefaultAudioDevice {
			continue
		}
		desc := d.Description
		if d.BuiltIn {
			desc += " (built-in)"
		}
		out = append(out, tui.Device{Name: d.Name, Description: desc})
	}
	return out, nil
}

// listDevices returns the devices for mode: ALSA hardware devices for
// exclusive, PipeWire sinks for shared. A shared listing that fails (mpv
// missing) still offers "auto".
func listDevices(d deps) func(ctx context.Context, mode config.AudioMode) ([]player.Device, error) {
	return func(ctx context.Context, mode config.AudioMode) ([]player.Device, error) {
		if mode == config.AudioExclusive {
			return player.ExclusiveDevices(d.asoundRoot)
		}
		help, err := d.mpvDeviceHelp(ctx)
		if err != nil {
			return nil, err
		}
		return player.SharedDevices(help), nil
	}
}

// audioFor builds the player's audio setup for mode and device.
func audioFor(d deps, a config.Audio, logger *slog.Logger) player.Audio {
	mpvCfg := player.MPVConfig{Mode: a.Mode, Device: a.Device, Logger: logger}
	return player.Audio{
		Mode:      a.Mode,
		NewEngine: func(ctx context.Context) (player.Engine, error) { return d.newEngine(ctx, mpvCfg) },
		Prober:    player.NewProber(a.Mode, a.Device),
	}
}

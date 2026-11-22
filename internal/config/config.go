// Package config loads and validates the tidal-tui configuration file.
//
// The file lives at $XDG_CONFIG_HOME/tidal-tui/config.toml. Every key is
// optional; a missing file means all defaults apply.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the configuration file name inside the config directory.
const FileName = "config.toml"

// ClientIDEnv is the environment variable that overrides the build-time
// client ID, mainly for development builds.
const ClientIDEnv = "TIDAL_CLIENT_ID"

// DefaultClientID is the TIDAL OAuth client ID used when neither the config
// file nor the environment provides one. It is empty in source and set at
// build time:
//
//	go build -ldflags "-X github.com/five07/tidal-tui/internal/config.DefaultClientID=<id>"
var DefaultClientID = ""

// Quality is the requested stream quality.
type Quality string

// Supported stream qualities.
const (
	QualityAAC      Quality = "aac"
	QualityLossless Quality = "lossless"
	QualityHiRes    Quality = "hires"
)

// AudioMode selects how audio reaches the output device.
type AudioMode string

// Supported audio modes.
const (
	// AudioShared plays through the system sound server (PipeWire).
	AudioShared AudioMode = "shared"
	// AudioExclusive opens the output device directly for bit-perfect output.
	AudioExclusive AudioMode = "exclusive"
)

// DefaultAudioDevice selects the system default output device.
const DefaultAudioDevice = "auto"

// Config is the validated application configuration.
type Config struct {
	Quality Quality `toml:"quality"`
	Audio   Audio   `toml:"audio"`
	Auth    Auth    `toml:"auth"`
}

// Audio holds output settings.
type Audio struct {
	// Mode is shared or exclusive.
	Mode AudioMode `toml:"mode"`
	// Device is an mpv audio device name, or "auto" for the system default.
	Device string `toml:"device"`
}

// Auth holds OAuth settings.
type Auth struct {
	// ClientID is the TIDAL developer app's public client ID.
	ClientID string `toml:"client_id"`
}

// ValidationError reports a configuration key with an unusable value.
type ValidationError struct {
	// Key is the dotted TOML key, for example "audio.mode".
	Key string
	// Reason explains what is wrong with the value.
	Reason string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Key, e.Reason)
}

// Defaults returns the configuration used for any key the file omits.
// The client ID comes from the environment or the build-time default.
func Defaults(getenv func(string) string) Config {
	clientID := DefaultClientID
	if v := strings.TrimSpace(getenv(ClientIDEnv)); v != "" {
		clientID = v
	}
	return Config{
		Quality: QualityHiRes,
		Audio:   Audio{Mode: AudioShared, Device: DefaultAudioDevice},
		Auth:    Auth{ClientID: clientID},
	}
}

// Load reads the configuration file at path, applies defaults for omitted
// keys, and validates the result. A missing file yields the defaults.
// Invalid values and unknown keys return a *ValidationError.
func Load(path string, getenv func(string) string) (Config, error) {
	cfg := Defaults(getenv)
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, cfg.Validate()
	}
	if err != nil {
		if perr, ok := errors.AsType[toml.ParseError](err); ok {
			if perr.LastKey != "" {
				return Config{}, fmt.Errorf("config: %s: line %d: %s: %s", path, perr.Position.Line, perr.LastKey, perr.Message)
			}
			return Config{}, fmt.Errorf("config: %s: line %d: %s", path, perr.Position.Line, perr.Message)
		}
		return Config{}, fmt.Errorf("config: %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, &ValidationError{Key: undecoded[0].String(), Reason: "unknown key"}
	}
	cfg.Audio.Device = strings.TrimSpace(cfg.Audio.Device)
	cfg.Auth.ClientID = strings.TrimSpace(cfg.Auth.ClientID)
	return cfg, cfg.Validate()
}

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Validate checks every value and returns a *ValidationError for the first
// invalid one. An empty client ID is allowed here; login reports it.
func (c Config) Validate() error {
	switch c.Quality {
	case QualityAAC, QualityLossless, QualityHiRes:
	default:
		return &ValidationError{Key: "quality", Reason: fmt.Sprintf("%q is not one of aac, lossless, hires", c.Quality)}
	}
	switch c.Audio.Mode {
	case AudioShared, AudioExclusive:
	default:
		return &ValidationError{Key: "audio.mode", Reason: fmt.Sprintf("%q is not one of shared, exclusive", c.Audio.Mode)}
	}
	if c.Audio.Device == "" {
		return &ValidationError{Key: "audio.device", Reason: `must not be empty (use "auto" for the system default)`}
	}
	if strings.ContainsFunc(c.Audio.Device, isControl) {
		return &ValidationError{Key: "audio.device", Reason: "contains control characters"}
	}
	if c.Auth.ClientID != "" && !clientIDPattern.MatchString(c.Auth.ClientID) {
		return &ValidationError{Key: "auth.client_id", Reason: "must be 1-128 letters, digits, '.', '_' or '-'"}
	}
	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// Package config loads and validates the needle configuration file.
//
// The file lives at $XDG_CONFIG_HOME/needle/config.toml. Every key is
// optional; a missing file means all defaults apply.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
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
//	go build -ldflags "-X github.com/five07/needle/internal/config.DefaultClientID=<id>"
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

// VolumeControl selects how volume works in exclusive mode (decisions 83
// and 84).
type VolumeControl string

// Supported volume controls.
const (
	// VolumeDevice leaves volume to the output device (the DAC or amp) in
	// exclusive mode. Shared mode keeps software volume.
	VolumeDevice VolumeControl = "device"
	// VolumeSoftware allows software volume in exclusive mode too. Below
	// 100% the samples change, so the output is no longer bit-perfect.
	VolumeSoftware VolumeControl = "software"
)

// Config is the validated application configuration.
type Config struct {
	Quality Quality `toml:"quality"`
	// Autoplay continues with track radio when the queue ends (decision
	// 62). It is the value at startup; the UI can toggle it.
	Autoplay   bool       `toml:"autoplay"`
	Audio      Audio      `toml:"audio"`
	Visualizer Visualizer `toml:"visualizer"`
	Auth       Auth       `toml:"auth"`
}

// Visualizer holds the spectrum visualizer settings (decisions 69 to 71).
type Visualizer struct {
	// Enabled is the value at startup; v toggles it.
	Enabled bool `toml:"enabled"`
	// Theme is one of VisualizerThemes.
	Theme string `toml:"theme"`
	// Custom is the "custom" theme: 4 colors "#rrggbb", bottom to top.
	Custom []string `toml:"custom"`
}

// ThemeCustom is the theme read from visualizer.custom.
const ThemeCustom = "custom"

// VisualizerThemes lists the visualizer themes, in the order the Settings
// section cycles through them. "terminal" follows the terminal's palette.
var VisualizerThemes = []string{"terminal", "burnt orange", "ice blue", "forest green", "royal purple", "deep red", ThemeCustom}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Audio holds output settings.
type Audio struct {
	// Mode is shared or exclusive.
	Mode AudioMode `toml:"mode"`
	// Device is an mpv audio device name, or "auto" for the system default.
	Device string `toml:"device"`
	// Volume is device or software (decision 83).
	Volume VolumeControl `toml:"volume"`
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
		Quality:    QualityHiRes,
		Audio:      Audio{Mode: AudioShared, Device: DefaultAudioDevice, Volume: VolumeDevice},
		Visualizer: Visualizer{Theme: "terminal"},
		Auth:       Auth{ClientID: clientID},
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
	switch c.Audio.Volume {
	case VolumeDevice, VolumeSoftware:
	default:
		return &ValidationError{Key: "audio.volume", Reason: fmt.Sprintf("%q is not one of device, software", c.Audio.Volume)}
	}
	if c.Audio.Device == "" {
		return &ValidationError{Key: "audio.device", Reason: `must not be empty (use "auto" for the system default)`}
	}
	if strings.ContainsFunc(c.Audio.Device, isControl) {
		return &ValidationError{Key: "audio.device", Reason: "contains control characters"}
	}
	if c.Audio.Mode == AudioExclusive {
		if _, _, ok := ParseALSAHW(c.Audio.Device); !ok {
			return &ValidationError{Key: "audio.device", Reason: fmt.Sprintf(
				`exclusive mode needs an ALSA hardware device such as "alsa/hw:CARD=Name,DEV=0", not %q (run needle -list-devices)`,
				c.Audio.Device)}
		}
	}
	if !slices.Contains(VisualizerThemes, c.Visualizer.Theme) {
		return &ValidationError{Key: "visualizer.theme", Reason: fmt.Sprintf("%q is not one of %s", c.Visualizer.Theme, strings.Join(VisualizerThemes, ", "))}
	}
	if len(c.Visualizer.Custom) > 0 || c.Visualizer.Theme == ThemeCustom {
		if len(c.Visualizer.Custom) != 4 {
			return &ValidationError{Key: "visualizer.custom", Reason: `needs exactly 4 colors such as "#1a2b3c", bottom to top`}
		}
		for _, col := range c.Visualizer.Custom {
			if !hexColor.MatchString(col) {
				return &ValidationError{Key: "visualizer.custom", Reason: fmt.Sprintf(`%q is not a color like "#1a2b3c"`, col)}
			}
		}
	}
	if c.Auth.ClientID != "" && !clientIDPattern.MatchString(c.Auth.ClientID) {
		return &ValidationError{Key: "auth.client_id", Reason: "must be 1-128 letters, digits, '.', '_' or '-'"}
	}
	return nil
}

// alsaHWPattern matches mpv ALSA hw device names: alsa/hw:CARD=Name,DEV=0,
// alsa/hw:Name,0, alsa/hw:4,0, or alsa/hw:4.
var alsaHWPattern = regexp.MustCompile(`^alsa/hw:(?:CARD=)?([A-Za-z0-9_-]{1,64})(?:,(?:DEV=)?([0-9]{1,2}))?$`)

// ParseALSAHW extracts the card (number or id) and device number from an
// mpv ALSA hw device name. Exclusive mode requires such a device (PLAN.md
// decision 19): "auto" or a plughw/front device could convert the format.
func ParseALSAHW(device string) (card string, dev int, ok bool) {
	m := alsaHWPattern.FindStringSubmatch(device)
	if m == nil {
		return "", 0, false
	}
	if m[2] != "" {
		dev, _ = strconv.Atoi(m[2])
	}
	return m[1], dev, true
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	defaults := Config{
		Quality: QualityHiRes,
		Audio:   Audio{Mode: AudioShared, Device: DefaultAudioDevice},
	}

	tests := []struct {
		name       string
		body       *string // nil means the file does not exist
		env        map[string]string
		buildID    string
		want       Config
		wantKey    string // expected ValidationError key
		wantErrSub string // expected substring for non-validation errors
	}{
		{
			name: "missing file yields defaults",
			want: defaults,
		},
		{
			name: "empty file yields defaults",
			body: new(""),
			want: defaults,
		},
		{
			name: "all keys overridden",
			body: new(`
quality = "lossless"
autoplay = true

[audio]
mode = "exclusive"
device = "alsa/hw:1,0"

[auth]
client_id = "abc_DEF-123.x"
`),
			want: Config{
				Quality:  QualityLossless,
				Autoplay: true,
				Audio:    Audio{Mode: AudioExclusive, Device: "alsa/hw:1,0"},
				Auth:     Auth{ClientID: "abc_DEF-123.x"},
			},
		},
		{
			name:       "autoplay must be a boolean",
			body:       new(`autoplay = "yes"`),
			wantErrSub: "autoplay",
		},
		{
			name: "partial override keeps other defaults",
			body: new(`quality = "aac"`),
			want: Config{Quality: QualityAAC, Audio: defaults.Audio},
		},
		{
			name:    "build-time client ID is the default",
			buildID: "buildid",
			want:    Config{Quality: QualityHiRes, Audio: defaults.Audio, Auth: Auth{ClientID: "buildid"}},
		},
		{
			name:    "environment overrides build-time client ID",
			buildID: "buildid",
			env:     map[string]string{ClientIDEnv: " envid "},
			want:    Config{Quality: QualityHiRes, Audio: defaults.Audio, Auth: Auth{ClientID: "envid"}},
		},
		{
			name:    "config file overrides environment client ID",
			body:    new("[auth]\nclient_id = \"fileid\"\n"),
			buildID: "buildid",
			env:     map[string]string{ClientIDEnv: "envid"},
			want:    Config{Quality: QualityHiRes, Audio: defaults.Audio, Auth: Auth{ClientID: "fileid"}},
		},
		{
			name: "values are trimmed",
			body: new("[audio]\ndevice = \"  pipewire  \"\n[auth]\nclient_id = \" id \"\n"),
			want: Config{Quality: QualityHiRes, Audio: Audio{Mode: AudioShared, Device: "pipewire"}, Auth: Auth{ClientID: "id"}},
		},
		{
			name:    "invalid quality",
			body:    new(`quality = "max"`),
			wantKey: "quality",
		},
		{
			name:    "invalid audio mode",
			body:    new("[audio]\nmode = \"loud\"\n"),
			wantKey: "audio.mode",
		},
		{
			name:    "empty audio device",
			body:    new("[audio]\ndevice = \"  \"\n"),
			wantKey: "audio.device",
		},
		{
			name:    "exclusive mode with the default device",
			body:    new("[audio]\nmode = \"exclusive\"\n"),
			wantKey: "audio.device",
		},
		{
			name:    "exclusive mode with a converting ALSA device",
			body:    new("[audio]\nmode = \"exclusive\"\ndevice = \"alsa/plughw:CARD=Vestri,DEV=0\"\n"),
			wantKey: "audio.device",
		},
		{
			name:    "exclusive mode with a PipeWire sink",
			body:    new("[audio]\nmode = \"exclusive\"\ndevice = \"pipewire/alsa_output.usb-x\"\n"),
			wantKey: "audio.device",
		},
		{
			name:    "audio device with control characters",
			body:    new("[audio]\ndevice = \"hw\\u0007\"\n"),
			wantKey: "audio.device",
		},
		{
			name:    "client ID with invalid characters",
			body:    new("[auth]\nclient_id = \"has space\"\n"),
			wantKey: "auth.client_id",
		},
		{
			name:    "invalid client ID from environment",
			env:     map[string]string{ClientIDEnv: "bad/id"},
			wantKey: "auth.client_id",
		},
		{
			name:    "unknown top-level key",
			body:    new(`qualty = "hires"`),
			wantKey: "qualty",
		},
		{
			name:    "unknown nested key",
			body:    new("[audio]\nvolume = 50\n"),
			wantKey: "audio.volume",
		},
		{
			name:       "wrong value type names the key",
			body:       new("quality = 5\n"),
			wantErrSub: "quality",
		},
		{
			name:       "malformed TOML reports the line",
			body:       new("quality = \"hires\"\n[audio"),
			wantErrSub: "line 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := DefaultClientID
			DefaultClientID = tt.buildID
			t.Cleanup(func() { DefaultClientID = old })

			path := filepath.Join(t.TempDir(), FileName)
			if tt.body != nil {
				path = writeConfig(t, *tt.body)
			}

			got, err := Load(path, env(tt.env))

			switch {
			case tt.wantKey != "":
				var verr *ValidationError
				if !errors.As(err, &verr) {
					t.Fatalf("err = %v, want *ValidationError for %q", err, tt.wantKey)
				}
				if verr.Key != tt.wantKey {
					t.Errorf("ValidationError.Key = %q, want %q", verr.Key, tt.wantKey)
				}
				if !strings.Contains(err.Error(), tt.wantKey) {
					t.Errorf("error %q does not name key %q", err, tt.wantKey)
				}
			case tt.wantErrSub != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), path) {
					t.Errorf("error %q does not name the file", err)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("Load() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	dir := t.TempDir() // a directory where a file is expected
	_, err := Load(dir, env(nil))
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v, want an error naming %q", err, dir)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := &ValidationError{Key: "audio.mode", Reason: "bad"}
	if got, want := err.Error(), "config: audio.mode: bad"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestParseALSAHW(t *testing.T) {
	tests := []struct {
		in   string
		card string
		dev  int
		ok   bool
	}{
		{"alsa/hw:CARD=Vestri,DEV=0", "Vestri", 0, true},
		{"alsa/hw:Vestri,1", "Vestri", 1, true},
		{"alsa/hw:4,0", "4", 0, true},
		{"alsa/hw:4", "4", 0, true},
		{"alsa/plughw:CARD=Vestri,DEV=0", "", 0, false},
		{"alsa/front:CARD=Vestri,DEV=0", "", 0, false},
		{"pipewire/x", "", 0, false},
		{"auto", "", 0, false},
		{"alsa/hw:CARD=a b", "", 0, false},
		{"alsa/hw:CARD=X,DEV=123", "", 0, false},
	}
	for _, tt := range tests {
		card, dev, ok := ParseALSAHW(tt.in)
		if card != tt.card || dev != tt.dev || ok != tt.ok {
			t.Errorf("ParseALSAHW(%q) = %q %d %v", tt.in, card, dev, ok)
		}
	}
}

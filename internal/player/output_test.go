package player

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/tidal"
)

const hw192 = `access: RW_INTERLEAVED
format: S32_LE
subformat: STD
channels: 2
rate: 192000 (192000/1)
period_size: 4800
buffer_size: 19200
`

func TestParseHWParams(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    HWParams
		open    bool
		wantErr bool
	}{
		{"running", hw192, HWParams{Format: "S32_LE", Rate: 192000, Channels: 2}, true, false},
		{"closed", "closed\n", HWParams{}, false, false},
		{"garbage", "nothing here\n", HWParams{}, false, true},
		{"rate without ratio", "format: S16_LE\nrate: 44100\n", HWParams{Format: "S16_LE", Rate: 44100}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, open, err := parseHWParams(strings.NewReader(tt.in))
			if (err != nil) != tt.wantErr || open != tt.open || got != tt.want {
				t.Errorf("= %+v %v %v", got, open, err)
			}
		})
	}
	if _, _, err := parseHWParams(errReader{}); err == nil {
		t.Error("read error not reported")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// fakeProc builds /proc/asound with card4 (id Vestri) and a symlink.
func fakeProc(t *testing.T, sub0, sub1 string) string {
	t.Helper()
	root := t.TempDir()
	for i, content := range []string{sub0, sub1} {
		if content == "" {
			continue
		}
		dir := filepath.Join(root, "card4", "pcm0p", "sub"+string(rune('0'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hw_params"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("card4", filepath.Join(root, "Vestri")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestALSAProber(t *testing.T) {
	root := fakeProc(t, "closed\n", hw192)
	for _, card := range []string{"4", "Vestri"} {
		h, err := ALSAProber{Root: root, Card: card}.Probe(context.Background())
		if err != nil || h.Rate != 192000 || h.Format != "S32_LE" || h.Card != card {
			t.Errorf("card %s: %+v %v", card, h, err)
		}
	}
	if _, err := (ALSAProber{Root: fakeProc(t, "closed\n", "")}).Probe(context.Background()); err == nil {
		t.Error("no card accepted")
	}
	idle := fakeProc(t, "closed\n", "")
	if _, err := (ALSAProber{Root: idle, Card: "4"}).Probe(context.Background()); !errors.Is(err, ErrOutputUnknown) {
		t.Errorf("idle device err = %v", err)
	}
	if _, err := (ALSAProber{Root: idle, Card: "4", Device: 3}).Probe(context.Background()); !errors.Is(err, ErrOutputUnknown) {
		t.Errorf("missing pcm err = %v", err)
	}
	unreadable := fakeProc(t, hw192, "")
	os.Chmod(filepath.Join(unreadable, "card4", "pcm0p", "sub0", "hw_params"), 0o000)
	if os.Geteuid() != 0 {
		if _, err := (ALSAProber{Root: unreadable, Card: "4"}).Probe(context.Background()); err == nil {
			t.Error("unreadable hw_params accepted")
		}
	}
	// Default root is /proc/asound; a card that cannot exist is unknown.
	if _, err := (ALSAProber{Card: "no-such-card-xyz"}).Probe(context.Background()); !errors.Is(err, ErrOutputUnknown) {
		t.Errorf("default root err = %v", err)
	}
}

const pwDump = `[
 {"id":0,"type":"PipeWire:Interface:Metadata","props":{"metadata.name":"default"},
  "metadata":[{"subject":0,"key":"default.audio.sink","type":"Spa:String:JSON","value":{"name":"alsa_output.usb-Schiit_Audio_Vestri-00.analog-stereo"}}]},
 {"id":5,"type":"PipeWire:Interface:Node","info":{"props":{"node.name":"alsa_output.usb-Schiit_Audio_Vestri-00.analog-stereo","media.class":"Audio/Sink","alsa.card":4,"alsa.device":0}}},
 {"id":6,"type":"PipeWire:Interface:Node","info":{"props":{"node.name":"audio_effect.j314-convolver","media.class":"Audio/Sink"}}},
 {"id":7,"type":"PipeWire:Interface:Node","info":{"props":{"node.name":"strcard","alsa.card":"3","alsa.device":"1"}}},
 {"id":8,"type":"PipeWire:Interface:Node","info":{"props":{"node.name":"evil","alsa.card":"../../etc","alsa.device":"0"}}}
]`

func TestSinkCard(t *testing.T) {
	tests := []struct {
		name string
		dump string
		sink string
		card string
		dev  int
		err  bool
	}{
		{"default sink", pwDump, "", "4", 0, false},
		{"named sink", pwDump, "alsa_output.usb-Schiit_Audio_Vestri-00.analog-stereo", "4", 0, false},
		{"string properties", pwDump, "strcard", "3", 1, false},
		{"filter sink has no card", pwDump, "audio_effect.j314-convolver", "", 0, true},
		{"unknown sink", pwDump, "nope", "", 0, true},
		{"card is not a plain name", pwDump, "evil", "", 0, true},
		{"no default", `[{"type":"PipeWire:Interface:Metadata","props":{"metadata.name":"other"}}]`, "", "", 0, true},
		{"not JSON", `{`, "", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, dev, err := sinkCard([]byte(tt.dump), tt.sink)
			if card != tt.card || dev != tt.dev || (err != nil) != tt.err {
				t.Errorf("= %q %d %v", card, dev, err)
			}
			if err != nil && !errors.Is(err, ErrOutputUnknown) {
				t.Errorf("err %v is not ErrOutputUnknown", err)
			}
		})
	}
}

func TestPipeWireProber(t *testing.T) {
	root := fakeProc(t, hw192, "")
	calls := 0
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	p := &PipeWireProber{
		Root: root,
		Dump: func(context.Context) ([]byte, error) { calls++; return []byte(pwDump), nil },
		Now:  func() time.Time { return now },
	}
	for range 3 {
		h, err := p.Probe(context.Background())
		if err != nil || h.Rate != 192000 {
			t.Fatalf("Probe = %+v %v", h, err)
		}
	}
	if calls != 1 {
		t.Errorf("pw-dump calls = %d, want 1 (cached)", calls)
	}
	now = now.Add(11 * time.Second)
	p.Probe(context.Background())
	if calls != 2 {
		t.Errorf("pw-dump calls = %d, want 2 after the cache expired", calls)
	}

	failing := &PipeWireProber{Dump: func(context.Context) ([]byte, error) { calls++; return nil, errors.New("not installed") }}
	before := calls
	for range 2 {
		if _, err := failing.Probe(context.Background()); !errors.Is(err, ErrOutputUnknown) {
			t.Errorf("err = %v", err)
		}
	}
	if calls-before != 1 {
		t.Error("failure not cached")
	}
	filter := &PipeWireProber{Sink: "audio_effect.j314-convolver", Dump: func(context.Context) ([]byte, error) { return []byte(pwDump), nil }}
	if _, err := filter.Probe(context.Background()); !errors.Is(err, ErrOutputUnknown) {
		t.Errorf("filter sink err = %v", err)
	}
}

func TestNewProber(t *testing.T) {
	if p, ok := NewProber(config.AudioExclusive, "alsa/hw:CARD=Vestri,DEV=1").(ALSAProber); !ok || p.Card != "Vestri" || p.Device != 1 {
		t.Errorf("exclusive prober = %#v", p)
	}
	for device, sink := range map[string]string{"auto": "", "pipewire": "", "pipewire/node.a": "node.a"} {
		p, ok := NewProber(config.AudioShared, device).(*PipeWireProber)
		if !ok || p.Sink != sink {
			t.Errorf("shared %s = %#v", device, p)
		}
	}
}

func TestSourceOf(t *testing.T) {
	hi := SourceOf(tidal.Manifest{Formats: []tidal.Format{tidal.FormatFLACHires}, SampleRate: 192000, BitDepth: 24})
	if hi != (Source{Codec: "FLAC", Lossless: true, SampleRate: 192000, BitDepth: 24}) || hi.String() != "FLAC 24-bit/192 kHz" {
		t.Errorf("hi-res = %+v %q", hi, hi)
	}
	aac := SourceOf(tidal.Manifest{Formats: []tidal.Format{tidal.FormatAACLC}, SampleRate: 44100})
	if aac.Lossless || aac.String() != "AAC 44.1 kHz" {
		t.Errorf("aac = %+v %q", aac, aac)
	}
	if got := (HWParams{Format: "S32_LE", Rate: 96000}).String(); got != "S32_LE 96 kHz" {
		t.Errorf("HWParams.String = %q", got)
	}
}

func TestEvaluate(t *testing.T) {
	flac := Source{Codec: "FLAC", Lossless: true, SampleRate: 192000, BitDepth: 24}
	s32 := AudioParams{SampleRate: 192000, Channels: 2, Format: "s32"}
	hw := HWParams{Format: "S32_LE", Rate: 192000, Channels: 2}
	base := func() Inputs {
		return Inputs{Mode: config.AudioExclusive, Source: flac, Decoded: s32, Output: s32, HW: hw, Volume: 100}
	}
	tests := []struct {
		name   string
		mutate func(*Inputs)
		want   State
		reason string
	}{
		{"exclusive at source format", func(*Inputs) {}, BitPerfect, "exclusive"},
		{"24-bit packed hardware", func(in *Inputs) { in.HW.Format = "S24_3LE" }, BitPerfect, "exclusive"},
		{"unknown depth with 32-bit output", func(in *Inputs) { in.Source.BitDepth = 0 }, BitPerfect, "exclusive"},
		{"16-bit source on 16-bit output", func(in *Inputs) {
			in.Source.BitDepth, in.Source.SampleRate, in.HW = 16, 44100, HWParams{Format: "S16_LE", Rate: 44100}
			in.Output = AudioParams{SampleRate: 44100, Format: "s16"}
		}, BitPerfect, "exclusive"},
		{"hardware rate differs", func(in *Inputs) { in.HW.Rate = 48000 }, Altered, "resampled to 48 kHz (source 192 kHz)"},
		{"player resampled", func(in *Inputs) { in.Output.SampleRate = 96000 }, Altered, "by the player"},
		{"hardware unknown", func(in *Inputs) { in.HWErr = ErrOutputUnknown }, Altered, "unknown"},
		{"hardware empty", func(in *Inputs) { in.HW = HWParams{} }, Altered, "unknown"},
		{"filters", func(in *Inputs) { in.Filters = 1 }, Altered, "filters"},
		{"volume below 100", func(in *Inputs) { in.Volume = 80 }, Altered, "software volume 80%"},
		{"float output", func(in *Inputs) { in.Output.Format = "float" }, Altered, "non-integer"},
		{"float hardware", func(in *Inputs) { in.HW.Format = "FLOAT_LE" }, Altered, "non-integer"},
		{"depth reduced", func(in *Inputs) { in.Output.Format = "s16" }, Altered, "reduced to 16"},
		{"unknown depth on 16-bit output", func(in *Inputs) { in.Source.BitDepth = 0; in.HW.Format = "S16_LE" }, Altered, "reduced to 16"},
		{"shared at source rate", func(in *Inputs) { in.Mode = config.AudioShared }, RateMatched, "shared"},
		{"shared resampled by PipeWire", func(in *Inputs) { in.Mode = config.AudioShared; in.HW.Rate = 48000 }, Altered, "resampled"},
		{"shared with volume", func(in *Inputs) { in.Mode = config.AudioShared; in.Volume = 50 }, Altered, "volume"},
		{"lossy source", func(in *Inputs) { in.Source.Lossless = false }, RateMatched, "lossy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base()
			tt.mutate(&in)
			got := Evaluate(in)
			if got.State != tt.want || !strings.Contains(got.Reason, tt.reason) {
				t.Errorf("Evaluate = %s %q, want %s containing %q", got.State, got.Reason, tt.want, tt.reason)
			}
		})
	}
	for s, want := range map[State]string{BitPerfect: "bit-perfect", RateMatched: "rate matched", Altered: "altered"} {
		if s.String() != want {
			t.Errorf("%d.String() = %q", s, s.String())
		}
	}
}

func TestSampleWidths(t *testing.T) {
	for f, want := range map[string]int{"S32_LE": 32, "U32_BE": 32, "S24_3LE": 24, "S24_LE": 24, "S16_LE": 16, "FLOAT_LE": 0, "U8": 0} {
		if got := alsaBits(f); got != want {
			t.Errorf("alsaBits(%s) = %d", f, got)
		}
	}
	for f, want := range map[string]int{"s32": 32, "s32p": 32, "s24": 24, "s16": 16, "float": 0, "floatp": 0, "": 0} {
		if got := mpvBits(f); got != want {
			t.Errorf("mpvBits(%s) = %d", f, got)
		}
	}
}

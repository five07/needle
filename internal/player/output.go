package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/tidal"
)

// ErrOutputUnknown means the hardware output format could not be determined
// (for example a PipeWire filter sink with no ALSA card behind it).
var ErrOutputUnknown = errors.New("player: output format unknown")

// HWParams is the format an ALSA device is actually running at, from
// /proc/asound/cardN/pcmDp/subS/hw_params.
type HWParams struct {
	Card     string
	Format   string // ALSA name, for example S32_LE
	Rate     int
	Channels int
}

// Bits returns the integer sample width of the format, or 0 for float and
// unknown formats.
func (h HWParams) Bits() int { return alsaBits(h.Format) }

// String describes the format for display.
func (h HWParams) String() string {
	return fmt.Sprintf("%s %s", h.Format, khz(h.Rate))
}

// parseHWParams reads a hw_params file. A device that is not open reads
// "closed" and returns ok=false.
func parseHWParams(r io.Reader) (HWParams, bool, error) {
	var h HWParams
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "closed" {
			return HWParams{}, false, nil
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "format":
			h.Format = v
		case "channels":
			h.Channels, _ = strconv.Atoi(v)
		case "rate":
			h.Rate, _ = strconv.Atoi(strings.Fields(v + " ")[0])
		}
	}
	if err := sc.Err(); err != nil {
		return HWParams{}, false, fmt.Errorf("player: read hw_params: %w", err)
	}
	if h.Format == "" || h.Rate == 0 {
		return HWParams{}, false, errors.New("player: hw_params has no format or rate")
	}
	return h, true, nil
}

// Prober reports the hardware format of the configured output.
type Prober interface {
	Probe(ctx context.Context) (HWParams, error)
}

// ALSAProber reads one card's playback hw_params.
type ALSAProber struct {
	// Root is the procfs ALSA directory. Default /proc/asound.
	Root string
	// Card is a card number ("4") or id ("Vestri").
	Card   string
	Device int
}

// Probe implements Prober. An idle device returns ErrOutputUnknown.
func (p ALSAProber) Probe(context.Context) (HWParams, error) {
	root := p.Root
	if root == "" {
		root = "/proc/asound"
	}
	card := p.Card
	if _, err := strconv.Atoi(card); err == nil {
		card = "card" + card
	}
	pcm := filepath.Join(root, card, fmt.Sprintf("pcm%dp", p.Device))
	subs, _ := filepath.Glob(filepath.Join(pcm, "sub*", "hw_params"))
	if len(subs) == 0 {
		return HWParams{}, fmt.Errorf("%w: no playback device %s", ErrOutputUnknown, filepath.Base(pcm))
	}
	for _, path := range subs {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		h, open, err := parseHWParams(f)
		f.Close()
		if err == nil && open {
			h.Card = p.Card
			return h, nil
		}
	}
	return HWParams{}, fmt.Errorf("%w: device is not open", ErrOutputUnknown)
}

// PipeWireProber finds the ALSA card behind a PipeWire sink and reads its
// hw_params. The sink lookup is cached for CacheFor.
type PipeWireProber struct {
	// Sink is a PipeWire node name, or "" for the default sink.
	Sink string
	// Dump returns `pw-dump` output. Default runs pw-dump.
	Dump func(ctx context.Context) ([]byte, error)
	// Root is passed to ALSAProber.
	Root     string
	CacheFor time.Duration
	Now      func() time.Time

	mu      sync.Mutex
	card    *ALSAProber
	err     error
	checked time.Time
}

// Probe implements Prober.
func (p *PipeWireProber) Probe(ctx context.Context) (HWParams, error) {
	alsa, err := p.resolve(ctx)
	if err != nil {
		return HWParams{}, err
	}
	return alsa.Probe(ctx)
}

func (p *PipeWireProber) resolve(ctx context.Context) (ALSAProber, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	cacheFor := p.CacheFor
	if cacheFor == 0 {
		cacheFor = 10 * time.Second
	}
	if !p.checked.IsZero() && now().Sub(p.checked) < cacheFor {
		if p.err != nil {
			return ALSAProber{}, p.err
		}
		return *p.card, nil
	}
	dump := p.Dump
	if dump == nil {
		dump = func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "pw-dump").Output()
		}
	}
	p.checked = now()
	p.card, p.err = nil, nil
	out, err := dump(ctx)
	if err != nil {
		p.err = fmt.Errorf("%w: pw-dump: %w", ErrOutputUnknown, err)
		return ALSAProber{}, p.err
	}
	card, dev, err := sinkCard(out, p.Sink)
	if err != nil {
		p.err = err
		return ALSAProber{}, err
	}
	p.card = &ALSAProber{Root: p.Root, Card: card, Device: dev}
	return *p.card, nil
}

// sinkCard finds the ALSA card and device of a PipeWire sink in pw-dump
// output. An empty sink means the default sink.
func sinkCard(dump []byte, sink string) (card string, dev int, err error) {
	var objs []struct {
		Type     string `json:"type"`
		Metadata []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"metadata"`
		Props map[string]json.RawMessage `json:"props"`
		Info  struct {
			Props map[string]json.RawMessage `json:"props"`
		} `json:"info"`
	}
	if err := json.Unmarshal(dump, &objs); err != nil {
		return "", 0, fmt.Errorf("%w: pw-dump output: %w", ErrOutputUnknown, err)
	}
	if sink == "" {
		for _, o := range objs {
			if !strings.HasSuffix(o.Type, ":Metadata") || propString(o.Props["metadata.name"]) != "default" {
				continue
			}
			for _, md := range o.Metadata {
				if md.Key == "default.audio.sink" {
					var v struct {
						Name string `json:"name"`
					}
					if json.Unmarshal(md.Value, &v) == nil {
						sink = v.Name
					}
				}
			}
		}
		if sink == "" {
			return "", 0, fmt.Errorf("%w: no default PipeWire sink", ErrOutputUnknown)
		}
	}
	for _, o := range objs {
		props := o.Info.Props
		if propString(props["node.name"]) != sink {
			continue
		}
		card = propString(props["alsa.card"])
		if card == "" {
			return "", 0, fmt.Errorf("%w: sink %q is not an ALSA device", ErrOutputUnknown, sink)
		}
		// The card becomes a /proc/asound path component.
		if !cardName.MatchString(card) {
			return "", 0, fmt.Errorf("%w: sink %q has an invalid ALSA card", ErrOutputUnknown, sink)
		}
		dev, _ = strconv.Atoi(propString(props["alsa.device"]))
		return card, dev, nil
	}
	return "", 0, fmt.Errorf("%w: sink %q not found", ErrOutputUnknown, sink)
}

var cardName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// propString reads a JSON string or number property as a string.
func propString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// NewProber returns the prober for an audio configuration.
func NewProber(mode config.AudioMode, device string) Prober {
	if mode == config.AudioExclusive {
		card, dev, _ := config.ParseALSAHW(device)
		return ALSAProber{Card: card, Device: dev}
	}
	// "auto" and "pipewire" mean the default sink.
	sink, _ := strings.CutPrefix(device, "pipewire/")
	if sink == config.DefaultAudioDevice || sink == "pipewire" {
		sink = ""
	}
	return &PipeWireProber{Sink: sink}
}

// State is the bit-perfect indicator state (PLAN.md decision 16).
type State int

// Indicator states.
const (
	// Altered: the output differs from the source (resampled, volume,
	// filters, reduced bit depth) or cannot be verified.
	Altered State = iota
	// RateMatched: shared mode at the source rate, or a lossy source.
	RateMatched
	// BitPerfect: exclusive mode, source samples reach the device unchanged.
	BitPerfect
)

// String returns a short label.
func (s State) String() string {
	switch s {
	case BitPerfect:
		return "bit-perfect"
	case RateMatched:
		return "rate matched"
	}
	return "altered"
}

// Source is the format of the track as delivered by TIDAL.
type Source struct {
	Codec      string // "FLAC" or "AAC"
	Lossless   bool
	SampleRate int
	BitDepth   int // 0 when unknown
}

// SourceOf describes a manifest's audio.
func SourceOf(m tidal.Manifest) Source {
	s := Source{Codec: "AAC", SampleRate: m.SampleRate, BitDepth: m.BitDepth}
	for _, f := range m.Formats {
		if f == tidal.FormatFLAC || f == tidal.FormatFLACHires {
			s.Codec, s.Lossless = "FLAC", true
		}
	}
	return s
}

// String describes the source for display, for example "FLAC 24-bit/192 kHz".
func (s Source) String() string {
	if s.BitDepth > 0 {
		return fmt.Sprintf("%s %d-bit/%s", s.Codec, s.BitDepth, khz(s.SampleRate))
	}
	return fmt.Sprintf("%s %s", s.Codec, khz(s.SampleRate))
}

// Assessment is the indicator state with its reason.
type Assessment struct {
	State  State
	Reason string
}

// Inputs to Evaluate.
type Inputs struct {
	Mode    config.AudioMode
	Source  Source
	Decoded AudioParams // what mpv decoded
	Output  AudioParams // what mpv sent to the audio output
	HW      HWParams
	HWErr   error // non-nil when the hardware format is unknown
	Volume  float64
	Filters int
}

// Evaluate decides the indicator state. It fails closed: anything it cannot
// verify is Altered.
func Evaluate(in Inputs) Assessment {
	src := in.Source
	switch {
	case in.HWErr != nil || in.HW.Rate == 0:
		return Assessment{Altered, "output format unknown"}
	case in.HW.Rate != src.SampleRate:
		return Assessment{Altered, fmt.Sprintf("resampled to %s (source %s)", khz(in.HW.Rate), khz(src.SampleRate))}
	case in.Output.SampleRate != 0 && in.Output.SampleRate != src.SampleRate:
		return Assessment{Altered, fmt.Sprintf("resampled to %s by the player", khz(in.Output.SampleRate))}
	case in.Filters > 0:
		return Assessment{Altered, "audio filters active"}
	case in.Volume < 100:
		return Assessment{Altered, fmt.Sprintf("software volume %.0f%%", in.Volume)}
	case !src.Lossless:
		return Assessment{RateMatched, "lossy source"}
	case in.Mode != config.AudioExclusive:
		return Assessment{RateMatched, "shared output at the source rate"}
	}
	out := min(mpvBits(in.Output.Format), in.HW.Bits())
	need := src.BitDepth
	if need == 0 {
		need = 24 // unknown depth: require room for hi-res
	}
	switch {
	case out == 0:
		return Assessment{Altered, "non-integer output format"}
	case out < need:
		return Assessment{Altered, fmt.Sprintf("bit depth reduced to %d", out)}
	}
	return Assessment{BitPerfect, "exclusive output at the source format"}
}

// alsaBits returns the integer sample width of an ALSA format name.
func alsaBits(format string) int {
	switch {
	case strings.HasPrefix(format, "S32_"), strings.HasPrefix(format, "U32_"):
		return 32
	case strings.HasPrefix(format, "S24_"), strings.HasPrefix(format, "U24_"):
		return 24
	case strings.HasPrefix(format, "S16_"), strings.HasPrefix(format, "U16_"):
		return 16
	}
	return 0
}

// mpvBits returns the integer sample width of an mpv format name.
func mpvBits(format string) int {
	switch strings.TrimSuffix(format, "p") {
	case "s32":
		return 32
	case "s24":
		return 24
	case "s16":
		return 16
	}
	return 0
}

func khz(rate int) string {
	if rate%1000 == 0 {
		return fmt.Sprintf("%d kHz", rate/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(rate)/1000)
}

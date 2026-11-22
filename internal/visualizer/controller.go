package visualizer

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

// FrameInterval is the redraw rate while bars move: 20 per second
// (decision 72).
const FrameInterval = 50 * time.Millisecond

// restartDrift is the sync limit: a jump in the reported position beyond
// it (a seek) restarts the decode there. A decode that is merely slow is
// not restarted; it catches up by skipping samples.
const restartDrift = 2 * time.Second

// Clock is what the controller needs to know about playback.
type Clock struct {
	// Key identifies the playing track (the queue handle); 0 is none.
	Key uint64
	// Position is the last reported position in the track.
	Position time.Duration
	// Playing is false when paused, loading, or stopped.
	Playing bool
	// TrackID is the TIDAL track ID, for logs.
	TrackID string
}

// Source tells the controller what is playing.
type Source interface {
	// Clock returns the current playback state; it must be cheap.
	Clock() Clock
	// Manifest returns the DASH manifest of the track with key.
	Manifest(ctx context.Context, key uint64) ([]byte, error)
}

// Frame is one set of bar levels, 0 to 1, lowest frequency first.
type Frame [Bands]float64

// decoder is a running analysis decode (*mpvDecoder; fakes in tests).
type decoder interface {
	Take(n int) []int16
	Close() error
}

// Controller runs the visualizer: while it is on and a track plays, it
// keeps an analysis decode in step with playback and sends a Frame every
// FrameInterval. Paused, stopped, or off, the bars fall and the frames
// stop. Failures only empty the bars; playback is never affected.
type Controller struct {
	src    Source
	frames chan Frame // capacity 1, newest wins
	logger *slog.Logger
	on     atomic.Bool
	wake   chan struct{}
	now    func() time.Time
	start  func(ctx context.Context, mpd []byte, pos time.Duration) (decoder, error)

	// Run goroutine only.
	an       *Analyzer
	dec      decoder
	key      uint64        // the track dec decodes
	at       time.Duration // dec's position: where the next sample belongs
	anchor   Clock         // the last position reported, and when
	anchorAt time.Time
	last     time.Time // the previous frame
	failed   uint64    // a key whose decode could not start: not retried
}

// Config configures a Controller.
type Config struct {
	Source Source
	// Binary is the mpv executable. Dir is a private directory for the
	// manifest copies.
	Binary, Dir string
	Logger      *slog.Logger
}

// New returns a Controller; call Run.
func New(cfg Config) *Controller {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Binary == "" {
		cfg.Binary = "mpv"
	}
	c := &Controller{src: cfg.Source, frames: make(chan Frame, 1), logger: cfg.Logger, wake: make(chan struct{}, 1),
		now: time.Now, an: NewAnalyzer()}
	var seq atomic.Uint64
	c.start = func(ctx context.Context, mpd []byte, pos time.Duration) (decoder, error) {
		if cfg.Dir == "" {
			return nil, errors.New("visualizer: no private directory for the manifest copy")
		}
		return startDecoder(ctx, cfg.Binary, cfg.Dir, seq.Add(1), mpd, pos, cfg.Logger)
	}
	return c
}

// Frames delivers bar levels; only the newest is kept.
func (c *Controller) Frames() <-chan Frame { return c.frames }

// On reports whether the visualizer is on.
func (c *Controller) On() bool { return c.on.Load() }

// SetOn turns the visualizer on or off.
func (c *Controller) SetOn(on bool) {
	c.on.Store(on)
	c.Wake()
}

// Wake tells the controller playback changed (call it on every player
// status change); it never blocks.
func (c *Controller) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run drives the visualizer until ctx ends.
func (c *Controller) Run(ctx context.Context) {
	defer c.stopDecoder()
	tick := time.NewTicker(FrameInterval)
	defer tick.Stop()
	for {
		if !c.step(ctx) {
			// Nothing moves: sleep until playback or the switch changes.
			select {
			case <-ctx.Done():
				return
			case <-c.wake:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-c.wake:
		}
	}
}

// step makes one frame and reports whether more frames are needed.
func (c *Controller) step(ctx context.Context) bool {
	now := c.now()
	dt := time.Duration(0)
	if !c.last.IsZero() {
		dt = now.Sub(c.last) // a long gap only empties the bars
	}
	c.last = now
	clk := c.src.Clock()
	jumped := false
	if clk != c.anchor {
		if clk.Key == c.anchor.Key {
			predicted := c.anchor.Position
			if c.anchor.Playing {
				predicted += now.Sub(c.anchorAt)
			}
			jumped = absDur(clk.Position-predicted) > restartDrift
		}
		c.anchor, c.anchorAt = clk, now
	}
	if !c.On() || !clk.Playing || clk.Key == 0 {
		if !c.On() || clk.Key != c.key || jumped {
			c.stopDecoder() // a paused decode waits, unless it seeked; anything else ends
		}
		levels, empty := c.an.Decay(dt)
		c.send(levels)
		return !empty
	}
	// Where playback is now: the last report plus the time since, but no
	// more than restartDrift past it if reports stop.
	want := c.anchor.Position + min(now.Sub(c.anchorAt), restartDrift)
	if clk.Key != c.key || jumped {
		c.restart(ctx, clk, want)
	}
	if c.dec != nil {
		c.feed(want)
	}
	c.send(c.an.Frame(dt))
	return true
}

// feed moves the decode up to want: taking every sample up to it, which
// skips what was missed when it is behind; ahead, it takes nothing and
// waits for playback.
func (c *Controller) feed(want time.Duration) {
	n := int((want - c.at).Seconds() * SampleRate)
	got := c.dec.Take(n)
	c.an.Push(got)
	c.at += time.Duration(len(got)) * time.Second / SampleRate
}

func (c *Controller) restart(ctx context.Context, clk Clock, pos time.Duration) {
	key := clk.Key
	c.stopDecoder()
	c.an.Reset()
	c.key, c.at = key, pos
	if key == c.failed {
		return
	}
	mpd, err := c.src.Manifest(ctx, key)
	if err == nil {
		c.dec, err = c.start(ctx, mpd, pos)
	}
	if err != nil {
		c.failed = key
		c.logger.Warn("visualizer: analysis decode not started", "track_id", clk.TrackID, "err", err)
	}
}

func (c *Controller) stopDecoder() {
	if c.dec == nil {
		return
	}
	if err := c.dec.Close(); err != nil {
		c.logger.Debug("visualizer: decoder cleanup", "err", err)
	}
	c.dec, c.key = nil, 0
}

func (c *Controller) send(levels [Bands]float64) {
	f := Frame(levels)
	for {
		select {
		case c.frames <- f:
			return
		default:
		}
		select { // drop the stale frame and try again
		case <-c.frames:
		default:
		}
	}
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

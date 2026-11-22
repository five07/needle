package player

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/stream"
	"github.com/five07/needle/internal/tidal"
)

// Track is a queued track.
type Track struct {
	ID       string
	Title    string
	Artists  string
	Album    string
	Duration time.Duration
}

// Resolver turns track IDs into manifests. *stream.Resolver implements it.
type Resolver interface {
	Resolve(ctx context.Context, trackID string) (tidal.Manifest, error)
	Invalidate(trackID string)
}

// PlayState is the transport state.
type PlayState int

// Transport states.
const (
	Stopped PlayState = iota
	Loading
	Playing
	Paused
)

// Status is a snapshot of the player for the UI.
type Status struct {
	State    PlayState
	Track    *Track
	Position time.Duration
	Duration time.Duration
	// Preview is set when TIDAL only provided a preview; PreviewMessage
	// explains why.
	Preview        bool
	PreviewMessage string
	Source         *Source
	Output         *HWParams
	Indicator      Assessment
	Volume         float64
	// VolumeEnabled is false in exclusive mode (volume is on the DAC/amp).
	VolumeEnabled bool
	// Message is a user-facing error or notice, empty when all is well.
	Message string
	// Notice is the latest passing notice, such as a skipped track.
	// NoticeID changes with every new one, so the same text twice is
	// still shown twice.
	Notice   string
	NoticeID uint64
}

// Config configures a Player.
type Config struct {
	Mode      config.AudioMode
	NewEngine func(ctx context.Context) (Engine, error)
	Resolver  Resolver
	Queue     *queue.Queue[Track]
	Prober    Prober
	Logger    *slog.Logger
	// PreloadAhead is how long before the end of a track the next one is
	// fetched and preloaded. Default 45 s.
	PreloadAhead time.Duration
}

type loadResult struct {
	gen      uint64
	handle   queue.Handle
	track    Track
	start    time.Duration
	manifest tidal.Manifest
	err      error
	preload  bool
}

// Player connects the queue, the manifest resolver, and the engine. Its
// methods are safe for concurrent use; all state changes happen on the Run
// goroutine.
type Player struct {
	cfg     Config
	logger  *slog.Logger
	cmds    chan func(context.Context)
	results chan loadResult
	updates chan struct{}
	done    chan struct{} // closed when Run returns

	status statusBox

	// Owned by the Run goroutine.
	engine     Engine
	events     <-chan Event
	gen        uint64
	current    queue.Handle
	manifests  map[queue.Handle]tidal.Manifest
	preloaded  queue.Handle
	preloading bool
	resumed    map[queue.Handle]bool
	skipped    int // unavailable tracks skipped in a row (decision 58)
	format     Event
	st         Status

	// loaded, when set (tests only), is called after each resolve result is
	// handled on the Run goroutine.
	loaded func()
}

// New returns a Player. Call Run to start it.
func New(cfg Config) *Player {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.PreloadAhead == 0 {
		cfg.PreloadAhead = 45 * time.Second
	}
	p := &Player{
		cfg:       cfg,
		logger:    cfg.Logger,
		cmds:      make(chan func(context.Context)),
		results:   make(chan loadResult, 4),
		updates:   make(chan struct{}, 1),
		done:      make(chan struct{}),
		manifests: map[queue.Handle]tidal.Manifest{},
		resumed:   map[queue.Handle]bool{},
	}
	p.st = Status{Volume: 100, VolumeEnabled: cfg.Mode != config.AudioExclusive}
	p.status.set(p.st)
	return p
}

// Status returns the current status.
func (p *Player) Status() Status { return p.status.get() }

// Updates is signaled (coalesced) whenever the status changes.
func (p *Player) Updates() <-chan struct{} { return p.updates }

// Run processes commands and engine events until ctx ends, then closes the
// engine.
func (p *Player) Run(ctx context.Context) error {
	defer func() {
		close(p.done)
		if p.engine != nil {
			p.engine.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cmd := <-p.cmds:
			cmd(ctx)
		case r := <-p.results:
			p.onLoaded(ctx, r)
			if p.loaded != nil {
				p.loaded()
			}
		case ev, ok := <-p.events:
			if !ok {
				p.events = nil
				continue
			}
			p.onEvent(ctx, ev)
		}
		p.publish()
	}
}

// do runs f on the Run goroutine and returns its error.
func (p *Player) do(ctx context.Context, f func(context.Context) error) error {
	errc := make(chan error, 1)
	select {
	// Publish before replying, so Status reflects the command once it returns.
	case p.cmds <- func(rctx context.Context) { err := f(rctx); p.publish(); errc <- err }:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Play starts the queue's current track.
func (p *Player) Play(ctx context.Context) error {
	return p.do(ctx, func(context.Context) error {
		s := p.cfg.Queue.Snapshot()
		if s.Current == nil {
			return nil
		}
		p.skipped = 0
		p.load(s.Current.Handle, s.Current.Item, 0)
		return nil
	})
}

// Next skips to the next track.
func (p *Player) Next(ctx context.Context) error {
	return p.do(ctx, func(rctx context.Context) error {
		p.skipped = 0
		if _, ok := p.cfg.Queue.Next(); !ok {
			return p.stop(rctx)
		}
		p.loadCurrent()
		return nil
	})
}

// Previous goes back one track, or restarts the first one.
func (p *Player) Previous(ctx context.Context) error {
	return p.do(ctx, func(context.Context) error {
		p.skipped = 0
		if _, ok := p.cfg.Queue.Previous(); ok {
			p.loadCurrent()
		}
		return nil
	})
}

// TogglePause pauses or resumes.
func (p *Player) TogglePause(ctx context.Context) error {
	return p.do(ctx, func(rctx context.Context) error {
		return p.setPaused(rctx, p.st.State == Playing)
	})
}

// SetPaused pauses (true) or resumes (false). It does nothing when the
// player is already in that state, or not playing or paused (MPRIS Play
// and Pause are not toggles).
func (p *Player) SetPaused(ctx context.Context, paused bool) error {
	return p.do(ctx, func(rctx context.Context) error {
		return p.setPaused(rctx, paused)
	})
}

func (p *Player) setPaused(ctx context.Context, pause bool) error {
	if p.engine == nil || (p.st.State != Playing && p.st.State != Paused) {
		return nil
	}
	if pause == (p.st.State == Paused) {
		return nil
	}
	if err := p.engine.SetPaused(ctx, pause); err != nil {
		return err
	}
	p.st.State = map[bool]PlayState{true: Paused, false: Playing}[pause]
	return nil
}

// Stop stops playback.
func (p *Player) Stop(ctx context.Context) error {
	return p.do(ctx, p.stop)
}

// SetVolume sets software volume (shared mode only).
func (p *Player) SetVolume(ctx context.Context, percent float64) error {
	return p.do(ctx, func(rctx context.Context) error {
		if p.cfg.Mode == config.AudioExclusive {
			return ErrVolumeDisabled
		}
		if p.engine == nil {
			return nil
		}
		return p.engine.SetVolume(rctx, percent)
	})
}

// QueueChanged must be called after the queue is edited, so a preloaded
// track that is no longer next is withdrawn.
func (p *Player) QueueChanged(ctx context.Context) error {
	return p.do(ctx, func(rctx context.Context) error {
		if p.preloaded == 0 || p.engine == nil {
			return nil
		}
		if up := p.cfg.Queue.Snapshot().Upcoming; len(up) > 0 && up[0].Handle == p.preloaded {
			return nil
		}
		p.preloaded = 0
		return p.engine.ClearPreload(rctx)
	})
}

func (p *Player) stop(ctx context.Context) error {
	p.gen++
	p.preloaded, p.preloading = 0, false
	p.st.State, p.st.Position = Stopped, 0
	if p.engine == nil {
		return nil
	}
	return p.engine.Stop(ctx)
}

func (p *Player) loadCurrent() {
	if s := p.cfg.Queue.Snapshot(); s.Current != nil {
		p.load(s.Current.Handle, s.Current.Item, 0)
	}
}

// load resolves a track in the background and plays it when ready. A newer
// load supersedes an older one.
func (p *Player) load(h queue.Handle, t Track, start time.Duration) {
	p.gen++
	gen := p.gen
	p.current = h
	p.preloaded, p.preloading = 0, false
	p.st.State, p.st.Track, p.st.Position, p.st.Message = Loading, &t, start, ""
	p.st.Duration = t.Duration
	go p.resolve(loadResult{gen: gen, handle: h, track: t, start: start})
}

func (p *Player) resolve(r loadResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r.manifest, r.err = p.cfg.Resolver.Resolve(ctx, r.track.ID)
	select {
	case p.results <- r:
	case <-p.done:
	}
}

func (p *Player) onLoaded(ctx context.Context, r loadResult) {
	if r.gen != p.gen {
		return // superseded
	}
	if r.preload {
		p.preloading = false
		up := p.cfg.Queue.Snapshot().Upcoming
		if r.err != nil || p.engine == nil || len(up) == 0 || up[0].Handle != r.handle {
			if r.err != nil {
				p.logger.Warn("preload failed", "track_id", r.track.ID, "err", r.err)
			}
			return
		}
		if err := p.engine.Preload(ctx, Item{Key: uint64(r.handle), Manifest: r.manifest}); err != nil {
			p.logger.Warn("preload failed", "track_id", r.track.ID, "err", err)
			return
		}
		p.manifests[r.handle] = r.manifest
		p.preloaded = r.handle
		p.logger.Info("preloaded", "track_id", r.track.ID)
		return
	}
	if r.err != nil {
		if unavailable(r.err) {
			p.skip(ctx, r)
			return
		}
		p.fail(r.track.ID, r.err)
		return
	}
	if err := p.ensureEngine(ctx); err != nil {
		p.fail(r.track.ID, err)
		return
	}
	p.manifests = map[queue.Handle]tidal.Manifest{r.handle: r.manifest}
	if err := p.engine.Play(ctx, Item{Key: uint64(r.handle), Manifest: r.manifest, Start: r.start}); err != nil {
		p.fail(r.track.ID, err)
		return
	}
	p.logger.Info("play", "track_id", r.track.ID, "start", r.start)
}

func (p *Player) ensureEngine(ctx context.Context) error {
	if p.engine != nil {
		return nil
	}
	e, err := p.cfg.NewEngine(ctx)
	if err != nil {
		return err
	}
	p.engine, p.events = e, e.Events()
	return nil
}

// unavailable reports whether err means the track itself cannot be played
// here: it is gone from TIDAL, or not available in the account's country.
func unavailable(err error) bool {
	return errors.Is(err, tidal.ErrNotFound) || errors.Is(err, tidal.ErrInvalidResourceID) ||
		errors.Is(err, tidal.ErrGeoRestricted)
}

// skip moves past a track that is not available, with a notice (decision
// 58). After a whole pass of such tracks in a row it stops, so repeat all
// cannot go round forever.
func (p *Player) skip(ctx context.Context, r loadResult) {
	p.logger.Warn("track unavailable, skipping", "track_id", r.track.ID, "err", r.err)
	p.skipped++
	if p.skipped == 1 {
		p.notice(`Skipped "` + r.track.Title + `": ` + stream.Message(r.err))
	} else {
		p.notice(fmt.Sprintf("Skipped %d tracks that are not available.", p.skipped))
	}
	if p.skipped >= p.cfg.Queue.Len() {
		p.skipped = 0
		p.halt(ctx)
		p.st.Message = "None of the tracks in the queue can be played."
		return
	}
	if _, ok := p.cfg.Queue.Next(); !ok {
		p.skipped = 0
		p.halt(ctx) // the end of the queue
		return
	}
	p.loadCurrent()
}

// halt stops playback from inside the Run loop, logging an engine error.
func (p *Player) halt(ctx context.Context) {
	if err := p.stop(ctx); err != nil {
		p.logger.Warn("stop failed", "err", err)
	}
}

func (p *Player) notice(text string) {
	p.st.Notice = text
	p.st.NoticeID++
}

// fail stops with a user-facing message.
func (p *Player) fail(trackID string, err error) {
	p.logger.Error("playback failed", "track_id", trackID, "err", err)
	p.st.State, p.st.Message = Stopped, Message(err)
}

func (p *Player) onEvent(ctx context.Context, ev Event) {
	h := queue.Handle(ev.Key)
	switch ev.Kind {
	case EventStarted:
		if h != p.current {
			return
		}
		m := p.manifests[h]
		src := SourceOf(m)
		p.st.State, p.st.Source, p.st.Message = Playing, &src, ""
		p.skipped = 0
		p.st.Preview, p.st.PreviewMessage = m.Preview(), ""
		if m.Preview() {
			p.st.PreviewMessage = stream.PreviewMessage(m.PreviewReason)
		}
		if p.preloaded == h {
			p.preloaded = 0
		}
		for k := range p.resumed {
			if k != h {
				delete(p.resumed, k)
			}
		}
		p.logger.Info("track started", "track_id", m.TrackID, "source", src.String(), "preview", m.Preview())
	case EventEnded:
		if h != p.current {
			return
		}
		p.onEnded(ctx, h, ev)
	case EventFormat:
		p.format = ev
		p.st.Volume = ev.Volume
		p.assess(ctx)
	case EventProgress:
		if h != p.current {
			return
		}
		p.st.Position = ev.Position
		if ev.Duration > 0 {
			p.st.Duration = ev.Duration
		}
		p.assess(ctx)
		p.maybePreload(ev)
	case EventFailed:
		p.engine, p.events = nil, nil
		p.preloaded, p.preloading = 0, false
		p.fail(p.trackID(), ev.Err)
	}
}

func (p *Player) onEnded(ctx context.Context, h queue.Handle, ev Event) {
	switch ev.Reason {
	case EndStopped:
		return
	case EndError:
		m := p.manifests[h]
		// A stream that breaks mid-track (usually expired signed URLs) is
		// re-fetched once and resumed where it stopped.
		if errors.Is(ev.Err, ErrStreamFailed) && ev.Position > 0 && !p.resumed[h] {
			p.resumed[h] = true
			p.cfg.Resolver.Invalidate(m.TrackID)
			if s := p.cfg.Queue.Snapshot(); s.Current != nil && s.Current.Handle == h {
				p.logger.Warn("stream failed, resuming", "track_id", m.TrackID, "position", ev.Position)
				p.load(h, s.Current.Item, ev.Position)
				return
			}
		}
		p.fail(m.TrackID, ev.Err)
		return
	}
	// EOF: advance the queue.
	if _, ok := p.cfg.Queue.Advance(); !ok {
		p.logger.Info("end of queue")
		p.st.State, p.st.Position = Stopped, 0
		return
	}
	s := p.cfg.Queue.Snapshot()
	next := s.Current.Handle
	if next == p.preloaded {
		// mpv is already playing it gaplessly.
		p.current = next
		p.st.Track, p.st.Position, p.st.Duration = &s.Current.Item, 0, s.Current.Item.Duration
		return
	}
	p.load(next, s.Current.Item, 0)
}

// maybePreload fetches and preloads the next track near the end of the
// current one. Repeat one replays the same entry, so nothing is preloaded.
func (p *Player) maybePreload(ev Event) {
	if p.preloaded != 0 || p.preloading || ev.Duration == 0 || ev.Duration-ev.Position > p.cfg.PreloadAhead {
		return
	}
	if p.cfg.Queue.Repeat() == queue.RepeatOne {
		return
	}
	up := p.cfg.Queue.Snapshot().Upcoming
	if len(up) == 0 {
		return
	}
	p.preloading = true
	go p.resolve(loadResult{gen: p.gen, handle: up[0].Handle, track: up[0].Item, preload: true})
}

func (p *Player) assess(ctx context.Context) {
	if p.st.Source == nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	hw, err := p.cfg.Prober.Probe(pctx)
	cancel()
	if err == nil {
		p.st.Output = &hw
	} else {
		p.st.Output = nil
	}
	p.st.Indicator = Evaluate(Inputs{
		Mode: p.cfg.Mode, Source: *p.st.Source, Decoded: p.format.Decoded, Output: p.format.Output,
		HW: hw, HWErr: err, Volume: p.format.Volume, Filters: p.format.Filters,
	})
}

func (p *Player) trackID() string {
	if p.st.Track != nil {
		return p.st.Track.ID
	}
	return ""
}

// publish copies the status for readers and signals Updates.
func (p *Player) publish() {
	p.status.set(p.st)
	select {
	case p.updates <- struct{}{}:
	default:
	}
}

// Message returns user-facing text for a playback error.
func Message(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrMPVNotFound):
		return "mpv is not installed. Install mpv to play music."
	case errors.Is(err, ErrDeviceBusy):
		return "The DAC is in use by another app. Stop it there, or set audio.mode to shared."
	case errors.Is(err, ErrOutputFailed):
		return "The audio output could not be opened. Check audio.device (run needle -list-devices)."
	case errors.Is(err, ErrStreamFailed):
		return "The stream stopped. Try playing the track again."
	case errors.Is(err, ErrEngineFailed), errors.Is(err, ErrIPCClosed):
		return "The audio player stopped unexpectedly. Press play to restart it."
	case errors.Is(err, ErrVolumeDisabled):
		return "In exclusive mode, set the volume on your DAC or amp."
	}
	return stream.Message(err)
}

package player

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tidal"
)

// fakeEngine records calls; tests push events into it.
type fakeEngine struct {
	mu       sync.Mutex
	calls    []string
	plays    []Item
	preloads []Item
	events   chan Event
	closed   bool
	fail     map[string]error // call name -> error to return
}

func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan Event, 32)} }

func (f *fakeEngine) record(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	return f.fail[strings.Fields(s)[0]]
}

func (f *fakeEngine) setFail(call string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = map[string]error{}
	}
	f.fail[call] = err
}

func (f *fakeEngine) Play(_ context.Context, it Item) error {
	f.mu.Lock()
	f.plays = append(f.plays, it)
	f.mu.Unlock()
	return f.record(fmt.Sprintf("play %d@%s", it.Key, it.Start))
}

func (f *fakeEngine) Preload(_ context.Context, it Item) error {
	f.mu.Lock()
	f.preloads = append(f.preloads, it)
	f.mu.Unlock()
	return f.record(fmt.Sprintf("preload %d", it.Key))
}

func (f *fakeEngine) ClearPreload(context.Context) error { return f.record("clear-preload") }
func (f *fakeEngine) SetPaused(_ context.Context, p bool) error {
	return f.record(fmt.Sprintf("pause %v", p))
}
func (f *fakeEngine) SetVolume(_ context.Context, v float64) error {
	return f.record(fmt.Sprintf("volume %.0f", v))
}
func (f *fakeEngine) Stop(context.Context) error { return f.record("stop") }
func (f *fakeEngine) Events() <-chan Event       { return f.events }
func (f *fakeEngine) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeEngine) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeEngine) has(call string) bool {
	for _, c := range f.callList() {
		if c == call {
			return true
		}
	}
	return false
}

func (f *fakeEngine) count(prefix string) int {
	n := 0
	for _, c := range f.callList() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// fakeResolver returns hi-res manifests unless an error is set for the ID.
type fakeResolver struct {
	mu          sync.Mutex
	errs        map[string]error
	gates       map[string]chan struct{}
	resolved    []string
	invalidated []string
	preview     bool
}

func (r *fakeResolver) Resolve(ctx context.Context, id string) (tidal.Manifest, error) {
	r.mu.Lock()
	gate := r.gates[id]
	r.mu.Unlock()
	if gate != nil {
		<-gate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved = append(r.resolved, id)
	if err := r.errs[id]; err != nil {
		return tidal.Manifest{}, err
	}
	m := tidal.Manifest{TrackID: id, Formats: []tidal.Format{tidal.FormatFLACHires}, Presentation: tidal.PresentationFull,
		SampleRate: 192000, BitDepth: 24, Duration: 30 * time.Second, MPD: []byte("<MPD/>")}
	if r.preview {
		m.Presentation, m.PreviewReason = tidal.PresentationPreview, tidal.PreviewRequiresHigherAccessTier
	}
	return m, nil
}

func (r *fakeResolver) Invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidated = append(r.invalidated, id)
}

type fakeProber struct {
	mu  sync.Mutex
	hw  HWParams
	err error
}

func (p *fakeProber) Probe(context.Context) (HWParams, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hw, p.err
}

type harness struct {
	t       *testing.T
	p       *Player
	q       *queue.Queue[Track]
	res     *fakeResolver
	prober  *fakeProber
	engines []*fakeEngine
	newErr  error
	mu      sync.Mutex
	cancel  context.CancelFunc
	runErr  chan error
	loads   atomic.Int64 // resolve results handled by Run
}

func newHarness(t *testing.T, mode config.AudioMode, tracks ...string) *harness {
	t.Helper()
	h := &harness{t: t, res: &fakeResolver{errs: map[string]error{}, gates: map[string]chan struct{}{}},
		prober: &fakeProber{hw: HWParams{Format: "S32_LE", Rate: 192000, Channels: 2}}, runErr: make(chan error, 1)}
	h.q = queue.New[Track](queue.WithRand(rand.New(rand.NewPCG(1, 2))))
	var items []Track
	for _, id := range tracks {
		items = append(items, Track{ID: id, Title: "Song " + id, Duration: 30 * time.Second})
	}
	if len(items) > 0 {
		h.q.Set(items, 0)
	}
	h.p = New(Config{
		Mode:     mode,
		Resolver: h.res,
		Queue:    h.q,
		Prober:   h.prober,
		NewEngine: func(context.Context) (Engine, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.newErr != nil {
				return nil, h.newErr
			}
			e := newFakeEngine()
			h.engines = append(h.engines, e)
			return e, nil
		},
	})
	h.p.loaded = func() { h.loads.Add(1) }
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.runErr <- h.p.Run(ctx) }()
	t.Cleanup(cancel)
	return h
}

func (h *harness) engine() *fakeEngine {
	h.t.Helper()
	var e *fakeEngine
	waitFor(h.t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.engines) > 0 {
			e = h.engines[len(h.engines)-1]
		}
		return e != nil
	})
	return e
}

func (h *harness) handle(i int) queue.Handle {
	s := h.q.Snapshot()
	all := append(append(s.History, *s.Current), s.Upcoming...)
	return all[i].Handle
}

func (h *harness) send(e *fakeEngine, ev Event) {
	h.t.Helper()
	e.events <- ev
	// Run is the only reader: once the channel is empty it has taken the
	// event, and a command sent afterwards runs after the event is handled.
	// (Run's select picks randomly among ready channels, so a round trip
	// alone is not enough.)
	waitFor(h.t, func() bool { return len(e.events) == 0 })
	_ = h.p.do(context.Background(), func(context.Context) error { return nil })
}

// waitLoads waits until Run has handled n resolve results.
func (h *harness) waitLoads(n int64) {
	h.t.Helper()
	waitFor(h.t, func() bool { return h.loads.Load() >= n })
}

func (h *harness) waitStatus(cond func(Status) bool) Status {
	h.t.Helper()
	var s Status
	waitFor(h.t, func() bool { s = h.p.Status(); return cond(s) })
	return s
}

func ctx() context.Context { return context.Background() }

func TestPlayStartsQueueAndReportsBitPerfect(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	if err := h.p.Play(ctx()); err != nil {
		t.Fatal(err)
	}
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.has(fmt.Sprintf("play %d@0s", ha)) })
	if s := h.p.Status(); s.State != Loading || s.Track.ID != "a" {
		t.Errorf("status before start = %+v", s)
	}
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventFormat, Decoded: AudioParams{192000, 2, "s32"}, Output: AudioParams{192000, 2, "s32"}, Volume: 100})
	s := h.p.Status()
	if s.State != Playing || s.Source.String() != "FLAC 24-bit/192 kHz" || s.Output.Rate != 192000 {
		t.Errorf("status = %+v", s)
	}
	if s.Indicator.State != BitPerfect || s.VolumeEnabled || s.Preview {
		t.Errorf("indicator %+v volume enabled %v preview %v", s.Indicator, s.VolumeEnabled, s.Preview)
	}

	// Resampling at the hardware turns the indicator off.
	h.prober.mu.Lock()
	h.prober.hw.Rate = 48000
	h.prober.mu.Unlock()
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: time.Second, Duration: 5 * time.Minute})
	if s := h.p.Status(); s.Indicator.State != Altered || !strings.Contains(s.Indicator.Reason, "48 kHz") || s.Position != time.Second {
		t.Errorf("after resample: %+v", s)
	}
	// Unknown hardware also fails closed.
	h.prober.mu.Lock()
	h.prober.err = ErrOutputUnknown
	h.prober.mu.Unlock()
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 2 * time.Second, Duration: 5 * time.Minute})
	if s := h.p.Status(); s.Indicator.State != Altered || s.Output != nil {
		t.Errorf("unknown hw: %+v", s)
	}
}

func TestPreviewIsSurfaced(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.res.preview = true
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(0))})
	s := h.p.Status()
	if !s.Preview || !strings.Contains(s.PreviewMessage, "Preview only") || !s.VolumeEnabled {
		t.Errorf("status = %+v", s)
	}
}

func TestTrackEndAdvancesQueue(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha, hb := h.handle(0), h.handle(1)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
	waitFor(t, func() bool { return e.has(fmt.Sprintf("play %d@0s", hb)) })
	h.send(e, Event{Kind: EventStarted, Key: uint64(hb)})
	if s := h.p.Status(); s.Track.ID != "b" || s.State != Playing {
		t.Errorf("status = %+v", s)
	}
	h.send(e, Event{Kind: EventEnded, Key: uint64(hb), Reason: EndEOF})
	if s := h.p.Status(); s.State != Stopped {
		t.Errorf("end of queue state = %v", s.State)
	}
}

func TestGaplessPreload(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b", "c")
	h.p.Play(ctx())
	e := h.engine()
	ha, hb := h.handle(0), h.handle(1)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	// Far from the end: no preload.
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: time.Second, Duration: 5 * time.Minute})
	if e.count("preload") != 0 {
		t.Fatal("preloaded too early")
	}
	// Within PreloadAhead: the next track is fetched and preloaded once.
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 4*time.Minute + 30*time.Second, Duration: 5 * time.Minute})
	waitFor(t, func() bool { return e.has(fmt.Sprintf("preload %d", hb)) })
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 4*time.Minute + 31*time.Second, Duration: 5 * time.Minute})
	if e.count("preload") != 1 {
		t.Errorf("preloads = %d", e.count("preload"))
	}
	// Track end: mpv moves on by itself, no new Play and no loading state.
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
	if s := h.p.Status(); s.State != Playing || s.Track.ID != "b" || h.loads.Load() != 2 {
		t.Errorf("after gapless end: state %v track %s loads %d", s.State, s.Track.ID, h.loads.Load())
	}
	h.send(e, Event{Kind: EventStarted, Key: uint64(hb)})
	if e.count("play") != 1 {
		t.Errorf("plays = %v, want gapless (no second play)", e.callList())
	}
	if s := h.p.Status(); s.Track.ID != "b" || s.State != Playing {
		t.Errorf("status = %+v", s)
	}
}

func TestQueueChangedWithdrawsStalePreload(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 20 * time.Second, Duration: 30 * time.Second})
	waitFor(t, func() bool { return e.count("preload") == 1 })

	h.p.QueueChanged(ctx()) // unchanged queue
	if e.has("clear-preload") {
		t.Error("cleared a preload that is still next")
	}
	h.q.PlayNext(Track{ID: "x"})
	h.p.QueueChanged(ctx())
	if !e.has("clear-preload") {
		t.Error("stale preload not cleared")
	}
	// The next progress tick preloads the new next track.
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 21 * time.Second, Duration: 30 * time.Second})
	waitFor(t, func() bool { return e.has(fmt.Sprintf("preload %d", h.handle(1))) })
}

func TestRepeatOneReplaysWithoutPreload(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	h.q.SetRepeat(queue.RepeatOne)
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 29 * time.Second, Duration: 30 * time.Second})
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
	waitFor(t, func() bool { return e.count("play") == 2 })
	if e.count("preload") != 0 || e.plays[1].Key != uint64(ha) {
		t.Errorf("calls = %v", e.callList())
	}
}

func TestPlaybackErrorsBecomeMessages(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(h *harness)
		event  func(h *harness, e *fakeEngine)
		want   string
		noPlay bool
	}{
		{name: "playing elsewhere", setup: func(h *harness) {
			h.res.errs["a"] = fmt.Errorf("x: %w", tidal.ErrConcurrentPlayback)
		}, want: "another app or device", noPlay: true},
		{name: "mpv missing", setup: func(h *harness) { h.newErr = fmt.Errorf("%w: exec", ErrMPVNotFound) }, want: "mpv is not installed", noPlay: true},
		{name: "busy DAC", event: func(h *harness, e *fakeEngine) {
			h.send(e, Event{Kind: EventEnded, Key: uint64(h.handle(0)), Reason: EndError, Err: ErrDeviceBusy})
		}, want: "in use by another app"},
		{name: "output failed", event: func(h *harness, e *fakeEngine) {
			h.send(e, Event{Kind: EventEnded, Key: uint64(h.handle(0)), Reason: EndError, Err: ErrOutputFailed})
		}, want: "-list-devices"},
		{name: "stream fails at the start", event: func(h *harness, e *fakeEngine) {
			h.send(e, Event{Kind: EventEnded, Key: uint64(h.handle(0)), Reason: EndError, Err: ErrStreamFailed})
		}, want: "stream stopped"},
		{name: "engine crash", event: func(h *harness, e *fakeEngine) {
			h.send(e, Event{Kind: EventFailed, Err: ErrEngineFailed})
		}, want: "stopped unexpectedly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, config.AudioExclusive, "a")
			if tt.setup != nil {
				tt.setup(h)
			}
			h.p.Play(ctx())
			if !tt.noPlay {
				e := h.engine()
				waitFor(t, func() bool { return e.count("play") == 1 })
				tt.event(h, e)
			}
			s := h.waitStatus(func(s Status) bool { return s.Message != "" })
			if s.State != Stopped || !strings.Contains(s.Message, tt.want) {
				t.Errorf("status = %v %q, want message containing %q", s.State, s.Message, tt.want)
			}
		})
	}
}

func TestStreamFailureMidTrackResumesOnce(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a")
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndError, Err: ErrStreamFailed, Position: 10 * time.Second})
	waitFor(t, func() bool { return e.has(fmt.Sprintf("play %d@10s", ha)) })
	h.res.mu.Lock()
	inv := append([]string(nil), h.res.invalidated...)
	h.res.mu.Unlock()
	if len(inv) != 1 || inv[0] != "a" {
		t.Errorf("invalidated = %v", inv)
	}
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndError, Err: ErrStreamFailed, Position: 12 * time.Second})
	if s := h.p.Status(); s.State != Stopped || !strings.Contains(s.Message, "stream stopped") {
		t.Errorf("second failure: %+v", s)
	}
}

func TestEngineRestartsAfterCrash(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a")
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventFailed, Err: ErrEngineFailed})
	close(e.events)
	h.p.Play(ctx())
	waitFor(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.engines) == 2 })
	waitFor(t, func() bool { return h.engine().count("play") == 1 })
}

func TestSupersededLoadIsDiscarded(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	gate := make(chan struct{})
	h.res.gates["a"] = gate
	h.p.Play(ctx())
	h.p.Next(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	close(gate)
	h.waitLoads(2)
	if e.count("play") != 1 || e.plays[0].Key != uint64(h.handle(1)) {
		t.Errorf("calls = %v, want only b", e.callList())
	}
}

func TestTransportControls(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	// No engine yet: controls are harmless.
	if err := h.p.TogglePause(ctx()); err != nil {
		t.Fatal(err)
	}
	if err := h.p.SetVolume(ctx(), 50); err != nil {
		t.Fatal(err)
	}
	if err := h.p.Stop(ctx()); err != nil {
		t.Fatal(err)
	}
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.p.TogglePause(ctx())
	if s := h.p.Status(); s.State != Paused || !e.has("pause true") {
		t.Errorf("pause: %v %v", s.State, e.callList())
	}
	h.p.TogglePause(ctx())
	if s := h.p.Status(); s.State != Playing || !e.has("pause false") {
		t.Errorf("resume: %v", s.State)
	}
	if err := h.p.SetVolume(ctx(), 60); err != nil || !e.has("volume 60") {
		t.Errorf("volume: %v %v", err, e.callList())
	}
	h.send(e, Event{Kind: EventFormat, Output: AudioParams{SampleRate: 192000, Format: "s32"}, Volume: 60})
	if s := h.p.Status(); s.Volume != 60 || s.Indicator.State != Altered {
		t.Errorf("volume status: %+v", s)
	}
	h.p.Next(ctx())
	waitFor(t, func() bool { return e.count("play") == 2 })
	h.p.Previous(ctx())
	waitFor(t, func() bool { return e.count("play") == 3 })
	h.p.Next(ctx())
	waitFor(t, func() bool { return e.count("play") == 4 })
	h.p.Next(ctx()) // past the end with repeat off: stop
	waitFor(t, func() bool { return e.has("stop") })
	if s := h.p.Status(); s.State != Stopped {
		t.Errorf("state after end = %v", s.State)
	}
	// Events for entries that are not current are ignored.
	h.send(e, Event{Kind: EventStarted, Key: 999})
	h.send(e, Event{Kind: EventEnded, Key: 999, Reason: EndEOF})
	h.send(e, Event{Kind: EventProgress, Key: 999})
	if s := h.p.Status(); s.State != Stopped {
		t.Errorf("stale events changed state to %v", s.State)
	}
}

func TestExclusiveVolumeDisabled(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a")
	if err := h.p.SetVolume(ctx(), 50); !errors.Is(err, ErrVolumeDisabled) {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(Message(ErrVolumeDisabled), "DAC or amp") {
		t.Error("message")
	}
}

func TestPlayWithEmptyQueue(t *testing.T) {
	h := newHarness(t, config.AudioShared)
	if err := h.p.Play(ctx()); err != nil {
		t.Fatal(err)
	}
	if err := h.p.Previous(ctx()); err != nil {
		t.Fatal(err)
	}
	if s := h.p.Status(); s.State != Stopped {
		t.Errorf("state = %v", s.State)
	}
}

func TestRunClosesEngineAndCommandsHonorContext(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.cancel()
	if err := <-h.runErr; !errors.Is(err, context.Canceled) {
		t.Errorf("Run err = %v", err)
	}
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if !closed {
		t.Error("engine not closed")
	}
	c, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.p.Play(c); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Play after Run err = %v", err)
	}
}

func TestStatusIsACopy(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(0))})
	h.send(e, Event{Kind: EventFormat, Output: AudioParams{SampleRate: 192000, Format: "s32"}, Volume: 100})
	s := h.p.Status()
	s.Track.ID, s.Source.Codec, s.Output.Rate = "mutated", "x", 1
	again := h.p.Status()
	if again.Track.ID != "a" || again.Source.Codec != "FLAC" || again.Output.Rate != 192000 {
		t.Errorf("status shares memory: %+v", again)
	}
	select {
	case <-h.p.Updates():
	default:
		t.Error("no update signaled")
	}
}

func TestMessage(t *testing.T) {
	tests := map[error]string{
		nil:                         "",
		ErrMPVNotFound:              "not installed",
		ErrDeviceBusy:               "in use",
		ErrOutputFailed:             "audio.device",
		ErrStreamFailed:             "stream stopped",
		ErrEngineFailed:             "unexpectedly",
		ErrIPCClosed:                "unexpectedly",
		ErrVolumeDisabled:           "DAC or amp",
		tidal.ErrConcurrentPlayback: "another app",
	}
	for err, want := range tests {
		if got := Message(err); (want == "" && got != "") || !strings.Contains(got, want) {
			t.Errorf("Message(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestPreloadFailuresAreIgnored(t *testing.T) {
	near := func(h *harness, e *fakeEngine, ha queue.Handle, pos time.Duration) {
		h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: pos, Duration: 30 * time.Second})
	}
	t.Run("resolve error", func(t *testing.T) {
		h := newHarness(t, config.AudioExclusive, "a", "b")
		h.res.errs["b"] = errors.New("offline")
		h.p.Play(ctx())
		e := h.engine()
		ha := h.handle(0)
		waitFor(t, func() bool { return e.count("play") == 1 })
		h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
		near(h, e, ha, 20*time.Second)
		h.waitLoads(2)
		if e.count("preload") != 0 || h.p.Status().Message != "" {
			t.Errorf("calls %v message %q", e.callList(), h.p.Status().Message)
		}
		// Track end falls back to a normal load.
		h.res.mu.Lock()
		delete(h.res.errs, "b")
		h.res.mu.Unlock()
		h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
		waitFor(t, func() bool { return e.count("play") == 2 })
	})
	t.Run("engine rejects preload", func(t *testing.T) {
		h := newHarness(t, config.AudioExclusive, "a", "b")
		h.p.Play(ctx())
		e := h.engine()
		e.setFail("preload", errors.New("nope"))
		ha := h.handle(0)
		waitFor(t, func() bool { return e.count("play") == 1 })
		h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
		near(h, e, ha, 20*time.Second)
		h.waitLoads(2)
		if e.count("preload") != 1 {
			t.Fatalf("calls = %v", e.callList())
		}
		h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
		waitFor(t, func() bool { return e.count("play") == 2 })
	})
	t.Run("queue changed while fetching", func(t *testing.T) {
		h := newHarness(t, config.AudioExclusive, "a", "b")
		gate := make(chan struct{})
		h.res.gates["b"] = gate
		h.p.Play(ctx())
		e := h.engine()
		ha := h.handle(0)
		waitFor(t, func() bool { return e.count("play") == 1 })
		h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
		near(h, e, ha, 20*time.Second)
		h.q.PlayNext(Track{ID: "x"})
		close(gate)
		h.waitLoads(2)
		if e.count("preload") != 0 {
			t.Errorf("preloaded a track that is no longer next: %v", e.callList())
		}
	})
	t.Run("nothing upcoming", func(t *testing.T) {
		h := newHarness(t, config.AudioExclusive, "a")
		h.p.Play(ctx())
		e := h.engine()
		ha := h.handle(0)
		waitFor(t, func() bool { return e.count("play") == 1 })
		h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
		near(h, e, ha, 29*time.Second)
		h.p.QueueChanged(ctx())
		if e.count("preload") != 0 || e.has("clear-preload") {
			t.Errorf("calls = %v", e.callList())
		}
	})
}

func TestEngineCommandErrors(t *testing.T) {
	boom := errors.New("boom")
	h := newHarness(t, config.AudioShared, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})

	e.setFail("pause", boom)
	if err := h.p.TogglePause(ctx()); !errors.Is(err, boom) || h.p.Status().State != Playing {
		t.Errorf("pause err = %v state %v", err, h.p.Status().State)
	}
	e.setFail("play", fmt.Errorf("%w: gone", ErrIPCClosed))
	h.p.Next(ctx())
	s := h.waitStatus(func(s Status) bool { return s.Message != "" })
	if s.State != Stopped || !strings.Contains(s.Message, "unexpectedly") {
		t.Errorf("play failure: %+v", s)
	}
}

func TestEngineStartFailsAfterCrash(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventFailed, Err: ErrEngineFailed})
	close(e.events) // the closed channel is dropped, not spun on
	h.mu.Lock()
	h.newErr = ErrMPVNotFound
	h.mu.Unlock()
	h.p.Play(ctx())
	s := h.waitStatus(func(s Status) bool { return strings.Contains(s.Message, "not installed") })
	if s.State != Stopped {
		t.Errorf("state = %v", s.State)
	}
}

func TestResumeBudgetResetsPerTrack(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha, hb := h.handle(0), h.handle(1)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndError, Err: ErrStreamFailed, Position: 5 * time.Second})
	waitFor(t, func() bool { return e.count("play") == 2 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
	waitFor(t, func() bool { return e.count("play") == 3 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(hb)})
	h.p.do(ctx(), func(context.Context) error {
		if len(h.p.resumed) != 0 {
			t.Errorf("resumed = %v, want the finished track forgotten", h.p.resumed)
		}
		return nil
	})
	// Format events before any track is known do not assess.
	h2 := newHarness(t, config.AudioShared)
	h2.p.do(ctx(), func(context.Context) error {
		h2.p.onEvent(ctx(), Event{Kind: EventFormat, Volume: 100})
		if h2.p.st.Indicator.Reason != "" || h2.p.trackID() != "" {
			t.Error("assessed without a source")
		}
		return nil
	})
}

func TestStopAfterEngineFailure(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	e := (*fakeEngine)(nil)
	_ = e
	if err := h.p.QueueChanged(ctx()); err != nil {
		t.Fatal(err)
	}
	// Results that arrive after Run exits do not block the resolver.
	gate := make(chan struct{})
	h.res.gates["a"] = gate
	h.p.Play(ctx())
	h.cancel()
	<-h.runErr
	close(gate)
	waitFor(t, func() bool { h.res.mu.Lock(); defer h.res.mu.Unlock(); return len(h.res.resolved) == 1 })
}

// SetPaused is idempotent: MPRIS Play and Pause are not toggles.
func TestSetPaused(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	if err := h.p.SetPaused(ctx(), true); err != nil { // no engine yet
		t.Fatal(err)
	}
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(0))})
	steps := []struct {
		paused bool
		want   PlayState
		calls  int // engine pause calls so far
	}{
		{false, Playing, 0}, // already playing
		{true, Paused, 1},
		{true, Paused, 1}, // already paused
		{false, Playing, 2},
	}
	for i, st := range steps {
		if err := h.p.SetPaused(ctx(), st.paused); err != nil {
			t.Fatal(err)
		}
		if s := h.p.Status(); s.State != st.want || e.count("pause true")+e.count("pause false") != st.calls {
			t.Errorf("step %d: state %v, calls %v", i, s.State, e.callList())
		}
	}
	h.p.Stop(ctx())
	if err := h.p.SetPaused(ctx(), false); err != nil || h.p.Status().State != Stopped {
		t.Errorf("stopped: %v %v", err, h.p.Status().State)
	}
}

func TestUnavailableTracksAreSkipped(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b", "c")
	h.res.errs["a"] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	h.p.Play(ctx())
	h.waitLoads(2)
	st := h.waitStatus(func(s Status) bool { return s.State == Loading || s.State == Playing })
	if c, _ := h.q.Current(); c.ID != "b" {
		t.Fatalf("current %q, want b", c.ID)
	}
	if st.Notice != `Skipped "Song a": This track is not available on TIDAL.` || st.NoticeID != 1 || st.Message != "" {
		t.Errorf("status %+v", st)
	}
	e := h.engine()
	e.mu.Lock()
	played := len(e.plays) == 1 && e.plays[0].Key == uint64(h.handle(1))
	e.mu.Unlock()
	if !played {
		t.Errorf("plays %+v", e.plays)
	}
}

func TestSkippedTracksInARowAreCounted(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b", "c")
	h.res.errs["a"] = fmt.Errorf("x: %w", tidal.ErrGeoRestricted)
	h.res.errs["b"] = fmt.Errorf("x: %w", tidal.ErrInvalidResourceID)
	h.p.Play(ctx())
	h.waitLoads(3)
	st := h.p.Status()
	if c, _ := h.q.Current(); c.ID != "c" || st.Notice != "Skipped 2 tracks that are not available." || st.NoticeID != 2 {
		t.Errorf("current %q status %+v", c.ID, st)
	}
}

func TestSkipStopsAtTheEndOfTheQueue(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.res.errs["b"] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	h.q.Next() // b is current, and last
	h.p.Play(ctx())
	h.waitLoads(1)
	st := h.p.Status()
	if c, _ := h.q.Current(); c.ID != "b" || st.State != Stopped || st.Notice == "" || st.Message != "" {
		t.Errorf("current %q status %+v", c.ID, st)
	}
}

func TestSkipStopsAfterAFullPass(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.q.SetRepeat(queue.RepeatAll)
	for _, id := range []string{"a", "b"} {
		h.res.errs[id] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	}
	h.p.Play(ctx())
	h.waitLoads(2)
	st := h.waitStatus(func(s Status) bool { return s.State == Stopped })
	if st.Message != "None of the tracks in the queue can be played." {
		t.Errorf("status %+v", st)
	}
	// It does not keep going round.
	time.Sleep(50 * time.Millisecond)
	if n := h.loads.Load(); n != 2 {
		t.Errorf("loads %d after stopping", n)
	}
	// Trying again starts a fresh pass.
	h.res.mu.Lock()
	delete(h.res.errs, "b")
	h.res.mu.Unlock()
	h.p.Play(ctx())
	h.waitLoads(3)
	if c, _ := h.q.Current(); c.ID != "b" || h.p.Status().State != Loading {
		t.Errorf("after retry current %q status %+v", c.ID, h.p.Status())
	}
}

func TestStartedTrackResetsTheSkipCount(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b", "c", "d")
	h.res.errs["a"] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	h.res.errs["c"] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	h.p.Play(ctx())
	h.waitLoads(2)
	e := h.engine()
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(1))})
	h.send(e, Event{Kind: EventEnded, Key: uint64(h.handle(1)), Reason: EndEOF})
	h.waitLoads(4)
	if st := h.p.Status(); st.Notice != `Skipped "Song c": This track is not available on TIDAL.` {
		t.Errorf("notice %q: the count was not reset", st.Notice)
	}
}

func TestOtherLoadErrorsStillStop(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.res.errs["a"] = fmt.Errorf("x: %w", tidal.ErrPurchaseRequired)
	h.p.Play(ctx())
	h.waitLoads(1)
	st := h.p.Status()
	if c, _ := h.q.Current(); c.ID != "a" || st.State != Stopped || st.Notice != "" || !strings.Contains(st.Message, "purchased") {
		t.Errorf("current %q status %+v", c.ID, st)
	}
}

// A skip that reaches the end of the queue stops the engine; if that
// fails, the player still ends up stopped.
func TestSkipToTheEndWithAStopError(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.res.errs["b"] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	h.p.Play(ctx())
	e := h.engine()
	e.setFail("stop", errors.New("ipc closed"))
	h.p.Next(ctx())
	h.waitLoads(2)
	st := h.waitStatus(func(s Status) bool { return s.State == Stopped })
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Contains(e.calls, "stop") || st.Notice == "" {
		t.Errorf("calls %v status %+v", e.calls, st)
	}
}

// Playing or skipping by hand in the middle of a run of skips starts a
// new count.
func TestUserActionResetsTheSkipCount(t *testing.T) {
	for _, tt := range []struct {
		name string
		act  func(h *harness) // each lands on c, which is unavailable
	}{
		{"play", func(h *harness) { h.q.Jump(h.handle(2)); h.p.Play(ctx()) }},
		{"next", func(h *harness) { h.p.Next(ctx()) }},
		{"previous", func(h *harness) { h.q.Jump(h.handle(3)); h.p.Previous(ctx()) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, config.AudioShared, "a", "b", "c", "d")
			for _, id := range []string{"a", "c"} {
				h.res.errs[id] = fmt.Errorf("x: %w", tidal.ErrNotFound)
			}
			gate := make(chan struct{})
			h.res.mu.Lock()
			h.res.gates["b"] = gate
			h.res.mu.Unlock()
			h.p.Play(ctx())
			h.waitLoads(1) // a skipped; b is loading, held at the gate
			tt.act(h)
			h.waitLoads(3) // c skipped, d loaded
			close(gate)    // b's result is now stale
			want := `Skipped "Song c": This track is not available on TIDAL.`
			if st := h.p.Status(); st.Notice != want {
				t.Errorf("notice %q, want %q", st.Notice, want)
			}
		})
	}
}

// EndOfQueue tells autoplay that playback ran out, not that it was stopped.
func TestEndOfQueue(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.p.Play(ctx())
	e := h.engine()
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(0))})
	h.send(e, Event{Kind: EventEnded, Key: uint64(h.handle(0)), Reason: EndEOF})
	if st := h.waitStatus(func(s Status) bool { return s.State == Stopped }); !st.EndOfQueue {
		t.Error("ran out: EndOfQueue not set")
	}
	h.p.Stop(ctx())
	if h.p.Status().EndOfQueue {
		t.Error("stop after running out kept EndOfQueue")
	}
	h.p.Next(ctx()) // ran out again
	h.p.Play(ctx())
	if h.p.Status().EndOfQueue {
		t.Error("a new load kept EndOfQueue")
	}
	h.p.Stop(ctx())
	if h.p.Status().EndOfQueue {
		t.Error("stop set EndOfQueue")
	}
	// Next on the last track ends the queue too.
	h.p.Play(ctx())
	h.p.Next(ctx())
	if st := h.p.Status(); st.State != Stopped || !st.EndOfQueue {
		t.Errorf("next on the last track: %+v", st)
	}
	// So does skipping an unavailable last track.
	s := newHarness(t, config.AudioShared, "a", "x")
	s.res.errs["x"] = fmt.Errorf("x: %w", tidal.ErrNotFound)
	s.q.Next()
	s.p.Play(ctx())
	s.waitLoads(1)
	if !s.p.Status().EndOfQueue {
		t.Error("skip at the end: EndOfQueue not set")
	}
}

package player

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	return newHarnessVolume(t, mode, "", tracks...)
}

// newHarnessVolume is newHarness with a volume control (decision 84).
func newHarnessVolume(t *testing.T, mode config.AudioMode, vol config.VolumeControl, tracks ...string) *harness {
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
		Volume:   vol,
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

// TestSoftwareVolumeInExclusive covers TASK-018 AC #2: with software
// volume chosen, volume works in exclusive mode, and the indicator is
// bit-perfect only at 100%.
func TestSoftwareVolumeInExclusive(t *testing.T) {
	h := newHarnessVolume(t, config.AudioExclusive, config.VolumeSoftware, "a")
	if !h.p.Status().VolumeEnabled {
		t.Fatal("volume disabled with software volume")
	}
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	exact := Event{Kind: EventFormat, Decoded: AudioParams{192000, 2, "s32"}, Output: AudioParams{192000, 2, "s32"}, Volume: 100}
	h.send(e, exact)
	if s := h.p.Status(); s.Indicator.State != BitPerfect {
		t.Fatalf("at 100%%: %+v", s.Indicator)
	}
	level, err := h.p.AdjustVolume(ctx(), -5)
	if err != nil || level != 95 || !e.has("volume 95") {
		t.Fatalf("adjust: %v %v %v", level, err, e.callList())
	}
	if s := h.p.Status(); s.Volume != 95 {
		t.Errorf("status volume %v before mpv reports it", s.Volume)
	}
	lower := exact
	lower.Volume = 95
	h.send(e, lower)
	if s := h.p.Status(); s.Indicator.State != Altered || !strings.Contains(s.Indicator.Reason, "software volume 95%") {
		t.Errorf("below 100%%: %+v", s.Indicator)
	}
	h.p.AdjustVolume(ctx(), 5)
	h.send(e, exact)
	if s := h.p.Status(); s.Indicator.State != BitPerfect {
		t.Errorf("back at 100%%: %+v", s.Indicator)
	}
}

func TestAdjustVolume(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	steps := []struct {
		delta, want float64
	}{{5, 100}, {-30, 70}, {-2.4, 68}, {-100, 0}, {-5, 0}, {150, 100}}
	for _, st := range steps {
		got, err := h.p.AdjustVolume(ctx(), st.delta)
		if err != nil || got != st.want {
			t.Errorf("adjust %v = %v, %v; want %v", st.delta, got, err, st.want)
		}
	}
	for _, bad := range []float64{-1, 101, math.NaN()} {
		if err := h.p.SetVolume(ctx(), bad); err == nil {
			t.Errorf("volume %v accepted", bad)
		}
	}
	ex := newHarness(t, config.AudioExclusive, "a")
	if _, err := ex.p.AdjustVolume(ctx(), -5); !errors.Is(err, ErrVolumeDisabled) {
		t.Errorf("exclusive adjust err = %v", err)
	}
	if s := ex.p.Status(); s.Volume != 100 {
		t.Errorf("a refused change moved the volume to %v", s.Volume)
	}
}

// TestSetVolumeControl covers decision 83: the control applies at once, and
// going back to device returns the volume to 100%.
func TestSetVolumeControl(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a")
	if h.p.Status().VolumeEnabled {
		t.Fatal("device volume enabled in exclusive mode")
	}
	// Switching with no engine and the volume at 100% sends nothing.
	if err := h.p.SetVolumeControl(ctx(), config.VolumeDevice); err != nil {
		t.Fatal(err)
	}
	if err := h.p.SetVolumeControl(ctx(), config.VolumeSoftware); err != nil || !h.p.Status().VolumeEnabled {
		t.Fatalf("software: %v %+v", err, h.p.Status())
	}
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	if err := h.p.SetVolume(ctx(), 40); err != nil {
		t.Fatal(err)
	}
	if err := h.p.SetVolumeControl(ctx(), config.VolumeDevice); err != nil {
		t.Fatal(err)
	}
	if s := h.p.Status(); s.VolumeEnabled || s.Volume != 100 || !e.has("volume 100") {
		t.Errorf("back to device: %+v %v", s, e.callList())
	}
	if err := h.p.SetVolume(ctx(), 40); !errors.Is(err, ErrVolumeDisabled) {
		t.Errorf("err = %v", err)
	}
	// Without an engine the reset is only recorded.
	idle := newHarnessVolume(t, config.AudioExclusive, config.VolumeSoftware)
	idle.p.SetVolume(ctx(), 30)
	if err := idle.p.SetVolumeControl(ctx(), config.VolumeDevice); err != nil || idle.p.Status().Volume != 100 {
		t.Errorf("idle reset: %v %+v", err, idle.p.Status())
	}
}

// TestVolumeKeptOnNewEngine covers decision 85: the level lasts for the
// session, across engine restarts, while software volume is allowed.
func TestVolumeKeptOnNewEngine(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.p.SetVolume(ctx(), 70) // no engine yet
	h.p.Play(ctx())
	e1 := h.engine()
	waitFor(t, func() bool { return e1.count("play") == 1 })
	if c := e1.callList(); c[0] != "volume 70" {
		t.Errorf("first engine calls %v, want the volume first", c)
	}
	var made atomic.Int32
	h.p.SetAudio(ctx(), h.newAudio(config.AudioShared, &made))
	var e2 *fakeEngine
	waitFor(t, func() bool { e2 = h.engine(); return e2 != e1 && e2.count("play") == 1 })
	if !e2.has("volume 70") {
		t.Errorf("new engine calls %v", e2.callList())
	}
	// To exclusive with the volume on the device: back to 100%, and the
	// new engine gets no volume call.
	h.p.SetAudio(ctx(), h.newAudio(config.AudioExclusive, &made))
	var e3 *fakeEngine
	waitFor(t, func() bool { e3 = h.engine(); return e3 != e2 && e3.count("play") == 1 })
	if s := h.p.Status(); s.Volume != 100 || s.VolumeEnabled || e3.count("volume") != 0 {
		t.Errorf("exclusive: %+v %v", s, e3.callList())
	}
	// A failed restore is logged, and playback goes on.
	f := newHarness(t, config.AudioShared, "a")
	f.p.SetVolume(ctx(), 50)
	f.mu.Lock()
	f.newErr = nil
	f.mu.Unlock()
	failing := newFakeEngine()
	failing.setFail("volume", errors.New("ipc"))
	f.p.SetAudio(ctx(), Audio{Mode: config.AudioShared, Prober: f.prober, NewEngine: func(context.Context) (Engine, error) { return failing, nil }})
	f.p.Play(ctx())
	waitFor(t, func() bool { return failing.count("play") == 1 })
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

// The visualizer reads the manifest and the status only: neither touches
// the engine, so a bit-perfect track stays bit-perfect (TASK-020 AC #2).
func TestManifestDoesNotTouchPlayback(t *testing.T) {
	h := newHarness(t, config.AudioExclusive, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e.has(fmt.Sprintf("play %d@0s", ha)) })
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventFormat, Decoded: AudioParams{192000, 2, "s32"}, Output: AudioParams{192000, 2, "s32"}, Volume: 100})
	before := h.p.Status()
	if before.Indicator.State != BitPerfect || before.Handle != ha {
		t.Fatalf("status %+v", before)
	}
	e.mu.Lock()
	calls := len(e.calls)
	e.mu.Unlock()
	for range 20 {
		mpd, err := h.p.Manifest(ctx(), ha)
		if err != nil || string(mpd) != "<MPD/>" {
			t.Fatalf("manifest %q, %v", mpd, err)
		}
		mpd[0] = 'X' // a copy: the player's manifest is not changed
		h.p.Status()
	}
	after := h.p.Status()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) != calls || after.Indicator != before.Indicator || after.State != Playing {
		t.Errorf("engine calls %v, indicator %+v", e.calls[calls:], after.Indicator)
	}
	if m, _ := h.p.Manifest(ctx(), ha); string(m) != "<MPD/>" {
		t.Errorf("the player's manifest changed: %q", m)
	}
	if _, err := h.p.Manifest(ctx(), h.handle(1)); !errors.Is(err, ErrNoManifest) {
		t.Errorf("not loaded: %v", err)
	}
}

func TestStatusHandleFollowsGapless(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha, hb := h.handle(0), h.handle(1)
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 28 * time.Second, Duration: 30 * time.Second})
	waitFor(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.preloads) == 1 })
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
	if s := h.p.Status(); s.Handle != hb {
		t.Errorf("handle %d after the gapless switch, want %d", s.Handle, hb)
	}
	if _, err := h.p.Manifest(ctx(), hb); err != nil {
		t.Errorf("preloaded manifest: %v", err)
	}
}

// newAudio returns an Audio whose engines and prober the test can see.
func (h *harness) newAudio(mode config.AudioMode, made *atomic.Int32) Audio {
	return Audio{Mode: mode, Prober: h.prober, NewEngine: func(ctx context.Context) (Engine, error) {
		made.Add(1)
		h.mu.Lock()
		defer h.mu.Unlock()
		e := newFakeEngine()
		h.engines = append(h.engines, e)
		return e, nil
	}}
}

func TestSetAudioWhilePlaying(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.p.Play(ctx())
	e1 := h.engine()
	ha := h.handle(0)
	waitFor(t, func() bool { return e1.has(fmt.Sprintf("play %d@0s", ha)) })
	h.send(e1, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e1, Event{Kind: EventFormat, Decoded: AudioParams{192000, 2, "s32"}, Output: AudioParams{48000, 2, "float"}, Volume: 60})
	h.send(e1, Event{Kind: EventProgress, Key: uint64(ha), Position: 12 * time.Second, Duration: 30 * time.Second})
	if s := h.p.Status(); s.Indicator.State != Altered || !s.VolumeEnabled {
		t.Fatalf("shared status %+v", s)
	}
	var made atomic.Int32
	if err := h.p.SetAudio(ctx(), h.newAudio(config.AudioExclusive, &made)); err != nil {
		t.Fatal(err)
	}
	e1.mu.Lock()
	closed := e1.closed
	e1.mu.Unlock()
	s := h.p.Status()
	if !closed || s.VolumeEnabled || s.Mode != config.AudioExclusive || s.Indicator != (Assessment{}) || s.Output != nil || s.State != Loading {
		t.Fatalf("after the switch: closed %v status %+v", closed, s)
	}
	// The same track resumes at its position on the new engine.
	var e2 *fakeEngine
	waitFor(t, func() bool { e2 = h.engine(); return e2 != e1 && e2.has(fmt.Sprintf("play %d@12s", ha)) })
	if made.Load() != 1 {
		t.Errorf("engines made %d", made.Load())
	}
	// Events from the closed engine are ignored; the new one drives status,
	// assessed in the new mode.
	e1.events <- Event{Kind: EventFailed, Err: errors.New("old engine")}
	h.send(e2, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e2, Event{Kind: EventFormat, Decoded: AudioParams{192000, 2, "s32"}, Output: AudioParams{192000, 2, "s32"}, Volume: 100})
	if s := h.p.Status(); s.State != Playing || s.Indicator.State != BitPerfect || s.Message != "" {
		t.Errorf("new engine status %+v", s)
	}
	if err := h.p.SetVolume(ctx(), 50); !errors.Is(err, ErrVolumeDisabled) {
		t.Errorf("volume in exclusive: %v", err)
	}
}

func TestSetAudioWhilePaused(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.p.Play(ctx())
	e1 := h.engine()
	ha := h.handle(0)
	h.send(e1, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e1, Event{Kind: EventProgress, Key: uint64(ha), Position: 5 * time.Second, Duration: 30 * time.Second})
	h.p.TogglePause(ctx())
	var made atomic.Int32
	h.p.SetAudio(ctx(), h.newAudio(config.AudioShared, &made))
	var e2 *fakeEngine
	waitFor(t, func() bool { e2 = h.engine(); return e2 != e1 && e2.has(fmt.Sprintf("play %d@5s", ha)) })
	h.send(e2, Event{Kind: EventStarted, Key: uint64(ha)})
	if s := h.p.Status(); s.State != Paused || !e2.has("pause true") {
		t.Errorf("state %v calls %v", s.State, e2.callList())
	}
	// Only once: a later start (after a seek, say) is not paused again.
	h.send(e2, Event{Kind: EventStarted, Key: uint64(ha)})
	if n := strings.Count(strings.Join(e2.callList(), ","), "pause true"); n != 1 {
		t.Errorf("paused %d times", n)
	}
}

func TestSetAudioStoppedOrIdle(t *testing.T) {
	// Before any playback there is no engine: the next play uses the new one.
	h := newHarness(t, config.AudioShared, "a")
	var made atomic.Int32
	h.p.SetAudio(ctx(), h.newAudio(config.AudioExclusive, &made))
	if made.Load() != 0 || h.p.Status().VolumeEnabled {
		t.Fatalf("made %d", made.Load())
	}
	h.p.Play(ctx())
	h.waitLoads(1)
	if made.Load() != 1 {
		t.Errorf("play did not use the new engine: %d", made.Load())
	}
	// Stopped: the engine closes and nothing plays until asked.
	e := h.engine()
	h.p.Stop(ctx())
	var made2 atomic.Int32
	h.p.SetAudio(ctx(), h.newAudio(config.AudioShared, &made2))
	time.Sleep(20 * time.Millisecond)
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if !closed || made2.Load() != 0 || h.p.Status().State != Stopped {
		t.Errorf("stopped: closed %v made %d state %v", closed, made2.Load(), h.p.Status().State)
	}
}

func TestDropPreload(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.p.Play(ctx())
	e := h.engine()
	ha, hb := h.handle(0), h.handle(1)
	h.send(e, Event{Kind: EventStarted, Key: uint64(ha)})
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 28 * time.Second, Duration: 30 * time.Second})
	waitFor(t, func() bool { return e.has(fmt.Sprintf("preload %d", hb)) })
	if err := h.p.DropPreload(ctx()); err != nil {
		t.Fatal(err)
	}
	if !e.has("clear-preload") {
		t.Error("preload not cleared")
	}
	if _, err := h.p.Manifest(ctx(), hb); !errors.Is(err, ErrNoManifest) {
		t.Errorf("dropped manifest kept: %v", err)
	}
	// A preload still being fetched is ignored when it arrives.
	gate := make(chan struct{})
	h.res.mu.Lock()
	h.res.gates["b"] = gate
	h.res.mu.Unlock()
	loads := h.loads.Load()
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 28500 * time.Millisecond, Duration: 30 * time.Second})
	h.p.DropPreload(ctx())
	close(gate)
	h.waitLoads(loads + 1)
	e.mu.Lock()
	preloads := len(e.preloads)
	e.mu.Unlock()
	if preloads != 1 {
		t.Errorf("a dropped fetch was preloaded: %d preloads", preloads)
	}
	// The next progress preloads again.
	h.res.mu.Lock()
	delete(h.res.gates, "b")
	h.res.mu.Unlock()
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 29 * time.Second, Duration: 30 * time.Second})
	waitFor(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.preloads) == 2 })
	// Nothing to drop, and no engine: no errors.
	h2 := newHarness(t, config.AudioShared, "a")
	if err := h2.p.DropPreload(ctx()); err != nil {
		t.Error(err)
	}
}

package visualizer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu     sync.Mutex
	clk    Clock
	mpdErr error
	asked  []uint64
}

func (f *fakeSource) Clock() Clock {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clk
}

func (f *fakeSource) set(c Clock) {
	f.mu.Lock()
	f.clk = c
	f.mu.Unlock()
}

func (f *fakeSource) Manifest(_ context.Context, key uint64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, key)
	return []byte("<MPD/>"), f.mpdErr
}

// fakeDecoder plays a 1 kHz tone; avail limits how many samples exist.
type fakeDecoder struct {
	closeErr error
	mu       sync.Mutex
	start    time.Duration
	taken    int
	avail    int // -1: unlimited
	closed   bool
}

func (d *fakeDecoder) Take(n int) []int16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.avail >= 0 {
		n = min(n, d.avail)
		d.avail -= n
	}
	out := make([]int16, max(n, 0))
	for i := range out {
		out[i] = int16(32000 * math.Sin(2*math.Pi*1000*float64(d.taken+i)/SampleRate))
	}
	d.taken += len(out)
	return out
}

func (d *fakeDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return d.closeErr
}

type ctlEnv struct {
	c        *Controller
	src      *fakeSource
	decs     []*fakeDecoder
	startErr error
	now      time.Time
	logs     *bytes.Buffer
}

func newCtlEnv(t *testing.T) *ctlEnv {
	t.Helper()
	e := &ctlEnv{src: &fakeSource{}, now: time.Unix(1000, 0), logs: &bytes.Buffer{}}
	e.c = New(Config{Source: e.src, Logger: slog.New(slog.NewTextHandler(e.logs, nil))})
	e.c.now = func() time.Time { return e.now }
	e.c.start = func(_ context.Context, mpd []byte, pos time.Duration) (decoder, error) {
		if e.startErr != nil {
			return nil, e.startErr
		}
		d := &fakeDecoder{start: pos, avail: -1}
		e.decs = append(e.decs, d)
		return d, nil
	}
	return e
}

func (e *ctlEnv) step() bool {
	e.now = e.now.Add(FrameInterval)
	return e.c.step(context.Background())
}

func (e *ctlEnv) frame(t *testing.T) Frame {
	t.Helper()
	select {
	case f := <-e.c.Frames():
		return f
	default:
		t.Fatal("no frame")
		return Frame{}
	}
}

func peak(f Frame) float64 {
	m := 0.0
	for _, v := range f {
		m = max(m, v)
	}
	return m
}

func TestControllerOffDoesNothing(t *testing.T) {
	e := newCtlEnv(t)
	e.src.set(Clock{Key: 1, Position: time.Second, Playing: true})
	if e.step() {
		t.Error("off: more frames wanted")
	}
	if len(e.decs) != 0 || peak(e.frame(t)) != 0 {
		t.Errorf("off: %d decoders", len(e.decs))
	}
}

func TestControllerFollowsPlayback(t *testing.T) {
	e := newCtlEnv(t)
	e.c.SetOn(true)
	e.src.set(Clock{Key: 7, Position: 30 * time.Second, Playing: true})
	for range 3 {
		if !e.step() {
			t.Fatal("playing: no more frames wanted")
		}
	}
	if len(e.decs) != 1 || e.decs[0].start != 30*time.Second || len(e.src.asked) != 1 || e.src.asked[0] != 7 {
		t.Fatalf("decoders %d asked %v", len(e.decs), e.src.asked)
	}
	// It takes about one frame of samples per frame, staying in step.
	if got, want := e.decs[0].taken, 2*SampleRate/20; got < want-2 || got > want+2 {
		t.Errorf("took %d samples, want about %d", got, want)
	}
	if peak(e.frame(t)) < 0.9 {
		t.Error("the tone does not show")
	}
}

func TestControllerSync(t *testing.T) {
	e := newCtlEnv(t)
	e.c.SetOn(true)
	e.src.set(Clock{Key: 1, Position: 10 * time.Second, Playing: true})
	e.step()
	d := e.decs[0]
	// A slow decode (nothing yet) is not restarted; it catches up once
	// samples arrive, skipping what it missed.
	d.avail = 0
	for range 50 { // 2.5 s with no samples
		e.step()
	}
	if len(e.decs) != 1 {
		t.Fatalf("restarted a slow decode: %d decoders", len(e.decs))
	}
	d.mu.Lock()
	d.avail = -1
	d.mu.Unlock()
	before := d.taken
	e.step()
	// Reports stopped for 2.5 s: it ran on no more than restartDrift past
	// the last one.
	if skipped := d.taken - before; skipped < 2*SampleRate || skipped > 2*SampleRate+SampleRate/20 {
		t.Errorf("caught up by %d samples, want 2 s worth", skipped)
	}
	// Ahead of playback (reports say the position stands still while
	// playing): it holds instead of running on.
	e.src.set(Clock{Key: 1, Position: e.c.at - 500*time.Millisecond, Playing: true})
	before = d.taken
	e.step()
	if d.taken != before {
		t.Errorf("ran ahead: took %d", d.taken-before)
	}
	// A seek: the reported position jumps, so the decode restarts there.
	e.src.set(Clock{Key: 1, Position: 2 * time.Minute, Playing: true})
	e.step()
	if len(e.decs) != 2 || !d.closed || e.decs[1].start != 2*time.Minute {
		t.Fatalf("seek: %d decoders, old closed %v", len(e.decs), d.closed)
	}
	// A new track restarts too.
	e.src.set(Clock{Key: 2, Position: 0, Playing: true})
	e.step()
	if len(e.decs) != 3 || !e.decs[1].closed || e.decs[2].start != 0 {
		t.Fatalf("new track: %d decoders", len(e.decs))
	}
}

func TestControllerPauseAndStop(t *testing.T) {
	e := newCtlEnv(t)
	e.c.SetOn(true)
	e.src.set(Clock{Key: 1, Position: time.Second, Playing: true})
	for range 4 {
		e.step()
	}
	d := e.decs[0]
	// Paused: the decode waits, the bars fall, then frames stop.
	e.src.set(Clock{Key: 1, Position: 1200 * time.Millisecond, Playing: false})
	steps := 0
	for e.step() {
		steps++
		if steps > 25 {
			t.Fatal("bars never emptied")
		}
	}
	if d.closed || peak(e.frame(t)) != 0 {
		t.Errorf("paused: decoder closed %v", d.closed)
	}
	if steps*int(FrameInterval) > int(time.Second) {
		t.Errorf("took %d frames to empty", steps)
	}
	// Resumed where it paused: the same decode carries on.
	e.src.set(Clock{Key: 1, Position: 1200 * time.Millisecond, Playing: true})
	e.step()
	if len(e.decs) != 1 {
		t.Errorf("resume restarted the decode")
	}
	// A seek while paused restarts on resume.
	e.src.set(Clock{Key: 1, Position: time.Minute, Playing: false})
	e.step()
	e.src.set(Clock{Key: 1, Position: time.Minute, Playing: true})
	e.step()
	if len(e.decs) != 2 {
		t.Errorf("seek while paused: %d decoders", len(e.decs))
	}
	// Stopped (no track): the decode ends.
	e.src.set(Clock{})
	e.step()
	if !e.decs[1].closed {
		t.Error("stopped: decoder kept")
	}
	// Off while playing: the decode ends too.
	e.src.set(Clock{Key: 3, Playing: true})
	e.step()
	e.c.SetOn(false)
	e.step()
	if !e.decs[2].closed {
		t.Error("off: decoder kept")
	}
}

func TestControllerFailures(t *testing.T) {
	for name, setup := range map[string]func(e *ctlEnv){
		"no manifest":     func(e *ctlEnv) { e.src.mpdErr = errors.New("gone") },
		"mpv won't start": func(e *ctlEnv) { e.startErr = errors.New("exec: mpv: not found") },
	} {
		e := newCtlEnv(t)
		setup(e)
		e.c.SetOn(true)
		e.src.set(Clock{Key: 1, Position: time.Second, Playing: true, TrackID: "123"})
		for range 5 {
			e.step()
		}
		if !strings.Contains(e.logs.String(), "track_id=123") {
			t.Errorf("%s: no track_id in %s", name, e.logs)
		}
		if peak(e.frame(t)) != 0 || strings.Count(e.logs.String(), "analysis decode not started") != 1 {
			t.Errorf("%s: logs %s", name, e.logs)
		}
		// Not retried for that track; the next track tries again.
		e.src.mpdErr, e.startErr = nil, nil
		e.step()
		if len(e.decs) != 0 {
			t.Errorf("%s: retried the same track", name)
		}
		// A seek on the failed track does not retry it either.
		e.src.set(Clock{Key: 1, Position: time.Minute, Playing: true})
		e.step()
		if len(e.decs) != 0 || len(e.src.asked) > 1 {
			t.Errorf("%s: retried after a seek (asked %v)", name, e.src.asked)
		}
		e.src.set(Clock{Key: 2, Playing: true})
		e.step()
		if len(e.decs) != 1 {
			t.Errorf("%s: next track not tried", name)
		}
	}
}

func TestControllerRun(t *testing.T) {
	e := newCtlEnv(t)
	e.c.now = time.Now
	e.src.set(Clock{Key: 1, Position: time.Second, Playing: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.c.Run(ctx); close(done) }()
	// Off: it sleeps. On: frames flow.
	e.c.SetOn(true)
	deadline := time.After(2 * time.Second)
	for got := 0; got < 3; {
		select {
		case f := <-e.c.Frames():
			if peak(f) > 0 {
				got++
			}
		case <-deadline:
			t.Fatal("no frames while on")
		}
	}
	cancel()
	<-done
	if !e.decs[0].closed {
		t.Error("Run left the decoder running")
	}
}

func TestSendKeepsTheNewest(t *testing.T) {
	e := newCtlEnv(t)
	e.c.send([Bands]float64{0: 0.1})
	e.c.send([Bands]float64{0: 0.2})
	if f := e.frame(t); f[0] != 0.2 {
		t.Errorf("got %v", f[0])
	}
}

// Off, Run sleeps without frames until woken; it quits while asleep too.
func TestControllerRunIdles(t *testing.T) {
	e := newCtlEnv(t)
	e.c.now = time.Now
	e.src.set(Clock{Key: 1, Playing: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.c.Run(ctx); close(done) }()
	<-e.c.Frames() // the first, empty frame; then it sleeps
	select {
	case <-e.c.Frames():
		t.Error("frames while off")
	case <-time.After(3 * FrameInterval):
	}
	e.c.SetOn(true) // wakes it
	waitFor(t, func() bool { return len(e.c.Frames()) > 0 })
	e.c.SetOn(false)
	time.Sleep(1200 * time.Millisecond) // the bars fall; it sleeps again
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not quit while asleep")
	}
}

func TestControllerDefaults(t *testing.T) {
	bin, _ := fakeMPV(t, `printf '\001\000'; sleep 5`)
	c := New(Config{Source: &fakeSource{}, Binary: bin, Dir: t.TempDir()})
	if c.logger == nil {
		t.Fatal("no default logger")
	}
	d, err := c.start(context.Background(), []byte("<MPD/>"), 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(d.Take(1)) == 1 })
	d.Close()
	// The default binary is "mpv" from PATH.
	t.Setenv("PATH", t.TempDir())
	if _, err := New(Config{Source: &fakeSource{}, Dir: t.TempDir()}).start(context.Background(), nil, 0); err == nil ||
		!strings.Contains(err.Error(), "start mpv") {
		t.Errorf("default binary: %v", err)
	}
}

func TestControllerLogsCleanupErrors(t *testing.T) {
	e := newCtlEnv(t)
	e.c.logger = slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.c.SetOn(true)
	e.src.set(Clock{Key: 1, Playing: true})
	e.step()
	e.decs[0].closeErr = errors.New("remove: busy")
	e.src.set(Clock{})
	e.step()
	if !strings.Contains(e.logs.String(), "decoder cleanup") {
		t.Errorf("log: %s", e.logs)
	}
}

// Without a private directory no manifest copy is written anywhere.
func TestControllerNeedsADirectory(t *testing.T) {
	c := New(Config{Source: &fakeSource{}})
	if _, err := c.start(context.Background(), []byte("<MPD/>"), 0); err == nil || !strings.Contains(err.Error(), "private directory") {
		t.Errorf("err %v", err)
	}
}

// Reports that come seconds apart, as playback moves on, are not seeks;
// neither is resuming after a long pause.
func TestControllerPredictsPosition(t *testing.T) {
	e := newCtlEnv(t)
	e.c.SetOn(true)
	e.src.set(Clock{Key: 1, Position: 10 * time.Second, Playing: true})
	e.step()
	for range 60 { // 3 s without a report
		e.step()
	}
	e.src.set(Clock{Key: 1, Position: 13*time.Second + FrameInterval, Playing: true})
	e.step()
	if len(e.decs) != 1 {
		t.Fatalf("a late report restarted the decode: %d decoders", len(e.decs))
	}
	e.src.set(Clock{Key: 1, Position: 14 * time.Second, Playing: false})
	e.step()
	for range 100 { // paused 5 s
		e.step()
	}
	e.src.set(Clock{Key: 1, Position: 14 * time.Second, Playing: true})
	e.step()
	if len(e.decs) != 1 {
		t.Errorf("resuming after a pause restarted the decode: %d decoders", len(e.decs))
	}
}

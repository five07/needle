package player

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/five07/needle/internal/config"
)

// setSeekInterval changes the seek interval on the Run goroutine.
func (h *harness) setSeekInterval(d time.Duration) {
	h.t.Helper()
	_ = h.p.do(ctx(), func(context.Context) error { h.p.cfg.SeekInterval = d; return nil })
}

// playing starts track 0, reports it started with the given duration at
// pos, and returns the engine.
func (h *harness) playing(dur, pos time.Duration) *fakeEngine {
	h.t.Helper()
	h.p.Play(ctx())
	e := h.engine()
	waitFor(h.t, func() bool { return e.count("play") == 1 })
	k := uint64(h.handle(0))
	h.send(e, Event{Kind: EventStarted, Key: k})
	h.send(e, Event{Kind: EventProgress, Key: k, Position: pos, Duration: dur})
	return e
}

func TestSeekNeedsAPlayingTrack(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	if _, err := h.p.SeekBy(ctx(), 5*time.Second); !errors.Is(err, ErrNotSeekable) {
		t.Errorf("stopped: err = %v", err)
	}
	gate := make(chan struct{})
	h.res.mu.Lock()
	h.res.gates["a"] = gate
	h.res.mu.Unlock()
	h.p.Play(ctx())
	if _, err := h.p.SeekBy(ctx(), 5*time.Second); !errors.Is(err, ErrNotSeekable) {
		t.Errorf("loading: err = %v", err)
	}
	close(gate)
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(0))})
	if _, err := h.p.SeekTo(ctx(), h.handle(1), time.Second); !errors.Is(err, ErrNotSeekable) {
		t.Errorf("other track: err = %v", err)
	}
	if n := e.count("seek"); n != 0 {
		t.Errorf("engine seeks = %d, want 0", n)
	}
}

func TestSeekStaysWithinTheTrack(t *testing.T) {
	const dur = 5 * time.Minute
	tests := []struct {
		name     string
		pos      time.Duration
		delta    time.Duration
		absolute bool
		want     time.Duration
	}{
		{name: "forward", pos: time.Minute, delta: 10 * time.Second, want: 70 * time.Second},
		{name: "back", pos: time.Minute, delta: -30 * time.Second, want: 30 * time.Second},
		{name: "back stops at the start", pos: 3 * time.Second, delta: -5 * time.Second, want: 0},
		{name: "forward stops before the end", pos: dur - 10*time.Second, delta: 30 * time.Second, want: dur - SeekEndMargin},
		{name: "absolute", pos: time.Minute, delta: 2 * time.Minute, absolute: true, want: 2 * time.Minute},
		{name: "absolute past the end", pos: time.Minute, delta: time.Hour, absolute: true, want: dur - SeekEndMargin},
		{name: "absolute before the start", pos: time.Minute, delta: -time.Second, absolute: true, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, config.AudioShared, "a", "b")
			h.setSeekInterval(time.Nanosecond)
			e := h.playing(dur, tt.pos)
			var got time.Duration
			var err error
			if tt.absolute {
				got, err = h.p.SeekTo(ctx(), h.handle(0), tt.delta)
			} else {
				got, err = h.p.SeekBy(ctx(), tt.delta)
			}
			if err != nil || got != tt.want {
				t.Fatalf("seek = %v, %v; want %v", got, err, tt.want)
			}
			if call := fmt.Sprintf("seek %d@%s", h.handle(0), tt.want); !e.has(call) {
				t.Errorf("calls %v, want %q", e.callList(), call)
			}
			if s := h.p.Status(); s.Position != tt.want || s.Track.ID != "a" || s.State != Playing {
				t.Errorf("status = %+v", s)
			}
		})
	}
}

func TestSeekShowsTheTargetAtOnce(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.setSeekInterval(time.Nanosecond)
	e := h.playing(5*time.Minute, time.Minute)
	k := uint64(h.handle(0))
	before := h.p.Status().SeekID
	if _, err := h.p.SeekBy(ctx(), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if s := h.p.Status(); s.Position != 90*time.Second || s.SeekID != before+1 {
		t.Errorf("after seek: position %v seek id %d", s.Position, s.SeekID)
	}
	// A sample taken before the seek landed does not move the bar back.
	h.send(e, Event{Kind: EventProgress, Key: k, Position: 61 * time.Second, Duration: 5 * time.Minute})
	if s := h.p.Status(); s.Position != 90*time.Second {
		t.Errorf("old sample moved the position to %v", s.Position)
	}
	h.send(e, Event{Kind: EventProgress, Key: k, Position: 91 * time.Second, Duration: 5 * time.Minute, Seeks: 1})
	if s := h.p.Status(); s.Position != 91*time.Second {
		t.Errorf("new sample: position %v", s.Position)
	}
}

func TestSeeksAreCombined(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.setSeekInterval(100 * time.Millisecond)
	e := h.playing(5*time.Minute, time.Minute)
	k := uint64(h.handle(0))
	for range 4 {
		if _, err := h.p.SeekBy(ctx(), 10*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if s := h.p.Status(); s.Position != 100*time.Second {
		t.Errorf("position = %v, want 1:40", s.Position)
	}
	// The first seek goes out at once; the rest wait and are combined.
	if n := e.count("seek"); n != 1 {
		t.Errorf("seeks sent at once = %d, want 1", n)
	}
	// While a seek waits, every sample is older than it.
	h.send(e, Event{Kind: EventProgress, Key: k, Position: 71 * time.Second, Duration: 5 * time.Minute, Seeks: 1})
	if s := h.p.Status(); s.Position != 100*time.Second {
		t.Errorf("sample during a waiting seek moved the position to %v", s.Position)
	}
	waitFor(t, func() bool { return e.count("seek") == 2 })
	if want := fmt.Sprintf("seek %d@1m40s", k); !e.has(want) {
		t.Errorf("calls %v, want %q", e.callList(), want)
	}
	time.Sleep(150 * time.Millisecond)
	if n := e.count("seek"); n != 2 {
		t.Errorf("seeks = %d, want 2", n)
	}
}

func TestSeekWhilePausedStaysPaused(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.setSeekInterval(time.Nanosecond)
	e := h.playing(5*time.Minute, time.Minute)
	h.p.TogglePause(ctx())
	if _, err := h.p.SeekBy(ctx(), -10*time.Second); err != nil {
		t.Fatal(err)
	}
	if s := h.p.Status(); s.State != Paused || s.Position != 50*time.Second {
		t.Errorf("status = %+v", s)
	}
	if e.has("pause false") || e.count("seek") != 1 {
		t.Errorf("calls = %v", e.callList())
	}
}

func TestWaitingSeekIsDroppedWhenTheTrackChanges(t *testing.T) {
	for _, change := range []string{"next", "stop", "engine failed"} {
		t.Run(change, func(t *testing.T) {
			h := newHarness(t, config.AudioShared, "a", "b")
			h.setSeekInterval(80 * time.Millisecond)
			e := h.playing(5*time.Minute, time.Minute)
			h.p.SeekBy(ctx(), 5*time.Second) // sent
			h.p.SeekBy(ctx(), 5*time.Second) // waits
			switch change {
			case "next":
				h.p.Next(ctx())
			case "stop":
				h.p.Stop(ctx())
			case "engine failed":
				h.send(e, Event{Kind: EventFailed, Err: ErrEngineFailed})
			}
			time.Sleep(150 * time.Millisecond)
			if n := e.count("seek"); n != 1 {
				t.Errorf("seeks = %d, want 1: %v", n, e.callList())
			}
		})
	}
}

func TestWaitingSeekIsDroppedOnGaplessChange(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a", "b")
	h.setSeekInterval(80 * time.Millisecond)
	e := h.playing(30*time.Second, 0)
	ha, hb := h.handle(0), h.handle(1)
	h.send(e, Event{Kind: EventProgress, Key: uint64(ha), Position: 20 * time.Second, Duration: 30 * time.Second})
	waitFor(t, func() bool { return e.count("preload") == 1 })
	h.p.SeekBy(ctx(), time.Second) // sent
	h.p.SeekBy(ctx(), time.Second) // waits
	h.send(e, Event{Kind: EventEnded, Key: uint64(ha), Reason: EndEOF})
	h.send(e, Event{Kind: EventStarted, Key: uint64(hb)})
	time.Sleep(150 * time.Millisecond)
	if n := e.count("seek"); n != 1 {
		t.Errorf("seeks = %d, want 1: %v", n, e.callList())
	}
}

func TestSeekInAPreview(t *testing.T) {
	h := newHarness(t, config.AudioShared)
	h.q.Set([]Track{{ID: "a", Title: "Song a", Duration: 5 * time.Minute}}, 0)
	h.res.preview = true
	h.setSeekInterval(time.Nanosecond)
	h.p.Play(ctx())
	e := h.engine()
	waitFor(t, func() bool { return e.count("play") == 1 })
	h.send(e, Event{Kind: EventStarted, Key: uint64(h.handle(0))})
	// The 30 s clip, not the full track, bounds the seek.
	got, err := h.p.SeekBy(ctx(), time.Minute)
	if want := 30*time.Second - SeekEndMargin; err != nil || got != want {
		t.Errorf("seek = %v, %v; want %v", got, err, want)
	}
}

func TestSeekEngineError(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.setSeekInterval(time.Nanosecond)
	e := h.playing(5*time.Minute, time.Minute)
	e.setFail("seek", errors.New("ipc broke"))
	got, err := h.p.SeekBy(ctx(), 5*time.Second)
	if err != nil || got != 65*time.Second {
		t.Errorf("seek = %v, %v", got, err)
	}
	// The seek did not happen, so the next sample is current.
	h.send(e, Event{Kind: EventProgress, Key: uint64(h.handle(0)), Position: 62 * time.Second, Duration: 5 * time.Minute})
	if s := h.p.Status(); s.Position != 62*time.Second {
		t.Errorf("position = %v", s.Position)
	}
}

func TestSeekCountRestartsWithANewEngine(t *testing.T) {
	h := newHarness(t, config.AudioShared, "a")
	h.setSeekInterval(time.Nanosecond)
	e := h.playing(5*time.Minute, time.Minute)
	h.p.SeekBy(ctx(), 5*time.Second)
	h.send(e, Event{Kind: EventFailed, Err: ErrEngineFailed})
	h.p.Play(ctx())
	waitFor(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.engines) == 2
	})
	e2 := h.engine()
	waitFor(t, func() bool { return e2.count("play") == 1 })
	k := uint64(h.handle(0))
	h.send(e2, Event{Kind: EventStarted, Key: k})
	h.send(e2, Event{Kind: EventProgress, Key: k, Position: 2 * time.Second, Duration: 5 * time.Minute})
	if s := h.p.Status(); s.Position != 2*time.Second {
		t.Errorf("position on the new engine = %v", s.Position)
	}
}

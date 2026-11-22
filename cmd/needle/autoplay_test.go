package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tidal"
	"github.com/five07/needle/internal/tui"
)

// fakeRadio serves one radio for every track.
type fakeRadio struct {
	mu       sync.Mutex
	tracks   []tidal.Track
	radioErr error
	pageErr  error
	gate     chan struct{} // when set, TrackRadio waits for it (or ctx)
	seeds    []string
}

func (f *fakeRadio) TrackRadio(ctx context.Context, id string) (tidal.Playlist, error) {
	f.mu.Lock()
	f.seeds = append(f.seeds, id)
	gate, err := f.gate, f.radioErr
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return tidal.Playlist{}, ctx.Err()
		}
	}
	return tidal.Playlist{ID: "radio-" + id, Mix: true}, err
}

func (f *fakeRadio) PlaylistTracks(_ context.Context, id, next string) (tidal.Page[tidal.Track], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(id, "radio-") || next != "" {
		return tidal.Page[tidal.Track]{}, fmt.Errorf("unexpected page %q %q", id, next)
	}
	return tidal.Page[tidal.Track]{Items: f.tracks}, f.pageErr
}

func (f *fakeRadio) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seeds...)
}

type autoEnv struct {
	q   *queue.Queue[player.Track]
	tr  *stubTransport
	b   *playback
	src *fakeRadio
	a   *autoplay
}

// newAutoEnv has queue 1, 2 with 2 (the last) current and playing, and
// autoplay on.
func newAutoEnv(t *testing.T) *autoEnv {
	t.Helper()
	q := newTestQueue()
	q.Set([]player.Track{track("1"), track("2")}, 1)
	tr := &stubTransport{state: player.Playing}
	b := &playback{q: q, p: tr, changed: make(chan struct{}, 1)}
	b.autoplay.Store(true)
	src := &fakeRadio{tracks: []tidal.Track{tt("1"), tt("10"), tt("2"), tt("11"), tt("10")}}
	return &autoEnv{q: q, tr: tr, b: b, src: src, a: newAutoplay(src, b, discardLogger())}
}

func (e *autoEnv) check() { e.a.check(context.Background(), e.tr.Status(), e.q.Snapshot()) }

// result waits for the fetch started by check.
func (e *autoEnv) result(t *testing.T) radioResult {
	t.Helper()
	select {
	case r := <-e.a.done():
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("no radio result")
		return radioResult{}
	}
}

func idsOf(q *queue.Queue[player.Track]) []string {
	var out []string
	s := q.Snapshot()
	for _, e := range append(append(s.History, *s.Current), s.Upcoming...) {
		id := e.Item.ID
		if e.Item.Autoplay {
			id = "~" + id
		}
		out = append(out, id)
	}
	return out
}

func TestAutoplayAddsRadio(t *testing.T) {
	e := newAutoEnv(t)
	e.check()
	r := e.result(t)
	if msg := e.a.apply(context.Background(), r); msg != "" {
		t.Errorf("notice %q", msg)
	}
	// Tracks already queued, and repeats within the radio, are left out.
	if got, want := idsOf(e.q), []string{"1", "2", "~10", "~11"}; !reflect.DeepEqual(got, want) {
		t.Errorf("queue %v, want %v", got, want)
	}
	if !reflect.DeepEqual(e.src.calls(), []string{"2"}) || e.tr.changes != 1 || e.tr.nexts != 0 {
		t.Errorf("seeds %v changes %d nexts %d", e.src.calls(), e.tr.changes, e.tr.nexts)
	}
	select {
	case <-e.b.changed:
	default:
		t.Error("no snapshot signaled")
	}
	// Upcoming tracks now: nothing more is fetched until the last one.
	e.check()
	if len(e.src.calls()) != 1 {
		t.Error("fetched with tracks still to come")
	}
	e.q.Next()
	e.q.Next() // ~11 is last
	e.check()
	e.result(t)
	if got := e.src.calls(); !reflect.DeepEqual(got, []string{"2", "11"}) {
		t.Errorf("seeds %v: the radio does not drift from the last track", got)
	}
}

func TestAutoplayCheckConditions(t *testing.T) {
	tests := map[string]func(e *autoEnv){
		"off":                  func(e *autoEnv) { e.b.autoplay.Store(false) },
		"repeat all":           func(e *autoEnv) { e.q.SetRepeat(queue.RepeatAll) },
		"repeat one":           func(e *autoEnv) { e.q.SetRepeat(queue.RepeatOne) },
		"tracks to come":       func(e *autoEnv) { e.q.Jump(e.q.Snapshot().History[0].Handle) },
		"stopped by the user":  func(e *autoEnv) { e.tr.state = player.Stopped },
		"empty queue":          func(e *autoEnv) { e.q.Set(nil, 0) },
		"a fetch already runs": func(e *autoEnv) { e.a.busy = true },
	}
	for name, setup := range tests {
		e := newAutoEnv(t)
		setup(e)
		e.check()
		// check records a fetch before starting it, so this is not racy.
		if e.a.seed != 0 {
			t.Errorf("%s: fetched", name)
		}
	}
	// Loading counts as playing on; running out counts too.
	for _, st := range []player.Status{{State: player.Loading}, {State: player.Stopped, EndOfQueue: true}} {
		e := newAutoEnv(t)
		e.a.check(context.Background(), st, e.q.Snapshot())
		e.result(t)
		if len(e.src.calls()) != 1 {
			t.Errorf("%+v: not fetched", st)
		}
	}
}

// When playback already ran out on the seed, the first new track plays.
func TestAutoplayContinuesAfterTheEnd(t *testing.T) {
	e := newAutoEnv(t)
	e.check()
	r := e.result(t)
	e.tr.state, e.tr.end = player.Stopped, true
	e.a.apply(context.Background(), r)
	if e.tr.nexts != 1 {
		t.Errorf("nexts %d", e.tr.nexts)
	}
	// Not when the user stopped it, or when another track is current.
	for name, setup := range map[string]func(e *autoEnv){
		"stopped":     func(e *autoEnv) { e.tr.state, e.tr.end = player.Stopped, false },
		"moved on":    func(e *autoEnv) { e.tr.state, e.tr.end = player.Stopped, true; e.q.Previous() },
		"still plays": func(e *autoEnv) {},
	} {
		e := newAutoEnv(t)
		e.check()
		r := e.result(t)
		setup(e)
		e.a.apply(context.Background(), r)
		if e.tr.nexts != 0 {
			t.Errorf("%s: nexts %d", name, e.tr.nexts)
		}
	}
}

func TestAutoplayFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *autoEnv)
		want  string
	}{
		{"no radio", func(e *autoEnv) { e.src.radioErr = fmt.Errorf("x: %w", tidal.ErrNoRadio) }, `Autoplay: TIDAL has no radio for "Song 2".`},
		{"radio request fails", func(e *autoEnv) { e.src.radioErr = errors.New("offline") }, `Autoplay could not load radio for "Song 2".`},
		{"tracks request fails", func(e *autoEnv) { e.src.pageErr = errors.New("offline") }, `Autoplay could not load radio for "Song 2".`},
		{"nothing new", func(e *autoEnv) { e.src.tracks = []tidal.Track{tt("1"), tt("2")} }, `Autoplay found no new tracks for "Song 2".`},
		{"canceled", func(e *autoEnv) { e.src.radioErr = context.Canceled }, ""},
	}
	for _, tt := range tests {
		e := newAutoEnv(t)
		tt.setup(e)
		e.check()
		if got := e.a.apply(context.Background(), e.result(t)); got != tt.want {
			t.Errorf("%s: notice %q, want %q", tt.name, got, tt.want)
		}
		if got := idsOf(e.q); !reflect.DeepEqual(got, []string{"1", "2"}) {
			t.Errorf("%s: queue %v", tt.name, got)
		}
		// A failure is not retried for the same last track...
		e.check()
		if e.a.busy {
			t.Errorf("%s: retried", tt.name)
		}
	}
	// ...until the switch is used again.
	e := newAutoEnv(t)
	e.src.radioErr = errors.New("offline")
	e.check()
	e.a.apply(context.Background(), e.result(t))
	e.b.SetAutoplay(context.Background(), false)
	e.b.SetAutoplay(context.Background(), true)
	e.check()
	e.result(t)
	if n := len(e.src.calls()); n != 2 {
		t.Errorf("after toggling: %d fetches", n)
	}
}

// A radio fetched before the switch changed is dropped.
func TestAutoplaySwitchedDuringFetch(t *testing.T) {
	for name, flip := range map[string]func(b *playback){
		"off": func(b *playback) { b.SetAutoplay(context.Background(), false) },
		"off and on": func(b *playback) {
			b.SetAutoplay(context.Background(), false)
			b.SetAutoplay(context.Background(), true)
		},
	} {
		e := newAutoEnv(t)
		e.check()
		r := e.result(t)
		flip(e.b)
		if msg := e.a.apply(context.Background(), r); msg != "" || len(idsOf(e.q)) != 2 {
			t.Errorf("%s: notice %q queue %v", name, msg, idsOf(e.q))
		}
		if e.a.busy {
			t.Errorf("%s: still busy", name)
		}
	}
}

func TestAutoplayFetchTimesOut(t *testing.T) {
	e := newAutoEnv(t)
	e.a.timeout = 10 * time.Millisecond
	e.src.gate = make(chan struct{}) // never opens
	e.check()
	if r := e.result(t); !errors.Is(r.err, context.DeadlineExceeded) {
		t.Errorf("err %v", r.err)
	}
	// A fetch still running when the app quits gives up instead of
	// waiting to deliver. With the result slot taken, quitting is the only
	// way out, so a result arriving after the slot is freed means it
	// waited.
	e2 := newAutoEnv(t)
	e2.src.gate = make(chan struct{})
	e2.a.results <- radioResult{}
	ctx, cancel := context.WithCancel(context.Background())
	e2.a.check(ctx, e2.tr.Status(), e2.q.Snapshot())
	cancel()
	time.Sleep(20 * time.Millisecond)
	<-e2.a.results
	select {
	case <-e2.a.results:
		t.Error("a canceled fetch delivered a result")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSetAutoplay(t *testing.T) {
	ctx := context.Background()
	e := newAutoEnv(t)
	e.check()
	e.a.apply(ctx, e.result(t))
	e.b.Add(ctx, []tidal.Track{tt("20")}) // ahead of the autoplayed tracks (decision 66)
	if got, want := idsOf(e.q), []string{"1", "2", "20", "~10", "~11"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after add %v, want %v", got, want)
	}
	changes := e.tr.changes
	// Off removes the upcoming autoplayed tracks; yours stay (decision 65).
	if err := e.b.SetAutoplay(ctx, false); err != nil || e.b.Autoplay() {
		t.Fatal(err)
	}
	if got, want := idsOf(e.q), []string{"1", "2", "20"}; !reflect.DeepEqual(got, want) || e.tr.changes != changes+1 {
		t.Errorf("after off %v (changes %d)", got, e.tr.changes-changes)
	}
	// Nothing to remove: the player is not bothered.
	e.b.SetAutoplay(ctx, false)
	e.b.SetAutoplay(ctx, true)
	if e.tr.changes != changes+1 || !e.b.Autoplay() {
		t.Errorf("changes %d", e.tr.changes-changes)
	}
}

func TestNowPlayingShowsAutoplay(t *testing.T) {
	e := newAutoEnv(t)
	e.check()
	e.a.apply(context.Background(), e.result(t))
	src := &stubStatus{updates: make(chan struct{}), status: &player.Status{State: player.Playing}}
	status := make(chan tui.NowPlaying, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchStatus(ctx, src, watch{notices: make(chan string, 1), status: status, q: e.q, auto: e.a})
		close(done)
	}()
	src.set("")
	np := <-status
	cancel()
	<-done
	if !np.Autoplay || len(np.Upcoming) != 2 || !np.Upcoming[0].Track.Autoplay {
		t.Errorf("np %+v", np)
	}
	if s := mprisState(tui.NowPlaying{Current: np.Current, Autoplay: true}, 100); !s.CanNext {
		t.Error("MPRIS: next is possible with autoplay on")
	}
}

// The watcher starts fetches and turns their failures into notices.
func TestWatchStatusRunsAutoplay(t *testing.T) {
	e := newAutoEnv(t)
	e.src.radioErr = fmt.Errorf("x: %w", tidal.ErrNoRadio)
	src := &stubStatus{updates: make(chan struct{}), status: &player.Status{State: player.Playing}}
	notices := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchStatus(ctx, src, watch{notices: notices, q: e.q, auto: e.a}); close(done) }()
	src.set("")
	select {
	case got := <-notices:
		if !strings.Contains(got, "no radio") {
			t.Errorf("notice %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Error("no notice")
	}
	cancel()
	<-done
}

// Player errors while continuing are logged; the radio stays queued.
func TestAutoplayPlayerErrors(t *testing.T) {
	e := newAutoEnv(t)
	logger, logs := bufLogger()
	e.a.logger = logger
	e.check()
	r := e.result(t)
	e.tr.state, e.tr.end, e.tr.err = player.Stopped, true, errors.New("engine gone")
	if msg := e.a.apply(context.Background(), r); msg != "" {
		t.Errorf("notice %q", msg)
	}
	if !strings.Contains(logs.String(), "autoplay: queue update failed") || !strings.Contains(logs.String(), "autoplay: next failed") {
		t.Errorf("log: %s", logs)
	}
	if got := idsOf(e.q); len(got) != 4 {
		t.Errorf("queue %v", got)
	}
}

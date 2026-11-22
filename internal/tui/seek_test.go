package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeSeeker records seeks and moves a position within 0 and dur.
type fakeSeeker struct {
	deltas []time.Duration
	pos    time.Duration
	dur    time.Duration
	err    error
}

func (f *fakeSeeker) SeekBy(_ context.Context, d time.Duration) (time.Duration, error) {
	f.deltas = append(f.deltas, d)
	if f.err != nil {
		return 0, f.err
	}
	f.pos = min(max(f.pos+d, 0), f.dur-3*time.Second)
	return f.pos, nil
}

// seekModel returns a model playing a 5:20 track at 1:42 with a fake
// seeker and a clock the test moves.
func seekModel(t *testing.T) (*Model, *fakeSeeker, *time.Time) {
	t.Helper()
	e := newEnv(t)
	sk := &fakeSeeker{pos: 102 * time.Second, dur: 320 * time.Second}
	e.svc.Seek = sk
	m := e.model(80, 24)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	drive(m, NowPlayingMsg{playingNow()})
	return m, sk, &now
}

// TASK-019 decision 90: the step grows within a run and resets after a
// pause or a change of direction.
func TestSeekRunStep(t *testing.T) {
	t0 := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	type press struct {
		dir int
		at  time.Duration
	}
	tests := []struct {
		name    string
		presses []press
		want    []time.Duration
	}{
		{"held key grows to 30 s", []press{{1, 0}, {1, 40 * time.Millisecond}, {1, 80 * time.Millisecond}, {1, 120 * time.Millisecond}, {1, 160 * time.Millisecond}, {1, 200 * time.Millisecond}},
			[]time.Duration{5, 5, 10, 10, 30, 30}},
		{"pause restarts", []press{{1, 0}, {1, 100 * time.Millisecond}, {1, 200 * time.Millisecond}, {1, 600 * time.Millisecond}},
			[]time.Duration{5, 5, 10, 5}},
		{"just under the window", []press{{-1, 0}, {-1, 399 * time.Millisecond}, {-1, 798 * time.Millisecond}},
			[]time.Duration{5, 5, 10}},
		{"direction change restarts", []press{{1, 0}, {1, 50 * time.Millisecond}, {1, 100 * time.Millisecond}, {-1, 150 * time.Millisecond}},
			[]time.Duration{5, 5, 10, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r seekRun
			var got []time.Duration
			for _, p := range tt.presses {
				got = append(got, r.step(p.dir, t0.Add(p.at))/time.Second)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("steps %v, want %v", got, tt.want)
			}
		})
	}
}

// TASK-019 AC #1, #2, #4: , and . seek from any view, the step grows, and
// the bar shows the new position at once.
func TestSeekKeys(t *testing.T) {
	m, sk, now := seekModel(t)
	var notices []string
	for range 5 {
		notices = append(notices, drive(m, press("."))...)
		*now = now.Add(50 * time.Millisecond)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second, 30 * time.Second}
	if !reflect.DeepEqual(sk.deltas, want) {
		t.Errorf("deltas %v, want %v", sk.deltas, want)
	}
	wantN := []string{"» 5 s · 1:47", "» 5 s · 1:52", "» 10 s · 2:02", "» 10 s · 2:12", "» 30 s · 2:42"}
	if !reflect.DeepEqual(notices, wantN) {
		t.Errorf("notices %q, want %q", notices, wantN)
	}
	if got := m.svc.playing.Position; got != 162*time.Second {
		t.Errorf("bar position %v, want 2:42", got)
	}
	if bar := strings.Join(barText(m), "\n"); !strings.Contains(bar, "2:42") {
		t.Errorf("bar does not show 2:42:\n%s", bar)
	}
	// Back, after a pause, from another pane.
	*now = now.Add(time.Second)
	if n := drive(m, press("l"), press(",")); !reflect.DeepEqual(n, []string{"« 5 s · 2:37"}) {
		t.Errorf("back notices %q", n)
	}
}

// TASK-019 decision 93: the keys do nothing while loading, stopped, or
// without a player.
func TestSeekKeysNeedAPlayingTrack(t *testing.T) {
	for _, st := range []PlayState{Stopped, Loading} {
		m, sk, _ := seekModel(t)
		np := playingNow()
		np.State = st
		drive(m, NowPlayingMsg{np})
		if n := drive(m, press("."), press(",")); len(n) != 0 || len(sk.deltas) != 0 {
			t.Errorf("state %v: notices %q deltas %v", st, n, sk.deltas)
		}
	}
	m, sk, _ := seekModel(t)
	np := playingNow()
	np.State = Paused
	drive(m, NowPlayingMsg{np})
	if n := drive(m, press(".")); len(n) != 1 || len(sk.deltas) != 1 {
		t.Errorf("paused: notices %q deltas %v", n, sk.deltas)
	}
	// The player found nothing to seek in after all: silent.
	sk.err = ErrNotSeekable
	if n := drive(m, press(".")); len(n) != 0 {
		t.Errorf("not seekable: notices %q", n)
	}
	// Other failures show a short notice without details.
	sk.err = errors.New("ipc closed")
	if n := drive(m, press(".")); len(n) != 1 || !strings.HasPrefix(n[0], "Seek: ") || strings.Contains(n[0], "ipc") {
		t.Errorf("failure notices %q", n)
	}
	m.svc.Seek = nil
	if n := drive(m, press(".")); len(n) != 0 {
		t.Errorf("without a seeker: %q", n)
	}
}

// The seek keys type into the search input instead.
func TestSeekKeysWhileTyping(t *testing.T) {
	m, sk, _ := seekModel(t)
	typeQuery(m, "a,b.c")
	if len(sk.deltas) != 0 {
		t.Errorf("deltas %v while typing", sk.deltas)
	}
	if !strings.Contains(screen(m), "a,b.c") {
		t.Errorf("query not typed:\n%s", screen(m))
	}
}

// An answer that arrives after a newer one does not move the bar back.
func TestLateSeekAnswerIsIgnored(t *testing.T) {
	m, _, _ := seekModel(t)
	drive(m, seekedMsg{seq: 2, dir: 1, step: 5 * time.Second, pos: 112 * time.Second})
	if n := drive(m, seekedMsg{seq: 1, dir: 1, step: 5 * time.Second, pos: 107 * time.Second}); len(n) != 0 {
		t.Errorf("late answer shown: %q", n)
	}
	if got := m.svc.playing.Position; got != 112*time.Second {
		t.Errorf("position %v, want 1:52", got)
	}
}

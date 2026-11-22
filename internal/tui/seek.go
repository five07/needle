package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Seeker moves the playing track (decisions 89 to 93).
type Seeker interface {
	// SeekBy moves the playing or paused track by delta (negative is
	// back) and returns the new position. It returns ErrNotSeekable when
	// nothing is playing or the track is still loading.
	SeekBy(ctx context.Context, delta time.Duration) (time.Duration, error)
}

// ErrNotSeekable means there is no track to seek in; the seek keys then do
// nothing (decision 93).
var ErrNotSeekable = errors.New("tui: nothing to seek in")

// SeekRunWindow is how close together seek key presses must be to form a
// run, in which the step grows (decision 90).
const SeekRunWindow = 400 * time.Millisecond

// seekSteps is the step for each press of a run: presses 1 and 2 move 5 s,
// 3 and 4 move 10 s, and every later one 30 s (decision 90).
var seekSteps = []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second, 30 * time.Second}

// seekRun tracks a run of seek key presses. Only the UI goroutine
// touches it.
type seekRun struct {
	dir   int // -1 back, 1 forward
	n     int // presses in the run so far
	last  time.Time
	seq   uint64 // numbers each seek, so a late answer does not win
	shown uint64 // seq of the newest answer applied
}

// step returns how far a press in direction dir at now moves. A pause of
// SeekRunWindow or more, or a change of direction, starts a new run.
func (r *seekRun) step(dir int, now time.Time) time.Duration {
	if dir != r.dir || r.last.IsZero() || now.Sub(r.last) >= SeekRunWindow {
		r.dir, r.n = dir, 0
	}
	r.last = now
	r.n++
	return seekSteps[min(r.n, len(seekSteps))-1]
}

// seekedMsg reports where a seek landed.
type seekedMsg struct {
	seq  uint64
	dir  int
	step time.Duration
	pos  time.Duration
}

// seek handles a seek key: dir is -1 for back and 1 for forward. It does
// nothing without a seeker or a playing or paused track (decision 93).
func (m *Model) seek(dir int) tea.Cmd {
	sk, st := m.svc.Seek, m.svc.playing.State
	if sk == nil || (st != Playing && st != Paused) {
		return nil
	}
	step := m.seekRun.step(dir, m.now())
	m.seekRun.seq++
	seq, ctx, delta := m.seekRun.seq, m.svc.ctx(), time.Duration(dir)*step
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()
		pos, err := sk.SeekBy(ctx, delta)
		switch {
		case errors.Is(err, ErrNotSeekable):
			return nil
		case err != nil:
			m.svc.logger().Warn("seek failed", "err", err)
			return NoticeMsg{Text: "Seek: " + userMessage(err)}
		}
		return seekedMsg{seq: seq, dir: dir, step: step, pos: pos}
	}
}

// onSeeked moves the bar to the new position at once (AC #4) and shows
// the step and the position, for example "» 10 s · 1:42" (decision 90).
func (m *Model) onSeeked(msg seekedMsg) tea.Cmd {
	if msg.seq < m.seekRun.shown {
		return nil // a newer seek already answered
	}
	m.seekRun.shown = msg.seq
	m.svc.playing.Position = msg.pos
	arrow := "»"
	if msg.dir < 0 {
		arrow = "«"
	}
	return Notify(fmt.Sprintf("%s %d s · %s", arrow, int(msg.step/time.Second), barClock(msg.pos)))
}

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/player"
	"github.com/five07/tidal-tui/internal/queue"
	"github.com/five07/tidal-tui/internal/tidal"
	"github.com/five07/tidal-tui/internal/tui"
)

// transport is the part of *player.Player that playback drives.
type transport interface {
	Play(ctx context.Context) error
	QueueChanged(ctx context.Context) error
	TogglePause(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
}

// playback implements tui.Playback on the play queue and the player.
type playback struct {
	q *queue.Queue[player.Track]
	p transport
	// changed, when set (capacity 1), is signaled after each queue edit
	// so watchStatus sends a new snapshot.
	changed chan struct{}
}

// edited signals a queue change without blocking.
func (b *playback) edited() {
	select {
	case b.changed <- struct{}{}:
	default: // a signal is already pending, or nobody listens
	}
}

func toTrack(t tidal.Track) player.Track {
	return player.Track{
		ID:       t.ID,
		Title:    t.FullTitle(),
		Artists:  strings.Join(t.Artists, ", "),
		Album:    t.Album,
		Duration: t.Duration,
	}
}

func toTracks(ts []tidal.Track) []player.Track {
	out := make([]player.Track, len(ts))
	for i, t := range ts {
		out[i] = toTrack(t)
	}
	return out
}

// PlayFrom replaces the queue with tracks and plays tracks[start].
func (b *playback) PlayFrom(ctx context.Context, tracks []tidal.Track, start int) error {
	if !b.q.Set(toTracks(tracks), start) {
		return fmt.Errorf("play: start %d is outside %d tracks", start, len(tracks))
	}
	b.edited()
	return b.p.Play(ctx)
}

// PlayNext inserts tracks, in order, after the current track. On an empty
// queue the first one starts playing.
func (b *playback) PlayNext(ctx context.Context, tracks []tidal.Track) error {
	if _, ok := b.q.Current(); !ok {
		return b.Add(ctx, tracks)
	}
	for i := len(tracks) - 1; i >= 0; i-- {
		b.q.PlayNext(toTrack(tracks[i]))
	}
	b.edited()
	return b.p.QueueChanged(ctx)
}

// Add appends tracks to the queue. On an empty queue the first one starts
// playing.
func (b *playback) Add(ctx context.Context, tracks []tidal.Track) error {
	_, had := b.q.Current()
	for _, t := range tracks {
		b.q.Add(toTrack(t))
	}
	b.edited()
	if !had && len(tracks) > 0 {
		return b.p.Play(ctx)
	}
	return b.p.QueueChanged(ctx)
}

// TogglePause implements tui.Transport.
func (b *playback) TogglePause(ctx context.Context) error { return b.p.TogglePause(ctx) }

// Next implements tui.Transport.
func (b *playback) Next(ctx context.Context) error { return b.p.Next(ctx) }

// Previous implements tui.Transport.
func (b *playback) Previous(ctx context.Context) error { return b.p.Previous(ctx) }

// SetShuffle implements tui.Transport.
func (b *playback) SetShuffle(ctx context.Context, on bool) error {
	b.q.SetShuffle(on)
	b.edited()
	return b.p.QueueChanged(ctx)
}

// CycleRepeat implements tui.Transport.
func (b *playback) CycleRepeat(ctx context.Context) error {
	b.q.CycleRepeat()
	b.edited()
	return b.p.QueueChanged(ctx)
}

// Jump implements tui.Transport.
func (b *playback) Jump(ctx context.Context, h uint64) error {
	if _, ok := b.q.Jump(queue.Handle(h)); !ok {
		return errNotQueued
	}
	b.edited()
	return b.p.Play(ctx)
}

// Remove implements tui.Transport.
func (b *playback) Remove(ctx context.Context, h uint64) error {
	if !b.q.Remove(queue.Handle(h)) {
		return errNotQueued
	}
	b.edited()
	return b.p.QueueChanged(ctx)
}

// Clear implements tui.Transport.
func (b *playback) Clear(ctx context.Context) error {
	b.q.Clear()
	b.edited()
	return b.p.QueueChanged(ctx)
}

// errNotQueued means a queue entry was removed or played before the
// command reached it.
var errNotQueued = errors.New("track is no longer in the queue")

// statusSource is the part of *player.Player that watchStatus reads.
type statusSource interface {
	Updates() <-chan struct{}
	Status() player.Status
}

// watch says where watchStatus sends what it reads.
type watch struct {
	// notices receives each new player message (such as "mpv was not
	// found").
	notices chan<- string
	// status, when set, receives a snapshot of the player and q after
	// every change, the newest replacing one not yet taken.
	status  chan tui.NowPlaying // capacity 1
	q       *queue.Queue[player.Track]
	changed <-chan struct{} // signaled after queue edits
	mode    config.AudioMode
	// best, when set, records the best quality received (decision 52).
	best *bestQuality
	now  func() time.Time // default time.Now
}

// watchStatus forwards player changes to the UI until ctx ends. It is the
// only reader of p.Updates, which coalesces signals.
func watchStatus(ctx context.Context, p statusSource, w watch) {
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.Updates():
		case <-w.changed:
		}
		st := p.Status()
		if w.status != nil {
			np := nowPlaying(st, w.q.Snapshot(), w.mode)
			if w.best != nil {
				now := time.Now
				if w.now != nil {
					now = w.now
				}
				w.best.offer(st, now())
				np.Best, np.BestSeen = w.best.label(), w.best.seen
			}
			sendLatest(w.status, np)
		}
		if msg := st.Message; msg != "" && msg != last {
			select {
			case w.notices <- msg:
			case <-ctx.Done():
				return
			}
		}
		last = st.Message
	}
}

// sendLatest puts v on ch (capacity 1, one sender), replacing a value the
// reader has not taken yet, so a slow UI only sees the newest snapshot.
func sendLatest[T any](ch chan T, v T) {
	for {
		select {
		case ch <- v:
			return
		default:
		}
		select {
		case <-ch:
		default:
		}
	}
}

// nowPlaying converts a player status and a queue snapshot for the UI.
func nowPlaying(st player.Status, s queue.Snapshot[player.Track], mode config.AudioMode) tui.NowPlaying {
	np := tui.NowPlaying{
		State:    tui.PlayState(st.State),
		Position: st.Position,
		Duration: st.Duration,
		Mode:     string(mode),
		Preview:  st.Preview,
		Shuffle:  s.Shuffle,
		Repeat:   tui.RepeatMode(s.Repeat),
	}
	if s.Current != nil {
		c := queueItem(*s.Current)
		np.Current = &c
	}
	for _, e := range s.Upcoming {
		np.Upcoming = append(np.Upcoming, queueItem(e))
	}
	if st.Source != nil {
		np.Source = st.Source.Codec + " " + shortFormat(st.Source.BitDepth, st.Source.SampleRate)
	}
	if st.Output != nil && st.Output.Rate > 0 {
		np.Output = shortFormat(st.Output.Bits(), st.Output.Rate)
	}
	if st.Source != nil && st.Indicator.Reason != "" {
		np.Reason = st.Indicator.Reason
		switch st.Indicator.State {
		case player.BitPerfect:
			np.Quality = tui.QualityBitPerfect
		case player.RateMatched:
			np.Quality = tui.QualityMatched
		default:
			np.Quality = tui.QualityAltered
		}
	}
	return np
}

func queueItem(e queue.Entry[player.Track]) tui.QueueItem {
	t := e.Item
	return tui.QueueItem{Handle: uint64(e.Handle), Track: tui.QueueTrack{ID: t.ID, Title: t.Title, Artists: t.Artists, Album: t.Album, Duration: t.Duration}}
}

// shortFormat writes bit depth and rate the way the bar shows them:
// "24/96", "16/44.1", or "44.1 kHz" when the depth is unknown.
func shortFormat(bits, rate int) string {
	k := strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64)
	if bits <= 0 {
		return k + " kHz"
	}
	return strconv.Itoa(bits) + "/" + k
}

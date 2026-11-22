package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/mpris"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tidal"
	"github.com/five07/needle/internal/tui"
)

// transport is the part of *player.Player that playback drives.
type transport interface {
	Play(ctx context.Context) error
	QueueChanged(ctx context.Context) error
	TogglePause(ctx context.Context) error
	SetPaused(ctx context.Context, paused bool) error
	Stop(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
	Status() player.Status
}

// volumer is the volume part of *player.Player (decisions 82 and 87).
type volumer interface {
	AdjustVolume(ctx context.Context, delta float64) (float64, error)
	SetVolume(ctx context.Context, percent float64) error
}

// playback implements tui.Playback on the play queue and the player.
type playback struct {
	q *queue.Queue[player.Track]
	p transport
	// vol changes the software volume; nil means volume cannot change.
	vol volumer
	// changed, when set (capacity 1), is signaled after each queue edit
	// so watchStatus sends a new snapshot.
	changed chan struct{}
	// autoplay is the autoplay switch (decision 62); autoplayGen counts
	// its changes, so a radio fetched before one is not used.
	autoplay    atomic.Bool
	autoplayGen atomic.Uint64
}

func isAutoplayed(t player.Track) bool { return t.Autoplay }

// Autoplay reports whether autoplay is on.
func (b *playback) Autoplay() bool { return b.autoplay.Load() }

// SetAutoplay implements tui.Transport. Turning it off removes the upcoming
// autoplayed tracks; the current one finishes (decision 65).
func (b *playback) SetAutoplay(ctx context.Context, on bool) error {
	b.autoplay.Store(on)
	b.autoplayGen.Add(1)
	removed := 0
	if !on {
		removed = b.q.RemoveUpcoming(isAutoplayed)
	}
	b.edited()
	if removed > 0 {
		return b.p.QueueChanged(ctx)
	}
	return nil
}

// AdjustVolume implements tui.Volume.
func (b *playback) AdjustVolume(ctx context.Context, delta float64) (float64, error) {
	if b.vol == nil {
		return 0, tui.ErrVolumeOnDevice
	}
	level, err := b.vol.AdjustVolume(ctx, delta)
	if errors.Is(err, player.ErrVolumeDisabled) {
		return level, tui.ErrVolumeOnDevice
	}
	return level, err
}

// addAutoplayed appends radio tracks as autoplayed entries, leaving out
// tracks already in the queue, and returns how many it added.
func (b *playback) addAutoplayed(tracks []tidal.Track) int {
	s := b.q.Snapshot()
	have := map[string]bool{}
	for _, e := range s.History {
		have[e.Item.ID] = true
	}
	if s.Current != nil {
		have[s.Current.Item.ID] = true
	}
	for _, e := range s.Upcoming {
		have[e.Item.ID] = true
	}
	n := 0
	for _, t := range tracks {
		if have[t.ID] {
			continue
		}
		have[t.ID] = true
		pt := toTrack(t)
		pt.Autoplay = true
		b.q.Add(pt)
		n++
	}
	return n
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

// Add appends tracks to the queue, ahead of any upcoming autoplayed ones
// (decision 66). On an empty queue the first one starts playing.
func (b *playback) Add(ctx context.Context, tracks []tidal.Track) error {
	_, had := b.q.Current()
	for _, t := range tracks {
		b.q.AddBefore(toTrack(t), isAutoplayed)
	}
	b.edited()
	if !had && len(tracks) > 0 {
		return b.p.Play(ctx)
	}
	return b.p.QueueChanged(ctx)
}

// TogglePause implements tui.Transport.
// With nothing playing, it starts the current track (a restored queue
// waits stopped, decision 59).
func (b *playback) TogglePause(ctx context.Context) error {
	if b.p.Status().State == player.Stopped {
		return b.p.Play(ctx)
	}
	return b.p.TogglePause(ctx)
}

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

// SetRepeat sets the repeat mode (for MPRIS LoopStatus).
func (b *playback) SetRepeat(ctx context.Context, r queue.Repeat) error {
	b.q.SetRepeat(r)
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
	// mpris, when set, receives every change for desktop widgets.
	mpris interface{ Update(mpris.State) }
	// saved, when set, keeps the queue file up to date (decision 60).
	saved *savedQueue
	// auto, when set, continues the queue with radio (decision 63).
	auto *autoplay
	// vis, when set, is woken on every change so it follows playback.
	vis interface{ Wake() }
	now func() time.Time // default time.Now
}

// watchStatus forwards player changes to the UI until ctx ends. It is the
// only reader of p.Updates, which coalesces signals.
func watchStatus(ctx context.Context, p statusSource, w watch) {
	last := ""
	var lastNotice uint64
	for {
		select {
		case <-ctx.Done():
			if w.saved != nil {
				w.saved.save() // a change made just before quitting
			}
			return
		case <-p.Updates():
		case <-w.changed:
		case r := <-w.auto.done():
			if msg := w.auto.apply(ctx, r); msg != "" {
				select {
				case w.notices <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
		st := p.Status()
		if w.vis != nil {
			w.vis.Wake()
		}
		var snap queue.Snapshot[player.Track]
		if w.q != nil {
			snap = w.q.Snapshot()
			w.auto.check(ctx, st, snap)
		}
		if w.status != nil || w.mpris != nil {
			mode := w.mode
			if st.Mode != "" {
				mode = st.Mode // after a live switch (decision 79)
			}
			np := nowPlaying(st, snap, mode)
			np.Autoplay = w.auto != nil && w.auto.b.Autoplay()
			if w.mpris != nil {
				w.mpris.Update(mprisState(np, st.Volume))
			}
			if w.best != nil {
				now := time.Now
				if w.now != nil {
					now = w.now
				}
				w.best.offer(st, now())
				np.Best, np.BestSeen = w.best.label(), w.best.seen
			}
			if w.status != nil {
				sendLatest(w.status, np)
			}
		}
		if w.saved != nil {
			w.saved.save()
		}
		if msg := st.Message; msg != "" && msg != last {
			select {
			case w.notices <- msg:
			case <-ctx.Done():
				return
			}
		}
		last = st.Message
		if st.NoticeID != lastNotice && st.Notice != "" {
			select {
			case w.notices <- st.Notice:
			case <-ctx.Done():
				return
			}
		}
		lastNotice = st.NoticeID
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
	np.Volume, np.HasVolume = int(math.Round(st.Volume)), true
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
	return tui.QueueItem{Handle: uint64(e.Handle), Track: tui.QueueTrack{ID: t.ID, Title: t.Title, Artists: t.Artists, Album: t.Album,
		Duration: t.Duration, Autoplay: t.Autoplay}}
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

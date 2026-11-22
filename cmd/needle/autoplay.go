package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tidal"
)

// radioSource is the part of *tidal.Catalog autoplay uses.
type radioSource interface {
	TrackRadio(ctx context.Context, trackID string) (tidal.Playlist, error)
	PlaylistTracks(ctx context.Context, playlistID, next string) (tidal.Page[tidal.Track], error)
}

// autoplayTimeout bounds one radio fetch.
const autoplayTimeout = 30 * time.Second

// autoplay continues the queue with track radio when it runs out
// (decisions 62 to 66). check and apply run on the watchStatus goroutine;
// each fetch runs on its own, one at a time.
type autoplay struct {
	src     radioSource
	b       *playback
	logger  *slog.Logger
	results chan radioResult // capacity 1
	timeout time.Duration    // default autoplayTimeout

	busy bool
	// seed and gen record the last fetch started: the same last track,
	// with the switch untouched since, is not fetched again (a failure is
	// not retried).
	seed queue.Handle
	gen  uint64
}

type radioResult struct {
	seed   queue.Handle
	gen    uint64
	id     string // the seed track
	title  string
	tracks []tidal.Track
	err    error
}

func newAutoplay(src radioSource, b *playback, logger *slog.Logger) *autoplay {
	return &autoplay{src: src, b: b, logger: logger, results: make(chan radioResult, 1), timeout: autoplayTimeout}
}

// done is the channel fetch results arrive on; nil (never ready) without
// autoplay.
func (a *autoplay) done() <-chan radioResult {
	if a == nil {
		return nil
	}
	return a.results
}

// check starts a radio fetch when the last track of the queue is playing,
// or has just ended, with autoplay on and repeat off.
func (a *autoplay) check(ctx context.Context, st player.Status, s queue.Snapshot[player.Track]) {
	if a == nil || a.busy || !a.b.Autoplay() || s.Repeat != queue.RepeatOff || s.Current == nil || len(s.Upcoming) > 0 {
		return
	}
	// A restored or stopped queue waits; only playing on, or running out,
	// counts.
	if st.State == player.Stopped && !st.EndOfQueue {
		return
	}
	gen := a.b.autoplayGen.Load()
	if s.Current.Handle == a.seed && gen == a.gen {
		return
	}
	a.busy, a.seed, a.gen = true, s.Current.Handle, gen
	r := radioResult{seed: s.Current.Handle, gen: gen, id: s.Current.Item.ID, title: s.Current.Item.Title}
	go func() {
		fctx, cancel := context.WithTimeout(ctx, a.timeout)
		defer cancel()
		r.tracks, r.err = a.fetch(fctx, r.id)
		select {
		case a.results <- r:
		case <-ctx.Done():
		}
	}()
}

func (a *autoplay) fetch(ctx context.Context, trackID string) ([]tidal.Track, error) {
	radio, err := a.src.TrackRadio(ctx, trackID)
	if err != nil {
		return nil, err
	}
	page, err := a.src.PlaylistTracks(ctx, radio.ID, "")
	return page.Items, err
}

// apply adds a fetched radio to the queue and, if playback already ran
// out on the seed track, goes on to the first new track. It returns a
// notice for the user, or "".
func (a *autoplay) apply(ctx context.Context, r radioResult) string {
	a.busy = false
	if !a.b.Autoplay() || r.gen != a.b.autoplayGen.Load() {
		return "" // switched since the fetch started
	}
	if r.err != nil {
		if errors.Is(r.err, context.Canceled) {
			return ""
		}
		a.logger.Warn("autoplay radio failed", "track_id", r.id, "err", r.err)
		if errors.Is(r.err, tidal.ErrNoRadio) {
			return `Autoplay: TIDAL has no radio for "` + r.title + `".`
		}
		return `Autoplay could not load radio for "` + r.title + `".`
	}
	n := a.b.addAutoplayed(r.tracks)
	if n == 0 {
		a.logger.Info("autoplay radio had no new tracks", "track_id", r.id)
		return `Autoplay found no new tracks for "` + r.title + `".`
	}
	a.logger.Info("autoplay added radio", "track_id", r.id, "tracks", n)
	a.b.edited()
	if err := a.b.p.QueueChanged(ctx); err != nil {
		a.logger.Warn("autoplay: queue update failed", "err", err)
	}
	st := a.b.p.Status()
	if cur := a.b.q.Snapshot().Current; st.State == player.Stopped && st.EndOfQueue && cur != nil && cur.Handle == r.seed {
		if err := a.b.p.Next(ctx); err != nil {
			a.logger.Warn("autoplay: next failed", "err", err)
		}
	}
	return ""
}

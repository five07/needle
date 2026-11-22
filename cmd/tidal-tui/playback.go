package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/five07/tidal-tui/internal/player"
	"github.com/five07/tidal-tui/internal/queue"
	"github.com/five07/tidal-tui/internal/tidal"
)

// transport is the part of *player.Player that playback drives.
type transport interface {
	Play(ctx context.Context) error
	QueueChanged(ctx context.Context) error
}

// playback implements tui.Playback on the play queue and the player.
type playback struct {
	q *queue.Queue[player.Track]
	p transport
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
	return b.p.QueueChanged(ctx)
}

// Add appends tracks to the queue. On an empty queue the first one starts
// playing.
func (b *playback) Add(ctx context.Context, tracks []tidal.Track) error {
	_, had := b.q.Current()
	for _, t := range tracks {
		b.q.Add(toTrack(t))
	}
	if !had && len(tracks) > 0 {
		return b.p.Play(ctx)
	}
	return b.p.QueueChanged(ctx)
}

// statusSource is the part of *player.Player that watchStatus reads.
type statusSource interface {
	Updates() <-chan struct{}
	Status() player.Status
}

// watchStatus sends each new player message (such as "mpv was not found")
// to out until ctx ends, so the UI can show it. The now-playing bar
// (TASK-013) will show the full status.
func watchStatus(ctx context.Context, p statusSource, out chan<- string) {
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.Updates():
		}
		msg := p.Status().Message
		if msg != "" && msg != last {
			select {
			case out <- msg:
			case <-ctx.Done():
				return
			}
		}
		last = msg
	}
}

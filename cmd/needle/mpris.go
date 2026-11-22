package main

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/five07/needle/internal/mpris"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tui"
	"github.com/godbus/dbus/v5"
)

// mprisControls lets MPRIS drive the player and the queue.
type mprisControls struct{ b *playback }

// Play resumes, or starts the current track again when stopped.
func (c mprisControls) Play(ctx context.Context) error {
	if c.b.p.Status().State == player.Stopped {
		return c.b.p.Play(ctx)
	}
	return c.b.p.SetPaused(ctx, false)
}

// Pause pauses; it does nothing when not playing.
func (c mprisControls) Pause(ctx context.Context) error { return c.b.p.SetPaused(ctx, true) }

// PlayPause toggles, or starts the current track when stopped.
func (c mprisControls) PlayPause(ctx context.Context) error { return c.b.TogglePause(ctx) }

func (c mprisControls) Stop(ctx context.Context) error     { return c.b.p.Stop(ctx) }
func (c mprisControls) Next(ctx context.Context) error     { return c.b.Next(ctx) }
func (c mprisControls) Previous(ctx context.Context) error { return c.b.Previous(ctx) }
func (c mprisControls) SetShuffle(ctx context.Context, on bool) error {
	return c.b.SetShuffle(ctx, on)
}

// SetLoop maps MPRIS loop statuses to repeat modes.
func (c mprisControls) SetLoop(ctx context.Context, l mpris.Loop) error {
	r := map[mpris.Loop]queue.Repeat{mpris.LoopNone: queue.RepeatOff, mpris.LoopTrack: queue.RepeatOne, mpris.LoopPlaylist: queue.RepeatAll}[l]
	return c.b.SetRepeat(ctx, r)
}

// SetVolume sets the software volume from MPRIS (0 to 1, decision 87).
func (c mprisControls) SetVolume(ctx context.Context, v float64) error {
	if c.b.vol == nil {
		return mpris.ErrVolumeNotSupported
	}
	err := c.b.vol.SetVolume(ctx, math.Round(v*100))
	if errors.Is(err, player.ErrVolumeDisabled) {
		return mpris.ErrVolumeNotSupported
	}
	return err
}

// Seek moves the current track from MPRIS (decision 95). Nothing to seek
// in is not an error: the spec says to ignore the call.
func (c mprisControls) Seek(ctx context.Context, offset time.Duration) error {
	if c.b.sk == nil {
		return nil
	}
	_, err := c.b.sk.SeekBy(ctx, offset)
	if errors.Is(err, player.ErrNotSeekable) {
		return nil
	}
	return err
}

// SetPosition moves queue entry h to position from MPRIS, when it is the
// current track; otherwise the call is ignored, as the spec says.
func (c mprisControls) SetPosition(ctx context.Context, h uint64, position time.Duration) error {
	if c.b.sk == nil {
		return nil
	}
	_, err := c.b.sk.SeekTo(ctx, queue.Handle(h), position)
	if errors.Is(err, player.ErrNotSeekable) {
		return nil
	}
	return err
}

// mprisState converts a UI snapshot and the player status for MPRIS.
func mprisState(np tui.NowPlaying, ps player.Status) mpris.State {
	st := mpris.State{
		Position: np.Position,
		Shuffle:  np.Shuffle,
		Loop:     [...]mpris.Loop{tui.RepeatOff: mpris.LoopNone, tui.RepeatAll: mpris.LoopPlaylist, tui.RepeatOne: mpris.LoopTrack}[np.Repeat],
		Volume:   min(max(ps.Volume/100, 0), 1),
		CanNext:  len(np.Upcoming) > 0 || (np.Current != nil && (np.Repeat != tui.RepeatOff || np.Autoplay)),
		CanPrev:  np.Current != nil, // previous restarts the first track
		CanSeek:  np.Current != nil && (np.State == tui.Playing || np.State == tui.Paused),
		SeekID:   ps.SeekID,
	}
	switch np.State {
	case tui.Playing, tui.Loading:
		st.Status = mpris.Playing
	case tui.Paused:
		st.Status = mpris.Paused
	default:
		st.Status = mpris.Stopped
	}
	if c := np.Current; c != nil {
		length := np.Duration
		if length <= 0 {
			length = c.Track.Duration
		}
		st.Track = &mpris.Track{Handle: c.Handle, ID: c.Track.ID, Title: c.Track.Title, Artists: c.Track.Artists, Album: c.Track.Album, Length: length}
	}
	return st
}

// startMPRIS registers on the session bus. Without a bus, or when
// registering fails, it logs why and returns nil: the app runs without
// desktop integration (AC #5). stop gives up the name and closes the bus.
func startMPRIS(connect func() (*dbus.Conn, error), b *playback, logger *slog.Logger) (svc *mpris.Service, stop func()) {
	noop := func() {}
	if connect == nil {
		return nil, noop
	}
	conn, err := connect()
	if err != nil {
		logger.Warn("mpris unavailable: no session bus", "err", err)
		return nil, noop
	}
	svc, err = mpris.Start(conn, mprisControls{b}, logger)
	if err != nil {
		logger.Warn("mpris unavailable", "err", err)
		conn.Close()
		return nil, noop
	}
	return svc, func() { svc.Close(); conn.Close() }
}

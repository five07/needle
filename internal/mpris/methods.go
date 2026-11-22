package mpris

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// errFailed is the one error MPRIS callers see; details go to the log.
var errFailed = dbus.NewError("org.freedesktop.DBus.Error.Failed", []any{"needle: playback control failed"})

// call runs a control with callTimeout and hides its error details.
func (s *Service) call(name string, f func(context.Context) error) *dbus.Error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := f(ctx); err != nil {
		s.logger.Warn("mpris control failed", "method", name, "err", err)
		return errFailed
	}
	return nil
}

func (s *Service) rootMethods() map[string]any {
	none := func() *dbus.Error { return nil } // CanRaise and CanQuit are false
	return map[string]any{"Raise": none, "Quit": none}
}

func (s *Service) playerMethods() map[string]any {
	c := s.controls
	return map[string]any{
		"Play":        func() *dbus.Error { return s.call("Play", c.Play) },
		"Pause":       func() *dbus.Error { return s.call("Pause", c.Pause) },
		"PlayPause":   func() *dbus.Error { return s.call("PlayPause", c.PlayPause) },
		"Stop":        func() *dbus.Error { return s.call("Stop", c.Stop) },
		"Next":        func() *dbus.Error { return s.call("Next", c.Next) },
		"Previous":    func() *dbus.Error { return s.call("Previous", c.Previous) },
		"Seek":        s.seek,
		"SetPosition": s.setPosition,
		"OpenUri":     func(string) *dbus.Error { return nil },
	}
}

// seek moves the current track by offset microseconds (decision 95). It
// stops at 0:00 and short of the end instead of going to the next track
// as the spec suggests, like the seek keys (decision 92).
func (s *Service) seek(offset int64) *dbus.Error {
	if !s.seekable() {
		return nil // the spec: ignore while CanSeek is false
	}
	// Kept within what a time.Duration holds; the player clamps to the
	// track anyway.
	const maxMicros = math.MaxInt64 / int64(time.Microsecond)
	d := time.Duration(min(max(offset, -maxMicros), maxMicros)) * time.Microsecond
	return s.call("Seek", func(ctx context.Context) error { return s.controls.Seek(ctx, d) })
}

// setPosition moves track id to position microseconds. As the spec says,
// it does nothing when id is not the current track or position is outside
// the track.
func (s *Service) setPosition(id dbus.ObjectPath, position int64) *dbus.Error {
	h, ok := trackHandle(id)
	s.mu.Lock()
	t := s.last.Track
	s.mu.Unlock()
	if !ok || !s.seekable() || t == nil || t.Handle != h || position < 0 ||
		(t.Length > 0 && position > t.Length.Microseconds()) {
		return nil
	}
	d := time.Duration(position) * time.Microsecond
	return s.call("SetPosition", func(ctx context.Context) error { return s.controls.SetPosition(ctx, h, d) })
}

// seekable reports whether the last state allowed seeking.
func (s *Service) seekable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last.CanSeek && s.last.Track != nil
}

func (s *Service) onShuffle(ch *prop.Change) *dbus.Error {
	on, _ := ch.Value.(bool) // prop rejects other types before calling
	return s.call("SetShuffle", func(ctx context.Context) error { return s.controls.SetShuffle(ctx, on) })
}

func (s *Service) onLoop(ch *prop.Change) *dbus.Error {
	v, _ := ch.Value.(string) // prop rejects other types before calling
	l := Loop(v)
	if l != LoopNone && l != LoopTrack && l != LoopPlaylist {
		return prop.ErrInvalidArg
	}
	return s.call("SetLoop", func(ctx context.Context) error { return s.controls.SetLoop(ctx, l) })
}

// errVolumeNotSupported answers a Volume write while the volume is set on
// the output device.
var errVolumeNotSupported = dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"needle: volume is set on the output device"})

// onVolume sets the software volume (decision 87). Values below 0 count as
// 0, as the spec says, and values above 1 as 1, since needle never adds
// gain. Returning an error keeps the stored value unchanged.
func (s *Service) onVolume(ch *prop.Change) *dbus.Error {
	v, _ := ch.Value.(float64) // prop rejects other types before calling
	if math.IsNaN(v) {
		return prop.ErrInvalidArg
	}
	v = min(max(v, 0), 1)
	s.volumeStale.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	err := s.controls.SetVolume(ctx, v)
	switch {
	case errors.Is(err, ErrVolumeNotSupported):
		s.logger.Debug("mpris volume write refused: volume is on the output device", "value", v)
		return errVolumeNotSupported
	case err != nil:
		s.logger.Warn("mpris control failed", "method", "SetVolume", "err", err)
		return errFailed
	}
	return nil
}

func introspectMethods(names ...string) []introspect.Method {
	out := make([]introspect.Method, len(names))
	for i, n := range names {
		out[i] = introspect.Method{Name: n}
	}
	return out
}

func playerIntrospection() []introspect.Method {
	ms := introspectMethods("Next", "Previous", "Pause", "PlayPause", "Stop", "Play")
	return append(ms,
		introspect.Method{Name: "Seek", Args: []introspect.Arg{{Name: "Offset", Type: "x", Direction: "in"}}},
		introspect.Method{Name: "SetPosition", Args: []introspect.Arg{{Name: "TrackId", Type: "o", Direction: "in"}, {Name: "Position", Type: "x", Direction: "in"}}},
		introspect.Method{Name: "OpenUri", Args: []introspect.Arg{{Name: "Uri", Type: "s", Direction: "in"}}},
	)
}

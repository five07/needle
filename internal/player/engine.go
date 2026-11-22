// Package player plays tracks through mpv with bit-perfect output where the
// hardware allows it, and drives the play queue from playback events.
package player

import (
	"context"
	"errors"
	"time"

	"github.com/five07/tidal-tui/internal/tidal"
)

// Engine errors callers branch on.
var (
	// ErrMPVNotFound means the mpv binary is not installed or not on PATH.
	ErrMPVNotFound = errors.New("player: mpv is not installed")
	// ErrEngineFailed means the audio engine stopped unexpectedly.
	ErrEngineFailed = errors.New("player: the audio engine stopped unexpectedly")
	// ErrDeviceBusy means exclusive mode could not open the output device
	// because another application is using it.
	ErrDeviceBusy = errors.New("player: the output device is in use by another application")
	// ErrOutputFailed means the audio output could not be opened.
	ErrOutputFailed = errors.New("player: the audio output could not be opened")
	// ErrStreamFailed means the stream could not be loaded or broke mid-track.
	ErrStreamFailed = errors.New("player: the stream could not be played")
	// ErrVolumeDisabled means software volume is off in exclusive mode.
	ErrVolumeDisabled = errors.New("player: volume is controlled on the output device in exclusive mode")
)

// Item is one track handed to the engine.
type Item struct {
	// Key identifies this queue entry; it is echoed in events.
	Key      uint64
	Manifest tidal.Manifest
	// Start is where playback begins (used to resume after a stream
	// re-fetch). Zero means the beginning.
	Start time.Duration
}

// EventKind is the type of an engine event.
type EventKind int

// Engine event kinds.
const (
	// EventStarted: the item with Key began playing.
	EventStarted EventKind = iota + 1
	// EventEnded: the item with Key stopped; see Reason and Err.
	EventEnded
	// EventFormat: the decoded or output audio format changed.
	EventFormat
	// EventProgress: periodic position update for the current item.
	EventProgress
	// EventFailed: the engine died; it accepts no further commands.
	EventFailed
)

// EndReason says why an item stopped.
type EndReason int

// End reasons.
const (
	// EndEOF: the track played to the end.
	EndEOF EndReason = iota + 1
	// EndStopped: playback was stopped or replaced.
	EndStopped
	// EndError: the track failed; Event.Err says why.
	EndError
)

// AudioParams is an audio format as mpv reports it.
type AudioParams struct {
	SampleRate int    `json:"samplerate"`
	Channels   int    `json:"channel-count"`
	Format     string `json:"format"`
}

// Event is an engine notification.
type Event struct {
	Kind   EventKind
	Key    uint64
	Reason EndReason
	Err    error
	// Position is the playback position (EventProgress, EventEnded).
	Position time.Duration
	// Duration is the item duration when known (EventProgress).
	Duration time.Duration
	// Format fields (EventFormat).
	Decoded AudioParams
	Output  AudioParams
	Volume  float64
	Filters int
}

// Engine plays items. Implementations must be safe for concurrent use.
type Engine interface {
	// Play replaces whatever is playing with item.
	Play(ctx context.Context, item Item) error
	// Preload queues item to start right after the current one, replacing
	// any previously preloaded item, so the transition can be gapless.
	Preload(ctx context.Context, item Item) error
	// ClearPreload drops the preloaded item, if any.
	ClearPreload(ctx context.Context) error
	// SetPaused pauses or resumes playback.
	SetPaused(ctx context.Context, paused bool) error
	// SetVolume sets software volume (0-100). Exclusive mode returns
	// ErrVolumeDisabled.
	SetVolume(ctx context.Context, percent float64) error
	// Stop stops playback and drops any preloaded item.
	Stop(ctx context.Context) error
	// Events delivers engine events. It is closed after Close.
	Events() <-chan Event
	// Close stops the engine and releases its resources.
	Close() error
}

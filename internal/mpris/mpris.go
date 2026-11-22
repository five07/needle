// Package mpris publishes the player on the D-Bus session bus with the
// MPRIS D-Bus Interface Specification, so desktop media keys and bar
// widgets can show and control playback, seeking included (decision 95).
package mpris

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Names and paths from the MPRIS specification.
const (
	// BusName is the well-known name the player asks for. A second running
	// copy uses BusName + ".instance<pid>".
	BusName    = "org.mpris.MediaPlayer2.needle"
	ObjectPath = dbus.ObjectPath("/org/mpris/MediaPlayer2")

	ifaceRoot   = "org.mpris.MediaPlayer2"
	ifacePlayer = "org.mpris.MediaPlayer2.Player"

	// noTrack is the track ID MPRIS reserves for "no track".
	noTrack = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
	// trackPrefix starts the object path that identifies a queue entry.
	trackPrefix = "/org/mpris/MediaPlayer2/needle/track/"
)

// callTimeout bounds one control call from the bus.
const callTimeout = 10 * time.Second

// Status is the MPRIS PlaybackStatus.
type Status string

// Playback statuses.
const (
	Playing Status = "Playing"
	Paused  Status = "Paused"
	Stopped Status = "Stopped"
)

// Loop is the MPRIS LoopStatus.
type Loop string

// Loop statuses.
const (
	LoopNone     Loop = "None"
	LoopTrack    Loop = "Track"
	LoopPlaylist Loop = "Playlist"
)

// Track is the current track's metadata.
type Track struct {
	// Handle identifies the queue entry; the same track queued twice has
	// two handles, so MPRIS sees two track IDs.
	Handle  uint64
	ID      string // TIDAL track ID
	Title   string
	Artists string
	Album   string
	Length  time.Duration
}

// State is what the player shows over MPRIS.
type State struct {
	Status   Status
	Track    *Track // nil when the queue is empty
	Position time.Duration
	Shuffle  bool
	Loop     Loop
	Volume   float64 // 0 to 1
	CanNext  bool
	CanPrev  bool
	// CanSeek is set while a track plays or is paused.
	CanSeek bool
	// SeekID changes with every seek; a change emits the Seeked signal.
	SeekID uint64
}

// Controls is what MPRIS methods and property writes do. The methods are
// called from D-Bus goroutines.
type Controls interface {
	// Play resumes, or starts the current track when stopped.
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	PlayPause(ctx context.Context) error
	Stop(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
	SetShuffle(ctx context.Context, on bool) error
	SetLoop(ctx context.Context, l Loop) error
	// SetVolume sets the volume, 0 to 1. It returns ErrVolumeNotSupported
	// when the volume is set on the output device (decision 87).
	SetVolume(ctx context.Context, v float64) error
	// Seek moves the current track by offset (negative is back). It never
	// leaves the track (decision 92).
	Seek(ctx context.Context, offset time.Duration) error
	// SetPosition moves the queue entry with handle to position, when it
	// is the current track.
	SetPosition(ctx context.Context, handle uint64, position time.Duration) error
}

// ErrVolumeNotSupported means volume cannot be changed from here: it is set
// on the output device. MPRIS callers get NotSupported.
var ErrVolumeNotSupported = errors.New("mpris: volume is set on the output device")

// Service is the player's presence on the bus. Its methods are safe for
// concurrent use.
type Service struct {
	conn     *dbus.Conn
	name     string
	controls Controls
	logger   *slog.Logger
	props    *prop.Properties

	mu   sync.Mutex
	last State
	// volumeStale is set after a Volume write, so the next Update publishes
	// the real volume even when it did not change (a clamped or refused
	// write leaves the written value stored).
	volumeStale atomic.Bool
}

// Start exports the MPRIS objects on conn and takes the bus name. The
// caller keeps ownership of conn.
func Start(conn *dbus.Conn, c Controls, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Service{conn: conn, controls: c, logger: logger, last: State{Status: Stopped, Loop: LoopNone}}
	if err := conn.ExportMethodTable(s.rootMethods(), ObjectPath, ifaceRoot); err != nil {
		return nil, fmt.Errorf("mpris: export: %w", err)
	}
	if err := conn.ExportMethodTable(s.playerMethods(), ObjectPath, ifacePlayer); err != nil {
		return nil, fmt.Errorf("mpris: export: %w", err)
	}
	props, err := prop.Export(conn, ObjectPath, s.propMap())
	if err != nil {
		return nil, fmt.Errorf("mpris: export properties: %w", err)
	}
	s.props = props
	node := &introspect.Node{
		Name: string(ObjectPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: ifaceRoot, Methods: introspectMethods("Raise", "Quit"), Properties: props.Introspection(ifaceRoot)},
			{Name: ifacePlayer, Methods: playerIntrospection(), Properties: props.Introspection(ifacePlayer),
				Signals: []introspect.Signal{{Name: "Seeked", Args: []introspect.Arg{{Name: "Position", Type: "x"}}}}},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), ObjectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("mpris: export introspection: %w", err)
	}
	name, err := requestName(conn)
	if err != nil {
		return nil, err
	}
	s.name = name
	logger.Info("mpris registered", "bus_name", name)
	return s, nil
}

// requestName takes BusName, or BusName.instance<pid> when another copy
// already owns it.
func requestName(conn *dbus.Conn) (string, error) {
	for _, name := range []string{BusName, BusName + ".instance" + strconv.Itoa(os.Getpid())} {
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil {
			return "", fmt.Errorf("mpris: request bus name: %w", err)
		}
		if reply == dbus.RequestNameReplyPrimaryOwner {
			return name, nil
		}
	}
	return "", fmt.Errorf("mpris: bus name %s is taken", BusName)
}

// Name returns the bus name the service owns.
func (s *Service) Name() string { return s.name }

// Close gives up the bus name. The objects stay exported until the caller
// closes the connection.
func (s *Service) Close() error {
	_, err := s.conn.ReleaseName(s.name)
	return err
}

func (s *Service) propMap() prop.Map {
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }
	return prop.Map{
		ifaceRoot: {
			"CanQuit":             ro(false),
			"CanRaise":            ro(false),
			"HasTrackList":        ro(false),
			"Identity":            ro("needle"),
			"SupportedUriSchemes": ro([]string{}),
			"SupportedMimeTypes":  ro([]string{}),
		},
		ifacePlayer: {
			"PlaybackStatus": ro(string(Stopped)),
			"LoopStatus":     {Value: string(LoopNone), Writable: true, Emit: prop.EmitTrue, Callback: s.onLoop},
			"Rate":           ro(1.0),
			"Shuffle":        {Value: false, Writable: true, Emit: prop.EmitTrue, Callback: s.onShuffle},
			"Metadata":       ro(metadata(nil)),
			// Volume writes set the software volume when allowed (decision 87).
			"Volume":        {Value: 1.0, Writable: true, Emit: prop.EmitTrue, Callback: s.onVolume},
			"Position":      {Value: int64(0), Emit: prop.EmitFalse}, // read when asked, per the spec
			"MinimumRate":   ro(1.0),
			"MaximumRate":   ro(1.0),
			"CanGoNext":     ro(false),
			"CanGoPrevious": ro(false),
			"CanPlay":       ro(false),
			"CanPause":      ro(false),
			"CanSeek":       ro(false),
			"CanControl":    {Value: true, Emit: prop.EmitConst},
		},
	}
}

// Update publishes a new state. Changed properties are announced with
// PropertiesChanged; Position is only stored (AC #4).
func (s *Service) Update(st State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.Status == "" {
		st.Status = Stopped
	}
	if st.Loop == "" {
		st.Loop = LoopNone
	}
	old := s.last
	s.last = st
	set := func(name string, v any) { s.props.SetMust(ifacePlayer, name, v) }
	if !sameTrack(old.Track, st.Track) {
		set("Metadata", metadata(st.Track))
	}
	if old.Status != st.Status {
		set("PlaybackStatus", string(st.Status))
	}
	if old.Shuffle != st.Shuffle {
		set("Shuffle", st.Shuffle)
	}
	if old.Loop != st.Loop {
		set("LoopStatus", string(st.Loop))
	}
	if s.volumeStale.Swap(false) || old.Volume != st.Volume {
		set("Volume", st.Volume)
	}
	has := st.Track != nil
	for name, v := range map[string]bool{"CanGoNext": st.CanNext, "CanGoPrevious": st.CanPrev, "CanPlay": has, "CanPause": has, "CanSeek": st.CanSeek && has} {
		if cur, err := s.props.Get(ifacePlayer, name); err != nil || cur.Value() != v {
			set(name, v)
		}
	}
	set("Position", st.Position.Microseconds())
	if st.SeekID != old.SeekID && has {
		// Widgets expect Seeked after a jump instead of a Position change.
		if err := s.conn.Emit(ObjectPath, ifacePlayer+".Seeked", st.Position.Microseconds()); err != nil {
			s.logger.Debug("mpris seeked signal failed", "err", err)
		}
	}
}

func sameTrack(a, b *Track) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// metadata builds the MPRIS Metadata map for t.
func metadata(t *Track) map[string]dbus.Variant {
	if t == nil {
		return map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(noTrack)}
	}
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath(trackPrefix + strconv.FormatUint(t.Handle, 10))),
		"xesam:title":   dbus.MakeVariant(t.Title),
	}
	if t.Length > 0 {
		m["mpris:length"] = dbus.MakeVariant(t.Length.Microseconds())
	}
	if t.Artists != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{t.Artists})
	}
	if t.Album != "" {
		m["xesam:album"] = dbus.MakeVariant(t.Album)
	}
	if validID(t.ID) {
		m["xesam:url"] = dbus.MakeVariant("https://tidal.com/browse/track/" + t.ID)
	}
	return m
}

// trackHandle returns the queue handle in an MPRIS track ID made by
// metadata, or false for any other path.
func trackHandle(p dbus.ObjectPath) (uint64, bool) {
	rest, ok := strings.CutPrefix(string(p), trackPrefix)
	if !ok {
		return 0, false
	}
	h, err := strconv.ParseUint(rest, 10, 64)
	return h, err == nil && h != 0
}

// validID reports whether id is a plain TIDAL track ID (digits), safe to
// put in a URL.
func validID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

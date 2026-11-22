package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/five07/needle/internal/tidal"
)

// Catalog is the read API the views use. *tidal.Catalog implements it.
type Catalog interface {
	Search(ctx context.Context, query string) (tidal.SearchResults, error)
	AlbumTracks(ctx context.Context, albumID, next string) (tidal.Page[tidal.Track], error)
	PlaylistTracks(ctx context.Context, playlistID, next string) (tidal.Page[tidal.Track], error)
	ArtistTracks(ctx context.Context, artistID, next string) (tidal.Page[tidal.Track], error)
	ArtistAlbums(ctx context.Context, artistID, next string) (tidal.Page[tidal.Album], error)
}

// Library reads and changes the user's favorites and playlists.
// *tidal.Catalog implements it.
type Library interface {
	FavoriteTracks(ctx context.Context, sort tidal.Sort, next string) (tidal.Page[tidal.Track], error)
	FavoriteAlbums(ctx context.Context, sort tidal.Sort, next string) (tidal.Page[tidal.Album], error)
	FavoriteArtists(ctx context.Context, sort tidal.Sort, next string) (tidal.Page[tidal.Artist], error)
	FavoritePlaylists(ctx context.Context, sort tidal.Sort, next string) (tidal.Page[tidal.Playlist], error)
	OwnedPlaylists(ctx context.Context, next string) (tidal.Page[tidal.Playlist], error)
	AddFavorite(ctx context.Context, c tidal.Collection, id string) error
	RemoveFavorite(ctx context.Context, c tidal.Collection, id string) error
	AddToPlaylist(ctx context.Context, playlistID, trackID string) error
}

// Discovery reads the user's mixes, radios, and similar items.
// *tidal.Catalog implements it.
type Discovery interface {
	Mixes(ctx context.Context, kind tidal.MixKind) ([]tidal.Playlist, error)
	TrackRadio(ctx context.Context, trackID string) (tidal.Playlist, error)
	ArtistRadio(ctx context.Context, artistID string) (tidal.Playlist, error)
	SimilarTracks(ctx context.Context, trackID, next string) (tidal.Page[tidal.Track], error)
	SimilarArtists(ctx context.Context, artistID, next string) (tidal.Page[tidal.Artist], error)
	SimilarAlbums(ctx context.Context, albumID, next string) (tidal.Page[tidal.Album], error)
}

// Playback starts and queues tracks. The views call it from commands, off
// the UI goroutine.
type Playback interface {
	// PlayFrom replaces the queue with tracks and plays tracks[start].
	PlayFrom(ctx context.Context, tracks []tidal.Track, start int) error
	// PlayNext inserts tracks, in order, right after the current track.
	PlayNext(ctx context.Context, tracks []tidal.Track) error
	// Add appends tracks to the end of the queue.
	Add(ctx context.Context, tracks []tidal.Track) error
}

// Transport drives the player and edits the queue. The views call it from
// commands, off the UI goroutine. Handles come from NowPlaying.
type Transport interface {
	TogglePause(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
	SetShuffle(ctx context.Context, on bool) error
	CycleRepeat(ctx context.Context) error
	// SetAutoplay turns autoplay on or off; off removes the upcoming
	// autoplayed tracks (decision 65).
	SetAutoplay(ctx context.Context, on bool) error
	// Jump plays the queued entry h.
	Jump(ctx context.Context, h uint64) error
	// Remove deletes the upcoming entry h.
	Remove(ctx context.Context, h uint64) error
	// Clear removes every upcoming entry; the current one keeps playing.
	Clear(ctx context.Context) error
}

// Timings and limits for the catalog views.
const (
	// SearchDebounce is how long typing must pause before a search starts.
	SearchDebounce = 300 * time.Millisecond
	// MaxQueueTracks caps how many tracks one album or playlist adds to the
	// queue (decision 29).
	MaxQueueTracks = 500
	// playbackTimeout bounds one call into Playback.
	playbackTimeout = 10 * time.Second
	// loadAhead is how close to the end of a list the cursor gets before the
	// next page is fetched.
	loadAhead = 5
)

// Services connects the views to TIDAL and the player.
type Services struct {
	// Ctx is canceled when the program stops. Default context.Background.
	Ctx       context.Context
	Catalog   Catalog
	Library   Library
	Discovery Discovery
	Playback  Playback
	Transport Transport
	// Logger receives failures in full; the UI shows short messages.
	Logger *slog.Logger
	// Tick schedules fn after d. Default tea.Tick (tests replace it).
	Tick func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd
	// Now is the clock. Default time.Now (tests replace it).
	Now func() time.Time

	// known holds favorite state learned this session. Only the UI
	// goroutine touches it.
	known *favorites
	// playing is the latest player snapshot. Only the UI goroutine
	// touches it.
	playing NowPlaying
}

func (s *Services) ctx() context.Context {
	if s.Ctx == nil {
		return context.Background()
	}
	return s.Ctx
}

func (s *Services) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Logger
}

func (s *Services) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Services) tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	if s.Tick == nil {
		return tea.Tick(d, fn)
	}
	return s.Tick(d, fn)
}

// userMessage turns an API error into a short message for the hint line or
// a view. Details stay in the log.
func userMessage(err error) string {
	switch {
	case errors.Is(err, tidal.ErrUnauthorized):
		return "Your TIDAL session has ended. Restart needle to log in again."
	case errors.Is(err, tidal.ErrRateLimited):
		return "TIDAL is busy right now. Try again in a moment."
	case errors.Is(err, tidal.ErrNotFound), errors.Is(err, tidal.ErrInvalidResourceID):
		return "Not found on TIDAL."
	case errors.Is(err, context.DeadlineExceeded):
		return "TIDAL did not respond in time. Check your connection."
	case errors.Is(err, tidal.ErrCollectionFull):
		return "Your TIDAL collection is full."
	case errors.Is(err, tidal.ErrPlaylistFull):
		return "The playlist is full."
	}
	return "Something went wrong. See the log for details."
}

// playCmd replaces the queue with tracks and plays tracks[start].
func (s *Services) playCmd(tracks []tidal.Track, start int) tea.Cmd {
	if s.Playback == nil || start < 0 || start >= len(tracks) {
		return nil
	}
	tracks = append([]tidal.Track(nil), tracks...)
	title := tracks[start].FullTitle()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.ctx(), playbackTimeout)
		defer cancel()
		if err := s.Playback.PlayFrom(ctx, tracks, start); err != nil {
			return s.failed("play", tracks[start].ID, err)
		}
		return NoticeMsg{Text: "Playing " + title}
	}
}

// queueCmd adds tracks to the queue (next: right after the current track).
// what names them in the notice.
func (s *Services) queueCmd(tracks []tidal.Track, next bool, what string) tea.Cmd {
	if s.Playback == nil || len(tracks) == 0 {
		return nil
	}
	tracks = append([]tidal.Track(nil), tracks...)
	return func() tea.Msg { return s.enqueue(tracks, next, what) }
}

func (s *Services) enqueue(tracks []tidal.Track, next bool, what string) tea.Msg {
	ctx, cancel := context.WithTimeout(s.ctx(), playbackTimeout)
	defer cancel()
	var err error
	if next {
		err = s.Playback.PlayNext(ctx, tracks)
	} else {
		err = s.Playback.Add(ctx, tracks)
	}
	if err != nil {
		return s.failed("queue", tracks[0].ID, err)
	}
	if next {
		return NoticeMsg{Text: "Playing next: " + what}
	}
	return NoticeMsg{Text: "Added to queue: " + what}
}

// queueAllCmd fetches every page of a collection (up to MaxQueueTracks)
// and queues the tracks.
func (s *Services) queueAllCmd(name string, next bool, fetch func(ctx context.Context, next string) (tidal.Page[tidal.Track], error)) tea.Cmd {
	if s.Playback == nil || s.Catalog == nil {
		return nil
	}
	return func() tea.Msg {
		tracks, capped, err := collect(s.ctx(), fetch, MaxQueueTracks)
		if err != nil {
			s.logger().Warn("load tracks to queue failed", "err", err)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return NoticeMsg{Text: "Could not queue " + name + ": " + userMessage(err)}
		}
		if len(tracks) == 0 {
			return NoticeMsg{Text: name + " has no playable tracks."}
		}
		what := fmt.Sprintf("%d tracks from %s", len(tracks), name)
		if capped {
			what += fmt.Sprintf(" (first %d)", MaxQueueTracks)
		}
		return s.enqueue(tracks, next, what)
	}
}

// collect follows pages until there are no more or limit tracks are
// loaded. capped reports that more tracks were left out.
func collect(ctx context.Context, fetch func(ctx context.Context, next string) (tidal.Page[tidal.Track], error), limit int) (tracks []tidal.Track, capped bool, err error) {
	next := ""
	for {
		p, err := fetch(ctx, next)
		if err != nil {
			return nil, false, err
		}
		tracks = append(tracks, p.Items...)
		if len(tracks) >= limit {
			return tracks[:limit], len(tracks) > limit || p.Next != "", nil
		}
		if p.Next == "" {
			return tracks, false, nil
		}
		next = p.Next
	}
}

func (s *Services) failed(action, trackID string, err error) tea.Msg {
	s.logger().Warn(action+" failed", "track_id", trackID, "err", err)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NoticeMsg{Text: "The player did not respond in time."}
	}
	return NoticeMsg{Text: "Could not " + action + " the track. See the log for details."}
}

// transportCmd runs f off the UI goroutine with playbackTimeout. A failure
// is logged and shown as a notice.
func (s *Services) transportCmd(f func(context.Context) error) tea.Cmd {
	ctx := s.ctx()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()
		if err := f(ctx); err != nil {
			s.logger().Warn("playback control failed", "err", err)
			return NoticeMsg{Text: "Playback: " + userMessage(err)}
		}
		return nil
	}
}

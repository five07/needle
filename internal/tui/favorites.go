package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/five07/tidal-tui/internal/tidal"
)

// favoriteTimeout bounds one favorites write.
const favoriteTimeout = 15 * time.Second

type favKey struct {
	c  tidal.Collection
	id string
}

// favorites is what tidal-tui knows about the user's favorites: items seen
// in a Library list or changed this session (decision 31). Anything else is
// unknown. Only the UI goroutine uses it.
type favorites struct {
	known map[favKey]bool
}

func (s *Services) favs() *favorites {
	if s.known == nil {
		s.known = &favorites{known: map[favKey]bool{}}
	}
	return s.known
}

func (f *favorites) set(c tidal.Collection, id string, fav bool) { f.known[favKey{c, id}] = fav }

// is reports whether the item is known to be a favorite.
func (f *favorites) is(c tidal.Collection, id string) bool { return f.known[favKey{c, id}] }

// FavoriteMsg reports a finished favorite change. The shell records it,
// passes it to every open view, and confirms it in the hint line.
type FavoriteMsg struct {
	Collection tidal.Collection
	ID         string
	Name       string
	Added      bool
}

// favoriteWatcher is implemented by views that show favorite state.
type favoriteWatcher interface {
	FavoriteChanged(FavoriteMsg) tea.Cmd
}

// favRef returns the favorites list, ID, and display name of an entry.
func favRef(en entry) (c tidal.Collection, id, name string, ok bool) {
	switch en.kind {
	case entryTrack:
		return tidal.FavTracks, en.track.ID, en.track.FullTitle(), true
	case entryAlbum:
		return tidal.FavAlbums, en.album.ID, en.album.Title, true
	case entryArtist:
		return tidal.FavArtists, en.artist.ID, en.artist.Name, true
	case entryPlaylist:
		return tidal.FavPlaylists, en.playlist.ID, en.playlist.Name, true
	}
	return "", "", "", false
}

// favoriteCmd adds (f) or removes (F) the entry from the favorites.
func (s *Services) favoriteCmd(en entry, add bool) tea.Cmd {
	c, id, name, ok := favRef(en)
	if !ok || s.Library == nil {
		return nil
	}
	if add && s.favs().is(c, id) {
		return Notify(name + " is already a favorite.")
	}
	lib := s.Library
	ctx := s.ctx()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, favoriteTimeout)
		defer cancel()
		var err error
		if add {
			err = lib.AddFavorite(ctx, c, id)
		} else {
			err = lib.RemoveFavorite(ctx, c, id)
		}
		if err != nil {
			s.logger().Warn("favorite change failed", "collection", string(c), "item_id", id, "add", add, "err", err)
			if ctx.Err() != nil && s.ctx().Err() != nil {
				return nil // shutting down
			}
			return NoticeMsg{Text: "Could not update favorites: " + userMessage(err)}
		}
		return FavoriteMsg{Collection: c, ID: id, Name: name, Added: add}
	}
}

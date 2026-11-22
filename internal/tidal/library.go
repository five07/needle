package tidal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

// Collection names one of the user's favorites lists.
type Collection string

// Favorites lists. The value is the JSON:API type of their items.
const (
	FavTracks    Collection = "tracks"
	FavAlbums    Collection = "albums"
	FavArtists   Collection = "artists"
	FavPlaylists Collection = "playlists"
)

// Sort is a server-side sort order for a favorites list.
type Sort string

// Sort orders. Not every list supports every order: see Sorts.
const (
	SortAdded  Sort = "-addedAt" // newest added first (TIDAL's default)
	SortTitle  Sort = "title"
	SortName   Sort = "name"
	SortArtist Sort = "artists.name"
)

// Sorts returns the sort orders c supports, the default first.
func Sorts(c Collection) []Sort {
	switch c {
	case FavTracks, FavAlbums:
		return []Sort{SortAdded, SortTitle, SortArtist}
	case FavArtists, FavPlaylists:
		return []Sort{SortAdded, SortName}
	}
	return nil
}

// ErrInvalidSort means a sort order is not supported by the list.
var ErrInvalidSort = errors.New("tidal: unsupported sort order")

var collectionPaths = map[Collection]string{
	FavTracks:    "/userCollectionTracks/me/relationships/items",
	FavAlbums:    "/userCollectionAlbums/me/relationships/items",
	FavArtists:   "/userCollectionArtists/me/relationships/items",
	FavPlaylists: "/userCollectionPlaylists/me/relationships/items",
}

func (k *Catalog) favoritesQuery(c Collection, sort Sort, include string) (url.Values, error) {
	if !slices.Contains(Sorts(c), sort) {
		return nil, fmt.Errorf("%w: %q for %s", ErrInvalidSort, truncate(string(sort), 40), c)
	}
	q := url.Values{"include": {include}, "sort": {string(sort)}}
	if c == FavPlaylists {
		q.Set("collectionView", "FLAT") // no folders
	}
	return q, nil
}

// FavoriteTracks returns a page of the user's favorite tracks.
func (k *Catalog) FavoriteTracks(ctx context.Context, sort Sort, next string) (Page[Track], error) {
	q, err := k.favoritesQuery(FavTracks, sort, "items.artists,items.albums")
	if err != nil {
		return Page[Track]{}, err
	}
	return listPage(ctx, k, collectionPaths[FavTracks], q, next, linked("tracks", trackFrom))
}

// FavoriteAlbums returns a page of the user's favorite albums.
func (k *Catalog) FavoriteAlbums(ctx context.Context, sort Sort, next string) (Page[Album], error) {
	q, err := k.favoritesQuery(FavAlbums, sort, "items.artists")
	if err != nil {
		return Page[Album]{}, err
	}
	return listPage(ctx, k, collectionPaths[FavAlbums], q, next, linked("albums", albumFrom))
}

// FavoriteArtists returns a page of the user's favorite artists.
func (k *Catalog) FavoriteArtists(ctx context.Context, sort Sort, next string) (Page[Artist], error) {
	q, err := k.favoritesQuery(FavArtists, sort, "items")
	if err != nil {
		return Page[Artist]{}, err
	}
	return listPage(ctx, k, collectionPaths[FavArtists], q, next, linked("artists", artistFrom))
}

// FavoritePlaylists returns a page of the playlists in the user's
// collection: ones they own and ones they added, without folders.
func (k *Catalog) FavoritePlaylists(ctx context.Context, sort Sort, next string) (Page[Playlist], error) {
	q, err := k.favoritesQuery(FavPlaylists, sort, "items")
	if err != nil {
		return Page[Playlist]{}, err
	}
	return listPage(ctx, k, collectionPaths[FavPlaylists], q, next, linked("playlists", playlistFrom))
}

// OwnedPlaylists returns a page of the playlists the user owns, the ones
// tracks can be added to.
func (k *Catalog) OwnedPlaylists(ctx context.Context, next string) (Page[Playlist], error) {
	q := url.Values{"countryCode": {k.country}, "filter[owners.id]": {"me"}}
	return listPage(ctx, k, "/playlists", q, next, func(doc *Document) ([]Playlist, error) {
		rs, _ := doc.Many() // listPage already checked that data is a list
		out := make([]Playlist, 0, len(rs))
		for _, r := range rs {
			if r.Type != "playlists" {
				continue
			}
			p, err := playlistFrom(doc, r)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	})
}

type itemRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func checkItemID(c Collection, id string) error {
	ok := false
	switch c {
	case FavTracks, FavAlbums, FavArtists:
		ok = numericID(id)
	case FavPlaylists:
		ok = playlistIDOK(id)
	default:
		return fmt.Errorf("tidal: unknown collection %q", truncate(string(c), 20))
	}
	if !ok {
		return fmt.Errorf("%w: %s %q", ErrInvalidResourceID, c, truncate(id, 40))
	}
	return nil
}

// AddFavorite adds item id to the favorites list c. An item that is
// already a favorite is not an error.
func (k *Catalog) AddFavorite(ctx context.Context, c Collection, id string) error {
	if err := checkItemID(c, id); err != nil {
		return err
	}
	body := map[string]any{"data": []itemRef{{ID: id, Type: string(c)}}}
	_, err := k.c.Do(ctx, http.MethodPost, collectionPaths[c], nil, body)
	if errors.Is(err, ErrAlreadyInCollection) {
		return nil
	}
	return err
}

// RemoveFavorite removes item id from the favorites list c.
func (k *Catalog) RemoveFavorite(ctx context.Context, c Collection, id string) error {
	if err := checkItemID(c, id); err != nil {
		return err
	}
	body := map[string]any{"data": []itemRef{{ID: id, Type: string(c)}}}
	_, err := k.c.Do(ctx, http.MethodDelete, collectionPaths[c], nil, body)
	return err
}

// AddToPlaylist appends a track to a playlist the user owns. A track that
// is already in the playlist is not added again: the error is
// ErrDuplicateInPlaylist.
func (k *Catalog) AddToPlaylist(ctx context.Context, playlistID, trackID string) error {
	if !playlistIDOK(playlistID) {
		return fmt.Errorf("%w: playlist %q", ErrInvalidResourceID, truncate(playlistID, 40))
	}
	if !numericID(trackID) {
		return fmt.Errorf("%w: track %q", ErrInvalidResourceID, truncate(trackID, 40))
	}
	body := map[string]any{
		"data": []itemRef{{ID: trackID, Type: "tracks"}},
		"meta": map[string]string{"onDuplicates": "FAIL"},
	}
	_, err := k.c.Do(ctx, http.MethodPost, "/playlists/"+playlistID+"/relationships/items", nil, body)
	return err
}

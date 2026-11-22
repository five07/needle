package tidal

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxQueryLen is the longest search query, in characters.
const MaxQueryLen = 200

// ErrInvalidQuery means a search query is empty or too long.
var ErrInvalidQuery = errors.New("tidal: invalid search query")

// Track is a catalog track as shown in lists and queued for playback.
type Track struct {
	ID string
	// Title is the track title without its version.
	Title string
	// Version distinguishes releases of the same title ("Remastered").
	Version  string
	Artists  []string
	AlbumID  string
	Album    string
	Duration time.Duration
	Explicit bool
}

// FullTitle returns the title with the version in parentheses, if any.
func (t Track) FullTitle() string {
	if t.Version == "" {
		return t.Title
	}
	return t.Title + " (" + t.Version + ")"
}

// Album is a catalog album.
type Album struct {
	ID      string
	Title   string
	Artists []string
	// Year is the release year, or "" when unknown.
	Year     string
	Tracks   int
	Duration time.Duration
}

// Artist is a catalog artist.
type Artist struct {
	ID   string
	Name string
}

// Playlist is a catalog playlist.
type Playlist struct {
	ID   string
	Name string
	// Editorial is set for playlists curated by TIDAL.
	Editorial bool
	Tracks    int
	Duration  time.Duration
}

// SearchResults holds the first page of each result type, in TIDAL's
// relevance order.
type SearchResults struct {
	Tracks    []Track
	Albums    []Album
	Artists   []Artist
	Playlists []Playlist
}

// Page is one page of a cursor-paginated list. Next, when non-empty, is
// passed back to the same method to fetch the following page.
type Page[T any] struct {
	Items []T
	Next  string
}

// Catalog reads search results, albums, artists, and playlists for one
// country. It is safe for concurrent use.
type Catalog struct {
	c       *Client
	country string
}

// NewCatalog returns a catalog for the ISO 3166-1 alpha-2 country code,
// which is the logged-in user's country from Me.
func NewCatalog(c *Client, country string) (*Catalog, error) {
	if c == nil {
		return nil, errors.New("tidal: catalog needs a client")
	}
	if len(country) != 2 || !isUpper(country[0]) || !isUpper(country[1]) {
		return nil, fmt.Errorf("tidal: invalid country code %q", truncate(country, 8))
	}
	return &Catalog{c: c, country: country}, nil
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

const searchInclude = "tracks.artists,tracks.albums,albums.artists,artists,playlists"

// Search returns tracks, albums, artists, and playlists matching query.
func (k *Catalog) Search(ctx context.Context, query string) (SearchResults, error) {
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > MaxQueryLen || !utf8.ValidString(query) {
		return SearchResults{}, ErrInvalidQuery
	}
	doc, err := k.c.Get(ctx, "/searchResults", url.Values{
		"filter[query]": {query},
		"countryCode":   {k.country},
		"include":       {searchInclude},
	})
	if err != nil {
		return SearchResults{}, err
	}
	rs, err := doc.Many()
	if err != nil {
		return SearchResults{}, fmt.Errorf("tidal: search results: %w", err)
	}
	var out SearchResults
	if len(rs) == 0 {
		return out, nil
	}
	rels := rs[0].Relationships
	if out.Tracks, err = related(doc, rels["tracks"], "tracks", trackFrom); err != nil {
		return SearchResults{}, err
	}
	if out.Albums, err = related(doc, rels["albums"], "albums", albumFrom); err != nil {
		return SearchResults{}, err
	}
	if out.Artists, err = related(doc, rels["artists"], "artists", artistFrom); err != nil {
		return SearchResults{}, err
	}
	if out.Playlists, err = related(doc, rels["playlists"], "playlists", playlistFrom); err != nil {
		return SearchResults{}, err
	}
	return out, nil
}

// AlbumTracks returns a page of an album's tracks in disc and track order.
func (k *Catalog) AlbumTracks(ctx context.Context, albumID, next string) (Page[Track], error) {
	if !numericID(albumID) {
		return Page[Track]{}, fmt.Errorf("%w: album %q", ErrInvalidResourceID, truncate(albumID, 40))
	}
	return listPage(ctx, k, "/albums/"+albumID+"/relationships/items", k.catalogQuery("items.artists,items.albums"), next, linked("tracks", trackFrom))
}

// PlaylistTracks returns a page of a playlist's tracks in playlist order.
// Videos in the playlist are skipped.
func (k *Catalog) PlaylistTracks(ctx context.Context, playlistID, next string) (Page[Track], error) {
	if !playlistIDOK(playlistID) {
		return Page[Track]{}, fmt.Errorf("%w: playlist %q", ErrInvalidResourceID, truncate(playlistID, 40))
	}
	return listPage(ctx, k, "/playlists/"+playlistID+"/relationships/items", k.catalogQuery("items.artists,items.albums"), next, linked("tracks", trackFrom))
}

// ArtistTracks returns a page of an artist's top tracks, with duplicate
// releases of the same recording collapsed.
func (k *Catalog) ArtistTracks(ctx context.Context, artistID, next string) (Page[Track], error) {
	if !numericID(artistID) {
		return Page[Track]{}, fmt.Errorf("%w: artist %q", ErrInvalidResourceID, truncate(artistID, 40))
	}
	return listPage(ctx, k, "/artists/"+artistID+"/relationships/tracks", k.catalogQuery("tracks.artists,tracks.albums", "collapseBy", "FINGERPRINT"), next, linked("tracks", trackFrom))
}

// ArtistAlbums returns a page of an artist's albums.
func (k *Catalog) ArtistAlbums(ctx context.Context, artistID, next string) (Page[Album], error) {
	if !numericID(artistID) {
		return Page[Album]{}, fmt.Errorf("%w: artist %q", ErrInvalidResourceID, truncate(artistID, 40))
	}
	return listPage(ctx, k, "/artists/"+artistID+"/relationships/albums", k.catalogQuery("albums.artists"), next, linked("albums", albumFrom))
}

// listPage fetches the first page of path with query first, or the page
// that next (a links.next value from an earlier page of the same path)
// points to. items turns the document into the page's items.
func listPage[T any](ctx context.Context, k *Catalog, path string, first url.Values, next string,
	items func(*Document) ([]T, error)) (Page[T], error) {
	var (
		doc *Document
		err error
	)
	if next == "" {
		doc, err = k.c.Get(ctx, path, first)
	} else {
		if u, perr := url.Parse(next); perr != nil || u.IsAbs() || u.Host != "" || u.Path != path {
			return Page[T]{}, fmt.Errorf("tidal: next page link does not belong to %s", path)
		}
		doc, err = k.c.Get(ctx, next, nil)
	}
	if err != nil {
		return Page[T]{}, err
	}
	if _, err := doc.Many(); err != nil {
		return Page[T]{}, fmt.Errorf("tidal: %s: %w", path, err)
	}
	out, err := items(doc)
	if err != nil {
		return Page[T]{}, err
	}
	return Page[T]{Items: out, Next: doc.Links.Next}, nil
}

// linked returns an items function for documents whose primary data links
// to included resources of type typ.
func linked[T any](typ string, conv func(*Document, Resource) (T, error)) func(*Document) ([]T, error) {
	return func(doc *Document) ([]T, error) { return related(doc, Relationship{Data: doc.Data}, typ, conv) }
}

// catalogQuery is the first-page query for catalog lists.
func (k *Catalog) catalogQuery(include string, extra ...string) url.Values {
	q := url.Values{"countryCode": {k.country}, "include": {include}}
	for i := 0; i+1 < len(extra); i += 2 {
		q.Set(extra[i], extra[i+1])
	}
	return q
}

// related converts the included resources of type typ that rel links to, in
// linkage order. Linked resources that were not included, or have another
// type (videos in a playlist), are skipped.
func related[T any](doc *Document, rel Relationship, typ string, conv func(*Document, Resource) (T, error)) ([]T, error) {
	found, _, err := doc.Resolve(rel)
	if err != nil {
		return nil, fmt.Errorf("tidal: %s linkage: %w", typ, err)
	}
	out := make([]T, 0, len(found))
	for _, r := range found {
		if r.Type != typ {
			continue
		}
		v, err := conv(doc, r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func trackFrom(doc *Document, r Resource) (Track, error) {
	a, err := DecodeAttributes[struct {
		Title    string `json:"title"`
		Version  string `json:"version"`
		Duration string `json:"duration"`
		Explicit bool   `json:"explicit"`
	}](r)
	if err != nil {
		return Track{}, err
	}
	t := Track{
		ID:       r.ID,
		Title:    a.Title,
		Version:  a.Version,
		Artists:  names(doc, r.Relationships["artists"]),
		Duration: duration(a.Duration),
		Explicit: a.Explicit,
	}
	if albums, _, _ := doc.Resolve(r.Relationships["albums"]); len(albums) > 0 {
		t.AlbumID = albums[0].ID
		if aa, err := DecodeAttributes[struct {
			Title string `json:"title"`
		}](albums[0]); err == nil {
			t.Album = aa.Title
		}
	}
	return t, nil
}

func albumFrom(doc *Document, r Resource) (Album, error) {
	a, err := DecodeAttributes[struct {
		Title         string `json:"title"`
		ReleaseDate   string `json:"releaseDate"`
		NumberOfItems int    `json:"numberOfItems"`
		Duration      string `json:"duration"`
	}](r)
	if err != nil {
		return Album{}, err
	}
	al := Album{
		ID:       r.ID,
		Title:    a.Title,
		Artists:  names(doc, r.Relationships["artists"]),
		Tracks:   a.NumberOfItems,
		Duration: duration(a.Duration),
	}
	if len(a.ReleaseDate) >= 4 {
		al.Year = a.ReleaseDate[:4]
	}
	return al, nil
}

func artistFrom(_ *Document, r Resource) (Artist, error) {
	a, err := DecodeAttributes[struct {
		Name string `json:"name"`
	}](r)
	if err != nil {
		return Artist{}, err
	}
	return Artist{ID: r.ID, Name: a.Name}, nil
}

func playlistFrom(_ *Document, r Resource) (Playlist, error) {
	a, err := DecodeAttributes[struct {
		Name               string `json:"name"`
		NumberOfTrackItems int    `json:"numberOfTrackItems"`
		Duration           string `json:"duration"`
		PlaylistType       string `json:"playlistType"`
	}](r)
	if err != nil {
		return Playlist{}, err
	}
	return Playlist{
		ID:        r.ID,
		Name:      a.Name,
		Editorial: a.PlaylistType == "EDITORIAL",
		Tracks:    a.NumberOfTrackItems,
		Duration:  duration(a.Duration),
	}, nil
}

// names returns the names of the included artists rel links to. Artists
// that were not included or have no name are left out.
func names(doc *Document, rel Relationship) []string {
	found, _, _ := doc.Resolve(rel)
	var out []string
	for _, r := range found {
		if a, err := DecodeAttributes[struct {
			Name string `json:"name"`
		}](r); err == nil && a.Name != "" {
			out = append(out, a.Name)
		}
	}
	return out
}

// duration parses an ISO 8601 duration, treating a missing or malformed
// value as unknown (zero): it is only displayed.
func duration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := parseISODuration(s)
	if err != nil {
		return 0
	}
	return d
}

// playlistIDOK accepts TIDAL playlist IDs (UUIDs): letters, digits, and
// hyphens, so the ID is safe as a single path segment.
func playlistIDOK(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

package tidal

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// MixKind names one of the user's personal mix lists.
type MixKind string

// Personal mix lists (THIRD_PARTY, scope recommendations.read).
const (
	DailyMixes      MixKind = "userDailyMixes"
	DiscoveryMixes  MixKind = "userDiscoveryMixes"
	NewReleaseMixes MixKind = "userNewReleaseMixes"
)

// maxMixPages bounds how many pages Mixes follows. TIDAL returns every mix
// on one page today (8 daily mixes at most).
const maxMixPages = 5

// ErrNoRadio means TIDAL has no radio for the track or artist.
var ErrNoRadio = errors.New("tidal: no radio for this item")

// SetLocale sets the BCP 47 language tag mix names are returned in, for
// example "en-US". An invalid tag is rejected and the default kept.
func (k *Catalog) SetLocale(tag string) error {
	if !localeOK(tag) {
		return fmt.Errorf("tidal: invalid locale %q", truncate(tag, 16))
	}
	k.locale = tag
	return nil
}

// localeOK accepts "ll", "lll", "ll-CC" and "lll-CC".
func localeOK(tag string) bool {
	lang, region, hasRegion := strings.Cut(tag, "-")
	if len(lang) < 2 || len(lang) > 3 || strings.Trim(lang, "abcdefghijklmnopqrstuvwxyz") != "" {
		return false
	}
	return !hasRegion || len(region) == 2 && isUpper(region[0]) && isUpper(region[1])
}

// Mixes returns the user's mixes of one kind, in TIDAL's order. A kind with
// no mixes returns an empty list.
func (k *Catalog) Mixes(ctx context.Context, kind MixKind) ([]Playlist, error) {
	switch kind {
	case DailyMixes, DiscoveryMixes, NewReleaseMixes:
	default:
		return nil, fmt.Errorf("tidal: unknown mix kind %q", truncate(string(kind), 24))
	}
	path := "/" + string(kind) + "/me/relationships/items"
	q := url.Values{"include": {"items"}}
	if k.locale != "" {
		q.Set("locale", k.locale)
	}
	var out []Playlist
	next := ""
	for range maxMixPages {
		p, err := listPage(ctx, k, path, q, next, linked("playlists", playlistFrom))
		if err != nil {
			return nil, err
		}
		out = append(out, p.Items...)
		if p.Next == "" {
			break
		}
		next = p.Next
	}
	return out, nil
}

// TrackRadio returns the radio mix for a track. Its tracks load like any
// playlist's.
func (k *Catalog) TrackRadio(ctx context.Context, trackID string) (Playlist, error) {
	if !numericID(trackID) {
		return Playlist{}, fmt.Errorf("%w: track %q", ErrInvalidResourceID, truncate(trackID, 40))
	}
	// The track radio endpoint takes no countryCode.
	return k.radio(ctx, "/tracks/"+trackID+"/relationships/radio", url.Values{"include": {"radio"}})
}

// ArtistRadio returns the radio mix for an artist.
func (k *Catalog) ArtistRadio(ctx context.Context, artistID string) (Playlist, error) {
	if !numericID(artistID) {
		return Playlist{}, fmt.Errorf("%w: artist %q", ErrInvalidResourceID, truncate(artistID, 40))
	}
	return k.radio(ctx, "/artists/"+artistID+"/relationships/radio", k.catalogQuery("radio"))
}

func (k *Catalog) radio(ctx context.Context, path string, q url.Values) (Playlist, error) {
	p, err := listPage(ctx, k, path, q, "", linked("playlists", playlistFrom))
	if err != nil {
		return Playlist{}, err
	}
	if len(p.Items) == 0 {
		return Playlist{}, ErrNoRadio
	}
	return p.Items[0], nil
}

// SimilarTracks returns a page of tracks similar to a track.
func (k *Catalog) SimilarTracks(ctx context.Context, trackID, next string) (Page[Track], error) {
	if !numericID(trackID) {
		return Page[Track]{}, fmt.Errorf("%w: track %q", ErrInvalidResourceID, truncate(trackID, 40))
	}
	return listPage(ctx, k, "/tracks/"+trackID+"/relationships/similarTracks",
		k.catalogQuery("similarTracks.artists,similarTracks.albums"), next, linked("tracks", trackFrom))
}

// SimilarArtists returns a page of artists similar to an artist.
func (k *Catalog) SimilarArtists(ctx context.Context, artistID, next string) (Page[Artist], error) {
	if !numericID(artistID) {
		return Page[Artist]{}, fmt.Errorf("%w: artist %q", ErrInvalidResourceID, truncate(artistID, 40))
	}
	return listPage(ctx, k, "/artists/"+artistID+"/relationships/similarArtists",
		k.catalogQuery("similarArtists"), next, linked("artists", artistFrom))
}

// SimilarAlbums returns a page of albums similar to an album.
func (k *Catalog) SimilarAlbums(ctx context.Context, albumID, next string) (Page[Album], error) {
	if !numericID(albumID) {
		return Page[Album]{}, fmt.Errorf("%w: album %q", ErrInvalidResourceID, truncate(albumID, 40))
	}
	return listPage(ctx, k, "/albums/"+albumID+"/relationships/similarAlbums",
		k.catalogQuery("similarAlbums.artists"), next, linked("albums", albumFrom))
}

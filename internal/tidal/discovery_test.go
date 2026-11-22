package tidal

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Documents mirror the TASK-012 live probe: mixes and radios are playlists
// with playlistType MIX and a description, and no track count.
const (
	dailyMixesBody = `{"data":[{"id":"00237b2a1d39536f0ef81e836a9176","type":"playlists"},{"id":"0021479b963809ce1ec1d8385ab439","type":"playlists"}],
	 "included":[
	  {"id":"0021479b963809ce1ec1d8385ab439","type":"playlists","attributes":{"name":"My Mix 2","description":"Gorillaz, MGMT and more","playlistType":"MIX","accessType":"UNLISTED"}},
	  {"id":"00237b2a1d39536f0ef81e836a9176","type":"playlists","attributes":{"name":"My Mix 1","description":"Morcheeba and more","playlistType":"MIX"}}],
	 "links":{"self":"/userDailyMixes/me/relationships/items?include=items"}}`
	radioBody = `{"data":[{"id":"0002737adb8d077f4591c4cdb7ba60","type":"playlists"}],
	 "included":[{"id":"0002737adb8d077f4591c4cdb7ba60","type":"playlists","attributes":{"name":"Daft Punk","description":"Artist Radio","playlistType":"MIX"}}]}`
	similarTracksBody = `{"data":[{"id":"16023228","type":"tracks"}],
	 "included":[{"id":"16023228","type":"tracks","attributes":{"title":"Make Me Wanna","duration":"PT4M2S"},
	   "relationships":{"artists":{"data":[{"id":"9","type":"artists"}]}}},{"id":"9","type":"artists","attributes":{"name":"The Herbaliser"}}],
	 "links":{"next":"/tracks/6757405/relationships/similarTracks?countryCode=US&page%5Bcursor%5D=n2"}}`
	similarArtistsBody = `{"data":[{"id":"57425","type":"artists"}],"included":[{"id":"57425","type":"artists","attributes":{"name":"Justice"}}]}`
	similarAlbumsBody  = `{"data":[{"id":"124208","type":"albums"}],
	 "included":[{"id":"124208","type":"albums","attributes":{"title":"Cross","releaseDate":"2007-06-11","numberOfItems":12},
	   "relationships":{"artists":{"data":[{"id":"57425","type":"artists"}]}}},{"id":"57425","type":"artists","attributes":{"name":"Justice"}}]}`
)

var (
	mix1 = Playlist{ID: "00237b2a1d39536f0ef81e836a9176", Name: "My Mix 1", Description: "Morcheeba and more", Mix: true}
	mix2 = Playlist{ID: "0021479b963809ce1ec1d8385ab439", Name: "My Mix 2", Description: "Gorillaz, MGMT and more", Mix: true}
)

func TestMixes(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		kind    MixKind
		locale  string
		replies []reply
		want    []Playlist
		pages   int
	}{
		{name: "daily, linkage order", kind: DailyMixes, replies: []reply{{status: 200, body: dailyMixesBody}}, want: []Playlist{mix1, mix2}, pages: 1},
		{name: "empty discovery", kind: DiscoveryMixes, replies: []reply{{status: 200, body: `{"data":[],"links":{}}`}}, pages: 1},
		{name: "paged new releases with locale", kind: NewReleaseMixes, locale: "nb-NO", replies: []reply{
			{status: 200, body: `{"data":[{"id":"00237b2a1d39536f0ef81e836a9176","type":"playlists"}],"included":[{"id":"00237b2a1d39536f0ef81e836a9176","type":"playlists","attributes":{"name":"My Mix 1","description":"Morcheeba and more","playlistType":"MIX"}}],
			  "links":{"next":"/userNewReleaseMixes/me/relationships/items?include=items&page%5Bcursor%5D=c2"}}`},
			{status: 200, body: `{"data":[{"id":"0021479b963809ce1ec1d8385ab439","type":"playlists"}],"included":[{"id":"0021479b963809ce1ec1d8385ab439","type":"playlists","attributes":{"name":"My Mix 2","description":"Gorillaz, MGMT and more","playlistType":"MIX"}}]}`},
		}, want: []Playlist{mix1, mix2}, pages: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, f := newTestCatalog(t, tt.replies...)
			if tt.locale != "" {
				if err := k.SetLocale(tt.locale); err != nil {
					t.Fatal(err)
				}
			}
			got, err := k.Mixes(ctx, tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mixes = %+v, want %+v", got, tt.want)
			}
			reqs := f.requests()
			if len(reqs) != tt.pages {
				t.Fatalf("%d requests, want %d", len(reqs), tt.pages)
			}
			r := reqs[0]
			if r.path != "/v2/"+string(tt.kind)+"/me/relationships/items" || r.query.Get("include") != "items" ||
				r.query.Get("locale") != tt.locale || r.query.Has("countryCode") {
				t.Errorf("request = %s %v", r.path, r.query)
			}
			if tt.pages > 1 && reqs[1].query.Get("page[cursor]") != "c2" {
				t.Errorf("page 2 query = %v", reqs[1].query)
			}
		})
	}
}

func TestMixesStopsAfterMaxPages(t *testing.T) {
	loop := reply{status: 200, body: `{"data":[],"links":{"next":"/userDailyMixes/me/relationships/items?page%5Bcursor%5D=again"}}`}
	replies := make([]reply, maxMixPages+2)
	for i := range replies {
		replies[i] = loop
	}
	k, f := newTestCatalog(t, replies...)
	if _, err := k.Mixes(context.Background(), DailyMixes); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != maxMixPages {
		t.Errorf("%d requests, want %d", n, maxMixPages)
	}
}

func TestRadio(t *testing.T) {
	ctx := context.Background()
	wantRadio := Playlist{ID: "0002737adb8d077f4591c4cdb7ba60", Name: "Daft Punk", Description: "Artist Radio", Mix: true}
	tests := []struct {
		name    string
		call    func(k *Catalog) (Playlist, error)
		reply   *reply
		path    string
		country bool
		errIs   error
	}{
		{name: "track radio", call: func(k *Catalog) (Playlist, error) { return k.TrackRadio(ctx, "6757405") },
			reply: &reply{status: 200, body: radioBody}, path: "/v2/tracks/6757405/relationships/radio"},
		{name: "artist radio", call: func(k *Catalog) (Playlist, error) { return k.ArtistRadio(ctx, "8847") },
			reply: &reply{status: 200, body: radioBody}, path: "/v2/artists/8847/relationships/radio", country: true},
		{name: "no radio", call: func(k *Catalog) (Playlist, error) { return k.TrackRadio(ctx, "1") },
			reply: &reply{status: 200, body: `{"data":[]}`}, path: "/v2/tracks/1/relationships/radio", errIs: ErrNoRadio},
		{name: "api error", call: func(k *Catalog) (Playlist, error) { return k.ArtistRadio(ctx, "1") },
			reply: &reply{status: 404}, path: "/v2/artists/1/relationships/radio", country: true, errIs: ErrNotFound},
		{name: "bad track id", call: func(k *Catalog) (Playlist, error) { return k.TrackRadio(ctx, "1/../x") }, errIs: ErrInvalidResourceID},
		{name: "bad artist id", call: func(k *Catalog) (Playlist, error) { return k.ArtistRadio(ctx, "a") }, errIs: ErrInvalidResourceID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var replies []reply
			if tt.reply != nil {
				replies = append(replies, *tt.reply)
			}
			k, f := newTestCatalog(t, replies...)
			got, err := tt.call(k)
			if tt.errIs != nil {
				if !errors.Is(err, tt.errIs) {
					t.Fatalf("err = %v, want %v", err, tt.errIs)
				}
			} else if err != nil || got != wantRadio {
				t.Fatalf("radio = %+v, %v", got, err)
			}
			reqs := f.requests()
			if tt.reply == nil {
				if len(reqs) != 0 {
					t.Errorf("rejected input sent %d requests", len(reqs))
				}
				return
			}
			r := reqs[0]
			if r.path != tt.path || r.query.Get("include") != "radio" || r.query.Has("countryCode") != tt.country {
				t.Errorf("request = %s %v", r.path, r.query)
			}
		})
	}
}

func TestSimilar(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		body    string
		call    func(k *Catalog) (any, string, error)
		path    string
		include string
		want    any
		next    string
	}{
		{"tracks", similarTracksBody, func(k *Catalog) (any, string, error) {
			p, err := k.SimilarTracks(ctx, "6757405", "")
			return p.Items, p.Next, err
		}, "/v2/tracks/6757405/relationships/similarTracks", "similarTracks.artists,similarTracks.albums",
			[]Track{{ID: "16023228", Title: "Make Me Wanna", Artists: []string{"The Herbaliser"}, Duration: 242e9}},
			"/tracks/6757405/relationships/similarTracks?countryCode=US&page%5Bcursor%5D=n2"},
		{"artists", similarArtistsBody, func(k *Catalog) (any, string, error) {
			p, err := k.SimilarArtists(ctx, "8847", "")
			return p.Items, p.Next, err
		}, "/v2/artists/8847/relationships/similarArtists", "similarArtists", []Artist{{ID: "57425", Name: "Justice"}}, ""},
		{"albums", similarAlbumsBody, func(k *Catalog) (any, string, error) {
			p, err := k.SimilarAlbums(ctx, "1550545", "")
			return p.Items, p.Next, err
		}, "/v2/albums/1550545/relationships/similarAlbums", "similarAlbums.artists",
			[]Album{{ID: "124208", Title: "Cross", Artists: []string{"Justice"}, Year: "2007", Tracks: 12}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, f := newTestCatalog(t, reply{status: 200, body: tt.body})
			got, next, err := tt.call(k)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) || next != tt.next {
				t.Errorf("items = %+v next %q, want %+v %q", got, next, tt.want, tt.next)
			}
			r := f.requests()[0]
			if r.path != tt.path || r.query.Get("include") != tt.include || r.query.Get("countryCode") != "US" {
				t.Errorf("request = %s %v", r.path, r.query)
			}
		})
	}
	// The next page follows the link; bad IDs and foreign links send nothing.
	k, f := newTestCatalog(t, reply{status: 200, body: similarTracksBody})
	if _, err := k.SimilarTracks(ctx, "6757405", "/tracks/6757405/relationships/similarTracks?page%5Bcursor%5D=n2"); err != nil {
		t.Fatal(err)
	}
	if r := f.requests()[0]; r.query.Get("page[cursor]") != "n2" {
		t.Errorf("next query = %v", r.query)
	}
	k, f = newTestCatalog(t)
	for _, err := range []error{
		func() error { _, err := k.SimilarTracks(ctx, "x", ""); return err }(),
		func() error { _, err := k.SimilarArtists(ctx, "", ""); return err }(),
		func() error { _, err := k.SimilarAlbums(ctx, "1;2", ""); return err }(),
	} {
		if !errors.Is(err, ErrInvalidResourceID) {
			t.Errorf("err = %v, want ErrInvalidResourceID", err)
		}
	}
	if _, err := k.SimilarAlbums(ctx, "1", "/albums/2/relationships/similarAlbums"); err == nil {
		t.Error("a next link for another album was followed")
	}
	if len(f.requests()) != 0 {
		t.Errorf("rejected input sent %d requests", len(f.requests()))
	}
}

func TestDiscoveryInputChecks(t *testing.T) {
	k, f := newTestCatalog(t)
	if _, err := k.Mixes(context.Background(), "userOfflineMixes"); err == nil {
		t.Error("unknown mix kind accepted")
	}
	for tag, ok := range map[string]bool{
		"en-US": true, "nb": true, "fil-PH": true, "": false, "en_US": false, "EN-us": false,
		"en-USA": false, "e": false, "engl": false, "en-": false, "en-US&x=1": false,
	} {
		if err := k.SetLocale(tag); (err == nil) != ok {
			t.Errorf("SetLocale(%q) = %v, want ok %v", tag, err, ok)
		}
	}
	if k.locale != "fil-PH" && k.locale != "en-US" && k.locale != "nb" {
		t.Errorf("a rejected tag replaced the locale: %q", k.locale)
	}
	if len(f.requests()) != 0 {
		t.Error("input checks sent requests")
	}
}

func TestPlaylistFromMixFields(t *testing.T) {
	r := Resource{ID: "p", Type: "playlists", Attributes: []byte(`{"name":"N","description":"D","playlistType":"EDITORIAL","numberOfTrackItems":3}`)}
	p, err := playlistFrom(nil, r)
	if err != nil || p.Mix || !p.Editorial || p.Description != "D" || p.Tracks != 3 {
		t.Errorf("playlist = %+v, %v", p, err)
	}
}

func TestMixesError(t *testing.T) {
	k, _ := newTestCatalog(t, reply{status: 401}, reply{status: 401})
	if _, err := k.Mixes(context.Background(), DailyMixes); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

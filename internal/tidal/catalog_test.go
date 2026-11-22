package tidal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// searchBody mirrors the live /searchResults shape (TASK-010 probe): one
// searchResults resource whose relationships link to included resources,
// with nested includes for track and album artists.
const searchBody = `{
 "data": [{"id": "LE1Y", "type": "searchResults", "attributes": {"query": "daft punk"},
  "relationships": {
   "tracks": {"data": [{"id": "1550546", "type": "tracks"}, {"id": "999", "type": "tracks"}, {"id": "2", "type": "tracks"}],
     "links": {"self": "/searchResults/LE1Y/relationships/tracks", "next": "/searchResults/LE1Y/relationships/tracks?page%5Bcursor%5D=x", "meta": {"nextCursor": "x"}}},
   "albums": {"data": [{"id": "1550545", "type": "albums"}]},
   "artists": {"data": [{"id": "8847", "type": "artists"}]},
   "playlists": {"data": [{"id": "0a76-f6", "type": "playlists"}, {"id": "u-1", "type": "playlists"}]}
  }}],
 "included": [
  {"id": "1550546", "type": "tracks", "attributes": {"title": "One More Time", "version": null, "duration": "PT5M20S", "explicit": false},
   "relationships": {"albums": {"data": [{"id": "1550545", "type": "albums"}]}, "artists": {"data": [{"id": "8847", "type": "artists"}, {"id": "404", "type": "artists"}]}}},
  {"id": "2", "type": "tracks", "attributes": {"title": "Get Lucky", "version": "Radio Edit", "duration": "bogus", "explicit": true}},
  {"id": "1550545", "type": "albums", "attributes": {"title": "Discovery", "releaseDate": "2001-03-12", "numberOfItems": 14, "duration": "PT1H1M9S"},
   "relationships": {"artists": {"data": [{"id": "8847", "type": "artists"}]}}},
  {"id": "8847", "type": "artists", "attributes": {"name": "Daft Punk"}},
  {"id": "0a76-f6", "type": "playlists", "attributes": {"name": "Daft Punk Essentials", "numberOfTrackItems": 40, "duration": "PT2H", "playlistType": "EDITORIAL"}},
  {"id": "u-1", "type": "playlists", "attributes": {"name": "Mine", "numberOfTrackItems": 3, "playlistType": "USER"}}
 ],
 "links": {"self": "/searchResults?filter%5Bquery%5D=daft+punk"}
}`

var (
	oneMoreTime = Track{ID: "1550546", Title: "One More Time", Artists: []string{"Daft Punk"}, AlbumID: "1550545",
		Album: "Discovery", Duration: 5*time.Minute + 20*time.Second}
	getLucky  = Track{ID: "2", Title: "Get Lucky", Version: "Radio Edit", Explicit: true}
	discovery = Album{ID: "1550545", Title: "Discovery", Artists: []string{"Daft Punk"}, Year: "2001", Tracks: 14,
		Duration: time.Hour + time.Minute + 9*time.Second}
)

func newTestCatalog(t *testing.T, replies ...reply) (*Catalog, *fakeAPI) {
	t.Helper()
	f := newFakeAPI(t, replies...)
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
	k, err := NewCatalog(c, "US")
	if err != nil {
		t.Fatal(err)
	}
	return k, f
}

func TestNewCatalog(t *testing.T) {
	c, err := New(&stubTokens{token: tokenA})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		client  *Client
		country string
		ok      bool
	}{
		{"valid", c, "US", true},
		{"nil client", nil, "US", false},
		{"lowercase", c, "us", false},
		{"too long", c, "USA", false},
		{"empty", c, "", false},
		{"first not letter", c, "1S", false},
		{"injection", c, "U&", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCatalog(tt.client, tt.country)
			if (err == nil) != tt.ok {
				t.Fatalf("NewCatalog(%q) err = %v, want ok=%v", tt.country, err, tt.ok)
			}
		})
	}
}

func TestSearch(t *testing.T) {
	k, f := newTestCatalog(t, reply{status: 200, body: searchBody})

	got, err := k.Search(context.Background(), "  daft punk ")
	if err != nil {
		t.Fatal(err)
	}

	want := SearchResults{
		// Track 999 is linked but not included, so it is skipped.
		Tracks:  []Track{oneMoreTime, getLucky},
		Albums:  []Album{discovery},
		Artists: []Artist{{ID: "8847", Name: "Daft Punk"}},
		Playlists: []Playlist{
			{ID: "0a76-f6", Name: "Daft Punk Essentials", Editorial: true, Tracks: 40, Duration: 2 * time.Hour},
			{ID: "u-1", Name: "Mine", Tracks: 3},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Search() =\n%+v\nwant\n%+v", got, want)
	}
	req := f.requests()[0]
	if req.path != "/v2/searchResults" {
		t.Errorf("path = %s", req.path)
	}
	for k, v := range map[string]string{"filter[query]": "daft punk", "countryCode": "US", "include": searchInclude} {
		if req.query.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, req.query.Get(k), v)
		}
	}
}

func TestSearchErrors(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		replies []reply
		errIs   error
		want    SearchResults
	}{
		{name: "empty query", query: "   ", errIs: ErrInvalidQuery},
		{name: "too long", query: strings.Repeat("é", MaxQueryLen+1), errIs: ErrInvalidQuery},
		{name: "invalid utf8", query: "a\xffb", errIs: ErrInvalidQuery},
		{name: "max length ok", query: strings.Repeat("é", MaxQueryLen), replies: []reply{{status: 200, body: `{"data":[]}`}}},
		{name: "no results resource", query: "x", replies: []reply{{status: 200, body: `{"data":[]}`}}},
		{name: "null data", query: "x", replies: []reply{{status: 200, body: `{"data":null}`}}},
		{name: "single resource", query: "x", replies: []reply{{status: 200, body: `{"data":{"id":"1","type":"searchResults"}}`}}, errIs: ErrNoData},
		{name: "not found", query: "x", replies: []reply{{status: 404}}, errIs: ErrNotFound},
		{name: "bad linkage", query: "x", replies: []reply{{status: 200, body: `{"data":[{"id":"1","type":"searchResults","relationships":{"tracks":{"data":5}}}]}`}}},
		{name: "bad track", query: "x", replies: []reply{{status: 200, body: badIncluded("tracks", `{"title":5}`)}}},
		{name: "bad album", query: "x", replies: []reply{{status: 200, body: badIncluded("albums", `{"title":5}`)}}},
		{name: "bad artist", query: "x", replies: []reply{{status: 200, body: badIncluded("artists", `{"name":5}`)}}},
		{name: "bad playlist", query: "x", replies: []reply{{status: 200, body: badIncluded("playlists", `{"name":5}`)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, f := newTestCatalog(t, tt.replies...)
			got, err := k.Search(context.Background(), tt.query)
			if len(tt.replies) == 0 && len(f.requests()) != 0 {
				t.Errorf("invalid query sent %d requests", len(f.requests()))
			}
			wantErr := tt.errIs != nil || strings.HasPrefix(tt.name, "bad")
			if !wantErr {
				if err != nil || !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("Search() = %+v, %v", got, err)
				}
				return
			}
			if err == nil || (tt.errIs != nil && !errors.Is(err, tt.errIs)) {
				t.Fatalf("err = %v, want %v", err, tt.errIs)
			}
		})
	}
}

// badIncluded is a search document with one included resource of typ whose
// attributes are attrs.
func badIncluded(typ, attrs string) string {
	return `{"data":[{"id":"s","type":"searchResults","relationships":{"` + typ + `":{"data":[{"id":"1","type":"` + typ + `"}]}}}],
		"included":[{"id":"1","type":"` + typ + `","attributes":` + attrs + `}]}`
}

// itemsBody mirrors the live album and playlist items shape: identifiers
// with item meta in data, tracks (and a video) in included.
const itemsBody = `{
 "data": [{"id": "1550546", "type": "tracks", "meta": {"volumeNumber": 1, "trackNumber": 1}},
          {"id": "77", "type": "videos", "meta": {"itemId": "v"}},
          {"id": "2", "type": "tracks", "meta": {"volumeNumber": 1, "trackNumber": 2}}],
 "included": [
  {"id": "1550546", "type": "tracks", "attributes": {"title": "One More Time", "duration": "PT5M20S"},
   "relationships": {"albums": {"data": [{"id": "1550545", "type": "albums"}]}, "artists": {"data": [{"id": "8847", "type": "artists"}]}}},
  {"id": "77", "type": "videos", "attributes": {"title": "A video"}},
  {"id": "2", "type": "tracks", "attributes": {"title": "Get Lucky", "version": "Radio Edit", "explicit": true}},
  {"id": "1550545", "type": "albums", "attributes": {"title": "Discovery"}},
  {"id": "8847", "type": "artists", "attributes": {"name": "Daft Punk"}}
 ],
 "links": {"self": "/x", "next": "%NEXT%"}
}`

func items(next string) string { return strings.Replace(itemsBody, "%NEXT%", next, 1) }

func TestTrackPages(t *testing.T) {
	type call func(k *Catalog, id, next string) (Page[Track], error)
	ctx := context.Background()
	tests := []struct {
		name    string
		call    call
		id      string
		path    string
		include string
		extra   map[string]string
	}{
		{"album", func(k *Catalog, id, n string) (Page[Track], error) { return k.AlbumTracks(ctx, id, n) },
			"1550545", "/albums/1550545/relationships/items", "items.artists,items.albums", nil},
		{"playlist", func(k *Catalog, id, n string) (Page[Track], error) { return k.PlaylistTracks(ctx, id, n) },
			"0a7602d2-f658-44d6", "/playlists/0a7602d2-f658-44d6/relationships/items", "items.artists,items.albums", nil},
		{"artist", func(k *Catalog, id, n string) (Page[Track], error) { return k.ArtistTracks(ctx, id, n) },
			"8847", "/artists/8847/relationships/tracks", "tracks.artists,tracks.albums", map[string]string{"collapseBy": "FINGERPRINT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := tt.path + "?countryCode=US&page%5Bcursor%5D=c2"
			k, f := newTestCatalog(t, reply{status: 200, body: items(next)}, reply{status: 200, body: items("")})

			p1, err := tt.call(k, tt.id, "")
			if err != nil {
				t.Fatal(err)
			}
			want := []Track{{ID: "1550546", Title: "One More Time", Artists: []string{"Daft Punk"}, AlbumID: "1550545",
				Album: "Discovery", Duration: 5*time.Minute + 20*time.Second}, getLucky}
			if !reflect.DeepEqual(p1.Items, want) || p1.Next != next {
				t.Fatalf("page 1 = %+v next %q", p1.Items, p1.Next)
			}
			p2, err := tt.call(k, tt.id, p1.Next)
			if err != nil || p2.Next != "" || len(p2.Items) != 2 {
				t.Fatalf("page 2 = %+v, %v", p2, err)
			}

			reqs := f.requests()
			if reqs[0].path != "/v2"+tt.path || reqs[0].query.Get("countryCode") != "US" || reqs[0].query.Get("include") != tt.include {
				t.Errorf("first request = %s %v", reqs[0].path, reqs[0].query)
			}
			for k, v := range tt.extra {
				if reqs[0].query.Get(k) != v {
					t.Errorf("query %s = %q, want %q", k, reqs[0].query.Get(k), v)
				}
			}
			if reqs[1].path != "/v2"+tt.path || reqs[1].query.Get("page[cursor]") != "c2" {
				t.Errorf("next request = %s %v", reqs[1].path, reqs[1].query)
			}
		})
	}
}

func TestArtistAlbums(t *testing.T) {
	body := `{"data":[{"id":"1550545","type":"albums"}],"included":[
		{"id":"1550545","type":"albums","attributes":{"title":"Discovery","releaseDate":"2001-03-12","numberOfItems":14,"duration":"PT1H1M9S"},
		 "relationships":{"artists":{"data":[{"id":"8847","type":"artists"}]}}},
		{"id":"8847","type":"artists","attributes":{"name":"Daft Punk"}}],
		"links":{"next":"/artists/8847/relationships/albums?page%5Bcursor%5D=n"}}`
	k, f := newTestCatalog(t, reply{status: 200, body: body})

	got, err := k.ArtistAlbums(context.Background(), "8847", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Items, []Album{discovery}) || got.Next != "/artists/8847/relationships/albums?page%5Bcursor%5D=n" {
		t.Errorf("ArtistAlbums() = %+v", got)
	}
	if r := f.requests()[0]; r.path != "/v2/artists/8847/relationships/albums" || r.query.Get("include") != "albums.artists" {
		t.Errorf("request = %s %v", r.path, r.query)
	}
}

func TestPageErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		call    func(k *Catalog) error
		replies []reply
		errIs   error
	}{
		{name: "album id not numeric", call: func(k *Catalog) error { _, err := k.AlbumTracks(ctx, "1/../users", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "album id empty", call: func(k *Catalog) error { _, err := k.AlbumTracks(ctx, "", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "artist tracks id", call: func(k *Catalog) error { _, err := k.ArtistTracks(ctx, "x", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "artist albums id", call: func(k *Catalog) error { _, err := k.ArtistAlbums(ctx, "x", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "playlist id slash", call: func(k *Catalog) error { _, err := k.PlaylistTracks(ctx, "a/b", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "playlist id query", call: func(k *Catalog) error { _, err := k.PlaylistTracks(ctx, "a?b", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "playlist id empty", call: func(k *Catalog) error { _, err := k.PlaylistTracks(ctx, "", ""); return err }, errIs: ErrInvalidResourceID},
		{name: "playlist id too long", call: func(k *Catalog) error { _, err := k.PlaylistTracks(ctx, strings.Repeat("a", 65), ""); return err }, errIs: ErrInvalidResourceID},
		{name: "next for another path", call: func(k *Catalog) error {
			_, err := k.AlbumTracks(ctx, "1", "/users/me?x=1")
			return err
		}},
		{name: "next absolute", call: func(k *Catalog) error {
			_, err := k.AlbumTracks(ctx, "1", "https://evil.example/albums/1/relationships/items")
			return err
		}},
		{name: "next unparsable", call: func(k *Catalog) error { _, err := k.AlbumTracks(ctx, "1", "%zz"); return err }},
		{name: "api error", call: func(k *Catalog) error { _, err := k.AlbumTracks(ctx, "1", ""); return err },
			replies: []reply{{status: 404}}, errIs: ErrNotFound},
		{name: "bad linkage", call: func(k *Catalog) error { _, err := k.AlbumTracks(ctx, "1", ""); return err },
			replies: []reply{{status: 200, body: `{"data":{"id":"1","type":"tracks"}}`}}},
		{name: "bad track attributes", call: func(k *Catalog) error { _, err := k.ArtistTracks(ctx, "1", ""); return err },
			replies: []reply{{status: 200, body: `{"data":[{"id":"1","type":"tracks"}],"included":[{"id":"1","type":"tracks","attributes":{"title":1}}]}`}}},
		{name: "bad album attributes", call: func(k *Catalog) error { _, err := k.ArtistAlbums(ctx, "1", ""); return err },
			replies: []reply{{status: 200, body: `{"data":[{"id":"1","type":"albums"}],"included":[{"id":"1","type":"albums","attributes":{"title":1}}]}`}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, f := newTestCatalog(t, tt.replies...)
			err := tt.call(k)
			if err == nil || (tt.errIs != nil && !errors.Is(err, tt.errIs)) {
				t.Fatalf("err = %v, want %v", err, tt.errIs)
			}
			if len(tt.replies) == 0 && len(f.requests()) != 0 {
				t.Errorf("rejected input sent %d requests", len(f.requests()))
			}
		})
	}
}

func TestTrackFullTitle(t *testing.T) {
	if got := oneMoreTime.FullTitle(); got != "One More Time" {
		t.Errorf("FullTitle() = %q", got)
	}
	if got := getLucky.FullTitle(); got != "Get Lucky (Radio Edit)" {
		t.Errorf("FullTitle() = %q", got)
	}
}

func TestAlbumNameUndecodable(t *testing.T) {
	// A track whose included album has malformed attributes keeps the album
	// ID and drops the title, rather than failing the page.
	body := `{"data":[{"id":"1","type":"tracks"}],"included":[
		{"id":"1","type":"tracks","attributes":{"title":"T"},"relationships":{"albums":{"data":[{"id":"9","type":"albums"}]},
		 "artists":{"data":[{"id":"5","type":"artists"},{"id":"6","type":"artists"}]}}},
		{"id":"9","type":"albums","attributes":{"title":7}},
		{"id":"5","type":"artists","attributes":{"name":3}},
		{"id":"6","type":"artists","attributes":{"name":""}}]}`
	k, _ := newTestCatalog(t, reply{status: 200, body: body})
	got, err := k.ArtistTracks(context.Background(), "1", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Track{{ID: "1", Title: "T", AlbumID: "9"}}; !reflect.DeepEqual(got.Items, want) {
		t.Errorf("items = %+v, want %+v", got.Items, want)
	}
}

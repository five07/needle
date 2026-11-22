package tidal

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// Collection documents mirror the live TASK-011 probe: identifiers with
// meta.addedAt in data, the items (and nested artists/albums) in included.
const (
	favTracksBody = `{"data":[{"id":"161011","type":"tracks","meta":{"addedAt":"2026-11-20T00:44:33Z"}}],
	 "included":[{"id":"161011","type":"tracks","attributes":{"title":"Around the World","duration":"PT7M9S"},
	   "relationships":{"artists":{"data":[{"id":"8847","type":"artists"}]},"albums":{"data":[{"id":"161010","type":"albums"}]}}},
	  {"id":"8847","type":"artists","attributes":{"name":"Daft Punk"}},
	  {"id":"161010","type":"albums","attributes":{"title":"Homework"}}],
	 "links":{"next":"/userCollectionTracks/me/relationships/items?include=items.artists&page%5Bcursor%5D=eyJ"}}`
	favAlbumsBody = `{"data":[{"id":"1550545","type":"albums","meta":{"addedAt":"2026-11-20T00:49:23Z"}}],
	 "included":[{"id":"1550545","type":"albums","attributes":{"title":"Discovery","releaseDate":"2001-03-12","numberOfItems":14},
	   "relationships":{"artists":{"data":[{"id":"8847","type":"artists"}]}}},{"id":"8847","type":"artists","attributes":{"name":"Daft Punk"}}]}`
	favArtistsBody   = `{"data":[{"id":"8847","type":"artists"}],"included":[{"id":"8847","type":"artists","attributes":{"name":"Daft Punk"}}]}`
	favPlaylistsBody = `{"data":[{"id":"c72e-7e0b","type":"playlists"},{"id":"f00","type":"folders"}],
	 "included":[{"id":"c72e-7e0b","type":"playlists","attributes":{"name":"Road trip","numberOfTrackItems":12,"playlistType":"USER"}}]}`
	// OwnedPlaylists returns full resources as primary data.
	ownedBody = `{"data":[{"id":"6f76-cfa5","type":"playlists","attributes":{"name":"New","numberOfTrackItems":1,"playlistType":"USER"}},
	  {"id":"x","type":"folders","attributes":{}}],
	 "links":{"next":"/playlists?filter%5Bowners.id%5D=me&page%5Bcursor%5D=n2"}}`
)

func TestFavoritesLists(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		body    string
		call    func(k *Catalog) (any, string, error)
		path    string
		include string
		sort    string
		view    string
		want    any
	}{
		{"tracks", favTracksBody, func(k *Catalog) (any, string, error) {
			p, err := k.FavoriteTracks(ctx, SortAdded, "")
			return p.Items, p.Next, err
		}, "/v2/userCollectionTracks/me/relationships/items", "items.artists,items.albums", "-addedAt", "",
			[]Track{{ID: "161011", Title: "Around the World", Artists: []string{"Daft Punk"}, AlbumID: "161010", Album: "Homework", Duration: 429e9}}},
		{"albums by artist", favAlbumsBody, func(k *Catalog) (any, string, error) {
			p, err := k.FavoriteAlbums(ctx, SortArtist, "")
			return p.Items, p.Next, err
		}, "/v2/userCollectionAlbums/me/relationships/items", "items.artists", "artists.name", "",
			[]Album{{ID: "1550545", Title: "Discovery", Artists: []string{"Daft Punk"}, Year: "2001", Tracks: 14}}},
		{"artists by name", favArtistsBody, func(k *Catalog) (any, string, error) {
			p, err := k.FavoriteArtists(ctx, SortName, "")
			return p.Items, p.Next, err
		}, "/v2/userCollectionArtists/me/relationships/items", "items", "name", "", []Artist{{ID: "8847", Name: "Daft Punk"}}},
		{"playlists flat, folders skipped", favPlaylistsBody, func(k *Catalog) (any, string, error) {
			p, err := k.FavoritePlaylists(ctx, SortAdded, "")
			return p.Items, p.Next, err
		}, "/v2/userCollectionPlaylists/me/relationships/items", "items", "-addedAt", "FLAT",
			[]Playlist{{ID: "c72e-7e0b", Name: "Road trip", Tracks: 12}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, f := newTestCatalog(t, reply{status: 200, body: tt.body})
			got, _, err := tt.call(k)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("items = %+v, want %+v", got, tt.want)
			}
			r := f.requests()[0]
			if r.path != tt.path || r.query.Get("include") != tt.include || r.query.Get("sort") != tt.sort ||
				r.query.Get("collectionView") != tt.view || r.query.Has("countryCode") {
				t.Errorf("request = %s %v", r.path, r.query)
			}
		})
	}
}

func TestFavoritesPagingAndSorts(t *testing.T) {
	k, f := newTestCatalog(t, reply{status: 200, body: favTracksBody}, reply{status: 200, body: favArtistsBody})
	p, err := k.FavoriteTracks(context.Background(), SortTitle, "")
	if err != nil {
		t.Fatal(err)
	}
	// The second reply holds artists, not tracks: a valid, empty page.
	if p2, err := k.FavoriteTracks(context.Background(), SortTitle, p.Next); err != nil || len(p2.Items) != 0 {
		t.Fatalf("page 2 = %+v, %v", p2, err)
	}
	if r := f.requests()[1]; r.path != "/v2/userCollectionTracks/me/relationships/items" || r.query.Get("page[cursor]") != "eyJ" {
		t.Errorf("next request = %s %v", r.path, r.query)
	}
	for c, want := range map[Collection][]Sort{
		FavTracks: {SortAdded, SortTitle, SortArtist}, FavAlbums: {SortAdded, SortTitle, SortArtist},
		FavArtists: {SortAdded, SortName}, FavPlaylists: {SortAdded, SortName}, "videos": nil,
	} {
		if got := Sorts(c); !reflect.DeepEqual(got, want) {
			t.Errorf("Sorts(%s) = %v", c, got)
		}
	}
}

func TestFavoritesErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		call    func(k *Catalog) error
		replies []reply
		errIs   error
	}{
		{"unsupported sort", func(k *Catalog) error { _, err := k.FavoriteArtists(ctx, SortTitle, ""); return err }, nil, ErrInvalidSort},
		{"injected sort", func(k *Catalog) error { _, err := k.FavoriteTracks(ctx, "title&x=1", ""); return err }, nil, ErrInvalidSort},
		{"album sort", func(k *Catalog) error { _, err := k.FavoriteAlbums(ctx, SortName, ""); return err }, nil, ErrInvalidSort},
		{"playlist sort", func(k *Catalog) error { _, err := k.FavoritePlaylists(ctx, SortTitle, ""); return err }, nil, ErrInvalidSort},
		{"foreign next link", func(k *Catalog) error { _, err := k.FavoriteTracks(ctx, SortAdded, "/users/me"); return err }, nil, nil},
		{"owned foreign next", func(k *Catalog) error { _, err := k.OwnedPlaylists(ctx, "/userCollectionTracks/me"); return err }, nil, nil},
		{"api error", func(k *Catalog) error { _, err := k.FavoriteAlbums(ctx, SortAdded, ""); return err },
			[]reply{{status: 401}, {status: 401}}, ErrUnauthorized},
		{"owned not a list", func(k *Catalog) error { _, err := k.OwnedPlaylists(ctx, ""); return err },
			[]reply{{status: 200, body: `{"data":{"id":"1","type":"playlists"}}`}}, ErrNoData},
		{"owned bad attributes", func(k *Catalog) error { _, err := k.OwnedPlaylists(ctx, ""); return err },
			[]reply{{status: 200, body: `{"data":[{"id":"1","type":"playlists","attributes":{"name":1}}]}`}}, nil},
		{"bad included item", func(k *Catalog) error { _, err := k.FavoriteArtists(ctx, SortAdded, ""); return err },
			[]reply{{status: 200, body: `{"data":[{"id":"1","type":"artists"}],"included":[{"id":"1","type":"artists","attributes":{"name":1}}]}`}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, f := newTestCatalog(t, tt.replies...)
			err := tt.call(k)
			if err == nil || (tt.errIs != nil && !errors.Is(err, tt.errIs)) {
				t.Fatalf("err = %v, want %v", err, tt.errIs)
			}
			if tt.replies == nil && len(f.requests()) != 0 {
				t.Errorf("rejected input sent %d requests", len(f.requests()))
			}
		})
	}
}

func TestOwnedPlaylists(t *testing.T) {
	k, f := newTestCatalog(t, reply{status: 200, body: ownedBody})
	p, err := k.OwnedPlaylists(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Playlist{{ID: "6f76-cfa5", Name: "New", Tracks: 1}}; !reflect.DeepEqual(p.Items, want) {
		t.Errorf("items = %+v", p.Items)
	}
	if p.Next != "/playlists?filter%5Bowners.id%5D=me&page%5Bcursor%5D=n2" {
		t.Errorf("next = %q", p.Next)
	}
	r := f.requests()[0]
	if r.path != "/v2/playlists" || r.query.Get("filter[owners.id]") != "me" || r.query.Get("countryCode") != "US" {
		t.Errorf("request = %s %v", r.path, r.query)
	}
}

// body decodes a recorded JSON request body.
func body(t *testing.T, r recorded) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.body), &m); err != nil {
		t.Fatalf("request body %q: %v", r.body, err)
	}
	return m
}

func TestFavoriteWrites(t *testing.T) {
	ctx := context.Background()
	dup := `{"errors":[{"status":"409","code":"DUPLICATE_ITEMS_IN_COLLECTION","detail":"Collection already contains one or more items"}]}`
	full := `{"errors":[{"status":"409","code":"TOO_MANY_ITEMS_IN_COLLECTION"}]}`
	tests := []struct {
		name   string
		add    bool
		c      Collection
		id     string
		reply  *reply
		method string
		path   string
		errIs  error
		ok     bool
	}{
		{name: "add track", add: true, c: FavTracks, id: "161011", reply: &reply{status: 200},
			method: "POST", path: "/v2/userCollectionTracks/me/relationships/items", ok: true},
		{name: "add album already favorite", add: true, c: FavAlbums, id: "1", reply: &reply{status: 409, body: dup},
			method: "POST", path: "/v2/userCollectionAlbums/me/relationships/items", ok: true},
		{name: "add when full", add: true, c: FavArtists, id: "1", reply: &reply{status: 409, body: full},
			method: "POST", path: "/v2/userCollectionArtists/me/relationships/items", errIs: ErrCollectionFull},
		{name: "remove playlist", c: FavPlaylists, id: "c72e-7e0b", reply: &reply{status: 200},
			method: "DELETE", path: "/v2/userCollectionPlaylists/me/relationships/items", ok: true},
		{name: "remove error", c: FavTracks, id: "1", reply: &reply{status: 404},
			method: "DELETE", path: "/v2/userCollectionTracks/me/relationships/items", errIs: ErrNotFound},
		{name: "bad track id", add: true, c: FavTracks, id: "1/../x", errIs: ErrInvalidResourceID},
		{name: "bad playlist id", c: FavPlaylists, id: "a?b", errIs: ErrInvalidResourceID},
		{name: "unknown collection", add: true, c: "videos", id: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var replies []reply
			if tt.reply != nil {
				replies = append(replies, *tt.reply)
			}
			k, f := newTestCatalog(t, replies...)
			var err error
			if tt.add {
				err = k.AddFavorite(ctx, tt.c, tt.id)
			} else {
				err = k.RemoveFavorite(ctx, tt.c, tt.id)
			}
			if tt.ok != (err == nil) || (tt.errIs != nil && !errors.Is(err, tt.errIs)) {
				t.Fatalf("err = %v, want ok=%v is %v", err, tt.ok, tt.errIs)
			}
			reqs := f.requests()
			if tt.reply == nil {
				if len(reqs) != 0 {
					t.Errorf("rejected input sent %d requests", len(reqs))
				}
				return
			}
			r := reqs[0]
			if r.method != tt.method || r.path != tt.path || r.header.Get("Idempotency-Key") == "" {
				t.Errorf("request = %s %s idem %q", r.method, r.path, r.header.Get("Idempotency-Key"))
			}
			want := map[string]any{"data": []any{map[string]any{"id": tt.id, "type": string(tt.c)}}}
			if got := body(t, r); !reflect.DeepEqual(got, want) {
				t.Errorf("body = %v, want %v", got, want)
			}
		})
	}
}

func TestAddToPlaylist(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		playlist string
		track    string
		reply    *reply
		errIs    error
		ok       bool
	}{
		{name: "added", playlist: "6f76-cfa5", track: "161011", reply: &reply{status: 200}, ok: true},
		{name: "duplicate", playlist: "6f76-cfa5", track: "161011",
			reply: &reply{status: 409, body: `{"errors":[{"status":"409","code":"DUPLICATE_ITEMS_IN_PLAYLIST"}]}`}, errIs: ErrDuplicateInPlaylist},
		{name: "full", playlist: "6f76-cfa5", track: "1",
			reply: &reply{status: 409, body: `{"errors":[{"status":"409","code":"TOO_MANY_ITEMS_IN_PLAYLIST"}]}`}, errIs: ErrPlaylistFull},
		{name: "not owned", playlist: "6f76-cfa5", track: "1", reply: &reply{status: 404}, errIs: ErrNotFound},
		{name: "bad playlist id", playlist: "../me", track: "1", errIs: ErrInvalidResourceID},
		{name: "bad track id", playlist: "p", track: "x", errIs: ErrInvalidResourceID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var replies []reply
			if tt.reply != nil {
				replies = append(replies, *tt.reply)
			}
			k, f := newTestCatalog(t, replies...)
			err := k.AddToPlaylist(ctx, tt.playlist, tt.track)
			if tt.ok != (err == nil) || (tt.errIs != nil && !errors.Is(err, tt.errIs)) {
				t.Fatalf("err = %v, want %v", err, tt.errIs)
			}
			reqs := f.requests()
			if tt.reply == nil {
				if len(reqs) != 0 {
					t.Errorf("rejected input sent %d requests", len(reqs))
				}
				return
			}
			r := reqs[0]
			if r.method != "POST" || r.path != "/v2/playlists/"+tt.playlist+"/relationships/items" {
				t.Errorf("request = %s %s", r.method, r.path)
			}
			want := map[string]any{
				"data": []any{map[string]any{"id": tt.track, "type": "tracks"}},
				"meta": map[string]any{"onDuplicates": "FAIL"},
			}
			if got := body(t, r); !reflect.DeepEqual(got, want) {
				t.Errorf("body = %v", got)
			}
		})
	}
}

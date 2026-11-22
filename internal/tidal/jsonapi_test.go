package tidal

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// albumDoc mirrors a live GET /albums/{id}?include=artists,items response.
const albumDoc = `{
  "data": {
    "id": "1550545", "type": "albums",
    "attributes": {"title": "OK Computer", "numberOfItems": 2},
    "relationships": {
      "artists": {"data": [{"id": "8847", "type": "artists"}], "links": {"self": "/albums/1550545/relationships/artists"}},
      "items": {"data": [
        {"id": "1550546", "type": "tracks", "meta": {"volumeNumber": 1, "trackNumber": 1}},
        {"id": "1550547", "type": "tracks", "meta": {"volumeNumber": 1, "trackNumber": 2}},
        {"id": "9999999", "type": "tracks", "meta": {"volumeNumber": 1, "trackNumber": 3}}
      ]},
      "coverArt": {"data": {"id": "c1", "type": "artworks"}},
      "owners": {"data": null},
      "similarAlbums": {"links": {"self": "/albums/1550545/relationships/similarAlbums"}}
    }
  },
  "included": [
    {"id": "8847", "type": "artists", "attributes": {"name": "Radiohead"}},
    {"id": "1550546", "type": "tracks", "attributes": {"title": "Airbag", "duration": "PT4M44S"}},
    {"id": "1550547", "type": "tracks", "attributes": {"title": "Paranoid Android"}},
    {"id": "c1", "type": "artworks", "attributes": {}}
  ],
  "links": {"self": {"href": "/albums/1550545"}}
}`

func decodeDoc(t *testing.T, s string) *Document {
	t.Helper()
	var d Document
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestDocumentResolvesIncludedResources(t *testing.T) {
	doc := decodeDoc(t, albumDoc)
	album, err := doc.One()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Links.Self != "/albums/1550545" {
		t.Errorf("object-form self link = %q", doc.Links.Self)
	}

	type albumAttrs struct {
		Title         string `json:"title"`
		NumberOfItems int    `json:"numberOfItems"`
	}
	attrs, err := DecodeAttributes[albumAttrs](album)
	if err != nil || attrs.Title != "OK Computer" || attrs.NumberOfItems != 2 {
		t.Errorf("attrs = %+v, %v", attrs, err)
	}

	tracks, missing, err := doc.Resolve(album.Relationships["items"])
	if err != nil {
		t.Fatal(err)
	}
	type trackAttrs struct {
		Title string `json:"title"`
	}
	var titles []string
	for _, tr := range tracks {
		a, err := DecodeAttributes[trackAttrs](tr)
		if err != nil {
			t.Fatal(err)
		}
		titles = append(titles, a.Title)
	}
	if got := strings.Join(titles, "|"); got != "Airbag|Paranoid Android" {
		t.Errorf("resolved titles = %q", got)
	}
	if len(missing) != 1 || missing[0].ID != "9999999" {
		t.Errorf("missing = %+v, want the one non-included track", missing)
	}

	ids, err := album.Relationships["items"].Identifiers()
	if err != nil {
		t.Fatal(err)
	}
	type itemMeta struct {
		TrackNumber int `json:"trackNumber"`
	}
	m, err := DecodeMeta[itemMeta](ids[1].Meta)
	if err != nil || m.TrackNumber != 2 {
		t.Errorf("item meta = %+v, %v", m, err)
	}

	tests := []struct {
		rel       string
		wantFound []string
	}{
		{"artists", []string{"8847"}},
		{"coverArt", []string{"c1"}}, // single-object linkage
		{"owners", nil},              // null linkage
		{"similarAlbums", nil},       // links only, no data
		{"absent", nil},              // relationship not present
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			found, missing, err := doc.Resolve(album.Relationships[tt.rel])
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range found {
				got = append(got, r.ID)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFound, ",") || len(missing) != 0 {
				t.Errorf("found = %v, missing = %v; want %v", got, missing, tt.wantFound)
			}
		})
	}

	if _, ok := doc.Find("artists", "nope"); ok {
		t.Error("Find returned a resource that is not included")
	}
	if r, ok := doc.Find("tracks", "1550547"); !ok || r.ID != "1550547" {
		t.Errorf("Find(tracks, 1550547) = %+v, %v", r, ok)
	}
}

func TestDocumentShapes(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		oneErr   error
		manyErr  error
		manyLen  int
		oneID    string
		wantBadJ bool
	}{
		{name: "single resource", body: `{"data":{"id":"1","type":"users"}}`, oneID: "1", manyErr: ErrNoData},
		{name: "array", body: `{"data":[{"id":"1","type":"a"},{"id":"2","type":"a"}]}`, oneErr: ErrNoData, manyLen: 2},
		{name: "null data", body: `{"data":null}`, oneErr: ErrNoData},
		{name: "no data member", body: `{"meta":{"total":0}}`, oneErr: ErrNoData},
		{name: "empty array", body: `{"data":[]}`, oneErr: ErrNoData},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := decodeDoc(t, tt.body)
			r, err := doc.One()
			if !errors.Is(err, tt.oneErr) || r.ID != tt.oneID {
				t.Errorf("One() = %+v, %v; want id %q, err %v", r, err, tt.oneID, tt.oneErr)
			}
			rs, err := doc.Many()
			if !errors.Is(err, tt.manyErr) || len(rs) != tt.manyLen {
				t.Errorf("Many() = %d items, %v; want %d, %v", len(rs), err, tt.manyLen, tt.manyErr)
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	bad := &Document{Data: json.RawMessage(`{"id": 5}`)}
	if _, err := bad.One(); err == nil {
		t.Error("One() with a numeric id succeeded")
	}
	badMany := &Document{Data: json.RawMessage(`[{"id": 5}]`)}
	if _, err := badMany.Many(); err == nil {
		t.Error("Many() with a numeric id succeeded")
	}
	for _, linkage := range []string{`[{"id": 5}]`, `{"id": 5}`} {
		rel := Relationship{Data: json.RawMessage(linkage)}
		if _, err := rel.Identifiers(); err == nil {
			t.Errorf("Identifiers(%s) succeeded", linkage)
		}
		if _, _, err := (&Document{}).Resolve(rel); err == nil {
			t.Errorf("Resolve(%s) succeeded", linkage)
		}
	}
	type attrs struct {
		Duration time.Duration `json:"duration"`
	}
	if _, err := DecodeAttributes[attrs](Resource{ID: "1", Type: "tracks", Attributes: json.RawMessage(`{"duration":"PT4M"}`)}); err == nil ||
		!strings.Contains(err.Error(), "attributes of tracks 1") {
		t.Errorf("DecodeAttributes error = %v, want it to name the resource", err)
	}
	if v, err := DecodeMeta[map[string]int](nil); err != nil || v != nil {
		t.Errorf("DecodeMeta(nil) = %v, %v; want zero value", v, err)
	}
}

func TestAPIErrorFormatting(t *testing.T) {
	e := &APIError{Method: "GET", Path: "/v2/x", Status: 403, Errors: []ErrorObject{{Code: "GEO_RESTRICTED", Detail: "line1\nline2\x00"}}}
	if got, want := e.Error(), "tidal: GET /v2/x: 403 Forbidden: GEO_RESTRICTED: line1 line2 "; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	bare := &APIError{Method: "GET", Path: "/v2/x", Status: 502}
	if got, want := bare.Error(), "tidal: GET /v2/x: 502 Bad Gateway"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if bare.Code() != "" {
		t.Errorf("Code() = %q, want empty", bare.Code())
	}
	long := &APIError{Method: "GET", Path: "/p", Status: 400, Errors: []ErrorObject{{Detail: strings.Repeat("é", 300)}}}
	if n := len([]rune(long.Error())); n > 260 {
		t.Errorf("long detail produced %d runes, want truncation", n)
	}
}

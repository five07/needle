package tidal

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// Page bodies mirror the live API: links.next is relative to /v2 and carries
// the full query with page%5Bcursor%5D; links.meta holds nextCursor.
const (
	page1 = `{"data":[{"id":"1","type":"albums","meta":{"addedAt":"2026-11-20T00:49:23.056Z"}}],
		"links":{"self":"/userCollectionAlbums/me/relationships/items?locale=en-US",
		"next":"/userCollectionAlbums/me/relationships/items?locale=en-US&page%5Bcursor%5D=c2",
		"meta":{"nextCursor":"c2"}}}`
	page2 = `{"data":[{"id":"2","type":"albums"}],
		"links":{"next":"/userCollectionAlbums/me/relationships/items?locale=en-US&page%5Bcursor%5D=c3"}}`
	page3 = `{"data":[{"id":"3","type":"albums"}],"links":{"self":"/x"}}`
)

func collectIDs(t *testing.T, docs []*Document) []string {
	t.Helper()
	var ids []string
	for _, d := range docs {
		rs, err := d.Many()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func TestPagesFollowsNextLinks(t *testing.T) {
	f := newFakeAPI(t, reply{status: 200, body: page1}, reply{status: 200, body: page2}, reply{status: 200, body: page3})
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})

	var docs []*Document
	for doc, err := range c.Pages(context.Background(), "/userCollectionAlbums/me/relationships/items", url.Values{"locale": {"en-US"}}) {
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}

	if got := strings.Join(collectIDs(t, docs), ","); got != "1,2,3" {
		t.Errorf("ids = %s, want 1,2,3", got)
	}
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	wantCursors := []string{"", "c2", "c3"}
	for i, r := range reqs {
		if r.path != "/v2/userCollectionAlbums/me/relationships/items" {
			t.Errorf("request %d path = %s", i, r.path)
		}
		if got := r.query.Get("page[cursor]"); got != wantCursors[i] {
			t.Errorf("request %d cursor = %q, want %q", i, got, wantCursors[i])
		}
		if r.query.Get("locale") != "en-US" || len(r.query["locale"]) != 1 {
			t.Errorf("request %d locale = %v, want exactly one en-US", i, r.query["locale"])
		}
	}
}

func TestPagesStopsWhenCallerBreaks(t *testing.T) {
	f := newFakeAPI(t, reply{status: 200, body: page1})
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})

	for _, err := range c.Pages(context.Background(), "/userCollectionAlbums/me/relationships/items", nil) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

func TestPagesStopsOnContextCancel(t *testing.T) {
	f := newFakeAPI(t, reply{status: 200, body: page1})
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pages int
	var lastErr error
	for doc, err := range c.Pages(ctx, "/userCollectionAlbums/me/relationships/items", nil) {
		if err != nil {
			lastErr = err
			continue
		}
		pages++
		_ = doc
		cancel() // cancel after the first page
	}
	if pages != 1 || !errors.Is(lastErr, context.Canceled) {
		t.Errorf("pages = %d, err = %v; want 1 page then context.Canceled", pages, lastErr)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("requests = %d, want 1 (no fetch after cancel)", n)
	}
}

func TestPagesYieldsErrorAndStops(t *testing.T) {
	tests := []struct {
		name    string
		replies []reply
		wantErr string
		pages   int
	}{
		{
			name:    "API error on second page",
			replies: []reply{{status: 200, body: page1}, {status: 404}},
			wantErr: "404",
			pages:   1,
		},
		{
			name:    "next link to a foreign host",
			replies: []reply{{status: 200, body: `{"data":[],"links":{"next":"https://evil.example/v2/steal"}}`}},
			wantErr: "foreign host",
			pages:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeAPI(t, tt.replies...)
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
			pages, errs := 0, 0
			var lastErr error
			for _, err := range c.Pages(context.Background(), "/items", nil) {
				if err != nil {
					errs++
					lastErr = err
					continue
				}
				pages++
			}
			if pages != tt.pages || errs != 1 || lastErr == nil || !strings.Contains(lastErr.Error(), tt.wantErr) {
				t.Errorf("pages = %d, errs = %d, err = %v; want %d pages and one error containing %q", pages, errs, lastErr, tt.pages, tt.wantErr)
			}
		})
	}
}

func TestPagesCanceledBeforeStart(t *testing.T) {
	f := newFakeAPI(t)
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range c.Pages(ctx, "/items", nil) {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}

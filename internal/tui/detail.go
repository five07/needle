package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/five07/tidal-tui/internal/tidal"
)

type pageFetch[T any] func(ctx context.Context, next string) (tidal.Page[T], error)

// pageMsg carries one loaded page to the view that requested it. part
// tells the artist view which list the page belongs to.
type pageMsg[T any] struct {
	part int
	page tidal.Page[T]
	err  error
}

// pager tracks lazy loading of one paged list.
type pager struct {
	next    string
	loading bool
	done    bool // the first page arrived
	errText string
}

// want reports whether the next page should be fetched: there is one, no
// earlier page failed (no retry on every key), and the cursor is within
// loadAhead rows of the list end. fetchPage skips it while one is loading.
func (p *pager) want(cursor, n int) bool {
	return p.errText == "" && p.next != "" && cursor >= n-loadAhead
}

func fetchPage[T any](v View, svc *Services, p *pager, part int, fetch pageFetch[T]) tea.Cmd {
	if p.loading {
		return nil
	}
	p.loading = true
	next := p.next
	ctx := svc.ctx()
	return To(v, func() tea.Msg {
		page, err := fetch(ctx, next)
		return pageMsg[T]{part: part, page: page, err: err}
	})
}

// received records a page result and returns whether it succeeded.
func (p *pager) received(svc *Services, what string, err error, next string) bool {
	p.loading, p.done = false, true
	if err != nil {
		svc.logger().Warn("load "+what+" failed", "err", err)
		p.errText = userMessage(err)
		return false
	}
	p.next = next
	return true
}

// TrackListView shows an album's or a playlist's tracks under a header,
// loading more pages as the cursor nears the end.
type TrackListView struct {
	svc    *Services
	title  string
	meta   string
	empty  string
	fetch  pageFetch[tidal.Track]
	list   entryList
	tracks pager
}

// NewAlbumView returns the detail view for album a.
func NewAlbumView(svc *Services, a tidal.Album) *TrackListView {
	cat := svc.Catalog
	return &TrackListView{
		svc:   svc,
		title: a.Title,
		meta:  joinMeta(strings.Join(a.Artists, ", "), a.Year, countTracks(a.Tracks), clock(a.Duration)),
		empty: "This album has no playable tracks.",
		fetch: func(ctx context.Context, next string) (tidal.Page[tidal.Track], error) {
			return cat.AlbumTracks(ctx, a.ID, next)
		},
		list: newEntryList(svc),
	}
}

// NewPlaylistView returns the detail view for playlist p.
func NewPlaylistView(svc *Services, p tidal.Playlist) *TrackListView {
	cat := svc.Catalog
	owner := ""
	if p.Editorial {
		owner = "TIDAL"
	}
	return &TrackListView{
		svc:   svc,
		title: p.Name,
		meta:  joinMeta(owner, countTracks(p.Tracks), clock(p.Duration)),
		empty: "This playlist has no playable tracks.",
		fetch: func(ctx context.Context, next string) (tidal.Page[tidal.Track], error) {
			return cat.PlaylistTracks(ctx, p.ID, next)
		},
		list: newEntryList(svc),
	}
}

func joinMeta(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, " · ")
}

// Open implements Opener: it loads the first page.
func (v *TrackListView) Open() tea.Cmd { return fetchPage(v, v.svc, &v.tracks, 0, v.fetch) }

// Title implements View.
func (v *TrackListView) Title() string { return Sanitize(v.title) }

// Render implements View.
func (v *TrackListView) Render(width, height int, focused bool) string {
	head := []string{styleTitle.Render(fit(Sanitize(v.title), width)), styleDim.Render(fit(Sanitize(v.meta), width)),
		styleBorder.Render(strings.Repeat("─", width))}
	bodyH := max(height-len(head), 0)
	var body string
	switch {
	case len(v.list.entries) > 0 && v.tracks.errText != "":
		// Keep the loaded tracks; say that the rest failed.
		body = v.list.render(width, max(bodyH-1, 0), focused) + "\n" + styleDim.Render(fit("Could not load more: "+v.tracks.errText, width))
	case len(v.list.entries) > 0:
		body = v.list.render(width, bodyH, focused)
	case v.tracks.errText != "":
		body = styleDim.Render(fit("Could not load tracks: "+v.tracks.errText, width))
	case !v.tracks.done:
		body = styleDim.Render(fit("Loading…", width))
	default:
		body = styleDim.Render(fit(v.empty, width))
	}
	return strings.Join(append(head[:min(len(head), height)], body), "\n")
}

// Update implements View.
func (v *TrackListView) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(pageMsg[tidal.Track]); ok {
		if v.tracks.received(v.svc, "tracks", m.err, m.page.Next) {
			v.list.add(trackEntries(m.page.Items, 0)...)
		}
		return v.more()
	}
	if c, ok := msg.(ClickMsg); ok {
		c.Y -= 3
		msg = c
	}
	cmd, _ := v.list.update(msg)
	return tea.Batch(cmd, v.more())
}

func (v *TrackListView) more() tea.Cmd {
	if v.tracks.want(v.list.Cursor(), len(v.list.entries)) {
		return fetchPage(v, v.svc, &v.tracks, 0, v.fetch)
	}
	return nil
}

// Bindings implements View.
func (v *TrackListView) Bindings() []key.Binding { return v.list.bindings(false) }

// FavoriteChanged implements favoriteWatcher.
func (v *TrackListView) FavoriteChanged(FavoriteMsg) tea.Cmd {
	v.list.favoritesChanged()
	return nil
}

// Capturing implements View.
func (v *TrackListView) Capturing() bool { return false }

// Artist view parts.
const (
	partTracks = iota
	partAlbums
)

// ArtistView shows an artist's top tracks, then their albums. Albums load
// more pages as the cursor nears the end.
type ArtistView struct {
	svc    *Services
	artist tidal.Artist
	list   entryList
	top    []tidal.Track
	albums []tidal.Album
	tracks pager
	albPgr pager
}

// NewArtistView returns the detail view for artist a.
func NewArtistView(svc *Services, a tidal.Artist) *ArtistView {
	return &ArtistView{svc: svc, artist: a, list: newEntryList(svc)}
}

func (v *ArtistView) fetchTracks(ctx context.Context, next string) (tidal.Page[tidal.Track], error) {
	return v.svc.Catalog.ArtistTracks(ctx, v.artist.ID, next)
}

func (v *ArtistView) fetchAlbums(ctx context.Context, next string) (tidal.Page[tidal.Album], error) {
	return v.svc.Catalog.ArtistAlbums(ctx, v.artist.ID, next)
}

// Open implements Opener: it loads the top tracks and the first albums.
func (v *ArtistView) Open() tea.Cmd {
	return tea.Batch(
		fetchPage(v, v.svc, &v.tracks, partTracks, v.fetchTracks),
		fetchPage(v, v.svc, &v.albPgr, partAlbums, v.fetchAlbums))
}

// Title implements View.
func (v *ArtistView) Title() string { return Sanitize(v.artist.Name) }

// rebuild lays out both sections, keeping the selected row.
func (v *ArtistView) rebuild() {
	var es []entry
	if len(v.top) > 0 {
		es = append(es, headingEntry("Top tracks", partTracks))
		es = append(es, trackEntries(v.top, partTracks)...)
	}
	if len(v.albums) > 0 {
		es = append(es, headingEntry("Albums", partAlbums))
		es = append(es, albumEntries(v.albums, partAlbums)...)
	}
	cur := v.list.Cursor()
	sel, had := v.list.selected()
	v.list.set(es)
	if had && sel.kind == entryAlbum {
		// Tracks arriving after albums shift the rows; follow the album.
		for i, en := range es {
			if en.kind == entryAlbum && en.album.ID == sel.album.ID {
				cur = i
			}
		}
	}
	if had {
		v.list.Select(cur)
		v.list.skip(1)
	}
}

// Render implements View.
func (v *ArtistView) Render(width, height int, focused bool) string {
	head := []string{styleTitle.Render(fit(Sanitize(v.artist.Name), width)), styleBorder.Render(strings.Repeat("─", width))}
	bodyH := max(height-len(head), 0)
	var body string
	var notes []string
	if v.tracks.errText != "" {
		notes = append(notes, "Could not load top tracks: "+v.tracks.errText)
	}
	if v.albPgr.errText != "" {
		notes = append(notes, "Could not load albums: "+v.albPgr.errText)
	}
	switch {
	case len(v.list.entries) > 0:
		body = v.list.render(width, max(bodyH-len(notes), 0), focused)
	case !v.tracks.done || !v.albPgr.done:
		body = styleDim.Render(fit("Loading…", width))
	case len(notes) == 0:
		body = styleDim.Render(fit("No tracks or albums for this artist.", width))
	}
	for _, n := range notes {
		if body != "" {
			body += "\n"
		}
		body += styleDim.Render(fit(n, width))
	}
	return strings.Join(append(head[:min(len(head), height)], body), "\n")
}

// Update implements View.
func (v *ArtistView) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case pageMsg[tidal.Track]:
		if v.tracks.received(v.svc, "artist tracks", m.err, "") {
			v.top = m.page.Items
			v.rebuild()
		}
		return nil
	case pageMsg[tidal.Album]:
		if v.albPgr.received(v.svc, "artist albums", m.err, m.page.Next) {
			v.albums = append(v.albums, m.page.Items...)
			v.rebuild()
		}
		return v.more()
	case ClickMsg:
		m.Y -= 2
		msg = m
	}
	cmd, _ := v.list.update(msg)
	return tea.Batch(cmd, v.more())
}

func (v *ArtistView) more() tea.Cmd {
	if v.albPgr.want(v.list.Cursor(), len(v.list.entries)) {
		return fetchPage(v, v.svc, &v.albPgr, partAlbums, v.fetchAlbums)
	}
	return nil
}

// Bindings implements View.
func (v *ArtistView) Bindings() []key.Binding { return v.list.bindings(true) }

// FavoriteChanged implements favoriteWatcher.
func (v *ArtistView) FavoriteChanged(FavoriteMsg) tea.Cmd {
	v.list.favoritesChanged()
	return nil
}

// Capturing implements View.
func (v *ArtistView) Capturing() bool { return false }

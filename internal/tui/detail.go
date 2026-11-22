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

// TrackListView shows a paged list under a header: an album's or a
// playlist's tracks (mixes and radios included), or similar items. It
// loads more pages as the cursor nears the end.
type TrackListView struct {
	svc    *Services
	title  string
	meta   string
	empty  string
	what   string // what the list holds, for errors ("tracks")
	fetch  pageFetch[entry]
	list   entryList
	tracks pager
}

// entriesOf adapts a page source of T to one of entries.
func entriesOf[T any](f pageFetch[T], conv func([]T) []entry) pageFetch[entry] {
	return func(ctx context.Context, next string) (tidal.Page[entry], error) {
		p, err := f(ctx, next)
		return tidal.Page[entry]{Items: conv(p.Items), Next: p.Next}, err
	}
}

// NewAlbumView returns the detail view for album a.
func NewAlbumView(svc *Services, a tidal.Album) *TrackListView {
	cat := svc.Catalog
	return &TrackListView{
		svc:   svc,
		title: a.Title,
		meta:  joinMeta(strings.Join(a.Artists, ", "), a.Year, countTracks(a.Tracks), clock(a.Duration)),
		empty: "This album has no playable tracks.",
		what:  "tracks",
		fetch: entriesOf(func(ctx context.Context, next string) (tidal.Page[tidal.Track], error) {
			return cat.AlbumTracks(ctx, a.ID, next)
		}, trackEntries),
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
	meta, empty := joinMeta(owner, countTracks(p.Tracks), clock(p.Duration)), "This playlist has no playable tracks."
	if p.Mix {
		meta, empty = joinMeta("TIDAL mix", p.Description), "This mix has no playable tracks."
	}
	return &TrackListView{
		svc:   svc,
		title: p.Name,
		meta:  meta,
		empty: empty,
		what:  "tracks",
		fetch: entriesOf(func(ctx context.Context, next string) (tidal.Page[tidal.Track], error) {
			return cat.PlaylistTracks(ctx, p.ID, next)
		}, trackEntries),
		list: newEntryList(svc),
	}
}

// NewRadioView returns the track list of a radio mix (decision 43).
func NewRadioView(svc *Services, radio tidal.Playlist) *TrackListView {
	v := NewPlaylistView(svc, radio)
	v.title = "Radio: " + radio.Name
	v.empty = "This radio has no playable tracks."
	return v
}

// NewSimilarView returns the items TIDAL lists as similar to en: tracks for
// a track, artists for an artist, albums for an album. ok is false for
// other entries.
func NewSimilarView(svc *Services, en entry) (v *TrackListView, ok bool) {
	d := svc.Discovery
	v = &TrackListView{svc: svc, list: newEntryList(svc)}
	switch en.kind {
	case entryTrack:
		id := en.track.ID
		v.title, v.meta, v.what = "Similar to "+en.track.FullTitle(), "Tracks", "similar tracks"
		v.fetch = entriesOf(func(ctx context.Context, next string) (tidal.Page[tidal.Track], error) {
			return d.SimilarTracks(ctx, id, next)
		}, trackEntries)
	case entryArtist:
		id := en.artist.ID
		v.title, v.meta, v.what = "Similar to "+en.artist.Name, "Artists", "similar artists"
		v.fetch = entriesOf(func(ctx context.Context, next string) (tidal.Page[tidal.Artist], error) {
			return d.SimilarArtists(ctx, id, next)
		}, artistEntries)
	case entryAlbum:
		id := en.album.ID
		v.title, v.meta, v.what = "Similar to "+en.album.Title, "Albums", "similar albums"
		v.fetch = entriesOf(func(ctx context.Context, next string) (tidal.Page[tidal.Album], error) {
			return d.SimilarAlbums(ctx, id, next)
		}, albumEntries)
	default:
		return nil, false
	}
	v.empty = "TIDAL lists no " + v.what + "."
	return v, true
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
		body = styleDim.Render(fit("Could not load "+v.what+": "+v.tracks.errText, width))
	case !v.tracks.done:
		body = styleDim.Render(fit("Loading…", width))
	default:
		body = styleDim.Render(fit(v.empty, width))
	}
	return strings.Join(append(head[:min(len(head), height)], body), "\n")
}

// Update implements View.
func (v *TrackListView) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(pageMsg[entry]); ok {
		if v.tracks.received(v.svc, v.what, m.err, m.page.Next) {
			v.list.add(m.page.Items...)
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
func (v *TrackListView) Bindings() []key.Binding { return v.list.bindings() }

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

var artistTabTitles = [...]string{partTracks: "Top tracks", partAlbums: "Albums"}

// ArtistView shows an artist's top tracks and albums in sub-tabs
// (decision 46). Top tracks are one page; albums load more pages as the
// cursor nears the end.
type ArtistView struct {
	svc    *Services
	artist tidal.Artist
	lists  [2]entryList // by part
	active int          // part shown; -1 until a list has items
	tracks pager
	albPgr pager
}

// NewArtistView returns the detail view for artist a.
func NewArtistView(svc *Services, a tidal.Artist) *ArtistView {
	return &ArtistView{svc: svc, artist: a, lists: [2]entryList{newEntryList(svc), newEntryList(svc)}, active: -1}
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

// parts returns the parts that have items, in tab order (AC #2).
func (v *ArtistView) parts() []int {
	var out []int
	for p := range v.lists {
		if len(v.lists[p].entries) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// arrived selects the first part to have items; a part arriving later
// leaves the shown tab alone.
func (v *ArtistView) arrived(part int) {
	if v.active < 0 {
		v.active = part
	}
}

// tabTitles returns the titles of the shown tabs and the index of the
// active one.
func (v *ArtistView) tabTitles() (titles []string, cur int) {
	for i, p := range v.parts() {
		titles = append(titles, artistTabTitles[p])
		if p == v.active {
			cur = i
		}
	}
	return titles, cur
}

func (v *ArtistView) switchTab(delta int) {
	ps := v.parts()
	for i, p := range ps {
		if p == v.active {
			v.active = ps[(i+delta+len(ps))%len(ps)]
			return
		}
	}
}

// Render implements View.
func (v *ArtistView) Render(width, height int, focused bool) string {
	head := []string{styleTitle.Render(fit(Sanitize(v.artist.Name), width))}
	titles, cur := v.tabTitles()
	if len(titles) > 0 {
		head = append(head, tabBar(titles, cur, width, ""))
	}
	head = append(head, styleBorder.Render(strings.Repeat("─", width)))
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
	case len(titles) > 0:
		body = v.lists[v.active].render(width, max(bodyH-len(notes), 0), focused)
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
		if v.tracks.received(v.svc, "artist tracks", m.err, "") && len(m.page.Items) > 0 {
			v.lists[partTracks].set(trackEntries(m.page.Items))
			v.arrived(partTracks)
		}
		return nil
	case pageMsg[tidal.Album]:
		if v.albPgr.received(v.svc, "artist albums", m.err, m.page.Next) && len(m.page.Items) > 0 {
			v.lists[partAlbums].add(albumEntries(m.page.Items)...)
			v.arrived(partAlbums)
		}
		return v.more()
	}
	if v.active < 0 {
		return nil
	}
	l := &v.lists[v.active]
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(m, Keys.NextTab):
			v.switchTab(1)
			return v.more()
		case key.Matches(m, Keys.PrevTab):
			v.switchTab(-1)
			return v.more()
		}
	case ClickMsg:
		if m.Y == 1 {
			titles, _ := v.tabTitles()
			if i := tabAt(titles, m.X); i >= 0 {
				v.active = v.parts()[i]
			}
			return v.more()
		}
		m.Y -= 3 // the title and separator rows land on no list row
		msg = m
	}
	cmd, _ := l.update(msg)
	return tea.Batch(cmd, v.more())
}

// more loads the next albums page when the Albums cursor nears the end.
func (v *ArtistView) more() tea.Cmd {
	l := &v.lists[partAlbums]
	if v.albPgr.want(l.Cursor(), len(l.entries)) {
		return fetchPage(v, v.svc, &v.albPgr, partAlbums, v.fetchAlbums)
	}
	return nil
}

// Bindings implements View.
func (v *ArtistView) Bindings() []key.Binding {
	if v.active < 0 {
		return nil
	}
	bs := v.lists[v.active].bindings()
	if len(v.parts()) < 2 {
		return bs
	}
	bs = append([]key.Binding{key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab"))}, bs...)
	return append(bs, key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous tab")))
}

// LeadHints implements HintLeader: with two tabs, tab comes first (AC #4).
func (v *ArtistView) LeadHints() int {
	if len(v.parts()) < 2 {
		return 0
	}
	return 1
}

// FavoriteChanged implements favoriteWatcher.
func (v *ArtistView) FavoriteChanged(FavoriteMsg) tea.Cmd {
	for i := range v.lists {
		v.lists[i].favoritesChanged()
	}
	return nil
}

// Capturing implements View.
func (v *ArtistView) Capturing() bool { return false }

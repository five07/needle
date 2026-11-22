package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/five07/needle/internal/tidal"
)

type entryKind int

const (
	entryNone entryKind = iota // the zero entry: no item
	entryTrack
	entryAlbum
	entryArtist
	entryPlaylist
)

// entry is one item of a catalog list. The tracks of a list form the list
// Enter plays from (decision 28).
type entry struct {
	kind     entryKind
	track    tidal.Track
	album    tidal.Album
	artist   tidal.Artist
	playlist tidal.Playlist
}

func trackEntries(ts []tidal.Track) []entry {
	out := make([]entry, len(ts))
	for i, t := range ts {
		out[i] = entry{kind: entryTrack, track: t}
	}
	return out
}

func albumEntries(as []tidal.Album) []entry {
	out := make([]entry, len(as))
	for i, a := range as {
		out[i] = entry{kind: entryAlbum, album: a}
	}
	return out
}

func artistEntries(as []tidal.Artist) []entry {
	out := make([]entry, len(as))
	for i, a := range as {
		out[i] = entry{kind: entryArtist, artist: a}
	}
	return out
}

// entryList is a list of catalog entries with the item actions: Enter
// plays a track or opens a collection, a/A queue, f/F favorite.
type entryList struct {
	List
	svc     *Services
	entries []entry
	width   int // width the rows were laid out for; 0 forces a rebuild
}

func newEntryList(svc *Services) entryList {
	return entryList{svc: svc}
}

// set replaces the entries and puts the cursor on the first item.
func (e *entryList) set(entries []entry) {
	e.entries = entries
	e.width = 0
	e.cursor, e.offset = 0, 0
	e.relayout(max(e.width, 1))
}

// add appends entries, keeping the cursor.
func (e *entryList) add(entries ...entry) {
	e.entries = append(e.entries, entries...)
	e.relayout(max(e.width, 1))
}

func (e *entryList) relayout(width int) {
	rows := make([]string, len(e.entries))
	for i, en := range e.entries {
		rows[i] = rowText(en, width, e.isFavorite(en))
	}
	e.SetRows(rows)
}

func (e *entryList) render(width, height int, focused bool) string {
	if width != e.width {
		e.width = width
		e.relayout(width)
	}
	return e.List.Render(width, height, focused)
}

// selected returns the entry under the cursor.
func (e *entryList) selected() (entry, bool) {
	if e.cursor < 0 || e.cursor >= len(e.entries) {
		return entry{}, false
	}
	return e.entries[e.cursor], true
}

// update handles keys, clicks, and the wheel. handled is false for keys
// the list does not use.
func (e *entryList) update(msg tea.Msg) (cmd tea.Cmd, handled bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case e.HandleKey(msg):
		case key.Matches(msg, Keys.Open):
			return e.activate(), true
		case key.Matches(msg, Keys.QueueAdd):
			return e.queue(false), true
		case key.Matches(msg, Keys.PlayNext):
			return e.queue(true), true
		case key.Matches(msg, Keys.Favorite), key.Matches(msg, Keys.Unfavorite):
			if en, ok := e.selected(); ok && e.svc != nil {
				return e.svc.favoriteCmd(en, key.Matches(msg, Keys.Favorite)), true
			}
		case key.Matches(msg, Keys.AddToPlaylist):
			return e.addToPlaylist(), true
		case key.Matches(msg, Keys.Radio):
			return e.radio(), true
		case key.Matches(msg, Keys.Similar):
			return e.similar(), true
		default:
			return nil, false
		}
		return nil, true
	case ClickMsg:
		if i := e.RowAt(msg.Y); i >= 0 {
			e.Select(i)
			if msg.Double {
				return e.activate(), true
			}
		}
		return nil, true
	case WheelMsg:
		e.Scroll(msg.Delta)
		return nil, true
	}
	return nil, false
}

// activate plays the list's tracks from the selected one, or opens the selected
// album, artist, or playlist.
func (e *entryList) activate() tea.Cmd {
	en, ok := e.selected()
	if !ok {
		return nil
	}
	switch en.kind {
	case entryTrack:
		if e.svc.pausable(en.track.ID) { // AC #8, decision 48
			return e.svc.transportCmd(e.svc.Transport.TogglePause)
		}
		var tracks []tidal.Track
		start := 0
		for i, other := range e.entries {
			if other.kind == entryTrack {
				if i == e.cursor {
					start = len(tracks)
				}
				tracks = append(tracks, other.track)
			}
		}
		return e.svc.playCmd(tracks, start)
	case entryAlbum:
		return Push(NewAlbumView(e.svc, en.album))
	case entryPlaylist:
		return Push(NewPlaylistView(e.svc, en.playlist))
	case entryArtist:
		return Push(NewArtistView(e.svc, en.artist))
	}
	return nil
}

// queue adds the selected item to the queue: a track, or every track of an
// album or playlist (decision 29). Artists are opened, not queued.
func (e *entryList) queue(next bool) tea.Cmd {
	en, ok := e.selected()
	if !ok {
		return nil
	}
	cat := e.svc.Catalog
	switch en.kind {
	case entryTrack:
		return e.svc.queueCmd([]tidal.Track{en.track}, next, en.track.FullTitle())
	case entryAlbum:
		id := en.album.ID
		return e.svc.queueAllCmd(en.album.Title, next, func(ctx context.Context, n string) (tidal.Page[tidal.Track], error) {
			return cat.AlbumTracks(ctx, id, n)
		})
	case entryPlaylist:
		id := en.playlist.ID
		return e.svc.queueAllCmd(en.playlist.Name, next, func(ctx context.Context, n string) (tidal.Page[tidal.Track], error) {
			return cat.PlaylistTracks(ctx, id, n)
		})
	case entryArtist:
		return Notify("Open the artist to queue its tracks.")
	}
	return nil
}

// bindings are the item keys, most used first, for the hint line and
// help.
func (e *entryList) bindings() []key.Binding {
	if len(e.entries) == 0 {
		return nil
	}
	bs := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play/open")),
		Keys.QueueAdd, Keys.PlayNext, Keys.Favorite, Keys.Unfavorite, Keys.AddToPlaylist,
	}
	if e.svc != nil && e.svc.Discovery != nil {
		bs = append(bs, Keys.Radio, Keys.Similar)
	}
	return bs
}

func (e *entryList) isFavorite(en entry) bool {
	c, id, _, ok := favRef(en)
	return ok && e.svc != nil && e.svc.favs().is(c, id)
}

// addToPlaylist opens the playlist picker for the selected track.
func (e *entryList) addToPlaylist() tea.Cmd {
	en, ok := e.selected()
	if !ok || e.svc == nil || e.svc.Library == nil {
		return nil
	}
	if en.kind != entryTrack {
		return Notify("Only tracks can be added to a playlist.")
	}
	return Push(NewPlaylistPicker(e.svc, en.track))
}

// radioTimeout bounds the request for a radio mix.
const radioTimeout = 15 * time.Second

// radio opens the radio of the selected track or artist (AC #3).
func (e *entryList) radio() tea.Cmd {
	en, ok := e.selected()
	if !ok || e.svc == nil || e.svc.Discovery == nil {
		return nil
	}
	var get func(context.Context) (tidal.Playlist, error)
	name := ""
	d := e.svc.Discovery
	switch en.kind {
	case entryTrack:
		id := en.track.ID
		name, get = en.track.FullTitle(), func(ctx context.Context) (tidal.Playlist, error) { return d.TrackRadio(ctx, id) }
	case entryArtist:
		id := en.artist.ID
		name, get = en.artist.Name, func(ctx context.Context) (tidal.Playlist, error) { return d.ArtistRadio(ctx, id) }
	default:
		return Notify("Radio is available for tracks and artists.")
	}
	svc := e.svc
	ctx := svc.ctx()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, radioTimeout)
		defer cancel()
		radio, err := get(ctx)
		switch {
		case errors.Is(err, tidal.ErrNoRadio):
			return NoticeMsg{Text: "TIDAL has no radio for " + name + "."}
		case err != nil:
			svc.logger().Warn("load radio failed", "item", name, "err", err)
			if svc.ctx().Err() != nil {
				return nil // shutting down
			}
			return NoticeMsg{Text: "Could not start radio: " + userMessage(err)}
		}
		return PushMsg{View: NewRadioView(svc, radio)}
	}
}

// similar lists items similar to the selected track, artist, or album
// (AC #4).
func (e *entryList) similar() tea.Cmd {
	en, ok := e.selected()
	if !ok || e.svc == nil || e.svc.Discovery == nil {
		return nil
	}
	v, ok := NewSimilarView(e.svc, en)
	if !ok {
		return Notify("Similar is available for tracks, artists, and albums.")
	}
	return Push(v)
}

// favoritesChanged redraws the rows on the next render, so favorite
// markers follow the change.
func (e *entryList) favoritesChanged() { e.width = 0 }

// Row layout.

// cell is one column of a row: a share of the flexible width, or a fixed
// width, optionally right-aligned.
type cell struct {
	text  string
	share int
	fixed int
	right bool
}

const colGap = 2

// columns lays cells out across width cells.
func columns(width int, cells ...cell) string {
	flex, shares := width, 0
	for i, c := range cells {
		flex -= c.fixed
		if i > 0 {
			flex -= colGap
		}
		shares += c.share
	}
	flex = max(flex, 0)
	var b strings.Builder
	used := 0
	for i, c := range cells {
		w := c.fixed
		if c.share > 0 {
			w = flex * c.share / shares
		}
		if i == len(cells)-1 {
			w = max(width-used, 0)
		}
		text := Sanitize(c.text)
		cellText := fit(text, w)
		if c.right {
			cellText = fit(strings.Repeat(" ", max(w-ansi.StringWidth(text), 0))+text, w)
		}
		b.WriteString(cellText)
		used += w
		if i < len(cells)-1 {
			gap := min(colGap, max(width-used, 0))
			b.WriteString(strings.Repeat(" ", gap))
			used += gap
		}
	}
	return b.String()
}

const indent = "  "

// favMark replaces the indent on rows known to be favorites.
const favMark = "♥ "

func rowText(en entry, width int, fav bool) string {
	w := max(width-len(indent), 1)
	indent := indent
	if fav {
		indent = favMark
	}
	switch en.kind {
	case entryTrack:
		t := en.track
		if width < 50 {
			return indent + columns(w, cell{text: t.FullTitle(), share: 3}, cell{text: strings.Join(t.Artists, ", "), share: 2})
		}
		return indent + columns(w,
			cell{text: t.FullTitle(), share: 9},
			cell{text: strings.Join(t.Artists, ", "), share: 6},
			cell{text: t.Album, share: 5},
			cell{text: clock(t.Duration), fixed: 7, right: true})
	case entryAlbum:
		a := en.album
		return indent + columns(w,
			cell{text: a.Title, share: 9},
			cell{text: strings.Join(a.Artists, ", "), share: 6},
			cell{text: a.Year, share: 5})
	case entryArtist:
		return indent + en.artist.Name
	case entryPlaylist:
		p := en.playlist
		if p.Mix { // no track count; the description names the artists
			return indent + columns(w, cell{text: p.Name, share: 7}, cell{text: p.Description, share: 13})
		}
		owner := ""
		if p.Editorial {
			owner = "TIDAL"
		}
		return indent + columns(w,
			cell{text: p.Name, share: 9},
			cell{text: owner, share: 6},
			cell{text: countTracks(p.Tracks), share: 5})
	}
	return ""
}

// clock formats d as m:ss or h:mm:ss, or "" when unknown.
func clock(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func countTracks(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 track"
	}
	return fmt.Sprintf("%d tracks", n)
}

package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/five07/tidal-tui/internal/tidal"
)

// libFake implements Library on top of fakeCatalog's bookkeeping.
type libFake struct {
	*fakeCatalog
	favTracks    []tidal.Page[tidal.Track]
	favAlbums    []tidal.Page[tidal.Album]
	favArtists   []tidal.Page[tidal.Artist]
	favPlaylists []tidal.Page[tidal.Playlist]
	owned        []tidal.Page[tidal.Playlist]
	sorts        []tidal.Sort
	writes       []string
	writeErr     error
}

func servePage[T any](f *fakeCatalog, key string, pages []tidal.Page[T], next string) (tidal.Page[T], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[key]++
	n := 0
	fmt.Sscanf(next, "p%d", &n)
	if err := f.errs[key]; err != nil && n >= f.errAfter[key] {
		return tidal.Page[T]{}, err
	}
	if n >= len(pages) {
		return tidal.Page[T]{}, nil
	}
	return pages[n], nil
}

func (l *libFake) sorted(s tidal.Sort) {
	l.mu.Lock()
	l.sorts = append(l.sorts, s)
	l.mu.Unlock()
}

func (l *libFake) FavoriteTracks(_ context.Context, s tidal.Sort, next string) (tidal.Page[tidal.Track], error) {
	l.sorted(s)
	return servePage(l.fakeCatalog, "fav:tracks", l.favTracks, next)
}
func (l *libFake) FavoriteAlbums(_ context.Context, s tidal.Sort, next string) (tidal.Page[tidal.Album], error) {
	l.sorted(s)
	return servePage(l.fakeCatalog, "fav:albums", l.favAlbums, next)
}
func (l *libFake) FavoriteArtists(_ context.Context, s tidal.Sort, next string) (tidal.Page[tidal.Artist], error) {
	l.sorted(s)
	return servePage(l.fakeCatalog, "fav:artists", l.favArtists, next)
}
func (l *libFake) FavoritePlaylists(_ context.Context, s tidal.Sort, next string) (tidal.Page[tidal.Playlist], error) {
	l.sorted(s)
	return servePage(l.fakeCatalog, "fav:playlists", l.favPlaylists, next)
}
func (l *libFake) OwnedPlaylists(_ context.Context, next string) (tidal.Page[tidal.Playlist], error) {
	return servePage(l.fakeCatalog, "owned", l.owned, next)
}
func (l *libFake) write(op string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writes = append(l.writes, op)
	return l.writeErr
}
func (l *libFake) AddFavorite(_ context.Context, c tidal.Collection, id string) error {
	return l.write("add " + string(c) + " " + id)
}
func (l *libFake) RemoveFavorite(_ context.Context, c tidal.Collection, id string) error {
	return l.write("remove " + string(c) + " " + id)
}
func (l *libFake) AddToPlaylist(_ context.Context, pl, track string) error {
	return l.write("playlist " + pl + " " + track)
}

func (l *libFake) gotWrites() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.writes...)
}

// newLibEnv is newEnv plus a Library fake with one page per tab.
func newLibEnv(t *testing.T) (*env, *libFake) {
	e := newEnv(t)
	l := &libFake{fakeCatalog: e.cat}
	l.favTracks = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tHarder, tLucky}}}
	l.favAlbums = []tidal.Page[tidal.Album]{{Items: []tidal.Album{aDisc, aRAM}}}
	l.favArtists = []tidal.Page[tidal.Artist]{{Items: []tidal.Artist{arDaft}}}
	l.favPlaylists = []tidal.Page[tidal.Playlist]{{Items: []tidal.Playlist{pEss, {ID: "mine-1", Name: "Road trip", Tracks: 12}}}}
	l.owned = []tidal.Page[tidal.Playlist]{{Items: []tidal.Playlist{{ID: "mine-1", Name: "Road trip", Tracks: 12}}, Next: "p1"},
		{Items: []tidal.Playlist{{ID: "mine-2", Name: "Gym", Tracks: 3}}}}
	e.svc.Library = l
	return e, l
}

func libView(m *Model) *LibraryView { return m.stacks[SectionLibrary][0].(*LibraryView) }

var tabKey = tea.KeyPressMsg{Code: tea.KeyTab}

func TestLibraryLoadsTabsOnFirstVisit(t *testing.T) {
	e, l := newLibEnv(t)
	m := e.model(80, 24)
	if l.count("fav:playlists") != 0 {
		t.Fatal("Library loaded before it was shown")
	}
	drive(m, press("2"))
	if l.count("fav:playlists") != 1 || l.count("fav:albums") != 0 {
		t.Fatalf("calls = %v", l.calls)
	}
	golden(t, "library_playlists_80x24", m.render())
	drive(m, press("l"), tabKey, tabKey)
	if l.count("fav:albums") != 1 || l.count("fav:tracks") != 1 {
		t.Fatalf("calls = %v", l.calls)
	}
	golden(t, "library_tracks_80x24", m.render())
	if hints := ansi.Strip(m.hintLine(300)); !strings.HasPrefix(hints, " / filter · tab next tab · j/k move · h back · enter") ||
		strings.Contains(hints, "/ search") || strings.Count(hints, "/ filter") != 1 {
		t.Errorf("hints = %q", hints)
	}
	// On a small terminal the lead keys are the ones that stay.
	if hints := ansi.Strip(m.hintLine(30)); !strings.HasPrefix(hints, " / filter · tab next tab") {
		t.Errorf("narrow hints = %q", hints)
	}
	// Coming back does not load again; shift+tab wraps.
	drive(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, tabKey)
	drive(m, press("1"), press("2"))
	if l.count("fav:albums") != 1 || l.count("fav:tracks") != 1 || l.count("fav:playlists") != 1 {
		t.Errorf("reloaded: %v", l.calls)
	}
	// Loaded items are known favorites.
	if !e.svc.favs().is(tidal.FavTracks, "2") || !e.svc.favs().is(tidal.FavPlaylists, "pl-1") {
		t.Error("loaded items not marked as favorites")
	}
	// Clicking a tab name switches to it.
	drive(m, tea.MouseClickMsg{X: m.layout.Main.X + 1 + 30, Y: m.layout.Main.Y, Button: tea.MouseLeft})
	if v := libView(m); v.tabs[v.active].title != "Artists" {
		t.Errorf("click selected %s", v.tabs[v.active].title)
	}
	drive(m, tea.MouseClickMsg{X: m.layout.Main.X + 1 + 55, Y: m.layout.Main.Y, Button: tea.MouseLeft})
	if v := libView(m); v.tabs[v.active].title != "Artists" {
		t.Error("a click right of the tabs switched tab")
	}
}

func TestLibraryPagingAndSort(t *testing.T) {
	e, l := newLibEnv(t)
	var p1, p2 []tidal.Track
	for i := range 20 {
		p1 = append(p1, track(fmt.Sprint(i), fmt.Sprintf("Song %02d", i)))
		p2 = append(p2, track(fmt.Sprint(100+i), "Later"))
	}
	l.favTracks = []tidal.Page[tidal.Track]{{Items: p1, Next: "p1"}, {Items: p2}}
	m := e.model(80, 24)
	drive(m, press("2"), press("l"), tabKey, tabKey)
	v := libView(m)
	for range 15 {
		drive(m, press("j"))
	}
	if l.count("fav:tracks") != 2 || len(v.tab().list.entries) != 40 {
		t.Fatalf("paging: calls %d entries %d", l.count("fav:tracks"), len(v.tab().list.entries))
	}
	// Enter plays the loaded list from the selection.
	drive(m, press("enter"))
	if got := e.play.got(); len(got) != 1 || len(got[0].ids) != 40 || got[0].start != 15 {
		t.Errorf("play = %+v", got)
	}
	// o cycles the server-side sort and reloads.
	notices := drive(m, press("o"))
	if !reflect.DeepEqual(notices, []string{"Tracks sorted by title"}) || l.count("fav:tracks") != 3 {
		t.Fatalf("sort: notices %q calls %d", notices, l.count("fav:tracks"))
	}
	drive(m, press("o"), press("o"))
	want := []tidal.Sort{tidal.SortAdded, tidal.SortAdded, tidal.SortTitle, tidal.SortArtist, tidal.SortAdded}
	if got := l.sorts[len(l.sorts)-5:]; !reflect.DeepEqual(got, want) {
		t.Errorf("sorts = %v, want %v", got, want)
	}
	if !strings.Contains(ansi.Strip(m.render()), "sort: date added") {
		t.Error("sort label missing")
	}
	// A page from before a reload is dropped.
	stale := libPageMsg{tab: v.active, gen: v.tab().gen - 1, entries: trackEntries([]tidal.Track{tLucky})}
	n := len(v.tab().all)
	v.Update(stale)
	v.Update(libPageMsg{tab: 9})
	if len(v.tab().all) != n {
		t.Error("a stale page was added")
	}
}

func TestLibraryFilter(t *testing.T) {
	e, l := newLibEnv(t)
	l.favTracks = []tidal.Page[tidal.Track]{
		{Items: []tidal.Track{tOneMore, tHarder}, Next: "p1"},
		{Items: []tidal.Track{{ID: "9", Title: "Café del Mar", Artists: []string{"Energy 52"}}}},
	}
	m := e.model(80, 24)
	drive(m, press("2"), press("l"), tabKey, tabKey)
	// "/" filters inside Library instead of going to Search.
	drive(m, press("/"))
	v := libView(m)
	if m.section != SectionLibrary || !v.Capturing() {
		t.Fatalf("/ went to section %d, capturing %v", m.section, v.Capturing())
	}
	if hints := ansi.Strip(m.hintLine(78)); !strings.Contains(hints, "esc clear filter") {
		t.Errorf("hints = %q", hints)
	}
	for _, r := range "cafe" {
		drive(m, press(string(r)))
	}
	// The filter loaded the remaining page and matches without accents.
	if l.count("fav:tracks") != 2 {
		t.Errorf("filter did not load the rest: %d calls", l.count("fav:tracks"))
	}
	if got := len(v.tab().list.entries); got != 1 || v.tab().list.entries[0].track.ID != "9" {
		t.Errorf("filtered = %d entries", got)
	}
	// Typing letters that are global keys does not act.
	drive(m, press("q"))
	if !strings.Contains(ansi.Strip(m.render()), `No matches for "cafeq".`) {
		t.Errorf("no-match state:\n%s", ansi.Strip(m.render()))
	}
	drive(m, press("backspace"), press("enter"))
	if v.Capturing() || len(v.tab().list.entries) != 1 {
		t.Error("enter did not keep the filter")
	}
	// Artist and album text match too, and the filter follows tab switches.
	drive(m, press("/"), press("backspace"), press("backspace"), press("backspace"), press("backspace"))
	for _, r := range "energy" {
		drive(m, press(string(r)))
	}
	if len(v.tab().list.entries) != 1 {
		t.Errorf("artist match = %d", len(v.tab().list.entries))
	}
	drive(m, press("enter"), tabKey)
	if v.tab().title != "Artists" || len(v.tab().list.entries) != 0 {
		t.Errorf("filter on the next tab: %s %d", v.tab().title, len(v.tab().list.entries))
	}
	drive(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	// esc clears the filter (both in and out of the input).
	drive(m, press("esc"))
	if v.filter.Value() != "" || len(v.tab().list.entries) != 3 {
		t.Errorf("esc: filter %q entries %d", v.filter.Value(), len(v.tab().list.entries))
	}
	drive(m, press("/"), press("x"), press("esc"))
	if v.filter.Value() != "" || v.Capturing() {
		t.Error("esc in the input did not clear it")
	}
	// Outside Library, "/" still goes to Search.
	drive(m, press("3"), press("/"))
	if m.section != SectionSearch {
		t.Error("/ outside Library did not go to Search")
	}
}

func TestLibraryFilterCap(t *testing.T) {
	e, l := newLibEnv(t)
	l.favArtists = nil
	for p := range 6 {
		var items []tidal.Artist
		for i := range 1000 {
			items = append(items, tidal.Artist{ID: fmt.Sprint(p*1000 + i), Name: "Band"})
		}
		l.favArtists = append(l.favArtists, tidal.Page[tidal.Artist]{Items: items, Next: fmt.Sprintf("p%d", p+1)})
	}
	m := e.model(80, 24)
	drive(m, press("2"), press("l"), tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, press("/"))
	notices := drive(m, press("b"))
	if l.count("fav:artists") != 5 || !reflect.DeepEqual(notices, []string{"Filtering the first 5000 artists only."}) {
		t.Errorf("calls %d notices %q", l.count("fav:artists"), notices)
	}
	if n := drive(m, press("a")); len(n) != 0 || l.count("fav:artists") != 5 {
		t.Errorf("kept loading past the cap: %q", n)
	}
}

func TestLibraryStates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(l *libFake)
		tabs  int
		want  string
	}{
		{"no playlists", func(l *libFake) { l.favPlaylists = nil }, 0, "No playlists yet."},
		{"no albums", func(l *libFake) { l.favAlbums = nil }, 1, "No favorite albums yet. Press f on an album"},
		{"no tracks", func(l *libFake) { l.favTracks = nil }, 2, "No favorite tracks yet."},
		{"no artists", func(l *libFake) { l.favArtists = nil }, 3, "No favorite artists yet."},
		{"load error", func(l *libFake) { l.errs["fav:albums"] = tidal.ErrUnauthorized }, 1, "Could not load albums: Your TIDAL session has ended."},
		{"later page error", func(l *libFake) {
			l.favTracks = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore}, Next: "p1"}}
			l.errs["fav:tracks"], l.errAfter["fav:tracks"] = tidal.ErrRateLimited, 1
		}, 2, "Could not load more: TIDAL is busy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, l := newLibEnv(t)
			tt.setup(l)
			m := e.model(80, 24)
			drive(m, press("2"), press("l"))
			for range tt.tabs {
				drive(m, tabKey)
			}
			if out := ansi.Strip(m.render()); !strings.Contains(out, tt.want) {
				t.Errorf("screen lacks %q:\n%s", tt.want, out)
			}
		})
	}
	// Before the first page arrives the tab says it is loading.
	e, _ := newLibEnv(t)
	v := NewLibraryView(e.svc)
	if !strings.Contains(ansi.Strip(v.Render(60, 5, true)), "Loading…") || v.Title() != "Library" {
		t.Error("loading state")
	}
	if NewLibraryView(e.svc).Render(60, 1, true) == "" {
		t.Error("tiny render")
	}
}

func TestFavoriteKeys(t *testing.T) {
	tests := []struct {
		name   string
		moves  []tea.Msg
		key    string
		setup  func(e *env, l *libFake)
		writes []string
		notice string
	}{
		{name: "f on a track", key: "f", writes: []string{"add tracks 1"}, notice: "Added to favorites: One More Time"},
		{name: "F on a track", key: "F", writes: []string{"remove tracks 1"}, notice: "Removed from favorites: One More Time"},
		{name: "f on an album", moves: []tea.Msg{tabKey}, key: "f", writes: []string{"add albums 1550545"}, notice: "Added to favorites: Discovery"},
		{name: "f on an artist", moves: []tea.Msg{tabKey, tabKey}, key: "f", writes: []string{"add artists 8847"}, notice: "Added to favorites: Daft Punk"},
		{name: "F on a playlist", moves: []tea.Msg{tabKey, tabKey, tabKey}, key: "F", writes: []string{"remove playlists pl-1"}, notice: "Removed from favorites: Daft Punk Essentials"},
		{name: "already a favorite", key: "f", setup: func(e *env, _ *libFake) { e.svc.favs().set(tidal.FavTracks, "1", true) },
			notice: "One More Time is already a favorite."},
		{name: "write fails", key: "f", setup: func(_ *env, l *libFake) { l.writeErr = tidal.ErrCollectionFull },
			writes: []string{"add tracks 1"}, notice: "Could not update favorites: Your TIDAL collection is full."},
		{name: "shutting down", key: "f", setup: func(e *env, l *libFake) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			e.svc.Ctx, l.writeErr = ctx, context.Canceled
		}, writes: []string{"add tracks 1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, l := newLibEnv(t)
			if tt.setup != nil {
				tt.setup(e, l)
			}
			_, m := searchedWith(t, e)
			drive(m, tt.moves...)
			notices := drive(m, press(tt.key))
			if got := l.gotWrites(); !reflect.DeepEqual(got, tt.writes) {
				t.Errorf("writes = %q, want %q", got, tt.writes)
			}
			var want []string
			if tt.notice != "" {
				want = []string{tt.notice}
			}
			if !reflect.DeepEqual(notices, want) {
				t.Errorf("notices = %q, want %q", notices, want)
			}
		})
	}
}

// searchedWith is searched for an env built by the caller.
func searchedWith(t *testing.T, e *env) (*env, *Model) {
	t.Helper()
	m := e.model(80, 24)
	drive(m, press("/"))
	searchView(m).input.SetValue("daft punk")
	drive(m, press("enter"))
	return e, m
}

func TestFavoritesUpdateViews(t *testing.T) {
	e, l := newLibEnv(t)
	m := e.model(80, 24)
	// Load the Library's Tracks tab, then search.
	drive(m, press("2"), press("l"), tabKey, tabKey)
	drive(m, press("/"))
	drive(m, press("esc"), press("1"))
	drive(m, press("/"))
	searchView(m).input.SetValue("daft punk")
	drive(m, press("enter"))
	// The search rows of library tracks carry the heart.
	if out := ansi.Strip(m.render()); !strings.Contains(out, "♥ One More Time") {
		t.Fatalf("no heart on a known favorite:\n%s", out)
	}
	// Favoriting from search reloads the Library tab.
	before := l.count("fav:tracks")
	drive(m, tabKey, press("f")) // album Discovery
	if l.count("fav:tracks") != before || !e.svc.favs().is(tidal.FavAlbums, "1550545") {
		t.Error("an album favorite reloaded tracks, or was not recorded")
	}
	drive(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, press("j"), press("j"), press("f")) // track 3 is known
	l.favTracks = append(l.favTracks[:0:0], tidal.Page[tidal.Track]{Items: []tidal.Track{tOneMore}})
	drive(m, press("F")) // remove track 3
	lv := libView(m)
	if got := len(lv.tabs[2].all); got != 2 {
		t.Errorf("removed track still listed: %d items", got)
	}
	if strings.Contains(ansi.Strip(m.render()), "♥ Get Lucky") {
		t.Error("heart kept after removal")
	}
	// Adding a track reloads the loaded Tracks tab from the start.
	drive(m, press("f"))
	if l.count("fav:tracks") != before+1 || len(lv.tabs[2].all) != 1 {
		t.Errorf("add did not reload: calls %d items %d", l.count("fav:tracks"), len(lv.tabs[2].all))
	}
	// Removing in the Library drops the row there and keeps the cursor near.
	drive(m, press("2"), press("l"))
	lv.active = 0
	drive(m, press("j"), press("F"))
	if got := len(lv.tabs[0].list.entries); got != 1 || lv.tabs[0].list.entries[0].playlist.ID != "pl-1" {
		t.Errorf("playlist rows after removal = %d", got)
	}
	if lv.Bindings()[0].Help().Key != "/" || lv.Bindings()[1].Help().Key != "tab" {
		t.Error("library bindings")
	}
}

func TestPlaylistPicker(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(l *libFake)
		moves   []string
		writes  []string
		notices []string
		closed  bool
		screen  string
	}{
		{name: "adds and closes", moves: []string{"j"}, writes: []string{"playlist mine-2 1"},
			notices: []string{"Added One More Time to Gym"}, closed: true},
		{name: "duplicate", setup: func(l *libFake) { l.writeErr = fmt.Errorf("x: %w", tidal.ErrDuplicateInPlaylist) },
			writes: []string{"playlist mine-1 1"}, notices: []string{"One More Time is already in Road trip"}, closed: true},
		{name: "error keeps it open", setup: func(l *libFake) { l.writeErr = tidal.ErrPlaylistFull },
			writes: []string{"playlist mine-1 1"}, notices: []string{"Could not add to Road trip: The playlist is full."}},
		{name: "no playlists", setup: func(l *libFake) { l.owned = nil }, screen: "You have no playlists yet."},
		{name: "load error", setup: func(l *libFake) { l.errs["owned"] = tidal.ErrNotFound }, screen: "Could not load your playlists: Not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, l := newLibEnv(t)
			if tt.setup != nil {
				tt.setup(l)
			}
			_, m := searchedWith(t, e)
			drive(m, press("+"))
			pk, ok := m.current().(*PlaylistPicker)
			if !ok {
				t.Fatalf("+ opened %T", m.current())
			}
			if tt.screen != "" {
				if out := ansi.Strip(m.render()); !strings.Contains(out, tt.screen) {
					t.Errorf("screen lacks %q:\n%s", tt.screen, out)
				}
				if n := drive(m, press("enter")); len(n) != 0 || len(l.gotWrites()) != 0 {
					t.Error("enter acted with no playlists")
				}
				return
			}
			if l.count("owned") != 2 || len(pk.playlists) != 2 {
				t.Fatalf("owned calls %d playlists %d", l.count("owned"), len(pk.playlists))
			}
			for _, k := range tt.moves {
				drive(m, press(k))
			}
			notices := drive(m, press("enter"))
			if got := l.gotWrites(); !reflect.DeepEqual(got, tt.writes) {
				t.Errorf("writes = %q", got)
			}
			if !reflect.DeepEqual(notices, tt.notices) {
				t.Errorf("notices = %q, want %q", notices, tt.notices)
			}
			if _, still := m.current().(*PlaylistPicker); still == tt.closed {
				t.Errorf("picker open = %v, want closed %v", still, tt.closed)
			}
		})
	}
}

func TestPlaylistPickerDetails(t *testing.T) {
	e, l := newLibEnv(t)
	_, m := searchedWith(t, e)
	drive(m, press("+"))
	golden(t, "playlist_picker_80x24", m.render())
	pk := m.current().(*PlaylistPicker)
	if pk.Title() != "Add to playlist" || pk.Capturing() || len(pk.Bindings()) != 2 {
		t.Error("picker metadata")
	}
	// Mouse: click selects, double-click adds; wheel scrolls.
	main := m.layout.Main
	drive(m, tea.MouseWheelMsg{X: main.X + 2, Y: main.Y + 3, Button: tea.MouseWheelDown})
	drive(m, tea.MouseClickMsg{X: main.X + 2, Y: main.Y + 3, Button: tea.MouseLeft})
	if pk.list.Cursor() != 1 {
		t.Errorf("click cursor = %d", pk.list.Cursor())
	}
	f := m.now
	m.now = func() time.Time { return f() }
	drive(m, tea.MouseClickMsg{X: main.X + 2, Y: main.Y + 3, Button: tea.MouseLeft})
	if got := l.gotWrites(); !reflect.DeepEqual(got, []string{"playlist mine-2 1"}) {
		t.Errorf("double-click writes = %q", got)
	}
	if l.count("owned") != 2 {
		t.Errorf("picker reloaded its playlists: %d calls", l.count("owned"))
	}
	// A second add while one is in flight is ignored.
	pk2 := NewPlaylistPicker(e.svc, tOneMore)
	if !strings.Contains(ansi.Strip(pk2.Render(60, 5, true)), "Loading your playlists…") {
		t.Error("picker loading state")
	}
	evil := NewPlaylistPicker(e.svc, tOneMore)
	evil.Update(ownedMsg{playlists: []tidal.Playlist{{ID: "a", Name: "A"}, {ID: "b", Name: "x\x1b]52;c;aGk=\x07y"}}})
	if out := evil.Render(60, 5, true); strings.Contains(out, "\x1b]52") || strings.Contains(out, "\x07") {
		t.Errorf("playlist name escape reached the screen: %q", out)
	}
	pk2.Update(ownedMsg{playlists: []tidal.Playlist{{ID: "a", Name: "A"}}})
	if pk2.add(0) == nil || pk2.add(0) != nil || pk2.add(5) != nil {
		t.Error("add guard")
	}
	// + on an album explains; without a Library nothing opens.
	_, m = searchedWith(t, e)
	drive(m, tabKey)
	if n := drive(m, press("+")); !reflect.DeepEqual(n, []string{"Only tracks can be added to a playlist."}) {
		t.Errorf("notices = %q", n)
	}
	e.svc.Library = nil
	if n := drive(m, press("+")); len(n) != 0 {
		t.Error("+ acted without a Library")
	}
	if n := drive(m, press("f")); len(n) != 0 {
		t.Error("f acted without a Library")
	}
}

func TestCloseOnlyClosesTheTopView(t *testing.T) {
	e, _ := newLibEnv(t)
	m := e.model(80, 24)
	pk := NewPlaylistPicker(e.svc, tOneMore)
	drive(m, PushMsg{View: pk})
	other := NewListView("Other", nil)
	drive(m, PushMsg{View: other})
	drive(m, CloseMsg{View: pk})
	if m.current() != other || len(m.stacks[SectionSearch]) != 3 {
		t.Error("closed a view that was not on top")
	}
	drive(m, PopMsg{}, CloseMsg{View: pk})
	if _, ok := m.current().(*SearchView); !ok || m.opened[pk] {
		t.Errorf("picker not closed: %T", m.current())
	}
}

func TestFold(t *testing.T) {
	for in, want := range map[string]string{"Café": "cafe", "BJÖRK": "bjork", "Sigur Rós": "sigur ros", "Łódź": "lodz", "Plain": "plain"} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
	if entryText(entry{kind: 99}) != "" || entryText(entry{kind: entryAlbum, album: aDisc}) != "Discovery Daft Punk" {
		t.Error("entryText")
	}
	if _, _, _, ok := favRef(entry{}); ok {
		t.Error("favRef on an empty entry")
	}
	if (&Services{}).favoriteCmd(entry{kind: entryTrack, track: tOneMore}, true) != nil {
		t.Error("favorite without a Library")
	}
}

func TestLibraryMouse(t *testing.T) {
	e, l := newLibEnv(t)
	var many []tidal.Track
	for i := range 30 {
		many = append(many, track(fmt.Sprint(i+1), "Song"))
	}
	l.favTracks = []tidal.Page[tidal.Track]{{Items: many}}
	m := e.model(80, 24)
	drive(m, press("2"), press("l"), tabKey, tabKey)
	v := libView(m)
	main := m.layout.Main
	// The separator row does nothing; a row click selects; a double-click plays.
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 1, Button: tea.MouseLeft})
	if v.tab().list.Cursor() != 0 || len(e.play.got()) != 0 {
		t.Fatal("separator click acted")
	}
	row := tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseLeft}
	drive(m, row)
	if v.tab().list.Cursor() != 1 {
		t.Errorf("click cursor = %d", v.tab().list.Cursor())
	}
	drive(m, row)
	if got := e.play.got(); len(got) != 1 || got[0].start != 1 {
		t.Errorf("double-click play = %+v", got)
	}
	m.render() // the list learns its height when drawn
	drive(m, tea.MouseWheelMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseWheelDown})
	if v.tab().list.offset == 0 {
		t.Error("wheel did not scroll")
	}
}

func TestLibraryInFlightStates(t *testing.T) {
	e, _ := newLibEnv(t)
	v := NewLibraryView(e.svc)
	v.Open()
	if v.load(0) != nil {
		t.Error("a second load started while one was in flight")
	}
	page := []entry{{kind: entryPlaylist, playlist: pEss}, {kind: entryPlaylist, playlist: tidal.Playlist{ID: "mine-1", Name: "Road trip"}}}
	v.Update(libPageMsg{tab: 0, entries: page, next: "p1"})
	v.filter.SetValue("road")
	v.applyFilter()
	v.tab().pg.loading = true
	if out := ansi.Strip(v.Render(60, 8, true)); !strings.Contains(out, "Filtering 2 loaded items, loading more…") {
		t.Errorf("loading note missing:\n%s", out)
	}
	v.filter.SetValue("zzz")
	v.applyFilter()
	if out := ansi.Strip(v.Render(60, 8, true)); !strings.Contains(out, `No matches for "zzz". Loading more…`) {
		t.Errorf("no-match note missing:\n%s", out)
	}
	// Keys that do not change the text leave the list alone; other messages
	// go to the input (cursor blink).
	v.StartFilter()
	v.filter.SetValue("road")
	v.applyFilter()
	v.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if len(v.tab().list.entries) != 1 {
		t.Error("cursor key refiltered")
	}
	v.Update(struct{}{})
	if entryText(entry{kind: entryPlaylist, playlist: pEss}) != pEss.Name {
		t.Error("playlist entryText")
	}
}

func TestFavoriteRemovedOnInactiveTab(t *testing.T) {
	e, _ := newLibEnv(t)
	v := NewLibraryView(e.svc)
	v.Open()
	v.Update(libPageMsg{tab: 0, entries: []entry{{kind: entryPlaylist, playlist: pEss}}})
	v.switchTab(1)
	v.FavoriteChanged(FavoriteMsg{Collection: tidal.FavPlaylists, ID: pEss.ID})
	if len(v.tabs[0].all) != 0 || len(v.tabs[0].list.entries) != 0 {
		t.Error("removal on an inactive tab kept the row")
	}
}

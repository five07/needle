package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/five07/tidal-tui/internal/tidal"
)

// discFake implements Discovery on top of fakeCatalog's bookkeeping.
type discFake struct {
	*fakeCatalog
	mixes      map[tidal.MixKind][]tidal.Playlist
	mixErr     map[tidal.MixKind]error
	radios     map[string]tidal.Playlist // by "track:ID" or "artist:ID"
	simTracks  []tidal.Page[tidal.Track]
	simArtists []tidal.Page[tidal.Artist]
	simAlbums  []tidal.Page[tidal.Album]
}

func (d *discFake) Mixes(_ context.Context, k tidal.MixKind) ([]tidal.Playlist, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls["mixes:"+string(k)]++
	return d.mixes[k], d.mixErr[k]
}

func (d *discFake) radio(key string) (tidal.Playlist, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls["radio:"+key]++
	if err := d.errs["radio:"+key]; err != nil {
		return tidal.Playlist{}, err
	}
	p, ok := d.radios[key]
	if !ok {
		return tidal.Playlist{}, tidal.ErrNoRadio
	}
	return p, nil
}

func (d *discFake) TrackRadio(_ context.Context, id string) (tidal.Playlist, error) {
	return d.radio("track:" + id)
}
func (d *discFake) ArtistRadio(_ context.Context, id string) (tidal.Playlist, error) {
	return d.radio("artist:" + id)
}
func (d *discFake) SimilarTracks(_ context.Context, id, next string) (tidal.Page[tidal.Track], error) {
	return servePage(d.fakeCatalog, "similar:track:"+id, d.simTracks, next)
}
func (d *discFake) SimilarArtists(_ context.Context, id, next string) (tidal.Page[tidal.Artist], error) {
	return servePage(d.fakeCatalog, "similar:artist:"+id, d.simArtists, next)
}
func (d *discFake) SimilarAlbums(_ context.Context, id, next string) (tidal.Page[tidal.Album], error) {
	return servePage(d.fakeCatalog, "similar:album:"+id, d.simAlbums, next)
}

var (
	mixA     = tidal.Playlist{ID: "00237b2a1d39536f0ef81e836a9176", Name: "My Mix 1", Description: "The Herbaliser, Morcheeba and more", Mix: true}
	mixB     = tidal.Playlist{ID: "0021479b963809ce1ec1d8385ab439", Name: "My Mix 2", Description: "Gorillaz, MGMT and more", Mix: true}
	mixDaily = tidal.Playlist{ID: "016bf035193def3111372b0bd658dd", Name: "My Daily Discovery", Description: "Songs by new and familiar artists", Mix: true}
)

// fakeClock is a settable time source for Services.Now.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// newDiscEnv is newEnv plus a Discovery fake: two Daily mixes, one
// Discovery mix, and no New Release mixes.
func newDiscEnv(t *testing.T) (*env, *discFake, *fakeClock) {
	e := newEnv(t)
	d := &discFake{fakeCatalog: e.cat,
		mixes: map[tidal.MixKind][]tidal.Playlist{
			tidal.DailyMixes: {mixA, mixB}, tidal.DiscoveryMixes: {mixDaily},
		},
		mixErr: map[tidal.MixKind]error{},
		radios: map[string]tidal.Playlist{},
	}
	c := &fakeClock{t: time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)}
	e.svc.Discovery, e.svc.Now = d, c.now
	e.cat.pages["playlist:"+mixA.ID] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tHarder}}}
	return e, d, c
}

func discView(m *Model) *DiscoveryView { return m.stacks[SectionDiscovery][0].(*DiscoveryView) }

func TestDiscoveryTabs(t *testing.T) {
	e, d, _ := newDiscEnv(t)
	m := e.model(80, 24)
	if d.count("mixes:userDailyMixes") != 0 {
		t.Fatal("mixes loaded before Discovery was shown")
	}
	drive(m, press("3"))
	for _, k := range []string{"userDailyMixes", "userDiscoveryMixes", "userNewReleaseMixes"} {
		if d.count("mixes:"+k) != 1 {
			t.Errorf("%s loaded %d times", k, d.count("mixes:"+k))
		}
	}
	golden(t, "discovery_80x24", m.render())
	v := discView(m)
	// The empty New Releases kind has no tab.
	if got := v.titles(); !reflect.DeepEqual(got, []string{"Daily", "Discovery"}) {
		t.Fatalf("tabs = %q", got)
	}
	drive(m, press("l"), tabKey)
	if v.tab().title != "Discovery" || len(v.tab().list.entries) != 1 {
		t.Errorf("tab = %s", v.tab().title)
	}
	drive(m, tabKey, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if v.tab().title != "Discovery" {
		t.Error("shift+tab did not go back")
	}
	// Clicking a tab name selects it; a click beside the tabs does nothing.
	main := m.layout.Main
	drive(m, tea.MouseClickMsg{X: main.X + 1 + 2, Y: main.Y, Button: tea.MouseLeft})
	if v.tab().title != "Daily" {
		t.Errorf("click selected %s", v.tab().title)
	}
	drive(m, tea.MouseClickMsg{X: main.X + 1 + 40, Y: main.Y, Button: tea.MouseLeft})
	if v.tab().title != "Daily" {
		t.Error("a click beside the tabs switched")
	}
	// Hints lead with tab.
	if hints := ansi.Strip(m.hintLine(300)); !strings.HasPrefix(hints, " tab next tab · j/k move · h back · enter") {
		t.Errorf("hints = %q", hints)
	}
	// A row click selects; the wheel goes to the list.
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseLeft})
	if v.tab().list.Cursor() != 1 {
		t.Errorf("row click cursor = %d", v.tab().list.Cursor())
	}
	drive(m, tea.MouseWheelMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseWheelUp})
	// Enter on a mix opens its tracks; the header names it as a mix.
	drive(m, press("k"), press("enter"))
	tl, ok := m.current().(*TrackListView)
	if !ok {
		t.Fatalf("enter opened %T", m.current())
	}
	if e.cat.count("playlist:"+mixA.ID) != 1 || len(tl.list.entries) != 2 {
		t.Errorf("mix tracks: calls %d entries %d", e.cat.count("playlist:"+mixA.ID), len(tl.list.entries))
	}
	golden(t, "mix_80x24", m.render())
	// Queue keys work on a mix row like on a playlist.
	drive(m, press("h"))
	drive(m, press("a"))
	if got := e.play.got(); len(got) != 1 || len(got[0].ids) != 2 {
		t.Errorf("queue = %+v", got)
	}
}

func TestDiscoveryReloadsStaleMixes(t *testing.T) {
	e, d, c := newDiscEnv(t)
	m := e.model(80, 24)
	drive(m, press("3"))
	drive(m, press("1"), press("3")) // back within the hour: no reload
	if d.count("mixes:userDailyMixes") != 1 {
		t.Fatalf("reloaded fresh mixes: %d", d.count("mixes:userDailyMixes"))
	}
	v := discView(m)
	drive(m, press("l"), press("j")) // cursor on My Mix 2
	c.t = c.t.Add(MixMaxAge + time.Minute)
	d.mixes[tidal.DailyMixes] = []tidal.Playlist{mixB, mixA}
	d.mixes[tidal.NewReleaseMixes] = []tidal.Playlist{mixDaily}
	drive(m, press("1"), press("3"))
	if d.count("mixes:userDailyMixes") != 2 {
		t.Fatalf("stale mixes not reloaded: %d", d.count("mixes:userDailyMixes"))
	}
	if got := v.titles(); !reflect.DeepEqual(got, []string{"Daily", "Discovery", "New Releases"}) {
		t.Errorf("tabs after reload = %q", got)
	}
	if v.tab().title != "Daily" || v.tab().list.Cursor() != 1 {
		t.Errorf("reload moved the selection: %s %d", v.tab().title, v.tab().list.Cursor())
	}
	// Moving inside Discovery is not a visit.
	c.t = c.t.Add(2 * MixMaxAge)
	drive(m, press("j"), press("k"))
	if d.count("mixes:userDailyMixes") != 2 {
		t.Error("a key press inside Discovery reloaded")
	}
	// A page from an older load is dropped.
	v.Update(mixesMsg{gen: v.gen - 1, mixes: make([][]tidal.Playlist, 3), errs: make([]error, 3)})
	if len(v.tabs) != 3 {
		t.Error("a stale load replaced the tabs")
	}
	// While a load is in flight, neither Open nor Revisit starts another.
	v.loading = true
	if v.load() != nil || v.Revisit() != nil {
		t.Error("a second load started")
	}
}

func TestDiscoveryStates(t *testing.T) {
	boom := tidal.ErrRateLimited
	tests := []struct {
		name   string
		setup  func(d *discFake)
		screen string
		tabs   []string
		notice string
	}{
		{name: "no mixes at all", setup: func(d *discFake) { d.mixes = map[tidal.MixKind][]tidal.Playlist{} },
			screen: "No mixes yet. TIDAL builds them as you listen."},
		{name: "every kind fails", setup: func(d *discFake) {
			for _, k := range mixKinds {
				d.mixErr[k.kind] = boom
			}
		}, screen: "Could not load your mixes: TIDAL is busy"},
		{name: "one kind fails", setup: func(d *discFake) { d.mixErr[tidal.DiscoveryMixes] = boom },
			tabs: []string{"Daily"}, notice: "Could not load Discovery mixes: TIDAL is busy right now. Try again in a moment."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, d, _ := newDiscEnv(t)
			tt.setup(d)
			m := e.model(80, 24)
			notices := drive(m, press("3"))
			v := discView(m)
			if out := ansi.Strip(m.render()); tt.screen != "" && !strings.Contains(out, tt.screen) {
				t.Errorf("screen lacks %q:\n%s", tt.screen, out)
			}
			if !reflect.DeepEqual(v.titles(), append([]string{}, tt.tabs...)) {
				t.Errorf("tabs = %q, want %q", v.titles(), tt.tabs)
			}
			if tt.notice != "" && !reflect.DeepEqual(notices, []string{tt.notice}) {
				t.Errorf("notices = %q", notices)
			}
			if tt.tabs == nil {
				// No tab: keys and clicks do nothing, and no bindings show.
				if drive(m, press("l"), press("enter")); m.current() != v || v.Bindings() != nil || v.LeadHints() != 0 {
					t.Error("an empty Discovery acted")
				}
				v.Update(ClickMsg{Y: 3})
			}
		})
	}
	// A failed kind keeps its old tab on reload.
	e, d, c := newDiscEnv(t)
	m := e.model(80, 24)
	drive(m, press("3"))
	d.mixErr[tidal.DailyMixes] = boom
	c.t = c.t.Add(2 * MixMaxAge)
	drive(m, press("1"), press("3"))
	if v := discView(m); !reflect.DeepEqual(v.titles(), []string{"Daily", "Discovery"}) || len(v.tabs[0].list.entries) != 2 {
		t.Errorf("tabs after a failed reload = %q", v.titles())
	}
	// Before the first load finishes, the view says it is loading.
	v := NewDiscoveryView(e.svc)
	if !strings.Contains(ansi.Strip(v.Render(60, 5, true)), "Loading your mixes…") || v.Title() != "Discovery" || v.Capturing() {
		t.Error("loading state")
	}
	if v.Revisit() != nil {
		t.Error("Revisit before the first load started one")
	}
}

func TestMixFavorites(t *testing.T) {
	e, d, _ := newDiscEnv(t)
	l := &libFake{fakeCatalog: d.fakeCatalog}
	e.svc.Library = l
	m := e.model(80, 24)
	drive(m, press("3"), press("l"))
	notices := drive(m, press("f"))
	if got := l.gotWrites(); !reflect.DeepEqual(got, []string{"add playlists " + mixA.ID}) {
		t.Errorf("writes = %q", got)
	}
	if !reflect.DeepEqual(notices, []string{"Added to favorites: My Mix 1"}) {
		t.Errorf("notices = %q", notices)
	}
	if !strings.Contains(ansi.Strip(m.render()), "♥ My Mix 1") {
		t.Error("no heart on the favorited mix")
	}
}

var radioMix = tidal.Playlist{ID: "00113cb0e00a7cbbe4b67263d1464d", Name: "One More Time", Description: "Daft Punk", Mix: true}

func TestRadioKey(t *testing.T) {
	tests := []struct {
		name   string
		moves  []tea.Msg
		setup  func(e *env, d *discFake)
		title  string // radio view opened
		notice string
	}{
		{name: "track", title: "Radio: One More Time"},
		{name: "artist", moves: []tea.Msg{tabKey, tabKey}, title: "Radio: Daft Punk"},
		{name: "album", moves: []tea.Msg{tabKey}, notice: "Radio is available for tracks and artists."},
		{name: "playlist", moves: []tea.Msg{tabKey, tabKey, tabKey}, notice: "Radio is available for tracks and artists."},
		{name: "no radio", setup: func(_ *env, d *discFake) { delete(d.radios, "track:1") },
			notice: "TIDAL has no radio for One More Time."},
		{name: "api error", setup: func(_ *env, d *discFake) { d.errs["radio:track:1"] = tidal.ErrUnauthorized },
			notice: "Could not start radio: Your TIDAL session has ended. Restart tidal-tui to log in again."},
		{name: "shutting down", setup: func(e *env, d *discFake) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			e.svc.Ctx, d.errs["radio:track:1"] = ctx, context.Canceled
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, d, _ := newDiscEnv(t)
			d.radios["track:1"] = radioMix
			d.radios["artist:8847"] = tidal.Playlist{ID: "0002737adb8d077f4591c4cdb7ba60", Name: "Daft Punk", Description: "Artist Radio", Mix: true}
			e.cat.pages["playlist:"+radioMix.ID] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tLucky}}}
			if tt.setup != nil {
				tt.setup(e, d)
			}
			_, m := searchedWith(t, e)
			drive(m, tt.moves...)
			notices := drive(m, press("R"))
			var want []string
			if tt.notice != "" {
				want = []string{tt.notice}
			}
			if !reflect.DeepEqual(notices, want) {
				t.Errorf("notices = %q, want %q", notices, want)
			}
			v, opened := m.current().(*TrackListView)
			if opened != (tt.title != "") || opened && v.Title() != tt.title {
				t.Fatalf("current = %T %v, want radio %q", m.current(), m.current().Title(), tt.title)
			}
			if tt.name == "track" {
				golden(t, "radio_80x24", m.render())
				// Enter plays the radio from the selected track.
				drive(m, press("j"), press("enter"))
				if got := e.play.got(); len(got) != 1 || len(got[0].ids) != 2 || got[0].start != 1 {
					t.Errorf("play = %+v", got)
				}
			}
		})
	}
}

func TestRadioAndSimilarFromEveryView(t *testing.T) {
	e, d, _ := newDiscEnv(t)
	d.radios["track:1"] = radioMix
	l := &libFake{fakeCatalog: d.fakeCatalog, favTracks: []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore}}}}
	e.svc.Library = l
	m := e.model(80, 24)
	// Library Tracks tab.
	drive(m, press("2"), press("l"), tabKey, tabKey) // the tab's page loads after these keys
	drive(m, press("R"))
	if v, ok := m.current().(*TrackListView); !ok || v.Title() != "Radio: One More Time" {
		t.Fatalf("R in Library opened %T", m.current())
	}
	drive(m, press("h"), press("S"))
	if v, ok := m.current().(*TrackListView); !ok || v.Title() != "Similar to One More Time" {
		t.Fatalf("S in Library opened %T", m.current())
	}
	// Inside a mix opened from Discovery.
	drive(m, press("3"), press("l")) // mixes load after these keys
	drive(m, press("enter"))
	drive(m, press("S"))
	if v, ok := m.current().(*TrackListView); !ok || v.Title() != "Similar to One More Time" {
		t.Fatalf("S in a mix opened %T %q", m.current(), m.current().Title())
	}
	// On a mix row neither applies.
	drive(m, press("h"), press("h"))
	if n := drive(m, press("R")); !reflect.DeepEqual(n, []string{"Radio is available for tracks and artists."}) {
		t.Errorf("R on a mix = %q", n)
	}
	if n := drive(m, press("S")); !reflect.DeepEqual(n, []string{"Similar is available for tracks, artists, and albums."}) {
		t.Errorf("S on a mix = %q", n)
	}
	// The keys show in the hints and help when Discovery is available.
	keys := ""
	for _, b := range discView(m).Bindings() {
		keys += b.Help().Key + " "
	}
	if !strings.Contains(keys, "R S") {
		t.Errorf("bindings = %q", keys)
	}
	// Without Discovery the keys do nothing.
	e.svc.Discovery = nil
	if n := drive(m, press("R"), press("S")); len(n) != 0 {
		t.Errorf("keys acted without Discovery: %q", n)
	}
}

func TestSimilarKey(t *testing.T) {
	var many []tidal.Track
	for i := range 20 {
		many = append(many, track(string(rune('a'+i)), "Song"))
	}
	tests := []struct {
		name   string
		moves  []tea.Msg
		setup  func(d *discFake)
		title  string
		screen string
		check  func(t *testing.T, e *env, d *discFake, m *Model, v *TrackListView)
	}{
		{name: "track, paged", setup: func(d *discFake) {
			d.simTracks = []tidal.Page[tidal.Track]{{Items: many, Next: "p1"}, {Items: []tidal.Track{tLucky}}}
		}, title: "Similar to One More Time", check: func(t *testing.T, e *env, d *discFake, m *Model, v *TrackListView) {
			for range 16 {
				drive(m, press("j"))
			}
			if d.count("similar:track:1") != 2 || len(v.list.entries) != 21 {
				t.Errorf("paging: calls %d entries %d", d.count("similar:track:1"), len(v.list.entries))
			}
		}},
		{name: "artist", moves: []tea.Msg{tabKey, tabKey}, setup: func(d *discFake) {
			d.simArtists = []tidal.Page[tidal.Artist]{{Items: []tidal.Artist{{ID: "57425", Name: "Justice"}}}}
		}, title: "Similar to Daft Punk", check: func(t *testing.T, e *env, d *discFake, m *Model, v *TrackListView) {
			golden(t, "similar_artists_80x24", m.render())
			drive(m, press("enter"))
			if a, ok := m.current().(*ArtistView); !ok || a.Title() != "Justice" {
				t.Errorf("enter on a similar artist opened %T", m.current())
			}
		}},
		{name: "album", moves: []tea.Msg{tabKey}, setup: func(d *discFake) {
			d.simAlbums = []tidal.Page[tidal.Album]{{Items: []tidal.Album{aRAM}}}
		}, title: "Similar to Discovery", screen: "Random Access Memories"},
		{name: "none", moves: []tea.Msg{tabKey, tabKey}, title: "Similar to Daft Punk", screen: "TIDAL lists no similar artists."},
		{name: "error", moves: []tea.Msg{tabKey}, setup: func(d *discFake) { d.errs["similar:album:1550545"] = tidal.ErrNotFound },
			title: "Similar to Discovery", screen: "Could not load similar albums: Not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, d, _ := newDiscEnv(t)
			if tt.setup != nil {
				tt.setup(d)
			}
			_, m := searchedWith(t, e)
			drive(m, tt.moves...)
			drive(m, press("S"))
			v, ok := m.current().(*TrackListView)
			if !ok || v.Title() != tt.title {
				t.Fatalf("current = %T, want %q", m.current(), tt.title)
			}
			if out := ansi.Strip(m.render()); tt.screen != "" && !strings.Contains(out, tt.screen) {
				t.Errorf("screen lacks %q:\n%s", tt.screen, out)
			}
			if tt.check != nil {
				tt.check(t, e, d, m, v)
			}
		})
	}
	if _, ok := NewSimilarView(&Services{}, entry{kind: entryPlaylist}); ok {
		t.Error("similar view for a playlist")
	}
}

// Detail views redraw their hearts when a favorite changes.
func TestDetailViewsShowFavoriteChanges(t *testing.T) {
	e, d, _ := newDiscEnv(t)
	l := &libFake{fakeCatalog: d.fakeCatalog}
	e.svc.Library = l
	e.cat.pages["artist:8847"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tLucky}}}
	e.cat.pages["album:1550545"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore}}}
	_, m := searchedWith(t, e)
	drive(m, tabKey, press("enter")) // album Discovery
	drive(m, press("f"))
	if !strings.Contains(ansi.Strip(m.render()), "♥ One More Time") {
		t.Errorf("album view lacks the heart:\n%s", ansi.Strip(m.render()))
	}
	drive(m, press("h"), tabKey, press("enter")) // artist Daft Punk
	drive(m, press("f"))
	if !strings.Contains(ansi.Strip(m.render()), "♥ Get Lucky") {
		t.Errorf("artist view lacks the heart:\n%s", ansi.Strip(m.render()))
	}
}

func TestTabAt(t *testing.T) {
	titles := []string{"Daily", "Discovery"} // " Daily " is columns 0-6, a gap at 7, " Discovery " 8-18
	for x, want := range map[int]int{-1: -1, 0: 0, 6: 0, 7: -1, 8: 1, 18: 1, 19: -1} {
		if got := tabAt(titles, x); got != want {
			t.Errorf("tabAt(%d) = %d, want %d", x, got, want)
		}
	}
	if bar := ansi.Strip(tabBar(titles, 0, 30, "sort: x")); !strings.HasSuffix(bar, "sort: x") || len(bar) != 30 {
		t.Errorf("tab bar = %q", bar)
	}
}

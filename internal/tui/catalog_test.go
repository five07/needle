package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/five07/tidal-tui/internal/tidal"
)

// fakeCatalog serves canned pages and counts calls.
type fakeCatalog struct {
	mu        sync.Mutex
	search    func(q string) (tidal.SearchResults, error)
	pages     map[string][]tidal.Page[tidal.Track] // by "album:ID", "playlist:ID", "artist:ID"
	albums    map[string][]tidal.Page[tidal.Album] // by artist ID
	errs      map[string]error                     // by key, returned for every page
	errAfter  map[string]int                       // by key: pages served before errs applies
	calls     map[string]int
	queries   []string
	nexts     []string
	searchCtx []context.Context
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		pages: map[string][]tidal.Page[tidal.Track]{}, albums: map[string][]tidal.Page[tidal.Album]{},
		errs: map[string]error{}, errAfter: map[string]int{}, calls: map[string]int{},
	}
}

func (f *fakeCatalog) count(k string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[k]
}

func (f *fakeCatalog) Search(ctx context.Context, q string) (tidal.SearchResults, error) {
	f.mu.Lock()
	f.calls["search"]++
	f.queries = append(f.queries, q)
	f.searchCtx = append(f.searchCtx, ctx)
	fn := f.search
	f.mu.Unlock()
	if fn == nil {
		return tidal.SearchResults{}, nil
	}
	return fn(q)
}

// page serves page n of key, where n is the index encoded in next ("" or "p<n>").
func (f *fakeCatalog) trackPage(key, next string) (tidal.Page[tidal.Track], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[key]++
	f.nexts = append(f.nexts, next)
	n := 0
	fmt.Sscanf(next, "p%d", &n)
	if err := f.errs[key]; err != nil && n >= f.errAfter[key] {
		return tidal.Page[tidal.Track]{}, err
	}
	ps := f.pages[key]
	if n >= len(ps) {
		return tidal.Page[tidal.Track]{}, nil
	}
	return ps[n], nil
}

func (f *fakeCatalog) AlbumTracks(_ context.Context, id, next string) (tidal.Page[tidal.Track], error) {
	return f.trackPage("album:"+id, next)
}

func (f *fakeCatalog) PlaylistTracks(_ context.Context, id, next string) (tidal.Page[tidal.Track], error) {
	return f.trackPage("playlist:"+id, next)
}

func (f *fakeCatalog) ArtistTracks(_ context.Context, id, next string) (tidal.Page[tidal.Track], error) {
	return f.trackPage("artist:"+id, next)
}

func (f *fakeCatalog) ArtistAlbums(_ context.Context, id, next string) (tidal.Page[tidal.Album], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := "albums:" + id
	f.calls[key]++
	n := 0
	fmt.Sscanf(next, "p%d", &n)
	if err := f.errs[key]; err != nil && n >= f.errAfter[key] {
		return tidal.Page[tidal.Album]{}, err
	}
	ps := f.albums[id]
	if n >= len(ps) {
		return tidal.Page[tidal.Album]{}, nil
	}
	return ps[n], nil
}

// playCall is one recorded Playback call.
type playCall struct {
	op     string // "play", "next", "add"
	ids    []string
	start  int
	hasCtx bool
}

type fakePlayback struct {
	mu    sync.Mutex
	calls []playCall
	err   error
}

func ids(ts []tidal.Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}

func (p *fakePlayback) record(ctx context.Context, op string, ts []tidal.Track, start int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, deadline := ctx.Deadline()
	p.calls = append(p.calls, playCall{op: op, ids: ids(ts), start: start, hasCtx: deadline})
	return p.err
}

func (p *fakePlayback) PlayFrom(ctx context.Context, ts []tidal.Track, start int) error {
	return p.record(ctx, "play", ts, start)
}
func (p *fakePlayback) PlayNext(ctx context.Context, ts []tidal.Track) error {
	return p.record(ctx, "next", ts, 0)
}
func (p *fakePlayback) Add(ctx context.Context, ts []tidal.Track) error {
	return p.record(ctx, "add", ts, 0)
}

func (p *fakePlayback) got() []playCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]playCall(nil), p.calls...)
}

// Test data.
func track(id, title string) tidal.Track {
	return tidal.Track{ID: id, Title: title, Artists: []string{"Daft Punk"}, Album: "Discovery", AlbumID: "1550545", Duration: 320 * time.Second}
}

var (
	tOneMore = track("1", "One More Time")
	tHarder  = tidal.Track{ID: "2", Title: "Harder, Better, Faster, Stronger", Artists: []string{"Daft Punk"}, Album: "Discovery", Duration: 224 * time.Second}
	tLucky   = tidal.Track{ID: "3", Title: "Get Lucky", Version: "feat. Pharrell Williams", Artists: []string{"Daft Punk", "Pharrell Williams"}, Album: "Random Access Memories", Duration: 369 * time.Second}
	aDisc    = tidal.Album{ID: "1550545", Title: "Discovery", Artists: []string{"Daft Punk"}, Year: "2001", Tracks: 14, Duration: 3669 * time.Second}
	aRAM     = tidal.Album{ID: "26000", Title: "Random Access Memories", Artists: []string{"Daft Punk"}, Year: "2013", Tracks: 13}
	arDaft   = tidal.Artist{ID: "8847", Name: "Daft Punk"}
	pEss     = tidal.Playlist{ID: "pl-1", Name: "Daft Punk Essentials", Editorial: true, Tracks: 40, Duration: 2 * time.Hour}
	results  = tidal.SearchResults{
		Tracks: []tidal.Track{tOneMore, tHarder, tLucky}, Albums: []tidal.Album{aDisc, aRAM},
		Artists: []tidal.Artist{arDaft}, Playlists: []tidal.Playlist{pEss},
	}
)

// env is a Services setup whose debounce ticks are held until fired.
type env struct {
	t     *testing.T
	cat   *fakeCatalog
	play  *fakePlayback
	svc   *Services
	ticks []func() tea.Msg
	log   *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	e := &env{t: t, cat: newFakeCatalog(), play: &fakePlayback{}, log: &bytes.Buffer{}}
	e.cat.search = func(string) (tidal.SearchResults, error) { return results, nil }
	e.svc = &Services{
		Catalog: e.cat, Playback: e.play,
		Logger: slog.New(slog.NewTextHandler(e.log, nil)),
		Tick: func(_ time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
			return func() tea.Msg {
				e.ticks = append(e.ticks, func() tea.Msg { return fn(time.Time{}) })
				return nil
			}
		},
	}
	return e
}

// run executes cmd and returns the messages it produced, expanding
// batches. Commands that do not finish quickly are timers (cursor blink,
// notice expiry) and are skipped; the fakes answer at once.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(40 * time.Millisecond):
		return nil
	}
	switch m := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, run(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// drive feeds msg to m and every message its commands produce back into
// m, recording notices without running their timers.
func drive(m *Model, msgs ...tea.Msg) (notices []string) {
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := m.Update(msg)
		if n, ok := msg.(NoticeMsg); ok {
			notices = append(notices, n.Text)
			continue
		}
		msgs = append(msgs, run(cmd)...)
	}
	return notices
}

// typeQuery focuses the search input of m and types q, one key per rune.
func typeQuery(m *Model, q string) {
	drive(m, press("/"))
	for _, r := range q {
		drive(m, press(string(r)))
	}
}

func (e *env) fire(m *Model) []string {
	ticks := e.ticks
	e.ticks = nil
	var ns []string
	for _, tk := range ticks {
		ns = append(ns, drive(m, tk())...)
	}
	return ns
}

func (e *env) model(w, h int) *Model {
	m := New(Config{Services: e.svc})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func searchView(m *Model) *SearchView { return m.stacks[SectionSearch][0].(*SearchView) }

func TestSearchDebounce(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	typeQuery(m, "daf")
	if e.cat.count("search") != 0 || len(e.ticks) != 3 {
		t.Fatalf("searched while typing: calls %d ticks %d", e.cat.count("search"), len(e.ticks))
	}
	// The ticks for "d" and "da" are stale: more typing followed them.
	ticks := e.ticks
	e.ticks = nil
	drive(m, ticks[0](), ticks[1]())
	if n := e.cat.count("search"); n != 0 {
		t.Fatalf("a stale tick searched (%d calls)", n)
	}
	drive(m, ticks[2]())
	if e.cat.count("search") != 1 || e.cat.queries[0] != "daf" {
		t.Fatalf("searches = %v", e.cat.queries)
	}
	if s := searchView(m); s.state != searchDone || len(s.results.entries) != 11 {
		t.Errorf("state %d entries %d", s.state, len(s.results.entries))
	}
	if len(e.play.got()) != 0 {
		t.Error("search touched playback")
	}
}

func TestSearchEnterSearchesNow(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	typeQuery(m, "air")
	drive(m, press("enter"))
	if e.cat.count("search") != 1 || searchView(m).Capturing() {
		t.Fatalf("enter: calls %d", e.cat.count("search"))
	}
	e.fire(m) // the debounce from typing is obsolete
	drive(m, press("/"), press("enter"))
	if e.cat.count("search") != 1 {
		t.Errorf("repeat searches: %v", e.cat.queries)
	}
}

func TestSearchStaleResponsesAreDropped(t *testing.T) {
	e := newEnv(t)
	e.cat.search = func(q string) (tidal.SearchResults, error) {
		return tidal.SearchResults{Artists: []tidal.Artist{{ID: "1", Name: "result for " + q}}}, nil
	}
	s := NewSearchView(e.svc)
	s.Update(FocusInputMsg{})
	s.Update(press("a"))
	first := s.Update(press("enter"))
	s.Update(FocusInputMsg{})
	s.Update(press("b"))
	second := s.Update(press("enter"))

	// The newer response arrives first, then the old one.
	for _, cmd := range []tea.Cmd{second, first} {
		for _, msg := range run(cmd) {
			s.Update(msg.(ForViewMsg).Msg)
		}
	}
	if got := s.results.entries[1].artist.Name; got != "result for ab" {
		t.Errorf("shown %q, want the newer results", got)
	}
	if e.cat.searchCtx[0].Err() == nil {
		t.Error("the superseded request was not canceled")
	}
	if e.cat.searchCtx[1].Err() == nil {
		t.Error("the finished request's context was not released")
	}
}

func TestSearchEditing(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	typeQuery(m, "ab")
	e.fire(m)
	// Editing back to the shown query does not search again.
	drive(m, press("c"), press("backspace"))
	e.fire(m)
	if e.cat.count("search") != 1 {
		t.Errorf("searches = %v", e.cat.queries)
	}
	// Whitespace around the query is ignored.
	drive(m, press(" "))
	e.fire(m)
	if e.cat.count("search") != 1 {
		t.Errorf("trailing space searched: %v", e.cat.queries)
	}
	// Clearing the input clears the results and searches nothing.
	drive(m, press("backspace"), press("backspace"), press("backspace"))
	e.fire(m)
	s := searchView(m)
	if s.state != searchIdle || len(s.results.entries) != 0 || e.cat.count("search") != 1 {
		t.Errorf("cleared: state %d entries %d", s.state, len(s.results.entries))
	}
	if !strings.Contains(ansi.Strip(m.render()), "Type to search") {
		t.Error("idle hint missing")
	}
}

func TestSearchStates(t *testing.T) {
	tests := []struct {
		name string
		err  error
		res  tidal.SearchResults
		want string
	}{
		{"no results", nil, tidal.SearchResults{}, `No results for "zz".`},
		{"session ended", fmt.Errorf("wrap: %w", tidal.ErrUnauthorized), tidal.SearchResults{}, "Search failed: Your TIDAL session has ended."},
		{"other error", errors.New("dial tcp 10.0.0.1: secret internals"), tidal.SearchResults{}, "Search failed: Something went wrong. See the log"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.cat.search = func(string) (tidal.SearchResults, error) { return tt.res, tt.err }
			m := e.model(80, 24)
			typeQuery(m, "zz")
			drive(m, press("enter"))
			out := ansi.Strip(m.render())
			if !strings.Contains(out, tt.want) {
				t.Errorf("screen lacks %q:\n%s", tt.want, out)
			}
			if strings.Contains(out, "internals") {
				t.Error("error details leaked to the screen")
			}
			if tt.err != nil && !strings.Contains(e.log.String(), "search failed") {
				t.Error("failure not logged")
			}
			// A failed query can be retried as is.
			drive(m, press("/"), press("enter"))
			if tt.err != nil && e.cat.count("search") != 2 {
				t.Errorf("retry: calls %d", e.cat.count("search"))
			}
		})
	}
}

func TestSearchCanceledResponseIsQuiet(t *testing.T) {
	e := newEnv(t)
	e.cat.search = func(string) (tidal.SearchResults, error) { return tidal.SearchResults{}, context.Canceled }
	m := e.model(80, 24)
	typeQuery(m, "x")
	drive(m, press("enter"))
	if s := searchView(m); s.state != searchLoading || e.log.Len() != 0 {
		t.Errorf("state %d log %q", s.state, e.log.String())
	}
}

func TestSearchUnavailable(t *testing.T) {
	for _, svc := range []*Services{nil, {}} {
		s := NewSearchView(svc)
		s.Update(FocusInputMsg{})
		if cmd := s.Update(press("x")); len(run(cmd)) != 0 {
			t.Error("edit scheduled work without a catalog")
		}
		if cmd := s.Update(press("enter")); cmd != nil {
			t.Error("enter searched without a catalog")
		}
		if out := ansi.Strip(s.Render(40, 5, true)); !strings.Contains(out, "Search is not available.") {
			t.Errorf("render = %q", out)
		}
	}
	if NewSearchView(nil).Render(40, 2, true) == "" {
		t.Error("input missing at height 2")
	}
}

// searched returns a model showing the standard results with the main
// pane focused on the results.
func searched(t *testing.T, w, h int) (*env, *Model) {
	e := newEnv(t)
	m := e.model(w, h)
	drive(m, press("/"))
	searchView(m).input.SetValue("daft punk") // typing is covered elsewhere
	drive(m, press("enter"))
	return e, m
}

func TestSearchGoldenScreens(t *testing.T) {
	tests := []struct {
		name  string
		w, h  int
		setup func(e *env, m *Model)
	}{
		{"search_results_80x24", 80, 24, nil},
		{"search_results_group_80x24", 80, 24, func(_ *env, m *Model) { drive(m, press("tab"), press("j")) }},
		{"search_results_compact_60x18", 60, 18, nil},
		{"search_results_wide_120x30", 120, 30, nil},
		{"album_80x24", 80, 24, func(e *env, m *Model) {
			e.cat.pages["album:1550545"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tHarder}}}
			drive(m, press("tab"), press("enter"))
		}},
		{"artist_80x24", 80, 24, func(e *env, m *Model) {
			e.cat.pages["artist:8847"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tLucky}}}
			e.cat.albums["8847"] = []tidal.Page[tidal.Album]{{Items: []tidal.Album{aDisc, aRAM}}}
			drive(m, press("tab"), press("tab"), press("enter"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, m := searched(t, tt.w, tt.h)
			if tt.setup != nil {
				tt.setup(e, m)
			}
			golden(t, tt.name, m.render())
		})
	}
}

func TestResultNavigation(t *testing.T) {
	_, m := searched(t, 80, 40)
	s := searchView(m)
	at := func() entry { en, _ := s.results.selected(); return en }
	if at().track.ID != "1" {
		t.Fatalf("first selection = %+v", at())
	}
	steps := []struct {
		key  string
		want string // what is selected after the key
	}{
		{"k", "track 1"}, // stays: the row above is a heading
		{"j", "track 2"},
		{"j", "track 3"},
		{"j", "album 1550545"}, // skips the Albums heading
		{"k", "track 3"},
		{"tab", "album 1550545"},
		{"tab", "artist 8847"},
		{"tab", "playlist pl-1"},
		{"tab", "track 1"}, // wraps
		{"shift+tab", "playlist pl-1"},
		{"g", "track 1"},
		{"G", "playlist pl-1"},
	}
	for _, st := range steps {
		k := press(st.key)
		switch st.key {
		case "tab":
			k = tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+tab":
			k = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		}
		drive(m, k)
		if got := describe(at()); got != st.want {
			t.Fatalf("after %s: selected %s, want %s", st.key, got, st.want)
		}
	}
	// Clicking a heading changes nothing; clicking an item selects it.
	main := m.layout.Main
	drive(m, tea.MouseClickMsg{X: main.X + 2, Y: main.Y + 2, Button: tea.MouseLeft}) // TRACKS heading
	if describe(at()) != "playlist pl-1" {
		t.Errorf("heading click selected %s", describe(at()))
	}
	drive(m, tea.MouseClickMsg{X: main.X + 2, Y: main.Y + 4, Button: tea.MouseLeft})
	if describe(at()) != "track 2" {
		t.Errorf("click selected %s", describe(at()))
	}
	drive(m, tea.MouseWheelMsg{X: main.X + 2, Y: main.Y + 4, Button: tea.MouseWheelDown})
	if _, ok := s.results.selected(); !ok {
		t.Error("wheel left the cursor on a heading")
	}
	// Bindings list the item keys once there are results.
	var keys []string
	for _, b := range s.Bindings() {
		keys = append(keys, b.Help().Key)
	}
	if strings.Join(keys, " ") != "enter a A f F + tab shift+tab" {
		t.Errorf("bindings = %v", keys)
	}
}

func describe(en entry) string {
	switch en.kind {
	case entryTrack:
		return "track " + en.track.ID
	case entryAlbum:
		return "album " + en.album.ID
	case entryArtist:
		return "artist " + en.artist.ID
	case entryPlaylist:
		return "playlist " + en.playlist.ID
	}
	return "heading"
}

func TestResultActions(t *testing.T) {
	many := make([]tidal.Track, 0, 600)
	var pages []tidal.Page[tidal.Track]
	for p := range 6 {
		var items []tidal.Track
		for i := range 100 {
			items = append(items, track(fmt.Sprint(p*100+i), "t"))
		}
		many = append(many, items...)
		next := fmt.Sprintf("p%d", p+1)
		if p == 5 {
			next = ""
		}
		pages = append(pages, tidal.Page[tidal.Track]{Items: items, Next: next})
	}
	tests := []struct {
		name      string
		moves     []string
		key       string
		setup     func(e *env)
		want      []playCall
		notice    string
		pushed    string
		pageCalls int
	}{
		{name: "enter plays the track list from the selection", moves: []string{"j"}, key: "enter",
			want: []playCall{{op: "play", ids: []string{"1", "2", "3"}, start: 1, hasCtx: true}}, notice: "Playing Harder, Better, Faster, Stronger"},
		{name: "a adds a track", moves: []string{"j", "j"}, key: "a",
			want: []playCall{{op: "add", ids: []string{"3"}, hasCtx: true}}, notice: "Added to queue: Get Lucky (feat. Pharrell Williams)"},
		{name: "A plays a track next", key: "A",
			want: []playCall{{op: "next", ids: []string{"1"}, hasCtx: true}}, notice: "Playing next: One More Time"},
		{name: "enter opens an album", moves: []string{"tab"}, key: "enter", pushed: "*tui.TrackListView"},
		{name: "enter opens an artist", moves: []string{"tab", "tab"}, key: "enter", pushed: "*tui.ArtistView"},
		{name: "enter opens a playlist", moves: []string{"G"}, key: "enter", pushed: "*tui.TrackListView"},
		{name: "a queues every album page", moves: []string{"tab"}, key: "a",
			setup: func(e *env) {
				e.cat.pages["album:1550545"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tHarder}, Next: "p1"}, {Items: []tidal.Track{tLucky}}}
			},
			want:   []playCall{{op: "add", ids: []string{"1", "2", "3"}, hasCtx: true}},
			notice: "Added to queue: 3 tracks from Discovery", pageCalls: 2},
		{name: "A queues a playlist next, capped", moves: []string{"G"}, key: "A",
			setup:  func(e *env) { e.cat.pages["playlist:pl-1"] = pages },
			want:   []playCall{{op: "next", ids: ids(many[:MaxQueueTracks]), hasCtx: true}},
			notice: "Playing next: 500 tracks from Daft Punk Essentials (first 500)", pageCalls: 5},
		{name: "empty album", moves: []string{"tab"}, key: "a", notice: "Discovery has no playable tracks.", pageCalls: 1},
		{name: "album load error", moves: []string{"tab"}, key: "A",
			setup:  func(e *env) { e.cat.errs["album:1550545"] = tidal.ErrNotFound },
			notice: "Could not queue Discovery: Not found on TIDAL.", pageCalls: 1},
		{name: "album load canceled", moves: []string{"tab"}, key: "A",
			setup: func(e *env) { e.cat.errs["album:1550545"] = context.Canceled }, pageCalls: 1},
		{name: "artist rows are not queued", moves: []string{"tab", "tab"}, key: "a", notice: "Open the artist to queue its tracks."},
		{name: "playback error", key: "enter",
			setup:  func(e *env) { e.play.err = errors.New("ipc: broken pipe /run/user/1000/x") },
			want:   []playCall{{op: "play", ids: []string{"1", "2", "3"}, hasCtx: true}},
			notice: "Could not play the track. See the log for details."},
		{name: "queue error", key: "a",
			setup:  func(e *env) { e.play.err = errors.New("boom") },
			want:   []playCall{{op: "add", ids: []string{"1"}, hasCtx: true}},
			notice: "Could not queue the track. See the log for details."},
		{name: "player timeout", key: "A",
			setup:  func(e *env) { e.play.err = context.DeadlineExceeded },
			want:   []playCall{{op: "next", ids: []string{"1"}, hasCtx: true}},
			notice: "The player did not respond in time."},
		{name: "player stopping", key: "A",
			setup: func(e *env) { e.play.err = context.Canceled },
			want:  []playCall{{op: "next", ids: []string{"1"}, hasCtx: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, m := searched(t, 80, 24)
			if tt.setup != nil {
				tt.setup(e)
			}
			for _, mv := range tt.moves {
				k := press(mv)
				if mv == "tab" {
					k = tea.KeyPressMsg{Code: tea.KeyTab}
				}
				drive(m, k)
			}
			before := len(m.stacks[SectionSearch])
			notices := drive(m, press(tt.key))

			if got := e.play.got(); !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("playback calls = %+v, want %+v", got, tt.want)
			}
			wantN := []string(nil)
			if tt.notice != "" {
				wantN = []string{tt.notice}
			}
			if !reflect.DeepEqual(notices, wantN) {
				t.Errorf("notices = %q, want %q", notices, wantN)
			}
			if tt.pushed != "" {
				if len(m.stacks[SectionSearch]) != before+1 || fmt.Sprintf("%T", m.current()) != tt.pushed {
					t.Errorf("pushed %T, want %s", m.current(), tt.pushed)
				}
			}
			calls := 0
			for k, n := range e.cat.calls {
				if k != "search" && !strings.HasPrefix(k, "artist:") && !strings.HasPrefix(k, "albums:") {
					calls += n
				}
			}
			if tt.pushed == "" && calls != tt.pageCalls {
				t.Errorf("page calls = %d, want %d", calls, tt.pageCalls)
			}
		})
	}
}

func TestActionsWithoutPlayback(t *testing.T) {
	e := newEnv(t)
	e.svc.Playback = nil
	m := e.model(80, 24)
	typeQuery(m, "x")
	drive(m, press("enter"))
	for _, k := range []string{"enter", "a", "A"} {
		if n := drive(m, press(k)); len(n) != 0 {
			t.Errorf("%s: notices %q", k, n)
		}
	}
	drive(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if n := drive(m, press("a")); len(n) != 0 {
		t.Errorf("album queue without playback: %q", n)
	}
	// An empty list has no actions or item bindings.
	var l entryList
	if l.activate() != nil || l.queue(true) != nil || l.bindings(true) != nil {
		t.Error("empty list acted")
	}
	l.jumpGroup(1)
}

func TestAlbumView(t *testing.T) {
	e := newEnv(t)
	var p1, p2 []tidal.Track
	for i := range 20 {
		p1 = append(p1, track(fmt.Sprint(i), fmt.Sprintf("Song %02d", i)))
		p2 = append(p2, track(fmt.Sprint(100+i), "Later"))
	}
	e.cat.pages["album:1550545"] = []tidal.Page[tidal.Track]{{Items: p1, Next: "p1"}, {Items: p2}}
	m := e.model(80, 24)
	v := NewAlbumView(e.svc, aDisc)
	if out := ansi.Strip(v.Render(60, 6, true)); !strings.Contains(out, "Loading…") || !strings.Contains(out, "Daft Punk · 2001 · 14 tracks · 1:01:09") {
		t.Errorf("before load: %q", out)
	}
	drive(m, PushMsg{View: v})
	if e.cat.count("album:1550545") != 1 || len(v.list.entries) != 20 {
		t.Fatalf("open: calls %d entries %d", e.cat.count("album:1550545"), len(v.list.entries))
	}
	// Moving near the end loads the next page, once.
	for range 14 {
		drive(m, press("j"))
	}
	if e.cat.count("album:1550545") != 1 {
		t.Fatalf("loaded early: %d", e.cat.count("album:1550545"))
	}
	drive(m, press("j"))
	if e.cat.count("album:1550545") != 2 || len(v.list.entries) != 40 || e.cat.nexts[1] != "p1" {
		t.Fatalf("paging: calls %d entries %d nexts %v", e.cat.count("album:1550545"), len(v.list.entries), e.cat.nexts)
	}
	drive(m, press("G"))
	if e.cat.count("album:1550545") != 2 {
		t.Error("loaded past the last page")
	}
	// Enter plays the whole loaded list from the selection.
	drive(m, press("enter"))
	if got := e.play.got(); len(got) != 1 || len(got[0].ids) != 40 || got[0].start != 39 {
		t.Errorf("play = %+v", got)
	}
	// A click selects relative to the list, under the 3 header rows.
	drive(m, press("g"))
	drive(m, tea.MouseClickMsg{X: m.layout.Main.X + 2, Y: m.layout.Main.Y + 4, Button: tea.MouseLeft})
	if v.list.Cursor() != 1 {
		t.Errorf("click cursor = %d", v.list.Cursor())
	}
	if v.Title() != "Discovery" || v.Capturing() || len(v.Bindings()) != 6 {
		t.Error("view metadata")
	}
	// h goes back to the results.
	drive(m, press("h"))
	if _, ok := m.current().(*SearchView); !ok {
		t.Errorf("back: %T", m.current())
	}
}

func TestTrackListStates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(c *fakeCatalog)
		view  func(svc *Services) *TrackListView
		moves int
		want  string
	}{
		{"empty playlist", nil, func(s *Services) *TrackListView { return NewPlaylistView(s, pEss) }, 0, "This playlist has no playable tracks."},
		{"empty album", nil, func(s *Services) *TrackListView { return NewAlbumView(s, aRAM) }, 0, "This album has no playable tracks."},
		{"first page fails", func(c *fakeCatalog) { c.errs["playlist:pl-1"] = tidal.ErrRateLimited },
			func(s *Services) *TrackListView { return NewPlaylistView(s, pEss) }, 0, "Could not load tracks: TIDAL is busy right now."},
		{"later page fails", func(c *fakeCatalog) {
			c.pages["playlist:pl-1"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tHarder}, Next: "p1"}}
			c.errs["playlist:pl-1"], c.errAfter["playlist:pl-1"] = tidal.ErrUnauthorized, 1
		}, func(s *Services) *TrackListView { return NewPlaylistView(s, pEss) }, 0, "Could not load more: Your TIDAL session has ended."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.setup != nil {
				tt.setup(e.cat)
			}
			m := e.model(80, 24)
			v := tt.view(e.svc)
			drive(m, PushMsg{View: v})
			out := ansi.Strip(m.render())
			if !strings.Contains(out, tt.want) {
				t.Errorf("screen lacks %q:\n%s", tt.want, out)
			}
			if strings.HasPrefix(tt.name, "later") {
				if !strings.Contains(out, "One More Time") {
					t.Error("loaded tracks hidden by the error")
				}
				// The failed page is requested once, and not again on keys.
				drive(m, press("j"), press("k"), press("j"))
				if got := e.cat.count("playlist:pl-1"); got != 2 {
					t.Errorf("page requests = %d, want 2 (first page, then the failed one)", got)
				}
			}
		})
	}
	// The playlist header names TIDAL as the curator.
	e := newEnv(t)
	if out := ansi.Strip(NewPlaylistView(e.svc, pEss).Render(60, 3, true)); !strings.Contains(out, "TIDAL · 40 tracks · 2:00:00") {
		t.Errorf("playlist header = %q", out)
	}
	if out := NewPlaylistView(e.svc, pEss).Render(60, 1, true); strings.Count(out, "\n") > 1 {
		t.Errorf("tiny render = %q", out)
	}
}

func TestArtistView(t *testing.T) {
	e := newEnv(t)
	var albums []tidal.Album
	for i := range 20 {
		albums = append(albums, tidal.Album{ID: fmt.Sprint(i), Title: fmt.Sprintf("Album %02d", i)})
	}
	e.cat.pages["artist:8847"] = []tidal.Page[tidal.Track]{{Items: []tidal.Track{tOneMore, tLucky}, Next: "p1"}}
	e.cat.albums["8847"] = []tidal.Page[tidal.Album]{{Items: albums, Next: "p1"}, {Items: []tidal.Album{aDisc}}}
	m := e.model(80, 24)
	v := NewArtistView(e.svc, arDaft)
	if !strings.Contains(ansi.Strip(v.Render(40, 4, true)), "Loading…") {
		t.Error("loading state")
	}
	drive(m, PushMsg{View: v})
	// 2 headings + 2 tracks + 20 albums; top tracks load one page only.
	if len(v.list.entries) != 24 || e.cat.count("artist:8847") != 1 || e.cat.count("albums:8847") != 1 {
		t.Fatalf("entries %d calls %v", len(v.list.entries), e.cat.calls)
	}
	drive(m, press("G"))
	if e.cat.count("albums:8847") != 2 || len(v.list.entries) != 25 {
		t.Fatalf("album paging: entries %d calls %v", len(v.list.entries), e.cat.calls)
	}
	// Enter on a top track plays the top tracks; on an album opens it.
	drive(m, press("g"), press("j"), press("enter"))
	if got := e.play.got(); len(got) != 1 || !reflect.DeepEqual(got[0].ids, []string{"1", "3"}) || got[0].start != 1 {
		t.Errorf("play = %+v", got)
	}
	drive(m, tea.KeyPressMsg{Code: tea.KeyTab}, press("enter"))
	if al, ok := m.current().(*TrackListView); !ok || al.Title() != "Album 00" {
		t.Errorf("opened %T", m.current())
	}
	drive(m, press("h"))
	drive(m, tea.MouseClickMsg{X: m.layout.Main.X + 2, Y: m.layout.Main.Y + 3, Button: tea.MouseLeft})
	if en, _ := v.list.selected(); en.track.ID != "1" {
		t.Errorf("click selected %s", describe(en))
	}
	if v.Title() != "Daft Punk" || v.Capturing() || len(v.Bindings()) != 8 {
		t.Error("view metadata")
	}
}

func TestArtistViewOrderAndErrors(t *testing.T) {
	e := newEnv(t)
	e.cat.albums["8847"] = []tidal.Page[tidal.Album]{{Items: []tidal.Album{aDisc, aRAM}}}
	v := NewArtistView(e.svc, arDaft)
	// Albums arrive first and the user selects one; then the tracks arrive
	// above them and the selection follows the album.
	v.Update(pageMsg[tidal.Album]{part: partAlbums, page: tidal.Page[tidal.Album]{Items: []tidal.Album{aDisc, aRAM}}})
	v.list.Move(1)
	v.Update(pageMsg[tidal.Track]{part: partTracks, page: tidal.Page[tidal.Track]{Items: []tidal.Track{tOneMore, tHarder}}})
	if en, _ := v.list.selected(); en.album.ID != aRAM.ID {
		t.Errorf("selection moved to %s", describe(en))
	}

	tests := []struct {
		name string
		errs map[string]error
		want []string
	}{
		{"nothing", nil, []string{"No tracks or albums for this artist."}},
		{"both fail", map[string]error{"artist:8847": tidal.ErrNotFound, "albums:8847": tidal.ErrRateLimited},
			[]string{"Could not load top tracks: Not found on TIDAL.", "Could not load albums: TIDAL is busy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			for k, err := range tt.errs {
				e.cat.errs[k] = err
			}
			m := e.model(80, 24)
			drive(m, PushMsg{View: NewArtistView(e.svc, arDaft)})
			out := ansi.Strip(m.render())
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("screen lacks %q:\n%s", w, out)
				}
			}
		})
	}
	// Loaded albums stay visible with a note when the tracks fail.
	e = newEnv(t)
	e.cat.errs["artist:8847"] = tidal.ErrNotFound
	e.cat.albums["8847"] = []tidal.Page[tidal.Album]{{Items: []tidal.Album{aDisc}}}
	m := e.model(80, 24)
	drive(m, PushMsg{View: NewArtistView(e.svc, arDaft)})
	if out := ansi.Strip(m.render()); !strings.Contains(out, "Discovery") || !strings.Contains(out, "Could not load top tracks") {
		t.Errorf("partial failure:\n%s", out)
	}
}

func TestResultsRouteToTheirView(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	typeQuery(m, "daft")
	_, cmd := m.Update(press("enter"))
	// The user switches to Library before the response arrives.
	drive(m, press("esc"), press("h"), press("2"))
	msgs := run(cmd)
	drive(m, msgs...)
	if s := searchView(m); s.state != searchDone || len(s.results.entries) == 0 {
		t.Errorf("results lost after switching sections: state %d", s.state)
	}
	if m.section != SectionLibrary {
		t.Error("the response changed the section")
	}
	// A debounce that fires after the user left Search still searches.
	drive(m, press("/"), press("x"), press("esc"), press("h"), press("3"))
	e.fire(m)
	if got := e.cat.queries; len(got) != 2 || got[1] != "daftx" {
		t.Errorf("searches = %q", got)
	}
	// A response for a view that was closed is dropped.
	closed := NewAlbumView(e.svc, aDisc)
	drive(m, ForViewMsg{View: closed, Msg: pageMsg[tidal.Track]{page: tidal.Page[tidal.Track]{Items: []tidal.Track{tOneMore}}}})
	if len(closed.list.entries) != 0 {
		t.Error("closed view received a message")
	}
}

func TestNoticesChannelAndSanitizing(t *testing.T) {
	ch := make(chan string, 1)
	m := New(Config{Notices: ch})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	ch <- "mpv was not found\x1b]0;pwned\x07"
	msgs := run(m.Init())
	if len(msgs) != 1 {
		t.Fatalf("init msgs = %v", msgs)
	}
	_, cmd := m.Update(msgs[0])
	if m.notice != "" {
		t.Error("notice shown before its message")
	}
	got := run(cmd) // Notify, then listen again (blocks: skipped)
	drive(m, got...)
	if m.notice != "mpv was not found" {
		t.Errorf("notice = %q", m.notice)
	}
	close(ch)
	if msgs := run(m.listen()); len(msgs) != 0 {
		t.Errorf("closed channel produced %v", msgs)
	}
	if New(Config{}).Init() != nil {
		t.Error("init without a channel")
	}
}

func TestUserMessage(t *testing.T) {
	tests := map[error]string{
		tidal.ErrUnauthorized:      "Your TIDAL session has ended. Restart tidal-tui to log in again.",
		tidal.ErrRateLimited:       "TIDAL is busy right now. Try again in a moment.",
		tidal.ErrNotFound:          "Not found on TIDAL.",
		tidal.ErrInvalidResourceID: "Not found on TIDAL.",
		context.DeadlineExceeded:   "TIDAL did not respond in time. Check your connection.",
		errors.New("x"):            "Something went wrong. See the log for details.",
	}
	for err, want := range tests {
		if got := userMessage(fmt.Errorf("ctx: %w", err)); got != want {
			t.Errorf("userMessage(%v) = %q", err, got)
		}
	}
}

func TestRowLayout(t *testing.T) {
	tests := []struct {
		name  string
		en    entry
		width int
		want  string
	}{
		{"track", entry{kind: entryTrack, track: tLucky}, 70,
			"  Get Lucky (feat. Pharre…  Daft Punk, Phar…  Random Acces…       6:09"},
		{"narrow track", entry{kind: entryTrack, track: tOneMore}, 30, "  One More Time    Daft Punk  "},
		{"long track", entry{kind: entryTrack, track: tidal.Track{Title: "Long", Duration: 3725 * time.Second}}, 50,
			"  Long                                     1:02:05"},
		{"album", entry{kind: entryAlbum, album: aDisc}, 50, "  Discovery            Daft Punk      2001        "},
		{"artist", entry{kind: entryArtist, artist: arDaft}, 50, "  Daft Punk"},
		{"playlist", entry{kind: entryPlaylist, playlist: tidal.Playlist{Name: "Mine", Tracks: 1}}, 50,
			"  Mine                                1 track     "},
		{"heading", headingEntry("Tracks", 0), 50, "TRACKS"},
		{"unknown", entry{kind: 99}, 50, ""},
		{"control chars", entry{kind: entryArtist, artist: tidal.Artist{Name: "a\x1b[31mb"}}, 50, "  a\x1b[31mb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowText(tt.en, tt.width, false)
			if got != tt.want {
				t.Errorf("rowText() = %q, want %q", got, tt.want)
			}
		})
	}
	// Columns never exceed the width, even when it is tiny.
	for w := 0; w < 12; w++ {
		if got := ansi.StringWidth(columns(w, cell{text: "abc", share: 1}, cell{text: "9:99", fixed: 7, right: true})); got > max(w, 0) {
			t.Errorf("columns(%d) width %d", w, got)
		}
	}
	// Rows go through List.SetRows, which strips escapes.
	var l entryList
	l.set([]entry{{kind: entryArtist, artist: tidal.Artist{Name: "a\x1b[31mb"}}})
	if out := l.render(10, 1, false); strings.Contains(out, "\x1b[31m") {
		t.Errorf("escape reached the screen: %q", out)
	}
	if clock(0) != "" || clock(59*time.Second) != "0:59" || countTracks(0) != "" || countTracks(2) != "2 tracks" {
		t.Error("clock/countTracks")
	}
}

func TestCollectStopsAtTheLimit(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, next string) (tidal.Page[tidal.Track], error) {
		calls++
		return tidal.Page[tidal.Track]{Items: []tidal.Track{tOneMore, tHarder}, Next: "more"}, nil
	}
	got, capped, err := collect(context.Background(), fetch, 4)
	if err != nil || len(got) != 4 || !capped || calls != 2 {
		t.Errorf("collect = %d tracks, capped %v, calls %d, err %v", len(got), capped, calls, err)
	}
	// Exactly at the limit on the last page is not capped.
	last := func(_ context.Context, _ string) (tidal.Page[tidal.Track], error) {
		return tidal.Page[tidal.Track]{Items: []tidal.Track{tOneMore, tHarder}}, nil
	}
	if _, capped, _ := collect(context.Background(), last, 2); capped {
		t.Error("capped at an exact fit")
	}
}

func TestServicesDefaults(t *testing.T) {
	s := &Services{}
	if s.ctx() == nil || s.logger() == nil || s.tick(time.Millisecond, func(time.Time) tea.Msg { return nil }) == nil {
		t.Error("defaults")
	}
	if s.playCmd([]tidal.Track{tOneMore}, 0) != nil || s.queueCmd([]tidal.Track{tOneMore}, false, "x") != nil {
		t.Error("commands without playback")
	}
	s.Playback = &fakePlayback{}
	if s.playCmd(nil, 0) != nil || s.playCmd([]tidal.Track{tOneMore}, 1) != nil || s.queueCmd(nil, true, "x") != nil {
		t.Error("commands with bad input")
	}
}

func TestFailedPageIsNotRetried(t *testing.T) {
	e := newEnv(t)
	v := NewPlaylistView(e.svc, pEss)
	v.Update(pageMsg[tidal.Track]{page: tidal.Page[tidal.Track]{Items: []tidal.Track{tOneMore}, Next: "p1"}})
	if !v.tracks.loading {
		t.Fatal("near the end of the list the next page was not requested")
	}
	if cmd := v.Update(press("j")); cmd != nil {
		t.Error("a key press started a second request while one was loading")
	}
	if cmd := v.Update(pageMsg[tidal.Track]{err: tidal.ErrRateLimited}); cmd != nil {
		t.Error("the error answer requested the page again")
	}
	if cmd := v.Update(press("j")); cmd != nil {
		t.Error("a key press requested the failed page again")
	}
}

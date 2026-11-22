package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/player"
	"github.com/five07/tidal-tui/internal/queue"
	"github.com/five07/tidal-tui/internal/tidal"
	"github.com/five07/tidal-tui/internal/tui"
)

// stubTransport counts player calls.
type stubTransport struct {
	plays, changes, pauses, nexts, prevs int
	err                                  error
}

func (s *stubTransport) Play(context.Context) error         { s.plays++; return s.err }
func (s *stubTransport) QueueChanged(context.Context) error { s.changes++; return s.err }
func (s *stubTransport) TogglePause(context.Context) error  { s.pauses++; return s.err }
func (s *stubTransport) Next(context.Context) error         { s.nexts++; return s.err }
func (s *stubTransport) Previous(context.Context) error     { s.prevs++; return s.err }

func tt(id string) tidal.Track {
	return tidal.Track{ID: id, Title: "T" + id, Version: "Live", Artists: []string{"A", "B"}, Album: "Al", Duration: time.Minute}
}

func queued(q *queue.Queue[player.Track]) []string {
	s := q.Snapshot()
	var out []string
	if s.Current != nil {
		out = append(out, "*"+s.Current.Item.ID)
	}
	for _, e := range s.Upcoming {
		out = append(out, e.Item.ID)
	}
	return out
}

func TestPlayback(t *testing.T) {
	tests := []struct {
		name        string
		prefill     []string // queue before the call, first is current
		call        func(b *playback) error
		want        []string
		plays, chgs int
		wantErr     bool
	}{
		{name: "play from replaces the queue", prefill: []string{"x", "y"},
			call: func(b *playback) error {
				return b.PlayFrom(context.Background(), []tidal.Track{tt("1"), tt("2"), tt("3")}, 1)
			},
			want: []string{"*2", "3"}, plays: 1},
		{name: "play from out of range", prefill: []string{"x"},
			call:    func(b *playback) error { return b.PlayFrom(context.Background(), []tidal.Track{tt("1")}, 3) },
			wantErr: true},
		{name: "add appends", prefill: []string{"x", "y"},
			call: func(b *playback) error { return b.Add(context.Background(), []tidal.Track{tt("1"), tt("2")}) },
			want: []string{"*x", "y", "1", "2"}, chgs: 1},
		{name: "add to an empty queue starts playing",
			call: func(b *playback) error { return b.Add(context.Background(), []tidal.Track{tt("1"), tt("2")}) },
			want: []string{"*1", "2"}, plays: 1},
		{name: "add nothing to an empty queue",
			call: func(b *playback) error { return b.Add(context.Background(), nil) },
			chgs: 1},
		{name: "play next keeps order after the current track", prefill: []string{"x", "y"},
			call: func(b *playback) error { return b.PlayNext(context.Background(), []tidal.Track{tt("1"), tt("2")}) },
			want: []string{"*x", "1", "2", "y"}, chgs: 1},
		{name: "play next on an empty queue starts playing in order",
			call: func(b *playback) error { return b.PlayNext(context.Background(), []tidal.Track{tt("1"), tt("2")}) },
			want: []string{"*1", "2"}, plays: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := queue.New[player.Track]()
			var pre []player.Track
			for _, id := range tc.prefill {
				pre = append(pre, player.Track{ID: id})
			}
			if len(pre) > 0 {
				q.Set(pre, 0)
			}
			tr := &stubTransport{}
			err := tc.call(&playback{q: q, p: tr})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tc.wantErr {
				return
			}
			if got := queued(q); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("queue = %v, want %v", got, tc.want)
			}
			if tr.plays != tc.plays || tr.changes != tc.chgs {
				t.Errorf("plays %d changes %d, want %d %d", tr.plays, tr.changes, tc.plays, tc.chgs)
			}
		})
	}
	want := player.Track{ID: "1", Title: "T1 (Live)", Artists: "A, B", Album: "Al", Duration: time.Minute}
	if got := toTrack(tt("1")); got != want {
		t.Errorf("toTrack = %+v", got)
	}
}

// stubStatus is a status source with scripted messages. Each update hands
// the watcher exactly one value, so the test does not race the watcher.
type stubStatus struct {
	mu      sync.Mutex
	updates chan struct{}
	pending []string
}

func (s *stubStatus) Updates() <-chan struct{} { return s.updates }
func (s *stubStatus) Status() player.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := s.pending[0]
	s.pending = s.pending[1:]
	return player.Status{Message: msg}
}
func (s *stubStatus) set(msg string) {
	s.mu.Lock()
	s.pending = append(s.pending, msg)
	s.mu.Unlock()
	s.updates <- struct{}{}
}

func TestWatchStatus(t *testing.T) {
	src := &stubStatus{updates: make(chan struct{})}
	out := make(chan string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchStatus(ctx, src, watch{notices: out}); close(done) }()

	for _, m := range []string{"mpv is not installed.", "mpv is not installed.", "", "mpv is not installed.", "busy"} {
		src.set(m)
	}
	src.set("busy") // unchanged: not sent again
	cancel()
	<-done
	close(out)
	var got []string
	for m := range out {
		got = append(got, m)
	}
	if want := []string{"mpv is not installed.", "mpv is not installed.", "busy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}

	// A full channel does not block shutdown.
	src = &stubStatus{updates: make(chan struct{}, 1)}
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { watchStatus(ctx, src, watch{notices: make(chan string)}); close(done) }()
	src.set("stuck")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchStatus did not stop")
	}
}

// exec runs cmd and returns its messages, expanding batches. Commands that
// do not finish within wait (timers) are skipped.
func exec(cmd tea.Cmd, wait time.Duration) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, exec(c, wait)...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(wait):
		return nil
	}
}

func screen(m tea.Model) string { return ansi.Strip(m.View().Content) }

// TestRunSearchEndToEnd drives the real UI from run against the fake TIDAL
// server: search, results on screen, Enter plays, and the player's error
// (the fake serves no manifests) comes back as a notice.
func TestRunSearchEndToEnd(t *testing.T) {
	getenv, _, _ := testEnv(t, map[string]string{"TIDAL_CLIENT_ID": "id", "LANG": "nb_NO.UTF-8"})
	f := newFakeTIDAL(t)
	var searchQuery, searchCountry string
	var mixLocale string
	f.mux.HandleFunc("/v2/userDailyMixes/me/relationships/items", func(w http.ResponseWriter, r *http.Request) {
		mixLocale = r.URL.Query().Get("locale")
		w.Header().Set("Content-Type", "application/vnd.api+json")
		io.WriteString(w, `{"data":[{"id":"00237b2a1d39536f0ef81e836a9176","type":"playlists"}],
			"included":[{"id":"00237b2a1d39536f0ef81e836a9176","type":"playlists","attributes":{"name":"Min miks 1","description":"Morcheeba og flere","playlistType":"MIX"}}]}`)
	})
	for _, k := range []string{"userDiscoveryMixes", "userNewReleaseMixes"} {
		f.mux.HandleFunc("/v2/"+k+"/me/relationships/items", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			io.WriteString(w, `{"data":[]}`)
		})
	}
	f.mux.HandleFunc("/v2/searchResults", func(w http.ResponseWriter, r *http.Request) {
		searchQuery, searchCountry = r.URL.Query().Get("filter[query]"), r.URL.Query().Get("countryCode")
		w.Header().Set("Content-Type", "application/vnd.api+json")
		io.WriteString(w, `{"data":[{"id":"s","type":"searchResults","relationships":{
			"tracks":{"data":[{"id":"1550546","type":"tracks"}]}}}],
			"included":[{"id":"1550546","type":"tracks","attributes":{"title":"One More Time","duration":"PT5M20S"},
			 "relationships":{"artists":{"data":[{"id":"8847","type":"artists"}]}}},
			 {"id":"8847","type":"artists","attributes":{"name":"Daft Punk"}}]}`)
	})
	var favMethod, favBody string
	f.mux.HandleFunc("/v2/userCollectionTracks/me/relationships/items", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		favMethod, favBody = r.Method, string(b)
	})
	f.mux.HandleFunc("/v2/userCollectionPlaylists/me/relationships/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		io.WriteString(w, `{"data":[{"id":"6f76-cfa5","type":"playlists"}],
			"included":[{"id":"6f76-cfa5","type":"playlists","attributes":{"name":"Road trip","numberOfTrackItems":12}}]}`)
	})
	d := f.deps()
	var engineStarts int
	d.newEngine = func(context.Context, player.MPVConfig) (player.Engine, error) {
		engineStarts++
		return nil, player.ErrMPVNotFound
	}
	var uiErr error
	d.runUI = func(ctx context.Context, m tea.Model) error {
		feed := func(msgs ...tea.Msg) {
			for len(msgs) > 0 {
				var cmd tea.Cmd
				m, cmd = m.Update(msgs[0])
				msgs = append(msgs[1:], exec(cmd, 50*time.Millisecond)...)
			}
		}
		key := func(s string) tea.KeyPressMsg {
			if s == "enter" {
				return tea.KeyPressMsg{Code: tea.KeyEnter}
			}
			return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
		}
		notices := m.Init() // waits for the first player message
		feed(tea.WindowSizeMsg{Width: 100, Height: 30}, key("/"))
		for _, r := range "daft" {
			feed(key(string(r)))
		}
		feed(key("enter"))
		if out := screen(m); !strings.Contains(out, "One More Time") || !strings.Contains(out, "Daft Punk") {
			uiErr = errors.New("results missing:\n" + out)
			return nil
		}
		feed(key("f"))
		if out := screen(m); !strings.Contains(out, "Added to favorites: One More Time") {
			uiErr = errors.New("favorite notice missing:\n" + out)
			return nil
		}
		feed(key("enter"))
		if out := screen(m); !strings.Contains(out, "Playing One More Time") {
			uiErr = errors.New("play notice missing:\n" + out)
			return nil
		}
		feed(exec(notices, 5*time.Second)...)
		if out := screen(m); !strings.Contains(out, "not available on TIDAL") && !strings.Contains(out, "mpv is not installed") {
			uiErr = errors.New("player message missing:\n" + out)
			return nil
		}
		feed(key("2"))
		if out := screen(m); !strings.Contains(out, "Road trip") {
			uiErr = errors.New("library playlists missing:\n" + out)
			return nil
		}
		feed(key("3"))
		if out := screen(m); !strings.Contains(out, "Min miks 1") || !strings.Contains(out, "Morcheeba og flere") || strings.Contains(out, "New Releases") {
			uiErr = errors.New("discovery mixes missing:\n" + out)
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), d, nil, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr %s", code, stderr.String())
	}
	if uiErr != nil {
		t.Fatal(uiErr)
	}
	if mixLocale != "nb-NO" {
		t.Errorf("mix locale = %q, want nb-NO from LANG", mixLocale)
	}
	if favMethod != http.MethodPost || favBody != `{"data":[{"id":"1550546","type":"tracks"}]}` {
		t.Errorf("favorite request = %s %s", favMethod, favBody)
	}
	if searchQuery != "daft" || searchCountry != "US" {
		t.Errorf("search query %q country %q", searchQuery, searchCountry)
	}
}

func TestNewUIErrors(t *testing.T) {
	c, err := tidal.New(stubTokenSource{})
	if err != nil {
		t.Fatal(err)
	}
	d := defaultDeps
	if _, _, err := newUI(context.Background(), d, testConfig(), c, tidal.User{Country: "us"}, "", discardLogger()); err == nil {
		t.Error("invalid country accepted")
	}
	cfg := testConfig()
	cfg.Quality = "loud"
	if _, _, err := newUI(context.Background(), d, cfg, c, tidal.User{Country: "US"}, "", discardLogger()); err == nil {
		t.Error("invalid quality accepted")
	}
	// The player starts the configured engine with the audio settings.
	cfg = testConfig()
	cfg.Audio.Device = "pipewire/dac"
	var got player.MPVConfig
	d.newEngine = func(_ context.Context, c player.MPVConfig) (player.Engine, error) {
		got = c
		return nil, player.ErrMPVNotFound
	}
	pc := playerConfig(d, cfg, nil, queue.New[player.Track](), discardLogger())
	if _, err := pc.NewEngine(context.Background()); !errors.Is(err, player.ErrMPVNotFound) || got.Mode != cfg.Audio.Mode || got.Device != "pipewire/dac" || got.Logger == nil {
		t.Errorf("engine config = %+v, err %v", got, err)
	}
	// The default engine reports a missing mpv as a typed error.
	d = defaultDeps
	_, err = d.newEngine(context.Background(), player.MPVConfig{Binary: "tidal-tui-no-such-mpv"})
	if !errors.Is(err, player.ErrMPVNotFound) {
		t.Errorf("newEngine err = %v", err)
	}
}

type stubTokenSource struct{}

func (stubTokenSource) Token(context.Context) (string, error)   { return "t", nil }
func (stubTokenSource) Refresh(context.Context) (string, error) { return "t", nil }

func testConfig() config.Config { return config.Defaults(func(string) string { return "" }) }

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRunFailsOnBadAccountCountry(t *testing.T) {
	getenv, _, _ := testEnv(t, map[string]string{"TIDAL_CLIENT_ID": "id"})
	f := newFakeTIDAL(t)
	f.meCountry = "??"
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), f.deps(), nil, getenv, &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stderr.String(), "invalid country code") || f.uiRuns.Load() != 0 {
		t.Errorf("stderr %q, ui runs %d", stderr.String(), f.uiRuns.Load())
	}
}

func TestLocaleFromEnv(t *testing.T) {
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LANG": "en_US.UTF-8"}, "en-US"},
		{map[string]string{"LANG": "de_DE@euro"}, "de-DE"},
		{map[string]string{"LANG": "fr_FR.UTF-8", "LC_MESSAGES": "nb_NO.UTF-8"}, "nb-NO"},
		{map[string]string{"LANG": "fr_FR", "LC_MESSAGES": "nb_NO", "LC_ALL": "pt_BR.UTF-8"}, "pt-BR"},
		{map[string]string{"LANG": "C.UTF-8"}, ""},
		{map[string]string{"LC_ALL": "POSIX"}, ""},
		{map[string]string{}, ""},
	}
	for _, tt := range tests {
		if got := localeFromEnv(func(k string) string { return tt.env[k] }); got != tt.want {
			t.Errorf("localeFromEnv(%v) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

// A locale TIDAL would reject is logged and left out, not fatal.
func TestNewUIIgnoresBadLocale(t *testing.T) {
	f := newFakeTIDAL(t)
	d := f.deps()
	c, err := tidal.New(stubTokenSource{}, append([]tidal.Option{tidal.WithLogger(discardLogger())}, d.apiOptions...)...)
	if err != nil {
		t.Fatal(err)
	}
	_, stop, err := newUI(context.Background(), d, testConfig(), c, tidal.User{Country: "US"}, "english", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	stop()
}

func TestTransportMethods(t *testing.T) {
	ctx := context.Background()
	q := queue.New[player.Track]()
	p := &stubTransport{}
	b := &playback{q: q, p: p, changed: make(chan struct{}, 1)}
	pending := func() bool {
		select {
		case <-b.changed:
			return true
		default:
			return false
		}
	}
	if err := b.PlayFrom(ctx, []tidal.Track{tt("1"), tt("2"), tt("3")}, 0); err != nil || !pending() {
		t.Fatalf("PlayFrom: %v", err)
	}
	b.TogglePause(ctx)
	b.Next(ctx)
	b.Previous(ctx)
	if p.pauses != 1 || p.nexts != 1 || p.prevs != 1 || pending() {
		t.Errorf("player calls %+v (transport keys must not signal a queue edit)", p)
	}
	tests := []struct {
		name  string
		do    func() error
		check func() bool
	}{
		{"shuffle", func() error { return b.SetShuffle(ctx, true) }, q.Shuffle},
		{"unshuffle", func() error { return b.SetShuffle(ctx, false) }, func() bool { return !q.Shuffle() }},
		{"repeat", func() error { return b.CycleRepeat(ctx) }, func() bool { return q.Repeat() == queue.RepeatAll }},
		{"remove", func() error { return b.Remove(ctx, uint64(q.Snapshot().Upcoming[0].Handle)) },
			func() bool { return reflect.DeepEqual(queued(q), []string{"*1", "3"}) }},
		{"jump", func() error { return b.Jump(ctx, uint64(q.Snapshot().Upcoming[0].Handle)) },
			func() bool { return reflect.DeepEqual(queued(q), []string{"*3"}) }},
		{"clear", func() error { b.Add(ctx, []tidal.Track{tt("4")}); pending(); return b.Clear(ctx) },
			func() bool { return reflect.DeepEqual(queued(q), []string{"*3"}) }},
	}
	for _, tc := range tests {
		if err := tc.do(); err != nil || !tc.check() || !pending() {
			t.Errorf("%s: err %v, queue %v", tc.name, err, queued(q))
		}
	}
	if p.plays != 2 || p.changes != 6 { // clear's Add counts too
		t.Errorf("plays %d changes %d", p.plays, p.changes)
	}
	// A handle that is gone fails without touching the player.
	if err := b.Jump(ctx, 999); !errors.Is(err, errNotQueued) {
		t.Errorf("jump to a gone entry: %v", err)
	}
	if err := b.Remove(ctx, 999); !errors.Is(err, errNotQueued) || p.plays != 2 || p.changes != 6 {
		t.Errorf("remove a gone entry: %v", err)
	}
	// Without a listener, edits never block.
	(&playback{q: q, p: p}).Clear(ctx)
}

func TestNowPlaying(t *testing.T) {
	q := queue.New[player.Track]()
	q.Set([]player.Track{{ID: "1", Title: "One", Artists: "A", Album: "Al", Duration: time.Minute}, {ID: "2"}}, 0)
	q.CycleRepeat()
	snap := q.Snapshot()
	tests := []struct {
		name string
		st   player.Status
		want tui.NowPlaying
	}{
		{"loading, not assessed", player.Status{State: player.Loading, Duration: time.Minute,
			Source: &player.Source{Codec: "FLAC", BitDepth: 24, SampleRate: 96000}},
			tui.NowPlaying{State: tui.Loading, Duration: time.Minute, Source: "FLAC 24/96"}},
		{"bit-perfect", player.Status{State: player.Playing, Position: time.Second,
			Source: &player.Source{Codec: "FLAC", BitDepth: 16, SampleRate: 44100}, Output: &player.HWParams{Format: "S16_LE", Rate: 44100},
			Indicator: player.Assessment{State: player.BitPerfect, Reason: "exclusive output at the source format"}},
			tui.NowPlaying{State: tui.Playing, Position: time.Second, Source: "FLAC 16/44.1", Output: "16/44.1",
				Quality: tui.QualityBitPerfect, Reason: "exclusive output at the source format"}},
		{"matched, float output", player.Status{State: player.Paused, Preview: true,
			Source: &player.Source{Codec: "AAC", SampleRate: 44100}, Output: &player.HWParams{Format: "FLOAT_LE", Rate: 48000},
			Indicator: player.Assessment{State: player.RateMatched, Reason: "lossy source"}},
			tui.NowPlaying{State: tui.Paused, Preview: true, Source: "AAC 44.1 kHz", Output: "48 kHz", Quality: tui.QualityMatched, Reason: "lossy source"}},
		{"altered", player.Status{Source: &player.Source{Codec: "FLAC"}, Output: &player.HWParams{},
			Indicator: player.Assessment{State: player.Altered, Reason: "output format unknown"}},
			tui.NowPlaying{Source: "FLAC 0 kHz", Quality: tui.QualityAltered, Reason: "output format unknown"}},
		{"reason without a source", player.Status{Indicator: player.Assessment{State: player.BitPerfect, Reason: "x"}}, tui.NowPlaying{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nowPlaying(tc.st, snap, config.AudioExclusive)
			want := tc.want
			want.Mode, want.Repeat = "exclusive", tui.RepeatAll
			want.Current = &tui.QueueItem{Handle: uint64(snap.Current.Handle), Track: tui.QueueTrack{ID: "1", Title: "One", Artists: "A", Album: "Al", Duration: time.Minute}}
			want.Upcoming = []tui.QueueItem{{Handle: uint64(snap.Upcoming[0].Handle), Track: tui.QueueTrack{ID: "2"}}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
	if np := nowPlaying(player.Status{}, queue.Snapshot[player.Track]{Shuffle: true}, config.AudioShared); np.Current != nil || !np.Shuffle || np.Mode != "shared" {
		t.Errorf("empty queue: %+v", np)
	}
}

func TestWatchStatusSnapshots(t *testing.T) {
	src := &stubStatus{updates: make(chan struct{})}
	q := queue.New[player.Track]()
	status := make(chan tui.NowPlaying, 1)
	changed := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchStatus(ctx, src, watch{notices: make(chan string, 8), status: status, q: q, changed: changed, mode: config.AudioShared})
		close(done)
	}()
	src.set("") // a player change
	q.Set([]player.Track{{ID: "7"}}, 0)
	src.mu.Lock()
	src.pending = append(src.pending, "")
	src.mu.Unlock()
	changed <- struct{}{} // a queue edit: the newer snapshot replaces the untaken one
	src.set("")
	cancel()
	<-done
	np := <-status
	if np.Current == nil || np.Current.Track.ID != "7" || np.Mode != "shared" {
		t.Errorf("latest snapshot = %+v", np)
	}
	select {
	case np := <-status:
		t.Errorf("a second snapshot was queued: %+v", np)
	default:
	}
}

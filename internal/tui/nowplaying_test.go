package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// npTrack is the current track of the now-playing fixtures.
var npTrack = QueueItem{Handle: 1, Track: QueueTrack{ID: "1", Title: "One More Time", Artists: "Daft Punk", Album: "Discovery", Duration: 320 * time.Second}}

func playingNow() NowPlaying {
	return NowPlaying{
		State: Playing, Current: &npTrack, Position: 102 * time.Second, Duration: 320 * time.Second,
		Source: "FLAC 24/96", Output: "24/96", Mode: "exclusive", Quality: QualityBitPerfect, Reason: "exclusive output at the source format",
		Shuffle: true, Repeat: RepeatAll,
		Upcoming: []QueueItem{
			{Handle: 2, Track: QueueTrack{ID: "2", Title: "Aerodynamic", Artists: "Daft Punk", Album: "Discovery", Duration: 212 * time.Second}},
			{Handle: 3, Track: QueueTrack{ID: "3", Title: "Digital Love", Artists: "Daft Punk", Album: "Discovery", Duration: 301 * time.Second}},
		},
	}
}

// barText returns the bar rows of m, without colors or the frame.
func barText(m *Model) []string {
	rows := strings.Split(ansi.Strip(m.render()), "\n")
	l := m.layout
	out := rows[l.Bar.Y : l.Bar.Y+BarHeight]
	for i, r := range out {
		out[i] = strings.TrimSpace(strings.Trim(r, "│"))
	}
	return out
}

func TestNowPlayingGoldens(t *testing.T) {
	long := npTrack
	long.Track.Title = "Harder, Better, Faster, Stronger (Alive 2007 Live Version, Extended 2026 Remaster)"
	tests := []struct {
		name string
		w, h int
		np   func() NowPlaying
	}{
		{"nowplaying_80x24", 80, 24, playingNow},
		{"nowplaying_paused_50x16", 50, 16, func() NowPlaying {
			np := playingNow()
			np.State, np.Shuffle, np.Repeat = Paused, false, RepeatOff
			np.Mode, np.Output, np.Quality, np.Reason = "shared", "48", QualityAltered, "resampled to 48 kHz (source 96 kHz)"
			return np
		}},
		{"nowplaying_long_title_80x24", 80, 24, func() NowPlaying {
			np := playingNow()
			np.Current = &long
			return np
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			m := e.model(tt.w, tt.h)
			at := time.Unix(1000, 0)
			m.now = func() time.Time { return at } // the marquee position
			drive(m, NowPlayingMsg{tt.np()})
			at = at.Add(5 * MarqueeStep)
			golden(t, tt.name, m.render())
		})
	}
}

func TestBarLines(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	if got := barText(m); got[barArtist] != "Nothing playing" {
		t.Fatalf("idle bar = %q", got)
	}
	drive(m, NowPlayingMsg{playingNow()})
	got := barText(m)
	want := []string{
		"One More Time",
		"",
		"Daft Punk · Discovery",
		"FLAC 24/96 → out 24/96 · exclusive · ✓ bit-perfect",
		"1:42 ━━━━━━━━━━━━━━━━━━●───────────────────────────────────────── 5:20",
		"⇄ on    [⏮]  [⏸]  [⏭]    ↻ all    ∞ off",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
	// Every line is centered (AC #7).
	rows := strings.Split(ansi.Strip(m.render()), "\n")[m.layout.Bar.Y : m.layout.Bar.Y+BarHeight]
	for i, r := range rows {
		r = strings.Trim(r, "│")
		if strings.TrimSpace(r) == "" {
			continue
		}
		l, rt := len(r)-len(strings.TrimLeft(r, " ")), len(r)-len(strings.TrimRight(r, " "))
		if d := l - rt; d < -1 || d > 1 {
			t.Errorf("row %d is off center by %d: %q", i, d, r)
		}
	}
}

func TestFormatLine(t *testing.T) {
	tests := []struct {
		name string
		np   NowPlaying
		want string
	}{
		{"bit-perfect", NowPlaying{Source: "FLAC 24/192", Output: "24/192", Mode: "exclusive", Quality: QualityBitPerfect}, "FLAC 24/192 → out 24/192 · exclusive · ✓ bit-perfect"},
		{"matched", NowPlaying{Source: "FLAC 16/44.1", Output: "16/44.1", Mode: "shared", Quality: QualityMatched}, "FLAC 16/44.1 → out 16/44.1 · shared · ✓ rate matched"},
		{"differs", NowPlaying{Source: "FLAC 24/96", Output: "48 kHz", Mode: "shared", Quality: QualityAltered, Reason: "resampled to 48 kHz"},
			"FLAC 24/96 → out 48 kHz · shared · ⚠ resampled to 48 kHz"},
		{"not assessed yet", NowPlaying{Source: "AAC 44.1 kHz", Mode: "shared"}, "AAC 44.1 kHz · shared"},
		{"preview", NowPlaying{Preview: true, Mode: "shared"}, "PREVIEW · shared"},
		{"nothing known", NowPlaying{Mode: "shared"}, ""},
		{"escape in reason", NowPlaying{Source: "FLAC", Quality: QualityAltered, Reason: "a\x1b[31mb"}, "FLAC · ⚠ ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := formatLine(tt.np, 70)
			if ansi.StringWidth(line) != 70 {
				t.Errorf("width %d", ansi.StringWidth(line))
			}
			if got := strings.TrimSpace(ansi.Strip(line)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	// Too narrow: plain, truncated, still the full width.
	if got := ansi.Strip(formatLine(tests[2].np, 20)); ansi.StringWidth(got) != 20 || !strings.HasSuffix(strings.TrimSpace(got), "…") {
		t.Errorf("narrow = %q", got)
	}
}

func TestProgressLine(t *testing.T) {
	np := NowPlaying{State: Playing, Current: &npTrack}
	tests := []struct {
		name string
		mod  func(*NowPlaying)
		w    int
		want string
	}{
		{"start, duration from the track", func(*NowPlaying) {}, 30, "0:00 ●─────────── 5:20"},
		{"half", func(n *NowPlaying) { n.Position, n.Duration = 60*time.Second, 120*time.Second }, 30, "1:00 ━━━━━●────── 2:00"},
		{"past the end", func(n *NowPlaying) { n.Position, n.Duration = 200*time.Second, 120*time.Second }, 30, "2:00 ━━━━━━━━━━━● 2:00"},
		{"no duration", func(n *NowPlaying) { n.Current = &QueueItem{}; n.Position = 5 * time.Second }, 30, "0:00 ●─────────── 0:00"},
		{"narrow", func(*NowPlaying) {}, 18, "0:00 / 5:20"},
		{"loading", func(n *NowPlaying) { n.State = Loading }, 30, "Loading…"},
		{"stopped", func(n *NowPlaying) { n.State = Stopped }, 30, "Stopped"},
		{"long track", func(n *NowPlaying) { n.Position, n.Duration = 3725*time.Second, 7200*time.Second }, 40, "1:02:05 ━━━━━━━●──────── 2:00:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := np
			tt.mod(&n)
			line := progressLine(n, tt.w)
			if got := strings.TrimSpace(ansi.Strip(line)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if ansi.StringWidth(line) != tt.w {
				t.Errorf("width %d", ansi.StringWidth(line))
			}
		})
	}
}

func TestControlsLine(t *testing.T) {
	tests := []struct {
		np   NowPlaying
		want string
	}{
		{NowPlaying{State: Paused}, "⇄ off    [⏮]  [▶]  [⏭]    ↻ off    ∞ off"},
		{NowPlaying{State: Loading, Shuffle: true, Repeat: RepeatOne}, "⇄ on    [⏮]  [⏸]  [⏭]    ↻ one    ∞ off"},
		{NowPlaying{State: Stopped, Repeat: 7, Autoplay: true}, "⇄ off    [⏮]  [▶]  [⏭]    ↻ one    ∞ on"},
	}
	for _, tt := range tests {
		line, zones := controlsLine(tt.np, 60)
		plain := ansi.Strip(line)
		if strings.TrimSpace(plain) != tt.want || ansi.StringWidth(line) != 60 || len(zones) != 6 {
			t.Errorf("got %q (%d zones)", plain, len(zones))
		}
		// Each zone covers its control's text.
		for _, z := range zones {
			if s := ansi.Cut(plain, z.x0, z.x1); strings.TrimSpace(s) != s || s == "" {
				t.Errorf("zone %+v covers %q", z, s)
			}
		}
	}
	// On, autoplay is highlighted like shuffle and repeat.
	if on, _ := controlsLine(NowPlaying{Autoplay: true}, 60); !strings.Contains(on, styleNotice.Render("∞ on")) {
		t.Errorf("autoplay on not highlighted: %q", on)
	}
	if line, zones := controlsLine(NowPlaying{}, 20); zones != nil || ansi.StringWidth(line) != 20 {
		t.Error("controls on a narrow bar")
	}
}

func TestMarquee(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	long := npTrack
	long.Handle = 9
	long.Track.Title = strings.Repeat("Long title ", 10)
	np := playingNow()
	np.Current = &long
	drive(m, NowPlayingMsg{np})
	first := barText(m)[barTitle]
	if !m.marqueeOn || !strings.HasPrefix(first, "Long title") || ansi.StringWidth(first) != m.layout.Bar.W {
		t.Fatalf("marquee not started: %q", first)
	}
	clock = clock.Add(3 * MarqueeStep)
	if got := barText(m)[barTitle]; !strings.HasPrefix(got, "g title") {
		t.Errorf("after 3 steps: %q", got)
	}
	// A tick continues the chain while the title scrolls; a stale one is
	// dropped.
	if m.Update(marqueeMsg{m.marqueeGen}); !m.marqueeOn {
		t.Error("chain stopped")
	}
	if _, cmd := m.Update(marqueeMsg{m.marqueeGen - 1}); cmd != nil {
		t.Error("a stale tick rescheduled")
	}
	// The same track again keeps the scroll position and the one chain.
	gen := m.marqueeGen
	drive(m, NowPlayingMsg{np})
	if m.marqueeGen != gen || !strings.HasPrefix(barText(m)[barTitle], "g title") {
		t.Error("an update restarted the marquee")
	}
	// A new track with a short title stops it at the next tick, and
	// starts from its beginning.
	clock = clock.Add(time.Second)
	drive(m, NowPlayingMsg{playingNow()})
	if m.marqueeFrom != clock {
		t.Error("new track did not reset the scroll")
	}
	if m.Update(marqueeMsg{m.marqueeGen}); m.marqueeOn {
		t.Error("chain kept running for a short title")
	}
	if m.marquee("abc", 0) != "abc" {
		t.Error("zero width")
	}
	// Wide characters scroll by cells and keep the width.
	wide := strings.Repeat("日本語", 20)
	for step := range 4 {
		m.marqueeFrom = clock.Add(-time.Duration(step) * MarqueeStep)
		if got := m.marquee(wide, 15); ansi.StringWidth(got) != 15 {
			t.Errorf("step %d: width %d %q", step, ansi.StringWidth(got), got)
		}
	}
}

func TestStatusChannel(t *testing.T) {
	ch := make(chan NowPlaying, 1)
	m := New(Config{Status: ch})
	if m.Init() == nil {
		t.Fatal("no listener")
	}
	ch <- playingNow()
	if msg, ok := m.listenStatus()().(NowPlayingMsg); !ok || msg.NowPlaying.Current.Track.ID != "1" {
		t.Errorf("got %+v", msg)
	}
	close(ch)
	if m.listenStatus()() != nil {
		t.Error("closed channel")
	}
	if New(Config{}).listenStatus() != nil {
		t.Error("listener without a channel")
	}
	// A snapshot reaches the Services and every watching view.
	w := &npWatcher{}
	m = New(Config{Queue: w})
	m.Update(NowPlayingMsg{playingNow()})
	if !m.svc.playing.playing("1") || m.svc.playing.playing("") || (NowPlaying{Current: &QueueItem{}}).playing("") || w.got.Current == nil {
		t.Error("snapshot not delivered")
	}
}

type npWatcher struct {
	Placeholder
	got NowPlaying
}

func (w *npWatcher) NowPlayingChanged(np NowPlaying) tea.Cmd { w.got = np; return nil }

// stubTransport records transport calls.
type stubTransport struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (s *stubTransport) rec(c string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, c)
	return s.err
}
func (s *stubTransport) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}
func (s *stubTransport) TogglePause(context.Context) error { return s.rec("pause") }
func (s *stubTransport) Next(context.Context) error        { return s.rec("next") }
func (s *stubTransport) Previous(context.Context) error    { return s.rec("prev") }
func (s *stubTransport) SetShuffle(_ context.Context, on bool) error {
	return s.rec(fmt.Sprint("shuffle ", on))
}
func (s *stubTransport) CycleRepeat(context.Context) error { return s.rec("repeat") }
func (s *stubTransport) SetAutoplay(_ context.Context, on bool) error {
	return s.rec(fmt.Sprint("autoplay ", on))
}
func (s *stubTransport) Jump(_ context.Context, h uint64) error {
	return s.rec(fmt.Sprint("jump ", h))
}
func (s *stubTransport) Remove(_ context.Context, h uint64) error {
	return s.rec(fmt.Sprint("remove ", h))
}
func (s *stubTransport) Clear(context.Context) error { return s.rec("clear") }

func transportEnv(t *testing.T) (*env, *Model, *stubTransport) {
	t.Helper()
	e := newEnv(t)
	tr := &stubTransport{}
	e.svc.Transport = tr
	m := e.model(80, 24)
	drive(m, NowPlayingMsg{playingNow()})
	return e, m, tr
}

func TestTransportKeys(t *testing.T) {
	tests := []struct {
		keys []tea.Msg
		want []string
	}{
		{[]tea.Msg{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}}, []string{"pause"}},
		{[]tea.Msg{press("n"), press("]")}, []string{"next", "next"}},
		{[]tea.Msg{press("p"), press("[")}, []string{"prev", "prev"}},
		{[]tea.Msg{press("s")}, []string{"shuffle false"}}, // the fixture has shuffle on
		{[]tea.Msg{press("r")}, []string{"repeat"}},
		{[]tea.Msg{press("c")}, []string{"autoplay true"}},
		{[]tea.Msg{NowPlayingMsg{func() NowPlaying { np := playingNow(); np.Autoplay = true; return np }()}, press("c")}, []string{"autoplay false"}},
		{[]tea.Msg{press("l"), press("s")}, []string{"shuffle false"}}, // from the main pane too
	}
	for _, tt := range tests {
		_, m, tr := transportEnv(t)
		drive(m, tt.keys...)
		if got := tr.got(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%v: calls %v, want %v", tt.keys, got, tt.want)
		}
	}
	// Shuffle turns on when it is off.
	_, m, tr := transportEnv(t)
	np := playingNow()
	np.Shuffle = false
	drive(m, NowPlayingMsg{np}, press("s"))
	if got := tr.got(); !reflect.DeepEqual(got, []string{"shuffle true"}) {
		t.Errorf("calls %v", got)
	}
	// Letters type into the search input instead.
	_, m, tr = transportEnv(t)
	drive(m, press("/"), press("s"), press("n"), press("p"), press("r"), tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := tr.got(); len(got) != 0 || searchView(m).input.Value() != "snpr " {
		t.Errorf("typing reached the player: %v, query %q", got, searchView(m).input.Value())
	}
	// An error is a notice; without a player nothing happens.
	e, m, tr := transportEnv(t)
	tr.err = errors.New("ipc broke")
	if n := drive(m, press("n")); len(n) != 1 || !strings.HasPrefix(n[0], "Playback: ") || strings.Contains(n[0], "ipc") {
		t.Errorf("notices %q", n)
	}
	e.svc.Transport = nil
	if m.transport(actNext) != nil {
		t.Error("command without a transport")
	}
}

func TestBarClicks(t *testing.T) {
	_, m, tr := transportEnv(t)
	bar := m.layout.Bar
	_, zones := controlsLine(m.svc.playing, bar.W)
	want := []string{"shuffle false", "prev", "pause", "next", "repeat", "autoplay true"}
	for i, z := range zones {
		m.last = click{} // no double-clicks
		drive(m, tea.MouseClickMsg{X: bar.X + z.x0, Y: bar.Y + barControls, Button: tea.MouseLeft})
		drive(m, tea.MouseClickMsg{X: bar.X + z.x1 - 1, Y: bar.Y + barControls, Button: tea.MouseRight}) // not a left click
		if got := tr.got(); len(got) != i+1 || got[i] != want[i] {
			t.Fatalf("zone %d: calls %v", i, got)
		}
	}
	// Between controls, on another bar row, or with nothing playing:
	// nothing.
	n := len(tr.got())
	drive(m, tea.MouseClickMsg{X: bar.X + zones[0].x1, Y: bar.Y + barControls, Button: tea.MouseLeft}) // the gap right after shuffle
	drive(m, tea.MouseClickMsg{X: bar.X + zones[2].x0, Y: bar.Y + barProgress, Button: tea.MouseLeft})
	drive(m, NowPlayingMsg{NowPlaying{}})
	drive(m, tea.MouseClickMsg{X: bar.X + zones[2].x0, Y: bar.Y + barControls, Button: tea.MouseLeft})
	if got := tr.got(); len(got) != n {
		t.Errorf("stray clicks: %v", got[n:])
	}
}

// AC #8: enter on the current track toggles pause; otherwise it plays.
func TestEnterOnCurrentTrack(t *testing.T) {
	tests := []struct {
		name  string
		state PlayState
		id    string // the current track
		moves []tea.Msg
		pause bool
	}{
		{"playing", Playing, "1", nil, true},
		{"paused", Paused, "1", nil, true},
		{"stopped at the end of the queue", Stopped, "1", nil, false},
		{"loading", Loading, "1", nil, false},
		{"another track", Playing, "1", []tea.Msg{press("j")}, false},
		{"same ID from another list", Playing, "2", []tea.Msg{press("j")}, true}, // decision 48
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, m, tr := transportEnv(t)
			np := playingNow()
			np.State = tt.state
			cur := QueueItem{Handle: 9, Track: QueueTrack{ID: tt.id}}
			np.Current = &cur
			drive(m, NowPlayingMsg{np})
			drive(m, press("/"))
			searchView(m).input.SetValue("daft punk")
			drive(m, press("enter"))
			drive(m, tt.moves...)
			drive(m, press("enter"))
			paused := reflect.DeepEqual(tr.got(), []string{"pause"})
			played := len(e.play.got()) == 1
			if paused != tt.pause || played == tt.pause {
				t.Errorf("transport %v, play calls %d", tr.got(), len(e.play.got()))
			}
		})
	}
	// In the Queue view, enter on a stopped current track plays it again.
	_, m, tr := transportEnv(t)
	np := playingNow()
	np.State = Stopped
	drive(m, NowPlayingMsg{np}, press("4"), press("l"), press("enter"))
	if got := tr.got(); !reflect.DeepEqual(got, []string{"jump 1"}) {
		t.Errorf("queue enter on a stopped track: %v", got)
	}
	if (*Services)(nil).pausable("1") {
		t.Error("nil services")
	}
}

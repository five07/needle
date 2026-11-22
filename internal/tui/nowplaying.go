package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// PlayState is the transport state shown in the now-playing bar.
type PlayState int

// Transport states.
const (
	Stopped PlayState = iota
	Loading
	Playing
	Paused
)

// RepeatMode is the queue's repeat setting.
type RepeatMode int

// Repeat modes, in the order r cycles through them.
const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// QualityKind says how faithfully the output matches the source.
type QualityKind int

// Quality kinds.
const (
	// QualityUnknown: not assessed yet.
	QualityUnknown QualityKind = iota
	// QualityAltered: the output differs from the source.
	QualityAltered
	// QualityMatched: shared output at the source rate, or a lossy source.
	QualityMatched
	// QualityBitPerfect: the source samples reach the device unchanged.
	QualityBitPerfect
)

// QueueTrack is a queued track as the UI shows it.
type QueueTrack struct {
	ID       string
	Title    string
	Artists  string
	Album    string
	Duration time.Duration
}

// QueueItem is one queue entry. Handle identifies it for Jump and Remove.
type QueueItem struct {
	Handle uint64
	Track  QueueTrack
}

// NowPlaying is a snapshot of the player and the queue for the UI.
type NowPlaying struct {
	State PlayState
	// Current is the playing or paused entry; nil when the queue is empty.
	Current  *QueueItem
	Upcoming []QueueItem
	Position time.Duration
	Duration time.Duration
	// Source is the received format, for example "FLAC 24/96"; Output is
	// what reaches the device, for example "24/96". Empty when unknown.
	Source string
	Output string
	// Mode is the audio mode, "shared" or "exclusive".
	Mode    string
	Quality QualityKind
	// Reason explains Quality, for example "resampled to 48 kHz".
	Reason  string
	Preview bool
	Shuffle bool
	Repeat  RepeatMode
	// Best is the highest quality TIDAL has delivered, for example
	// "FLAC 24/192", and BestSeen when it first arrived (TASK-014).
	Best     string
	BestSeen time.Time
}

// NowPlayingMsg delivers a new player snapshot.
type NowPlayingMsg struct{ NowPlaying NowPlaying }

// listenStatus waits for the next snapshot on the status channel. It
// returns nil when there is no channel, and stops when the channel is
// closed.
func (m *Model) listenStatus() tea.Cmd {
	if m.status == nil {
		return nil
	}
	ch := m.status
	return func() tea.Msg {
		np, ok := <-ch
		if !ok {
			return nil
		}
		return NowPlayingMsg{np}
	}
}

// nowPlayingWatcher is implemented by views that show the queue or the
// player state, such as the Queue view.
type nowPlayingWatcher interface {
	NowPlayingChanged(NowPlaying) tea.Cmd
}

// nowPlayingChanged tells every open view about a new snapshot.
func (m *Model) nowPlayingChanged() tea.Cmd {
	var cmds []tea.Cmd
	for _, stack := range m.stacks {
		for _, v := range stack {
			if w, ok := v.(nowPlayingWatcher); ok {
				cmds = append(cmds, w.NowPlayingChanged(m.svc.playing))
			}
		}
	}
	return tea.Batch(cmds...)
}

// pausable reports whether enter on track id should toggle pause: it is
// the current track, playing or paused, and there is a player.
func (s *Services) pausable(id string) bool {
	if s == nil || s.Transport == nil || !s.playing.playing(id) {
		return false
	}
	return s.playing.State == Playing || s.playing.State == Paused
}

// playing reports whether track id is the current entry.
func (np NowPlaying) playing(id string) bool {
	return np.Current != nil && id != "" && np.Current.Track.ID == id
}

// Now-playing bar rows (AC #7). The title takes two rows: the second is
// the lower half of a 2x title (decision 47 leaves it blank for now).
const (
	barTitle = iota
	barTitleLower
	barArtist
	barFormat
	barProgress
	barControls
)

// MarqueeStep is how often a title too wide for the bar scrolls one cell.
const MarqueeStep = 200 * time.Millisecond

// marqueeGap separates the end of a scrolling title from its next pass.
const marqueeGap = "   •   "

// barAction is what a click on a bar control does.
type barAction int

const (
	actShuffle barAction = iota
	actPrevious
	actPlayPause
	actNext
	actRepeat
)

// barZone is the cells [x0, x1) of the controls row a control occupies,
// relative to the bar.
type barZone struct {
	x0, x1 int
	act    barAction
}

// barLines draws the now-playing bar, every line centered (AC #7).
func (m *Model) barLines(w int) []string {
	out := make([]string, BarHeight)
	for i := range out {
		out[i] = strings.Repeat(" ", w)
	}
	np := m.svc.playing
	if np.Current == nil {
		out[barArtist] = styleDim.Render(center("Nothing playing", w))
		return out
	}
	t := np.Current.Track
	out[barTitle] = styleTitle.Render(center(m.marquee(Sanitize(t.Title), w), w))
	out[barArtist] = center(joinNonEmpty(" · ", Sanitize(t.Artists), Sanitize(t.Album)), w)
	out[barFormat] = formatLine(np, w)
	out[barProgress] = progressLine(np, w)
	controls, _ := controlsLine(np, w)
	out[barControls] = controls
	return out
}

// marquee returns title, or for a title wider than w a w-cell window
// that scrolls one cell per MarqueeStep since the track started.
func (m *Model) marquee(title string, w int) string {
	tw := ansi.StringWidth(title)
	if tw <= w || w <= 0 {
		return title
	}
	loop := title + marqueeGap
	cycle := ansi.StringWidth(loop)
	off := int(m.now().Sub(m.marqueeFrom)/MarqueeStep) % cycle
	return fit(ansi.Cut(loop+loop, off, off+w), w)
}

// titleScrolls reports whether the current title needs the marquee.
func (m *Model) titleScrolls() bool {
	np := m.svc.playing
	return np.Current != nil && ansi.StringWidth(Sanitize(np.Current.Track.Title)) > m.layout.Bar.W
}

// marqueeMsg redraws a scrolling title. gen drops ticks of an older chain.
type marqueeMsg struct{ gen int }

// startMarquee resets the scroll when the track changes, and keeps one
// tick chain running while the title is too wide.
func (m *Model) startMarquee() tea.Cmd {
	np := m.svc.playing
	var h uint64
	if np.Current != nil {
		h = np.Current.Handle
	}
	if h != m.marqueeTrack {
		m.marqueeTrack, m.marqueeFrom = h, m.now()
	}
	if m.marqueeOn || !m.titleScrolls() {
		return nil
	}
	m.marqueeOn = true
	m.marqueeGen++
	return m.marqueeTick()
}

func (m *Model) marqueeTick() tea.Cmd {
	gen := m.marqueeGen
	return tea.Tick(MarqueeStep, func(time.Time) tea.Msg { return marqueeMsg{gen} })
}

// onMarquee continues the chain while the title still scrolls.
func (m *Model) onMarquee(msg marqueeMsg) tea.Cmd {
	if msg.gen != m.marqueeGen {
		return nil
	}
	if !m.titleScrolls() {
		m.marqueeOn = false
		return nil
	}
	return m.marqueeTick()
}

// formatLine shows the received format, the output, the audio mode, and
// whether the output differs from the source (AC #2).
func formatLine(np NowPlaying, w int) string {
	type seg struct {
		text  string
		style func(...string) string
	}
	var segs []seg
	plain := func(s ...string) string { return strings.Join(s, "") }
	if np.Preview {
		segs = append(segs, seg{"PREVIEW", styleWarn.Render})
	}
	if np.Source != "" {
		src := Sanitize(np.Source)
		if np.Output != "" {
			src += " → out " + Sanitize(np.Output)
		}
		segs = append(segs, seg{src, plain})
	}
	if np.Mode != "" && (np.Source != "" || np.Preview) {
		segs = append(segs, seg{Sanitize(np.Mode), plain})
	}
	switch np.Quality {
	case QualityBitPerfect:
		segs = append(segs, seg{"✓ bit-perfect", styleGood.Render})
	case QualityMatched:
		segs = append(segs, seg{"✓ rate matched", styleGood.Render})
	case QualityAltered:
		segs = append(segs, seg{"⚠ " + Sanitize(np.Reason), styleWarn.Render})
	}
	if len(segs) == 0 {
		return strings.Repeat(" ", w)
	}
	texts := make([]string, len(segs))
	for i, s := range segs {
		texts[i] = s.text
	}
	line := strings.Join(texts, " · ")
	if ansi.StringWidth(line) > w {
		return center(line, w) // too narrow for colors: truncated plain text
	}
	left := (w - ansi.StringWidth(line)) / 2
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", left))
	for i, s := range segs {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(s.style(s.text))
	}
	b.WriteString(strings.Repeat(" ", w-left-ansi.StringWidth(line)))
	return b.String()
}

// progressLine shows elapsed time, a bar, and the total (display only).
func progressLine(np NowPlaying, w int) string {
	switch np.State {
	case Loading:
		return styleDim.Render(center("Loading…", w))
	case Stopped:
		return styleDim.Render(center("Stopped", w))
	}
	dur := np.Duration
	if dur <= 0 && np.Current != nil {
		dur = np.Current.Track.Duration
	}
	pos := min(max(np.Position, 0), max(dur, 0))
	el, tot := barClock(pos), barClock(dur)
	barW := min(max(w-len(el)-len(tot)-2-8, 0), 60)
	if barW < 5 {
		return center(el+" / "+tot, w)
	}
	filled := 0
	if dur > 0 {
		filled = int(int64(barW-1) * int64(pos) / int64(dur))
	}
	bar := styleNotice.Render(strings.Repeat("━", filled)+"●") + styleDim.Render(strings.Repeat("─", barW-1-filled))
	line := el + " " + bar + " " + tot
	left := (w - ansi.StringWidth(line)) / 2
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", w-left-ansi.StringWidth(line))
}

// barClock is clock with "0:00" for zero or unknown.
func barClock(d time.Duration) string {
	if c := clock(d); c != "" {
		return c
	}
	return "0:00"
}

// controlsLine draws shuffle, previous/play-pause/next, and repeat, and
// returns where each sits for clicks (AC #3, #5).
func controlsLine(np NowPlaying, w int) (string, []barZone) {
	shuffle := "⇄ off"
	if np.Shuffle {
		shuffle = "⇄ on"
	}
	repeat := [...]string{RepeatOff: "↻ off", RepeatAll: "↻ all", RepeatOne: "↻ one"}[min(max(np.Repeat, 0), RepeatOne)]
	play := "[▶]"
	if np.State == Playing || np.State == Loading {
		play = "[⏸]"
	}
	parts := []struct {
		text string
		act  barAction
		gap  string // after the part
	}{
		{shuffle, actShuffle, "    "},
		{"[⏮]", actPrevious, "  "},
		{play, actPlayPause, "  "},
		{"[⏭]", actNext, "    "},
		{repeat, actRepeat, ""},
	}
	total := 0
	for _, p := range parts {
		total += ansi.StringWidth(p.text) + len(p.gap)
	}
	if total > w {
		return center("", w), nil
	}
	x := (w - total) / 2
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", x))
	zones := make([]barZone, 0, len(parts))
	for _, p := range parts {
		pw := ansi.StringWidth(p.text)
		zones = append(zones, barZone{x, x + pw, p.act})
		text := p.text
		if (p.act == actShuffle && np.Shuffle) || (p.act == actRepeat && np.Repeat != RepeatOff) {
			text = styleNotice.Render(text)
		}
		b.WriteString(text + p.gap)
		x += pw + len(p.gap)
	}
	b.WriteString(strings.Repeat(" ", w-x))
	return b.String(), zones
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// transport runs a playback action from a key or a bar click. Errors
// show as a notice; nothing happens without a player.
func (m *Model) transport(act barAction) tea.Cmd {
	t := m.svc.Transport
	if t == nil {
		return nil
	}
	var f func(context.Context) error
	switch act {
	case actPlayPause:
		f = t.TogglePause
	case actNext:
		f = t.Next
	case actPrevious:
		f = t.Previous
	case actShuffle:
		on := !m.svc.playing.Shuffle
		f = func(ctx context.Context) error { return t.SetShuffle(ctx, on) }
	case actRepeat:
		f = t.CycleRepeat
	}
	return m.svc.transportCmd(f)
}

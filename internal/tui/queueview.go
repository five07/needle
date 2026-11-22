package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/five07/tidal-tui/internal/tidal"
)

// ClearConfirm is how long after a first D a second D clears the queue
// (decision 49).
const ClearConfirm = 2 * time.Second

// QueueView is the Queue section: the current track, then the upcoming
// ones (AC #6). History is not shown.
type QueueView struct {
	svc   *Services
	list  List
	items []QueueItem // current first, then upcoming; rows of list
	width int
	// armed is when D was first pressed; zero when not waiting.
	armed time.Time
}

// NewQueueView returns the Queue section view.
func NewQueueView(svc *Services) *QueueView {
	v := &QueueView{svc: svc}
	v.NowPlayingChanged(svc.playing)
	return v
}

// Title implements View. The sidebar shows the upcoming count.
func (v *QueueView) Title() string {
	if n := len(v.svc.playing.Upcoming); n > 0 {
		return fmt.Sprintf("Queue (%d)", n)
	}
	return "Queue"
}

// NowPlayingChanged implements nowPlayingWatcher: it keeps the cursor on
// the same entry when it is still queued.
func (v *QueueView) NowPlayingChanged(np NowPlaying) tea.Cmd {
	var sel uint64
	if c := v.list.Cursor(); c < len(v.items) {
		sel = v.items[c].Handle
	}
	cur := v.list.Cursor()
	v.items = v.items[:0]
	if np.Current != nil {
		v.items = append(v.items, *np.Current)
	}
	v.items = append(v.items, np.Upcoming...)
	v.relayout()
	v.list.Select(cur)
	for i, it := range v.items {
		if it.Handle == sel {
			v.list.Select(i)
		}
	}
	return nil
}

func (v *QueueView) relayout() {
	np := v.svc.playing
	rows := make([]string, len(v.items))
	for i, it := range v.items {
		t := it.Track
		row := rowText(entry{kind: entryTrack, track: tidal.Track{Title: t.Title, Artists: []string{t.Artists}, Album: t.Album, Duration: t.Duration}},
			max(v.width, 1), false)
		if i == 0 && np.Current != nil && np.Current.Handle == it.Handle {
			mark := "▶ "
			if np.State == Paused {
				mark = "⏸ "
			}
			row = mark + strings.TrimPrefix(row, indent)
		}
		rows[i] = row
	}
	v.list.SetRows(rows)
}

// Render implements View.
func (v *QueueView) Render(width, height int, focused bool) string {
	if width != v.width {
		v.width = width
		v.relayout()
	}
	if len(v.items) == 0 {
		return styleDim.Render(fit("The queue is empty. Press enter on a track to play it, or a to add it.", width))
	}
	return v.list.Render(width, height, focused)
}

// Update implements View.
func (v *QueueView) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if v.list.HandleKey(m) {
			return nil
		}
		switch {
		case key.Matches(m, Keys.Open):
			return v.activate()
		case key.Matches(m, queueRemove):
			return v.remove()
		case key.Matches(m, queueClear):
			return v.clear()
		}
	case ClickMsg:
		if i := v.list.RowAt(m.Y); i >= 0 {
			v.list.Select(i)
			if m.Double {
				return v.activate()
			}
		}
	case WheelMsg:
		v.list.Scroll(m.Delta)
	}
	return nil
}

// selected returns the entry under the cursor.
func (v *QueueView) selected() (QueueItem, bool) {
	if c := v.list.Cursor(); c >= 0 && c < len(v.items) {
		return v.items[c], true
	}
	return QueueItem{}, false
}

// activate plays the selected entry, or toggles pause on the current one
// (AC #8).
func (v *QueueView) activate() tea.Cmd {
	it, ok := v.selected()
	if !ok || v.svc.Transport == nil {
		return nil
	}
	if c := v.svc.playing.Current; c != nil && c.Handle == it.Handle && v.svc.pausable(c.Track.ID) {
		return v.svc.transportCmd(v.svc.Transport.TogglePause)
	}
	h := it.Handle
	return v.svc.transportCmd(func(ctx context.Context) error { return v.svc.Transport.Jump(ctx, h) })
}

func (v *QueueView) remove() tea.Cmd {
	it, ok := v.selected()
	if !ok || v.svc.Transport == nil {
		return nil
	}
	if c := v.svc.playing.Current; c != nil && c.Handle == it.Handle {
		return Notify("The playing track stays in the queue. Press n to skip it.")
	}
	h := it.Handle
	return v.svc.transportCmd(func(ctx context.Context) error { return v.svc.Transport.Remove(ctx, h) })
}

// clear asks first; a second D within ClearConfirm clears (decision 49).
func (v *QueueView) clear() tea.Cmd {
	n := len(v.svc.playing.Upcoming)
	if n == 0 || v.svc.Transport == nil {
		return nil
	}
	now := v.svc.now()
	if v.armed.IsZero() || now.Sub(v.armed) > ClearConfirm {
		v.armed = now
		return Notify("Press D again to clear the queue")
	}
	v.armed = time.Time{}
	clear := v.svc.transportCmd(v.svc.Transport.Clear)
	return tea.Batch(clear, Notify(fmt.Sprintf("Cleared %d upcoming %s", n, plural(n, "track", "tracks"))))
}

// Queue view keys (UI.md section 5).
var (
	queueRemove = key.NewBinding(key.WithKeys("d", "x"), key.WithHelp("d", "remove"))
	queueClear  = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "clear upcoming"))
)

// Bindings implements View.
func (v *QueueView) Bindings() []key.Binding {
	if len(v.items) == 0 {
		return nil
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play / pause")),
		queueRemove, queueClear,
	}
}

// Capturing implements View.
func (v *QueueView) Capturing() bool { return false }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

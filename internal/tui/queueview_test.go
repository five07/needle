package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func queueView(m *Model) *QueueView { return m.stacks[SectionQueue][0].(*QueueView) }

func TestQueueViewGoldens(t *testing.T) {
	tests := []struct {
		name string
		np   func() NowPlaying
	}{
		{"queue_80x24", playingNow},
		{"queue_paused_80x24", func() NowPlaying { np := playingNow(); np.State = Paused; return np }},
		{"queue_empty_80x24", func() NowPlaying { return NowPlaying{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, m, _ := transportEnv(t)
			drive(m, NowPlayingMsg{tt.np()}, press("4"), press("l"))
			golden(t, tt.name, m.render())
		})
	}
}

func TestQueueView(t *testing.T) {
	_, m, tr := transportEnv(t)
	v := queueView(m)
	if v.Title() != "Queue (2)" || !strings.Contains(ansi.Strip(m.render()), "Queue (2)") {
		t.Errorf("sidebar label %q", v.Title())
	}
	drive(m, press("4"), press("l"))
	// enter on the current track toggles pause; on another, jumps.
	drive(m, press("enter"))
	drive(m, press("j"), press("enter"))
	// d and x remove an upcoming track; the current one stays.
	drive(m, press("d"))
	drive(m, press("j"), press("x"))
	if got, want := tr.got(), []string{"pause", "jump 2", "remove 2", "remove 3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
	if n := drive(m, press("g"), press("d")); len(n) != 1 || !strings.Contains(n[0], "stays in the queue") {
		t.Errorf("removing the current track: %q", n)
	}
	// A snapshot keeps the cursor on the same entry.
	drive(m, press("G"))
	np := playingNow()
	np.Upcoming = np.Upcoming[1:] // track 2 was removed
	drive(m, NowPlayingMsg{np})
	if it, _ := v.selected(); it.Handle != 3 || v.Title() != "Queue (1)" {
		t.Errorf("cursor on %d, title %q", it.Handle, v.Title())
	}
	// When the selected entry is gone, the cursor stays in range.
	np.Upcoming = nil
	drive(m, NowPlayingMsg{np})
	if it, ok := v.selected(); !ok || it.Handle != 1 || v.Title() != "Queue" {
		t.Errorf("cursor on %+v %v", it, ok)
	}
	// Clicks select; a double click plays; the wheel scrolls.
	drive(m, NowPlayingMsg{playingNow()})
	main := m.layout.Main
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 2, Button: tea.MouseLeft})
	if it, _ := v.selected(); it.Handle != 3 {
		t.Errorf("click selected %d", it.Handle)
	}
	n := len(tr.got())
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 2, Button: tea.MouseLeft})
	drive(m, tea.MouseWheelMsg{X: main.X + 3, Y: main.Y + 2, Button: tea.MouseWheelDown})
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 9, Button: tea.MouseLeft}) // below the rows
	if got := tr.got(); len(got) != n+1 || got[n] != "jump 3" {
		t.Errorf("double click: %v", got[n:])
	}
	if keys := bindingKeys(v.Bindings()); keys != "enter d D" {
		t.Errorf("bindings %q", keys)
	}
}

func bindingKeys(bs []key.Binding) string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Help().Key)
	}
	return strings.Join(out, " ")
}

func TestQueueClearConfirm(t *testing.T) {
	e, m, tr := transportEnv(t)
	at := time.Unix(1000, 0)
	e.svc.Now = func() time.Time { return at }
	drive(m, press("4"), press("l"))
	if n := drive(m, press("D")); len(n) != 1 || n[0] != "Press D again to clear the queue" || len(tr.got()) != 0 {
		t.Fatalf("first D: %q %v", n, tr.got())
	}
	// Too late: the second D asks again.
	at = at.Add(ClearConfirm + time.Millisecond)
	if drive(m, press("D")); len(tr.got()) != 0 {
		t.Fatal("cleared after the window")
	}
	at = at.Add(ClearConfirm)
	if n := drive(m, press("D")); !reflect.DeepEqual(tr.got(), []string{"clear"}) || len(n) != 1 || n[0] != "Cleared 2 upcoming tracks" {
		t.Errorf("second D: %q %v", n, tr.got())
	}
	// The confirmation is used up: D asks again.
	if n := drive(m, press("D")); len(n) != 1 || n[0] != "Press D again to clear the queue" {
		t.Errorf("after clearing: %q", n)
	}
	np := playingNow()
	np.Upcoming = np.Upcoming[:1]
	drive(m, NowPlayingMsg{np}) // still armed from the D above
	if n := drive(m, press("D")); len(n) != 1 || n[0] != "Cleared 1 upcoming track" {
		t.Errorf("one track: %q", n)
	}
	// Nothing upcoming: D does nothing.
	np.Upcoming = nil
	if n := drive(m, NowPlayingMsg{np}, press("D")); len(n) != 0 {
		t.Errorf("empty: %q", n)
	}
}

func TestQueueViewWithoutTransport(t *testing.T) {
	v := NewQueueView(&Services{playing: playingNow()})
	if v.activate() != nil || v.remove() != nil || v.clear() != nil || v.Capturing() {
		t.Error("acted without a transport")
	}
	empty := NewQueueView(&Services{})
	if empty.Bindings() != nil || empty.activate() != nil || !strings.Contains(ansi.Strip(empty.Render(80, 5, true)), "queue is empty") {
		t.Error("empty queue")
	}
	// Without a transport, the Queue section is a placeholder.
	if _, ok := New(Config{Services: &Services{}}).stacks[SectionQueue][0].(*Placeholder); !ok {
		t.Error("queue view without a player")
	}
}

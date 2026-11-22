package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/<name>.golden (ANSI stripped).
func golden(t *testing.T, name, got string) {
	t.Helper()
	got = ansi.Strip(got) + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch.\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

// Key helpers.
func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

type fixture struct {
	t    *testing.T
	m    *Model
	now  time.Time
	list *ListView
	open []int
}

func newFixture(t *testing.T, w, h int) *fixture {
	f := &fixture{t: t, now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	rows := make([]string, 30)
	for i := range rows {
		rows[i] = fmt.Sprintf("Track %02d", i+1)
	}
	f.list = NewListView("Library", rows)
	f.list.OnOpen = func(i int) tea.Cmd { f.open = append(f.open, i); return nil }
	f.m = New(Config{Library: f.list, Now: func() time.Time { return f.now }})
	f.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return f
}

// send delivers msg and runs any returned command once, feeding its message
// back (one level), so PushMsg and NoticeMsg round-trip like in a program.
// It returns true if the program would quit.
func (f *fixture) send(msgs ...tea.Msg) (quit bool) {
	for _, msg := range msgs {
		_, cmd := f.m.Update(msg)
		if cmd == nil {
			continue
		}
		switch out := cmd().(type) {
		case tea.QuitMsg:
			quit = true
		case PushMsg, PopMsg, NoticeMsg:
			f.m.Update(out)
		}
	}
	return quit
}

func (f *fixture) keys(ks ...string) bool {
	quit := false
	for _, k := range ks {
		quit = f.send(press(k)) || quit
	}
	return quit
}

func (f *fixture) click(x, y int) {
	f.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func TestComputeLayout(t *testing.T) {
	tests := []struct {
		w, h          int
		tooSmall      bool
		compact       bool
		sidebar, main Rect
		bar, hints    Rect
	}{
		{80, 24, false, false, Rect{1, 1, 15, 13}, Rect{17, 1, 62, 13}, Rect{1, 15, 78, 6}, Rect{1, 22, 78, 1}},
		{120, 40, false, false, Rect{1, 1, 15, 29}, Rect{17, 1, 102, 29}, Rect{1, 31, 118, 6}, Rect{1, 38, 118, 1}},
		{79, 24, false, true, Rect{1, 1, 3, 13}, Rect{5, 1, 73, 13}, Rect{1, 15, 77, 6}, Rect{1, 22, 77, 1}},
		{50, 16, false, true, Rect{1, 1, 3, 5}, Rect{5, 1, 44, 5}, Rect{1, 7, 48, 6}, Rect{1, 14, 48, 1}},
		{49, 24, true, false, Rect{}, Rect{}, Rect{}, Rect{}},
		{80, 15, true, false, Rect{}, Rect{}, Rect{}, Rect{}},
	}
	for _, tt := range tests {
		l := ComputeLayout(tt.w, tt.h)
		if l.TooSmall != tt.tooSmall || l.Compact != tt.compact || l.Sidebar != tt.sidebar || l.Main != tt.main || l.Bar != tt.bar || l.Hints != tt.hints {
			t.Errorf("%dx%d = %+v", tt.w, tt.h, l)
		}
	}
	r := Rect{2, 3, 4, 5}
	for _, c := range []struct {
		x, y int
		in   bool
	}{{2, 3, true}, {5, 7, true}, {6, 3, false}, {2, 8, false}, {1, 3, false}} {
		if r.Contains(c.x, c.y) != c.in {
			t.Errorf("Contains(%d,%d) != %v", c.x, c.y, c.in)
		}
	}
}

// Every size renders exactly h lines of exactly w cells.
func TestRenderFitsEverySize(t *testing.T) {
	for _, w := range []int{20, 49, 50, 60, 79, 80, 81, 100, 160} {
		for _, h := range []int{8, 15, 16, 24, 40} {
			f := newFixture(t, w, h)
			for _, help := range []bool{false, true} {
				f.m.help = help
				lines := strings.Split(f.m.render(), "\n")
				if len(lines) != h {
					t.Fatalf("%dx%d help=%v: %d lines", w, h, help, len(lines))
				}
				for i, line := range lines {
					if got := ansi.StringWidth(line); got != w {
						t.Fatalf("%dx%d help=%v line %d width %d: %q", w, h, help, i, got, ansi.Strip(line))
					}
				}
			}
		}
	}
}

func TestGoldenScreens(t *testing.T) {
	tests := []struct {
		name  string
		w, h  int
		setup func(f *fixture)
	}{
		{"main_80x24", 80, 24, nil},
		{"library_list_80x24", 80, 24, func(f *fixture) { f.keys("j", "l", "j", "j") }},
		{"help_80x24", 80, 24, func(f *fixture) { f.keys("?") }},
		{"search_input_80x24", 80, 24, func(f *fixture) { f.keys("/", "d", "a", "f", "t") }},
		{"wide_120x30", 120, 30, func(f *fixture) { f.keys("2", "l", "G") }},
		{"compact_60x18", 60, 18, func(f *fixture) { f.keys("j") }},
		{"help_compact_60x18", 60, 18, func(f *fixture) { f.keys("?") }},
		{"minimum_50x16", 50, 16, nil},
		{"placeholder_80x24", 80, 24, func(f *fixture) { f.keys("3", "l") }},
		{"too_small_40x12", 40, 12, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.w, tt.h)
			if tt.setup != nil {
				tt.setup(f)
			}
			golden(t, tt.name, f.m.render())
		})
	}
}

func TestSidebarKeys(t *testing.T) {
	tests := []struct {
		keys    []string
		section Section
		focus   pane
	}{
		{[]string{"j"}, SectionLibrary, paneSidebar},
		{[]string{"down", "down"}, SectionDiscovery, paneSidebar},
		{[]string{"G"}, SectionAccount, paneSidebar},
		{[]string{"G", "j"}, SectionAccount, paneSidebar},
		{[]string{"G", "k", "up"}, SectionDiscovery, paneSidebar},
		{[]string{"G", "g"}, SectionSearch, paneSidebar},
		{[]string{"k"}, SectionSearch, paneSidebar},
		{[]string{"4"}, SectionQueue, paneSidebar},
		{[]string{"j", "l"}, SectionLibrary, paneMain},
		{[]string{"j", "right"}, SectionLibrary, paneMain},
		{[]string{"j", "enter"}, SectionLibrary, paneMain},
		{[]string{"j", "l", "h"}, SectionLibrary, paneSidebar},
		{[]string{"j", "l", "left"}, SectionLibrary, paneSidebar},
		{[]string{"j", "l", "5"}, SectionAccount, paneMain},
		{[]string{"h"}, SectionSearch, paneSidebar},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.keys, ","), func(t *testing.T) {
			f := newFixture(t, 80, 24)
			if f.keys(tt.keys...) {
				t.Fatal("quit")
			}
			if f.m.section != tt.section || f.m.focus != tt.focus {
				t.Errorf("section %d focus %d, want %d %d", f.m.section, f.m.focus, tt.section, tt.focus)
			}
		})
	}
}

func TestListKeys(t *testing.T) {
	tests := []struct {
		keys   []string
		cursor int
	}{
		{[]string{"j", "j", "down"}, 3},
		{[]string{"j", "j", "k"}, 1},
		{[]string{"up"}, 0},
		{[]string{"G"}, 29},
		{[]string{"G", "g"}, 0},
		{[]string{"ctrl+d"}, 6}, // 13 visible rows: half page is 6
		{[]string{"ctrl+d", "ctrl+d", "ctrl+u"}, 6},
		{[]string{"G", "j"}, 29},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.keys, ","), func(t *testing.T) {
			f := newFixture(t, 80, 24)
			f.keys("2", "l")
			f.m.render() // the list learns its height from rendering
			f.keys(tt.keys...)
			if got := f.list.Cursor(); got != tt.cursor {
				t.Errorf("cursor = %d, want %d", got, tt.cursor)
			}
		})
	}
	// The window follows the cursor.
	f := newFixture(t, 80, 24)
	f.keys("2", "l")
	f.m.render()
	f.keys("G")
	f.m.render()
	if f.list.offset != 17 || f.list.RowAt(12) != 29 || f.list.RowAt(13) != -1 || f.list.RowAt(-1) != -1 {
		t.Errorf("offset = %d", f.list.offset)
	}
	f.keys("enter")
	if len(f.open) != 1 || f.open[0] != 29 {
		t.Errorf("opened %v", f.open)
	}
}

func TestQuitAndHelp(t *testing.T) {
	f := newFixture(t, 80, 24)
	if !f.keys("q") {
		t.Error("q did not quit")
	}
	f = newFixture(t, 80, 24)
	if !f.keys("ctrl+c") {
		t.Error("ctrl+c did not quit")
	}
	f = newFixture(t, 80, 24)
	f.keys("?")
	if !f.m.help {
		t.Fatal("? did not open help")
	}
	// Keys do nothing behind the overlay; q closes it instead of quitting.
	if f.keys("j", "/", "1") || f.m.section != SectionSearch || f.m.focus != paneSidebar {
		t.Error("keys leaked through the help overlay")
	}
	for _, closeKey := range []string{"?", "esc", "q"} {
		f.m.help = true
		if f.keys(closeKey) || f.m.help {
			t.Errorf("%s did not close help (or quit)", closeKey)
		}
	}
	f.m.help = true
	if !f.keys("ctrl+c") {
		t.Error("ctrl+c did not quit from help")
	}
}

func TestHelpListsActiveBindings(t *testing.T) {
	f := newFixture(t, 120, 40)
	f.keys("?")
	screen := ansi.Strip(f.m.render())
	for _, b := range append(Keys.globalGroup().bindings, Keys.navGroup().bindings...) {
		h := b.Help()
		if !strings.Contains(screen, h.Key) || !strings.Contains(screen, h.Desc) {
			t.Errorf("help is missing %s %q", h.Key, h.Desc)
		}
	}
	if !strings.Contains(screen, "search (in the input)") {
		t.Error("help is missing the Search view's bindings")
	}
	// A short terminal shows a scrollable window; every binding is reachable.
	f = newFixture(t, 50, 16)
	f.keys("?")
	seen := ""
	first := ansi.Strip(f.m.render())
	if !strings.Contains(first, "↓ more (j)") || strings.Contains(first, "↑ more") {
		t.Fatalf("first page:\n%s", first)
	}
	for range 40 {
		seen += ansi.Strip(f.m.render())
		f.keys("j")
	}
	last := ansi.Strip(f.m.render())
	if !strings.Contains(last, "↑ more (k)") || strings.Contains(last, "↓ more") {
		t.Errorf("last page:\n%s", last)
	}
	for _, b := range append(Keys.globalGroup().bindings, Keys.navGroup().bindings...) {
		if !strings.Contains(seen, b.Help().Desc) {
			t.Errorf("scrolling never showed %q", b.Help().Desc)
		}
	}
	f.keys("g")
	if ansi.Strip(f.m.render()) != first {
		t.Error("g did not return to the top")
	}
	f.keys("G")
	if ansi.Strip(f.m.render()) != last {
		t.Error("G did not jump to the end")
	}
	f.keys("k")
	if strings.Contains(ansi.Strip(f.m.render()), "↓ more") == false {
		t.Error("k did not scroll up")
	}
	// Reopening starts at the top.
	f.keys("?", "?")
	if ansi.Strip(f.m.render()) != first {
		t.Error("help did not reopen at the top")
	}
}

func TestSearchInputCapturesKeys(t *testing.T) {
	f := newFixture(t, 80, 24)
	f.keys("2", "/")
	if f.m.section != SectionSearch || f.m.focus != paneMain || !f.m.current().Capturing() {
		t.Fatalf("/ did not focus the search input: section %d focus %d", f.m.section, f.m.focus)
	}
	// Global letters type instead of acting.
	if f.keys("q", "j", "?", "1", "/", "h", "backspace") {
		t.Fatal("q quit while typing")
	}
	sv := f.m.current().(*SearchView)
	if sv.Query() != "qj?1/" || f.m.help || f.m.section != SectionSearch {
		t.Errorf("query %q help %v section %d", sv.Query(), f.m.help, f.m.section)
	}
	f.keys("esc")
	if sv.Capturing() || f.m.focus != paneMain {
		t.Error("esc did not leave the input")
	}
	f.keys("/", "enter")
	if sv.Capturing() {
		t.Error("enter did not leave the input")
	}
	if !f.keys("q") {
		t.Error("q did not quit after leaving the input")
	}
	f = newFixture(t, 80, 24)
	f.keys("/")
	if !f.keys("ctrl+c") {
		t.Error("ctrl+c did not quit from the input")
	}
	// Bindings shown in the hint line follow the focus.
	f = newFixture(t, 80, 24)
	f.keys("/")
	if hints := ansi.Strip(f.m.hintLine(78)); !strings.Contains(hints, "esc leave input") {
		t.Errorf("input hints = %q", hints)
	}
	f.keys("esc")
	if hints := ansi.Strip(f.m.hintLine(78)); !strings.Contains(hints, "h back") || !strings.Contains(hints, "enter search (in the input)") {
		t.Errorf("main hints = %q", hints)
	}
}

func TestSubViews(t *testing.T) {
	f := newFixture(t, 80, 24)
	f.keys("2", "l")
	album := NewListView("Album", []string{"a", "b"})
	f.send(PushMsg{View: album})
	if f.m.current() != album || f.m.focus != paneMain {
		t.Fatal("push failed")
	}
	f.keys("h")
	if f.m.current() != f.list || f.m.focus != paneMain {
		t.Error("h did not go back")
	}
	for _, back := range []string{"esc", "backspace"} {
		f.send(PushMsg{View: album})
		f.keys(back)
		if f.m.current() != f.list {
			t.Errorf("%s did not go back", back)
		}
	}
	f.keys("esc") // at the root: stays
	if f.m.current() != f.list || f.m.focus != paneMain {
		t.Error("esc at the root changed state")
	}
	f.send(Push(album), PopMsg{}, PopMsg{}, PushMsg{})
	if f.m.current() != f.list {
		t.Error("Push/Pop commands")
	}
	// The sidebar shows the root title even with a sub-view open, and "/"
	// returns Search to its root.
	f.send(PushMsg{View: album})
	if !strings.Contains(ansi.Strip(f.m.render()), "Library") {
		t.Error("sidebar lost the section name")
	}
	f.keys("1")
	f.send(PushMsg{View: album})
	f.keys("/")
	if _, ok := f.m.current().(*SearchView); !ok {
		t.Error("/ did not return to the search root")
	}
}

func TestMouse(t *testing.T) {
	f := newFixture(t, 80, 24)
	l := f.m.layout
	// Click a sidebar entry: switches section and focuses the sidebar.
	f.click(l.Sidebar.X+2, l.Sidebar.Y+1)
	if f.m.section != SectionLibrary || f.m.focus != paneSidebar {
		t.Errorf("sidebar click: section %d focus %d", f.m.section, f.m.focus)
	}
	f.click(l.Sidebar.X+2, l.Sidebar.Y+10) // below the entries
	if f.m.section != SectionLibrary {
		t.Error("click below the entries changed section")
	}
	// Click a row in the main pane: focuses it and selects the row.
	f.m.render()
	f.click(l.Main.X+3, l.Main.Y+4)
	if f.m.focus != paneMain || f.list.Cursor() != 4 || len(f.open) != 0 {
		t.Errorf("row click: focus %d cursor %d open %v", f.m.focus, f.list.Cursor(), f.open)
	}
	// Double-click on the same cell opens; a third click does not.
	f.now = f.now.Add(200 * time.Millisecond)
	f.click(l.Main.X+3, l.Main.Y+4)
	f.now = f.now.Add(100 * time.Millisecond)
	f.click(l.Main.X+3, l.Main.Y+4)
	if len(f.open) != 1 || f.open[0] != 4 {
		t.Errorf("double click opened %v", f.open)
	}
	// Too slow is two single clicks.
	f.now = f.now.Add(time.Second)
	f.click(l.Main.X+3, l.Main.Y+5)
	f.now = f.now.Add(DoubleClick + time.Millisecond)
	f.click(l.Main.X+3, l.Main.Y+5)
	if len(f.open) != 1 {
		t.Errorf("slow clicks opened %v", f.open)
	}
	// Wheel over the list scrolls it.
	f.list.Select(0)
	f.m.render()
	for range 3 {
		f.send(tea.MouseWheelMsg{X: l.Main.X + 3, Y: l.Main.Y + 2, Button: tea.MouseWheelDown})
	}
	f.m.render()
	if f.list.offset != 3 || f.list.Cursor() != 3 {
		t.Errorf("wheel: offset %d cursor %d", f.list.offset, f.list.Cursor())
	}
	f.send(tea.MouseWheelMsg{X: l.Main.X + 3, Y: l.Main.Y + 2, Button: tea.MouseWheelUp})
	f.m.render()
	if f.list.offset != 2 {
		t.Errorf("wheel up: offset %d", f.list.offset)
	}
	// Wheel over the sidebar moves between sections.
	f.send(tea.MouseWheelMsg{X: l.Sidebar.X + 1, Y: l.Sidebar.Y + 1, Button: tea.MouseWheelDown})
	if f.m.section != SectionDiscovery {
		t.Errorf("sidebar wheel: section %d", f.m.section)
	}
	// Double-click a sidebar entry opens it in the main pane.
	f.now = f.now.Add(time.Second)
	f.click(l.Sidebar.X+2, l.Sidebar.Y+1)
	f.click(l.Sidebar.X+2, l.Sidebar.Y+1)
	if f.m.section != SectionLibrary || f.m.focus != paneMain {
		t.Errorf("sidebar double click: section %d focus %d", f.m.section, f.m.focus)
	}
	// Ignored: right click, horizontal wheel, clicks on the bar.
	before := *f.m
	f.send(
		tea.MouseClickMsg{X: l.Main.X + 3, Y: l.Main.Y + 1, Button: tea.MouseRight},
		tea.MouseWheelMsg{X: l.Main.X + 3, Y: l.Main.Y + 1, Button: tea.MouseWheelLeft},
		tea.MouseWheelMsg{X: l.Bar.X + 3, Y: l.Bar.Y + 1, Button: tea.MouseWheelDown},
	)
	f.now = f.now.Add(time.Second)
	f.click(l.Bar.X+3, l.Bar.Y+1)
	if f.m.section != before.section || f.m.focus != before.focus || f.list.Cursor() != 3 {
		t.Error("ignored mouse input changed state")
	}
	// Any click closes help; the wheel does nothing while it is open.
	f.keys("?")
	f.send(tea.MouseWheelMsg{X: l.Main.X + 3, Y: l.Main.Y + 2, Button: tea.MouseWheelDown})
	f.click(1, 1)
	if f.m.help || f.m.section != SectionLibrary {
		t.Error("click did not just close help")
	}
	// Clicking the search input focuses it; clicking below leaves it.
	f.keys("1")
	f.click(l.Main.X+5, l.Main.Y)
	if !f.m.current().Capturing() {
		t.Error("click did not focus the search input")
	}
	f.now = f.now.Add(time.Second)
	f.click(l.Main.X+5, l.Main.Y+3)
	if f.m.current().Capturing() {
		t.Error("click below did not leave the input")
	}
	// Too small: mouse is ignored.
	f.send(tea.WindowSizeMsg{Width: 30, Height: 10})
	f.click(2, 2)
	f.send(tea.MouseWheelMsg{X: 2, Y: 2, Button: tea.MouseWheelDown})
	if f.m.section != SectionSearch {
		t.Error("mouse acted on a too-small screen")
	}
}

func TestResizeKeepsState(t *testing.T) {
	f := newFixture(t, 80, 24)
	f.keys("2", "l", "G")
	f.send(tea.WindowSizeMsg{Width: 40, Height: 10})
	if !strings.Contains(f.m.render(), "Terminal too small (needs 50x16)") {
		t.Error("no too-small message")
	}
	// Keys still work while too small.
	if f.keys("j"); f.list.Cursor() != 29 {
		t.Error("state lost while too small")
	}
	f.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	f.m.render()
	if f.m.section != SectionLibrary || f.list.Cursor() != 29 || f.list.offset != 1 {
		t.Errorf("after resize: section %d cursor %d offset %d", f.m.section, f.list.Cursor(), f.list.offset)
	}
	if !f.keys("q") {
		t.Error("q did not quit after resize")
	}
}

func TestNotice(t *testing.T) {
	f := newFixture(t, 80, 24)
	_, cmd := f.m.Update(NoticeMsg{Text: "Volume 40%"})
	if cmd == nil || !strings.Contains(ansi.Strip(f.m.hintLine(78)), "Volume 40%") {
		t.Fatal("notice not shown")
	}
	f.send(Notify("Next track")())
	if !strings.Contains(ansi.Strip(f.m.hintLine(78)), "Next track") {
		t.Fatal("second notice not shown")
	}
	// The first notice's timer must not clear the second one.
	f.m.Update(clearNoticeMsg{id: 1})
	if f.m.notice != "Next track" {
		t.Error("stale timer cleared the notice")
	}
	f.m.Update(clearNoticeMsg{id: 2})
	if f.m.notice != "" || !strings.Contains(ansi.Strip(f.m.hintLine(78)), "j/k move") {
		t.Error("notice not cleared")
	}
}

func TestViewSettingsAndStyles(t *testing.T) {
	f := newFixture(t, 80, 24)
	v := f.m.View()
	if !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion || v.WindowTitle != "tidal-tui" {
		t.Errorf("view = %+v", v)
	}
	if f.m.Init() != nil {
		t.Error("Init returned a command")
	}
	// The selected sidebar entry is reverse video while the sidebar has
	// focus, and only bold once focus moves to the main pane.
	lines := strings.Split(f.m.render(), "\n")
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Errorf("focused selection not reverse: %q", lines[1])
	}
	f.keys("l")
	lines = strings.Split(f.m.render(), "\n")
	if strings.Contains(lines[1], "\x1b[7m") || !strings.Contains(lines[1], "\x1b[1m") {
		t.Errorf("unfocused selection: %q", lines[1])
	}
}

// echoView records the messages it gets.
type echoView struct {
	Placeholder
	got []tea.Msg
}

func (e *echoView) Update(msg tea.Msg) tea.Cmd { e.got = append(e.got, msg); return nil }
func (e *echoView) Bindings() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "do x"))}
}

type customMsg struct{}

func TestMessagesReachTheView(t *testing.T) {
	e := &echoView{Placeholder: Placeholder{TitleText: "Discovery"}}
	m := New(Config{Discovery: e})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(press("3"))
	m.Update(press("l"))
	m.Update(press("x"))
	m.Update(customMsg{})
	if len(e.got) != 2 {
		t.Fatalf("got %v", e.got)
	}
	if k, ok := e.got[0].(tea.KeyPressMsg); !ok || k.String() != "x" {
		t.Errorf("key = %v", e.got[0])
	}
	if _, ok := e.got[1].(customMsg); !ok {
		t.Errorf("custom = %v", e.got[1])
	}
	m.Update(press("?"))
	if !strings.Contains(ansi.Strip(m.render()), "do x") {
		t.Error("view binding missing from help")
	}
}

func TestListViewEdges(t *testing.T) {
	v := NewListView("Empty", nil)
	v.Empty = "Nothing here."
	if !strings.Contains(v.Render(20, 3, true), "Nothing here.") || v.Update(press("enter")) != nil {
		t.Error("empty list")
	}
	v.Update(ClickMsg{Y: 0, Double: true})
	v.Update(WheelMsg{Delta: 1})
	if v.Cursor() != 0 || v.Capturing() || v.Bindings() != nil {
		t.Error("empty list state")
	}
	// Without a rendered height every row counts as visible.
	v.SetRows([]string{"a", "b", "c"})
	v.Move(5)
	if v.Cursor() != 2 || v.RowAt(2) != 2 {
		t.Errorf("cursor %d", v.Cursor())
	}
	v.Scroll(-5)
	if v.Update(press("enter")) != nil {
		t.Error("enter with no OnOpen")
	}
	// The window follows the cursor (on row 2).
	if got := ansi.Strip(v.Render(3, 2, false)); got != "b  \nc  " {
		t.Errorf("render = %q", got)
	}
	p := &Placeholder{TitleText: "X", Message: "m"}
	if p.Update(nil) != nil || p.Bindings() != nil || p.Capturing() {
		t.Error("placeholder")
	}
	if fit("abc", 0) != "" || center("abc", 0) != "" || center("abcdef", 4) != "abc…" {
		t.Error("fit/center edges")
	}
}

func TestSearchBody(t *testing.T) {
	s := NewSearchView()
	body := NewListView("Results", []string{"r1", "r2", "r3"})
	opened := -1
	body.OnOpen = func(i int) tea.Cmd { opened = i; return nil }
	s.Body = body
	out := ansi.Strip(s.Render(30, 5, true))
	if !strings.Contains(out, "r1") || strings.Contains(out, "not available") {
		t.Errorf("render = %q", out)
	}
	s.Update(press("j"))
	s.Update(WheelMsg{Delta: 1})
	s.Update(ClickMsg{Y: 4, Double: true}) // row 2 of the body
	if body.Cursor() != 2 || opened != 2 {
		t.Errorf("cursor %d opened %d", body.Cursor(), opened)
	}
	s.Update(customMsg{}) // forwarded to the input, harmless
}

func TestSanitize(t *testing.T) {
	tests := map[string]string{
		"plain":                 "plain",
		"\x1b[31mred\x1b[0m":    "red",
		"title\x1b]0;pwned\x07": "title",
		"bell\a and nul\x00":    "bell and nul",
		"tab\tnew\nline\r":      "tab new line ",
		"c1\u009bcontrol":       "c1control",
		"del\x7f":               "del",
		"Beyoncé · 日本語":         "Beyoncé · 日本語",
	}
	for in, want := range tests {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	l := NewListView("x", []string{"\x1b]52;c;ZXZpbA==\x07ok"})
	if got := ansi.Strip(l.Render(10, 1, false)); got != "ok        " {
		t.Errorf("row = %q", got)
	}
	if strings.Contains(l.Render(10, 1, false), "]52") {
		t.Error("escape sequence reached the screen")
	}
}

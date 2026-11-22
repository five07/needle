package tui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Section indexes the sidebar entries.
type Section int

// Sidebar sections, in order (UI.md section 2).
const (
	SectionSearch Section = iota
	SectionLibrary
	SectionDiscovery
	SectionQueue
	SectionAccount
	sectionCount
)

type pane int

const (
	paneSidebar pane = iota
	paneMain
)

// Timings.
const (
	// NoticeDuration is how long a notice replaces the hint line
	// (decision 25).
	NoticeDuration = 2 * time.Second
	// DoubleClick is the longest gap between the two clicks of a
	// double-click.
	DoubleClick = 400 * time.Millisecond
)

// Config supplies the root view of each section. A nil view is shown as a
// placeholder.
type Config struct {
	Search, Library, Discovery, Queue, Account View
	// Now returns the current time (tests replace it). Default time.Now.
	Now func() time.Time
	// Notices, when set, delivers texts from outside the UI (such as
	// playback errors) to show as notices.
	Notices <-chan string
	// Services lets the default Search view search and play. Nil leaves
	// search unavailable.
	Services *Services
}

// NoticeMsg shows Text in the hint line for NoticeDuration.
type NoticeMsg struct{ Text string }

// Notify returns a command that shows a short notice in the hint line.
func Notify(text string) tea.Cmd { return func() tea.Msg { return NoticeMsg{Text: text} } }

type clearNoticeMsg struct{ id int }

type click struct {
	at   time.Time
	x, y int
}

// Model is the root Bubble Tea model.
type Model struct {
	layout   Layout
	focus    pane
	section  Section
	stacks   [sectionCount][]View
	help     bool
	helpTop  int  // first help line shown, when the help is taller than the screen
	helpLong bool // the help did not fit on the last render and scrolls
	notice   string
	noticeID int
	last     click
	now      func() time.Time
	notices  <-chan string
	svc      *Services
	opened   map[View]bool // views whose Open has run
}

// New returns the shell with the given section views.
func New(cfg Config) *Model {
	m := &Model{now: cfg.Now, focus: paneSidebar, notices: cfg.Notices, svc: cfg.Services, opened: map[View]bool{}}
	if m.now == nil {
		m.now = time.Now
	}
	if cfg.Library == nil && cfg.Services != nil && cfg.Services.Library != nil {
		cfg.Library = NewLibraryView(cfg.Services)
	}
	roots := [sectionCount]View{cfg.Search, cfg.Library, cfg.Discovery, cfg.Queue, cfg.Account}
	names := [sectionCount]string{"Search", "Library", "Discovery", "Queue", "Account"}
	for i, v := range roots {
		if v == nil {
			if i == int(SectionSearch) {
				v = NewSearchView(cfg.Services)
			} else {
				v = &Placeholder{TitleText: names[i], Message: names[i] + " is not available yet."}
			}
		}
		m.stacks[i] = []View{v}
	}
	m.layout = ComputeLayout(80, 24)
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return m.listen() }

type noticeChanMsg struct{ text string }

// listen waits for the next text on the notices channel. It returns nil
// when there is no channel, and stops when the channel is closed.
func (m *Model) listen() tea.Cmd {
	if m.notices == nil {
		return nil
	}
	ch := m.notices
	return func() tea.Msg {
		text, ok := <-ch
		if !ok {
			return nil
		}
		return noticeChanMsg{text}
	}
}

// current returns the view on top of the selected section.
func (m *Model) current() View {
	s := m.stacks[m.section]
	return s[len(s)-1]
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout = ComputeLayout(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		return m, m.withOpen(m.onKey(msg))
	case tea.MouseClickMsg:
		return m, m.withOpen(m.onClick(msg.Mouse()))
	case tea.MouseWheelMsg:
		return m, m.withOpen(m.onWheel(msg.Mouse()))
	case NoticeMsg:
		m.noticeID++
		m.notice = Sanitize(msg.Text)
		id := m.noticeID
		return m, tea.Tick(NoticeDuration, func(time.Time) tea.Msg { return clearNoticeMsg{id} })
	case clearNoticeMsg:
		if msg.id == m.noticeID {
			m.notice = ""
		}
	case PushMsg:
		if msg.View != nil {
			m.stacks[m.section] = append(m.stacks[m.section], msg.View)
			m.focus = paneMain
			return m, m.openCurrent()
		}
	case FavoriteMsg:
		return m, m.favoriteChanged(msg)
	case ForViewMsg:
		for _, stack := range m.stacks {
			for _, v := range stack {
				if v == msg.View {
					return m, v.Update(msg.Msg)
				}
			}
		}
	case noticeChanMsg:
		return m, tea.Batch(Notify(msg.text), m.listen())
	case PopMsg:
		m.pop()
	case CloseMsg:
		for i, stack := range m.stacks {
			if len(stack) > 1 && stack[len(stack)-1] == msg.View {
				delete(m.opened, msg.View)
				m.stacks[i] = stack[:len(stack)-1]
			}
		}
	default:
		// Messages from view commands (input cursor blink, results).
		return m, m.current().Update(msg)
	}
	return m, nil
}

func (m *Model) pop() bool {
	s := m.stacks[m.section]
	if len(s) <= 1 {
		return false
	}
	delete(m.opened, s[len(s)-1])
	m.stacks[m.section] = s[:len(s)-1]
	return true
}

func (m *Model) selectSection(s Section) {
	m.section = min(max(s, 0), sectionCount-1)
}

func (m *Model) onKey(k tea.KeyPressMsg) tea.Cmd {
	if key.Matches(k, Keys.ForceQ) {
		return tea.Quit
	}
	if m.help {
		switch {
		case key.Matches(k, Keys.Help, Keys.Quit) || k.String() == "esc":
			m.help = false
		case key.Matches(k, Keys.Down):
			m.helpTop++
		case key.Matches(k, Keys.Up):
			m.helpTop--
		case key.Matches(k, Keys.Top):
			m.helpTop = 0
		case key.Matches(k, Keys.Bottom):
			m.helpTop = 1 << 20 // clamped when drawn
		}
		return nil
	}
	if m.focus == paneMain && m.current().Capturing() {
		return m.current().Update(k)
	}
	switch {
	case key.Matches(k, Keys.Quit):
		return tea.Quit
	case key.Matches(k, Keys.Help):
		m.help, m.helpTop = true, 0
		return nil
	case key.Matches(k, Keys.Search):
		if f, ok := m.current().(Filterer); ok {
			m.focus = paneMain
			return f.StartFilter()
		}
		m.section = SectionSearch
		m.stacks[SectionSearch] = m.stacks[SectionSearch][:1]
		m.focus = paneMain
		return m.current().Update(FocusInputMsg{})
	case key.Matches(k, Keys.Sections):
		m.selectSection(Section(k.String()[0] - '1'))
		return nil
	}
	if m.focus == paneSidebar {
		switch {
		case key.Matches(k, Keys.Up):
			m.selectSection(m.section - 1)
		case key.Matches(k, Keys.Down):
			m.selectSection(m.section + 1)
		case key.Matches(k, Keys.Top):
			m.selectSection(0)
		case key.Matches(k, Keys.Bottom):
			m.selectSection(sectionCount - 1)
		case key.Matches(k, Keys.Right, Keys.Open):
			m.focus = paneMain
		}
		return nil
	}
	switch {
	case key.Matches(k, Keys.Left):
		if !m.pop() {
			m.focus = paneSidebar
		}
		return nil
	case key.Matches(k, Keys.Back):
		if !m.pop() {
			return m.current().Update(k) // at the root: the view may use esc (clear a filter)
		}
		return nil
	}
	return m.current().Update(k)
}

func (m *Model) onClick(ms tea.Mouse) tea.Cmd {
	if ms.Button != tea.MouseLeft || m.layout.TooSmall {
		return nil
	}
	if m.help {
		m.help = false
		return nil
	}
	now := m.now()
	double := now.Sub(m.last.at) <= DoubleClick && m.last.x == ms.X && m.last.y == ms.Y
	m.last = click{at: now, x: ms.X, y: ms.Y}
	if double {
		m.last = click{} // a third click starts over
	}
	l := m.layout
	switch {
	case l.Sidebar.Contains(ms.X, ms.Y):
		m.focus = paneSidebar
		if row := ms.Y - l.Sidebar.Y; row < int(sectionCount) {
			m.selectSection(Section(row))
			if double {
				m.focus = paneMain
			}
		}
	case l.Main.Contains(ms.X, ms.Y):
		m.focus = paneMain
		return m.current().Update(ClickMsg{X: ms.X - l.Main.X - 1, Y: ms.Y - l.Main.Y, Double: double})
	}
	return nil
}

func (m *Model) onWheel(ms tea.Mouse) tea.Cmd {
	if m.help || m.layout.TooSmall {
		return nil
	}
	delta := 0
	switch ms.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	l := m.layout
	switch {
	case l.Sidebar.Contains(ms.X, ms.Y):
		m.selectSection(m.section + Section(delta))
	case l.Main.Contains(ms.X, ms.Y):
		return m.current().Update(WheelMsg{X: ms.X - l.Main.X - 1, Y: ms.Y - l.Main.Y, Delta: delta})
	}
	return nil
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "tidal-tui"
	return v
}

// withOpen adds the Open command of the current view when it has not run
// yet, so a section's root view loads the first time it is shown.
func (m *Model) withOpen(cmd tea.Cmd) tea.Cmd {
	if open := m.openCurrent(); open != nil {
		return tea.Batch(cmd, open)
	}
	return cmd
}

// openCurrent runs Open once for the current view, if it is an Opener.
func (m *Model) openCurrent() tea.Cmd {
	v := m.current()
	o, ok := v.(Opener)
	if !ok || m.opened[v] {
		return nil
	}
	m.opened[v] = true
	return o.Open()
}

// favoriteChanged records a finished favorite change, tells every open
// view (so markers and Library lists update), and confirms it.
func (m *Model) favoriteChanged(msg FavoriteMsg) tea.Cmd {
	if m.svc != nil {
		m.svc.favs().set(msg.Collection, msg.ID, msg.Added)
	}
	cmds := []tea.Cmd{}
	for _, stack := range m.stacks {
		for _, v := range stack {
			if w, ok := v.(favoriteWatcher); ok {
				cmds = append(cmds, w.FavoriteChanged(msg))
			}
		}
	}
	text := "Removed from favorites: " + msg.Name
	if msg.Added {
		text = "Added to favorites: " + msg.Name
	}
	return tea.Batch(append(cmds, Notify(text))...)
}

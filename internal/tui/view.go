package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// View is the content of the main pane for one section, or a sub-view
// pushed on top of it (an album opened from search results, for example).
type View interface {
	// Title names the view: the sidebar label for a section's root view.
	Title() string
	// Render draws the view into width x height cells.
	Render(width, height int, focused bool) string
	// Update handles a message: key presses while the main pane has focus,
	// ClickMsg and WheelMsg with view-relative coordinates, FocusInputMsg,
	// and any message a command of the view produced.
	Update(msg tea.Msg) tea.Cmd
	// Bindings lists the view's own keys for the help overlay and hint line.
	Bindings() []key.Binding
	// Capturing reports whether a text input has focus. While it does, every
	// key except ctrl+c goes to the view.
	Capturing() bool
}

// ClickMsg is a left click inside a view, relative to the view's top-left
// cell. Double is set for the second click on the same cell within
// DoubleClick.
type ClickMsg struct {
	X, Y   int
	Double bool
}

// WheelMsg is a scroll-wheel step over a view: Delta is -1 (up) or +1 (down).
type WheelMsg struct {
	X, Y  int
	Delta int
}

// FocusInputMsg asks a view to focus its text input (sent by "/").
type FocusInputMsg struct{}

// PushMsg opens View on top of the current section (drill-in).
type PushMsg struct{ View View }

// PopMsg returns to the previous view in the current section.
type PopMsg struct{}

// CloseMsg closes View if it is the top view of its section. A view that
// finishes asynchronously closes itself this way, so it never closes
// whatever the user opened meanwhile.
type CloseMsg struct{ View View }

// Push returns a command that opens v as a sub-view.
func Push(v View) tea.Cmd { return func() tea.Msg { return PushMsg{View: v} } }

// Opener is implemented by views that start work (such as loading) when
// they are pushed. The shell runs the returned command after the push.
type Opener interface {
	Open() tea.Cmd
}

// Revisiter is implemented by views that refresh when shown again (after
// another view was shown), such as Discovery reloading stale mixes.
type Revisiter interface {
	Revisit() tea.Cmd
}

// Filterer is implemented by views with their own filter. In such a view
// "/" starts the filter instead of going to Search.
type Filterer interface {
	StartFilter() tea.Cmd
}

// HintLeader is implemented by views whose most important keys go first
// in the hint line, ahead of the general keys, so they still show on small
// terminals. LeadHints is how many of the first Bindings lead.
type HintLeader interface {
	LeadHints() int
}

// ForViewMsg carries Msg to View wherever View is in the section stacks,
// so a result reaches the view that asked for it even after the user
// switched sections or opened another view. It is dropped when View has
// been closed. Views must be pointers so they compare by identity.
type ForViewMsg struct {
	View View
	Msg  tea.Msg
}

// To returns a command that runs f off the UI goroutine and delivers its
// result to v.
func To(v View, f func() tea.Msg) tea.Cmd {
	return func() tea.Msg { return ForViewMsg{View: v, Msg: f()} }
}

// List is a scrollable, selectable list of rows. Views embed it.
type List struct {
	rows   []string
	cursor int
	offset int
	height int
	// heading, when set, marks group heading rows: drawn as headings and
	// never highlighted as selected.
	heading func(i int) bool
}

// SetRows replaces the rows, keeping the cursor in range. Rows are plain
// text: escape sequences and control characters are removed, so text from
// the API cannot drive the terminal.
func (l *List) SetRows(rows []string) {
	l.rows = make([]string, len(rows))
	for i, r := range rows {
		l.rows[i] = Sanitize(r)
	}
	l.clamp()
}

// Sanitize makes untrusted text safe to draw: it removes escape sequences
// and control characters (tabs and newlines become spaces).
func Sanitize(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
}

// Len returns the number of rows.
func (l *List) Len() int { return len(l.rows) }

// Cursor returns the selected row index (0 when empty).
func (l *List) Cursor() int { return l.cursor }

// Select moves the cursor to row i, clamped to the list.
func (l *List) Select(i int) {
	l.cursor = i
	l.clamp()
}

// Move moves the cursor by delta rows.
func (l *List) Move(delta int) { l.Select(l.cursor + delta) }

// Scroll moves the visible window by delta rows, dragging the cursor along
// when it would leave the window.
func (l *List) Scroll(delta int) {
	h := l.visible()
	l.offset = min(max(l.offset+delta, 0), max(len(l.rows)-h, 0))
	if l.cursor < l.offset {
		l.cursor = l.offset
	}
	if l.cursor >= l.offset+h {
		l.cursor = l.offset + h - 1
	}
}

// HandleKey applies list navigation keys and reports whether k was one.
func (l *List) HandleKey(k tea.KeyPressMsg) bool {
	half := max(1, l.visible()/2)
	switch {
	case key.Matches(k, Keys.Up):
		l.Move(-1)
	case key.Matches(k, Keys.Down):
		l.Move(1)
	case key.Matches(k, Keys.Top):
		l.Select(0)
	case key.Matches(k, Keys.Bottom):
		l.Select(len(l.rows) - 1)
	case key.Matches(k, Keys.HalfDown):
		l.Move(half)
	case key.Matches(k, Keys.HalfUp):
		l.Move(-half)
	default:
		return false
	}
	return true
}

// RowAt returns the row index shown at view line y, or -1.
func (l *List) RowAt(y int) int {
	i := l.offset + y
	if y < 0 || y >= l.visible() || i >= len(l.rows) {
		return -1
	}
	return i
}

// Render draws the visible rows into width x height cells.
func (l *List) Render(width, height int, focused bool) string {
	l.height = height
	l.clamp()
	var b strings.Builder
	for y := range height {
		i := l.offset + y
		if i >= len(l.rows) {
			break
		}
		line := fit(l.rows[i], width)
		if l.heading != nil && l.heading(i) {
			line = styleHeading.Render(line)
		} else if i == l.cursor {
			if focused {
				line = styleSelected.Render(line)
			} else {
				line = styleSelectedDim.Render(line)
			}
		}
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

func (l *List) visible() int {
	if l.height <= 0 {
		return max(1, len(l.rows))
	}
	return l.height
}

func (l *List) clamp() {
	n := len(l.rows)
	l.cursor = min(max(l.cursor, 0), max(n-1, 0))
	h := l.visible()
	l.offset = min(max(l.offset, 0), max(n-h, 0))
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
}

// ListView is a titled list. Later views build on it; tests use it as a
// stand-in for real content.
type ListView struct {
	List
	TitleText string
	// Empty is shown when there are no rows.
	Empty string
	// OnOpen runs for enter or a double-click on row i.
	OnOpen func(i int) tea.Cmd
}

// NewListView returns a list view with rows.
func NewListView(title string, rows []string) *ListView {
	v := &ListView{TitleText: title}
	v.SetRows(rows)
	return v
}

// Title implements View.
func (v *ListView) Title() string { return v.TitleText }

// Render implements View.
func (v *ListView) Render(width, height int, focused bool) string {
	if v.Len() == 0 {
		return styleDim.Render(fit(v.Empty, width))
	}
	return v.List.Render(width, height, focused)
}

// Update implements View.
func (v *ListView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.HandleKey(msg) {
			return nil
		}
		if key.Matches(msg, Keys.Open) {
			return v.open(v.Cursor())
		}
	case ClickMsg:
		if i := v.RowAt(msg.Y); i >= 0 {
			v.Select(i)
			if msg.Double {
				return v.open(i)
			}
		}
	case WheelMsg:
		v.Scroll(msg.Delta)
	}
	return nil
}

func (v *ListView) open(i int) tea.Cmd {
	if v.OnOpen == nil || v.Len() == 0 {
		return nil
	}
	return v.OnOpen(i)
}

// Bindings implements View.
func (v *ListView) Bindings() []key.Binding { return nil }

// Capturing implements View.
func (v *ListView) Capturing() bool { return false }

// Placeholder is a section whose view has not been built yet.
type Placeholder struct {
	TitleText string
	Message   string
}

// Title implements View.
func (p *Placeholder) Title() string { return p.TitleText }

// Render implements View.
func (p *Placeholder) Render(width, _ int, _ bool) string {
	return styleDim.Render(fit(p.Message, width))
}

// Update implements View.
func (p *Placeholder) Update(tea.Msg) tea.Cmd { return nil }

// Bindings implements View.
func (p *Placeholder) Bindings() []key.Binding { return nil }

// Capturing implements View.
func (p *Placeholder) Capturing() bool { return false }

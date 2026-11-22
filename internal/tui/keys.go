package tui

import "charm.land/bubbles/v2/key"

// KeyMap holds the shell's bindings. It is the single source for key
// handling and for the help overlay, so the overlay always matches what
// the keys do.
type KeyMap struct {
	// Global.
	Quit     key.Binding
	ForceQ   key.Binding
	Help     key.Binding
	Search   key.Binding
	Sections key.Binding

	// Navigation.
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Top      key.Binding
	Bottom   key.Binding
	HalfDown key.Binding
	HalfUp   key.Binding
	Open     key.Binding
	Back     key.Binding

	// Item actions (catalog views).
	QueueAdd  key.Binding
	PlayNext  key.Binding
	NextGroup key.Binding
	PrevGroup key.Binding

	// Library actions.
	Favorite      key.Binding
	Unfavorite    key.Binding
	AddToPlaylist key.Binding
	Sort          key.Binding

	// Discovery actions (decision 43).
	Radio   key.Binding
	Similar key.Binding
}

// Keys is the default keymap (UI.md section 5).
var Keys = KeyMap{
	Quit:     key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	ForceQ:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit (even in input)")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle this help")),
	Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Sections: key.NewBinding(key.WithKeys("1", "2", "3", "4", "5"), key.WithHelp("1-5", "jump to section")),

	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "move up")),
	Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "move down")),
	Left:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "sidebar or back")),
	Right:    key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "main pane")),
	Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "jump to top")),
	Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "jump to bottom")),
	HalfDown: key.NewBinding(key.WithKeys("ctrl+d", "pgdown"), key.WithHelp("ctrl+d", "half page down")),
	HalfUp:   key.NewBinding(key.WithKeys("ctrl+u", "pgup"), key.WithHelp("ctrl+u", "half page up")),
	Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open or play")),
	Back:     key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back / leave input")),

	QueueAdd:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add to queue")),
	PlayNext:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "play next")),
	NextGroup: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next group")),
	PrevGroup: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous group")),

	Favorite:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "favorite")),
	Unfavorite:    key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "unfavorite")),
	AddToPlaylist: key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "add to playlist")),
	Sort:          key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "change sort")),

	Radio:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "radio")),
	Similar: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "similar")),
}

// helpGroup is a titled list of bindings in the help overlay.
type helpGroup struct {
	title    string
	bindings []key.Binding
}

func (k KeyMap) globalGroup() helpGroup {
	return helpGroup{"Global", []key.Binding{k.Help, k.Search, k.Sections, k.Quit, k.ForceQ}}
}

func (k KeyMap) navGroup() helpGroup {
	return helpGroup{"Navigation", []key.Binding{k.Up, k.Down, k.Left, k.Right, k.Top, k.Bottom, k.HalfDown, k.HalfUp, k.Open, k.Back}}
}

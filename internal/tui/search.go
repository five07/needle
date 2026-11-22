package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// SearchView is the Search section: a query input over a results area.
// Results arrive with TASK-010; until then the area shows a placeholder.
type SearchView struct {
	input textinput.Model
	// Body renders under the input (results). Nil shows the placeholder.
	Body View
}

// NewSearchView returns the Search section view.
func NewSearchView() *SearchView {
	in := textinput.New()
	in.Prompt = "Search: "
	in.Placeholder = "artists, albums, tracks, playlists"
	in.CharLimit = 200
	return &SearchView{input: in}
}

// Query returns the current input text.
func (s *SearchView) Query() string { return s.input.Value() }

// Title implements View.
func (s *SearchView) Title() string { return "Search" }

// Render implements View.
func (s *SearchView) Render(width, height int, focused bool) string {
	s.input.SetWidth(max(1, width-len(s.input.Prompt)-1))
	lines := []string{fit(s.input.View(), width), styleBorder.Render(strings.Repeat("─", width))}
	bodyH := max(0, height-2)
	if s.Body != nil {
		lines = append(lines, s.Body.Render(width, bodyH, focused && !s.input.Focused()))
	} else if bodyH > 0 {
		lines = append(lines, styleDim.Render(fit("Search is not available yet.", width)))
	}
	return strings.Join(lines, "\n")
}

// Update implements View.
func (s *SearchView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case FocusInputMsg:
		return s.input.Focus()
	case tea.KeyPressMsg:
		if s.input.Focused() {
			// esc leaves the input; backspace edits text, so it is not "back"
			// here. enter submits (TASK-010) and leaves the input.
			if k := msg.String(); k == "esc" || k == "enter" {
				s.input.Blur()
				return nil
			}
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return cmd
		}
		if s.Body != nil {
			return s.Body.Update(msg)
		}
	case ClickMsg:
		if msg.Y == 0 {
			return s.input.Focus()
		}
		s.input.Blur()
		if s.Body != nil && msg.Y >= 2 {
			msg.Y -= 2
			return s.Body.Update(msg)
		}
	case WheelMsg:
		if s.Body != nil {
			return s.Body.Update(msg)
		}
	default:
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return cmd
	}
	return nil
}

// Bindings implements View.
func (s *SearchView) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "search (in the input)")),
	}
}

// Capturing implements View.
func (s *SearchView) Capturing() bool { return s.input.Focused() }

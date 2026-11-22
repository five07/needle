package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/five07/tidal-tui/internal/tidal"
)

type searchState int

const (
	searchIdle searchState = iota
	searchLoading
	searchDone
	searchFailed
)

// debounceMsg fires SearchDebounce after an edit; typed identifies the edit.
type debounceMsg struct{ typed int }

// resultsMsg is a search response; req identifies the request.
type resultsMsg struct {
	req     int
	results tidal.SearchResults
	err     error
}

// SearchView is the Search section: a query input over grouped results.
// Typing searches after a pause (SearchDebounce); enter searches at once.
// Only the newest request's response is shown.
type SearchView struct {
	input textinput.Model
	svc   *Services

	typed   int // bumped on every edit; stale debounce ticks are ignored
	req     int // bumped per request; stale responses are dropped
	cancel  context.CancelFunc
	query   string // the query being searched or shown
	state   searchState
	errText string
	results entryList
}

// NewSearchView returns the Search section view. With nil services the
// view says search is unavailable.
func NewSearchView(svc *Services) *SearchView {
	in := textinput.New()
	in.Prompt = "Search: "
	in.Placeholder = "artists, albums, tracks, playlists"
	in.CharLimit = tidal.MaxQueryLen
	return &SearchView{input: in, svc: svc, results: newEntryList(svc)}
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
	if bodyH == 0 {
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	var body string
	switch {
	case s.svc == nil || s.svc.Catalog == nil:
		body = styleDim.Render(fit("Search is not available.", width))
	case s.state == searchFailed:
		body = styleDim.Render(fit("Search failed: "+s.errText, width))
	case len(s.results.entries) > 0:
		body = s.results.render(width, bodyH, focused && !s.input.Focused())
	case s.state == searchLoading:
		body = styleDim.Render(fit("Searching…", width))
	case s.state == searchDone:
		body = styleDim.Render(fit("No results for \""+Sanitize(s.query)+"\".", width))
	default:
		body = styleDim.Render(fit("Type to search. Press / from anywhere to come back here.", width))
	}
	return strings.Join(append(lines, body), "\n")
}

// Update implements View.
func (s *SearchView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case FocusInputMsg:
		return s.input.Focus()
	case debounceMsg:
		if msg.typed != s.typed {
			return nil // more typing followed
		}
		return s.start()
	case resultsMsg:
		return s.receive(msg)
	case tea.KeyPressMsg:
		if s.input.Focused() {
			return s.onInputKey(msg)
		}
		cmd, _ := s.results.update(msg)
		return cmd
	case ClickMsg:
		if msg.Y == 0 {
			return s.input.Focus()
		}
		s.input.Blur()
		if msg.Y >= 2 {
			msg.Y -= 2
			cmd, _ := s.results.update(msg)
			return cmd
		}
	case WheelMsg:
		cmd, _ := s.results.update(msg)
		return cmd
	default:
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return cmd
	}
	return nil
}

func (s *SearchView) onInputKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		// backspace edits text, so it is not "back" here.
		s.input.Blur()
		return nil
	case "enter":
		s.input.Blur()
		s.typed++ // a pending debounce is no longer needed
		return s.start()
	}
	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(k)
	if s.input.Value() == before {
		return cmd
	}
	return tea.Batch(cmd, s.edited())
}

// edited schedules a search for the new input after the debounce pause.
func (s *SearchView) edited() tea.Cmd {
	s.typed++
	q := strings.TrimSpace(s.input.Value())
	switch {
	case q == "":
		s.stop()
		s.query, s.state, s.errText = "", searchIdle, ""
		s.results.set(nil)
		return nil
	case q == s.query && s.state != searchFailed:
		return nil // back to what is already shown or loading
	case s.svc == nil:
		return nil
	}
	typed := s.typed
	return s.svc.tick(SearchDebounce, func(time.Time) tea.Msg {
		return ForViewMsg{View: s, Msg: debounceMsg{typed: typed}}
	})
}

// start searches for the current input unless that query is already shown
// or loading. It cancels the previous request.
func (s *SearchView) start() tea.Cmd {
	q := strings.TrimSpace(s.input.Value())
	if s.svc == nil || s.svc.Catalog == nil || q == "" || (q == s.query && s.state != searchFailed) {
		return nil
	}
	s.stop()
	s.req++
	s.query, s.state, s.errText = q, searchLoading, ""
	ctx, cancel := context.WithCancel(s.svc.ctx())
	s.cancel = cancel
	req, cat := s.req, s.svc.Catalog
	return To(s, func() tea.Msg {
		res, err := cat.Search(ctx, q)
		return resultsMsg{req: req, results: res, err: err}
	})
}

func (s *SearchView) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *SearchView) receive(m resultsMsg) tea.Cmd {
	if m.req != s.req {
		return nil // a newer search replaced this one
	}
	s.stop()
	if m.err != nil {
		if errors.Is(m.err, context.Canceled) {
			return nil
		}
		s.svc.logger().Warn("search failed", "query_len", len(s.query), "err", m.err)
		s.state, s.errText = searchFailed, userMessage(m.err)
		s.results.set(nil)
		return nil
	}
	s.state = searchDone
	s.results.set(groupResults(m.results))
	return nil
}

// groupResults lays out the results as Tracks, Albums, Artists, then
// Playlists, leaving out empty groups.
func groupResults(r tidal.SearchResults) []entry {
	var es []entry
	if len(r.Tracks) > 0 {
		es = append(es, headingEntry("Tracks", 0))
		es = append(es, trackEntries(r.Tracks, 0)...)
	}
	if len(r.Albums) > 0 {
		es = append(es, headingEntry("Albums", 1))
		es = append(es, albumEntries(r.Albums, 1)...)
	}
	if len(r.Artists) > 0 {
		es = append(es, headingEntry("Artists", 2))
		for _, a := range r.Artists {
			es = append(es, entry{kind: entryArtist, artist: a, group: 2})
		}
	}
	if len(r.Playlists) > 0 {
		es = append(es, headingEntry("Playlists", 3))
		for _, p := range r.Playlists {
			es = append(es, entry{kind: entryPlaylist, playlist: p, group: 3})
		}
	}
	return es
}

// Bindings implements View.
func (s *SearchView) Bindings() []key.Binding {
	if s.input.Focused() || len(s.results.entries) == 0 {
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "search (in the input)"))}
	}
	return s.results.bindings(true)
}

// FavoriteChanged implements favoriteWatcher.
func (s *SearchView) FavoriteChanged(FavoriteMsg) tea.Cmd {
	s.results.favoritesChanged()
	return nil
}

// Capturing implements View.
func (s *SearchView) Capturing() bool { return s.input.Focused() }

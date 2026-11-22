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

// resultTab is one kind of search result: Tracks, Albums, Artists, or
// Playlists.
type resultTab struct {
	title string
	list  entryList
}

// SearchView is the Search section: a query input over results in
// sub-tabs, one per kind (decision 40).
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
	tabs    []*resultTab // kinds with results, in order
	active  int
}

// NewSearchView returns the Search section view. With nil services the
// view says search is unavailable.
func NewSearchView(svc *Services) *SearchView {
	in := textinput.New()
	in.Prompt = "Search: "
	in.Placeholder = "artists, albums, tracks, playlists"
	in.CharLimit = tidal.MaxQueryLen
	return &SearchView{input: in, svc: svc}
}

// Query returns the current input text.
func (s *SearchView) Query() string { return s.input.Value() }

// Title implements View.
func (s *SearchView) Title() string { return "Search" }

// Render implements View.
func (s *SearchView) Render(width, height int, focused bool) string {
	s.input.SetWidth(max(1, width-len(s.input.Prompt)-1))
	lines := []string{fit(s.input.View(), width)}
	if len(s.tabs) > 0 {
		lines = append(lines, tabBar(s.titles(), s.active, width, ""))
	}
	lines = append(lines, styleBorder.Render(strings.Repeat("─", width)))
	bodyH := max(0, height-len(lines))
	if bodyH == 0 {
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	var body string
	switch {
	case s.svc == nil || s.svc.Catalog == nil:
		body = styleDim.Render(fit("Search is not available.", width))
	case s.state == searchFailed:
		body = styleDim.Render(fit("Search failed: "+s.errText, width))
	case len(s.tabs) > 0:
		body = s.tab().list.render(width, bodyH, focused && !s.input.Focused())
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
		return s.onResultKey(msg)
	case ClickMsg:
		if msg.Y == 0 {
			return s.input.Focus()
		}
		s.input.Blur()
		if len(s.tabs) == 0 {
			return nil
		}
		if msg.Y == 1 {
			if i := tabAt(s.titles(), msg.X); i >= 0 {
				s.active = i
			}
			return nil
		}
		if msg.Y >= 3 {
			msg.Y -= 3
			cmd, _ := s.tab().list.update(msg)
			return cmd
		}
	case WheelMsg:
		if len(s.tabs) == 0 {
			return nil
		}
		cmd, _ := s.tab().list.update(msg)
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
	case "tab", "shift+tab":
		// Decision 45: leave the input and switch tabs in one key.
		s.input.Blur()
		return s.onResultKey(k)
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
		s.tabs = nil
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
		s.tabs = nil
		return nil
	}
	s.state = searchDone
	s.tabs, s.active = resultTabs(s.svc, m.results), 0 // AC #2: the first tab with results
	return nil
}

// onResultKey handles keys outside the input: tab switching, then the
// item keys of the active tab.
func (s *SearchView) onResultKey(k tea.KeyPressMsg) tea.Cmd {
	if len(s.tabs) == 0 {
		return nil
	}
	switch {
	case key.Matches(k, Keys.NextTab):
		s.active = (s.active + 1) % len(s.tabs)
		return nil
	case key.Matches(k, Keys.PrevTab):
		s.active = (s.active - 1 + len(s.tabs)) % len(s.tabs)
		return nil
	}
	cmd, _ := s.tab().list.update(k)
	return cmd
}

func (s *SearchView) tab() *resultTab { return s.tabs[s.active] }

func (s *SearchView) titles() []string {
	out := make([]string, len(s.tabs))
	for i, t := range s.tabs {
		out[i] = t.title
	}
	return out
}

// resultTabs makes a tab for each kind with results, in the order Tracks,
// Albums, Artists, Playlists.
func resultTabs(svc *Services, r tidal.SearchResults) []*resultTab {
	kinds := []struct {
		title string
		es    []entry
	}{
		{"Tracks", trackEntries(r.Tracks)},
		{"Albums", albumEntries(r.Albums)},
		{"Artists", artistEntries(r.Artists)},
		{"Playlists", playlistEntries(r.Playlists)},
	}
	var tabs []*resultTab
	for _, k := range kinds {
		if len(k.es) == 0 {
			continue
		}
		t := &resultTab{title: k.title, list: newEntryList(svc)}
		t.list.set(k.es)
		tabs = append(tabs, t)
	}
	return tabs
}

func playlistEntries(ps []tidal.Playlist) []entry {
	out := make([]entry, len(ps))
	for i, p := range ps {
		out[i] = entry{kind: entryPlaylist, playlist: p}
	}
	return out
}

// Bindings implements View.
func (s *SearchView) Bindings() []key.Binding {
	if s.input.Focused() || len(s.tabs) == 0 {
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "search (in the input)"))}
	}
	bs := []key.Binding{ // the first searchLeadHints lead the hint line (decision 44)
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	}
	bs = append(bs, s.tab().list.bindings()...)
	return append(bs, key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous tab")))
}

const searchLeadHints = 2

// LeadHints implements HintLeader: search and tab switching come first,
// once there are results. (While typing, the shell shows input hints.)
func (s *SearchView) LeadHints() int {
	if len(s.tabs) == 0 {
		return 0
	}
	return searchLeadHints
}

// FavoriteChanged implements favoriteWatcher.
func (s *SearchView) FavoriteChanged(FavoriteMsg) tea.Cmd {
	for _, t := range s.tabs {
		t.list.favoritesChanged()
	}
	return nil
}

// Capturing implements View.
func (s *SearchView) Capturing() bool { return s.input.Focused() }

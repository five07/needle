package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/five07/tidal-tui/internal/tidal"
)

// MaxFilterItems caps how many items one Library tab loads in the
// background so a filter covers the whole list (decision 35).
const MaxFilterItems = 5000

// libFetch loads one page of a tab as entries.
type libFetch func(ctx context.Context, sort tidal.Sort, next string) ([]entry, string, error)

// libPageMsg is one loaded page of tab, for request generation gen.
type libPageMsg struct {
	tab, gen int
	entries  []entry
	next     string
	err      error
}

// libTab is one Library list: Playlists, Albums, Tracks, or Artists.
type libTab struct {
	title  string
	c      tidal.Collection
	empty  string
	sorts  []tidal.Sort
	sort   int
	fetch  libFetch
	all    []entry // loaded items, unfiltered, in server order
	list   entryList
	pg     pager
	gen    int  // bumped on reload; pages of older loads are dropped
	opened bool // the first page was requested
	capped bool // the filter stopped loading at MaxFilterItems
}

// LibraryView is the Library section: the user's playlists and favorite
// albums, tracks, and artists in sub-tabs (decision 34), loaded page by
// page as the cursor nears the end, with a client-side filter on "/".
type LibraryView struct {
	svc    *Services
	tabs   []*libTab
	active int
	filter textinput.Model
}

// NewLibraryView returns the Library section view.
func NewLibraryView(svc *Services) *LibraryView {
	lib := svc.Library
	in := textinput.New()
	in.Prompt = "Filter: "
	in.Placeholder = "title, artist, or album"
	in.CharLimit = tidal.MaxQueryLen
	v := &LibraryView{svc: svc, filter: in}
	v.tabs = []*libTab{
		{title: "Playlists", c: tidal.FavPlaylists,
			empty: "No playlists yet. Playlists you create or add in TIDAL appear here.",
			fetch: func(ctx context.Context, s tidal.Sort, next string) ([]entry, string, error) {
				p, err := lib.FavoritePlaylists(ctx, s, next)
				es := make([]entry, len(p.Items))
				for i, x := range p.Items {
					es[i] = entry{kind: entryPlaylist, playlist: x}
				}
				return es, p.Next, err
			}},
		{title: "Albums", c: tidal.FavAlbums,
			empty: "No favorite albums yet. Press f on an album in Search to add one.",
			fetch: func(ctx context.Context, s tidal.Sort, next string) ([]entry, string, error) {
				p, err := lib.FavoriteAlbums(ctx, s, next)
				return albumEntries(p.Items, 0), p.Next, err
			}},
		{title: "Tracks", c: tidal.FavTracks,
			empty: "No favorite tracks yet. Press f on a track to add it.",
			fetch: func(ctx context.Context, s tidal.Sort, next string) ([]entry, string, error) {
				p, err := lib.FavoriteTracks(ctx, s, next)
				return trackEntries(p.Items, 0), p.Next, err
			}},
		{title: "Artists", c: tidal.FavArtists,
			empty: "No favorite artists yet. Press f on an artist in Search to add one.",
			fetch: func(ctx context.Context, s tidal.Sort, next string) ([]entry, string, error) {
				p, err := lib.FavoriteArtists(ctx, s, next)
				es := make([]entry, len(p.Items))
				for i, x := range p.Items {
					es[i] = entry{kind: entryArtist, artist: x}
				}
				return es, p.Next, err
			}},
	}
	for _, t := range v.tabs {
		t.sorts = tidal.Sorts(t.c)
		t.list = newEntryList(svc)
	}
	return v
}

func (v *LibraryView) tab() *libTab { return v.tabs[v.active] }

// Open implements Opener: it loads the first tab.
func (v *LibraryView) Open() tea.Cmd { return v.ensureLoaded() }

func (v *LibraryView) ensureLoaded() tea.Cmd {
	t := v.tab()
	if t.opened {
		return nil
	}
	t.opened = true
	return v.load(v.active)
}

// load requests the next page of tab i.
func (v *LibraryView) load(i int) tea.Cmd {
	t := v.tabs[i]
	if t.pg.loading {
		return nil
	}
	t.pg.loading = true
	gen, next, sort, fetch, ctx := t.gen, t.pg.next, t.sorts[t.sort], t.fetch, v.svc.ctx()
	return To(v, func() tea.Msg {
		es, n, err := fetch(ctx, sort, next)
		return libPageMsg{tab: i, gen: gen, entries: es, next: n, err: err}
	})
}

// reload drops what tab i has loaded and loads it again from the start.
func (v *LibraryView) reload(i int) tea.Cmd {
	t := v.tabs[i]
	t.gen++
	t.all, t.pg, t.capped = nil, pager{}, false
	t.list.set(nil)
	t.opened = true
	return v.load(i)
}

// Title implements View.
func (v *LibraryView) Title() string { return "Library" }

var sortNames = map[tidal.Sort]string{
	tidal.SortAdded: "date added", tidal.SortTitle: "title", tidal.SortName: "name", tidal.SortArtist: "artist",
}

func (v *LibraryView) titles() []string {
	out := make([]string, len(v.tabs))
	for i, t := range v.tabs {
		out[i] = t.title
	}
	return out
}

func (v *LibraryView) filtering() bool { return v.filter.Focused() || v.filter.Value() != "" }

// Render implements View.
func (v *LibraryView) Render(width, height int, focused bool) string {
	bar := tabBar(v.titles(), v.active, width, "sort: "+sortNames[v.tab().sorts[v.tab().sort]])
	second := styleBorder.Render(strings.Repeat("─", width))
	if v.filtering() {
		v.filter.SetWidth(max(1, width-len(v.filter.Prompt)-1))
		second = fit(v.filter.View(), width)
	}
	head := []string{bar, second}
	t := v.tab()
	bodyH := max(height-len(head), 0)
	var body string
	switch {
	case t.pg.errText != "" && len(t.all) == 0:
		body = styleDim.Render(fit("Could not load "+strings.ToLower(t.title)+": "+t.pg.errText, width))
	case len(t.list.entries) > 0:
		note := ""
		switch {
		case t.pg.errText != "":
			note = "Could not load more: " + t.pg.errText
		case v.filtering() && t.pg.loading:
			note = fmt.Sprintf("Filtering %d loaded items, loading more…", len(t.all))
		}
		if note != "" && bodyH > 1 {
			body = t.list.render(width, bodyH-1, focused && !v.filter.Focused()) + "\n" + styleDim.Render(fit(note, width))
		} else {
			body = t.list.render(width, bodyH, focused && !v.filter.Focused())
		}
	case !t.pg.done:
		body = styleDim.Render(fit("Loading…", width))
	case v.filtering() && len(t.all) > 0:
		more := ""
		if t.pg.loading {
			more = " Loading more…"
		}
		body = styleDim.Render(fit("No matches for \""+Sanitize(v.filter.Value())+"\"."+more, width))
	default:
		body = styleDim.Render(fit(t.empty, width))
	}
	return strings.Join(append(head[:min(len(head), height)], body), "\n")
}

// Update implements View.
func (v *LibraryView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case libPageMsg:
		return v.received(msg)
	case tea.KeyPressMsg:
		if v.filter.Focused() {
			return v.onFilterKey(msg)
		}
		return v.onKey(msg)
	case ClickMsg:
		v.filter.Blur()
		if msg.Y == 0 {
			if i := tabAt(v.titles(), msg.X); i >= 0 {
				return v.switchTab(i - v.active)
			}
			return nil
		}
		if msg.Y >= 2 {
			msg.Y -= 2
			cmd, _ := v.tab().list.update(msg)
			return tea.Batch(cmd, v.more())
		}
	case WheelMsg:
		cmd, _ := v.tab().list.update(msg)
		return tea.Batch(cmd, v.more())
	default:
		var cmd tea.Cmd
		v.filter, cmd = v.filter.Update(msg)
		return cmd
	}
	return nil
}

func (v *LibraryView) onKey(k tea.KeyPressMsg) tea.Cmd {
	t := v.tab()
	switch {
	case key.Matches(k, Keys.NextGroup):
		return v.switchTab(1)
	case key.Matches(k, Keys.PrevGroup):
		return v.switchTab(-1)
	case key.Matches(k, Keys.Sort):
		t.sort = (t.sort + 1) % len(t.sorts)
		return tea.Batch(v.reload(v.active), Notify(t.title+" sorted by "+sortNames[t.sorts[t.sort]]))
	case k.String() == "esc" && v.filter.Value() != "":
		v.filter.SetValue("")
		v.applyFilter()
		return nil
	}
	cmd, _ := t.list.update(k)
	return tea.Batch(cmd, v.more())
}

func (v *LibraryView) onFilterKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "enter":
		v.filter.Blur()
		if k.String() == "esc" {
			v.filter.SetValue("")
			v.applyFilter()
		}
		return nil
	}
	before := v.filter.Value()
	var cmd tea.Cmd
	v.filter, cmd = v.filter.Update(k)
	if v.filter.Value() == before {
		return cmd
	}
	v.applyFilter()
	return tea.Batch(cmd, v.more())
}

// StartFilter implements Filterer: "/" focuses the filter of this tab.
func (v *LibraryView) StartFilter() tea.Cmd { return v.filter.Focus() }

func (v *LibraryView) switchTab(delta int) tea.Cmd {
	v.active = (v.active + delta + len(v.tabs)) % len(v.tabs)
	v.applyFilter() // the filter applies to whichever tab is shown
	return tea.Batch(v.ensureLoaded(), v.more())
}

// applyFilter shows the loaded items of the active tab that match the
// filter.
func (v *LibraryView) applyFilter() {
	t := v.tab()
	t.list.set(v.matching(t.all))
}

func (v *LibraryView) matching(es []entry) []entry {
	q := fold(strings.TrimSpace(v.filter.Value()))
	if q == "" {
		return append([]entry(nil), es...) // the list never shares t.all's array
	}
	var out []entry
	for _, en := range es {
		if strings.Contains(fold(entryText(en)), q) {
			out = append(out, en)
		}
	}
	return out
}

// more loads the next page of the active tab when the cursor nears the end
// or, while filtering, until the tab is complete or capped.
func (v *LibraryView) more() tea.Cmd {
	t := v.tab()
	if !t.opened || t.pg.done && t.pg.next == "" {
		return nil
	}
	if v.filter.Value() != "" {
		if t.pg.errText != "" || t.pg.next == "" || t.capped {
			return nil
		}
		if len(t.all) >= MaxFilterItems {
			t.capped = true
			return Notify(fmt.Sprintf("Filtering the first %d %s only.", MaxFilterItems, strings.ToLower(t.title)))
		}
		return v.load(v.active)
	}
	if t.pg.want(t.list.Cursor(), len(t.list.entries)) {
		return v.load(v.active)
	}
	return nil
}

func (v *LibraryView) received(m libPageMsg) tea.Cmd {
	if m.tab < 0 || m.tab >= len(v.tabs) {
		return nil
	}
	t := v.tabs[m.tab]
	if m.gen != t.gen {
		return nil // the tab was reloaded (new sort) meanwhile
	}
	if !t.pg.received(v.svc, strings.ToLower(t.title), m.err, m.next) {
		return nil
	}
	for _, en := range m.entries {
		if c, id, _, ok := favRef(en); ok {
			v.svc.favs().set(c, id, true)
		}
	}
	t.all = append(t.all, m.entries...)
	if m.tab == v.active {
		t.list.add(v.matching(m.entries)...)
		return v.more()
	}
	t.list.add(m.entries...)
	return nil
}

// FavoriteChanged implements favoriteWatcher: a removed favorite leaves
// its list at once; an added one reloads its tab if it was loaded.
func (v *LibraryView) FavoriteChanged(m FavoriteMsg) tea.Cmd {
	var cmds []tea.Cmd
	for i, t := range v.tabs {
		t.list.favoritesChanged()
		if t.c != m.Collection || !t.opened {
			continue
		}
		if m.Added {
			cmds = append(cmds, v.reload(i))
			continue
		}
		keep := t.all[:0]
		for _, en := range t.all {
			if _, id, _, _ := favRef(en); id != m.ID {
				keep = append(keep, en)
			}
		}
		t.all = keep
		cur := t.list.Cursor()
		if i == v.active {
			t.list.set(v.matching(t.all))
		} else {
			t.list.set(t.all)
		}
		t.list.Select(cur)
		t.list.skip(1)
	}
	return tea.Batch(cmds...)
}

// Bindings implements View.
func (v *LibraryView) Bindings() []key.Binding {
	bs := []key.Binding{ // the first libLeadHints lead the hint line
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	}
	bs = append(bs, v.tab().list.bindings(false)...)
	return append(bs, Keys.Sort, key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous tab")))
}

const libLeadHints = 2

// LeadHints implements HintLeader: filter and tab switching come first.
func (v *LibraryView) LeadHints() int { return libLeadHints }

// Capturing implements View.
func (v *LibraryView) Capturing() bool { return v.filter.Focused() }

// entryText is the text the filter matches: title, artists, and album.
func entryText(en entry) string {
	switch en.kind {
	case entryTrack:
		return en.track.FullTitle() + " " + strings.Join(en.track.Artists, " ") + " " + en.track.Album
	case entryAlbum:
		return en.album.Title + " " + strings.Join(en.album.Artists, " ")
	case entryArtist:
		return en.artist.Name
	case entryPlaylist:
		return en.playlist.Name
	}
	return ""
}

// accentFold maps accented Latin letters to their base letter.
var accentFold = func() map[rune]rune {
	m := map[rune]rune{}
	for base, accented := range map[rune]string{
		'a': "àáâãäåāăą", 'c': "çćĉċč", 'd': "ďđ", 'e': "èéêëēĕėęě", 'g': "ĝğġģ", 'h': "ĥħ",
		'i': "ìíîïĩīĭįı", 'j': "ĵ", 'k': "ķ", 'l': "ĺļľŀł", 'n': "ñńņňŉ", 'o': "òóôõöøōŏő",
		'r': "ŕŗř", 's': "śŝşšß", 't': "ţťŧ", 'u': "ùúûüũūŭůűų", 'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	} {
		for _, r := range accented {
			m[r] = base
		}
	}
	return m
}()

// fold lowercases s and strips accents, for matching.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		if b, ok := accentFold[r]; ok {
			return b
		}
		return r
	}, strings.ToLower(s))
}

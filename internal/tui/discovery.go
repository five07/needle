package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/five07/needle/internal/tidal"
)

// MixMaxAge is how old the mixes may get before a visit to Discovery loads
// them again (decision 41). TIDAL updates Daily and Discovery mixes every
// morning.
const MixMaxAge = time.Hour

// mixKinds are the Discovery tabs, in order.
var mixKinds = []struct {
	kind  tidal.MixKind
	title string
}{
	{tidal.DailyMixes, "Daily"},
	{tidal.DiscoveryMixes, "Discovery"},
	{tidal.NewReleaseMixes, "New Releases"},
}

// mixesMsg carries every mix list of one load (generation gen).
type mixesMsg struct {
	gen   int
	mixes [][]tidal.Playlist // per mixKinds index
	errs  []error
}

// mixTab is one non-empty mix list.
type mixTab struct {
	kind  tidal.MixKind
	title string
	list  entryList
}

// DiscoveryView is the Discovery section: the user's Daily, Discovery, and
// New Release mixes in sub-tabs (decision 40). Kinds with no mixes get no
// tab (AC #5). Opening a mix shows its tracks.
type DiscoveryView struct {
	svc      *Services
	tabs     []*mixTab
	active   int
	gen      int
	loading  bool
	loadedAt time.Time
	errText  string // set when no list could be loaded
}

// NewDiscoveryView returns the Discovery section view.
func NewDiscoveryView(svc *Services) *DiscoveryView { return &DiscoveryView{svc: svc} }

// Open implements Opener: it loads the mixes.
func (v *DiscoveryView) Open() tea.Cmd { return v.load() }

// Revisit implements Revisiter: mixes older than MixMaxAge load again.
func (v *DiscoveryView) Revisit() tea.Cmd {
	if v.loading || v.loadedAt.IsZero() || v.svc.now().Sub(v.loadedAt) <= MixMaxAge {
		return nil
	}
	return v.load()
}

func (v *DiscoveryView) load() tea.Cmd {
	if v.loading {
		return nil
	}
	v.loading = true
	v.gen++
	gen, disc, ctx := v.gen, v.svc.Discovery, v.svc.ctx()
	return To(v, func() tea.Msg {
		m := mixesMsg{gen: gen, mixes: make([][]tidal.Playlist, len(mixKinds)), errs: make([]error, len(mixKinds))}
		for i, k := range mixKinds {
			m.mixes[i], m.errs[i] = disc.Mixes(ctx, k.kind)
		}
		return m
	})
}

func (v *DiscoveryView) received(m mixesMsg) tea.Cmd {
	if m.gen != v.gen {
		return nil
	}
	v.loading = false
	v.loadedAt = v.svc.now()
	activeKind := tidal.MixKind("")
	if t := v.tab(); t != nil {
		activeKind = t.kind
	}
	var tabs []*mixTab
	var failed []string
	var lastErr error
	for i, k := range mixKinds {
		if err := m.errs[i]; err != nil {
			v.svc.logger().Warn("load mixes failed", "kind", string(k.kind), "err", err)
			failed, lastErr = append(failed, k.title), err
			if old := v.find(k.kind); old != nil {
				tabs = append(tabs, old) // keep what was shown before
			}
			continue
		}
		if len(m.mixes[i]) == 0 {
			continue // decision 40: empty kinds get no tab
		}
		t := v.find(k.kind)
		if t == nil {
			t = &mixTab{kind: k.kind, title: k.title, list: newEntryList(v.svc)}
		}
		cur := t.list.Cursor()
		es := make([]entry, len(m.mixes[i]))
		for j, p := range m.mixes[i] {
			es[j] = entry{kind: entryPlaylist, playlist: p}
		}
		t.list.set(es)
		t.list.Select(cur)
		tabs = append(tabs, t)
	}
	v.tabs, v.active, v.errText = tabs, 0, ""
	for i, t := range tabs {
		if t.kind == activeKind {
			v.active = i
		}
	}
	switch {
	case len(failed) == 0:
		return nil
	case len(tabs) == 0:
		v.errText = userMessage(lastErr)
		return nil
	}
	return Notify("Could not load " + strings.Join(failed, " and ") + " mixes: " + userMessage(lastErr))
}

func (v *DiscoveryView) find(k tidal.MixKind) *mixTab {
	for _, t := range v.tabs {
		if t.kind == k {
			return t
		}
	}
	return nil
}

func (v *DiscoveryView) tab() *mixTab {
	if v.active < 0 || v.active >= len(v.tabs) {
		return nil
	}
	return v.tabs[v.active]
}

func (v *DiscoveryView) titles() []string {
	out := make([]string, len(v.tabs))
	for i, t := range v.tabs {
		out[i] = t.title
	}
	return out
}

// Title implements View.
func (v *DiscoveryView) Title() string { return "Discovery" }

// Render implements View.
func (v *DiscoveryView) Render(width, height int, focused bool) string {
	t := v.tab()
	var body string
	switch {
	case v.errText != "":
		body = styleDim.Render(fit("Could not load your mixes: "+v.errText, width))
	case t == nil && v.loadedAt.IsZero():
		body = styleDim.Render(fit("Loading your mixes…", width))
	case t == nil:
		body = styleDim.Render(fit("No mixes yet. TIDAL builds them as you listen.", width))
	default:
		head := []string{tabBar(v.titles(), v.active, width, ""), styleBorder.Render(strings.Repeat("─", width))}
		body = t.list.render(width, max(height-len(head), 0), focused)
		return strings.Join(append(head[:min(len(head), height)], body), "\n")
	}
	return body
}

// Update implements View.
func (v *DiscoveryView) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(mixesMsg); ok {
		return v.received(m)
	}
	t := v.tab()
	if t == nil {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, Keys.NextTab):
			v.active = (v.active + 1) % len(v.tabs)
			return nil
		case key.Matches(msg, Keys.PrevTab):
			v.active = (v.active - 1 + len(v.tabs)) % len(v.tabs)
			return nil
		}
		cmd, _ := t.list.update(msg)
		return cmd
	case ClickMsg:
		if msg.Y == 0 {
			if i := tabAt(v.titles(), msg.X); i >= 0 {
				v.active = i
			}
			return nil
		}
		if msg.Y >= 2 {
			msg.Y -= 2
			cmd, _ := t.list.update(msg)
			return cmd
		}
	case WheelMsg:
		cmd, _ := t.list.update(msg)
		return cmd
	}
	return nil
}

// FavoriteChanged implements favoriteWatcher.
func (v *DiscoveryView) FavoriteChanged(FavoriteMsg) tea.Cmd {
	for _, t := range v.tabs {
		t.list.favoritesChanged()
	}
	return nil
}

// Bindings implements View.
func (v *DiscoveryView) Bindings() []key.Binding {
	t := v.tab()
	if t == nil {
		return nil
	}
	bs := []key.Binding{key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab"))}
	bs = append(bs, t.list.bindings()...)
	return append(bs, key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous tab")))
}

// LeadHints implements HintLeader: tab switching comes first.
func (v *DiscoveryView) LeadHints() int {
	if v.tab() == nil {
		return 0
	}
	return 1
}

// Capturing implements View.
func (v *DiscoveryView) Capturing() bool { return false }

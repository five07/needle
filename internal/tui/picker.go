package tui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/five07/needle/internal/tidal"
)

// maxPickerPlaylists caps how many owned playlists the picker loads.
const maxPickerPlaylists = 500

type ownedMsg struct {
	playlists []tidal.Playlist
	err       error
}

type addedMsg struct {
	playlist tidal.Playlist
	err      error
}

// PlaylistPicker lists the playlists the user owns, to add one track to.
// It opens over the current view: enter adds, esc cancels.
type PlaylistPicker struct {
	svc       *Services
	track     tidal.Track
	list      List
	playlists []tidal.Playlist
	loaded    bool
	adding    bool
	errText   string
}

// NewPlaylistPicker returns the picker for track t.
func NewPlaylistPicker(svc *Services, t tidal.Track) *PlaylistPicker {
	return &PlaylistPicker{svc: svc, track: t}
}

// Open implements Opener: it loads the owned playlists.
func (p *PlaylistPicker) Open() tea.Cmd {
	lib, ctx := p.svc.Library, p.svc.ctx()
	return To(p, func() tea.Msg {
		var all []tidal.Playlist
		next := ""
		for {
			page, err := lib.OwnedPlaylists(ctx, next)
			if err != nil {
				return ownedMsg{err: err}
			}
			all = append(all, page.Items...)
			if page.Next == "" || len(all) >= maxPickerPlaylists {
				return ownedMsg{playlists: all[:min(len(all), maxPickerPlaylists)]}
			}
			next = page.Next
		}
	})
}

// Title implements View.
func (p *PlaylistPicker) Title() string { return "Add to playlist" }

// Render implements View.
func (p *PlaylistPicker) Render(width, height int, focused bool) string {
	head := []string{
		styleTitle.Render(fit("Add to playlist: "+Sanitize(p.track.FullTitle()), width)),
		styleBorder.Render(strings.Repeat("─", width)),
	}
	var body string
	switch {
	case p.errText != "":
		body = styleDim.Render(fit("Could not load your playlists: "+p.errText, width))
	case !p.loaded:
		body = styleDim.Render(fit("Loading your playlists…", width))
	case len(p.playlists) == 0:
		body = styleDim.Render(fit("You have no playlists yet. Create one in TIDAL, then try again.", width))
	default:
		body = p.list.Render(width, max(height-len(head), 0), focused)
	}
	return strings.Join(append(head[:min(len(head), height)], body), "\n")
}

// Update implements View.
func (p *PlaylistPicker) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ownedMsg:
		p.loaded = true
		if msg.err != nil {
			p.svc.logger().Warn("load owned playlists failed", "err", msg.err)
			p.errText = userMessage(msg.err)
			return nil
		}
		p.playlists = msg.playlists
		rows := make([]string, len(msg.playlists))
		for i, pl := range msg.playlists {
			rows[i] = indent + joinMeta(pl.Name, countTracks(pl.Tracks)) // SetRows strips escapes
		}
		p.list.SetRows(rows)
	case addedMsg:
		p.adding = false
		return p.added(msg)
	case tea.KeyPressMsg:
		if p.list.HandleKey(msg) {
			return nil
		}
		if key.Matches(msg, Keys.Open) {
			return p.add(p.list.Cursor())
		}
	case ClickMsg:
		if i := p.list.RowAt(msg.Y - 2); i >= 0 {
			p.list.Select(i)
			if msg.Double {
				return p.add(i)
			}
		}
	case WheelMsg:
		p.list.Scroll(msg.Delta)
	}
	return nil
}

func (p *PlaylistPicker) add(i int) tea.Cmd {
	if p.adding || i < 0 || i >= len(p.playlists) {
		return nil
	}
	p.adding = true
	pl, trackID, lib, ctx := p.playlists[i], p.track.ID, p.svc.Library, p.svc.ctx()
	return To(p, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, favoriteTimeout)
		defer cancel()
		return addedMsg{playlist: pl, err: lib.AddToPlaylist(ctx, pl.ID, trackID)}
	})
}

// added reports the result and closes the picker, except after an error
// that a retry could fix.
func (p *PlaylistPicker) added(m addedMsg) tea.Cmd {
	title := p.track.FullTitle()
	switch {
	case m.err == nil:
		return tea.Batch(Notify("Added "+title+" to "+m.playlist.Name), p.close)
	case errors.Is(m.err, tidal.ErrDuplicateInPlaylist):
		return tea.Batch(Notify(title+" is already in "+m.playlist.Name), p.close)
	}
	p.svc.logger().Warn("add to playlist failed", "track_id", p.track.ID, "playlist_id", m.playlist.ID, "err", m.err)
	return Notify("Could not add to " + m.playlist.Name + ": " + userMessage(m.err))
}

func (p *PlaylistPicker) close() tea.Msg { return CloseMsg{View: p} }

// Bindings implements View.
func (p *PlaylistPicker) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "add to this playlist")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// Capturing implements View.
func (p *PlaylistPicker) Capturing() bool { return false }

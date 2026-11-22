package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/five07/needle/internal/visualizer"
)

// Visualizer turns the spectrum visualizer on and off.
// *visualizer.Controller implements it.
type Visualizer interface {
	On() bool
	SetOn(on bool)
}

// SpectrumMsg carries a new set of bar levels.
type SpectrumMsg struct{ Frame visualizer.Frame }

// themeColors are the named visualizer themes, darkest (bottom) to
// brightest (top) (decision 69). "terminal" uses the terminal's palette,
// so it follows the terminal's theme.
var themeColors = map[string][4]string{
	"burnt orange": {"#5a2a0a", "#8a3f12", "#c4561a", "#f08a3c"},
	"ice blue":     {"#0b2a3f", "#12527a", "#2f8fc4", "#8fd3f5"},
	"forest green": {"#0f2b14", "#1d5a28", "#33913f", "#7fd18a"},
	"royal purple": {"#2a1245", "#4b2283", "#7440c4", "#b38cf0"},
	"deep red":     {"#3a0808", "#6e1111", "#a81d1d", "#e85454"},
}

// themeStyles returns the 4 gradient steps of a theme, bottom to top. An
// unknown theme, or "custom" without 4 colors, falls back to "terminal".
func themeStyles(theme string, custom []string) [4]lipgloss.Style {
	s := lipgloss.NewStyle()
	colors, ok := themeColors[theme]
	if theme == "custom" && len(custom) == 4 {
		colors, ok = [4]string{custom[0], custom[1], custom[2], custom[3]}, true
	}
	if !ok {
		return [4]lipgloss.Style{
			s.Foreground(lipgloss.Color("4")).Faint(true),
			s.Foreground(lipgloss.Color("4")),
			s.Foreground(lipgloss.Color("12")),
			s.Foreground(lipgloss.Color("14")).Bold(true),
		}
	}
	var out [4]lipgloss.Style
	for i, c := range colors {
		out[i] = s.Foreground(lipgloss.Color(c))
	}
	return out
}

// rowStep maps the bar's rows, bottom first, to gradient steps: 1, 1, 2,
// 3, 4, 4, so each step covers at least one row (decision 69).
var rowStep = [BarHeight]int{0, 0, 1, 2, 3, 3}

// eighths are the partial block characters, empty to full.
var eighths = []rune(" ▁▂▃▄▅▆▇█")

// spectrumGlyph is the character for column x (of w) on row fromBottom:
// the band's level fills BarHeight rows in eighths.
func spectrumGlyph(f visualizer.Frame, x, w, fromBottom int) rune {
	lv := min(max(f[x*visualizer.Bands/w], 0), 1)
	fill := int(lv*BarHeight*8+0.5) - fromBottom*8
	return eighths[min(max(fill, 0), 8)]
}

// withSpectrum draws the bars into the free cells of the now-playing rows:
// the cells left and right of each row's text, keeping one blank cell
// next to the text, so text is never covered (decision 68).
func withSpectrum(rows []string, f visualizer.Frame, steps [4]lipgloss.Style, w int) []string {
	if f == (visualizer.Frame{}) || w <= 0 {
		return rows
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		fromBottom := len(rows) - 1 - i
		plain := ansi.Strip(row)
		trimmed := strings.TrimSpace(plain)
		lead, trail := w, 0 // an empty row is all free
		if trimmed != "" {
			lead = len(plain) - len(strings.TrimLeft(plain, " "))
			trail = len(plain) - len(strings.TrimRight(plain, " "))
			lead, trail = max(lead-1, 0), max(trail-1, 0) // the margin
		}
		run := func(x0, x1 int) string {
			if x1 <= x0 {
				return ""
			}
			var b strings.Builder
			for x := x0; x < x1; x++ {
				b.WriteRune(spectrumGlyph(f, x, w, fromBottom))
			}
			return steps[rowStep[fromBottom]].Render(b.String())
		}
		if trimmed == "" {
			out[i] = run(0, w)
			continue
		}
		out[i] = run(0, lead) + ansi.Cut(row, lead, w-trail) + run(w-trail, w)
	}
	return out
}

// listenSpectrum waits for the next frame; nil without a channel.
func (m *Model) listenSpectrum() tea.Cmd {
	if m.spectrumCh == nil {
		return nil
	}
	ch := m.spectrumCh
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return nil
		}
		return SpectrumMsg{f}
	}
}

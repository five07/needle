package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	appName   = "tidal-tui"
	headerKey = " ? help ─ q quit "
)

// render draws the whole screen for the current layout.
func (m *Model) render() string {
	l := m.layout
	if l.TooSmall {
		msg := fmt.Sprintf("Terminal too small (needs %dx%d)", MinWidth, MinHeight)
		return strings.Join(block(placeMiddle(msg, l.Width, l.Height), l.Width, l.Height), "\n")
	}
	w := l.Width
	b := func(s string) string { return styleBorder.Render(s) }
	leftBorder, midBorder, rightBorder := b("│"), b("│"), b("│")
	if m.focus == paneSidebar {
		leftBorder, midBorder = styleFocusBorder.Render("│"), styleFocusBorder.Render("│")
	} else {
		midBorder, rightBorder = styleFocusBorder.Render("│"), styleFocusBorder.Render("│")
	}

	rows := make([]string, 0, l.Height)
	rows = append(rows, m.header(w))
	if m.help {
		// The overlay covers the panes and the bar, keeping the frame.
		h := l.Height - 3
		for _, line := range m.helpLines(w-2, h) {
			rows = append(rows, b("│")+line+b("│"))
		}
	} else {
		side := m.sidebarLines(l.Sidebar)
		main := block(m.current().Render(l.Main.W-2, l.Main.H, m.focus == paneMain), l.Main.W-2, l.Main.H)
		for i := range l.Main.H {
			rows = append(rows, leftBorder+side[i]+midBorder+" "+main[i]+" "+rightBorder)
		}
		rows = append(rows, b("├"+strings.Repeat("─", l.Sidebar.W)+"┴"+strings.Repeat("─", l.Main.W)+"┤"))
		for _, line := range barLines(l.Bar.W) {
			rows = append(rows, b("│")+line+b("│"))
		}
		rows = append(rows, b("├"+strings.Repeat("─", w-2)+"┤"))
	}
	rows = append(rows, b("│")+m.hintLine(l.Hints.W)+b("│"))
	rows = append(rows, b("└"+strings.Repeat("─", w-2)+"┘"))
	return strings.Join(rows, "\n")
}

// header is the top border with the app name and the help/quit keys.
func (m *Model) header(w int) string {
	// MinWidth leaves room for both labels.
	fill := w - len("┌ ") + 2 - len(appName) - 1 - ansi.StringWidth(headerKey) - 1
	return styleBorder.Render("┌ ") + styleTitle.Render(appName) + styleBorder.Render(" "+strings.Repeat("─", fill)+headerKey+"┐")
}

// sidebarLines draws the section list, full labels or the letter strip.
func (m *Model) sidebarLines(r Rect) []string {
	out := make([]string, r.H)
	for i := range out {
		if i >= int(sectionCount) {
			out[i] = strings.Repeat(" ", r.W)
			continue
		}
		label := m.stacks[i][0].Title()
		var line string
		switch {
		case m.layout.Compact:
			line = center(string([]rune(label)[:1]), r.W)
		case Section(i) == m.section:
			line = fit(" ▸ "+label, r.W)
		default:
			line = fit("   "+label, r.W)
		}
		if Section(i) == m.section {
			if m.focus == paneSidebar {
				line = styleSelected.Render(line)
			} else {
				line = styleSelectedDim.Render(line)
			}
		}
		out[i] = line
	}
	return out
}

// barLines draws the now-playing bar. Until the player is connected
// (TASK-013) it shows the idle state, at full height (decision 23).
func barLines(w int) []string {
	out := make([]string, BarHeight)
	for i := range out {
		out[i] = strings.Repeat(" ", w)
	}
	out[2] = styleDim.Render(center("Nothing playing", w))
	return out
}

// hintLine shows a notice, or the keys for the focused pane.
func (m *Model) hintLine(w int) string {
	if m.notice != "" {
		return styleNotice.Render(center(m.notice, w))
	}
	var hints []key.Binding
	switch {
	case m.help:
		hints = []key.Binding{hint("esc", "close help")}
		if m.helpLong {
			hints = append([]key.Binding{hint("j/k", "scroll")}, hints...)
		}
	case m.focus == paneMain && m.current().Capturing():
		hints = []key.Binding{hint("enter", "search"), hint("esc", "leave input"), hint("ctrl+c", "quit")}
	case m.focus == paneSidebar:
		hints = []key.Binding{hint("j/k", "move"), hint("enter", "open"), hint("/", "search"), hint("?", "help"), hint("q", "quit")}
	default:
		hints = append(hints, hint("j/k", "move"), hint("h", "back"))
		hints = append(hints, m.current().Bindings()...)
		hints = append(hints, hint("/", "search"), hint("?", "help"), hint("q", "quit"))
	}
	// Whole hints only: the ones that do not fit are left out (the help
	// overlay lists them all).
	line, used := " ", 1
	for i, h := range hints {
		part := h.Help().Key + " " + h.Help().Desc
		sep := ""
		if i > 0 {
			sep = " · "
		}
		if used+len(sep)+ansi.StringWidth(part) > w && i > 0 {
			break
		}
		line += sep + styleKey.Render(h.Help().Key) + " " + h.Help().Desc
		used += len(sep) + ansi.StringWidth(part)
	}
	return fit(line, w)
}

// helpLines draws the help overlay: global, navigation, and the current
// view's bindings, centered in w x h.
func (m *Model) helpLines(w, h int) []string {
	groups := []helpGroup{Keys.globalGroup(), Keys.navGroup()}
	if vb := m.current().Bindings(); len(vb) > 0 {
		groups = append(groups, helpGroup{m.current().Title(), vb})
	}
	render := func(gs []helpGroup) string {
		var lines []string
		for i, g := range gs {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, styleHeading.Render(g.title))
			for _, bnd := range g.bindings {
				lines = append(lines, fmt.Sprintf("%s %s", styleKey.Render(fit(bnd.Help().Key, 7)), bnd.Help().Desc))
			}
		}
		return strings.Join(lines, "\n")
	}
	inner := h - 2  // the box border
	innerW := w - 4 // the box border and padding
	body := render(groups)
	if lipgloss.Height(body) > inner {
		// Two columns when they fit across, else one column cut short.
		if two := lipgloss.JoinHorizontal(lipgloss.Top, render(groups[:1]), "    ", render(groups[1:])); lipgloss.Width(two) <= innerW {
			body = two
		}
	}
	if lipgloss.Height(body) > inner {
		// Too tall even in two columns: show a window that j/k scroll, with
		// markers where lines are hidden.
		lines := strings.Split(body, "\n")
		page := max(inner-2, 1)
		m.helpTop = min(max(m.helpTop, 0), len(lines)-page)
		window := append([]string{styleDim.Render(" ")}, lines[m.helpTop:m.helpTop+page]...)
		if m.helpTop > 0 {
			window[0] = styleDim.Render("↑ more (k)")
		}
		more := " "
		if m.helpTop+page < len(lines) {
			more = "↓ more (j)"
		}
		body = strings.Join(append(window, styleDim.Render(more)), "\n")
		m.helpLong = true
	} else {
		m.helpTop, m.helpLong = 0, false
	}
	if lipgloss.Width(body) > innerW {
		lines := strings.Split(body, "\n")
		for i := range lines {
			lines[i] = fit(lines[i], innerW)
		}
		body = strings.Join(lines, "\n")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Blue).Padding(0, 1).Render(body)
	return block(placeMiddle(box, w, h), w, h)
}

// placeMiddle centers s in w x h.
func placeMiddle(s string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}

// hint is a display-only binding for the hint line.
func hint(k, desc string) key.Binding { return key.NewBinding(key.WithHelp(k, desc)) }

package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Styles use the terminal's 16-color palette so terminal themes apply.
var (
	styleBorder      = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	styleFocusBorder = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	styleTitle       = lipgloss.NewStyle().Bold(true)
	styleSelected    = lipgloss.NewStyle().Reverse(true)
	styleSelectedDim = lipgloss.NewStyle().Bold(true)
	styleDim         = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	styleHeading     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Blue)
	styleNotice      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	styleKey         = lipgloss.NewStyle().Bold(true)
	styleGood        = lipgloss.NewStyle().Foreground(lipgloss.Green)
	styleWarn        = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
)

// fit truncates s to width cells (with an ellipsis) and pads it with
// spaces to exactly width.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, width, "…")
	}
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

// center places s in the middle of width cells.
func center(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > width {
		return fit(s, width)
	}
	left := (width - ansi.StringWidth(s)) / 2
	return fit(strings.Repeat(" ", left)+s, width)
}

// block normalizes s to exactly h lines of exactly w cells.
func block(s string, w, h int) []string {
	lines := strings.Split(s, "\n")
	if s == "" {
		lines = nil
	}
	out := make([]string, h)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out[i] = fit(line, w)
	}
	return out
}

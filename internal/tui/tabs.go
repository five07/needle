package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// tabBar draws tab titles in one row, the active one highlighted, with
// right shown dim at the right edge when it fits. Library and Discovery use
// it (decisions 34 and 40).
func tabBar(titles []string, active, width int, right string) string {
	var b strings.Builder
	for i, t := range titles {
		label := " " + t + " "
		if i == active {
			label = styleSelected.Render(label)
		}
		b.WriteString(label + " ")
	}
	line := b.String()
	if pad := width - ansi.StringWidth(line) - ansi.StringWidth(right); right != "" && pad > 0 {
		line += strings.Repeat(" ", pad) + styleDim.Render(right)
	}
	return fit(line, width)
}

// tabAt returns the index of the tab drawn at column x by tabBar, or -1.
func tabAt(titles []string, x int) int {
	start := 0
	for i, t := range titles {
		end := start + ansi.StringWidth(t) + 2
		if x >= start && x < end {
			return i
		}
		start = end + 1
	}
	return -1
}

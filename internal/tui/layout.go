// Package tui is the terminal user interface: a sidebar of sections, a main
// pane that shows the selected section's view, a persistent now-playing bar,
// and a hint line. Layout, keymap, and mouse behavior follow UI.md and
// PLAN.md decisions 21 to 26.
package tui

// Rect is a screen region in cells. X and Y are zero-based.
type Rect struct {
	X, Y, W, H int
}

// Contains reports whether the cell (x, y) is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Layout sizes.
const (
	// MinWidth and MinHeight are the smallest usable terminal (decision 22).
	MinWidth  = 50
	MinHeight = 16
	// CompactBelow is the width under which the sidebar collapses to a
	// letter strip.
	CompactBelow = 80
	// BarHeight is the now-playing bar, kept at full height when idle
	// (decision 23).
	BarHeight = 6

	sidebarFull    = 15
	sidebarCompact = 3
	// chrome is every row that is not sidebar/main content: the top border,
	// two separators, the bar, the hint line, and the bottom border.
	chrome = 1 + 1 + BarHeight + 1 + 1 + 1
)

// Layout is the position of every region for one terminal size. Regions
// are the content areas inside the borders.
type Layout struct {
	Width, Height int
	// TooSmall means the terminal is below MinWidth x MinHeight and only a
	// message is shown.
	TooSmall bool
	// Compact means the sidebar is the letter strip.
	Compact bool
	Sidebar Rect
	Main    Rect
	Bar     Rect
	Hints   Rect
}

// ComputeLayout places the regions for a w x h terminal:
//
//	row 0            top border with the title
//	rows 1..c        sidebar | main pane          (c = h - 11)
//	row c+1          separator
//	rows c+2..c+7    now-playing bar
//	row c+8          separator
//	row c+9          hint line
//	row c+10         bottom border
func ComputeLayout(w, h int) Layout {
	l := Layout{Width: w, Height: h}
	if w < MinWidth || h < MinHeight {
		l.TooSmall = true
		return l
	}
	sw := sidebarFull
	if w < CompactBelow {
		l.Compact = true
		sw = sidebarCompact
	}
	content := h - chrome
	l.Sidebar = Rect{X: 1, Y: 1, W: sw, H: content}
	l.Main = Rect{X: sw + 2, Y: 1, W: w - sw - 3, H: content}
	l.Bar = Rect{X: 1, Y: content + 2, W: w - 2, H: BarHeight}
	l.Hints = Rect{X: 1, Y: content + 2 + BarHeight + 1, W: w - 2, H: 1}
	return l
}

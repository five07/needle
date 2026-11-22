package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/five07/needle/internal/visualizer"
)

type fakeVisualizer struct{ on bool }

func (f *fakeVisualizer) On() bool      { return f.on }
func (f *fakeVisualizer) SetOn(on bool) { f.on = on }

// ramp rises from silent at the lowest band to full at the highest.
func ramp() visualizer.Frame {
	var f visualizer.Frame
	for i := range f {
		f[i] = float64(i) / float64(visualizer.Bands-1)
	}
	return f
}

// AC #3: the bars fill the free cells and every line of text stays
// readable at 80x24 and at the minimum size.
func TestSpectrumGoldens(t *testing.T) {
	for _, tt := range []struct {
		name string
		w, h int
	}{{"spectrum_80x24", 80, 24}, {"spectrum_50x16", MinWidth, MinHeight}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			m := e.model(tt.w, tt.h)
			at := time.Unix(1000, 0)
			m.now = func() time.Time { return at }
			drive(m, NowPlayingMsg{playingNow()}, SpectrumMsg{ramp()})
			golden(t, tt.name, m.render())
		})
	}
}

// The overlay never covers the text: with or without the bars, the text
// of each row (the bit-perfect indicator among it) is the same.
func TestSpectrumKeepsText(t *testing.T) {
	e := newEnv(t)
	m := e.model(80, 24)
	drive(m, NowPlayingMsg{playingNow()})
	plain := barText(m)
	var full visualizer.Frame
	for i := range full {
		full[i] = 1
	}
	drive(m, SpectrumMsg{full})
	lines := m.barLines(m.layout.Bar.W)
	for i, l := range lines {
		got := ansi.Strip(l)
		if w := ansi.StringWidth(got); w != m.layout.Bar.W {
			t.Errorf("row %d is %d wide", i, w)
		}
		if plain[i] != "" && !strings.Contains(got, " "+plain[i]+" ") {
			t.Errorf("row %d: %q lost %q", i, got, plain[i])
		}
	}
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "✓ bit-perfect") {
		t.Error("the indicator is covered")
	}
	// A full frame fills the empty rows completely.
	if got := ansi.Strip(lines[0]); strings.Trim(got, "█") != "" && plain[0] == "" {
		t.Errorf("empty row %q", got)
	}
}

func TestSpectrumGlyphs(t *testing.T) {
	var f visualizer.Frame
	f[0] = 0.5 // 3 of 6 rows
	for row, want := range []rune{'█', '█', '█', ' ', ' ', ' '} {
		if got := spectrumGlyph(f, 0, 32, row); got != want {
			t.Errorf("row %d: %q, want %q", row, got, want)
		}
	}
	f[0] = 1.0 / 48 // one eighth of the bottom row
	if got := spectrumGlyph(f, 0, 32, 0); got != '▁' {
		t.Errorf("one eighth: %q", got)
	}
	f[0] = 7 // out of range is clamped
	if got := spectrumGlyph(f, 0, 32, 5); got != '█' {
		t.Errorf("clamped: %q", got)
	}
	// Columns spread over the bands.
	f = ramp()
	if spectrumGlyph(f, 0, 80, 0) != ' ' || spectrumGlyph(f, 79, 80, 5) != '█' {
		t.Error("columns do not follow the bands")
	}
	// Silence and no width draw nothing.
	rows := []string{"a", "b"}
	if got := withSpectrum(rows, visualizer.Frame{}, themeStyles("terminal", nil), 10); got[0] != "a" {
		t.Errorf("silent: %v", got)
	}
	if got := withSpectrum(rows, ramp(), themeStyles("terminal", nil), 0); got[0] != "a" {
		t.Errorf("no width: %v", got)
	}
}

// The gradient is 4 steps over the 6 rows, darker at the bottom.
func TestThemeGradient(t *testing.T) {
	if rowStep != [BarHeight]int{0, 0, 1, 2, 3, 3} {
		t.Errorf("row steps %v", rowStep)
	}
	for _, theme := range []string{"burnt orange", "ice blue", "forest green", "royal purple", "deep red"} {
		st := themeStyles(theme, nil)
		for i := 1; i < 4; i++ {
			if lum(st[i]) <= lum(st[i-1]) {
				t.Errorf("%s: step %d is not brighter", theme, i)
			}
		}
	}
	custom := []string{"#000001", "#000002", "#000003", "#000004"}
	if st := themeStyles("custom", custom); st[3].GetForeground() != lipgloss.Color("#000004") {
		t.Errorf("custom: %v", st[3].GetForeground())
	}
	// Unknown, or custom without colors: the terminal palette.
	term := themeStyles("terminal", nil)
	for _, theme := range []string{"nope", "custom"} {
		if st := themeStyles(theme, nil); st[3].GetForeground() != term[3].GetForeground() {
			t.Errorf("%s: %v", theme, st[3].GetForeground())
		}
	}
	if !term[0].GetFaint() || !term[3].GetBold() {
		t.Error("terminal steps are not dim to bold")
	}
}

func lum(s lipgloss.Style) float64 {
	r, g, b, _ := s.GetForeground().RGBA()
	return 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
}

// AC #1: v toggles the visualizer in every view.
func TestVisualizerKey(t *testing.T) {
	e := newEnv(t)
	vis := &fakeVisualizer{}
	e.svc.Visualizer = vis
	m := e.model(80, 24)
	for _, keys := range [][]string{{"v"}, {"4", "l", "v"}, {"2", "l", "v"}} {
		before := vis.on
		drive(m, presses(keys...)...)
		if vis.on == before {
			t.Errorf("%v did not toggle", keys)
		}
	}
	e.svc.Visualizer = nil
	if n := drive(m, press("v")); len(n) != 1 || !strings.Contains(n[0], "not available") {
		t.Errorf("notices %v", n)
	}
}

func TestSpectrumListener(t *testing.T) {
	ch := make(chan visualizer.Frame, 1)
	m := New(Config{Services: &Services{}, Spectrum: ch})
	ch <- ramp()
	msg := m.listenSpectrum()()
	if f, ok := msg.(SpectrumMsg); !ok || f.Frame != ramp() {
		t.Fatalf("msg %#v", msg)
	}
	if _, cmd := m.Update(msg); cmd == nil || m.spectrum != ramp() {
		t.Error("frame not kept or not listening on")
	}
	close(ch)
	if msg := m.listenSpectrum()(); msg != nil {
		t.Errorf("closed: %#v", msg)
	}
	if New(Config{Services: &Services{}}).listenSpectrum() != nil {
		t.Error("listening without a channel")
	}
	var _ tea.Msg = SpectrumMsg{}
}

func presses(keys ...string) []tea.Msg {
	out := make([]tea.Msg, len(keys))
	for i, k := range keys {
		out[i] = press(k)
	}
	return out
}

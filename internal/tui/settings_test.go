package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var allThemes = []string{"terminal", "burnt orange", "ice blue", "forest green", "royal purple", "deep red", "custom"}

var testSettings = SettingsInfo{Quality: "hires", AudioMode: "exclusive", AudioDevice: "alsa/hw:CARD=DAC,DEV=0", Autoplay: true,
	Theme: "ice blue", Themes: allThemes}

func TestSettingsGoldens(t *testing.T) {
	for _, tt := range []struct {
		name string
		w, h int
	}{{"settings_80x24", 80, 24}, {"settings_50x16", MinWidth, MinHeight}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.svc.Visualizer = &fakeVisualizer{on: true}
			m := New(Config{Services: e.svc, Settings: NewSettingsView(e.svc, testSettings)})
			drive(m, tea.WindowSizeMsg{Width: tt.w, Height: tt.h}, press("6"), press("l"))
			golden(t, tt.name, m.render())
		})
	}
}

func TestSettingsView(t *testing.T) {
	svc := &Services{}
	vis := &fakeVisualizer{}
	svc.Visualizer = vis
	v := NewSettingsView(svc, testSettings)
	out := ansi.Strip(v.Render(100, 20, true))
	for _, want := range []string{"Visualizer       off", "ice blue", "hires", "exclusive", "alsa/hw:CARD=DAC,DEV=0", "on at start", "config.toml"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if svc.theme != "ice blue" {
		t.Errorf("bar theme %q", svc.theme)
	}
	// enter on the first row toggles the visualizer.
	v.Update(press("enter"))
	if !vis.on || !strings.Contains(ansi.Strip(v.Render(100, 20, true)), "Visualizer       on") {
		t.Error("enter did not turn it on")
	}
	// enter on the theme cycles it; custom has no colors, so it is skipped.
	v.Update(press("j"))
	var seen []string
	for range 6 {
		v.Update(press("enter"))
		seen = append(seen, svc.theme)
	}
	want := []string{"forest green", "royal purple", "deep red", "terminal", "burnt orange", "ice blue"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("cycle %v", seen)
	}
	// The read-only rows say where to change them.
	v.Update(press("j"))
	if cmd := v.Update(press("enter")); cmd == nil || !strings.Contains(cmd().(NoticeMsg).Text, "config.toml") {
		t.Error("read-only row has no notice")
	}
	if v.Update(press("x")) != nil || v.Update(nil) != nil || v.Capturing() || len(v.Bindings()) != 1 || v.Title() != "Settings" {
		t.Error("view basics")
	}
}

func TestSettingsCustomAndUnavailable(t *testing.T) {
	svc := &Services{}
	info := testSettings
	info.Theme, info.Custom = "custom", []string{"#111111", "#222222", "#333333", "#444444"}
	v := NewSettingsView(svc, info)
	if len(v.themes) != 7 || svc.theme != "custom" || len(svc.custom) != 4 {
		t.Errorf("themes %v theme %q", v.themes, svc.theme)
	}
	v.Update(press("j"))
	v.Update(press("enter"))
	if svc.theme != "terminal" {
		t.Errorf("after custom: %q", svc.theme)
	}
	// No visualizer (it could not start): the row says so.
	if !strings.Contains(ansi.Strip(v.Render(100, 20, true)), "unavailable") {
		t.Error("no unavailable")
	}
	v.Update(press("k"))
	if cmd := v.Update(press("enter")); cmd == nil || !strings.Contains(cmd().(NoticeMsg).Text, "not available") {
		t.Error("no notice")
	}
	// No theme list and no theme: terminal, and enter does nothing.
	empty := NewSettingsView(svc, SettingsInfo{})
	if svc.theme != "terminal" {
		t.Errorf("default theme %q", svc.theme)
	}
	empty.Update(press("j"))
	empty.Update(press("enter"))
	if svc.theme != "terminal" {
		t.Errorf("cycled with no themes: %q", svc.theme)
	}
	if empty.Render(80, 0, false) != "" {
		t.Error("zero height drew lines")
	}
}

type applied struct {
	key   string
	value any
}

type fakeSettings struct {
	applied []applied
	devices map[string][]Device
	err     error
}

func (f *fakeSettings) Apply(key string, value any) {
	f.applied = append(f.applied, applied{key, value})
}
func (f *fakeSettings) Devices(_ context.Context, mode string) ([]Device, error) {
	return f.devices[mode], f.err
}

func newSettingsEnv(t *testing.T) (*env, *Model, *fakeSettings, *fakeVisualizer) {
	t.Helper()
	e := newEnv(t)
	fs := &fakeSettings{devices: map[string][]Device{
		"shared":    {{"auto", "System default"}, {"pipewire/hp", "Headphones"}},
		"exclusive": {{"alsa/hw:CARD=DAC,DEV=0", "USB DAC"}, {"alsa/hw:CARD=PCH,DEV=0", "Speakers (built-in)"}},
	}}
	vis := &fakeVisualizer{}
	e.svc.Settings, e.svc.Visualizer = fs, vis
	info := testSettings
	info.Quality, info.AudioMode, info.AudioDevice, info.Autoplay = "hires", "shared", "auto", false
	m := New(Config{Services: e.svc, Settings: NewSettingsView(e.svc, info)})
	drive(m, tea.WindowSizeMsg{Width: 80, Height: 24}, press("6"), press("l"))
	return e, m, fs, vis
}

func screen(m *Model) string { return ansi.Strip(m.render()) }

// AC #1, #5, #6: each row changes its setting and hands it to the service
// to apply and save.
func TestSettingsRowsApply(t *testing.T) {
	_, m, fs, vis := newSettingsEnv(t)
	drive(m, press("enter"))                                                 // visualizer
	drive(m, press("j"), press("enter"))                                     // theme
	n := drive(m, press("j"), press("enter"))                                // quality
	drive(m, press("j"), press("j"), press("j"), press("j"), press("enter")) // autoplay
	want := []applied{{"visualizer.enabled", true}, {"visualizer.theme", "forest green"}, {"quality", "aac"}, {"autoplay", true}}
	if !reflect.DeepEqual(fs.applied, want) {
		t.Errorf("applied %v", fs.applied)
	}
	if len(n) != 1 || n[0] != QualityNotice {
		t.Errorf("notices %v", n)
	}
	vis.on = true // the service switched it
	out := screen(m)
	for _, s := range []string{"Visualizer       on", "forest green", "Quality          aac", "Autoplay         on (now off)", "saved to config.toml"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q:\n%s", s, out)
		}
	}
	// v changes the session only: the row shows both (decision 77).
	drive(m, press("v"))
	if !strings.Contains(screen(m), "Visualizer       on (now off)") || len(fs.applied) != 4 {
		t.Errorf("after v:\n%s\napplied %v", screen(m), fs.applied)
	}
	// Quality cycles round.
	drive(m, press("g"), press("j"), press("j"), press("enter"), press("enter"))
	if got := fs.applied[len(fs.applied)-2:]; got[0].value != "lossless" || got[1].value != "hires" {
		t.Errorf("cycle %v", got)
	}
}

// AC #1, #2: the device picker lists the devices for the mode; exclusive
// needs one picked, and esc cancels without a change.
func TestSettingsModeAndDevice(t *testing.T) {
	_, m, fs, _ := newSettingsEnv(t)
	drive(m, press("j"), press("j"), press("j"), press("enter")) // mode: shared → exclusive
	out := screen(m)
	if !strings.Contains(out, "Audio device for exclusive mode") || !strings.Contains(out, "USB DAC") || len(fs.applied) != 0 {
		t.Fatalf("picker:\n%s\napplied %v", out, fs.applied)
	}
	golden(t, "device_picker_80x24", m.render())
	drive(m, press("esc"))
	if out := screen(m); !strings.Contains(out, "Audio mode       shared") || len(fs.applied) != 0 {
		t.Fatalf("esc changed it:\n%s", out)
	}
	drive(m, press("enter"))
	drive(m, press("j"), press("enter")) // pick the speakers
	want := []applied{{"audio.mode", "exclusive"}, {"audio.device", "alsa/hw:CARD=PCH,DEV=0"}}
	if !reflect.DeepEqual(fs.applied, want) {
		t.Fatalf("applied %v", fs.applied)
	}
	if out := screen(m); !strings.Contains(out, "Audio mode       exclusive") || !strings.Contains(out, "alsa/hw:CARD=PCH,DEV=0") {
		t.Errorf("rows:\n%s", out)
	}
	// The device row opens the picker at the current device, marked.
	drive(m, press("j"), press("enter"))
	if out := screen(m); !strings.Contains(out, "✓ alsa/hw:CARD=PCH,DEV=0") {
		t.Errorf("current not marked:\n%s", out)
	}
	drive(m, press("enter")) // the cursor starts on it
	if last := fs.applied[len(fs.applied)-1]; last != (applied{"audio.device", "alsa/hw:CARD=PCH,DEV=0"}) || len(fs.applied) != 3 {
		t.Errorf("device %v", fs.applied)
	}
	drive(m, press("enter"))
	drive(m, press("k"), press("enter"))
	if last := fs.applied[len(fs.applied)-1]; last != (applied{"audio.device", "alsa/hw:CARD=DAC,DEV=0"}) {
		t.Errorf("device %v", last)
	}
	// Back to shared: the device resets to auto, at once.
	fs.applied = nil
	drive(m, press("k"), press("enter"))
	if want := []applied{{"audio.mode", "shared"}, {"audio.device", "auto"}}; !reflect.DeepEqual(fs.applied, want) {
		t.Errorf("to shared %v", fs.applied)
	}
	// Shared devices: auto first.
	drive(m, press("j"), press("enter"))
	if out := screen(m); !strings.Contains(out, "✓ auto") || !strings.Contains(out, "pipewire/hp") {
		t.Errorf("shared picker:\n%s", out)
	}
}

func TestDevicePickerProblems(t *testing.T) {
	_, m, fs, _ := newSettingsEnv(t)
	fs.devices["exclusive"] = nil
	drive(m, press("j"), press("j"), press("j"), press("enter"))
	if out := screen(m); !strings.Contains(out, "No devices found for exclusive mode. Nothing was changed.") {
		t.Errorf("empty:\n%s", out)
	}
	drive(m, press("enter")) // nothing to pick
	if len(fs.applied) != 0 {
		t.Errorf("applied %v", fs.applied)
	}
	drive(m, press("esc"))
	fs.devices["exclusive"] = []Device{{"alsa/hw:0", "x"}}
	fs.err = errors.New("boom")
	drive(m, press("enter"))
	if out := screen(m); !strings.Contains(out, "Could not list the devices") {
		t.Errorf("error:\n%s", out)
	}
	// Loading, before the list arrives.
	p := newDevicePicker(&Services{}, "shared", "", func(Device) {})
	if out := ansi.Strip(p.Render(60, 5, true)); !strings.Contains(out, "Listing devices") {
		t.Errorf("loading: %s", out)
	}
	if p.Title() != "Audio device" || p.Capturing() || len(p.Bindings()) != 2 {
		t.Error("picker basics")
	}
}

// TASK-018 AC #1, #3: the Exclusive volume row shows the volume control
// and cycles it, saved like the other rows (decision 83).
func TestSettingsVolumeRow(t *testing.T) {
	_, m, fs, _ := newSettingsEnv(t)
	if out := screen(m); !strings.Contains(out, "Exclusive volume on the output device") {
		t.Fatalf("unset volume control:\n%s", out)
	}
	for range setVolume {
		drive(m, press("j"))
	}
	drive(m, press("enter"))
	if out := screen(m); !strings.Contains(out, "Exclusive volume software") {
		t.Errorf("after enter:\n%s", out)
	}
	drive(m, press("enter"))
	want := []applied{{"audio.volume", "software"}, {"audio.volume", "device"}}
	if !reflect.DeepEqual(fs.applied, want) {
		t.Errorf("applied %v", fs.applied)
	}
	// Without a settings service the row is read-only.
	e := newEnv(t)
	info := testSettings
	info.Volume = "software"
	ro := New(Config{Services: e.svc, Settings: NewSettingsView(e.svc, info)})
	drive(ro, tea.WindowSizeMsg{Width: 80, Height: 24}, press("6"), press("l"))
	for range setVolume {
		drive(ro, press("j"))
	}
	n := drive(ro, press("enter"))
	if out := screen(ro); !strings.Contains(out, "Exclusive volume software") || len(n) != 1 || !strings.Contains(n[0], "config.toml") {
		t.Errorf("read-only: %v\n%s", n, out)
	}
}

func TestSettingsAndPickerMouse(t *testing.T) {
	_, m, fs, _ := newSettingsEnv(t)
	main := m.layout.Main
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 1 + setAutoplay, Button: tea.MouseLeft})
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 1 + setAutoplay, Button: tea.MouseLeft})
	if len(fs.applied) != 1 || fs.applied[0].key != "autoplay" {
		t.Fatalf("double click: %v", fs.applied)
	}
	drive(m, press("k"), press("k"), press("enter")) // device picker
	drive(m, tea.MouseWheelMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseWheelDown})
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseLeft})
	drive(m, tea.MouseClickMsg{X: main.X + 3, Y: main.Y + 3, Button: tea.MouseLeft})
	if last := fs.applied[len(fs.applied)-1]; last.key != "audio.device" {
		t.Errorf("picker double click: %v", fs.applied)
	}
}

// AC #3 layout: the editable Settings section at the minimum size.
func TestSettingsSavedGolden(t *testing.T) {
	e := newEnv(t)
	e.svc.Settings, e.svc.Visualizer = &fakeSettings{}, &fakeVisualizer{}
	m := New(Config{Services: e.svc, Settings: NewSettingsView(e.svc, testSettings)})
	drive(m, tea.WindowSizeMsg{Width: MinWidth, Height: MinHeight}, press("6"), press("l"))
	golden(t, "settings_saved_50x16", m.render())
}

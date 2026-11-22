package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/tidal"
	"github.com/five07/needle/internal/tui"
)

type fakeTimer struct {
	mu      sync.Mutex
	fn      func()
	d       time.Duration
	stopped int
}

func (f *fakeTimer) Stop() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped++
	f.fn = nil
	return true
}

// fire runs the pending callback, as the timer would after the delay.
func (f *fakeTimer) fire(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	fn := f.fn
	f.fn = nil
	f.mu.Unlock()
	if fn == nil {
		t.Fatal("no save scheduled")
	}
	fn()
}

type settingsEnv struct {
	s        *settingsService
	timer    *fakeTimer
	path     string
	notices  []string
	logs     *bytes.Buffer
	qualites []config.Quality
	autoplay []bool
	vis      []bool
	volumes  []config.VolumeControl
	switches []config.Audio
}

func newSettingsEnv(t *testing.T, body string) *settingsEnv {
	t.Helper()
	e := &settingsEnv{timer: &fakeTimer{}, path: filepath.Join(t.TempDir(), config.FileName), logs: &bytes.Buffer{}}
	if body != "" {
		os.WriteFile(e.path, []byte(body), 0o600)
	}
	e.s = newSettingsService(e.path, config.Audio{Mode: config.AudioShared, Device: "auto"},
		slog.New(slog.NewTextHandler(e.logs, nil)), func(s string) { e.notices = append(e.notices, s) })
	e.s.after = func(d time.Duration, f func()) stopper {
		e.timer.mu.Lock()
		e.timer.fn, e.timer.d = f, d
		e.timer.mu.Unlock()
		return e.timer
	}
	e.s.setQuality = func(q config.Quality) { e.qualites = append(e.qualites, q) }
	e.s.setAutoplay = func(on bool) { e.autoplay = append(e.autoplay, on) }
	e.s.setVisualizer = func(on bool) { e.vis = append(e.vis, on) }
	e.s.setVolume = func(c config.VolumeControl) { e.volumes = append(e.volumes, c) }
	e.s.switchAudio = func(a config.Audio) { e.switches = append(e.switches, a) }
	return e
}

func (e *settingsEnv) file(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(e.path)
	return string(b)
}

func TestSettingsApplyNowSaveLater(t *testing.T) {
	e := newSettingsEnv(t, "# my settings\nquality = \"hires\" # best\n")
	e.s.Apply("quality", "lossless")
	e.s.Apply("autoplay", true)
	e.s.Apply("visualizer.enabled", true)
	e.s.Apply("visualizer.theme", "ice blue")
	e.s.Apply("quality", "aac") // cycling on: only the last is saved
	// The session changes at once.
	if !reflect.DeepEqual(e.qualites, []config.Quality{"lossless", "aac"}) || !reflect.DeepEqual(e.autoplay, []bool{true}) ||
		!reflect.DeepEqual(e.vis, []bool{true}) {
		t.Errorf("session: %v %v %v", e.qualites, e.autoplay, e.vis)
	}
	// Nothing is saved until the changes settle; each change restarts the wait.
	if got := e.file(t); got != "# my settings\nquality = \"hires\" # best\n" {
		t.Errorf("saved early:\n%s", got)
	}
	if e.timer.d != time.Second || e.timer.stopped != 4 {
		t.Errorf("delay %v, restarted %d times", e.timer.d, e.timer.stopped)
	}
	e.timer.fire(t)
	want := "# my settings\nquality = \"aac\" # best\nautoplay = true\n\n[visualizer]\nenabled = true\ntheme = \"ice blue\"\n"
	if got := e.file(t); got != want {
		t.Errorf("saved:\n%s\nwant:\n%s", got, want)
	}
	if len(e.switches) != 0 || len(e.notices) != 0 {
		t.Errorf("switches %v notices %v", e.switches, e.notices)
	}
}

// TASK-018 decision 83: the volume control applies at once, without an
// engine switch, and is saved after the changes settle.
func TestSettingsVolumeControl(t *testing.T) {
	e := newSettingsEnv(t, "[audio]\nmode = \"exclusive\" # my DAC\ndevice = \"alsa/hw:4\"\n")
	e.s.Apply("audio.volume", "software")
	if !reflect.DeepEqual(e.volumes, []config.VolumeControl{config.VolumeSoftware}) {
		t.Fatalf("session: %v", e.volumes)
	}
	e.timer.fire(t)
	want := "[audio]\nmode = \"exclusive\" # my DAC\ndevice = \"alsa/hw:4\"\nvolume = \"software\"\n"
	if got := e.file(t); got != want || len(e.switches) != 0 {
		t.Errorf("saved:\n%s\nswitches %v", got, e.switches)
	}
	// Without the hook (tests, or no player) it is still saved.
	e.s.setVolume = nil
	e.s.Apply("audio.volume", "device")
	e.timer.fire(t)
	if !strings.Contains(e.file(t), `volume = "device"`) {
		t.Errorf("saved:\n%s", e.file(t))
	}
}

// The audio output switches once, after the changes settle, with the mode
// and device together (decision 80).
func TestSettingsAudioSwitchIsDebounced(t *testing.T) {
	e := newSettingsEnv(t, "")
	e.s.Apply("audio.mode", "exclusive")
	e.s.Apply("audio.device", "alsa/hw:CARD=DAC,DEV=0")
	if len(e.switches) != 0 {
		t.Fatal("switched before settling")
	}
	e.timer.fire(t)
	if want := []config.Audio{{Mode: config.AudioExclusive, Device: "alsa/hw:CARD=DAC,DEV=0"}}; !reflect.DeepEqual(e.switches, want) {
		t.Errorf("switches %v", e.switches)
	}
	if got := e.file(t); !strings.Contains(got, "mode = \"exclusive\"") || !strings.Contains(got, "device = \"alsa/hw:CARD=DAC,DEV=0\"") {
		t.Errorf("saved:\n%s", got)
	}
	// A later save of other settings does not switch again.
	e.s.Apply("autoplay", true)
	e.timer.fire(t)
	if len(e.switches) != 1 {
		t.Errorf("switched again: %v", e.switches)
	}
}

func TestSettingsSaveFailures(t *testing.T) {
	// A key the file writes as a dotted key is not saved, with a notice;
	// the others are.
	e := newSettingsEnv(t, "visualizer.theme = \"terminal\"\n")
	e.s.Apply("visualizer.theme", "deep red")
	e.s.Apply("quality", "aac")
	e.timer.fire(t)
	if len(e.notices) != 1 || !strings.Contains(e.notices[0], "visualizer.theme not saved") {
		t.Errorf("notices %v", e.notices)
	}
	if got := e.file(t); got != "visualizer.theme = \"terminal\"\nquality = \"aac\"\n" {
		t.Errorf("saved:\n%q", got)
	}
	// Only unsupported keys: nothing is written.
	e.notices = nil
	e.s.Apply("visualizer.theme", "ice blue")
	e.timer.fire(t)
	if len(e.notices) != 1 || !strings.Contains(e.file(t), "terminal") {
		t.Errorf("notices %v file %s", e.notices, e.file(t))
	}
	// Any other failure: one notice, logged, and the session keeps it.
	e = newSettingsEnv(t, "quality = \n") // broken by hand since startup
	e.s.Apply("autoplay", true)
	e.timer.fire(t)
	if len(e.notices) != 1 || !strings.HasPrefix(e.notices[0], "Settings not saved") || !strings.Contains(e.logs.String(), "settings not saved") {
		t.Errorf("notices %v logs %s", e.notices, e.logs)
	}
	if !reflect.DeepEqual(e.autoplay, []bool{true}) {
		t.Error("session not changed")
	}
}

// Quitting saves what is pending without starting a new engine, and later
// changes are ignored.
func TestSettingsClose(t *testing.T) {
	e := newSettingsEnv(t, "")
	e.s.Apply("audio.mode", "shared")
	e.s.Apply("audio.device", "pipewire/sink")
	e.s.Close()
	if len(e.switches) != 0 || !strings.Contains(e.file(t), "device = \"pipewire/sink\"") || e.timer.stopped == 0 {
		t.Errorf("switches %v file %s", e.switches, e.file(t))
	}
	e.s.Apply("quality", "aac")
	if e.timer.fn != nil || strings.Contains(e.file(t), "aac") {
		t.Error("a change after Close was scheduled")
	}
	// Nothing pending: Close writes nothing.
	e2 := newSettingsEnv(t, "")
	e2.s.Close()
	if _, err := os.Stat(e2.path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wrote an empty config: %v", err)
	}
	// Without a path (tests) nothing is written, but the session changes.
	e3 := newSettingsEnv(t, "")
	e3.s.path = ""
	e3.s.Apply("autoplay", true)
	e3.timer.fire(t)
	if len(e3.autoplay) != 1 || len(e3.notices) != 0 {
		t.Errorf("autoplay %v notices %v", e3.autoplay, e3.notices)
	}
}

func TestSettingsRealTimer(t *testing.T) {
	e := newSettingsEnv(t, "")
	e.s.after = newSettingsService("", config.Audio{}, nil, nil).after
	e.s.delay = 10 * time.Millisecond
	e.s.Apply("autoplay", true)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(e.file(t), "autoplay = true") {
		if time.Now().After(deadline) {
			t.Fatal("not saved")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSettingsDevices(t *testing.T) {
	e := newSettingsEnv(t, "")
	var asked []config.AudioMode
	var err error
	e.s.devices = func(_ context.Context, m config.AudioMode) ([]player.Device, error) {
		asked = append(asked, m)
		if m == config.AudioExclusive {
			return []player.Device{{Name: "alsa/hw:CARD=DAC,DEV=0", Description: "USB DAC"}, {Name: "alsa/hw:CARD=PCH,DEV=0", Description: "Speakers", BuiltIn: true}}, err
		}
		return []player.Device{{Name: "auto", Description: "Autoselect device"}, {Name: "pipewire/x", Description: "Headphones"}}, err
	}
	got, _ := e.s.Devices(context.Background(), "shared")
	want := []tui.Device{{Name: "auto", Description: "System default"}, {Name: "pipewire/x", Description: "Headphones"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shared %v", got)
	}
	got, _ = e.s.Devices(context.Background(), "exclusive")
	want = []tui.Device{{Name: "alsa/hw:CARD=DAC,DEV=0", Description: "USB DAC"}, {Name: "alsa/hw:CARD=PCH,DEV=0", Description: "Speakers (built-in)"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("exclusive %v", got)
	}
	// mpv missing: shared still offers auto; exclusive reports the error.
	err = errors.New("no mpv")
	if got, gerr := e.s.Devices(context.Background(), "shared"); gerr != nil || len(got) != 1 || got[0].Name != "auto" {
		t.Errorf("shared without mpv: %v %v", got, gerr)
	}
	if _, gerr := e.s.Devices(context.Background(), "exclusive"); gerr == nil {
		t.Error("exclusive error hidden")
	}
}

// listDevices reads the same sources as -list-devices.
func TestListDevicesForSettings(t *testing.T) {
	f := newFakeTIDAL(t)
	d := f.deps()
	d.asoundRoot = t.TempDir() // no cards
	list := listDevices(d)
	shared, err := list(context.Background(), config.AudioShared)
	if err != nil || len(shared) != 2 || shared[1].Name != "pipewire/alsa_output.usb-x" {
		t.Errorf("shared %v %v", shared, err)
	}
	if ex, err := list(context.Background(), config.AudioExclusive); err != nil || len(ex) != 0 {
		t.Errorf("exclusive %v %v", ex, err)
	}
	d.mpvDeviceHelp = func(context.Context) ([]byte, error) { return nil, errors.New("no mpv") }
	if _, err := listDevices(d)(context.Background(), config.AudioShared); err == nil {
		t.Error("error hidden")
	}
}

// pressAll sends keys to the model, running the commands each returns
// (timers and listeners that do not finish are skipped).
func pressAll(m tea.Model, keys ...string) {
	for _, k := range keys {
		msgs := []tea.Msg{tea.KeyPressMsg{Code: []rune(k)[0], Text: k}}
		if k == "enter" {
			msgs = []tea.Msg{tea.KeyPressMsg{Code: tea.KeyEnter}}
		}
		for len(msgs) > 0 {
			msg := msgs[0]
			msgs = msgs[1:]
			_, cmd := m.Update(msg)
			msgs = append(msgs, exec(cmd, 50*time.Millisecond)...)
		}
	}
}

// End to end (AC #1, #3, #5, #6, and TASK-018 AC #1): rows changed in the Settings section are
// saved to config.toml, keeping its comments. (The live engine switch is
// tested in the player package; here nothing plays.)
func TestSettingsSavedFromTheUI(t *testing.T) {
	f := newFakeTIDAL(t)
	d := f.deps()
	c, err := tidal.New(stubTokenSource{}, append([]tidal.Option{tidal.WithLogger(discardLogger())}, d.apiOptions...)...)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	os.WriteFile(path, []byte("# mine\nquality = \"hires\" # keep\n"), 0o600)
	model, stop, err := newUI(context.Background(), d, testConfig(), path, c, tidal.User{Country: "US"}, "", "", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pressAll(model, "6", "l", "enter", "j", "enter", "j", "enter") // visualizer, theme, quality
	pressAll(model, "j", "j", "j", "enter", "j", "enter")          // volume control (TASK-018), autoplay
	pressAll(model, "k", "k", "enter")                             // the device picker
	pressAll(model, "j", "enter")                                  // the PipeWire sink
	// The audio switch and the save wait for the changes to settle.
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(readConfig(t, path), "device") {
		if time.Now().After(deadline) {
			t.Fatalf("not saved:\n%s", readConfig(t, path))
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	want := "# mine\nquality = \"aac\" # keep\nautoplay = true\n\n[visualizer]\nenabled = true\ntheme = \"burnt orange\"\n\n[audio]\nvolume = \"software\"\ndevice = \"pipewire/alsa_output.usb-x\"\n"
	if got := readConfig(t, path); got != want {
		t.Errorf("saved:\n%s\nwant:\n%s", got, want)
	}
}

func readConfig(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

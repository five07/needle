package tui

import (
	"context"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// SettingsService applies and saves settings (decisions 74 to 81).
type SettingsService interface {
	// Apply changes a setting for the session now and saves it to
	// config.toml after a short pause. key is the config.toml key (for
	// example "audio.mode"); value is a string or a bool.
	Apply(key string, value any)
	// Devices lists the audio devices for mode, as -list-devices does.
	Devices(ctx context.Context, mode string) ([]Device, error)
}

// Device is an audio device choice.
type Device struct {
	Name, Description string
}

// SettingsInfo is what the Settings section shows: every setting that is
// not about the TIDAL account (decision 70). The values are the saved
// ones (config.toml); the view keeps them up to date as it changes them.
type SettingsInfo struct {
	Quality     string
	AudioMode   string
	AudioDevice string
	Autoplay    bool
	// Visualizer is visualizer.enabled.
	Visualizer bool
	// Theme and Custom are the visualizer theme; Themes lists the choices
	// in cycling order.
	Theme  string
	Custom []string
	Themes []string
}

// Qualities is the order enter cycles the quality in.
var Qualities = []string{"aac", "lossless", "hires"}

// QualityNotice is shown when the quality changes (decision 78).
const QualityNotice = "Quality updates on the next track"

// Settings rows.
const (
	setVisualizer = iota
	setTheme
	setQuality
	setMode
	setDevice
	setAutoplay
	settingRows
)

// SettingsView is the Settings section. With a SettingsService, enter
// changes the selected setting for the session and saves it (decisions 74
// to 81); without one, only the visualizer rows change, for the session.
type SettingsView struct {
	svc    *Services
	info   SettingsInfo
	themes []string // the choices offered: custom only with its colors
	list   List
}

// NewSettingsView returns the Settings section view. It sets the
// visualizer theme the now-playing bar draws with.
func NewSettingsView(svc *Services, info SettingsInfo) *SettingsView {
	v := &SettingsView{svc: svc, info: info}
	for _, t := range info.Themes {
		if t != "custom" || len(info.Custom) == 4 {
			v.themes = append(v.themes, t)
		}
	}
	svc.theme, svc.custom = info.Theme, info.Custom
	if svc.theme == "" {
		svc.theme = "terminal"
	}
	v.list.SetRows(v.rows(80))
	return v
}

// Title implements View.
func (v *SettingsView) Title() string { return "Settings" }

func onOff(on bool) string { return map[bool]string{false: "off", true: "on"}[on] }

// withSession adds the session value when v or c changed it (decision 77).
func withSession(saved, now bool) string {
	if saved == now {
		return onOff(saved)
	}
	return onOff(saved) + " (now " + onOff(now) + ")"
}

func (v *SettingsView) rows(width int) []string {
	row := func(label, value string) string {
		return fit("  "+fit(label, accountLabelW)+Sanitize(value), width)
	}
	vis := "unavailable"
	if v.svc.Visualizer != nil {
		vis = onOff(v.svc.Visualizer.On())
		if v.svc.Settings != nil {
			vis = withSession(v.info.Visualizer, v.svc.Visualizer.On())
		}
	}
	autoplay := onOff(v.info.Autoplay) + " at start"
	if v.svc.Settings != nil {
		autoplay = withSession(v.info.Autoplay, v.svc.playing.Autoplay)
	}
	r := make([]string, settingRows)
	r[setVisualizer] = row("Visualizer", vis)
	r[setTheme] = row("Visualizer theme", v.svc.theme)
	r[setQuality] = row("Quality", orDash(v.info.Quality))
	r[setMode] = row("Audio mode", orDash(v.info.AudioMode))
	r[setDevice] = row("Device", orDash(v.info.AudioDevice))
	r[setAutoplay] = row("Autoplay", autoplay)
	return r
}

// Render implements View.
func (v *SettingsView) Render(width, height int, focused bool) string {
	v.list.SetRows(v.rows(width))
	head := []string{styleTitle.Render(fit("Settings", width))}
	text := "  Visualizer changes last for this session. Change the others in config.toml and restart."
	if v.svc.Settings != nil {
		text = "  Changes are saved to config.toml. v and c change the session only."
	}
	note := []string{"", styleDim.Render(fit(text, width))}
	if height < len(head)+settingRows+len(note) {
		note = note[1:] // short: the rows come before the blank line
	}
	body := v.list.Render(width, max(height-len(head)-len(note), 0), focused)
	lines := append(append(head, strings.Split(body, "\n")...), note...)
	return strings.Join(lines[:min(len(lines), max(height, 0))], "\n")
}

// Update implements View.
func (v *SettingsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.list.HandleKey(msg) {
			return nil
		}
		if key.Matches(msg, Keys.Open) {
			return v.change(v.list.Cursor())
		}
	case ClickMsg:
		if i := v.list.RowAt(msg.Y - 1); i >= 0 {
			v.list.Select(i)
			if msg.Double {
				return v.change(i)
			}
		}
	}
	return nil
}

// change acts on enter on row i.
func (v *SettingsView) change(i int) tea.Cmd {
	s := v.svc.Settings
	switch i {
	case setVisualizer:
		if v.svc.Visualizer == nil {
			return Notify("The visualizer is not available.")
		}
		if s == nil {
			v.svc.Visualizer.SetOn(!v.svc.Visualizer.On())
			return nil
		}
		v.info.Visualizer = !v.info.Visualizer
		s.Apply("visualizer.enabled", v.info.Visualizer) // the service switches it
		return nil
	case setTheme:
		if len(v.themes) == 0 {
			return nil
		}
		v.svc.theme = v.themes[(slices.Index(v.themes, v.svc.theme)+1)%len(v.themes)]
		v.info.Theme = v.svc.theme
		if s != nil {
			s.Apply("visualizer.theme", v.svc.theme)
		}
		return nil
	}
	if s == nil {
		return Notify("Change this in config.toml and restart.")
	}
	switch i {
	case setQuality:
		v.info.Quality = Qualities[(slices.Index(Qualities, v.info.Quality)+1)%len(Qualities)]
		s.Apply("quality", v.info.Quality)
		return Notify(QualityNotice)
	case setMode:
		if v.info.AudioMode == "exclusive" {
			v.info.AudioMode, v.info.AudioDevice = "shared", "auto"
			s.Apply("audio.mode", "shared")
			s.Apply("audio.device", "auto")
			return nil
		}
		// Exclusive needs a hardware device: the mode changes only with
		// one picked (decision 80).
		return Push(newDevicePicker(v.svc, "exclusive", "", func(d Device) {
			v.info.AudioMode, v.info.AudioDevice = "exclusive", d.Name
			s.Apply("audio.mode", "exclusive")
			s.Apply("audio.device", d.Name)
		}))
	case setDevice:
		mode := v.info.AudioMode
		if mode == "" {
			mode = "shared"
		}
		return Push(newDevicePicker(v.svc, mode, v.info.AudioDevice, func(d Device) {
			v.info.AudioDevice = d.Name
			s.Apply("audio.device", d.Name)
		}))
	default: // setAutoplay
		v.info.Autoplay = !v.info.Autoplay
		s.Apply("autoplay", v.info.Autoplay)
		return nil
	}
}

// Bindings implements View.
func (v *SettingsView) Bindings() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "change"))}
}

// Capturing implements View.
func (v *SettingsView) Capturing() bool { return false }

type devicesMsg struct {
	devices []Device
	err     error
}

// DevicePicker lists the audio devices for one mode. It opens over the
// Settings section: enter picks, esc cancels.
type DevicePicker struct {
	svc     *Services
	mode    string
	current string
	pick    func(Device)
	list    List
	devices []Device
	loaded  bool
	errText string
}

func newDevicePicker(svc *Services, mode, current string, pick func(Device)) *DevicePicker {
	return &DevicePicker{svc: svc, mode: mode, current: current, pick: pick}
}

// Open implements Opener: it lists the devices.
func (p *DevicePicker) Open() tea.Cmd {
	s, ctx, mode := p.svc.Settings, p.svc.ctx(), p.mode
	return To(p, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, favoriteTimeout)
		defer cancel()
		d, err := s.Devices(ctx, mode)
		return devicesMsg{devices: d, err: err}
	})
}

// Title implements View.
func (p *DevicePicker) Title() string { return "Audio device" }

// Render implements View.
func (p *DevicePicker) Render(width, height int, focused bool) string {
	head := []string{
		styleTitle.Render(fit("Audio device for "+p.mode+" mode", width)),
		styleBorder.Render(strings.Repeat("─", width)),
	}
	var body string
	switch {
	case p.errText != "":
		body = styleDim.Render(fit("Could not list the devices: "+p.errText, width))
	case !p.loaded:
		body = styleDim.Render(fit("Listing devices…", width))
	case len(p.devices) == 0:
		body = styleDim.Render(fit("No devices found for "+p.mode+" mode. Nothing was changed.", width))
	default:
		body = p.list.Render(width, max(height-len(head), 0), focused)
	}
	return strings.Join(append(head[:min(len(head), height)], body), "\n")
}

// Update implements View.
func (p *DevicePicker) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case devicesMsg:
		p.loaded = true
		if msg.err != nil {
			p.svc.logger().Warn("list audio devices failed", "mode", p.mode, "err", msg.err)
			p.errText = userMessage(msg.err)
			return nil
		}
		p.devices = msg.devices
		rows := make([]string, len(msg.devices))
		at := 0
		for i, d := range msg.devices {
			mark := indent
			if d.Name == p.current {
				mark, at = "✓ ", i
			}
			rows[i] = mark + joinMeta(d.Name, d.Description) // SetRows strips escapes
		}
		p.list.SetRows(rows)
		p.list.Select(at)
	case tea.KeyPressMsg:
		if p.list.HandleKey(msg) {
			return nil
		}
		if key.Matches(msg, Keys.Open) {
			return p.choose(p.list.Cursor())
		}
	case ClickMsg:
		if i := p.list.RowAt(msg.Y - 2); i >= 0 {
			p.list.Select(i)
			if msg.Double {
				return p.choose(i)
			}
		}
	case WheelMsg:
		p.list.Scroll(msg.Delta)
	}
	return nil
}

func (p *DevicePicker) choose(i int) tea.Cmd {
	if i < 0 || i >= len(p.devices) {
		return nil
	}
	p.pick(p.devices[i])
	return func() tea.Msg { return CloseMsg{View: p} }
}

// Bindings implements View.
func (p *DevicePicker) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "use this device")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// Capturing implements View.
func (p *DevicePicker) Capturing() bool { return false }

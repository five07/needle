package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/five07/needle/internal/config"
)

// MPVConfig configures the mpv engine.
type MPVConfig struct {
	// Binary is the mpv executable name or path. Default "mpv".
	Binary string
	Mode   config.AudioMode
	// Device is the mpv audio device ("auto" for the default in shared mode).
	Device string
	// Getenv reads the environment (for XDG_RUNTIME_DIR). Default os.Getenv.
	Getenv func(string) string
	Logger *slog.Logger
	// StartTimeout bounds how long mpv may take to open its socket.
	StartTimeout time.Duration
	// PollInterval is how often the position is sampled.
	PollInterval time.Duration
	// QuitTimeout bounds how long Close takes, the fade included, before
	// mpv is killed.
	QuitTimeout time.Duration
	// FadeOut is how long Close ramps the volume down when a track is
	// playing, so quitting does not cut the stream mid-waveform (an audible
	// pop). Default 100 ms.
	FadeOut time.Duration
	// FadeDrain is how long Close then waits for audio already buffered in
	// the output device to play out. Default 200 ms, mpv's audio buffer.
	FadeDrain time.Duration
}

// maxSocketPath stays under the 108-byte sockaddr_un limit.
const maxSocketPath = 100

// mpvArgs returns the mpv command line. It ignores user mpv config and
// scripts (which could add filters or volume), never uses
// gapless-audio=yes (which resamples format changes), and caps volume at
// 100% (no software gain).
func mpvArgs(mode config.AudioMode, device, socket string) []string {
	args := []string{
		"--idle=yes", "--no-config", "--no-video", "--no-terminal",
		"--load-scripts=no", "--ytdl=no", "--load-auto-profiles=no",
		"--input-ipc-server=" + socket,
		"--gapless-audio=weak", "--prefetch-playlist=yes",
		"--volume=100", "--volume-max=100", "--replaygain=no", "--af=",
		"--audio-display=no", "--audio-client-name=needle",
		// The MPD is a local file; its segments are absolute https URLs.
		"--demuxer-lavf-o=protocol_whitelist=[file,http,https,tls,tcp,crypto,data]",
	}
	if mode == config.AudioExclusive {
		return append(args, "--ao=alsa", "--audio-device="+device, "--audio-exclusive=yes")
	}
	args = append(args, "--ao=pipewire", "--audio-exclusive=no")
	if device != "" && device != config.DefaultAudioDevice {
		args = append(args, "--audio-device="+device)
	}
	return args
}

type mpvEntry struct {
	key  uint64
	file string
}

// MPV is an Engine backed by an mpv subprocess controlled over JSON IPC.
type MPV struct {
	cfg     MPVConfig
	logger  *slog.Logger
	cmd     *exec.Cmd
	ipc     *ipcConn
	dir     string
	ownsDir bool
	socket  string

	exited  chan struct{}
	waitErr error

	mu       sync.Mutex
	entries  map[int64]mpvEntry
	early    map[int64][]ipcEvent // events for entries not yet registered
	maxKnown int64
	current  int64
	pending  int64
	seq      int
	lastPos  time.Duration
	format   Event
	busySeen bool
	closing  bool

	qmu    sync.Mutex
	queue  []Event
	qsig   chan struct{}
	events chan Event

	stop      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// StartMPV launches mpv and connects to it.
func StartMPV(ctx context.Context, cfg MPVConfig) (*MPV, error) {
	if cfg.Binary == "" {
		cfg.Binary = "mpv"
	}
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 5 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.QuitTimeout == 0 {
		cfg.QuitTimeout = 2 * time.Second
	}
	if cfg.FadeOut == 0 {
		cfg.FadeOut = 100 * time.Millisecond
	}
	if cfg.FadeDrain == 0 {
		cfg.FadeDrain = 200 * time.Millisecond
	}
	bin, err := exec.LookPath(cfg.Binary)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMPVNotFound, err)
	}
	dir, owns, err := privateDir(cfg.Getenv)
	if err != nil {
		return nil, err
	}
	m := &MPV{
		cfg:     cfg,
		logger:  cfg.Logger,
		dir:     dir,
		ownsDir: owns,
		socket:  filepath.Join(dir, fmt.Sprintf("mpv-%d.sock", os.Getpid())),
		exited:  make(chan struct{}),
		entries: map[int64]mpvEntry{},
		early:   map[int64][]ipcEvent{},
		qsig:    make(chan struct{}, 1),
		events:  make(chan Event, 16),
		stop:    make(chan struct{}),
	}
	if len(m.socket) > maxSocketPath {
		m.removeDir()
		return nil, fmt.Errorf("player: socket path %q is too long", m.socket)
	}
	_ = os.Remove(m.socket) // stale socket from a crashed run

	m.cmd = exec.Command(bin, mpvArgs(cfg.Mode, cfg.Device, m.socket)...)
	configureProcess(m.cmd)
	if err := m.cmd.Start(); err != nil {
		m.removeDir()
		return nil, fmt.Errorf("player: start mpv: %w", err)
	}
	go func() {
		m.waitErr = m.cmd.Wait()
		close(m.exited)
	}()

	dctx, cancel := context.WithTimeout(ctx, cfg.StartTimeout)
	defer cancel()
	m.ipc, err = dialIPC(dctx, m.socket, m.exited)
	if err == nil {
		err = m.setup(dctx)
	}
	if err != nil {
		m.kill()
		m.removeDir()
		return nil, err
	}

	m.wg.Add(3)
	go m.eventLoop()
	go m.forward()
	go m.poll()
	m.logger.Info("mpv started", "mode", cfg.Mode, "device", cfg.Device, "pid", m.cmd.Process.Pid)
	return m, nil
}

func (m *MPV) setup(ctx context.Context) error {
	for i, prop := range []string{"audio-params", "audio-out-params", "volume", "af"} {
		if _, err := m.ipc.command(ctx, "observe_property", i+1, prop); err != nil {
			return fmt.Errorf("player: mpv setup: %w", err)
		}
	}
	if _, err := m.ipc.command(ctx, "request_log_messages", "warn"); err != nil {
		return fmt.Errorf("player: mpv setup: %w", err)
	}
	return nil
}

// Events implements Engine.
func (m *MPV) Events() <-chan Event { return m.events }

// Play implements Engine.
func (m *MPV) Play(ctx context.Context, item Item) error {
	file, err := m.writeMPD(item)
	if err != nil {
		return err
	}
	args := []any{"loadfile", file, "replace"}
	if item.Start > 0 {
		args = append(args, -1, "start="+strconv.FormatFloat(item.Start.Seconds(), 'f', 3, 64))
	}
	id, err := m.loadfile(ctx, args)
	if err != nil {
		_ = os.Remove(file)
		return err
	}
	m.mu.Lock()
	// replace drops entries that never started; the playing one gets an
	// end-file event that removes its file.
	for eid, e := range m.entries {
		if eid != m.current {
			_ = os.Remove(e.file)
			delete(m.entries, eid)
		}
	}
	m.pending = 0
	m.mu.Unlock()
	m.register(id, item.Key, file)
	_, err = m.ipc.command(ctx, "set_property", "pause", false)
	return err
}

// register records a playlist entry and replays any events mpv sent for it
// before the loadfile reply was processed.
func (m *MPV) register(id int64, key uint64, file string) {
	m.mu.Lock()
	m.entries[id] = mpvEntry{key: key, file: file}
	m.maxKnown = max(m.maxKnown, id)
	early := m.early[id]
	delete(m.early, id)
	for eid := range m.early {
		if eid < id { // belonged to a load that failed
			delete(m.early, eid)
		}
	}
	m.mu.Unlock()
	for _, ev := range early {
		m.handle(ev)
	}
}

// Preload implements Engine.
func (m *MPV) Preload(ctx context.Context, item Item) error {
	if _, err := m.ipc.command(ctx, "playlist-clear"); err != nil {
		return err
	}
	m.dropPending()
	file, err := m.writeMPD(item)
	if err != nil {
		return err
	}
	id, err := m.loadfile(ctx, []any{"loadfile", file, "append"})
	if err != nil {
		_ = os.Remove(file)
		return err
	}
	m.mu.Lock()
	m.pending = id
	m.mu.Unlock()
	m.register(id, item.Key, file)
	return nil
}

// ClearPreload implements Engine.
func (m *MPV) ClearPreload(ctx context.Context) error {
	_, err := m.ipc.command(ctx, "playlist-clear")
	m.dropPending()
	return err
}

func (m *MPV) dropPending() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[m.pending]; ok && m.pending != m.current {
		_ = os.Remove(e.file)
		delete(m.entries, m.pending)
	}
	m.pending = 0
}

func (m *MPV) loadfile(ctx context.Context, args []any) (int64, error) {
	data, err := m.ipc.command(ctx, args...)
	if err != nil {
		return 0, err
	}
	var r struct {
		ID int64 `json:"playlist_entry_id"`
	}
	if json.Unmarshal(data, &r) != nil || r.ID == 0 {
		return 0, errors.New("player: mpv did not return a playlist entry id (mpv 0.38 or newer is required)")
	}
	return r.ID, nil
}

// SetPaused implements Engine.
func (m *MPV) SetPaused(ctx context.Context, paused bool) error {
	_, err := m.ipc.command(ctx, "set_property", "pause", paused)
	return err
}

// SetVolume implements Engine.
func (m *MPV) SetVolume(ctx context.Context, percent float64) error {
	if m.cfg.Mode == config.AudioExclusive {
		return ErrVolumeDisabled
	}
	if percent < 0 || percent > 100 {
		return fmt.Errorf("player: volume %.0f is outside 0-100", percent)
	}
	_, err := m.ipc.command(ctx, "set_property", "volume", percent)
	return err
}

// Stop implements Engine.
func (m *MPV) Stop(ctx context.Context) error {
	_, err := m.ipc.command(ctx, "stop")
	m.dropPending()
	return err
}

// Close implements Engine. It fades out a playing track, asks mpv to
// quit, kills it if it has not exited within QuitTimeout, and removes the
// socket and manifest files.
func (m *MPV) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closing = true
		m.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.QuitTimeout)
		defer cancel()
		// The fade has its own budget, so quit keeps time when mpv stalls.
		fadeCtx, cancelFade := context.WithTimeout(ctx, m.cfg.FadeOut+m.cfg.FadeDrain+fadeSlack)
		m.fadeOut(fadeCtx)
		cancelFade()
		_, _ = m.ipc.command(ctx, "quit")
		select {
		case <-m.exited:
		case <-ctx.Done():
			m.kill()
		}
		close(m.stop)
		m.ipc.close()
		m.wg.Wait()
		m.mu.Lock()
		for _, e := range m.entries {
			_ = os.Remove(e.file)
		}
		m.entries = map[int64]mpvEntry{}
		m.mu.Unlock()
		m.removeDir()
		m.logger.Info("mpv stopped")
	})
	return nil
}

// fadeSteps is how many volume steps the quit fade takes; fadeSlack is
// the extra time the fade's IPC round trips may use.
const (
	fadeSteps = 20
	fadeSlack = 250 * time.Millisecond
)

// fadeOut silences a playing track before quit: it ramps the volume to
// zero (linear in amplitude; mpv's volume is cubic), waits for the device
// buffer to drain, then pauses. Nothing happens when mpv is idle or paused.
// On any IPC error it stops, and quit goes ahead.
func (m *MPV) fadeOut(ctx context.Context) {
	data, err := m.ipc.command(ctx, "get_property", "core-idle")
	var idle bool
	if err != nil || json.Unmarshal(data, &idle) != nil || idle {
		return
	}
	var vol float64
	data, err = m.ipc.command(ctx, "get_property", "volume")
	if err != nil || json.Unmarshal(data, &vol) != nil {
		// Without the level a ramp could jump up: mute in one step instead.
		if _, err := m.ipc.command(ctx, "set_property", "mute", true); err != nil {
			return
		}
	} else {
		step := m.cfg.FadeOut / fadeSteps
		for i := fadeSteps - 1; i >= 0; i-- {
			v := vol * math.Cbrt(float64(i)/fadeSteps)
			if _, err := m.ipc.command(ctx, "set_property", "volume", v); err != nil {
				return
			}
			if !sleep(ctx, step) {
				return
			}
		}
	}
	if !sleep(ctx, m.cfg.FadeDrain) {
		return
	}
	_, _ = m.ipc.command(ctx, "set_property", "pause", true)
}

// sleep waits d, or reports false if ctx ends first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *MPV) kill() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		<-m.exited
	}
}

func (m *MPV) removeDir() {
	_ = os.Remove(m.socket)
	if m.ownsDir {
		_ = os.RemoveAll(m.dir)
	}
}

// writeMPD stores the manifest in a 0600 file in the private directory, so
// signed URLs never appear on a command line.
func (m *MPV) writeMPD(item Item) (string, error) {
	m.mu.Lock()
	m.seq++
	name := filepath.Join(m.dir, fmt.Sprintf("track-%d-%d.mpd", os.Getpid(), m.seq))
	m.mu.Unlock()
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("player: write manifest: %w", err)
	}
	if _, err := f.Write(item.Manifest.MPD); err != nil {
		f.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("player: write manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("player: write manifest: %w", err)
	}
	return name, nil
}

// emit queues an event without blocking the IPC reader. Consecutive
// progress events collapse into the latest one.
func (m *MPV) emit(e Event) {
	m.qmu.Lock()
	if n := len(m.queue); n > 0 && e.Kind == EventProgress && m.queue[n-1].Kind == EventProgress {
		m.queue[n-1] = e
	} else {
		m.queue = append(m.queue, e)
	}
	m.qmu.Unlock()
	select {
	case m.qsig <- struct{}{}:
	default:
	}
}

// forward delivers queued events in order and closes Events at the end.
func (m *MPV) forward() {
	defer m.wg.Done()
	defer close(m.events)
	for {
		m.qmu.Lock()
		batch := m.queue
		m.queue = nil
		m.qmu.Unlock()
		for _, e := range batch {
			select {
			case m.events <- e:
			case <-m.stop:
				return
			}
		}
		if len(batch) > 0 && batch[len(batch)-1].Kind == EventFailed {
			return
		}
		select {
		case <-m.qsig:
		case <-m.stop:
			return
		}
	}
}

func (m *MPV) eventLoop() {
	defer m.wg.Done()
	for {
		select {
		case ev := <-m.ipc.events:
			m.handle(ev)
		case <-m.ipc.done:
			m.failed()
			return
		case <-m.exited:
			m.failed()
			return
		}
	}
}

// failed reports an unexpected exit (not during Close).
func (m *MPV) failed() {
	m.mu.Lock()
	closing := m.closing
	m.mu.Unlock()
	if closing {
		return
	}
	m.kill()
	err := ErrEngineFailed
	if m.waitErr != nil {
		err = fmt.Errorf("%w: %w", ErrEngineFailed, m.waitErr)
	}
	m.logger.Error("mpv exited unexpectedly", "err", err)
	m.emit(Event{Kind: EventFailed, Err: err})
}

func (m *MPV) handle(ev ipcEvent) {
	if ev.Event == "start-file" || ev.Event == "end-file" {
		m.mu.Lock()
		_, known := m.entries[ev.PlaylistEntryID]
		if !known && ev.PlaylistEntryID > m.maxKnown {
			m.early[ev.PlaylistEntryID] = append(m.early[ev.PlaylistEntryID], ev)
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
	}
	switch ev.Event {
	case "start-file":
		m.mu.Lock()
		e, ok := m.entries[ev.PlaylistEntryID]
		m.current = ev.PlaylistEntryID
		if m.pending == ev.PlaylistEntryID {
			m.pending = 0
		}
		m.lastPos = 0
		m.mu.Unlock()
		if ok {
			m.emit(Event{Kind: EventStarted, Key: e.key})
		}
	case "end-file":
		m.mu.Lock()
		e, ok := m.entries[ev.PlaylistEntryID]
		delete(m.entries, ev.PlaylistEntryID)
		if m.current == ev.PlaylistEntryID {
			m.current = 0
		}
		pos, busy := m.lastPos, m.busySeen
		m.busySeen = false // the flag belongs to the load that just ended
		m.mu.Unlock()
		if !ok {
			return
		}
		_ = os.Remove(e.file)
		out := Event{Kind: EventEnded, Key: e.key, Position: pos}
		switch ev.Reason {
		case "eof":
			out.Reason = EndEOF
		case "error":
			out.Reason = EndError
			out.Err = classifyFileError(ev.FileError, busy)
		default:
			out.Reason = EndStopped
		}
		m.emit(out)
	case "property-change":
		m.mu.Lock()
		switch ev.Name {
		case "audio-params":
			m.format.Decoded = decodeParams(ev.Data)
		case "audio-out-params":
			m.format.Output = decodeParams(ev.Data)
		case "volume":
			_ = json.Unmarshal(ev.Data, &m.format.Volume)
		case "af":
			var filters []json.RawMessage
			_ = json.Unmarshal(ev.Data, &filters)
			m.format.Filters = len(filters)
		}
		f := m.format
		m.mu.Unlock()
		f.Kind = EventFormat
		m.emit(f)
	case "log-message":
		text := strings.TrimSpace(ev.Text)
		if strings.Contains(text, "Device or resource busy") {
			m.mu.Lock()
			m.busySeen = true
			m.mu.Unlock()
		}
		m.logger.Debug("mpv", "prefix", ev.Prefix, "level", ev.Level, "text", redact(text))
	}
}

func decodeParams(raw json.RawMessage) AudioParams {
	var p AudioParams
	_ = json.Unmarshal(raw, &p)
	return p
}

func classifyFileError(fileError string, busy bool) error {
	switch {
	case busy:
		return ErrDeviceBusy
	case strings.Contains(fileError, "audio output"):
		return fmt.Errorf("%w: %s", ErrOutputFailed, redact(fileError))
	default:
		return fmt.Errorf("%w: %s", ErrStreamFailed, redact(fileError))
	}
}

// poll samples the position of the current item.
func (m *MPV) poll() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		m.mu.Lock()
		e, ok := m.entries[m.current]
		m.mu.Unlock()
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.PollInterval)
		pos, perr := m.seconds(ctx, "time-pos")
		dur, _ := m.seconds(ctx, "duration")
		cancel()
		if perr != nil {
			continue
		}
		m.mu.Lock()
		m.lastPos = pos
		m.mu.Unlock()
		m.emit(Event{Kind: EventProgress, Key: e.key, Position: pos, Duration: dur})
	}
}

func (m *MPV) seconds(ctx context.Context, prop string) (time.Duration, error) {
	data, err := m.ipc.command(ctx, "get_property", prop)
	if err != nil {
		return 0, err
	}
	var s float64
	if err := json.Unmarshal(data, &s); err != nil {
		return 0, err
	}
	return time.Duration(s * float64(time.Second)), nil
}

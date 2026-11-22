package player

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/tidal"
)

func TestMain(m *testing.M) {
	if os.Getenv("FAKE_MPV") == "1" {
		fakeMPVMain()
		return
	}
	os.Exit(m.Run())
}

// shortDir returns a short private directory (socket paths are limited).
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "tt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

type fakeEnv struct {
	runtime string
	log     string
}

// startFake launches the fake mpv in mode and returns the engine.
func startFake(t *testing.T, mode string, mutate ...func(*MPVConfig)) (*MPV, fakeEnv, error) {
	t.Helper()
	env := fakeEnv{runtime: shortDir(t)}
	env.log = filepath.Join(env.runtime, "fake.log")
	t.Setenv("FAKE_MPV", "1")
	t.Setenv("FAKE_MPV_MODE", mode)
	t.Setenv("FAKE_MPV_LOG", env.log)
	// A -race build sleeps 1 s at exit by default; the fake mpv must quit
	// promptly like the real one, or Close would always kill it.
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	cfg := MPVConfig{
		Binary:       os.Args[0],
		Mode:         config.AudioShared,
		Device:       "auto",
		Getenv:       func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": env.runtime}[k] },
		StartTimeout: 2 * time.Second,
		PollInterval: 20 * time.Millisecond,
		QuitTimeout:  300 * time.Millisecond,
	}
	for _, f := range mutate {
		f(&cfg)
	}
	m, err := StartMPV(context.Background(), cfg)
	if err == nil {
		t.Cleanup(func() { m.Close() })
	}
	return m, env, err
}

func item(key uint64) Item {
	return Item{Key: key, Manifest: tidal.Manifest{TrackID: "1", MPD: []byte("<MPD/>")}}
}

// collect reads events until stop returns true or the timeout passes.
func collect(t *testing.T, m *MPV, stop func(Event) bool) []Event {
	t.Helper()
	var got []Event
	timeout := time.After(3 * time.Second)
	for {
		select {
		case e, ok := <-m.Events():
			if !ok {
				return got
			}
			if e.Kind == EventProgress || e.Kind == EventFormat {
				got = append(got, e)
				continue
			}
			got = append(got, e)
			if stop(e) {
				return got
			}
		case <-timeout:
			t.Fatalf("timed out; events so far: %+v", got)
		}
	}
}

func lifecycle(events []Event) []string {
	var out []string
	for _, e := range events {
		switch e.Kind {
		case EventStarted:
			out = append(out, "start", keyStr(e.Key))
		case EventEnded:
			out = append(out, "end", keyStr(e.Key), []string{"", "eof", "stopped", "error"}[e.Reason])
		case EventFailed:
			out = append(out, "failed")
		}
	}
	return out
}

func keyStr(k uint64) string { return string(rune('A' + k - 1)) }

func fakeLog(t *testing.T, env fakeEnv) string {
	t.Helper()
	b, _ := os.ReadFile(env.log)
	return string(b)
}

func TestMPVArgs(t *testing.T) {
	tests := []struct {
		name    string
		mode    config.AudioMode
		device  string
		want    []string
		without []string
	}{
		{"exclusive", config.AudioExclusive, "alsa/hw:CARD=Vestri,DEV=0",
			[]string{"--ao=alsa", "--audio-device=alsa/hw:CARD=Vestri,DEV=0", "--audio-exclusive=yes"}, []string{"--audio-exclusive=no", "--ao=pipewire"}},
		{"shared default device", config.AudioShared, "auto", []string{"--ao=pipewire", "--audio-exclusive=no"}, []string{"--ao=alsa", "--audio-device=auto"}},
		{"shared named device", config.AudioShared, "pipewire/alsa_output.usb-x", []string{"--ao=pipewire", "--audio-device=pipewire/alsa_output.usb-x"}, []string{"--ao=alsa"}},
	}
	always := []string{"--idle=yes", "--no-config", "--load-scripts=no", "--input-ipc-server=/s", "--gapless-audio=weak",
		"--prefetch-playlist=yes", "--volume=100", "--volume-max=100", "--replaygain=no", "--af=",
		"--demuxer-lavf-o=protocol_whitelist=[file,http,https,tls,tcp,crypto,data]"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := mpvArgs(tt.mode, tt.device, "/s")
			for _, w := range append(always, tt.want...) {
				if !slices.Contains(args, w) {
					t.Errorf("missing %s in %v", w, args)
				}
			}
			for _, w := range append(tt.without, "--gapless-audio=yes") {
				if slices.Contains(args, w) {
					t.Errorf("unexpected %s", w)
				}
			}
		})
	}
}

func TestMPVPlaysAndReportsEvents(t *testing.T) {
	m, env, err := startFake(t, "normal")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Play(context.Background(), Item{Key: 1, Manifest: tidal.Manifest{MPD: []byte("<MPD/>")}, Start: 12500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	events := collect(t, m, func(e Event) bool { return e.Kind == EventEnded })

	if got := lifecycle(events); !slices.Equal(got, []string{"start", "A", "end", "A", "eof"}) {
		t.Errorf("lifecycle = %v", got)
	}
	var sawFormat, sawProgress bool
	for _, e := range events {
		if e.Kind == EventFormat && e.Output.SampleRate == 192000 && e.Decoded.Format == "s32" && e.Volume == 100 {
			sawFormat = true
		}
		if e.Kind == EventProgress && e.Key == 1 && e.Position > 12*time.Second && e.Duration == 29994*time.Millisecond {
			sawProgress = true
		}
	}
	if !sawFormat || !sawProgress {
		t.Errorf("format %v progress %v in %+v", sawFormat, sawProgress, events)
	}
	if end := events[len(events)-1]; end.Position < 12*time.Second {
		t.Errorf("end position = %s, want the last sampled position", end.Position)
	}
	log := fakeLog(t, env)
	if !strings.Contains(log, `"replace",-1,"start=12.500"`) || !strings.Contains(log, `"set_property","pause",false`) {
		t.Errorf("commands:\n%s", log)
	}
	if strings.Contains(log, "<MPD/>") || strings.Contains(fakeArgv(log), ".mpd") {
		t.Error("manifest content or path leaked onto the command line")
	}
	waitFor(t, func() bool { return countMPDs(env.runtime) == 0 })
}

func fakeArgv(log string) string {
	for line := range strings.SplitSeq(log, "\n") {
		if strings.HasPrefix(line, "argv ") {
			return line
		}
	}
	return ""
}

func countMPDs(dir string) int {
	n := 0
	entries, _ := os.ReadDir(filepath.Join(dir, "needle"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".mpd") {
			n++
		}
	}
	return n
}

func TestMPVGaplessPreload(t *testing.T) {
	m, env, err := startFake(t, "normal")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Play(ctx, item(1)); err != nil {
		t.Fatal(err)
	}
	if err := m.Preload(ctx, item(2)); err != nil {
		t.Fatal(err)
	}
	// A second preload replaces the first.
	if err := m.Preload(ctx, item(3)); err != nil {
		t.Fatal(err)
	}
	if n := countMPDs(env.runtime); n != 2 {
		t.Errorf("manifest files = %d, want 2 (current and the one preloaded item)", n)
	}
	ends := 0
	events := collect(t, m, func(e Event) bool {
		if e.Kind == EventEnded {
			ends++
		}
		return ends == 2
	})
	if got := lifecycle(events); !slices.Equal(got, []string{"start", "A", "end", "A", "eof", "start", "C", "end", "C", "eof"}) {
		t.Errorf("lifecycle = %v", got)
	}
}

func TestMPVStopAndReplace(t *testing.T) {
	t.Setenv("FAKE_MPV_TRACK_MS", "5000")
	m, _, err := startFake(t, "normal")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Play(ctx, item(1)); err != nil {
		t.Fatal(err)
	}
	collect(t, m, func(e Event) bool { return e.Kind == EventStarted })
	if err := m.Play(ctx, item(2)); err != nil {
		t.Fatal(err)
	}
	events := collect(t, m, func(e Event) bool { return e.Kind == EventStarted })
	if got := lifecycle(events); !slices.Equal(got, []string{"end", "A", "stopped", "start", "B"}) {
		t.Errorf("replace lifecycle = %v", got)
	}
	if err := m.Preload(ctx, item(3)); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	events = collect(t, m, func(e Event) bool { return e.Kind == EventEnded })
	if got := lifecycle(events); !slices.Equal(got, []string{"end", "B", "stopped"}) {
		t.Errorf("stop lifecycle = %v", got)
	}
}

func TestMPVVolume(t *testing.T) {
	m, env, err := startFake(t, "normal")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.SetVolume(ctx, 40); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []float64{-1, 101} {
		if err := m.SetVolume(ctx, bad); err == nil {
			t.Errorf("volume %v accepted", bad)
		}
	}
	if !strings.Contains(fakeLog(t, env), `"set_property","volume",40`) {
		t.Error("volume command not sent")
	}

	ex, _, err := startFake(t, "normal", func(c *MPVConfig) { c.Mode = config.AudioExclusive; c.Device = "alsa/hw:CARD=X,DEV=0" })
	if err != nil {
		t.Fatal(err)
	}
	// The Player decides whether software volume is allowed (decision 84);
	// the engine applies it in exclusive mode too.
	if err := ex.SetVolume(ctx, 40); err != nil {
		t.Errorf("exclusive SetVolume err = %v", err)
	}
}

func TestMPVPlaybackErrors(t *testing.T) {
	tests := []struct {
		mode string
		is   error
	}{
		{"busy", ErrDeviceBusy},
		{"stream-error", ErrStreamFailed},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			m, _, err := startFake(t, tt.mode)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Play(context.Background(), item(1)); err != nil {
				t.Fatal(err)
			}
			events := collect(t, m, func(e Event) bool { return e.Kind == EventEnded })
			end := events[len(events)-1]
			if end.Reason != EndError || !errors.Is(end.Err, tt.is) {
				t.Errorf("end = %+v, want error %v", end, tt.is)
			}
			if strings.Contains(end.Err.Error(), "SECRETSIG") || strings.Contains(end.Err.Error(), "tidal.com") {
				t.Errorf("error leaks a URL: %v", end.Err)
			}
		})
	}
}

func TestMPVCrashIsReported(t *testing.T) {
	m, _, err := startFake(t, "crash-on-load")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Play(context.Background(), item(1)); err == nil {
		t.Error("Play succeeded against a crashed mpv")
	}
	events := collect(t, m, func(e Event) bool { return e.Kind == EventFailed })
	if last := events[len(events)-1]; !errors.Is(last.Err, ErrEngineFailed) {
		t.Errorf("failure = %+v", last)
	}
	select {
	case _, ok := <-m.Events():
		if ok {
			t.Error("events not closed after failure")
		}
	case <-time.After(time.Second):
		t.Error("events channel left open after failure")
	}
	if err := m.Play(context.Background(), item(2)); !errors.Is(err, ErrIPCClosed) {
		t.Errorf("Play after crash err = %v", err)
	}
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung after a crash")
	}
}

func TestMPVStartupFailures(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		mutate  func(*MPVConfig)
		is      error
		wantErr string
	}{
		{name: "binary missing", mutate: func(c *MPVConfig) { c.Binary = "mpv-does-not-exist-xyz" }, is: ErrMPVNotFound},
		{name: "exits before socket", mode: "nosocket", wantErr: "exited during startup"},
		{name: "never opens socket", mode: "hang", mutate: func(c *MPVConfig) { c.StartTimeout = 200 * time.Millisecond }, is: context.DeadlineExceeded},
		{name: "socket path too long", mutate: func(c *MPVConfig) {
			long := filepath.Join(os.TempDir(), strings.Repeat("d", 90))
			c.Getenv = func(string) string { return long }
		}, wantErr: "too long"},
		{name: "runtime dir is a file", mutate: func(c *MPVConfig) {
			d, _ := os.MkdirTemp("", "tt")
			os.WriteFile(filepath.Join(d, "needle"), nil, 0o600)
			c.Getenv = func(string) string { return d }
		}, wantErr: "runtime dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode := tt.mode
			if mode == "" {
				mode = "normal"
			}
			mutate := tt.mutate
			if mutate == nil {
				mutate = func(*MPVConfig) {}
			}
			start := time.Now()
			_, _, err := startFake(t, mode, mutate)
			if err == nil {
				t.Fatal("StartMPV succeeded")
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("err = %v, want %v", err, tt.is)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
			if time.Since(start) > 3*time.Second {
				t.Error("startup failure took too long")
			}
		})
	}
}

func TestMPVOldVersion(t *testing.T) {
	m, _, err := startFake(t, "old-mpv")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Play(context.Background(), item(1)); err == nil || !strings.Contains(err.Error(), "0.38") {
		t.Errorf("err = %v", err)
	}
	if err := m.Preload(context.Background(), item(2)); err == nil {
		t.Error("Preload succeeded on old mpv")
	}
}

func TestMPVCloseKillsUnresponsiveProcess(t *testing.T) {
	m, env, err := startFake(t, "ignore-quit")
	if err != nil {
		t.Fatal(err)
	}
	pid := m.cmd.Process.Pid
	start := time.Now()
	m.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Close took %s", d)
	}
	if err := m.cmd.Process.Signal(os.Signal(nil)); err == nil {
		t.Errorf("process %d still running", pid)
	}
	if _, err := os.Stat(m.socket); !os.IsNotExist(err) {
		t.Error("socket left behind")
	}
	if _, ok := <-m.Events(); ok {
		t.Error("events not closed")
	}
	m.Close() // idempotent
	_ = env
}

func TestPrivateDir(t *testing.T) {
	rd := shortDir(t)
	if err := os.Mkdir(filepath.Join(rd, "needle"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, owned, err := privateDir(func(string) string { return rd })
	if err != nil || owned || dir != filepath.Join(rd, "needle") {
		t.Fatalf("privateDir = %s %v %v", dir, owned, err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %o, want tightened to 700", info.Mode().Perm())
	}

	dir, owned, err = privateDir(func(string) string { return "relative/path" })
	if err != nil || !owned {
		t.Fatalf("fallback = %s %v %v", dir, owned, err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("temp mode = %o", info.Mode().Perm())
	}
	os.RemoveAll(dir)

	link := shortDir(t)
	if err := os.Symlink(rd, filepath.Join(link, "needle")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := privateDir(func(string) string { return link }); err == nil {
		t.Error("symlinked runtime dir accepted")
	}
}

func TestTempRuntimeDirIsRemovedOnClose(t *testing.T) {
	m, _, err := startFake(t, "normal", func(c *MPVConfig) { c.Getenv = func(string) string { return "" } })
	if err != nil {
		t.Fatal(err)
	}
	dir := m.dir
	m.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("temp runtime dir %s not removed", dir)
	}
}

func TestRedactAndClassify(t *testing.T) {
	got := redact("open https://a.example/x?Signature=S3 and file:///tmp/a.mpd " + strings.Repeat("x", 400))
	if strings.Contains(got, "Signature") || strings.Contains(got, "/tmp/a.mpd") || len([]rune(got)) > 303 {
		t.Errorf("redact = %q", got)
	}
	if !errors.Is(classifyFileError("audio output initialization failed", false), ErrOutputFailed) {
		t.Error("audio output error misclassified")
	}
	if !errors.Is(classifyFileError("audio output initialization failed", true), ErrDeviceBusy) {
		t.Error("busy not detected")
	}
	if !errors.Is(classifyFileError("loading failed", false), ErrStreamFailed) {
		t.Error("stream error misclassified")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMPVClearPreloadAndWriteFailure(t *testing.T) {
	m, env, err := startFake(t, "normal")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Play(ctx, item(1)); err != nil {
		t.Fatal(err)
	}
	if err := m.Preload(ctx, item(2)); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearPreload(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countMPDs(env.runtime); n != 1 {
		t.Errorf("manifest files after ClearPreload = %d, want 1", n)
	}
	events := collect(t, m, func(e Event) bool { return e.Kind == EventEnded })
	waitFor(t, func() bool { return countMPDs(env.runtime) == 0 })
	if got := lifecycle(events); !slices.Equal(got, []string{"start", "A", "end", "A", "eof"}) {
		t.Errorf("lifecycle = %v", got)
	}

	// The private directory disappears: manifests cannot be written.
	if err := os.Chmod(m.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(m.dir, 0o700)
	if os.Geteuid() != 0 {
		if err := m.Play(ctx, item(3)); err == nil || !strings.Contains(err.Error(), "write manifest") {
			t.Errorf("Play err = %v", err)
		}
		if err := m.Preload(ctx, item(4)); err == nil {
			t.Error("Preload succeeded without a manifest")
		}
	}
}

func TestCheckPrivateRejectsForeignDirs(t *testing.T) {
	if err := checkPrivate(filepath.Join(shortDir(t), "missing")); err == nil {
		t.Error("missing dir accepted")
	}
	if os.Geteuid() != 0 {
		if err := checkPrivate("/"); err == nil || !strings.Contains(err.Error(), "another user") {
			t.Errorf("root-owned dir err = %v", err)
		}
	}
	file := filepath.Join(shortDir(t), "f")
	os.WriteFile(file, nil, 0o600)
	if _, _, err := privateDir(func(string) string { return file }); err == nil {
		t.Error("runtime dir under a file accepted")
	}
}

func TestMPVBusyFlagIsPerLoad(t *testing.T) {
	m, _, err := startFake(t, "busy-then-fail")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []error{ErrDeviceBusy, ErrOutputFailed} {
		if err := m.Play(context.Background(), item(uint64(i+1))); err != nil {
			t.Fatal(err)
		}
		events := collect(t, m, func(e Event) bool { return e.Kind == EventEnded })
		if end := events[len(events)-1]; !errors.Is(end.Err, want) {
			t.Errorf("load %d: err = %v, want %v", i+1, end.Err, want)
		}
	}
}

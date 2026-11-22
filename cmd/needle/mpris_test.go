package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	osexec "os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/five07/needle/internal/mpris"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tui"
	"github.com/godbus/dbus/v5"
)

// stubVolume records volume calls and returns err.
type stubVolume struct {
	set    []float64
	deltas []float64
	err    error
}

func (v *stubVolume) AdjustVolume(_ context.Context, d float64) (float64, error) {
	v.deltas = append(v.deltas, d)
	return 95, v.err
}
func (v *stubVolume) SetVolume(_ context.Context, p float64) error {
	v.set = append(v.set, p)
	return v.err
}

// stubSeeker records seeks and returns err.
type stubSeeker struct {
	calls []string
	err   error
}

func (s *stubSeeker) SeekBy(_ context.Context, d time.Duration) (time.Duration, error) {
	s.calls = append(s.calls, "by "+d.String())
	return time.Minute + d, s.err
}
func (s *stubSeeker) SeekTo(_ context.Context, h queue.Handle, d time.Duration) (time.Duration, error) {
	s.calls = append(s.calls, fmt.Sprintf("to %d %s", h, d))
	return d, s.err
}

// TASK-019: the seek keys and MPRIS reach the player; nothing to seek in
// is silent for the keys and ignored over MPRIS (decisions 93 and 95).
func TestSeekControls(t *testing.T) {
	ctx := context.Background()
	sk := &stubSeeker{}
	b := &playback{q: queue.New[player.Track](), p: &stubTransport{}, sk: sk}
	c := mprisControls{b}
	if pos, err := b.SeekBy(ctx, 5*time.Second); err != nil || pos != 65*time.Second {
		t.Errorf("keys: %v %v", pos, err)
	}
	if err := c.Seek(ctx, -10*time.Second); err != nil {
		t.Errorf("mpris seek: %v", err)
	}
	if err := c.SetPosition(ctx, 4, 90*time.Second); err != nil {
		t.Errorf("mpris set position: %v", err)
	}
	if want := []string{"by 5s", "by -10s", "to 4 1m30s"}; !reflect.DeepEqual(sk.calls, want) {
		t.Errorf("calls %v, want %v", sk.calls, want)
	}
	sk.err = player.ErrNotSeekable
	if _, err := b.SeekBy(ctx, time.Second); !errors.Is(err, tui.ErrNotSeekable) {
		t.Errorf("keys, not seekable: %v", err)
	}
	if c.Seek(ctx, time.Second) != nil || c.SetPosition(ctx, 4, time.Second) != nil {
		t.Error("mpris, not seekable: error returned")
	}
	other := errors.New("ipc")
	sk.err = other
	if _, err := b.SeekBy(ctx, time.Second); !errors.Is(err, other) {
		t.Errorf("keys, failure: %v", err)
	}
	if !errors.Is(c.Seek(ctx, time.Second), other) || !errors.Is(c.SetPosition(ctx, 4, time.Second), other) {
		t.Error("mpris, failure not returned")
	}
	// Without a seeker nothing happens.
	b.sk = nil
	if _, err := b.SeekBy(ctx, time.Second); !errors.Is(err, tui.ErrNotSeekable) {
		t.Errorf("no seeker: %v", err)
	}
	if c.Seek(ctx, time.Second) != nil || c.SetPosition(ctx, 4, time.Second) != nil {
		t.Error("no seeker: error returned")
	}
	// The player's seek count reaches MPRIS, which announces it.
	if st := mprisState(tui.NowPlaying{}, player.Status{SeekID: 3}); st.SeekID != 3 || st.CanSeek {
		t.Errorf("state %+v", st)
	}
}

// TASK-018: the volume keys and MPRIS reach the player, and a refusal
// (volume on the output device) becomes each side's own error.
func TestVolumeControls(t *testing.T) {
	ctx := context.Background()
	vol := &stubVolume{}
	b := &playback{q: queue.New[player.Track](), p: &stubTransport{}, vol: vol}
	if level, err := b.AdjustVolume(ctx, -5); err != nil || level != 95 {
		t.Errorf("adjust: %v %v", level, err)
	}
	c := mprisControls{b}
	if err := c.SetVolume(ctx, 0.426); err != nil || !reflect.DeepEqual(vol.set, []float64{43}) {
		t.Errorf("mpris: %v %v", err, vol.set)
	}
	vol.err = player.ErrVolumeDisabled
	if _, err := b.AdjustVolume(ctx, 5); !errors.Is(err, tui.ErrVolumeOnDevice) {
		t.Errorf("adjust on the device: %v", err)
	}
	if err := c.SetVolume(ctx, 0.5); !errors.Is(err, mpris.ErrVolumeNotSupported) {
		t.Errorf("mpris on the device: %v", err)
	}
	other := errors.New("ipc")
	vol.err = other
	if _, err := b.AdjustVolume(ctx, 5); !errors.Is(err, other) {
		t.Errorf("adjust failure: %v", err)
	}
	if err := c.SetVolume(ctx, 0.5); !errors.Is(err, other) {
		t.Errorf("mpris failure: %v", err)
	}
	// Without a player volume, both refuse.
	none := &playback{q: queue.New[player.Track](), p: &stubTransport{}}
	if _, err := none.AdjustVolume(ctx, 5); !errors.Is(err, tui.ErrVolumeOnDevice) {
		t.Errorf("no volume: %v", err)
	}
	if err := (mprisControls{none}).SetVolume(ctx, 0.5); !errors.Is(err, mpris.ErrVolumeNotSupported) {
		t.Errorf("no volume over mpris: %v", err)
	}
}

func TestMPRISControls(t *testing.T) {
	ctx := context.Background()
	q := queue.New[player.Track]()
	q.Set([]player.Track{{ID: "1"}}, 0)
	tr := &stubTransport{}
	c := mprisControls{&playback{q: q, p: tr}}

	// Stopped: Play and PlayPause start the current track.
	tr.state = player.Stopped
	c.Play(ctx)
	c.PlayPause(ctx)
	if tr.plays != 2 || tr.pauses != 0 || len(tr.setPaused) != 0 {
		t.Errorf("stopped: %+v", tr)
	}
	// Paused or playing: Play resumes, Pause pauses, PlayPause toggles.
	tr.state = player.Paused
	c.Play(ctx)
	c.Pause(ctx)
	c.PlayPause(ctx)
	if tr.plays != 2 || tr.pauses != 1 || !reflect.DeepEqual(tr.setPaused, []bool{false, true}) {
		t.Errorf("paused: %+v", tr)
	}
	c.Stop(ctx)
	c.Next(ctx)
	c.Previous(ctx)
	if tr.stops != 1 || tr.nexts != 1 || tr.prevs != 1 {
		t.Errorf("stop/next/prev: %+v", tr)
	}
	c.SetShuffle(ctx, true)
	if !q.Shuffle() {
		t.Error("shuffle")
	}
	for l, want := range map[mpris.Loop]queue.Repeat{mpris.LoopTrack: queue.RepeatOne, mpris.LoopPlaylist: queue.RepeatAll, mpris.LoopNone: queue.RepeatOff} {
		if c.SetLoop(ctx, l); q.Repeat() != want {
			t.Errorf("loop %s: repeat %v", l, q.Repeat())
		}
	}
}

func TestMPRISState(t *testing.T) {
	cur := &tui.QueueItem{Handle: 4, Track: tui.QueueTrack{ID: "9", Title: "T", Artists: "A", Album: "Al", Duration: time.Minute}}
	tests := []struct {
		name string
		np   tui.NowPlaying
		vol  float64
		want mpris.State
	}{
		{"empty", tui.NowPlaying{}, 100, mpris.State{Status: mpris.Stopped, Loop: mpris.LoopNone, Volume: 1}},
		{"empty with repeat on: nothing next", tui.NowPlaying{Repeat: tui.RepeatAll}, 100, mpris.State{Status: mpris.Stopped, Loop: mpris.LoopPlaylist, Volume: 1}},
		{"loading, repeat all wraps", tui.NowPlaying{State: tui.Loading, Current: cur, Repeat: tui.RepeatAll}, 50,
			mpris.State{Status: mpris.Playing, Loop: mpris.LoopPlaylist, Volume: 0.5, CanNext: true, CanPrev: true,
				Track: &mpris.Track{Handle: 4, ID: "9", Title: "T", Artists: "A", Album: "Al", Length: time.Minute}}},
		{"paused, upcoming, player duration", tui.NowPlaying{State: tui.Paused, Current: cur, Upcoming: []tui.QueueItem{{}}, Duration: 2 * time.Minute,
			Position: time.Second, Shuffle: true, Repeat: tui.RepeatOne}, 150,
			mpris.State{Status: mpris.Paused, Loop: mpris.LoopTrack, Volume: 1, CanNext: true, CanPrev: true, CanSeek: true, Position: time.Second, Shuffle: true,
				Track: &mpris.Track{Handle: 4, ID: "9", Title: "T", Artists: "A", Album: "Al", Length: 2 * time.Minute}}},
		{"last track, no repeat", tui.NowPlaying{State: tui.Playing, Current: cur}, -5,
			mpris.State{Status: mpris.Playing, Loop: mpris.LoopNone, CanPrev: true, CanSeek: true,
				Track: &mpris.Track{Handle: 4, ID: "9", Title: "T", Artists: "A", Album: "Al", Length: time.Minute}}},
	}
	for _, tt := range tests {
		if got := mprisState(tt.np, player.Status{Volume: tt.vol}); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", tt.name, got, tt.want)
		}
	}
}

// testBus starts a private dbus-daemon, or skips the test without one.
func testBus(t *testing.T) string {
	t.Helper()
	path, err := osexec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := osexec.Command(path, "--session", "--nofork", "--nosyslog", "--print-address=1")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

func TestStartMPRIS(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	b := &playback{q: queue.New[player.Track](), p: &stubTransport{}}

	// Off (tests): nothing logged.
	if svc, stop := startMPRIS(nil, b, logger); svc != nil || logs.Len() != 0 {
		t.Error("nil connect")
	} else {
		stop()
	}
	// No session bus: logged, and startup goes on (AC #5).
	svc, stop := startMPRIS(func() (*dbus.Conn, error) { return nil, errors.New("no DBUS_SESSION_BUS_ADDRESS") }, b, logger)
	stop()
	if svc != nil || !strings.Contains(logs.String(), "mpris unavailable: no session bus") {
		t.Errorf("no bus: %s", logs.String())
	}
	addr := testBus(t)
	// Registering fails (a closed connection): logged, connection closed.
	logs.Reset()
	svc, stop = startMPRIS(func() (*dbus.Conn, error) {
		c, err := dbus.Connect(addr)
		if err == nil {
			c.Close()
		}
		return c, err
	}, b, logger)
	stop()
	if svc != nil || !strings.Contains(logs.String(), "mpris unavailable") {
		t.Errorf("failed start: %s", logs.String())
	}
	// Both names taken by other copies: logged, and the open connection is
	// closed.
	for range 2 {
		c, err := dbus.Connect(addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		if _, err := mpris.Start(c, mprisControls{b}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var third *dbus.Conn
	logs.Reset()
	svc, _ = startMPRIS(func() (*dbus.Conn, error) {
		var err error
		third, err = dbus.Connect(addr)
		return third, err
	}, b, logger)
	if svc != nil || third.Connected() || !strings.Contains(logs.String(), "taken") {
		t.Errorf("name taken: svc %v connected %v logs %s", svc, third.Connected(), logs.String())
	}
	addr = testBus(t) // a fresh bus for the success case

	// Registered: the name is owned until stop.
	svc, stop = startMPRIS(func() (*dbus.Conn, error) { return dbus.Connect(addr) }, b, logger)
	if svc == nil || svc.Name() != mpris.BusName {
		t.Fatalf("not registered: %s", logs.String())
	}
	client, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	owned := func() bool {
		var ok bool
		client.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, mpris.BusName).Store(&ok)
		return ok
	}
	if !owned() {
		t.Error("name not owned")
	}
	stop()
	if owned() {
		t.Error("name kept after stop")
	}
}

type fakeUpdater struct{ got []mpris.State }

func (f *fakeUpdater) Update(s mpris.State) { f.got = append(f.got, s) }

func TestWatchStatusFeedsMPRIS(t *testing.T) {
	src := &stubStatus{updates: make(chan struct{}), status: &player.Status{State: player.Playing, Volume: 80}}
	q := queue.New[player.Track]()
	q.Set([]player.Track{{ID: "5", Title: "Five"}}, 0)
	up := &fakeUpdater{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchStatus(ctx, src, watch{notices: make(chan string, 1), q: q, mpris: up}); close(done) }()
	src.set("")
	cancel()
	<-done
	if len(up.got) != 1 || up.got[0].Status != mpris.Playing || up.got[0].Track == nil || up.got[0].Track.ID != "5" || up.got[0].Volume != 0.8 {
		t.Errorf("mpris got %+v", up.got)
	}
}

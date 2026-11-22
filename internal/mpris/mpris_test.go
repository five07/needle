package mpris

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a dbus-daemon for the test and returns its address.
// The test is skipped when dbus-daemon is not installed.
func privateBus(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := exec.Command(path, "--session", "--nofork", "--nosyslog", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon address: %v", err)
	}
	return strings.TrimSpace(addr)
}

func connect(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// fakeControls records calls.
type fakeControls struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeControls) rec(c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	return f.err
}
func (f *fakeControls) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}
func (f *fakeControls) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}
func (f *fakeControls) Play(context.Context) error      { return f.rec("Play") }
func (f *fakeControls) Pause(context.Context) error     { return f.rec("Pause") }
func (f *fakeControls) PlayPause(context.Context) error { return f.rec("PlayPause") }
func (f *fakeControls) Stop(context.Context) error      { return f.rec("Stop") }
func (f *fakeControls) Next(context.Context) error      { return f.rec("Next") }
func (f *fakeControls) Previous(context.Context) error  { return f.rec("Previous") }
func (f *fakeControls) SetShuffle(_ context.Context, on bool) error {
	if on {
		return f.rec("Shuffle on")
	}
	return f.rec("Shuffle off")
}
func (f *fakeControls) SetLoop(_ context.Context, l Loop) error { return f.rec("Loop " + string(l)) }
func (f *fakeControls) SetVolume(_ context.Context, v float64) error {
	return f.rec(fmt.Sprintf("Volume %.2f", v))
}
func (f *fakeControls) Seek(_ context.Context, d time.Duration) error {
	return f.rec(fmt.Sprintf("Seek %s", d))
}
func (f *fakeControls) SetPosition(_ context.Context, h uint64, d time.Duration) error {
	return f.rec(fmt.Sprintf("SetPosition %d %s", h, d))
}

type env struct {
	svc    *Service
	ctl    *fakeControls
	client *dbus.Conn
	obj    dbus.BusObject
}

func newEnv(t *testing.T) *env {
	t.Helper()
	addr := privateBus(t)
	ctl := &fakeControls{}
	svc, err := Start(connect(t, addr), ctl, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := connect(t, addr)
	return &env{svc: svc, ctl: ctl, client: client, obj: client.Object(BusName, ObjectPath)}
}

func (e *env) get(t *testing.T, iface, name string) any {
	t.Helper()
	v, err := e.obj.GetProperty(iface + "." + name)
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return v.Value()
}

func TestRegistersAndExposesInterfaces(t *testing.T) {
	e := newEnv(t)
	if e.svc.Name() != BusName {
		t.Errorf("name %q", e.svc.Name())
	}
	tests := []struct {
		iface, name string
		want        any
	}{
		{ifaceRoot, "Identity", "needle"},
		{ifaceRoot, "CanQuit", false},
		{ifaceRoot, "CanRaise", false},
		{ifaceRoot, "HasTrackList", false},
		{ifacePlayer, "CanSeek", false}, // nothing playing
		{ifacePlayer, "CanControl", true},
		{ifacePlayer, "PlaybackStatus", "Stopped"},
		{ifacePlayer, "LoopStatus", "None"},
		{ifacePlayer, "Rate", 1.0},
		{ifacePlayer, "Position", int64(0)},
		{ifacePlayer, "CanPlay", false},
	}
	for _, tt := range tests {
		if got := e.get(t, tt.iface, tt.name); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
		}
	}
	md := e.get(t, ifacePlayer, "Metadata").(map[string]dbus.Variant)
	if md["mpris:trackid"].Value() != noTrack {
		t.Errorf("empty metadata %v", md)
	}
	var xml string
	if err := e.obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{ifaceRoot, ifacePlayer, `name="PlayPause"`, `name="Seeked"`, `name="CanSeek"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("introspection lacks %s", want)
		}
	}
}

func TestSecondInstanceAndClose(t *testing.T) {
	addr := privateBus(t)
	a, err := Start(connect(t, addr), &fakeControls{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Start(connect(t, addr), &fakeControls{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.Name(), BusName+".instance") {
		t.Errorf("second name %q", b.Name())
	}
	// Both names taken by others: Start fails.
	if _, err := Start(connect(t, addr), &fakeControls{}, nil); err == nil || !strings.Contains(err.Error(), "taken") {
		t.Errorf("third start: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := connect(t, addr).BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, BusName).Store(&owner); err == nil {
		t.Errorf("name still owned by %s", owner)
	}
}

func TestStartFailures(t *testing.T) {
	addr := privateBus(t)
	// A closed connection cannot request a name.
	closed, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, err := Start(closed, &fakeControls{}, nil); err == nil {
		t.Error("started on a closed connection")
	}
}

func TestMethods(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"Play", "Pause", "PlayPause", "Stop", "Next", "Previous"} {
		if call := e.obj.Call(ifacePlayer+"."+m, 0); call.Err != nil {
			t.Errorf("%s: %v", m, call.Err)
		}
	}
	// Nothing to seek in, no URIs, no raise or quit: accepted and ignored.
	calls := []*dbus.Call{
		e.obj.Call(ifacePlayer+".Seek", 0, int64(5e6)),
		e.obj.Call(ifacePlayer+".SetPosition", 0, dbus.ObjectPath(trackPrefix+"1"), int64(5e6)),
		e.obj.Call(ifacePlayer+".OpenUri", 0, "https://tidal.com/browse/track/1"),
		e.obj.Call(ifaceRoot+".Raise", 0),
		e.obj.Call(ifaceRoot+".Quit", 0),
	}
	for i, c := range calls {
		if c.Err != nil {
			t.Errorf("call %d: %v", i, c.Err)
		}
	}
	if got, want := e.ctl.got(), []string{"Play", "Pause", "PlayPause", "Stop", "Next", "Previous"}; !reflect.DeepEqual(got, want) {
		t.Errorf("controls %v, want %v", got, want)
	}
	// A failing control returns a generic error, not the details.
	e.ctl.fail(errors.New("mpv ipc socket /run/user/1000/x broke"))
	err := e.obj.Call(ifacePlayer+".Next", 0).Err
	var de dbus.Error
	if !errors.As(err, &de) || de.Name != "org.freedesktop.DBus.Error.Failed" || strings.Contains(err.Error(), "socket") {
		t.Errorf("error %v", err)
	}
}

// TASK-018 decision 87: Volume writes set the software volume, clamped to
// 0-1, and are refused with NotSupported while the volume is on the
// output device.
func TestVolumeWrites(t *testing.T) {
	e := newEnv(t)
	e.svc.Update(State{Volume: 1})
	set := func(v any) error {
		return e.obj.Call("org.freedesktop.DBus.Properties.Set", 0, ifacePlayer, "Volume", dbus.MakeVariant(v)).Err
	}
	for _, v := range []float64{0.5, -0.2, 1.5} {
		if err := set(v); err != nil {
			t.Errorf("set %v: %v", v, err)
		}
	}
	if got, want := e.ctl.got(), []string{"Volume 0.50", "Volume 0.00", "Volume 1.00"}; !reflect.DeepEqual(got, want) {
		t.Errorf("controls %v, want %v", got, want)
	}
	// The next Update publishes the real volume, even when it is the same
	// as the last one published (1.5 was stored, 1 is real).
	e.svc.Update(State{Volume: 1})
	if v := e.get(t, ifacePlayer, "Volume"); v != 1.0 {
		t.Errorf("volume %v after update", v)
	}
	n := len(e.ctl.got())
	if set("loud") == nil || set(math.NaN()) == nil || len(e.ctl.got()) != n {
		t.Error("bad values accepted")
	}
	e.ctl.fail(ErrVolumeNotSupported)
	var derr dbus.Error
	if err := set(0.3); !errors.As(err, &derr) || derr.Name != "org.freedesktop.DBus.Error.NotSupported" {
		t.Errorf("on the device: %v", err)
	}
	e.ctl.fail(errors.New("ipc"))
	if err := set(0.3); !errors.As(err, &derr) || derr.Name != "org.freedesktop.DBus.Error.Failed" {
		t.Errorf("failure: %v", err)
	}
}

func TestWritableProperties(t *testing.T) {
	e := newEnv(t)
	set := func(name string, v any) error {
		return e.obj.Call("org.freedesktop.DBus.Properties.Set", 0, ifacePlayer, name, dbus.MakeVariant(v)).Err
	}
	for _, step := range []struct {
		name string
		v    any
		ok   bool
	}{
		{"Shuffle", true, true},
		{"Shuffle", false, true},
		{"LoopStatus", "Track", true},
		{"LoopStatus", "Playlist", true},
		{"LoopStatus", "None", true},
		{"LoopStatus", "Sometimes", false},
		{"CanSeek", true, false},
	} {
		if err := set(step.name, step.v); (err == nil) != step.ok {
			t.Errorf("set %s=%v: %v", step.name, step.v, err)
		}
	}
	if got, want := e.ctl.got(), []string{"Shuffle on", "Shuffle off", "Loop Track", "Loop Playlist", "Loop None"}; !reflect.DeepEqual(got, want) {
		t.Errorf("controls %v, want %v", got, want)
	}
	// A failing control rejects the write.
	e.ctl.fail(errors.New("boom"))
	if err := set("Shuffle", true); err == nil {
		t.Error("failed shuffle accepted")
	}
	// Wrong types are rejected without calling the controls.
	n := len(e.ctl.got())
	if set("Shuffle", "yes") == nil || set("LoopStatus", int32(1)) == nil || len(e.ctl.got()) != n {
		t.Error("wrong types accepted")
	}
}

// AC #4: changes are announced; Position is stored without a signal.
func TestUpdateAnnouncesChanges(t *testing.T) {
	e := newEnv(t)
	if err := e.client.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchMember("PropertiesChanged")); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 32)
	e.client.Signal(sigs)
	changed := func() map[string]any {
		t.Helper()
		out := map[string]any{}
		deadline := time.After(2 * time.Second)
		for {
			select {
			case s := <-sigs:
				if s.Body[0] != ifacePlayer {
					continue
				}
				for k, v := range s.Body[1].(map[string]dbus.Variant) {
					out[k] = v.Value()
				}
			case <-time.After(150 * time.Millisecond):
				return out
			case <-deadline:
				return out
			}
		}
	}

	tr := &Track{Handle: 7, ID: "1234", Title: "One More Time", Artists: "Daft Punk", Album: "Discovery", Length: 320 * time.Second}
	e.svc.Update(State{Status: Playing, Track: tr, Position: 5 * time.Second, Shuffle: true, Loop: LoopPlaylist, Volume: 0.8, CanNext: true})
	got := changed()
	for _, k := range []string{"Metadata", "PlaybackStatus", "Shuffle", "LoopStatus", "Volume", "CanGoNext", "CanPlay", "CanPause"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s not announced (got %v)", k, keys(got))
		}
	}
	if _, ok := got["Position"]; ok {
		t.Error("Position announced")
	}
	if _, ok := got["CanGoPrevious"]; ok {
		t.Error("an unchanged property was announced")
	}
	md := got["Metadata"].(map[string]dbus.Variant)
	want := map[string]any{
		"mpris:trackid": dbus.ObjectPath(trackPrefix + "7"),
		"mpris:length":  int64(320e6),
		"xesam:title":   "One More Time",
		"xesam:artist":  []string{"Daft Punk"},
		"xesam:album":   "Discovery",
		"xesam:url":     "https://tidal.com/browse/track/1234",
	}
	for k, v := range want {
		if !reflect.DeepEqual(md[k].Value(), v) {
			t.Errorf("metadata %s = %v, want %v", k, md[k].Value(), v)
		}
	}
	if p := e.get(t, ifacePlayer, "Position"); p != int64(5e6) {
		t.Errorf("position %v", p)
	}

	// Only the position moves: nothing is announced.
	same := *tr
	e.svc.Update(State{Status: Playing, Track: &same, Position: 6 * time.Second, Shuffle: true, Loop: LoopPlaylist, Volume: 0.8, CanNext: true})
	if got := changed(); len(got) != 0 {
		t.Errorf("announced %v for a position change", keys(got))
	}
	if p := e.get(t, ifacePlayer, "Position"); p != int64(6e6) {
		t.Errorf("position %v", p)
	}

	// Paused, then the queue empties: defaults fill in.
	e.svc.Update(State{Status: Paused, Track: &same, Shuffle: true, Loop: LoopPlaylist, Volume: 0.8, CanNext: true})
	if got := changed(); got["PlaybackStatus"] != "Paused" || len(got) != 1 {
		t.Errorf("pause announced %v", got)
	}
	e.svc.Update(State{})
	got = changed()
	if got["PlaybackStatus"] != "Stopped" || got["LoopStatus"] != "None" || got["CanPlay"] != false {
		t.Errorf("empty queue announced %v", got)
	}
	if md := got["Metadata"].(map[string]dbus.Variant); md["mpris:trackid"].Value() != noTrack {
		t.Errorf("empty metadata %v", md)
	}
}

// TASK-019 decision 95: Seek and SetPosition reach the player while a
// track plays, CanSeek follows, and every seek emits Seeked.
func TestSeeking(t *testing.T) {
	e := newEnv(t)
	if err := e.client.AddMatchSignal(dbus.WithMatchInterface(ifacePlayer), dbus.WithMatchMember("Seeked")); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 8)
	e.client.Signal(sigs)
	tr := &Track{Handle: 7, ID: "1", Title: "T", Length: 320 * time.Second}
	st := State{Status: Playing, Track: tr, Position: time.Minute, CanSeek: true}
	e.svc.Update(st)
	if v := e.get(t, ifacePlayer, "CanSeek"); v != true {
		t.Errorf("CanSeek = %v while playing", v)
	}
	call := func(method string, args ...any) {
		t.Helper()
		if err := e.obj.Call(ifacePlayer+"."+method, 0, args...).Err; err != nil {
			t.Errorf("%s%v: %v", method, args, err)
		}
	}
	call("Seek", int64(10e6))
	call("Seek", int64(-5e6))
	call("Seek", int64(math.MaxInt64)) // clamped, not wrapped
	call("SetPosition", dbus.ObjectPath(trackPrefix+"7"), int64(90e6))
	// Ignored, as the spec says: another track, outside the track, or a
	// path that is not a track.
	call("SetPosition", dbus.ObjectPath(trackPrefix+"8"), int64(90e6))
	call("SetPosition", dbus.ObjectPath(trackPrefix+"7"), int64(-1))
	call("SetPosition", dbus.ObjectPath(trackPrefix+"7"), int64(321e6))
	call("SetPosition", dbus.ObjectPath(trackPrefix+"x"), int64(1e6))
	call("SetPosition", noTrack, int64(1e6))
	want := []string{"Seek 10s", "Seek -5s", fmt.Sprintf("Seek %s", time.Duration(math.MaxInt64/1000)*time.Microsecond), "SetPosition 7 1m30s"}
	if got := e.ctl.got(); !reflect.DeepEqual(got, want) {
		t.Errorf("controls %v, want %v", got, want)
	}

	// A seek from anywhere (the keys too) emits Seeked with the new position.
	st.Position, st.SeekID = 95*time.Second, 1
	e.svc.Update(st)
	select {
	case s := <-sigs:
		if s.Name != ifacePlayer+".Seeked" || s.Body[0] != int64(95e6) {
			t.Errorf("signal %s %v", s.Name, s.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no Seeked signal")
	}
	// No seek, no signal.
	st.Position = 96 * time.Second
	e.svc.Update(st)
	select {
	case s := <-sigs:
		t.Errorf("unexpected signal %s %v", s.Name, s.Body)
	case <-time.After(150 * time.Millisecond):
	}

	// A failing seek returns the generic error.
	e.ctl.fail(errors.New("mpv broke"))
	var de dbus.Error
	if err := e.obj.Call(ifacePlayer+".Seek", 0, int64(1e6)).Err; !errors.As(err, &de) || de.Name != "org.freedesktop.DBus.Error.Failed" {
		t.Errorf("failure: %v", err)
	}
	e.ctl.fail(nil)

	// Loading or stopped: CanSeek is false and seeks are ignored.
	n := len(e.ctl.got())
	st.CanSeek = false
	e.svc.Update(st)
	if v := e.get(t, ifacePlayer, "CanSeek"); v != false {
		t.Errorf("CanSeek = %v while loading", v)
	}
	call("Seek", int64(10e6))
	call("SetPosition", dbus.ObjectPath(trackPrefix+"7"), int64(90e6))
	if len(e.ctl.got()) != n {
		t.Errorf("seeks reached the player while not seekable: %v", e.ctl.got()[n:])
	}
}

func TestTrackHandle(t *testing.T) {
	for p, want := range map[dbus.ObjectPath]uint64{trackPrefix + "7": 7, trackPrefix + "0": 0, trackPrefix: 0, trackPrefix + "-1": 0, noTrack: 0} {
		if h, ok := trackHandle(p); h != want || ok != (want != 0) {
			t.Errorf("trackHandle(%s) = %d %v", p, h, ok)
		}
	}
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestMetadata(t *testing.T) {
	// Optional fields are left out; a non-numeric ID gets no URL.
	md := metadata(&Track{Handle: 3, ID: "x/../1", Title: "T"})
	for _, k := range []string{"mpris:length", "xesam:artist", "xesam:album", "xesam:url"} {
		if _, ok := md[k]; ok {
			t.Errorf("%s set", k)
		}
	}
	for id, want := range map[string]bool{"1234": true, "": false, "12a": false, strings.Repeat("1", 21): false, strings.Repeat("1", 20): true} {
		if validID(id) != want {
			t.Errorf("validID(%q) = %v", id, !want)
		}
	}
	if !sameTrack(nil, nil) || sameTrack(nil, &Track{}) || sameTrack(&Track{Handle: 1}, &Track{Handle: 2}) || !sameTrack(&Track{Handle: 1}, &Track{Handle: 1}) {
		t.Error("sameTrack")
	}
}
